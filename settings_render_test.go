package main

import (
	"os"
	"strings"
	"testing"
)

// TestRenderRoutingPresets pins the five presets' rule sets: global ignores the
// domain lists, direct turns mihomo off the proxy, blacklist falls back to
// DIRECT, and custom keeps the two lists in the documented order (proxy rules
// first, so a domain in both lists takes the tunnel).
func TestRenderRoutingPresets(t *testing.T) {
	base := options{
		mixedPort:    7890,
		apiPort:      9090,
		mtu:          1280,
		dns:          "223.5.5.5, 1.1.1.1",
		domainDirect: []string{"intra.example"},
		domainProxy:  []string{"example.com"},
	}
	ce := cachedEndpoint{Endpoint: "162.159.192.1:2408"}
	run := protoRun{kind: kindWG, name: protoWG}

	cases := []struct {
		name    string
		mode    string
		want    []string
		notWant []string
	}{
		{
			name:    "global",
			mode:    modeGlobal,
			want:    []string{"mode: rule", "  - MATCH,WARP"},
			notWant: []string{"DOMAIN-SUFFIX", "GEOSITE"},
		},
		{
			name:    "direct",
			mode:    modeDirect,
			want:    []string{"mode: direct", "  - MATCH,DIRECT"},
			notWant: []string{"  - MATCH,WARP", "DOMAIN-SUFFIX"},
		},
		{
			name: "blacklist",
			mode: modeBlacklist,
			want: []string{
				"DOMAIN-SUFFIX,example.com,WARP",
				"  - MATCH,DIRECT",
			},
			notWant: []string{"DOMAIN-SUFFIX,intra.example,DIRECT", "  - MATCH,WARP"},
		},
		{
			name: "custom",
			mode: modeCustom,
			want: []string{
				"DOMAIN-SUFFIX,example.com,WARP",
				"DOMAIN-SUFFIX,intra.example,DIRECT",
				"  - MATCH,WARP",
			},
		},
		{
			name: "bypass",
			mode: modeBypass,
			want: []string{
				"DOMAIN-SUFFIX,example.com,WARP",
				"DOMAIN-SUFFIX,intra.example,DIRECT",
				"  - MATCH,WARP",
			},
		},
	}

	for _, tc := range cases {
		opts := base
		opts.proxyMode = tc.mode
		b, err := renderClientConfig(opts, ce, run)
		if err != nil {
			t.Fatalf("%s: renderClientConfig: %v", tc.name, err)
		}
		cfg := string(b)
		for _, w := range tc.want {
			if !strings.Contains(cfg, w) {
				t.Errorf("%s: config missing %q\n%s", tc.name, w, cfg)
			}
		}
		for _, w := range tc.notWant {
			if strings.Contains(cfg, w) {
				t.Errorf("%s: config unexpectedly contains %q\n%s", tc.name, w, cfg)
			}
		}
		if err := os.WriteFile("routing-"+tc.name+"-test.yaml", b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestRenderDNSLeak checks the leak-protection block: respect-rules plus a
// proxy-server resolver, the resolvers' own addresses pinned to the tunnel, and
// the TUN hijack. Each has to disappear when the switch is off or the proxy is
// bypassed, or the config would promise protection it does not provide.
func TestRenderDNSLeak(t *testing.T) {
	ce := cachedEndpoint{Endpoint: "162.159.192.1:2408"}
	run := protoRun{kind: kindWG, name: protoWG}
	base := options{
		mixedPort: 7890,
		apiPort:   9090,
		dns:       "223.5.5.5, 1.1.1.1",
		dnsLeak:   true,
		proxyMode: modeCustom,
	}

	cases := []struct {
		name    string
		mutate  func(*options)
		want    []string
		notWant []string
	}{
		{
			name:   "protected",
			mutate: func(o *options) {},
			want: []string{
				"respect-rules: true",
				"proxy-server-nameserver:",
				"IP-CIDR,223.5.5.5/32,WARP",
				"IP-CIDR,1.1.1.1/32,WARP",
			},
		},
		{
			name:    "switch off",
			mutate:  func(o *options) { o.dnsLeak = false },
			notWant: []string{"respect-rules", "proxy-server-nameserver", "IP-CIDR,223.5.5.5/32"},
		},
		{
			name:    "no dns handed out",
			mutate:  func(o *options) { o.noDNS = true },
			notWant: []string{"dns:\n", "respect-rules"},
		},
		{
			name:    "direct mode",
			mutate:  func(o *options) { o.proxyMode = modeDirect },
			notWant: []string{"respect-rules", "dns-hijack"},
		},
		{
			name:   "tun hijack",
			mutate: func(o *options) { o.tunMode = true },
			want:   []string{"dns-hijack:\n    - any:53"},
		},
		{
			name: "lan resolver stays direct",
			mutate: func(o *options) {
				o.dns = "192.168.1.1, 1.1.1.1"
			},
			want: []string{"IP-CIDR,192.168.1.1/32,DIRECT", "IP-CIDR,1.1.1.1/32,WARP"},
		},
	}

	for _, tc := range cases {
		opts := base
		tc.mutate(&opts)
		b, err := renderClientConfig(opts, ce, run)
		if err != nil {
			t.Fatalf("%s: renderClientConfig: %v", tc.name, err)
		}
		cfg := string(b)
		for _, w := range tc.want {
			if !strings.Contains(cfg, w) {
				t.Errorf("%s: config missing %q\n%s", tc.name, w, cfg)
			}
		}
		for _, w := range tc.notWant {
			if strings.Contains(cfg, w) {
				t.Errorf("%s: config unexpectedly contains %q\n%s", tc.name, w, cfg)
			}
		}
	}
}

// TestRenderCaptureAndFamily pins the config-level effect of the settings page:
// LAN access widens the listener, TUN adds its block, and the address family
// decides both the core's IPv6 switch and the outbound's addresses/routes.
func TestRenderCaptureAndFamily(t *testing.T) {
	ce := cachedEndpoint{Endpoint: "162.159.192.1:2408"}
	run := protoRun{kind: kindWG, name: protoWG}
	base := options{mixedPort: 7890, apiPort: 9090, mtu: 1280, dns: "223.5.5.5"}

	cases := []struct {
		name    string
		mutate  func(*options)
		want    []string
		notWant []string
	}{
		{
			name:   "lan on",
			mutate: func(o *options) { o.allowLan = true },
			want:   []string{`allow-lan: true`, `bind-address: "*"`},
		},
		{
			name:    "lan off",
			mutate:  func(o *options) { o.allowLan = false },
			want:    []string{"allow-lan: false"},
			notWant: []string{"bind-address:"},
		},
		{
			name:   "tun",
			mutate: func(o *options) { o.tunMode = true },
			want:   []string{"tun:\n  enable: true", "listen: 127.0.0.1:1053"},
		},
		{
			name:    "v4 only",
			mutate:  func(o *options) { o.family = famV4 },
			want:    []string{"ipv6: false", "ip: " + warpAddress, "allowed-ips: ['0.0.0.0/0']"},
			notWant: []string{"ipv6: " + warpAddressV6, "::/0"},
		},
		{
			name:   "v6 only",
			mutate: func(o *options) { o.family = famV6 },
			want: []string{
				"ipv6: true",
				"ipv6: " + warpAddressV6,
				"allowed-ips: ['::/0']",
			},
			notWant: []string{"ip: " + warpAddress, "0.0.0.0/0"},
		},
		{
			name:   "dual stack",
			mutate: func(o *options) { o.family = famBoth },
			want: []string{
				"ipv6: true",
				"ip: " + warpAddress,
				"ipv6: " + warpAddressV6,
				"allowed-ips: ['0.0.0.0/0', '::/0']",
			},
		},
	}

	for _, tc := range cases {
		opts := base
		tc.mutate(&opts)
		b, err := renderClientConfig(opts, ce, run)
		if err != nil {
			t.Fatalf("%s: renderClientConfig: %v", tc.name, err)
		}
		cfg := string(b)
		for _, w := range tc.want {
			if !strings.Contains(cfg, w) {
				t.Errorf("%s: config missing %q\n%s", tc.name, w, cfg)
			}
		}
		for _, w := range tc.notWant {
			if strings.Contains(cfg, w) {
				t.Errorf("%s: config unexpectedly contains %q\n%s", tc.name, w, cfg)
			}
		}
	}
}
