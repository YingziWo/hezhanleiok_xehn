//go:build windows

package main

import "testing"

// The settings scrollbar is the only way to move that page besides the wheel,
// so its geometry and its mapping from pointer position to scroll offset have
// to hold at both ends of the track.
func TestScrollbarDrag(t *testing.T) {
	u := &natUI{m: newNatModel(), k: 1, dw: 1080}
	u.m.setScrollMax = 600

	trackTop, trackH, thumbTop, thumbH := u.scrollbarGeom()
	if trackH <= 0 || thumbH <= 0 || thumbH >= trackH {
		t.Fatalf("bad geometry: trackH=%v thumbH=%v", trackH, thumbH)
	}
	if u.m.setScroll != 0 || thumbTop != trackTop {
		t.Fatalf("at offset 0 the thumb must sit at the track top: %v vs %v", thumbTop, trackTop)
	}

	// Grab the handle and drag it to the bottom of the track: the page must
	// reach its maximum, not stop short.
	u.beginScrollDrag(u.sy(thumbTop + 2))
	if !u.scrollDrag {
		t.Fatal("drag did not start")
	}
	u.dragScrollTo(u.sy(trackTop + trackH))
	if u.m.setScroll != u.m.setScrollMax {
		t.Fatalf("drag to bottom = %d, want %d", u.m.setScroll, u.m.setScrollMax)
	}

	// And back to the top.
	u.dragScrollTo(u.sy(trackTop - 100))
	if u.m.setScroll != 0 {
		t.Fatalf("drag past the top = %d, want 0", u.m.setScroll)
	}

	// Clicking the middle of the track jumps the handle there, i.e. about half
	// the range.
	u.beginScrollDrag(u.sy(trackTop + trackH/2))
	half := u.m.setScrollMax / 2
	if d := u.m.setScroll - half; d > 20 || d < -20 {
		t.Fatalf("mid-track click = %d, want about %d", u.m.setScroll, half)
	}

	// A page that fits has nothing to drag.
	u.scrollDrag = false
	u.m.setScrollMax = 0
	u.beginScrollDrag(u.sy(trackTop + 10))
	if u.scrollDrag {
		t.Fatal("drag must not start when the page fits")
	}
}
