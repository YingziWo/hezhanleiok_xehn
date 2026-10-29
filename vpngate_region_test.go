package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A chosen country must survive the failure memory and the last-good shortcut.
//
// The report behind this test: the user picked the United States, every US relay
// had failed within the ten-minute window, `freshVPNGateNodes` dropped them all,
// and the last-good Japanese relay was promoted to the front — so the client
// connected to Japan while the selector said US.
func TestCandidatesKeepChosenCountry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WARP_CLIENT_HOME", dir)

	now := time.Now().Format(time.RFC3339Nano)
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Both US relays failed a minute ago; the last good relay was Japanese.
	write("vpngate-failed.json", `{"at":{"us-1.1.1.1-443":"`+now+`","us-2.2.2.2-443":"`+now+`"}}`)
	write("vpngate-last.json", `{"id":"jp-9.9.9.9-443","country":"JP","at":"`+now+`"}`)

	// Proto left empty: probeVPNGateLatency only dials tcp nodes, so the test
	// exercises the ordering without touching the network.
	nodes := []vpnGateNode{
		{IP: "1.1.1.1", Port: 443, CountryShort: "US", Profile: "p"},
		{IP: "2.2.2.2", Port: 443, CountryShort: "US", Profile: "p"},
		{IP: "9.9.9.9", Port: 443, CountryShort: "JP", Profile: "p"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := preferLastGoodVPNGate(vpnGateCandidates(ctx, nodes, "US", "", 24), "", "US")
	if len(got) == 0 {
		t.Fatal("no candidates")
	}
	if got[0].CountryShort != "US" {
		t.Fatalf("first candidate is %s, want US (region must not be overridden)", got[0].CountryShort)
	}
	if n := len(nodesInCountry(got, "US")); n != 2 {
		t.Fatalf("US relays in the list = %d, want 2 (failure memory must not empty the region)", n)
	}
}

// Without a region the list is free to start anywhere, but it must not crash or
// come back empty, and a foreign last-good relay may lead.
func TestCandidatesWithoutRegion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WARP_CLIENT_HOME", dir)

	nodes := []vpnGateNode{
		{IP: "1.1.1.1", Port: 443, CountryShort: "US", Profile: "p"},
		{IP: "9.9.9.9", Port: 443, CountryShort: "JP", Profile: "p"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := vpnGateCandidates(ctx, nodes, "", "", 24)
	if len(got) != 2 {
		t.Fatalf("candidates = %d, want 2", len(got))
	}
	if hasCountry(got, "US") == false || hasCountry(got, "JP") == false {
		t.Fatalf("both countries should be present: %+v", got)
	}
}
