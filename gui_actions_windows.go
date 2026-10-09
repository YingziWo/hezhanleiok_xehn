//go:build windows

package main

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lxn/walk"
)

// actionPages routes every id that is not part of the connect pipeline (which
// action() handles). The prefixes are the contract between a paint function and
// this file: nav:, theme:, lang:, ruletab:, settab:, logtab:, mode:, node
// filter/sort, star:, set:, about:, upd:, go:.
func (u *natUI) actionPages(id string) {
	m := u.m
	switch {
	case strings.HasPrefix(id, "nav:"):
		u.gotoPage(strings.TrimPrefix(id, "nav:"))

	case strings.HasPrefix(id, "go:"):
		u.gotoPage(strings.TrimPrefix(id, "go:"))

	case id == "theme:cycle":
		u.cycleTheme()
	case strings.HasPrefix(id, "theme:"):
		v := strings.TrimPrefix(id, "theme:")
		u.mutateSettings(func(s *clientSettings) { s.Theme = v })
		u.applyTheme()

	case id == "lang:cycle":
		next := langEN
		if m.set.Lang == langEN {
			next = langZH
		}
		u.mutateSettings(func(s *clientSettings) { s.Lang = next })
		u.applyLang()

	case strings.HasPrefix(id, "lang:"):
		v := strings.TrimPrefix(id, "lang:")
		u.mutateSettings(func(s *clientSettings) { s.Lang = v })
		u.applyLang()

	case strings.HasPrefix(id, "ruletab:"):
		m.ruleTab = strings.TrimPrefix(id, "ruletab:")
		u.invalidate()
	case strings.HasPrefix(id, "settab:"):
		m.setTab = strings.TrimPrefix(id, "settab:")
		u.invalidate()
	case strings.HasPrefix(id, "logtab:"):
		m.logTab = strings.TrimPrefix(id, "logtab:")
		u.invalidate()

	case strings.HasPrefix(id, "nodefilter:"):
		m.nodeFilter = strings.TrimPrefix(id, "nodefilter:")
		m.scroll = 0
		u.invalidate()
	case strings.HasPrefix(id, "nodesort:"):
		m.nodeSort = strings.TrimPrefix(id, "nodesort:")
		m.scroll = 0
		u.invalidate()
	case id == "nodesearch":
		if v, ok := u.promptText(tr("搜索节点"), tr("名称、国家、城市或 IP"), m.nodeSearch); ok {
			m.nodeSearch = strings.TrimSpace(v)
			m.scroll = 0
			u.invalidate()
		}
	case id == "nodesearch:clear":
		m.nodeSearch = ""
		m.scroll = 0
		u.invalidate()

	case strings.HasPrefix(id, "star:"):
		ep := strings.TrimPrefix(id, "star:")
		u.mutateSettings(func(s *clientSettings) { s.Starred = toggleStar(s.Starred, ep) })

	case strings.HasPrefix(id, "set:"):
		u.actionSetting(id)
	case strings.HasPrefix(id, "about:"):
		u.actionAbout(id)
	case strings.HasPrefix(id, "upd:"):
		u.actionUpdate(id)
	}
}

// gotoPage switches pages. The choice is persisted so the window reopens where
// the user left it — unless they turned that off.
func (u *natUI) gotoPage(page string) {
	switch page {
	case pageHome, pageNodes, pageRules, pageSettings, pageLogs:
	default:
		return
	}
	u.m.page = page
	u.m.set.Page = page
	if !u.m.set.RememberPage {
		u.m.set.Page = pageHome
	}
	if err := saveSettings(u.m.set); err != nil {
		u.addLog(tr("设置保存失败: ")+err.Error(), true)
	}
	u.m.scroll = 0
	u.invalidate()
}

func (u *natUI) cycleTheme() {
	next := themeDark
	switch u.m.set.Theme {
	case themeLight:
		next = themeDark
	case themeDark:
		next = themeAuto
	default:
		next = themeLight
	}
	u.mutateSettings(func(s *clientSettings) { s.Theme = next })
	u.applyTheme()
}

// ---------------------------------------------------------------------------
// Preferences
// ---------------------------------------------------------------------------

