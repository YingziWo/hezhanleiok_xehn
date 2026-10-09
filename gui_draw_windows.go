//go:build windows

package main

import (
	"math"
	"strconv"

	"github.com/lxn/walk"
)

// ---------------------------------------------------------------------------
// Primitives
//
// Everything takes design coordinates (the 1080x700 grid) and scales through
// u.r/u.s, so a widget never has to know the display DPI.
// ---------------------------------------------------------------------------

func (u *natUI) fillRound(c *walk.Canvas, col walk.Color, r walk.Rectangle, rad float64) {
	b := u.brush(col)
	defer b.Dispose()
	_ = c.FillRoundedRectanglePixels(b, r, walk.Size{Width: u.s(rad), Height: u.s(rad)})
}

func (u *natUI) strokeRound(c *walk.Canvas, col walk.Color, r walk.Rectangle, rad float64) {
	p := u.pen(col)
	defer p.Dispose()
	_ = c.DrawRoundedRectanglePixels(p, r, walk.Size{Width: u.s(rad), Height: u.s(rad)})
}

func (u *natUI) fillRect(c *walk.Canvas, col walk.Color, r walk.Rectangle) {
	b := u.brush(col)
	defer b.Dispose()
	_ = c.FillRectanglePixels(b, r)
}

func (u *natUI) brush(col walk.Color) *walk.SolidColorBrush {
	b, _ := walk.NewSolidColorBrush(col)
	return b
}

func (u *natUI) pen(col walk.Color) *walk.CosmeticPen {
	p, _ := walk.NewCosmeticPen(walk.PenSolid, col)
	return p
}

// hline is the one-pixel separator used inside cards and under tabs.
func (u *natUI) hline(c *walk.Canvas, x, y, w float64, col walk.Color) {
	u.fillRect(c, col, u.r(x, y, w, 1))
}

// circleFill / circleStroke build the round shapes: the connect button, status
// dots, avatars and the signal rings. They are separate from fillRound because
// a circle takes a diameter, not a corner radius.
func (u *natUI) circleFill(c *walk.Canvas, col walk.Color, cx, cy, r float64) {
	b := u.brush(col)
	defer b.Dispose()
	_ = c.FillEllipsePixels(b, u.r(cx-r, cy-r, 2*r, 2*r))
}

func (u *natUI) circleStroke(c *walk.Canvas, col walk.Color, cx, cy, r float64) {
	p := u.pen(col)
	defer p.Dispose()
	_ = c.DrawEllipsePixels(p, u.r(cx-r, cy-r, 2*r, 2*r))
}

// ring draws a thick circle as "outer disc minus inner disc". A cosmetic pen is
// always one pixel wide, so this is how a heavy ring is drawn without a
// geometric pen.
func (u *natUI) ring(c *walk.Canvas, col, inner walk.Color, cx, cy, r, width float64) {
	u.circleFill(c, col, cx, cy, r)
	u.circleFill(c, inner, cx, cy, r-width)
}

func (u *natUI) line(c *walk.Canvas, col walk.Color, x1, y1, x2, y2 float64) {
	p := u.pen(col)
	defer p.Dispose()
	_ = c.DrawLinePixels(p, u.p(x1, y1), u.p(x2, y2))
}

func mix(a, b walk.Color, t float64) walk.Color {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	ar, ag, ab := a.R(), a.G(), a.B()
	br, bg, bb := b.R(), b.G(), b.B()
	return walk.RGB(
		byte(float64(ar)+(float64(br)-float64(ar))*t),
		byte(float64(ag)+(float64(bg)-float64(ag))*t),
		byte(float64(ab)+(float64(bb)-float64(ab))*t))
}

// radial fills a circle with a soft top-to-bottom gradient, drawn as concentric
// discs. The step count is fixed: it is a gradient, not a computation.
func (u *natUI) radial(c *walk.Canvas, from, to walk.Color, cx, cy, r float64) {
	const steps = 24
	for i := 0; i < steps; i++ {
		t := float64(i) / float64(steps-1)
		u.circleFill(c, mix(from, to, t), cx, cy-t*2, r*(1-0.06*t))
	}
}

// ---------------------------------------------------------------------------
// Text
// ---------------------------------------------------------------------------

