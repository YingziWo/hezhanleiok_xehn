//go:build windows

package main

import (
	"github.com/lxn/walk"

	"github.com/xhwarp/warp-client/internal/core"
)

var setTabIDs = []string{"general", "startup", "proxy", "dns", "about"}
var setTabLabels = []string{"常规", "启动", "代理", "DNS", "关于"}

func (u *natUI) paintSettings(c *walk.Canvas) {
	x, y, w := shellContentX, shellBodyY, 1080-shellPadX-shellContentX
	u.subtabs(c, x, y, "settab:", setTabIDs, setTabLabels, u.m.setTab)
	top := y + 46
	switch u.m.setTab {
	case "startup":
		u.paintSetStartup(c, x, top, w)
	case "proxy":
		u.paintSetProxy(c, x, top, w)
	case "dns":
		u.paintSetDNS(c, x, top, w)
	case "about":
		u.paintAbout(c, x, top, w)
	default:
		u.paintSetGeneral(c, x, top, w)
	}
}

// ---------------------------------------------------------------------------
// General: language and theme
// ---------------------------------------------------------------------------

func (u *natUI) paintSetGeneral(c *walk.Canvas, x, y, w float64) {
	cy := u.card(c, x, y, w, 268, tr("常规"))
	u.rowChips(c, x, cy, w, "lang:", tr("语言"), []string{langZH, langEN},
		[]string{"中文", "English"}, u.m.set.Lang)
	u.hline(c, x+16, cy+40, w-32, th.line)

	u.txt(c, x+16, cy+54, w-32, 18, tr("主题"), u.f.f10, th.text)
	// Three live previews. They are painted with the palettes themselves, so
	// what the tiles show is literally what selecting them produces.
	tileW, tileH := 168.0, 84.0
	opts := []struct{ id, label, icon string }{
		{themeLight, "浅色", "sun"},
		{themeDark, "深色", "moon"},
		{themeAuto, "跟随系统", "monitor"},
	}
	for i, o := range opts {
		tx := x + 16 + float64(i)*(tileW+12)
		u.themeTile(c, tx, cy+78, tileW, tileH, o.id, tr(o.label), o.icon)
	}
	u.hline(c, x+16, cy+178, w-32, th.line)
	u.rowToggle(c, x, cy+188, w, "set:rememberpage", tr("记住上次页面"),
		tr("关闭后每次都从首页开始"), u.m.set.RememberPage, true)
}

// themeTile previews a theme: a sidebar rail, a card and an accent dot in that
// theme's colours, with a selection ring around the active one.
func (u *natUI) themeTile(c *walk.Canvas, x, y, w, h float64, id, label, icon string) {
	p := paletteLight
	if id == themeDark {
		p = paletteDark
	} else if id == themeAuto {
		if systemDark() {
			p = paletteDark
		}
	}
	active := u.m.set.Theme == id
	r := u.r(x, y, w, h)
	u.fillRound(c, th.input, r, 10)
	if active {
		u.strokeRound(c, th.accent, u.r(x+0.5, y+0.5, w-1, h-1), 10)
	}
	// Miniature of the window: rail, two cards, accent bar.
	px, py, pw, ph := x+10, y+10, w-20, h-30
	u.fillRound(c, p.window, u.r(px, py, pw, ph), 6)
	u.fillRound(c, p.sidebar, u.r(px, py, pw*0.26, ph), 6)
	u.fillRound(c, p.accent, u.r(px+6, py+7, pw*0.26-12, 5), 2.5)
	for i := 0; i < 3; i++ {
		u.fillRound(c, p.chip, u.r(px+8, py+20+float64(i)*9, pw*0.26-16, 4), 2)
	}
	u.fillRound(c, p.card, u.r(px+pw*0.32, py+7, pw*0.62, ph*0.5), 5)
	u.fillRound(c, p.card, u.r(px+pw*0.32, py+7+ph*0.56, pw*0.62, ph*0.34), 5)
	u.circleFill(c, p.accent, px+pw*0.5, py+ph*0.3, 7)
	u.icon(c, icon, x+14, y+h-24, 14, th.dim)
	u.txt(c, x+32, y+h-26, w-40, 18, label, u.f.f9, th.text)
	u.addSpot("theme:"+id, r, true)
}

// ---------------------------------------------------------------------------
// Startup
// ---------------------------------------------------------------------------

