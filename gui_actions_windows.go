//go:build windows

package main

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/xhwarp/warp-client/internal/core"
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

	case strings.HasPrefix(id, "pickpreset:"):
		v := strings.TrimPrefix(id, "pickpreset:")
		m.dropOpen = ""
		switch v {
		case modeBypass, modeBlacklist, modeGlobal, modeDirect, modeCustom:
		default:
			u.invalidate()
			return
		}
		// Bypass mainland is a ruleset over the geo databases; without them the
		// core refuses the whole config. Rather than leaving the row inert, the
		// click fetches the databases and applies the preset when they land.
		if v == modeBypass && !geoDataReady() {
			u.startGeoDownload()
			return
		}
		u.mutateSettings(func(s *clientSettings) { s.ProxyMode = v })

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

// startGeoDownload fetches the geo databases the bypass preset needs and then
// applies it. Called on the GUI thread; the transfer runs off-thread and
// reports through the log panel so a slow download is visible rather than
// looking like a dead click.
func (u *natUI) startGeoDownload() {
	if u.m.geoBusy {
		return
	}
	dir, err := coreDir()
	if err != nil {
		u.addLog(tr("无法定位核心目录: ")+err.Error(), true)
		return
	}
	u.m.geoBusy = true
	u.addLog(tr("缺少 Geo 数据，开始下载（约 13 MB）…"), false)
	u.invalidate()
	go func() {
		derr := core.DownloadGeoData(u.ctx, dir, func(line string) {
			u.sync(func() { u.addLog(line, false) })
		})
		u.sync(func() {
			u.m.geoBusy = false
			if derr != nil {
				u.addLog(tr("Geo 数据下载失败: ")+derr.Error(), true)
				u.invalidate()
				return
			}
			u.addLog(tr("Geo 数据就绪，已启用「绕过大陆」"), false)
			u.mutateSettings(func(s *clientSettings) { s.ProxyMode = modeBypass })
		})
	}()
}

// gotoPage switches pages. The start page is always Home, so nothing about the
// current page is persisted.
func (u *natUI) gotoPage(page string) {
	switch page {
	case pageHome, pageNodes, pageRules, pageSettings, pageAbout, pageLogs:
	default:
		return
	}
	u.m.page = page
	u.m.scroll = 0
	if page != pageSettings {
		u.m.setScroll = 0
	}
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
	case "set:allowlan":
		toggle(func(s *clientSettings) { s.AllowLan = !s.AllowLan })
		return
	case "set:rescan":
		toggle(func(s *clientSettings) { s.Rescan = !s.Rescan })
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
	case "set:dnsdi":
		toggle(func(s *clientSettings) { s.DNSOff = !s.DNSOff })
		return
	case "set:dnsleak":
		// The switch is disabled in the paint when the configuration cannot
		// honour it; the guard keeps a stale click from lying about the result.
		if u.m.set.DNSOff || u.m.set.ProxyMode == modeDirect {
			return
		}
		toggle(func(s *clientSettings) { s.DNSLeak = !s.DNSLeak })
		return
	case "set:reset":
		if u.confirm(tr("恢复默认设置？"), tr("所有设置将回到初始值，需要重新连接才生效。")) {
			// Autostart lives in the registry, not only in the file: resetting
			// the file alone would leave the program starting with Windows while
			// the switch says it does not.
			if u.m.set.AutoStart {
				_ = setAutoStart(false)
			}
			u.m.set = defaultSettings()
			if err := saveSettings(u.m.set); err != nil {
				u.addLog(tr("设置保存失败: ")+err.Error(), true)
			}
			u.m.pin = ""
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

	if v, ok := strings.CutPrefix(id, "set:capture:"); ok {
		if v == captureTun && !core.IsElevated() {
			// Stored anyway: the user may be configuring now and relaunching as
			// administrator later. The card says why it will not connect yet.
			u.addLog(tr("TUN 模式需要管理员权限：请以管理员身份重新运行本程序"), true)
		}
		toggle(func(s *clientSettings) { s.CaptureMode = v })
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
	// the same modal input box. Each setter validates its own input and returns
	// an error instead of silently keeping the old value.
	type field struct {
		hint  string
		value string
		set   func(*clientSettings, string) error
	}
	port := func(n int, other int, label string) error {
		if n < 1 || n > 65535 {
			return fmt.Errorf("%s", trf("%s 必须在 1-65535 之间", label))
		}
		if n == other {
			return fmt.Errorf("%s", trf("%s 不能与另一个端口相同", label))
		}
		return nil
	}
	fields := map[string]field{
		"set:mixedport": {tr("请输入 1-65535 之间的端口号"), itoa(int64(u.m.set.MixedPort)), func(s *clientSettings, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("%s", tr("端口必须是数字"))
			}
			if err := port(n, s.APIPort, tr("本地代理端口")); err != nil {
				return err
			}
			s.MixedPort = n
			return nil
		}},
		"set:apiport": {tr("请输入 1-65535 之间的端口号"), itoa(int64(u.m.set.APIPort)), func(s *clientSettings, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("%s", tr("端口必须是数字"))
			}
			if err := port(n, s.MixedPort, tr("API 端口")); err != nil {
				return err
			}
			s.APIPort = n
			return nil
		}},
		"set:mtu": {tr("0 = 自动"), itoa(int64(u.m.set.MTU)), func(s *clientSettings, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 || n > 9000 {
				return fmt.Errorf("%s", tr("MTU 必须在 0-9000 之间（0 = 自动）"))
			}
			s.MTU = n
			return nil
		}},
		"set:timeout": {tr("1-60 秒"), itoa(int64(u.m.set.TimeoutSec)), func(s *clientSettings, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 60 {
				return fmt.Errorf("%s", tr("连接超时必须在 1-60 秒之间"))
			}
			s.TimeoutSec = n
			return nil
		}},
		"set:dns": {tr("逗号分隔，如 223.5.5.5, 1.1.1.1"), u.m.set.DNSCustom, func(s *clientSettings, v string) error {
			v = strings.TrimSpace(v)
			for _, item := range fieldList(v) {
				if _, err := netip.ParseAddr(item); err != nil {
					return fmt.Errorf("%s", trf("无效的 DNS 地址: %s", item))
				}
			}
			s.DNSCustom = v
			return nil
		}},
		"set:domdirect": {tr("逗号分隔的域名后缀，如 example.com；留空不启用"), u.m.set.DomainDirect, func(s *clientSettings, v string) error {
			s.DomainDirect = strings.TrimSpace(v)
			return nil
		}},
		"set:domproxy": {tr("逗号分隔的域名后缀，如 example.com；留空不启用"), u.m.set.DomainProxy, func(s *clientSettings, v string) error {
			s.DomainProxy = strings.TrimSpace(v)
			return nil
		}},
		"set:picknode": {tr("端点，如 188.114.97.198:4500（留空 = 自动）"), u.m.pin, func(s *clientSettings, v string) error {
			v = strings.TrimSpace(v)
			if v != "" && !validPinEndpoint(v) {
				return fmt.Errorf("%s", tr("节点格式应为 IP 或 IP:端口"))
			}
			s.Pin = v
			return nil
		}},
	}
	f, ok := fields[id]
	if !ok {
		return
	}
	v, ok := u.promptTextCheck(tr(fieldTitle(id)), f.hint, f.value, func(raw string) error {
		// Validate against a copy: a rejected value must leave the live
		// settings untouched while the dialog stays open.
		tmp := u.m.set
		return f.set(&tmp, strings.TrimSpace(raw))
	})
	if !ok {
		return
	}
	if err := f.set(&u.m.set, strings.TrimSpace(v)); err != nil {
		u.addLog(err.Error(), true)
		return
	}
	if err := saveSettings(u.m.set); err != nil {
		u.addLog(tr("设置保存失败: ")+err.Error(), true)
	} else if u.m.connected {
		u.m.setDirty = true
	}
	// Say what was saved: the field sits in a card that may be scrolled out of
	// view, and "nothing happened" is the worst answer a settings dialog can give.
	u.addLog(tr("已保存：")+fieldTitle(id)+" = "+strings.TrimSpace(v), false)
	if id == "set:picknode" {
		// normalize may have cleared a pin that does not match the family.
		if trimmed := strings.TrimSpace(v); trimmed != "" && u.m.set.Pin == "" {
			u.addLog(tr("该节点与当前地址族不匹配，已清除"), true)
		}
		u.m.pin = u.m.set.Pin
	}
	u.invalidate()
}

