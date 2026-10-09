package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/xhwarp/warp-client/internal/core"
)

// Pages of the shell. The window opens on the page the user left, so these
// values are persisted and must stay stable across releases.
const (
	pageHome     = "home"
	pageNodes    = "nodes"
	pageRules    = "rules"
	pageSettings = "settings"
	pageLogs     = "logs"
)

// Appearance. ThemeAuto follows the system's light/dark setting.
const (
	themeLight = "light"
	themeDark  = "dark"
	themeAuto  = "auto"
)

// UI languages.
const (
	langZH = "zh"
	langEN = "en"
)

// Proxy modes, mapped straight onto the core's `mode:` field: everything
// through the exit, rules decide, or nothing is proxied at all.
const (
	modeGlobal = "global"
	modeRule   = "rule"
	modeDirect = "direct"
)

// clientSettings is the persisted preference set. It is deliberately separate
// from clientState: a session is ephemeral (it can end at any moment, and
// disconnect clears it), while these choices are the user's and must survive a
// restart, a crash, or a reconnect. Everything a settings page can change lives
// here, so the file is also the single place to look when asking "what is this
// install configured to do".
type clientSettings struct {
	// Appearance. Applied live — switching theme or language repaints the
	// window, it does not restart it.
	Theme string `json:"theme"`
	Lang  string `json:"lang"`

	// Session choices, remembered so a restart starts where you left off.
	Proto      string `json:"proto"`
	OuterProto string `json:"outer_proto"`
	InnerProto string `json:"inner_proto"`
	Tun        bool   `json:"tun"`
	Rescan     bool   `json:"rescan"`
	Psiphon    bool   `json:"psiphon"`
	AutoRepair bool   `json:"auto_repair"`

	// Local proxy: where clients point, and where the core's API listens.
	MixedPort   int  `json:"mixed_port"`
	APIPort     int  `json:"api_port"`
	SystemProxy bool `json:"system_proxy"`

	// Network.
	IPv6       bool `json:"ipv6"`
	MTU        int  `json:"mtu"`
	TimeoutSec int  `json:"timeout_sec"`

	// DNS: DNSCustom is a comma separated resolver list (empty = not set);
	// DNSOff stops the client from handing any resolver to the tunnel at all.
	DNSCustom string `json:"dns_custom"`
	DNSOff    bool   `json:"dns_off"`

	// Split tunnelling: SplitMode is splitOff / splitProxy (only the listed
	// apps use the tunnel) / splitDirect (the listed apps bypass it). SplitApps
	// holds process names, e.g. "chrome.exe, steam.exe".
	SplitMode string `json:"split_mode"`
	SplitApps string `json:"split_apps"`

	// ProxyMode decides what the rules below are for: modeGlobal sends
	// everything to the exit (no rules at all), modeRule honours them, and
	// modeDirect bypasses the proxy entirely.
	ProxyMode string `json:"proxy_mode"`

	// Domain lists: comma separated suffixes routed straight (DomainDirect) or
	// through the exit (DomainProxy). They become DOMAIN-SUFFIX rules, which
	// need no geo data. The list order decides: proxy rules are emitted first,
	// so a domain in both lists goes through the tunnel.
	DomainDirect string `json:"domain_direct"`
	DomainProxy  string `json:"domain_proxy"`

	// GeoSplit routes mainland domains and IPs direct and everything else
	// through the exit. It needs the core's geosite.dat/geoip.metadb: without
	// those files the core refuses the config (it would try to download them),
	// so the renderer only emits these rules when the data is present.
	GeoSplit bool `json:"geo_split"`

	// Behaviour.
	AutoConnect   bool `json:"auto_connect"`
	AutoStart     bool `json:"auto_start"`
	LogAutoScroll bool `json:"log_auto_scroll"`
	RememberPage  bool `json:"remember_page"`

	// Starred holds endpoints marked as favourites; the node list shows them
	// first and they survive a rescan.
	Starred []string `json:"starred"`

	// Page is the page the window opens on, so the client comes back where the
	// user left it.
	Page string `json:"page"`
}

const settingsName = "settings.json"

const (
	splitOff    = "off"
	splitProxy  = "proxy"
	splitDirect = "direct"
)

func defaultSettings() clientSettings {
	return clientSettings{
		Theme:         themeLight,
		Lang:          langZH,
		Proto:         protoAWG,
		OuterProto:    protoWG,
		InnerProto:    protoWG,
		AutoRepair:    true,
		MixedPort:     core.DefaultMixedPort,
		APIPort:       core.DefaultAPIPort,
		SystemProxy:   true,
		TimeoutSec:    2,
		SplitMode:     splitOff,
		ProxyMode:     modeRule,
		LogAutoScroll: true,
		RememberPage:  true,
		Page:          pageHome,
	}
}

func settingsPath() (string, error) {
	d, err := appDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, settingsName), nil
}

