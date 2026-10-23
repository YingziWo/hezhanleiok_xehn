//go:build windows

package main

import (
	"github.com/lxn/walk"
)

// The shell: a fixed sidebar on the left, a page header on the right, and the
// active page below it. Every page paints into the same content box, so the
// navigation, the theme pills and the language pill never move.
const (
	// The mock's rail is ~17.7% of the window (271/1536); this is the same
	// proportion on the 1080 design grid.
	shellSidebarW = 191.0
	shellPadX     = 22.0
	shellHeadH    = 74.0

	shellContentX = shellSidebarW + shellPadX // 200
	shellBodyY    = shellHeadH + 10
	shellBodyH    = 700 - shellBodyY - 14
)

// contentW is the width of the content column in design units. It comes from
// the window, not from the fixed 1080 grid: a window wider than the grid's
// aspect would otherwise leave a blank strip on the right and the cards would
// keep their windowed size no matter how far it is maximised.
func (u *natUI) contentW() float64 { return u.dw - shellContentX - shellPadX }

type navItem struct {
	page, icon, label string
}

var navItems = []navItem{
	{pageHome, "home", "首页"},
	{pageSettings, "settings", "设置"},
	{pageAbout, "info", "关于"},
	{pageLogs, "logs", "日志"},
}

func (u *natUI) paintShell(c *walk.Canvas) {
	u.paintSidebar(c)
	// The home page is the poster view: no title bar of its own, its top row
	// is the exit-region dropdown.
	if u.m.page != pageHome {
		u.paintPageHead(c)
	}
	switch u.m.page {
	case pageSettings:
		u.paintSettings(c)
	case pageAbout:
		u.paintAbout(c, shellContentX, shellBodyY, u.contentW())
	case pageLogs:
		u.paintLogs(c)
	default:
		u.paintHome(c)
	}
	// Open dropdown overlays next, so they sit above the page (and hit-testing
	// scans spots back to front), and the window buttons above everything.
	u.paintDropdownPopups(c)
	u.paintCaption(c, u.dw)
}

// paintWallpaper is the reference's background: not a flat colour, but a pale
// blue wash with several out-of-focus white banks and a small floating ball.
// Everything is painted in device space, edge to edge, so the same scenery
// fills a maximised window.
func (u *natUI) paintWallpaper(c *walk.Canvas, pxW, pxH int) {
	// The wash itself: lighter at the top, deeper blue at the foot.
	top := mix(th.window, rx(255, 255, 255), 0.20)
	bottom := mix(th.window, rx(198, 218, 241), 0.60)
	u.vgradPx(c, pxRect(0, 0, pxW, pxH), top, bottom)

	// Banks. Big, bright and overlapping — the several shapes the reference is
	// built from. They are mixed from the mid wash, so their rims dissolve.
	mid := mix(top, bottom, 0.35)
	u.softEllipse(c, mid, 300, 980, 660, 400, 0.95)
	u.softEllipse(c, mid, 820, 1040, 720, 400, 0.85)
	u.softEllipse(c, mid, -70, 860, 430, 300, 0.80)
	u.softEllipse(c, top, 150, -110, 420, 260, 0.55)
	u.softEllipse(c, top, 1010, -90, 440, 300, 0.60)

	// The small floating sphere on the right, as in the reference.
	u.ball(c, 938, 206, 24)
}

// ball is the out-of-focus sphere: a soft shadow, a body and one highlight, all
// in canvas colours. Concentric gradients are avoided — on this palette their
// rings show as bands.
func (u *natUI) ball(c *walk.Canvas, cx, cy, r float64) {
	u.ellipseFill(c, mix(th.window, rx(150, 175, 205), 0.16), cx, cy+r*0.62, r*1.2, r*0.36)
	u.circleFill(c, mix(rx(255, 255, 255), th.window, 0.24), cx, cy, r)
	u.circleFill(c, mix(rx(255, 255, 255), th.window, 0.04), cx-r*0.16, cy-r*0.20, r*0.62)
}