func (u *natUI) paintSetStartup(c *walk.Canvas, x, y, w float64) {
	cy := u.card(c, x, y, w, 316, tr("启动"))
	s := u.m.set
	u.rowToggle(c, x, cy, w, "set:autostart", tr("开机自动启动"),
		tr("写入当前用户的启动项"), s.AutoStart, true)
	u.rowToggle(c, x, cy+40, w, "set:autoconnect", tr("启动后自动连接"),
		tr("程序启动即连接上次的协议"), s.AutoConnect, true)
	u.rowToggle(c, x, cy+80, w, "rescan", tr("连接前重新扫描"),
		tr("连接时重新扫描端点，耗时更长但更新"), s.Rescan, !u.m.connected)
	u.rowToggle(c, x, cy+120, w, "set:autorepair", tr("自动修复（健康守护）"),
		tr("会话断开后由后台守护自动重建"), s.AutoRepair, true)
	u.rowEdit(c, x, cy+164, w, "set:timeout", tr("连接超时（秒）"),
		itoa(int64(s.TimeoutSec)), "2")
	u.hline(c, x+16, cy+210, w-32, th.line)
	u.txt(c, x+16, cy+222, w-32, 18, tr("连接超时用于扫描与握手，过小会漏掉慢端点"), u.f.f8, th.faint)
}

// ---------------------------------------------------------------------------
// Proxy
// ---------------------------------------------------------------------------

func (u *natUI) paintSetProxy(c *walk.Canvas, x, y, w float64) {
	cy := u.card(c, x, y, w, 376, tr("代理"))
	s := u.m.set
	u.rowEdit(c, x, cy, w, "set:mixedport", tr("混合端口"),
		itoa(int64(s.MixedPort)), itoa(int64(core.DefaultMixedPort)))
	u.rowEdit(c, x, cy+38, w, "set:apiport", tr("API 端口"),
		itoa(int64(s.APIPort)), itoa(int64(core.DefaultAPIPort)))
	u.rowToggle(c, x, cy+78, w, "set:sysproxy", tr("设置系统代理"),
		tr("连接时写入系统代理，断开时还原"), s.SystemProxy, true)
	u.rowToggle(c, x, cy+118, w, "tun", tr("TUN 全局模式"),
		tr("需要管理员权限；开启后接管全部流量"), s.Tun, !u.m.connected)
	u.rowChips(c, x, cy+160, w, "set:ipfam:", tr("地址族"), []string{"v4", "v6"},
		[]string{"IPv4", "IPv6"}, currentFamily(s.IPv6))
	u.rowEdit(c, x, cy+196, w, "set:mtu", tr("MTU"), itoa(int64(s.MTU)), tr("0 = 自动"))
	u.hline(c, x+16, cy+244, w-32, th.line)
	u.txt(c, x+16, cy+256, w-32, 18, tr("端口、地址族与 MTU 在下次连接时读取"), u.f.f8, th.faint)
	u.button(c, x+16, cy+286, 150, 34, "set:connect", tr("重新连接以应用"), true, u.m.connected)
}

// ---------------------------------------------------------------------------
// DNS
// ---------------------------------------------------------------------------

func (u *natUI) paintSetDNS(c *walk.Canvas, x, y, w float64) {
	cy := u.card(c, x, y, w, 300, tr("DNS"))
	s := u.m.set
	u.rowEdit(c, x, cy, w, "set:dns", tr("自定义 DNS"),
		s.DNSCustom, tr("逗号分隔，如 223.5.5.5, 1.1.1.1"))
	u.rowChips(c, x, cy+40, w, "set:dns:", tr("常用"),
		dnsPresetIDs(), dnsPresetLabels(), currentDNSPreset(s.DNSCustom))
	u.rowToggle(c, x, cy+80, w, "set:dnsdi", tr("不下发 DNS"),
		tr("不向隧道提供解析器，全部用系统 DNS"), s.DNSOff, true)
	u.hline(c, x+16, cy+126, w-32, th.line)
	for i, line := range []string{
		tr("留空表示不指定，由内核自己决定解析方式。"),
		tr("解析器会写进 mihomo 配置的 dns.nameserver，并下发到 tunnel。"),
		tr("自定义解析器必须能解析域名，否则连接验证会失败。"),
	} {
		u.txt(c, x+16, cy+140+float64(i)*22, w-32, 20, line, u.f.f8, th.faint)
	}
}

