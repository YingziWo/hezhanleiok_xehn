//go:build windows

package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/lxn/walk"
)

// Home: the connect button and the live summary of the session it produces.
func (u *natUI) paintHome(c *walk.Canvas) {
	const (
		cardX = shellContentX
		cardW = 330.0
		rightX = shellContentX + cardW + 14
		rightW = 1080 - shellPadX - rightX
	)
	bodyY := shellBodyY
	bodyH := 700 - bodyY - 14

	u.cardPlain(c, cardX, bodyY, cardW, bodyH)
	u.paintConnectButton(c, cardX+cardW/2, bodyY+216)

	// Under the button: the exit switch, the split mode, and the hint.
	u.hline(c, cardX+16, bodyY+430, cardW-32, th.line)
	u.rowToggle(c, cardX, bodyY+442, cardW, "psiphon", tr("Psiphon 出口"),
		tr("先连基础协议，再叠加 Psiphon 出口"), u.m.psiphon, u.m.proto != protoWWarp && !u.m.connected)
	u.splitRow(c, cardX, bodyY+486, cardW)
	u.hline(c, cardX+16, bodyY+534, cardW-32, th.line)
	u.textC(c, tr("点击圆钮连接或断开"), u.f.f8, th.faint, u.r(cardX+16, bodyY+540, cardW-32, 20))
	u.textC(c, tr("会话：")+u.sessionLabel(), u.f.f9, th.dim, u.r(cardX+16, bodyY+560, cardW-32, 20))

	u.paintExitCard(c, rightX, bodyY, rightW, 132)
	u.paintProtoCard(c, rightX, bodyY+146, rightW, 158)
	u.paintNodeCard(c, rightX, bodyY+318, rightW, bodyH-318)
}

// paintConnectButton is the big round control. It is the only way to connect or
// disconnect, so it carries the state in its own colour and text.
func (u *natUI) paintConnectButton(c *walk.Canvas, cx, cy float64) {
	const (
		outerR = 112.0
		innerR = 96.0
	)
	ring, disc, fg := th.accentSoft, th.card, th.accent
	switch {
	case u.m.connected:
		ring, disc, fg = th.accentSoft, th.accent, th.accentText
	case u.m.connecting:
		ring, fg = th.warnSoft, th.warn
	case u.m.working:
		ring = th.chip
	}
	u.circleFill(c, ring, cx, cy, outerR)
	if u.m.connected {
		u.radial(c, th.connectFrom, th.connectTo, cx, cy, innerR)
	} else {
		u.circleFill(c, disc, cx, cy, innerR)
	}
	if u.m.connecting || u.m.working {
		u.drawSpinner(c, cx, cy, innerR-6, u.m.spin)
	}

	// Power glyph, state word, and the session clock.
	u.icon(c, "power", cx-13, cy-52, 26, fg)
	state := tr("未连接")
	switch {
	case u.m.connecting:
		state = tr("连接中…")
	case u.m.working:
		state = tr("断开中…")
	case u.m.connected:
		state = tr("已连接")
	}
	u.textC(c, state, u.f.f13b, fg, u.r(cx-90, cy-18, 180, 26))
	sub := tr("点击连接")
	subCol := th.dim
	if u.m.connected {
		sub = clockText(sessionUptime(u.m.status))
		subCol = th.accentText
	} else if u.m.connecting {
		sub = u.sessionLabel()
		subCol = th.dim
	}
	u.textC(c, sub, u.f.f12b, subCol, u.r(cx-90, cy+12, 180, 24))
	if u.m.connected {
		u.textC(c, protoLabel(u.m.proto), u.f.f8, th.accentText, u.r(cx-90, cy+38, 180, 18))
	}
	u.addSpot("connect", u.r(cx-outerR, cy-outerR, outerR*2, outerR*2), !u.m.working)
}

// splitRow shows the current split mode and jumps to the rules page.
func (u *natUI) splitRow(c *walk.Canvas, x, y, w float64) {
	u.txt(c, x+16, y+9, 60, 20, tr("分流"), u.f.f10, th.text)
	val := tr("关闭")
	switch u.m.set.SplitMode {
	case splitProxy:
		val = tr("仅代理所选")
	case splitDirect:
		val = tr("所选直连")
	}
	n := len(u.m.set.splitAppList())
	u.textR(c, val, u.f.f9, th.dim, u.r(x+80, y+9, w-150, 20))
	if n > 0 {
		u.textR(c, itoa(int64(n))+tr("个"), u.f.f8, th.faint, u.r(x+w-70, y+10, 40, 18))
	}
	u.icon(c, "chevron", x+w-32, y+14, 12, th.faint)
	u.addSpot("go:rules", u.r(x, y, w, 38), true)
}

