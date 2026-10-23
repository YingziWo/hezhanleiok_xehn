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
	if b == nil {
		return
	}
	defer b.Dispose()
	_ = c.FillRoundedRectanglePixels(b, r, walk.Size{Width: u.s(rad), Height: u.s(rad)})
}

func (u *natUI) strokeRound(c *walk.Canvas, col walk.Color, r walk.Rectangle, rad float64) {
	p := u.pen(col)
	if p == nil {
		return
	}
	defer p.Dispose()
	_ = c.DrawRoundedRectanglePixels(p, r, walk.Size{Width: u.s(rad), Height: u.s(rad)})
}

func (u *natUI) fillRect(c *walk.Canvas, col walk.Color, r walk.Rectangle) {
	b := u.brush(col)
	if b == nil {
		return
	}
	defer b.Dispose()
	_ = c.FillRectanglePixels(b, r)
}

// brush hands back a solid brush or nil. A nil result must never reach a paint
// call: GDI does run out of handles, and a nil brush used to turn one failed
// allocation into a nil-pointer panic that closed the window mid-connect.
func (u *natUI) brush(col walk.Color) *walk.SolidColorBrush {
	b, err := walk.NewSolidColorBrush(col)
	if err != nil {
		return nil
	}
	return b
}

func (u *natUI) pen(col walk.Color) *walk.CosmeticPen {
	p, err := walk.NewCosmeticPen(walk.PenSolid, col)
	if err != nil {
		return nil
	}
	return p
}

// penW is a thick pen: width is in 1/96", which at this app's DPI is one
// design pixel. Icons, the ring around the connect button and the traffic
// curves all need strokes a cosmetic (always 1px) pen draws invisibly thin.
//
// The brush is disposed here: GeometricPen only remembers the colour values
// (logbrush builds a LOGBRUSH from them, no handle), so keeping the brush alive
// just leaked one GDI handle per thick stroke. At sixteen repaints a second
// that exhausted the process's handle quota in well under a minute.
func (u *natUI) penW(col walk.Color, w float64) *walk.GeometricPen {
	b := u.brush(col)
	if b == nil {
		return nil
	}
	defer b.Dispose()
	p, err := walk.NewGeometricPen(walk.PenSolid, int(w+0.5), b)
	if err != nil {
		return nil
	}
	return p
}

// shadowRound is the drop shadow under the cards: a stack of concentric
// rounded rectangles blending from the shadow tint back to the surface, which
// is what GDI+ (no blur, no alpha in Color) can do.
//
// Kept deliberately faint — on a light grey canvas macOS cards are separated by
// a hairline and a 2-3% shadow, not by the heavy blue halo the old mock used.
func (u *natUI) shadowRound(c *walk.Canvas, r walk.Rectangle, rad float64, tint walk.Color, base walk.Color) {
	for i := 3; i >= 1; i-- {
		d := float64(i)
		sr := walk.Rectangle{
			X:      r.X - u.s(d*0.5),
			Y:      r.Y - u.s(d*0.1),
			Width:  r.Width + u.s(d*1.0),
			Height: r.Height + u.s(d*1.2),
		}
		u.fillRound(c, mix(base, tint, 0.16/float64(i+1)), sr, rad+d)
	}
}

// circleStrokeW is a thick circle outline (the connect button's concentric
// rings, the rate badges).
func (u *natUI) circleStrokeW(c *walk.Canvas, col walk.Color, cx, cy, r, width float64) {
	p := u.penW(col, width)
	if p == nil {
		return
	}
	defer p.Dispose()
	if err := c.DrawEllipsePixels(p, u.r(cx-r, cy-r, 2*r, 2*r)); err != nil {
		// fall through: a failed decorative stroke must not break the frame
		_ = err
	}
}

// polylineW draws a smooth polyline with a thick pen.
func (u *natUI) polylineW(c *walk.Canvas, col walk.Color, pts []walk.Point, width float64) {
	if len(pts) < 2 {
		return
	}
	p := u.penW(col, width)
	if p == nil {
		return
	}
	defer p.Dispose()
	_ = c.DrawPolylinePixels(p, pts)
}

// arcW draws the open circle of the power glyph: an arc from a1 to a2 degrees
// (0 = 3 o'clock) sampled as a polyline, because there is no arc primitive.
func (u *natUI) arcW(c *walk.Canvas, col walk.Color, cx, cy, r, width, a1, a2 float64) {
	const steps = 40
	pts := make([]walk.Point, 0, steps+1)
	for i := 0; i <= steps; i++ {
		a := (a1 + (a2-a1)*float64(i)/float64(steps)) * math.Pi / 180
		pts = append(pts, u.p(cx+math.Cos(a)*r, cy+math.Sin(a)*r))
	}
	u.polylineW(c, col, pts, width)
}