// ---------------------------------------------------------------------------
// About + updates
// ---------------------------------------------------------------------------

func (u *natUI) paintAbout(c *walk.Canvas, x, y, w float64) {
	half := (w - 14) / 2
	cy := u.card(c, x, y, half, 240, tr("本程序"))
	dir, _ := appDir()
	cDir, _ := coreDir()
	rows := [][2]string{
		{tr("名称"), appName},
		{tr("版本"), shortVersion()},
		{tr("构建"), guiBuildKind()},
		{tr("数据目录"), orEmpty(dir, tr("未知"))},
		{tr("核心版本"), orEmpty(u.m.coreVersion, tr("未探测"))},
		{tr("核心大小"), coreSizeText(u.m.coreSize)},
	}
	ry := cy
	for _, kv := range rows {
		u.kv(c, x, ry, half, kv[0], kv[1])
		ry += 24
	}
	bw := (half - 16 - 10) / 2
	u.button(c, x+16, y+192, bw, 34, "about:opendata", tr("打开数据目录"), false, true)
	u.button(c, x+16+bw+10, y+192, bw, 34, "about:copydiag", tr("复制诊断信息"), false, true)

	rx := x + half + 14
	cy = u.card(c, rx, y, half, 240, tr("开源与许可"))
	rows = [][2]string{
		{tr("内核"), "mihomo (Clash Meta) " + orEmpty(u.m.coreVersion, "")},
		{tr("内核许可"), "GPL-3.0"},
		{tr("协议引擎"), "WireGuard / AmneziaWG / MASQUE"},
		{tr("声明文件"), "THIRD_PARTY_NOTICES.md"},
		{tr("许可全文"), "third_party/GPL-3.0.txt"},
		{tr("更新源"), projectURL},
	}
	ry = cy
	for _, kv := range rows {
		u.kv(c, rx, ry, half, kv[0], kv[1])
		ry += 24
	}
	u.button(c, rx+16, y+192, bw, 34, "about:notices", tr("打开第三方声明"), false, true)
	u.button(c, rx+16+bw+10, y+192, bw, 34, "about:project", tr("打开发布页"), false, true)

	// Update row.
	y2 := y + 254
	cy = u.card(c, x, y2, half, 170, tr("客户端更新"))
	u.kv(c, x, cy, half, tr("当前版本"), shortVersion())
	u.kv(c, x, cy+24, half, tr("最新版本"), orEmpty(u.m.updLatest, tr("尚未检查")))
	u.kv(c, x, cy+48, half, tr("状态"), orEmpty(u.m.updNote, tr("点击下方按钮检查更新")))
	u.button(c, x+16, y2+126, bw, 34, "upd:check", tr("检查更新"), true, !u.m.updBusy)
	u.button(c, x+16+bw+10, y2+126, bw, 34, "about:project", tr("打开发布页"), false, true)

	cy = u.card(c, rx, y2, half, 170, tr("内核 (mihomo)"))
	u.kv(c, rx, cy, half, tr("状态"), coreSizeText(u.m.coreSize))
	u.kv(c, rx, cy+24, half, tr("版本"), orEmpty(u.m.coreVersion, tr("未探测")))
	u.kv(c, rx, cy+48, half, tr("路径"), core.CoreInDir(cDir))
	u.button(c, rx+16, y2+126, bw, 34, "upd:core", tr("检查/下载内核"), true, !u.m.coreBusy)
	u.button(c, rx+16+bw+10, y2+126, bw, 34, "upd:refresh", tr("重新探测内核"), false, !u.m.coreBusy)

	// Disclaimer: one line of fact, one line of limits.
	y3 := y2 + 184
	u.cardPlain(c, x, y3, w, 700-14-y3)
	u.txt(c, x+16, y3+12, w-32, 20, tr("免责声明"), u.f.f10b, th.text)
	for i, line := range []string{
		tr("本程序只负责在你自己的设备上配置一条加密隧道，不提供、不代理任何服务器资源。"),
		tr("隧道出口由 Cloudflare WARP 提供；内核与协议引擎均为独立的 GPL-3.0 作品，以单独的可执行文件分发。"),
		tr("程序按「现状」提供，不对可用性、速度或适用性作任何承诺。"),
	} {
		u.txt(c, x+16, y3+36+float64(i)*20, w-32, 18, line, u.f.f8, th.dim)
	}
}