func (u *natUI) text(c *walk.Canvas, s string, f *walk.Font, col walk.Color, r walk.Rectangle, fmtFlag walk.DrawTextFormat) {
	_ = c.DrawTextPixels(s, f, col, r, fmtFlag|walk.TextNoPrefix)
}

func (u *natUI) textL(c *walk.Canvas, s string, f *walk.Font, col walk.Color, r walk.Rectangle) {
	u.text(c, s, f, col, r, walk.TextSingleLine|walk.TextVCenter|walk.TextLeft|walk.TextEndEllipsis)
}

func (u *natUI) textR(c *walk.Canvas, s string, f *walk.Font, col walk.Color, r walk.Rectangle) {
	u.text(c, s, f, col, r, walk.TextSingleLine|walk.TextVCenter|walk.TextRight)
}

func (u *natUI) textC(c *walk.Canvas, s string, f *walk.Font, col walk.Color, r walk.Rectangle) {
	u.text(c, s, f, col, r, walk.TextSingleLine|walk.TextVCenter|walk.TextCenter)
}

// txt draws a single left-aligned line at (x, y) with the given height.
func (u *natUI) txt(c *walk.Canvas, x, y, w, h float64, s string, f *walk.Font, col walk.Color) {
	u.textL(c, s, f, col, u.r(x, y, w, h))
}

// ---------------------------------------------------------------------------
// Icons
//
// Drawn from primitives rather than shipped as images: there is no icon font in
// the build, and a dozen shapes is cheaper than a resource pipeline.
// ---------------------------------------------------------------------------