// validPinEndpoint accepts a bare IP or an IP:port endpoint.
func validPinEndpoint(v string) bool {
	if _, err := netip.ParseAddr(strings.Trim(v, "[]")); err == nil {
		return true
	}
	host, port, err := net.SplitHostPort(v)
	if err != nil {
		return false
	}
	if _, err := netip.ParseAddr(strings.Trim(host, "[]")); err != nil {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}

func fieldTitle(id string) string {
	switch id {
	case "set:mixedport":
		return "本地代理端口"
	case "set:apiport":
		return "API 端口"
	case "set:mtu":
		return "MTU"
	case "set:timeout":
		return "连接超时（秒）"
	case "set:dns":
		return "自定义 DNS"
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
	u.m.pin = s.Pin
	u.m.psiphon = s.Psiphon
	u.m.openvpn = s.OpenVPN
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
		if err := setClipboardText(u.diagText()); err != nil {
			u.addLog(tr("复制失败")+": "+err.Error(), true)
		} else {
			u.addLog(tr("诊断信息已复制到剪贴板"), false)
		}
	case "about:copycode":
		// The code is what the maintainer needs to stop (or un-stop) this
		// install, so copying it beats asking the user to retype 16 characters.
		if err := setClipboardText(u.m.set.ControlCode); err != nil {
			u.addLog(tr("复制失败")+": "+err.Error(), true)
		} else {
			u.addLog(tr("控制码已复制到剪贴板"), false)
		}
	case "about:checkcontrol":
		u.startControlCheck(true)
	case "about:opendata":
		// Still used by the Logs page's diagnostics tab.
		u.openDataDir()
	case "about:project":
		openURL(repoURL)
	case "about:releases":
		openURL(repoReleasesURL)
	case "about:issues":
		openURL(repoIssuesURL)
	case "about:notices":
		openURL(repoNoticesURL)
	case "about:license":
		openURL(repoLicenseURL)
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
			latest, answered := fetchLatestVersion(u.ctx)
			u.sync(func() {
				u.m.updBusy = false
				u.m.updChecked = true
				u.m.updTime = time.Now()
				switch {
				case !answered:
					u.m.updLatest = ""
					u.m.updNote = tr("检查失败：发布页不可达（网络受限或 GitHub 被拦截）")
				case latest == "":
					// The repository answered and has no releases yet: saying
					// "unreachable" here would blame the network for a version
					// that simply does not exist.
					u.m.updLatest = ""
					u.m.updNote = tr("发布页还没有 Release，可到项目主页查看进度")
				default:
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