// paintExitCard is the "current exit" summary: where traffic leaves, how far
// away it is, and how much of it is moving.
func (u *natUI) paintExitCard(c *walk.Canvas, x, y, w, h float64) {
	u.cardPlain(c, x, y, w, h)
	u.txt(c, x+16, y+12, w-32, 20, tr("当前出口"), u.f.f11b, th.text)

	cc := coloCountry[strings.ToUpper(u.m.status.Colo)]
	if cc == "" {
		cc = "UN"
	}
	u.flagChip(c, x+16, y+42, 26, 18, cc)
	name, sub := tr("未连接"), tr("点击连接")
	if u.m.connected {
		name = orEmpty(strings.ToUpper(u.m.status.Colo), tr("已连接"))
		sub = u.sessionLabel()
	}
	u.txt(c, x+50, y+38, 150, 22, name, u.f.f10b, th.text)
	u.txt(c, x+50, y+58, 200, 18, sub, u.f.f8, th.dim)

	ms := u.m.status.LatencyMS
	if ms == 0 {
		ms = u.m.latency
	}
	u.textR(c, latencyText(ms), u.f.f12b, th.text, u.r(x+w-110, y+38, 94, 24))
	u.bars(c, x+w-62, y+62, ms, th.accent)

	// Throughput tiles.
	tileW := (w - 32 - 10) / 2
	u.statTile(c, x+16, y+h-52, tileW, 40, tr("上传"), speedText(lastOf(u.m.upRates)))
	u.statTile(c, x+16+tileW+10, y+h-52, tileW, 40, tr("下载"), speedText(lastOf(u.m.downRates)))
}

func (u *natUI) statTile(c *walk.Canvas, x, y, w, h float64, label, value string) {
	u.fillRound(c, th.input, u.r(x, y, w, h), 9)
	u.txt(c, x+12, y+4, w-24, 16, label, u.f.f8, th.dim)
	u.txt(c, x+12, y+19, w-24, 20, value, u.f.f12b, th.text)
}

// paintProtoCard is the protocol picker. Each tile is a real protocol of the
// engine, and the state line says whether the installed core can run it.
func (u *natUI) paintProtoCard(c *walk.Canvas, x, y, w, h float64) {
	u.cardPlain(c, x, y, w, h)
	u.txt(c, x+16, y+12, 220, 20, tr("协议"), u.f.f11b, th.text)

	main := []string{protoWG, protoAWG, protoMASQUE, protoMASQUEH2}
	gap := 8.0
	tw := (w - 32 - gap*float64(len(main)-1)) / float64(len(main))
	for i, p := range main {
		u.protoTile(c, x+16+float64(i)*(tw+gap), y+38, tw, 52, p)
	}

	// WARP-in-WARP is one tile plus its carrier and inner protocol.
	rowY := y + 98
	ww := tw*1.6 + gap
	u.protoTile(c, x+16, rowY, ww, 44, protoWWarp)
	if u.m.proto == protoWWarp {
		pairX := x + 16 + ww + 12
		u.txt(c, pairX, rowY + 2, 60, 18, tr("外层"), u.f.f8, th.dim)
		u.chainChips(c, pairX, rowY+20, "outer:", u.m.outerProto)
		pairX += 96
		u.txt(c, pairX, rowY + 2, 60, 18, tr("内层"), u.f.f8, th.dim)
		u.chainChips(c, pairX, rowY+20, "inner:", u.m.innerProto)
	}
}

func (u *natUI) chainChips(c *walk.Canvas, x, y float64, prefix, cur string) {
	for i, p := range []string{protoWG, protoAWG} {
		u.chipAt(c, prefix+p, strings.ToUpper(p), x+float64(i)*54, y, cur == p, !u.m.working && !u.m.connecting)
	}
}