func (u *natUI) icon(c *walk.Canvas, kind string, x, y, size float64, col walk.Color) {
	s := size
	switch kind {
	case "home":
		// Roof: two lines meeting at the top; body: a rounded box under it.
		u.line(c, col, x, y+s*0.46, x+s*0.5, y+s*0.10)
		u.line(c, col, x+s*0.5, y+s*0.10, x+s, y+s*0.46)
		u.strokeRound(c, col, u.r(x+s*0.16, y+s*0.44, s*0.68, s*0.46), 2)
		u.line(c, col, x+s*0.44, y+s*0.90, x+s*0.44, y+s*0.66)
		u.line(c, col, x+s*0.56, y+s*0.90, x+s*0.56, y+s*0.66)
	case "nodes":
		// A stack of server rows.
		for i := 0; i < 3; i++ {
			yy := y + s*0.12 + float64(i)*s*0.30
			u.strokeRound(c, col, u.r(x+s*0.06, yy, s*0.88, s*0.22), 2)
			u.circleFill(c, col, x+s*0.22, yy+s*0.11, s*0.045)
		}
	case "rules":
		// Shield outline with a check inside.
		u.line(c, col, x+s*0.10, y+s*0.22, x+s*0.90, y+s*0.22)
		u.strokeRound(c, col, u.r(x+s*0.10, y+s*0.14, s*0.80, s*0.52), 3)
		u.line(c, col, x+s*0.36, y+s*0.44, x+s*0.47, y+s*0.58)
		u.line(c, col, x+s*0.47, y+s*0.58, x+s*0.68, y+s*0.32)
	case "settings":
		// Sliders: three rails with a knob each, which reads as "settings"
		// without a gear's aliasing at small sizes.
		for i := 0; i < 3; i++ {
			yy := y + s*0.20 + float64(i)*s*0.30
			u.line(c, col, x+s*0.08, yy, x+s*0.92, yy)
			knob := 0.30 + float64(i)*0.20
			u.circleFill(c, th.card, x+s*knob, yy, s*0.10)
			u.circleStroke(c, col, x+s*knob, yy, s*0.10)
		}
	case "logs":
		for i := 0; i < 3; i++ {
			yy := y + s*0.18 + float64(i)*s*0.30
			u.circleFill(c, col, x+s*0.10, yy, s*0.055)
			u.line(c, col, x+s*0.28, yy, x+s*0.94, yy)
		}
	case "close":
		u.line(c, col, x+s*0.18, y+s*0.18, x+s*0.82, y+s*0.82)
		u.line(c, col, x+s*0.82, y+s*0.18, x+s*0.18, y+s*0.82)
	case "search":
		u.circleStroke(c, col, x+s*0.42, y+s*0.42, s*0.32)
		u.line(c, col, x+s*0.66, y+s*0.66, x+s*0.94, y+s*0.94)
	case "refresh":
		u.circleStroke(c, col, x+s*0.5, y+s*0.52, s*0.34)
		u.line(c, col, x+s*0.62, y+s*0.10, x+s*0.86, y+s*0.20)
		u.line(c, col, x+s*0.62, y+s*0.10, x+s*0.58, y+s*0.34)
	case "chevron":
		u.line(c, col, x+s*0.18, y+s*0.34, x+s*0.50, y+s*0.66)
		u.line(c, col, x+s*0.50, y+s*0.66, x+s*0.82, y+s*0.34)
	case "power":
		u.circleStroke(c, col, x+s*0.5, y+s*0.56, s*0.34)
		u.line(c, col, x+s*0.5, y+s*0.10, x+s*0.5, y+s*0.46)
	case "globe":
		u.circleStroke(c, col, x+s*0.5, y+s*0.5, s*0.4)
		u.line(c, col, x+s*0.10, y+s*0.5, x+s*0.90, y+s*0.5)
		u.circleStroke(c, col, x+s*0.5, y+s*0.5, s*0.18)
	case "shield":
		u.strokeRound(c, col, u.r(x+s*0.14, y+s*0.10, s*0.72, s*0.62), 4)
		u.line(c, col, x+s*0.32, y+s*0.42, x+s*0.46, y+s*0.58)
		u.line(c, col, x+s*0.46, y+s*0.58, x+s*0.70, y+s*0.28)
	case "bolt":
		u.line(c, col, x+s*0.56, y+s*0.06, x+s*0.32, y+s*0.52)
		u.line(c, col, x+s*0.32, y+s*0.52, x+s*0.52, y+s*0.52)
		u.line(c, col, x+s*0.52, y+s*0.52, x+s*0.44, y+s*0.94)
		u.line(c, col, x+s*0.44, y+s*0.94, x+s*0.68, y+s*0.44)
		u.line(c, col, x+s*0.68, y+s*0.44, x+s*0.48, y+s*0.44)
		u.line(c, col, x+s*0.48, y+s*0.44, x+s*0.56, y+s*0.06)
	case "sun":
		u.circleStroke(c, col, x+s*0.5, y+s*0.5, s*0.22)
		for i := 0; i < 8; i++ {
			a := float64(i) * math.Pi / 4
			cs, sn := math.Cos(a), math.Sin(a)
			u.line(c, col,
				x+s*0.5+cs*s*0.34, y+s*0.5+sn*s*0.34,
				x+s*0.5+cs*s*0.46, y+s*0.5+sn*s*0.46)
		}
	case "moon":
		u.circleFill(c, col, x+s*0.5, y+s*0.5, s*0.36)
		u.circleFill(c, th.card, x+s*0.66, y+s*0.36, s*0.34)
	case "monitor":
		u.strokeRound(c, col, u.r(x+s*0.06, y+s*0.14, s*0.88, s*0.58), 2)
		u.line(c, col, x+s*0.34, y+s*0.88, x+s*0.66, y+s*0.88)
		u.line(c, col, x+s*0.5, y+s*0.72, x+s*0.5, y+s*0.88)
	case "star":
		// Four-point sparkle: cheap, and unmistakably a favourite marker.
		u.line(c, col, x+s*0.5, y+s*0.08, x+s*0.5, y+s*0.92)
		u.line(c, col, x+s*0.08, y+s*0.5, x+s*0.92, y+s*0.5)
		u.line(c, col, x+s*0.22, y+s*0.22, x+s*0.78, y+s*0.78)
		u.line(c, col, x+s*0.78, y+s*0.22, x+s*0.22, y+s*0.78)
	case "pin":
		u.circleFill(c, col, x+s*0.5, y+s*0.36, s*0.20)
		u.line(c, col, x+s*0.5, y+s*0.56, x+s*0.5, y+s*0.92)
	case "grid":
		for i := 0; i < 2; i++ {
			for j := 0; j < 2; j++ {
				u.strokeRound(c, col, u.r(x+s*0.10+float64(i)*s*0.46, y+s*0.10+float64(j)*s*0.46, s*0.36, s*0.36), 2)
			}
		}
	case "list":
		u.line(c, col, x+s*0.08, y+s*0.24, x+s*0.92, y+s*0.24)
		u.line(c, col, x+s*0.08, y+s*0.50, x+s*0.92, y+s*0.50)
		u.line(c, col, x+s*0.08, y+s*0.76, x+s*0.92, y+s*0.76)
	case "link":
		u.circleStroke(c, col, x+s*0.32, y+s*0.5, s*0.22)
		u.circleStroke(c, col, x+s*0.68, y+s*0.5, s*0.22)
		u.line(c, col, x+s*0.32, y+s*0.5, x+s*0.68, y+s*0.5)
	case "folder":
		u.strokeRound(c, col, u.r(x+s*0.08, y+s*0.22, s*0.84, s*0.62), 2)
		u.line(c, col, x+s*0.08, y+s*0.38, x+s*0.40, y+s*0.38)
		u.line(c, col, x+s*0.40, y+s*0.38, x+s*0.46, y+s*0.24)
	}
}

