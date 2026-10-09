//go:build windows

package main

import (
	"github.com/lxn/walk"
)

// The shell: a fixed sidebar on the left, a page header on the right, and the
// active page below it. Every page paints into the same content box, so the
// navigation, the theme pills and the language pill never move.
const (
	shellSidebarW = 178.0
	shellPadX     = 22.0
	shellHeadH    = 74.0

	shellContentX = shellSidebarW + shellPadX // 200
	shellContentW = 1080 - shellContentX - shellPadX
	shellBodyY    = shellHeadH + 10
	shellBodyH    = 700 - shellBodyY - 14
)

type navItem struct {
	page, icon, label string
}

var navItems = []navItem{
	{pageHome, "home", "首页"},
	{pageNodes, "nodes", "节点"},
	{pageRules, "rules", "规则"},
	{pageSettings, "settings", "设置"},
	{pageLogs, "logs", "日志"},
}

func (u *natUI) paintShell(c *walk.Canvas) {
	u.paintSidebar(c)
	u.paintPageHead(c)
	switch u.m.page {
	case pageNodes:
		u.paintNodes(c)
	case pageRules:
		u.paintRules(c)
	case pageSettings:
		u.paintSettings(c)
	case pageLogs:
		u.paintLogs(c)
	default:
		u.paintHome(c)
	}
}

func (u *natUI) paintSidebar(c *walk.Canvas) {
	u.fillRect(c, th.sidebar, u.r(0, 0, shellSidebarW, 700))
	u.fillRect(c, th.line, u.r(shellSidebarW-1, 0, 1, 700))

	// Brand: a rounded accent tile with the initials, next to a two-line name.
	u.fillRound(c, th.accent, u.r(16, 18, 34, 34), 10)
	u.textC(c, "XH", u.f.f11b, th.accentText, u.r(16, 18, 34, 34))
	u.txt(c, 58, 17, 110, 20, "xiaohe", u.f.f11b, th.text)
	u.txt(c, 58, 35, 110, 16, "VPN Client", u.f.f8, th.dim)

	for i, n := range navItems {
		y := 78.0 + float64(i)*44
		r := u.r(12, y, shellSidebarW-24, 40)
		active := u.m.page == n.page
		if active {
			u.fillRound(c, th.accentSoft, r, 9)
			u.fillRound(c, th.accent, u.r(12, y+9, 3, 22), 1.5)
		} else if u.m.hover == "nav:"+n.page {
			u.fillRound(c, th.cardHover, r, 9)
		}
		col := th.dim
		if active {
			col = th.accent
		}
		u.icon(c, n.icon, 28, y+12, 16, col)
		u.txt(c, 54, y+9, 110, 22, tr(n.label), u.f.f10b, col)
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
	u.statusDot(c, 26, 656, 4, col)
	u.txt(c, 38, 646, 130, 20, st, u.f.f9, th.dim)
	u.txt(c, 16, 668, shellSidebarW-32, 18, shortVersion(), u.f.f8, th.faint)
}

func (u *natUI) paintPageHead(c *walk.Canvas) {
	u.txt(c, shellContentX, 18, shellContentW-260, 26, tr(u.pageTitle()), u.f.f15b, th.text)
	u.txt(c, shellContentX, 46, shellContentW-260, 18, tr(u.pageSubtitle()), u.f.f8, th.dim)

	// Quick appearance controls, mirroring the settings page: the two things a
	// user changes most often should not require a trip to a settings tab.
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
	lx := 1080 - shellPadX - lw
	tx := lx - 8 - 88
	u.headPill(c, tx, 24, 88, themeIcon, themeLabel, "theme:cycle")
	u.headPill(c, lx, 24, lw, "globe", langLabel, "lang:cycle")
}

// headPill is the bordered pill with an icon and a label used in the header.
func (u *natUI) headPill(c *walk.Canvas, x, y, w float64, icon, label, id string) {
	r := u.r(x, y, w, 28)
	u.fillRound(c, th.card, r, 14)
	u.strokeRound(c, th.line, r, 14)
	u.icon(c, icon, x+10, y+7, 14, th.dim)
	u.txt(c, x+29, y+5, w-36, 18, label, u.f.f9, th.text)
	u.addSpot(id, r, true)
}

func (u *natUI) pageTitle() string {
	switch u.m.page {
	case pageNodes:
		return "节点"
	case pageRules:
		return "规则"
	case pageSettings:
		return "设置"
	case pageLogs:
		return "日志"
	default:
		return "首页"
	}
}

func (u *natUI) pageSubtitle() string {
	switch u.m.page {
	case pageNodes:
		return "节点列表 · 选择节点 · 延迟测试"
	case pageRules:
		return "全局代理 · 分流规则 · 路由设置"
	case pageSettings:
		return "常规设置 · 启动设置 · 代理设置 · DNS 设置 · 关于"
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