// powerGlyph is the connect button's power mark, drawn with the same thick
// stroke as its ring so it stays legible at any size. The thin 1px version was
// the "icon nearly invisible" bug: at 200% DPI a cosmetic pen is one device
// pixel, which disappears against the gradient fill.
func (u *natUI) powerGlyph(c *walk.Canvas, cx, cy, size float64, col walk.Color) {
	r := size * 0.36
	w := size * 0.13
	// Stem: a filled bar through the gap at the top of the ring.
	u.fillRound(c, col, u.r(cx-w/2, cy-r-0.5, w, r*0.95), w/2)
	// Ring: 300° of circle, open at the top (from -60° to 240°).
	u.arcW(c, col, cx, cy, r, w, -62, 242)
}

// curveSamples turns a series into a dense, smoothed point list: Catmull-Rom
// interpolation between samples, which is what makes the sparkline read as a
// flowing curve instead of a zig-zag.
func (u *natUI) curveSamples(x, y, w, h float64, s []int64, peak int64, steps int) []walk.Point {
	if len(s) < 2 || peak <= 0 {
		return nil
	}
	px := func(i int) float64 { return x + w*float64(i)/float64(len(s)-1) }
	pts := make([]walk.Point, 0, len(s)*steps)
	at := func(i float64) (float64, float64) {
		i0 := int(math.Floor(i))
		if i0 < 0 {
			i0 = 0
		}
		if i0 > len(s)-2 {
			i0 = len(s) - 2
		}
		t := i - float64(i0)
		p0, p1, p2, p3 := i0-1, i0, i0+1, i0+2
		cl := func(i int) int {
			if i < 0 {
				return 0
			}
			if i > len(s)-1 {
				return len(s) - 1
			}
			return i
		}
		// Catmull-Rom on the y values with a uniform x step.
		v0, v1 := float64(s[cl(p0)]), float64(s[cl(p1)])
		v2, v3 := float64(s[cl(p2)]), float64(s[cl(p3)])
		yy := 0.5 * ((2 * v1) + (-v0+v2)*t + (2*v0-5*v1+4*v2-v3)*t*t + (-v0+3*v1-3*v2+v3)*t*t*t) / 1.0
		val := yy
		if val < 0 {
			val = 0
		}
		xx := px(cl(p1)) + (px(cl(p2))-px(cl(p1)))*t
		return xx, y + h - val/float64(peak)*h
	}
	for i := 0; i <= (len(s)-1)*steps; i++ {
		fx := float64(i) / float64(steps)
		xx, yy := at(fx)
		pts = append(pts, u.p(xx, yy))
	}
	return pts
}

// curveFill shades the area under a curve with a very light tint by painting
// thin columns from the curve down to the baseline — GDI+ has no alpha here,
// so the "low opacity" of the mock is a colour mixed toward the card.
func (u *natUI) curveFill(c *walk.Canvas, pts []walk.Point, baseline float64, col walk.Color, step float64) {
	for i := 0; i+1 < len(pts); i++ {
		x := u.ux(pts[i].X)
		yy := u.uy(pts[i].Y)
		if baseline <= yy {
			continue
		}
		u.fillRect(c, col, u.r(x, yy, step+0.6, baseline-yy))
	}
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
	if b == nil {
		return
	}
	defer b.Dispose()
	_ = c.FillEllipsePixels(b, u.r(cx-r, cy-r, 2*r, 2*r))
}

// ellipseFill is circleFill with independent radii, for the wallpaper's soft
// landscape banks (they are wide and flat, not round).
func (u *natUI) ellipseFill(c *walk.Canvas, col walk.Color, cx, cy, ew, eh float64) {
	b := u.brush(col)
	if b == nil {
		return
	}
	defer b.Dispose()
	_ = c.FillEllipsePixels(b, u.r(cx-ew, cy-eh, 2*ew, 2*eh))
}