// statusDot is the small coloured bullet used by pills and rows.
func (u *natUI) statusDot(c *walk.Canvas, cx, cy, r float64, col walk.Color) {
	u.circleFill(c, col, cx, cy, r)
}

// bars renders the four-bar signal strength for a latency in milliseconds. It
// is a coarse quality hint, not a measurement: <60ms all four, then one bar per
// 80ms.
func (u *natUI) bars(c *walk.Canvas, x, y float64, ms int64, col walk.Color) {
	lit := 0
	switch {
	case ms <= 0:
		lit = 0
	case ms < 60:
		lit = 4
	case ms < 120:
		lit = 3
	case ms < 200:
		lit = 2
	default:
		lit = 1
	}
	for i := 0; i < 4; i++ {
		h := 4.5 + float64(i)*3.5
		cx := x + float64(i)*5.5
		cy := y + (16-h)/2
		r := u.r(cx, cy, 3, h)
		if i < lit {
			u.fillRound(c, col, r, 1.5)
		} else {
			u.fillRound(c, th.toggleOff, r, 1.5)
		}
	}
}

// spark draws one or more normalised series as polylines inside a box. An empty
// series leaves the box blank rather than drawing a fake flat line.
func (u *natUI) spark(c *walk.Canvas, x, y, w, h float64, series [][]int64, cols []walk.Color) {
	var peak int64
	for _, s := range series {
		for _, v := range s {
			if v > peak {
				peak = v
			}
		}
	}
	if peak <= 0 {
		return
	}
	for si, s := range series {
		if len(s) < 2 {
			continue
		}
		col := th.accent
		if si < len(cols) {
			col = cols[si]
		}
		step := w / float64(len(s)-1)
		px := x
		py := y + h - float64(s[0])/float64(peak)*h
		for i := 1; i < len(s); i++ {
			nx := x + float64(i)*step
			ny := y + h - float64(s[i])/float64(peak)*h
			u.line(c, col, px, py, nx, ny)
			px, py = nx, ny
		}
	}
}

// gridLines draws the faint horizontal rules behind a chart.
func (u *natUI) gridLines(c *walk.Canvas, x, y, w, h float64, n int) {
	for i := 1; i < n; i++ {
		yy := y + h*float64(i)/float64(n)
		u.fillRect(c, th.line, u.r(x, yy, w, 1))
	}
}

// ---------------------------------------------------------------------------
// Widgets
// ---------------------------------------------------------------------------

// card draws a titled surface and returns the y where its content starts.
func (u *natUI) card(c *walk.Canvas, x, y, w, h float64, title string) float64 {
	u.fillRound(c, th.card, u.r(x, y, w, h), 12)
	u.strokeRound(c, th.line, u.r(x+0.5, y+0.5, w-1, h-1), 12)
	if title == "" {
		return y + 12
	}
	u.txt(c, x+16, y+12, w-32, 20, title, u.f.f11b, th.text)
	return y + 40
}

// cardPlain is a card without a title (or with one drawn by the caller).
func (u *natUI) cardPlain(c *walk.Canvas, x, y, w, h float64) {
	u.fillRound(c, th.card, u.r(x, y, w, h), 12)
	u.strokeRound(c, th.line, u.r(x+0.5, y+0.5, w-1, h-1), 12)
}