func (u *natUI) actionSetting(id string) {
	// Toggles first: one line each, and they all behave the same way.
	toggle := func(fn func(*clientSettings)) {
		u.mutateSettings(fn)
	}
	switch id {
	case "set:sysproxy":
		toggle(func(s *clientSettings) { s.SystemProxy = !s.SystemProxy })
		return
	case "set:autostart":
		next := !u.m.set.AutoStart
		if err := setAutoStart(next); err != nil {
			u.addLog(tr("写入启动项失败")+": "+err.Error(), true)
			return
		}
		u.mutateSettings(func(s *clientSettings) { s.AutoStart = next })
		return
	case "set:autoconnect":
		toggle(func(s *clientSettings) { s.AutoConnect = !s.AutoConnect })
		return
	case "set:autorepair":
		toggle(func(s *clientSettings) { s.AutoRepair = !s.AutoRepair })
		return
	case "set:rememberpage":
		toggle(func(s *clientSettings) { s.RememberPage = !s.RememberPage })
		return
	case "set:dnsdi":
		toggle(func(s *clientSettings) { s.DNSOff = !s.DNSOff })
		return
	case "set:geo":
		toggle(func(s *clientSettings) { s.GeoSplit = !s.GeoSplit })
		return
	case "set:logauto":
		toggle(func(s *clientSettings) { s.LogAutoScroll = !s.LogAutoScroll })
		// Freezing keeps the current lines on screen; unfreezing drops the
		// snapshot so the view follows the log again.
		if u.m.set.LogAutoScroll {
			u.m.frozenLogs = nil
		} else {
			u.m.frozenLogs = append([]natLog(nil), u.m.logs...)
		}
		return
	case "set:reset":
		if u.confirm(tr("恢复默认设置？"), tr("所有设置将回到初始值，需要重新连接才生效。")) {
			u.m.set = defaultSettings()
			if err := saveSettings(u.m.set); err != nil {
				u.addLog(tr("设置保存失败: ")+err.Error(), true)
			}
			u.applyTheme()
			u.applyLang()
			u.syncSessionFromSettings()
		}
		return
	case "set:connect":
		if u.m.connected {
			u.doSwitch()
		} else {
			u.doConnect()
		}
		return
	}

	if v, ok := strings.CutPrefix(id, "set:ipfam:"); ok {
		toggle(func(s *clientSettings) { s.IPv6 = v == "v6" })
		return
	}
	if v, ok := strings.CutPrefix(id, "set:split:"); ok {
		toggle(func(s *clientSettings) { s.SplitMode = v })
		return
	}
	if v, ok := strings.CutPrefix(id, "set:dns:"); ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n < len(dnsPresets) {
			val := dnsPresets[n].value
			toggle(func(s *clientSettings) { s.DNSCustom = val })
		}
		return
	}

	// Text fields: the canvas has no edit control, so every one of them opens
	// the same modal input box.
	type field struct{ hint, value string; set func(*clientSettings, string) }
	fields := map[string]field{
		"set:mixedport": {tr("1-65535"), itoa(int64(u.m.set.MixedPort)), func(s *clientSettings, v string) {
			if n, err := strconv.Atoi(v); err == nil {
				s.MixedPort = n
			}
		}},
		"set:apiport": {tr("1-65535"), itoa(int64(u.m.set.APIPort)), func(s *clientSettings, v string) {
			if n, err := strconv.Atoi(v); err == nil {
				s.APIPort = n
			}
		}},
		"set:mtu": {tr("0 = 自动"), itoa(int64(u.m.set.MTU)), func(s *clientSettings, v string) {
			if n, err := strconv.Atoi(v); err == nil {
				s.MTU = n
			}
		}},
		"set:timeout": {tr("1-60 秒"), itoa(int64(u.m.set.TimeoutSec)), func(s *clientSettings, v string) {
			if n, err := strconv.Atoi(v); err == nil {
				s.TimeoutSec = n
			}
		}},
		"set:dns": {tr("逗号分隔，如 223.5.5.5, 1.1.1.1"), u.m.set.DNSCustom, func(s *clientSettings, v string) {
			s.DNSCustom = strings.TrimSpace(v)
		}},
		"set:splitapps": {tr("进程名，如 chrome.exe, steam.exe"), u.m.set.SplitApps, func(s *clientSettings, v string) {
			s.SplitApps = strings.TrimSpace(v)
		}},
		"set:domdirect": {tr("域名后缀，如 example.com"), u.m.set.DomainDirect, func(s *clientSettings, v string) {
			s.DomainDirect = strings.TrimSpace(v)
		}},
		"set:domproxy": {tr("域名后缀，如 example.com"), u.m.set.DomainProxy, func(s *clientSettings, v string) {
			s.DomainProxy = strings.TrimSpace(v)
		}},
		"set:picknode": {tr("端点，如 188.114.97.198:4500（留空 = 自动）"), u.m.pin, func(s *clientSettings, v string) {
			u.m.pin = strings.TrimSpace(v)
		}},
	}
	f, ok := fields[id]
	if !ok {
		return
	}
	v, ok := u.promptText(tr(fieldTitle(id)), f.hint, f.value)
	if !ok {
		return
	}
	fn := f.set
	u.mutateSettings(func(s *clientSettings) { fn(s, v) })
	if id == "set:picknode" {
		u.m.pin = strings.TrimSpace(v)
	}
}