// softEllipse paints a blurred-looking blob: a stack of nested ellipses whose
// colour fades toward the rim. GDI+ has no blur and no alpha in a solid brush,
// so blending each ring toward the surface colour is how the mock's out-of-focus
// hills are reproduced.
func (u *natUI) softEllipse(c *walk.Canvas, base walk.Color, cx, cy, ew, eh, strength float64) {
	// Many thin rings, mixed from the *local* surface colour toward white: the
	// outermost ring has to be exactly the surface, or the shape's rim paints a
	// darker halo and the "blur" shows up as a hard arc (which is what a wash
	// based on th.window does over a lighter gradient).
	const steps = 26
	for i := steps; i >= 1; i-- {
		t := float64(i) / float64(steps)
		f := strength * (1 - t) * (1 - t)
		if f <= 0.012 {
			continue
		}
		u.ellipseFill(c, mix(base, rx(255, 255, 255), f),
			cx, cy, ew*(0.55+0.45*t), eh*(0.55+0.45*t))
	}
}

func (u *natUI) circleStroke(c *walk.Canvas, col walk.Color, cx, cy, r float64) {
	p := u.pen(col)
	if p == nil {
		return
	}
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
	if p == nil {
		return
	}
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
	case "gear", "settings":
		// A gear outline: ring, hub and eight teeth. Drawn with weighted
		// strokes so it survives 20px on a 200% display.
		u.circleStrokeW(c, col, x+s*0.5, y+s*0.5, s*0.29, 1.6)
		u.circleStrokeW(c, col, x+s*0.5, y+s*0.5, s*0.12, 1.5)
		for i := 0; i < 8; i++ {
			a := float64(i) * math.Pi / 4
			cs, sn := math.Cos(a), math.Sin(a)
			u.thickLine(c, col,
				x+s*0.5+cs*s*0.29, y+s*0.5+sn*s*0.29,
				x+s*0.5+cs*s*0.42, y+s*0.5+sn*s*0.42, 1.8)
		}
	case "logs":
		for i := 0; i < 3; i++ {
			yy := y + s*0.18 + float64(i)*s*0.30
			u.circleFill(c, col, x+s*0.10, yy, s*0.055)
			u.line(c, col, x+s*0.28, yy, x+s*0.94, yy)
		}
	case "info":
		u.circleStrokeW(c, col, x+s*0.5, y+s*0.5, s*0.4, 1.6)
		u.circleFill(c, col, x+s*0.5, y+s*0.30, s*0.055)
		u.thickLine(c, col, x+s*0.5, y+s*0.44, x+s*0.5, y+s*0.72, 1.7)
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
		u.thickLine(c, col, x+s*0.18, y+s*0.34, x+s*0.50, y+s*0.66, 1.8)
		u.thickLine(c, col, x+s*0.50, y+s*0.66, x+s*0.82, y+s*0.34, 1.8)
	case "power":
		// Bold glyph: the thin 1px ring was invisible against the gradient.
		u.powerGlyph(c, x+s*0.5, y+s*0.5, s, col)
	case "globe":
		u.circleStrokeW(c, col, x+s*0.5, y+s*0.5, s*0.4, 1.6)
		u.thickLine(c, col, x+s*0.10, y+s*0.5, x+s*0.90, y+s*0.5, 1.6)
		u.circleStrokeW(c, col, x+s*0.5, y+s*0.5, s*0.18, 1.6)
	case "shield":
		// A real shield outline: straight shoulders and a pointed foot. The old
		// version (a rounded box with a tick) read as a checkbox at 22px, which
		// is the wrong signal for the protocol selector.
		u.thickLine(c, col, x+s*0.14, y+s*0.26, x+s*0.50, y+s*0.10, 1.7)
		u.thickLine(c, col, x+s*0.50, y+s*0.10, x+s*0.86, y+s*0.26, 1.7)
		u.thickLine(c, col, x+s*0.14, y+s*0.26, x+s*0.19, y+s*0.60, 1.7)
		u.thickLine(c, col, x+s*0.19, y+s*0.60, x+s*0.50, y+s*0.90, 1.7)
		u.thickLine(c, col, x+s*0.86, y+s*0.26, x+s*0.81, y+s*0.60, 1.7)
		u.thickLine(c, col, x+s*0.81, y+s*0.60, x+s*0.50, y+s*0.90, 1.7)
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
	case "clock":
		u.circleStrokeW(c, col, x+s*0.5, y+s*0.5, s*0.4, 1.6)
		u.thickLine(c, col, x+s*0.5, y+s*0.28, x+s*0.5, y+s*0.52, 1.6)
		u.thickLine(c, col, x+s*0.5, y+s*0.52, x+s*0.68, y+s*0.62, 1.6)
	case "up":
		u.thickLine(c, col, x+s*0.5, y+s*0.14, x+s*0.5, y+s*0.86, 1.8)
		u.thickLine(c, col, x+s*0.26, y+s*0.40, x+s*0.5, y+s*0.14, 1.8)
		u.thickLine(c, col, x+s*0.74, y+s*0.40, x+s*0.5, y+s*0.14, 1.8)
	case "down":
		u.thickLine(c, col, x+s*0.5, y+s*0.14, x+s*0.5, y+s*0.86, 1.8)
		u.thickLine(c, col, x+s*0.26, y+s*0.60, x+s*0.5, y+s*0.86, 1.8)
		u.thickLine(c, col, x+s*0.74, y+s*0.60, x+s*0.5, y+s*0.86, 1.8)
	case "copy":
		u.thickRound(c, col, u.r(x+s*0.34, y+s*0.34, s*0.52, s*0.52), 2, 1.6)
		u.thickRound(c, col, u.r(x+s*0.10, y+s*0.10, s*0.52, s*0.52), 2, 1.6)
	}
}