// rowToggle is a labelled switch with an optional second line.
func (u *natUI) rowToggle(c *walk.Canvas, x, y, w float64, id, label, sub string, on, enabled bool) {
	lh := 20.0
	if sub != "" {
		lh = 34
		u.txt(c, x+16, y+2, w-80, 18, label, u.f.f10, th.text)
		u.txt(c, x+16, y+20, w-80, 16, sub, u.f.f8, th.dim)
	} else {
		u.txt(c, x+16, y+(38-lh)/2, w-80, lh, label, u.f.f10, th.text)
	}
	u.switchAt(c, id, x+w-16-44, y+9, on, enabled)
	if enabled {
		u.addSpot(id, u.r(x, y, w, 38), true)
	}
}

// switchAt draws the pill switch itself.
func (u *natUI) switchAt(c *walk.Canvas, id string, x, y float64, on, enabled bool) {
	track := u.r(x, y, 44, 22)
	col := th.toggleOff
	if on {
		col = th.accent
	}
	if !enabled {
		col = mix(col, th.card, 0.55)
	}
	u.fillRound(c, col, track, 11)
	knobX := x + 3
	if on {
		knobX = x + 44 - 22
	}
	u.circleFill(c, th.card, knobX+9, y+11, 8)
}

// rowEdit is a label with an editable value box; clicking it opens the text
// dialog, which is the only way this window accepts typing.
func (u *natUI) rowEdit(c *walk.Canvas, x, y, w float64, id, label, value, placeholder string) {
	u.txt(c, x+16, y+8, w-260, 20, label, u.f.f10, th.text)
	box := u.r(x+w-16-228, y+4, 228, 30)
	u.fillRound(c, th.input, box, 8)
	if u.m.hover == id {
		u.strokeRound(c, th.accentLine, box, 8)
	}
	disp, col := value, th.text
	if disp == "" {
		disp, col = placeholder, th.faint
	}
	u.textL(c, disp, u.f.f9, col, u.r(x+w-16-228+10, y+4, 208, 30))
	u.addSpot(id, box, true)
}

// rowChips is a label with a set of mutually exclusive chips beside it.
func (u *natUI) rowChips(c *walk.Canvas, x, y, w float64, idPrefix, label string, ids, labels []string, cur string) {
	const labelW = 84.0
	u.txt(c, x+16, y+4, labelW, 22, label, u.f.f10, th.text)
	cx := x + 16 + labelW
	for i := range ids {
		cw := chipWidth(labels[i])
		if cx+cw > x+w-12 {
			break
		}
		u.chipAt(c, idPrefix+ids[i], labels[i], cx, y, cur == ids[i], true)
		cx += cw + 6
	}
}

// chipAt draws one chip and registers its hit area. Returns its width.
func (u *natUI) chipAt(c *walk.Canvas, id, label string, x, y float64, active, enabled bool) float64 {
	w := chipWidth(label)
	r := u.r(x, y, w, 26)
	bg, fg, line := th.chip, th.chipText, th.line
	if active {
		bg, fg, line = th.accentSoft, th.accent, th.accentLine
	} else if u.m.hover == id && enabled {
		bg = th.cardHover
	}
	if !enabled {
		fg = th.faint
	}
	u.fillRound(c, bg, r, 8)
	u.strokeRound(c, line, r, 8)
	u.textC(c, label, u.f.f9, fg, r)
	if enabled {
		u.addSpot(id, r, true)
	}
	return w
}

// chipWidth estimates a chip's width in design units. Deliberately not measured
// from the canvas: MeasureTextPixels answers in device pixels while every
// coordinate here is in the design grid, so mixing them doubles widths on a
// high-DPI display.
func chipWidth(label string) float64 {
	w := 0.0
	for _, r := range label {
		switch {
		case r > 0x2E80: // CJK and fullwidth forms
			w += 12.5
		case r >= 'A' && r <= 'Z':
			w += 7.6
		default:
			w += 6.6
		}
	}
	if w < 20 {
		w = 20
	}
	return w + 22
}

// button is the filled (primary) or outlined action button.
func (u *natUI) button(c *walk.Canvas, x, y, w, h float64, id, label string, primary, enabled bool) {
	r := u.r(x, y, w, h)
	bg, fg, line := th.card, th.text, th.line
	if primary {
		bg, fg, line = th.accent, th.accentText, th.accent
	} else if u.m.hover == id && enabled {
		bg = th.cardHover
	}
	if !enabled {
		bg, fg = mix(bg, th.card, 0.5), th.faint
	}
	u.fillRound(c, bg, r, 8)
	if !primary || !enabled {
		u.strokeRound(c, line, r, 8)
	}
	u.textC(c, label, u.f.f10b, fg, r)
	if enabled {
		u.addSpot(id, r, true)
	}
}