func (u *natUI) paintSidebar(c *walk.Canvas) {
	h := u.mw.ClientBoundsPixels().Height
	if h < 10 {
		h = 700
	}
	sep := u.sx(shellSidebarW)

	// The rail runs the full height of the window — a sidebar is a pane, not a
	// card — so it is painted in device pixels and keeps its edge when the
	// design grid is letterboxed by a maximised window. It is a translucent
	// veil in the reference: near-white, with the background bank still faintly
	// visible crossing its lower half.
	veilTop := mix(th.sidebar, rx(255, 255, 255), 0.55)
	veilBottom := mix(th.sidebar, th.window, 0.6)
	u.vgradPx(c, pxRect(0, 0, sep, h), veilTop, veilBottom)
	u.softEllipse(c, mix(veilTop, veilBottom, 0.6), -70, 800, 330, 300, 0.55)
	u.fillRect(c, mix(th.sidebar, th.line, 0.55), pxRect(sep-1, 0, 1, h))

	// Brand: the logo above the wordmark, centred on the rail. The old 26px mark
	// inline with the name was too small to read; this gives the logo the rail's
	// width and uses the empty band above the navigation.
	const brandSize = 60.0
	u.drawLogo(c, (shellSidebarW-brandSize)/2, 14, brandSize)
	u.textC(c, "XEHN", u.f.f18b, th.text,
		u.r(12, 14+brandSize+8, shellSidebarW-24, 26))

	for i, n := range navItems {
		y := 118.0 + float64(i)*52
		r := u.r(12, y, shellSidebarW-24, 44)
		active := u.m.page == n.page
		switch {
		case active:
			// The selected row is the brightest surface on the rail: a lighter
			// frosted pill (not a white rectangle), a bright rim and a shadow.
			u.shadowRound(c, r, navRadius, shadowTint(), th.sidebar)
			u.fillRound(c, mix(th.card, rx(255, 255, 255), 0.78), r, navRadius)
			u.strokeRound(c, mix(th.card, rx(255, 255, 255), 0.8), r, navRadius)
		case u.m.hover == "nav:"+n.page:
			u.fillRound(c, mix(th.card, th.accentSoft, 0.45), r, navRadius)
		}
		iconCol, txtCol := th.faint, th.dim
		if active {
			iconCol, txtCol = th.accent, th.text
		}
		u.icon(c, n.icon, 26, y+12, 20, iconCol)
		u.txt(c, 56, y+10, 116, 24, tr(n.label), u.f.f16b, txtCol)
		u.addSpot("nav:"+n.page, r, true)
	}

	// Connection state at the foot of the rail: one glance from any page.
	st, col := tr("未连接"), th.faint
	switch {
	case u.m.connecting:
		st, col = tr("连接中…"), th.warn
	case u.m.connected:
		st, col = tr("已连接"), th.ok
	}
	u.statusDot(c, 24, 650, 4.5, col)
	u.txt(c, 38, 638, 130, 24, st, u.f.f13, th.dim)
	u.txt(c, 24, 672, shellSidebarW-48, 20, shortVersion(), u.f.f8, th.faint)
}

func (u *natUI) paintPageHead(c *walk.Canvas) {
	u.txt(c, shellContentX, 16, u.contentW()-40, 28, tr(u.pageTitle()), u.f.f15b, th.text)
	u.txt(c, shellContentX, 46, u.contentW()-40, 18, tr(u.pageSubtitle()), u.f.f8, th.faint)
	u.hline(c, shellContentX, shellHeadH-2, u.contentW(), mix(th.window, th.line, 0.8))
}

// paintAppearancePills is the theme/language pair in Home's top-right corner.
// They belong to the page without a title bar; every other page keeps its
// header clear.
func (u *natUI) paintAppearancePills(c *walk.Canvas, y float64) {
	themeIcon, themeLabel := "sun", tr("浅色")
	switch {
	case u.m.set.Theme == themeDark:
		themeIcon, themeLabel = "moon", tr("深色")
	case u.m.set.Theme == themeAuto:
		themeIcon, themeLabel = "monitor", tr("跟随系统")
	}
	lw := 68.0
	if u.m.set.Lang == langEN {
		lw = 80
	}
	langLabel := "中文"
	if u.m.set.Lang == langEN {
		langLabel = "English"
	}
	lx := u.dw - shellPadX - lw
	// 100, not 88: the four-character automatic label needs the room, otherwise
	// it is the one pill that renders as an ellipsis.
	const themeW = 100.0
	tx := lx - 8 - themeW
	u.headPill(c, tx, y, themeW, themeIcon, themeLabel, "theme:cycle")
	u.headPill(c, lx, y, lw, "globe", langLabel, "lang:cycle")
}

// headPill is the bordered pill with an icon and a label used in the header.
func (u *natUI) headPill(c *walk.Canvas, x, y, w float64, icon, label, id string) {
	r := u.r(x, y, w, 28)
	bg := mix(th.card, th.accentSoft, 0.4)
	if u.m.hover == id {
		bg = th.cardHover
	}
	u.fillRound(c, bg, r, pillRad(28))
	u.strokeRound(c, mix(th.card, th.accentLine, 0.7), r, pillRad(28))
	u.icon(c, icon, x+10, y+7, 14, th.dim)
	u.txt(c, x+29, y+4, w-34, 20, label, u.f.f9, th.text)
	u.addSpot(id, r, true)
}

func (u *natUI) pageTitle() string {
	switch u.m.page {
	case pageSettings:
		return "设置"
	case pageAbout:
		return "关于"
	case pageLogs:
		return "日志"
	default:
		return "首页"
	}
}

func (u *natUI) pageSubtitle() string {
	switch u.m.page {
	case pageSettings:
		return "连接与接管 · 分流规则 · 启动与行为 · DNS 与出口"
	case pageAbout:
		return "本程序 · 开源许可 · 客户端更新 · 内核"
	case pageLogs:
		return "运行日志 · 连接记录 · 调试信息"
	default:
		return "快速概览 · 连接状态 · 节点信息"
	}
}

// sessionLabel is the human name of the current exit, used by the home card.
func (u *natUI) sessionLabel() string {
	if !u.m.connected {
		return tr("未连接")
	}
	return u.m.status.sessionLabel()
}
