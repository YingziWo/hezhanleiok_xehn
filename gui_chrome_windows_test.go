//go:build windows

package main

import (
	"math"
	"testing"
)

func TestDefaultClientSize(t *testing.T) {
	cases := []struct {
		name                 string
		dpi                  float64
		workW, workH, fw, fh int32
		wantW, wantH         int32
	}{
		// Roomy 100% screen: the design grid in device pixels.
		{"96dpi", 96, 1920, 1040, 16, 16, 1080, 734},
		// Same grid at 150%, still under the 90% cap.
		{"144dpi", 144, 2560, 1504, 24, 24, 1620, 1101},
		// 200%: uncapped would be 2160x1468, but the work area only allows
		// 90% of its height, and both axes must shrink together.
		{"192dpi-capped", 192, 2560, 1504, 24, 24, 1960, 1332},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, h := defaultClientSize(c.dpi, c.workW, c.workH, c.fw, c.fh)
			if w != c.wantW || h != c.wantH {
				t.Fatalf("client = %dx%d, want %dx%d", w, h, c.wantW, c.wantH)
			}
			assertDesignAspect(t, w, h)
		})
	}
}

func TestDefaultClientSizeSmallWorkArea(t *testing.T) {
	// 1920x1080 at 200%: the DPI-scaled design (2160x1468) does not fit, so
	// the startup window must shrink uniformly instead of spilling off screen.
	w, h := defaultClientSize(192, 1920, 1080, 24, 24)
	if w+24 > 1920 || h+24 > 1080 {
		t.Fatalf("client %dx%d with frame overflows work area", w, h)
	}
	assertDesignAspect(t, w, h)
}

func TestAspectLockRect(t *testing.T) {
	const fw, fh int32 = 24, 24
	cases := []struct {
		name string
		edge uintptr
		in   winRect
		want winRect
	}{
		// WMSZ_RIGHT: width drives, top stays, bottom moves.
		{"right", 2,
			winRect{0, 100, 1200, 1000},
			winRect{0, 100, 1200, 923}},
		// WMSZ_LEFT: right stays, height follows width from the top.
		{"left", 1,
			winRect{100, 100, 1200, 900},
			winRect{100, 100, 1200, 855}},
		// WMSZ_TOP / WMSZ_BOTTOM: height drives, width grows to the right.
		{"top", 3,
			winRect{200, 100, 1400, 1000},
			winRect{200, 100, 1513, 1000}},
		{"bottom", 6,
			winRect{200, 100, 1400, 1000},
			winRect{200, 100, 1513, 1000}},
		// Corners: width drives; the edge opposite the drag stays anchored.
		{"top-right", 5,
			winRect{100, 50, 1200, 1000},
			winRect{100, 245, 1200, 1000}},
		{"bottom-left", 7,
			winRect{100, 50, 1200, 1000},
			winRect{100, 50, 1200, 805}},
		{"bottom-right", 8,
			winRect{0, 0, 1200, 1000},
			winRect{0, 0, 1200, 823}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := c.in
			aspectLockRect(c.edge, &r, fw, fh)
			if r != c.want {
				t.Fatalf("rect = %+v, want %+v", r, c.want)
			}
			assertDesignAspect(t, r.right-r.left-fw, r.bottom-r.top-fh)
		})
	}
}

func assertDesignAspect(t *testing.T, w, h int32) {
	t.Helper()
	if h <= 0 {
		t.Fatalf("client %dx%d has no height", w, h)
	}
	if got := float64(w) / float64(h); math.Abs(got-designW/designH) > 0.002 {
		t.Fatalf("client %dx%d has ratio %.5f, want %.5f", w, h, got, designW/designH)
	}
}
