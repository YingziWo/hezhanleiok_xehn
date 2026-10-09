package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSettingsRoundTrip pins the two properties the settings page depends on:
// a written file is read back byte-for-byte equivalent, and a value that came
// from the UI survives a save/load cycle. The page field is checked explicitly
// because it decides which page the window opens on.
func TestSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WARP_CLIENT_HOME", dir)

	in := defaultSettings()
	in.Page = pageSettings
	in.Theme = themeDark
	in.Lang = langEN
	in.MixedPort = 1080
	in.IPv6 = true
	in.MTU = 1280
	in.DNSCustom = "223.5.5.5, 1.1.1.1"
	in.SplitMode = splitProxy
	in.SplitApps = "chrome.exe, steam.exe"
	in.SystemProxy = false

	if err := saveSettings(in); err != nil {
		t.Fatalf("saveSettings: %v", err)
	}
	p := filepath.Join(dir, settingsName)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("settings file not written: %v", err)
	}

	// A BOM would make encoding/json reject the file and silently reset every
	// preference to its default, so the bytes are checked directly.
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		t.Fatal("settings.json written with a UTF-8 BOM")
	}

	out := loadSettings()
	if out.Page != pageSettings {
		t.Errorf("page = %q, want %q", out.Page, pageSettings)
	}
	if out.Theme != themeDark || out.Lang != langEN {
		t.Errorf("theme/lang = %q/%q, want dark/en", out.Theme, out.Lang)
	}
	if out.MixedPort != 1080 {
		t.Errorf("mixed port = %d, want 1080", out.MixedPort)
	}
	if !out.IPv6 || out.MTU != 1280 {
		t.Errorf("ipv6/mtu = %v/%d, want true/1280", out.IPv6, out.MTU)
	}
	if out.DNSCustom != in.DNSCustom {
		t.Errorf("dns = %q, want %q", out.DNSCustom, in.DNSCustom)
	}
	if out.SplitMode != splitProxy || len(out.splitAppList()) != 2 {
		t.Errorf("split = %q %v, want proxy with 2 apps", out.SplitMode, out.splitAppList())
	}
	if out.SystemProxy {
		t.Error("system proxy = true, want false")
	}
	if out.AutoStart {
		t.Error("autostart = true, want false")
	}
}

// TestSettingsNormalize covers the guards: nothing a hand-edited file can hold
// may reach the renderer as an unusable value, and an unknown page falls back to
// the connect page rather than leaving the window blank.
func TestSettingsNormalize(t *testing.T) {
	s := clientSettings{
		MixedPort:  0,
		APIPort:    0,
		MTU:        -5,
		TimeoutSec: 900,
		SplitMode:  "sometimes",
		Page:       "what",
		Theme:      "neon",
		Lang:       "fr",
		ProxyMode:  "sometimes",
	}
	s.normalize()
	if s.MixedPort <= 0 || s.APIPort <= 0 {
		t.Errorf("ports not defaulted: %d/%d", s.MixedPort, s.APIPort)
	}
	if s.APIPort == s.MixedPort {
		t.Error("api port left equal to the mixed port")
	}
	if s.MTU != 0 {
		t.Errorf("mtu = %d, want 0 (automatic)", s.MTU)
	}
	if s.TimeoutSec != 2 {
		t.Errorf("timeout = %d, want the default", s.TimeoutSec)
	}
	if s.SplitMode != splitOff {
		t.Errorf("split mode = %q, want off", s.SplitMode)
	}
	if s.Page != pageHome {
		t.Errorf("page = %q, want %q", s.Page, pageHome)
	}
	if s.Theme != themeLight || s.Lang != langZH || s.ProxyMode != modeRule {
		t.Errorf("theme/lang/mode = %q/%q/%q, want light/zh/rule", s.Theme, s.Lang, s.ProxyMode)
	}
}
