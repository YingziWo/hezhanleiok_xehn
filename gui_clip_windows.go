//go:build windows

package main

import (
	"github.com/lxn/win"
	"github.com/lxn/walk"
)

// clipCanvas restricts painting to r for the duration of fn. walk has no
// clipping API, but the canvas is a plain memory HDC, so the GDI clip stack
// does the job: the settings page scrolls its cards under a fixed header, and
// without this the top card would paint over the title (and over the caption
// band) as it slides up.
func (u *natUI) clipCanvas(c *walk.Canvas, r walk.Rectangle, fn func()) {
	hdc := c.HDC()
	saved := win.SaveDC(hdc)
	win.IntersectClipRect(hdc, int32(r.X), int32(r.Y), int32(r.X+r.Width), int32(r.Y+r.Height))
	fn()
	win.RestoreDC(hdc, saved)
}

// clipSpots trims every spot registered since start to the painted band and
// drops the ones that fall outside it. Without this a card row scrolled above
// the settings body would still take clicks over the page header.
func (u *natUI) clipSpots(start int, r walk.Rectangle) {
	kept := u.sp[:start]
	for _, s := range u.sp[start:] {
		x1, y1 := s.r.X, s.r.Y
		x2, y2 := x1+s.r.Width, y1+s.r.Height
		if x1 < r.X {
			x1 = r.X
		}
		if y1 < r.Y {
			y1 = r.Y
		}
		if x2 > r.X+r.Width {
			x2 = r.X + r.Width
		}
		if y2 > r.Y+r.Height {
			y2 = r.Y + r.Height
		}
		if x2 <= x1 || y2 <= y1 {
			continue
		}
		s.r = walk.Rectangle{X: x1, Y: y1, Width: x2 - x1, Height: y2 - y1}
		kept = append(kept, s)
	}
	u.sp = kept
}
