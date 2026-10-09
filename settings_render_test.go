package main

import (
	"os"
	"strings"
	"testing"
)

// TestRenderSplitConfig renders the config for both split-tunnelling modes and
// writes them out, so they can be validated against the real core with
// `mihomo -t`. The rule order is the thing under test: PROCESS rules must come
// before the MATCH fallback, and splitProxy must fall back to DIRECT - a
// fallback of WARP there would silently tunnel exactly the apps the user asked
// to leave alone.
func TestRenderSplitConfig(t *testing.T) {
	base := options{
		mixedPort: 7890,
		apiPort:   9090,
		mtu:       1280,
		dns:       "223.5.5.5, 1.1.1.1",
		splitApps: []string{"chrome.exe", "steam.exe"},
	}
	ce := cachedEndpoint{Endpoint: "162.159.192.1:2408"}
	run := protoRun{kind: kindWG, name: protoWG}

	cases := []struct {
		mode    string
		file    string
		want    []string
		notWant []string
	}{
		{
			mode: splitDirect,
			file: "split-direct-test.yaml",
			want: []string{
				"find-process-mode: strict",
				"PROCESS-NAME,chrome.exe,DIRECT",
				"PROCESS-NAME,steam.exe,DIRECT",
				"  - MATCH,WARP",
				"mtu: 1280",
			},
		},
		{
			mode: splitProxy,
			file: "split-proxy-test.yaml",
			want: []string{
				"PROCESS-NAME,chrome.exe,WARP",
				"PROCESS-NAME,steam.exe,WARP",
				"  - MATCH,DIRECT",
			},
			notWant: []string{"  - MATCH,WARP"},
		},
	}

	for _, tc := range cases {
		opts := base
		opts.splitMode = tc.mode
		if tc.mode == splitDirect {
			opts.wantMeta = true
		}
		b, err := renderClientConfig(opts, ce, run)
		if err != nil {
			t.Fatalf("%s: renderClientConfig: %v", tc.mode, err)
		}
		cfg := string(b)
		for _, w := range tc.want {
			if !strings.Contains(cfg, w) {
				t.Errorf("%s: config missing %q\n%s", tc.mode, w, cfg)
			}
		}
		for _, w := range tc.notWant {
			if strings.Contains(cfg, w) {
				t.Errorf("%s: config unexpectedly contains %q\n%s", tc.mode, w, cfg)
			}
		}
		if err := os.WriteFile(tc.file, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