func (u *natUI) protoTile(c *walk.Canvas, x, y, w, h float64, p string) {
	r := u.r(x, y, w, h)
	active := u.m.proto == p
	bg, line := th.input, th.line
	tcol := th.text
	if active {
		bg, line, tcol = th.accentSoft, th.accentLine, th.accent
	} else if u.m.hover == "proto:"+p {
		bg = th.cardHover
	}
	u.fillRound(c, bg, r, 10)
	u.strokeRound(c, line, r, 10)
	u.textC(c, protoTileLabel(p), u.f.f10b, tcol, u.r(x, y+6, w, 20))

	state := tr("不可用")
	col := th.bad
	if u.m.capab == nil {
		state, col = tr("未探测"), th.faint
	} else if u.protoOK(p) {
		state, col = tr("可用"), th.ok
	}
	u.statusDot(c, x+w/2-22, y+h-13, 3, col)
	u.txt(c, x+w/2-14, y+h-22, 60, 18, state, u.f.f8, th.dim)
	u.addSpot("proto:"+p, r, !u.m.working && !u.m.connecting)
}

// paintNodeCard is the selected node's detail, with the live throughput chart.
func (u *natUI) paintNodeCard(c *walk.Canvas, x, y, w, h float64) {
	u.cardPlain(c, x, y, w, h)
	u.txt(c, x+16, y+12, w-32, 20, tr("节点信息"), u.f.f11b, th.text)

	st := u.m.status
	name := tr("未选择节点")
	if u.m.pin != "" {
		name = u.m.pin
	} else if st.Endpoint != "" {
		name = st.Endpoint
	}
	rows := [][2]string{
		{tr("节点名称"), name},
		{tr("延迟"), latencyText(st.LatencyMS)},
		{tr("出口"), orDash(st.ExitIP)},
		{tr("国家/地区"), orDash(strings.ToUpper(st.Country))},
		{tr("协议"), protoLabel(u.m.proto)},
	}
	ry := y + 44
	for _, kv := range rows {
		u.kv(c, x, ry, w/2+40, kv[0], kv[1])
		ry += 26
	}

	// Chart: only real samples are drawn; an empty series says so instead of
	// showing a flat line that would read as "zero traffic".
	chartX, chartY := x+w/2+52, y+44
	chartW, chartH := w-(w/2+52)-16, 118.0
	u.txt(c, chartX, chartY-18, chartW, 16, tr("实时流量"), u.f.f8, th.dim)
	u.gridLines(c, chartX, chartY, chartW, chartH, 4)
	if len(u.m.downRates) < 2 && len(u.m.upRates) < 2 {
		u.textC(c, tr("连接后开始采集"), u.f.f8, th.faint, u.r(chartX, chartY+40, chartW, 20))
	} else {
		u.spark(c, chartX, chartY, chartW, chartH,
			[][]int64{u.m.downRates, u.m.upRates},
			[]walk.Color{th.accent, th.ok})
	}
	u.statusDot(c, chartX+4, chartY+chartH+14, 3, th.accent)
	u.txt(c, chartX+12, chartY+chartH+6, 60, 16, tr("下载"), u.f.f8, th.dim)
	u.statusDot(c, chartX+62, chartY+chartH+14, 3, th.ok)
	u.txt(c, chartX+70, chartY+chartH+6, 60, 16, tr("上传"), u.f.f8, th.dim)

	u.txt(c, x+16, y+h-24, w-32, 18, tr("先连接后查看节点信息"), u.f.f8, th.faint)
}

// protoTileLabel is the display name on a protocol tile. protoLabel is the
// uppercase log tag; this is what a user should read.
func protoTileLabel(p string) string {
	switch p {
	case protoMASQUE:
		return "MASQUE H3"
	case protoMASQUEH2:
		return "MASQUE H2"
	case protoWWarp:
		return "WARP-in-WARP"
	default:
		return strings.ToUpper(p)
	}
}

// sessionUptime is how long the recorded session has been up, from the state
// file's start stamp.
func sessionUptime(st clientState) time.Duration {
	if !st.Connected || st.StartedAt == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, st.StartedAt)
	if err != nil {
		return 0
	}
	return time.Since(t)
}

func clockText(d time.Duration) string {
	if d <= 0 {
		return "00:00:00"
	}
	s := int64(d.Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, (s/60)%60, s%60)
}

func lastOf(v []int64) int64 {
	if len(v) == 0 {
		return 0
	}
	return v[len(v)-1]
}
