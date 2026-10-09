package main

import (
	"encoding/base64"
	"fmt"
	"strings"
	"sync"

	"github.com/xhwarp/warp-client/internal/core"
)

// Capability probing: the UI must not offer a protocol the running mihomo
// cannot parse. Rather than mapping versions to feature flags by hand (which
// rots the moment the upstream adds or renames a field), each protocol is
// probed by feeding the actual core a minimal, syntactically valid config of
// exactly the shape connect would render and asking `mihomo -t` to validate
// it. Exit 0 = supported; anything else = "please upgrade the core".

// Deterministic throwaway key material: the probe only needs parseable keys,
// never a tunnel that answers. base64(32 zero-ish bytes) is a valid WireGuard
// key format; the WARP public key is public by definition.
const (
	probeWGPrivateKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	probeWGPublicKey  = "bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo="
)

// probeYAML renders the minimal config one protocol's connect config is shaped
// like. It must track mihomoPeer's field set in wgconf.go: a probe config that
// drifts from the real render would pass while the real connect fails (or
// vice versa).
func probeYAML(proto string) (string, error) {
	wgPeer := func(indent string) string {
		// indent lands the key at the proxy-item field level; the list and its
		// fields step in by 2/4 — anything deeper is a different mapping and
		// mihomo rejects the file as invalid YAML.
		return fmt.Sprintf(`%speers:
%s  - server: 162.159.192.1
%s    port: 2408
%s    public-key: %s
%s    allowed-ips: ['0.0.0.0/0']
%s    persistent-keepalive: 25
`, indent, indent, indent, indent, probeWGPublicKey, indent, indent)
	}
	switch proto {
	case protoWG, protoAWG:
		var b strings.Builder
		b.WriteString("proxies:\n  - name: \"PROBE\"\n    type: wireguard\n")
		b.WriteString("    private-key: " + probeWGPrivateKey + "\n    ip: 172.16.0.2\n")
		b.WriteString(wgPeer("    "))
		b.WriteString("    mtu: 1280\n    udp: true\n    remote-dns-resolve: true\n    dns: ['1.1.1.1']\n")
		if proto == protoAWG {
			b.WriteString(`    amnezia-wg-option:
      jc: 3
      jmin: 10
      jmax: 50
      s1: 0
      s2: 0
      h1: 1
      h2: 2
      h3: 3
      h4: 4
`)
		}
		b.WriteString("rules:\n  - MATCH,PROBE\n")
		return b.String(), nil

	case protoWWarp:
		// dialer-proxy chaining: the outer half carries no DNS, the inner one
		// dials through it — exactly mihomoProxies' chain render.
		var b strings.Builder
		b.WriteString("proxies:\n  - name: \"PROBE OUTER\"\n    type: wireguard\n")
		b.WriteString("    private-key: " + probeWGPrivateKey + "\n    ip: 172.16.0.2\n")
		b.WriteString(wgPeer("    "))
		b.WriteString("    mtu: 1280\n    udp: true\n")
		b.WriteString("  - name: \"PROBE INNER\"\n    type: wireguard\n")
		b.WriteString("    private-key: " + probeWGPrivateKey + "\n    ip: 172.16.0.3\n")
		b.WriteString(wgPeer("    "))
		b.WriteString("    mtu: 1220\n    udp: true\n    remote-dns-resolve: true\n    dns: ['1.1.1.1']\n")
		b.WriteString("    dialer-proxy: \"PROBE OUTER\"\n")
		b.WriteString("rules:\n  - MATCH,PROBE INNER\n")
		return b.String(), nil

	case protoMASQUE, protoMASQUEH2:
		privDER, pubDER, err := genECKeyPair()
		if err != nil {
			return "", err
		}
		var b strings.Builder
		b.WriteString("proxies:\n  - name: \"PROBE\"\n    type: masque\n    server: 162.159.198.2\n    port: 443\n")
		if proto == protoMASQUEH2 {
			b.WriteString("    network: h2\n")
		}
		fmt.Fprintf(&b, "    sni: %s\n    private-key: %s\n    public-key: %s\n",
			masqueSNI, base64.StdEncoding.EncodeToString(privDER), base64.StdEncoding.EncodeToString(pubDER))
		b.WriteString("    ip: 172.16.0.2\n    udp: true\n    remote-dns-resolve: true\n    dns: ['1.1.1.1']\n")
		b.WriteString("rules:\n  - MATCH,PROBE\n")
		return b.String(), nil
	}
	return "", fmt.Errorf("no probe for proto %q", proto)
}

// protoSupportedBy reports whether the core binary accepts this protocol's
// config shape. Unknown probe errors count as unsupported: the caller shows
// "please upgrade the core", which is the honest safe answer.
func protoSupportedBy(bin, workDir, proto string) bool {
	_, supported, _ := probeCoreCapabilities(bin, workDir)
	return supported[proto]
}

// coreCapabilities caches one probe round per binary path.
var (
	capMu     sync.Mutex
	capBin    string
	capVer    string
	capResult map[string]bool
)

// resetCoreCapabilityCache drops the cached probe result so the next
// probeCoreCapabilities re-runs against a (possibly replaced) binary — a
// re-download lands on the same path the cache is keyed by.
func resetCoreCapabilityCache() {
	capMu.Lock()
	capResult = nil
	capBin = ""
	capMu.Unlock()
}

// probeCoreCapabilities probes every protocol against bin and caches the
// outcome. It is safe to call repeatedly; only the first call runs mihomo.
func probeCoreCapabilities(bin, workDir string) (version string, supported map[string]bool, err error) {
	capMu.Lock()
	defer capMu.Unlock()
	if capResult != nil && capBin == bin {
		return capVer, capResult, nil
	}
	ver, verr := core.BinaryVersion(bin)
	supported = map[string]bool{}
	var firstErr error
	for _, r := range protoRuns {
		yaml, err := probeYAML(r.name)
		if err != nil {
			supported[r.name] = false
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		supported[r.name] = core.TestConfig(bin, workDir, []byte(yaml)) == nil
	}
	if verr != nil && firstErr == nil {
		firstErr = verr
	}
	capBin, capVer, capResult = bin, ver, supported
	return ver, supported, firstErr
}