func fieldTitle(id string) string {
	switch id {
	case "set:mixedport":
		return "混合端口"
	case "set:apiport":
		return "API 端口"
	case "set:mtu":
		return "MTU"
	case "set:timeout":
		return "连接超时（秒）"
	case "set:dns":
		return "自定义 DNS"
	case "set:splitapps":
		return "应用进程"
	case "set:domdirect":
		return "直连域名"
	case "set:domproxy":
		return "代理域名"
	default:
		return "默认节点"
	}
}

// syncSessionFromSettings pulls the session-shaped fields back out of the
// preference file (after a reset, or after loading a different language).
func (u *natUI) syncSessionFromSettings() {
	s := u.m.set
	u.m.proto = s.Proto
	u.m.tun = s.Tun
	u.m.rescan = s.Rescan
	u.m.psiphon = s.Psiphon
	u.m.outerProto = s.OuterProto
	u.m.innerProto = s.InnerProto
	u.invalidate()
}

// ---------------------------------------------------------------------------
// About / update
// ---------------------------------------------------------------------------

func (u *natUI) actionAbout(id string) {
	switch id {
	case "about:copydiag":
		if err := walk.Clipboard().SetText(u.diagText()); err != nil {
			u.addLog(tr("复制失败")+": "+err.Error(), true)
		} else {
			u.addLog(tr("诊断信息已复制到剪贴板"), false)
		}
	case "about:notices":
		if err := opensNotice(); err != nil {
			u.addLog(err.Error(), true)
		}
	case "about:opendata":
		u.openDataDir()
	case "about:project":
		openURL(projectURL)
	}
}

func (u *natUI) actionUpdate(id string) {
	switch id {
	case "upd:check":
		if u.m.updBusy {
			return
		}
		u.m.updBusy = true
		u.m.updNote = tr("正在检查…")
		u.invalidate()
		go func() {
			// fetchLatestVersion, not latestVersion: a manual check must not be
			// answered from the 6h cache, or the button would lie.
			latest := fetchLatestVersion(u.ctx)
			u.sync(func() {
				u.m.updBusy = false
				u.m.updChecked = true
				u.m.updTime = time.Now()
				if latest == "" {
					u.m.updLatest = ""
					u.m.updNote = tr("检查失败：发布页不可达（网络受限或 GitHub 被拦截）")
				} else {
					u.m.updLatest = latest
					if newerVersion(latest, resolveVersion()) {
						u.m.updNote = tr("发现新版本，可到发布页下载")
					} else {
						u.m.updNote = tr("已是最新版本")
					}
				}
				u.invalidate()
			})
		}()
	case "upd:core":
		u.doCore()
	case "upd:refresh":
		resetCoreCapabilityCache()
		u.addLog(tr("重新探测内核…"), false)
		go u.probeCore()
	}
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
