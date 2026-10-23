//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/lxn/walk"
)

// uiPalette is every colour the dashboard paints. One struct, two values: a new
// colour is added here and filled in twice, so light and dark cannot drift
// apart, and no paint site invents a colour of its own. (The CLI's own colour
// helper is a different type — see palette in ui.go.)
//
// The light palette is the reference mock's "frosted glass" look: a pale blue
// wash for the canvas, near-white surfaces, and text in a soft blue-grey. There
// is deliberately no dark text and no saturated accent fill — the window reads
// as one continuous light material, and colour is only used for state.
type uiPalette struct {
	// Surfaces, from the window down to the smallest control.
	window, sidebar, card, cardHover, line, input walk.Color
	// Text: primary, secondary, and the faintest (hints, disabled).
	text, dim, faint walk.Color
	// Accent and status.
	accent, accentText, accentSoft, accentLine walk.Color
	ok, okSoft, warn, warnSoft, bad, badSoft   walk.Color
	// Controls. Logs deliberately share the same card language as the rest of
	// the client rather than looking like a separate terminal window.
	chip, chipText, toggleOff walk.Color
	// The connect button's fill, top to bottom.
	connectFrom, connectTo walk.Color
}

// The light palette is sampled from the reference: nothing on this page is
// pure white. The page is a blue-grey wash, the panels are a *pale blue*
// translucent glass (not white), and the type is a slate blue that stays
// readable on it. "Card = #FFFFFF" is exactly what made the old build look
// bleached next to the mock.
var paletteLight = uiPalette{
	window:      rx(224, 236, 250), // #E0ECFA: the wash the banks sit on
	sidebar:     rx(245, 250, 255), // veil over the wash
	card:        rx(243, 248, 254), // #F3F8FE: same material as the disc and the pill
	cardHover:   rx(250, 253, 255),
	line:        rx(220, 231, 245),
	input:       rx(233, 241, 252),
	text:        rx(93, 114, 146),  // #5D7292 slate blue: values, titles
	dim:         rx(147, 163, 190), // #93A3BE labels
	faint:       rx(183, 196, 216), // #B7C4D8 hints
	accent:      rx(110, 155, 214), // #6E9BD6 periwinkle icons
	accentText:  rx(255, 255, 255),
	accentSoft:  rx(219, 232, 249), // badge / chip fill
	accentLine:  rx(200, 217, 240),
	ok:          rx(95, 185, 143), // mint: download
	okSoft:      rx(216, 240, 229),
	warn:        rx(232, 170, 82),
	warnSoft:    rx(252, 241, 224),
	bad:         rx(224, 108, 108),
	badSoft:     rx(250, 232, 232),
	chip:        rx(232, 241, 252),
	chipText:    rx(132, 150, 180),
	toggleOff:   rx(205, 219, 236),
	connectFrom: rx(255, 255, 255), // the disc: frosted white, cooling downward
	connectTo:   rx(228, 240, 253),
}

var paletteDark = uiPalette{
	window:     rx(20, 24, 31),
	sidebar:    rx(25, 30, 38),
	card:       rx(32, 38, 48),
	cardHover:  rx(40, 47, 58),
	line:       rx(45, 53, 65),
	input:      rx(40, 47, 58),
	text:       rx(196, 209, 227),
	dim:        rx(146, 161, 183),
	faint:      rx(105, 121, 143),
	accent:     rx(122, 168, 232),
	accentText: rx(18, 23, 30),
	accentSoft: rx(30, 43, 60),
	accentLine: rx(42, 58, 78),
	ok:         rx(95, 185, 143),
	okSoft:     rx(26, 44, 36),
	warn:       rx(232, 170, 82),
	warnSoft:   rx(52, 42, 24),
	bad:        rx(224, 108, 108),
	badSoft:    rx(52, 32, 34),
	chip:       rx(37, 43, 53),
	chipText:   rx(146, 161, 183),
	toggleOff:  rx(56, 65, 78),
	connectFrom: rx(36, 42, 52),
	connectTo:   rx(26, 31, 39),
}

func rx(r, g, b byte) walk.Color { return walk.RGB(r, g, b) }

// th is the active palette. The paint code reads it on every frame, so the
// theme is a global rather than a parameter; applyTheme is its only writer.
var th = paletteLight

// Shape constants. They live beside the palette because they are part of the
// same visual language: one radius per surface class, no paint site inventing a
// rounder corner than its neighbours.
const (
	cardRadius  = 16.0 // content cards and the large popups
	panelRadius = 20.0 // the protocol selector, popups
	ctrlRadius  = 10.0 // buttons, inputs, chips
)

// pillRad is the fully-round radius for a control of the given height: GDI+'s
// rounded rectangle takes a radius, not a "make it a pill" flag, and handing it
// half the height is how a pill is drawn without measuring twice.
func pillRad(h float64) float64 { return h / 2 }

// navRadius is the sidebar selection's corner radius: a soft pill, the way the
// mock highlights the active rail item.
const navRadius = 12.0

// darkNow reports whether the UI is currently dark. ThemeAuto defers to the
// system's app-theme preference; anything else is explicit.
func darkNow(set clientSettings) bool {
	switch set.Theme {
	case themeDark:
		return true
	case themeAuto:
		return systemDark()
	default:
		return false
	}
}

// systemDark reads Windows' "app mode" preference. A missing value (older
// builds, locked-down profile) means light, which is also the app default.
func systemDark() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return false
	}
	return v == 0
}

// applyTheme makes the palette match the settings and repaints. The dark title
// bar is set separately because it lives on the window, not on the canvas.
func (u *natUI) applyTheme() {
	if darkNow(u.m.set) {
		th = paletteDark
	} else {
		th = paletteLight
	}
	setDarkTitleBar(u.mw, darkNow(u.m.set))
	u.setFrameColor()
	u.invalidate()
}

// setDarkTitleBar switches the non-client area between the light and dark
// title bars. DWMWA_USE_IMMERSIVE_DARK_MODE is 20 on current builds and 19 on
// the first ones that shipped it; both are tried, and a failure is ignored
// (the window simply keeps the default bar).
func setDarkTitleBar(mw *walk.MainWindow, dark bool) {
	if mw == nil {
		return
	}
	dwm := windows.NewLazySystemDLL("dwmapi.dll")
	proc := dwm.NewProc("DwmSetWindowAttribute")
	if err := proc.Find(); err != nil {
		return
	}
	v := int32(0)
	if dark {
		v = 1
	}
	for _, attr := range []uintptr{20, 19} {
		r, _, _ := proc.Call(uintptr(mw.Handle()), attr, uintptr(unsafe.Pointer(&v)), 4)
		if r == 0 {
			return
		}
	}
}