// thickLine is the straight line with a weighted stroke: the small icons read
// at 14px only when they are not a single hairline.
func (u *natUI) thickLine(c *walk.Canvas, col walk.Color, x1, y1, x2, y2, w float64) {
	p := u.penW(col, w)
	if p == nil {
		return
	}
	defer p.Dispose()
	_ = c.DrawLinePixels(p, u.p(x1, y1), u.p(x2, y2))
}

// thickRound is strokeRound with a weighted pen.
func (u *natUI) thickRound(c *walk.Canvas, col walk.Color, r walk.Rectangle, rad, w float64) {
	p := u.penW(col, w)
	if p == nil {
		return
	}
	defer p.Dispose()
	_ = c.DrawRoundedRectanglePixels(p, r, walk.Size{Width: u.s(rad), Height: u.s(rad)})
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
	u.cardPlain(c, x, y, w, h)
	if title == "" {
		return y + 12
	}
	u.txt(c, x+16, y+13, w-32, 20, title, u.f.f11b, th.text)
	return y + 40
}

// cardPlain is a card without a title (or with one drawn by the caller): a
// frosted blue panel from the palette, a bright 1px rim (glass edges catch the
// light, they are not outlined in grey), and a soft shadow on the page.
func (u *natUI) cardPlain(c *walk.Canvas, x, y, w, h float64) {
	u.shadowRound(c, u.r(x, y, w, h), cardRadius, shadowTint(), th.window)
	u.fillRound(c, th.card, u.r(x, y, w, h), cardRadius)
	rim := mix(th.card, rx(255, 255, 255), 0.55)
	if th.window.R() < 128 {
		rim = mix(th.card, rx(255, 255, 255), 0.08)
	}
	u.strokeRound(c, rim, u.r(x+0.5, y+0.5, w-1, h-1), cardRadius)
}

// shadowTint is the colour a card's shadow blends toward. Light: a slightly
// deeper tint of the blue canvas, so the shadow looks like the same material
// rather than grey dirt. Dark: black, or the "shadow" would turn into a glow.
func shadowTint() walk.Color {
	if th.window.R() < 128 {
		return mix(th.window, rx(0, 0, 0), 0.45)
	}
	return mix(th.window, rx(86, 108, 140), 0.30)
}

// vgrad fills a rectangle with a top-to-bottom colour blend, painted as
// horizontal bands. Used for the content canvas behind the cards.
func (u *natUI) vgrad(c *walk.Canvas, x, y, w, h float64, from, to walk.Color) {
	const steps = 48
	bh := h / float64(steps)
	for i := 0; i < steps; i++ {
		t := float64(i) / float64(steps-1)
		u.fillRect(c, mix(from, to, t), u.r(x, y+float64(i)*bh, w, bh+0.5))
	}
}

// vgradPx is vgrad in device pixels, for surfaces that are not part of the
// design grid (the sidebar strip and the canvas behind it run edge to edge at
// any window size, so they must not be letterboxed with the cards).
func (u *natUI) vgradPx(c *walk.Canvas, r walk.Rectangle, from, to walk.Color) {
	const steps = 64
	bh := float64(r.Height) / float64(steps)
	for i := 0; i < steps; i++ {
		t := float64(i) / float64(steps-1)
		u.fillRect(c, mix(from, to, t), walk.Rectangle{
			X:      r.X,
			Y:      r.Y + int(float64(i)*bh+0.5),
			Width:  r.Width,
			Height: int(bh + 1.5),
		})
	}
}

