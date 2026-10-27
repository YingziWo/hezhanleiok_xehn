//go:build windows

package main

import "testing"

// TestAboutLayoutFitsBody pins the About page's vertical budget. The page does
// not scroll, so every card and both footer lines must end above the body's
// bottom edge: a clipped footer would hide the licence sentence the page exists
// to carry, and a card that overruns its row would paint over the next one.
func TestAboutLayoutFitsBody(t *testing.T) {
	row1, row2, row3, footer := aboutLayout(shellBodyY)

	if row1 != shellBodyY {
		t.Fatalf("first row starts at %v, want the body top %v", row1, shellBodyY)
	}
	if want := row1 + aboutRow1H + aboutGap; row2 != want {
		t.Fatalf("row 2 starts at %v, want %v", row2, want)
	}
	if want := row2 + aboutRow2H + aboutGap; row3 != want {
		t.Fatalf("row 3 starts at %v, want %v", row3, want)
	}
	if want := row3 + aboutRow3H + aboutFootG; footer != want {
		t.Fatalf("footer starts at %v, want %v", footer, want)
	}

	bodyBottom := shellBodyY + shellBodyH
	// Footer: two lines, 18px each, the second one starting 20px lower.
	if bottom := footer + 20 + 18; bottom > bodyBottom {
		t.Fatalf("footer ends at %v, past the body bottom %v", bottom, bodyBottom)
	}

	// The link card's five rows: 22px apart from the first row's top, each 20
	// tall. The last row must stay inside the card.
	linkLast := row3 + 40 + 4*22 + 20
	if linkLast > row3+aboutRow3H {
		t.Fatalf("last link row ends at %v, card ends at %v", linkLast, row3+aboutRow3H)
	}

	// Buttons sit 34 tall at +146 / +140 from their row's top.
	if bottom := row1 + 146 + 34; bottom > row1+aboutRow1H {
		t.Fatalf("row 1 buttons end at %v, card ends at %v", bottom, row1+aboutRow1H)
	}
	if bottom := row2 + 140 + 34; bottom > row2+aboutRow2H {
		t.Fatalf("row 2 buttons end at %v, card ends at %v", bottom, row2+aboutRow2H)
	}
}

// TestAboutColumnsFitWidth checks the two-column rows at both the default grid
// and a wide window: half + gap + half has to stay inside the content width, or
// the right-hand card would be drawn off the page.
func TestAboutColumnsFitWidth(t *testing.T) {
	for _, w := range []float64{858, 1200, 1600} {
		half := (w - aboutGap) / 2
		if total := half*2 + aboutGap; total > w {
			t.Fatalf("width %v: columns span %v", w, total)
		}
		// Buttons: 16 margin + two buttons + 10 between them + 16 margin.
		bw := (half - 16 - 16 - 10) / 2
		if total := 16 + bw + 10 + bw + 16; total > half {
			t.Fatalf("width %v: buttons span %v in a %v card", w, total, half)
		}
		if bw < 80 {
			t.Fatalf("width %v: button %v is too narrow for its label", w, bw)
		}
	}
}

// TestAboutLinksAreGitHub pins the rule this page was rewritten for: no local
// path, every row a GitHub address.
func TestAboutLinksAreGitHub(t *testing.T) {
	links := aboutLinks()
	if len(links) == 0 {
		t.Fatal("no links")
	}
	for _, ln := range links {
		if ln.id == "" || ln.label == "" {
			t.Fatalf("incomplete link: %+v", ln)
		}
		if got := shortURL(ln.url); len(got) < len("github.com/") || got[:len("github.com/")] != "github.com/" {
			t.Fatalf("%s: %q is not a github.com address", ln.id, ln.url)
		}
	}
}
