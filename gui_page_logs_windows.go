//go:build windows

package main

import (
	"strings"

	"github.com/lxn/walk"
)

var logTabIDs = []string{"run", "conn", "debug"}
var logTabLabels = []string{"运行日志", "连接记录", "调试信息"}

func (u *natUI) paintLogs(c *walk.Canvas) {
	x, y, w := shellContentX, shellBodyY, 1080-shellPadX-shellContentX
	u.subtabs(c, x, y, "logtab:", logTabIDs, logTabLabels, u.m.logTab)
	top := y + 46
	switch u.m.logTab {
	case "conn":
		u.paintSessions(c, x, top, w)
	case "debug":
		u.paintDebug(c, x, top, w)
	default:
		u.paintRunLog(c, x, top, w)
	}
}

// paintRunLog is the console: newest at the bottom, one line per entry, level
// tagged. Turning auto-scroll off freezes the view instead of silently dropping
// lines, so what is on screen is always a real snapshot.
func (u *natUI) paintRunLog(c *walk.Canvas, x, y, w float64) {
	h := 700 - 14 - y
	const footerH = 52.0
	u.cardPlain(c, x, y, w, h)
	u.fillRound(c, th.logBg, u.r(x+1, y+1, w-2, h-footerH), 11)

	lines := u.m.logs
	if !u.m.set.LogAutoScroll && len(u.m.frozenLogs) > 0 {
		lines = u.m.frozenLogs
	}

	const lineH = 21.0
	rows := int((h - footerH - 20) / lineH)
	if rows < 1 {
		rows = 1
	}
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	if len(lines) == 0 {
		u.textC(c, tr("暂无日志"), u.f.f9, th.faint, u.r(x+16, y+h/2-20, w-32, 20))
	} else {
		ly := y + 10
		for _, l := range lines {
			tag, col := "INFO", th.logText
			switch {
			case l.lv == 2:
				tag, col = "ERR", rx(255, 122, 122)
			case l.lv == 1:
				tag, col = "WARN", rx(240, 185, 90)
			}
			if l.core {
				col = rx(150, 165, 190)
			}
			u.txt(c, x+14, ly, 58, lineH, l.at.Format("15:04:05"), u.f.fmono, rx(122, 136, 158))
			u.txt(c, x+76, ly, 44, lineH, tag, u.f.fmono, col)
			u.txt(c, x+124, ly, w-150, lineH, l.text, u.f.fmono, col)
			ly += lineH
		}
	}

	// Footer: actions on the left, the scroll switch on the right.
	fy := y + h - footerH
	u.hline(c, x+16, fy, w-32, th.line)
	u.button(c, x+16, fy+10, 96, 32, "logclear", tr("清空日志"), false, len(u.m.logs) > 0)
	u.button(c, x+120, fy+10, 96, 32, "logcopy", tr("复制"), false, len(u.m.logs) > 0)
	u.txt(c, x+w-16-160, fy+15, 84, 22, tr("自动滚动"), u.f.f9, th.dim)
	u.switchAt(c, "set:logauto", x+w-16-44, fy+16, u.m.set.LogAutoScroll, true)
	u.addSpot("set:logauto", u.r(x+w-64, fy+10, 48, 32), true)
}

// paintSessions shows only the connection pipeline's own lines: the connect
// attempts, the endpoint chosen, and the disconnect. Core chatter is filtered
// out, so this is a history of sessions rather than a dump.
func (u *natUI) paintSessions(c *walk.Canvas, x, y, w float64) {
	h := 700 - 14 - y
	u.cardPlain(c, x, y, w, h)
	var lines []natLog
	for _, l := range u.m.logs {
		if l.stage {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		u.textC(c, tr("暂无连接记录"), u.f.f10, th.faint, u.r(x+16, y+h/2-24, w-32, 22))
		u.textC(c, tr("连接事件会显示在这里"), u.f.f8, th.faint, u.r(x+16, y+h/2, w-32, 20))
		return
	}
	const lineH = 30.0
	rows := int((h - 40) / lineH)
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	ly := y + 18
	for _, l := range lines {
		col := th.text
		icon := "bolt"
		if l.lv == 2 {
			col, icon = th.bad, "close"
		} else if l.lv == 1 {
			col = th.warn
		}
		u.icon(c, icon, x+20, ly+6, 14, col)
		u.txt(c, x+44, ly, 70, 22, l.at.Format("15:04:05"), u.f.f9, th.dim)
		u.txt(c, x+118, ly, w-140, 22, l.text, u.f.f9, col)
		ly += lineH
		u.hline(c, x+16, ly-4, w-32, th.line)
	}
}

// paintDebug is the copy-and-send block: paths, versions, the state line and
// the tail of the log, in the exact form the diagnostics button copies.
func (u *natUI) paintDebug(c *walk.Canvas, x, y, w float64) {
	h := 700 - 14 - y
	const footerH = 52.0
	u.cardPlain(c, x, y, w, h)
	u.fillRound(c, th.logBg, u.r(x+1, y+1, w-2, h-footerH), 11)

	text := strings.TrimSpace(u.diagText())
	const lineH = 19.0
	rows := int((h - footerH - 20) / lineH)
	lines := strings.Split(text, "\r\n")
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	ly := y + 10
	for _, l := range lines {
		u.txt(c, x+14, ly, w-30, lineH, l, u.f.fmono, th.logText)
		ly += lineH
	}

	fy := y + h - footerH
	u.hline(c, x+16, fy, w-32, th.line)
	u.button(c, x+16, fy+10, 120, 32, "about:copydiag", tr("复制诊断信息"), true, true)
	u.button(c, x+144, fy+10, 100, 32, "about:opendata", tr("打开数据目录"), false, true)
	u.button(c, x+252, fy+10, 120, 32, "upd:refresh", tr("重新探测内核"), false, !u.m.coreBusy)
}
