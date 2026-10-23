//go:build windows

package main

import (
	"strings"

	"github.com/lxn/walk"
)

var logTabIDs = []string{"run", "conn", "debug"}

func (u *natUI) paintLogs(c *walk.Canvas) {
	x, y, w := shellContentX, shellBodyY, u.contentW()
	labels := []string{tr("运行日志"), tr("连接记录"), tr("调试信息")}
	u.subtabs(c, x, y, "logtab:", logTabIDs, labels, u.m.logTab)
	u.paintLogActions(c, x, y, w)

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

// paintLogActions puts the tab's actions on the tab line itself, flush with the
// card's right padding. The card below is then free for content on every tab.
// Widths follow the label because the English strings are roughly twice as wide
// as the Chinese ones and would otherwise overflow.
func (u *natUI) paintLogActions(c *walk.Canvas, x, y, w float64) {
	const btnH = 30.0
	btnW := func(label string) float64 {
		bw := chipWidth(label) + 8
		if bw < 80 {
			bw = 80
		}
		return bw
	}
	switch u.m.logTab {
	case "conn":
		return
	case "debug":
		specs := []struct {
			id, label string
			primary   bool
		}{
			{"about:copydiag", tr("复制诊断信息"), true},
			{"about:opendata", tr("打开数据目录"), false},
			{"upd:refresh", tr("重新探测内核"), false},
		}
		widths := make([]float64, len(specs))
		total := 0.0
		for i, s := range specs {
			widths[i] = btnW(s.label)
			total += widths[i]
		}
		bx := x + w - 16 - total - 8*float64(len(specs)-1)
		for i, s := range specs {
			enabled := true
			if s.id == "upd:refresh" {
				enabled = !u.m.coreBusy
			}
			u.button(c, bx, y, widths[i], btnH, s.id, s.label, s.primary, enabled)
			bx += widths[i] + 8
		}
	default:
		copyW := btnW(tr("复制"))
		clearW := btnW(tr("清空日志"))
		bx := x + w - 16 - copyW
		u.button(c, bx, y, copyW, btnH, "logcopy", tr("复制"), false, len(u.m.logs) > 0)
		bx -= 8 + clearW
		u.button(c, bx, y, clearW, btnH, "logclear", tr("清空日志"), false, len(u.m.logs) > 0)
	}
}

// paintRunLog is a light log reader: newest at the bottom, one line per entry,
// level tagged. It shares the session list's card surface and text colours so
// the two views read as one material, and it always follows the newest line.
func (u *natUI) paintRunLog(c *walk.Canvas, x, y, w float64) {
	h := 670 - y
	u.cardPlain(c, x, y, w, h)

	const lineH = 24.0
	rows := int((h - 28) / lineH)
	if rows < 1 {
		rows = 1
	}
	lines := u.m.logs
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	if len(lines) == 0 {
		u.textC(c, tr("暂无日志"), u.f.f9, th.faint, u.r(x+16, y+h/2-20, w-32, 20))
		return
	}
	ly := y + 14
	for _, l := range lines {
		// Core chatter stays identifiable by its tag instead of being faded out:
		// it is the bulk of the log and has to remain readable.
		tag, tagCol := "INFO", th.dim
		msgCol := th.text
		switch {
		case l.lv == 2:
			tag, tagCol, msgCol = "ERR", th.bad, th.bad
		case l.lv == 1:
			tag, tagCol, msgCol = "WARN", th.warn, th.warn
		case l.core:
			tag = "CORE"
		}
		u.txt(c, x+16, ly, 64, lineH, l.at.Format("15:04:05"), u.f.fmono, th.dim)
		u.txt(c, x+86, ly, 52, lineH, tag, u.f.fmono, tagCol)
		u.txt(c, x+144, ly, w-160, lineH, l.text, u.f.fmono, msgCol)
		ly += lineH
	}
}

// paintSessions shows only the connection pipeline's own lines: the connect
// attempts, the endpoint chosen, and the disconnect. Core chatter is filtered
// out, so this is a history of sessions rather than a dump.
func (u *natUI) paintSessions(c *walk.Canvas, x, y, w float64) {
	h := 670 - y
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

// paintDebug previews the support snapshot: the environment block first, then
// the newest log lines. The full log lives on its own tab, so repeating all of
// it here only made this tab a dimmer copy of that one.
func (u *natUI) paintDebug(c *walk.Canvas, x, y, w float64) {
	h := 670 - y
	u.cardPlain(c, x, y, w, h)

	const (
		lineH = 24.0
		pad   = 16.0
	)
	lines := strings.Split(strings.TrimSpace(u.diagHeader()), "\r\n")
	rows := int((h - pad*2) / lineH)
	if len(lines) > rows {
		lines = lines[:rows]
	}
	if room := rows - len(lines) - 1; room > 0 {
		logs := u.m.logs
		if len(logs) > room {
			logs = logs[len(logs)-room:]
		}
		if len(logs) > 0 {
			lines = append(lines, "")
			for _, l := range logs {
				lines = append(lines, "log "+l.at.Format("15:04:05")+" "+l.text)
			}
		}
	}
	ly := y + pad
	for _, l := range lines {
		u.txt(c, x+16, ly, w-32, lineH, l, u.f.fmono, th.text)
		ly += lineH
	}
}