// iconButton is a square, borderless button holding one icon.
func (u *natUI) iconButton(c *walk.Canvas, x, y, size float64, id, kind string, enabled bool) {
	r := u.r(x, y, size, size)
	if u.m.hover == id && enabled {
		u.fillRound(c, th.cardHover, r, 8)
	}
	col := th.dim
	if !enabled {
		col = th.faint
	}
	u.icon(c, kind, x+size*0.22, y+size*0.22, size*0.56, col)
	if enabled {
		u.addSpot(id, r, true)
	}
}

// link is an inline text action with an accent colour and an underline on hover.
func (u *natUI) link(c *walk.Canvas, x, y, w, h float64, id, label string) {
	u.txt(c, x, y, w, h, label, u.f.f9, th.accent)
	if u.m.hover == id {
		u.hline(c, x, y+h-6, float64(len([]rune(label)))*7, th.accent)
	}
	u.addSpot(id, u.r(x, y, w, h), true)
}

// inputBox draws a read-only-looking field. Clicking it opens the text dialog;
// the field itself never takes focus.
func (u *natUI) inputBox(c *walk.Canvas, x, y, w, h float64, id, value, placeholder string) {
	r := u.r(x, y, w, h)
	u.fillRound(c, th.input, r, 8)
	if u.m.hover == id {
		u.strokeRound(c, th.accentLine, r, 8)
	}
	disp, col := value, th.text
	if disp == "" {
		disp, col = placeholder, th.faint
	}
	u.textL(c, disp, u.f.f9, col, u.r(x+10, y, w-20, h))
	u.addSpot(id, r, true)
}

// subtabs is the underlined tab bar used inside the rules, settings and log
// pages. It returns nothing: the active tab is whatever cur says.
func (u *natUI) subtabs(c *walk.Canvas, x, y float64, prefix string, ids, labels []string, cur string) float64 {
	cx := x
	for i := range ids {
		w := chipWidth(labels[i]) + 8
		r := u.r(cx, y, w, 30)
		active := cur == ids[i]
		col := th.dim
		if active {
			col = th.accent
		} else if u.m.hover == prefix+ids[i] {
			col = th.text
		}
		u.textC(c, labels[i], u.f.f10b, col, r)
		if active {
			u.fillRound(c, th.accent, u.r(cx+(w-26)/2, y+30, 26, 3), 1.5)
		}
		u.addSpot(prefix+ids[i], r, true)
		cx += w + 6
	}
	return cx
}

// kv is a label/value pair on one line (used by the about cards).
func (u *natUI) kv(c *walk.Canvas, x, y, w float64, label, value string) {
	u.txt(c, x+16, y, 84, 22, label, u.f.f9, th.dim)
	u.txt(c, x+16+84, y, w-32-84, 22, value, u.f.f9, th.text)
}

// bulletRow is the "icon + text" row used by the split-rule list: a tinted
// square icon, a title, an optional subtitle and a right-hand state chip.
func (u *natUI) bulletRow(c *walk.Canvas, x, y, w float64, kind, title, sub, right string, rightCol walk.Color, iconBg walk.Color) {
	u.fillRound(c, iconBg, u.r(x+16, y+6, 26, 26), 8)
	u.icon(c, kind, x+16+7, y+6+7, 12, rightCol)
	if sub == "" {
		u.txt(c, x+52, y+7, w-160, 24, title, u.f.f10, th.text)
	} else {
		u.txt(c, x+52, y+2, w-160, 18, title, u.f.f10, th.text)
		u.txt(c, x+52, y+20, w-160, 16, sub, u.f.f8, th.dim)
	}
	if right != "" {
		u.textR(c, right, u.f.f9, rightCol, u.r(x+w-16-70, y+7, 70, 24))
	}
}

// flagChip draws a flag and the country code from an ISO alpha-2 code.
func (u *natUI) flagChip(c *walk.Canvas, x, y, w, h float64, cc string) {
	u.fillRound(c, th.input, u.r(x, y, w, h), 4)
	u.drawFlag(c, x+2, y+2, w-4, h-4, cc)
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