// loadSettings returns the persisted preferences. A missing file is normal (a
// fresh install), and a corrupt one is never fatal: either way the caller gets
// defaults and the next save rewrites the file.
func loadSettings() clientSettings {
	s := defaultSettings()
	p, err := settingsPath()
	if err != nil {
		return s
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return s
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return defaultSettings()
	}
	s.normalize()
	return s
}

func saveSettings(s clientSettings) error {
	p, err := settingsPath()
	if err != nil {
		return err
	}
	s.normalize()
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, raw, 0o600)
}

// normalize clamps everything that reaches the renderer. A hand-edited file
// must not be able to produce a core config that fails to parse, nor an
// unopenable window — a bad port, MTU or page name here would look like "the
// client is broken", not "the file is wrong".
func (s *clientSettings) normalize() {
	switch s.Theme {
	case themeLight, themeDark, themeAuto:
	default:
		s.Theme = themeLight
	}
	switch s.Lang {
	case langZH, langEN:
	default:
		s.Lang = langZH
	}
	if s.Proto == "" {
		s.Proto = protoAWG
	}
	if s.OuterProto == "" {
		s.OuterProto = protoWG
	}
	if s.InnerProto == "" {
		s.InnerProto = protoWG
	}
	switch s.ProxyMode {
	case modeGlobal, modeRule, modeDirect:
	default:
		s.ProxyMode = modeRule
	}
	if s.MixedPort < 1 || s.MixedPort > 65535 {
		s.MixedPort = core.DefaultMixedPort
	}
	if s.APIPort < 1 || s.APIPort > 65535 {
		s.APIPort = core.DefaultAPIPort
	}
	if s.APIPort == s.MixedPort {
		s.APIPort = s.MixedPort + 1
	}
	// 0 means "let the core pick" (mihomo's own default), which is what an
	// unset MTU has always meant here.
	if s.MTU < 0 || s.MTU > 9000 {
		s.MTU = 0
	}
	if s.TimeoutSec < 1 || s.TimeoutSec > 60 {
		s.TimeoutSec = 2
	}
	switch s.SplitMode {
	case splitOff, splitProxy, splitDirect:
	default:
		s.SplitMode = splitOff
	}
	switch s.Page {
	case pageHome, pageNodes, pageRules, pageSettings, pageLogs:
	default:
		s.Page = pageHome
	}
	out := s.Starred[:0]
	for _, e := range s.Starred {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	s.Starred = out
}

// splitAppList returns the process names the user listed, de-duplicated and in
// order. Commas, semicolons and whitespace all separate, so a list pasted from
// anywhere works without hand-editing.
func (s clientSettings) splitAppList() []string { return fieldList(s.SplitApps) }

// domainDirectList / domainProxyList are the two domain suffix lists.
func (s clientSettings) domainDirectList() []string { return fieldList(s.DomainDirect) }
func (s clientSettings) domainProxyList() []string  { return fieldList(s.DomainProxy) }

// fieldList splits a comma/semicolon/whitespace separated list, de-duplicated.
func fieldList(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	seen := make(map[string]bool, len(fields))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		key := strings.ToLower(f)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	return out
}

// splitActive reports whether per-app split tunnelling should emit rules.
func (s clientSettings) splitActive() bool {
	return s.SplitMode != splitOff && len(s.splitAppList()) > 0
}

// starred reports whether an endpoint is a favourite.
func (s clientSettings) starred(endpoint string) bool {
	for _, e := range s.Starred {
		if e == endpoint {
			return true
		}
	}
	return false
}

// toggleStar adds or removes an endpoint from the favourites, returning the new
// list. Sorted on write so the file stays diff-friendly.
func toggleStar(list []string, endpoint string) []string {
	out := make([]string, 0, len(list)+1)
	found := false
	for _, e := range list {
		if e == endpoint {
			found = true
			continue
		}
		out = append(out, e)
	}
	if !found {
		out = append(out, endpoint)
	}
	sortStrings(out)
	return out
}

func sortStrings(v []string) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}

// apply folds the preference file into the options a connect will run with.
// Session choices (proto, tun, rescan, psiphon, pinned endpoint, protocol pair)
// stay the caller's: they describe this connect, not the preference file.
func (s clientSettings) apply(o *options) {
	if s.MixedPort > 0 {
		o.mixedPort = s.MixedPort
	}
	if s.APIPort > 0 {
		o.apiPort = s.APIPort
	}
	o.ipv6 = s.IPv6
	o.mtu = s.MTU
	if s.TimeoutSec > 0 {
		o.timeoutSec = s.TimeoutSec
	}
	o.dns = strings.TrimSpace(s.DNSCustom)
	o.noDNS = s.DNSOff
	// SystemProxy is the user-facing switch; the engine's flag is its inverse.
	o.noProxy = !s.SystemProxy
	o.splitMode = s.SplitMode
	o.splitApps = s.splitAppList()
	o.proxyMode = s.ProxyMode
	o.domainDirect = s.domainDirectList()
	o.domainProxy = s.domainProxyList()
	o.geoSplit = s.GeoSplit
	o.noGuard = !s.AutoRepair
}