// rowToggle is a labelled switch with an optional second line.
func (u *natUI) rowToggle(c *walk.Canvas, x, y, w float64, id, label, sub string, on, enabled bool) {
	lh := 20.0
	if sub != "" {
		lh = 34
		u.txt(c, x+16, y+1, w-80, 17, label, u.f.f10, th.text)
		u.txt(c, x+16, y+19, w-80, 14, sub, u.f.f8, th.dim)
	} else {
		u.txt(c, x+16, y+(38-lh)/2, w-80, lh, label, u.f.f10, th.text)
	}
	u.switchAt(c, id, x+w-16-44, y+6, on, enabled)
	if enabled {
		u.addSpot(id, u.r(x, y, w, 38), true)
	}
}

// switchAt draws the pill switch itself.
func (u *natUI) switchAt(c *walk.Canvas, id string, x, y float64, on, enabled bool) {
	track := u.r(x, y, 44, 26)
	col := th.toggleOff
	if on {
		col = th.accent
	}
	if !enabled {
		col = mix(col, th.card, 0.55)
	}
	u.fillRound(c, col, track, 13)
	knobX := x + 3
	if on {
		knobX = x + 44 - 26
	}
	u.circleFill(c, th.card, knobX+10, y+13, 10)
}

// rowEdit is a label with an editable value box; clicking it opens the text
// dialog, which is the only way this window accepts typing.
func (u *natUI) rowEdit(c *walk.Canvas, x, y, w float64, id, label, value, placeholder string) {
	u.rowEditState(c, x, y, w, id, label, value, placeholder, true)
}

// rowEditState is rowEdit with an enable flag: a field whose value no preset
// can honour is drawn muted and takes no clicks rather than opening a dialog
// that silently changes nothing.
func (u *natUI) rowEditState(c *walk.Canvas, x, y, w float64, id, label, value, placeholder string, enabled bool) {
	lcol := th.text
	if !enabled {
		lcol = th.faint
	}
	u.txt(c, x+16, y+8, w-260, 20, label, u.f.f10, lcol)
	box := u.r(x+w-16-228, y+4, 228, 30)
	u.fillRound(c, th.input, box, ctrlRadius)
	if u.m.hover == id && enabled {
		u.strokeRound(c, th.accentLine, box, ctrlRadius)
	}
	disp, col := value, th.text
	if disp == "" {
		disp, col = placeholder, th.faint
	}
	if !enabled {
		disp, col = value, th.faint
		if disp == "" {
			disp = placeholder
		}
	}
	u.textL(c, disp, u.f.f10, col, u.r(x+w-16-228+10, y+4, 208, 30))
	if enabled {
		u.addSpot(id, box, true)
	}
}

// popupBelow places a dropdown popup under its anchor, flipping above when the
// list would run off the bottom of the window. One helper for every dropdown,
// so none of them can poke past the frame.
func popupBelow(ay, ah, ph float64) float64 {
	py := ay + ah + 6
	if py+ph > 686 {
		py = ay - 6 - ph
	}
	if py < 4 {
		py = 4
	}
	return py
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
	r := u.r(x, y, w, 28)
	bg, fg, line := th.chip, th.chipText, th.line
	if active {
		bg, fg, line = th.accentSoft, th.accent, th.accentLine
	} else if u.m.hover == id && enabled {
		bg = th.cardHover
	}
	if !enabled {
		fg = th.faint
	}
	u.fillRound(c, bg, r, ctrlRadius)
	if active {
		u.strokeRound(c, line, r, ctrlRadius)
	}
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
			w += 13.4
		case r >= 'A' && r <= 'Z':
			w += 7.4
		default:
			w += 6.8
		}
	}
	if w < 20 {
		w = 20
	}
	return w + 24
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
		// Still legible: on this palette a disabled button that fades to the
		// faint tint disappears entirely.
		bg, fg = mix(bg, th.card, 0.55), th.dim
	}
	u.fillRound(c, bg, r, ctrlRadius)
	if !primary || !enabled {
		u.strokeRound(c, line, r, ctrlRadius)
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
		u.fillRound(c, th.cardHover, r, 12)
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
	u.fillRound(c, th.input, r, ctrlRadius)
	if u.m.hover == id {
		u.strokeRound(c, th.accentLine, r, ctrlRadius)
	}
	disp, col := value, th.text
	if disp == "" {
		disp, col = placeholder, th.faint
	}
	u.textL(c, disp, u.f.f10, col, u.r(x+10, y, w-20, h))
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
		u.textC(c, labels[i], u.f.f12b, col, r)
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
	u.fillRound(c, iconBg, u.r(x+16, y+6, 26, 26), 13)
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
	u.fillRound(c, th.input, u.r(x, y, w, h), 7)
	u.drawFlag(c, x+2, y+2, w-4, h-4, cc)
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
