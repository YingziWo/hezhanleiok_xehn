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
type uiPalette struct {
	// Surfaces, from the window down to the smallest control.
	window, sidebar, card, cardHover, line, input walk.Color
	// Text: primary, secondary, and the faintest (hints, disabled).
	text, dim, faint walk.Color
	// Accent and status.
	accent, accentText, accentSoft, accentLine walk.Color
	ok, okSoft, warn, warnSoft, bad, badSoft   walk.Color
	// Controls and the log console.
	chip, chipText, toggleOff, logBg, logText walk.Color
	// The connect button's fill, top to bottom.
	connectFrom, connectTo walk.Color
}

var paletteLight = uiPalette{
	window:      rx(244, 246, 250),
	sidebar:     rx(255, 255, 255),
	card:        rx(255, 255, 255),
	cardHover:   rx(246, 248, 252),
	line:        rx(231, 235, 242),
	input:       rx(243, 246, 250),
	text:        rx(32, 38, 52),
	dim:         rx(136, 145, 163),
	faint:       rx(178, 185, 198),
	accent:      rx(61, 99, 245),
	accentText:  rx(255, 255, 255),
	accentSoft:  rx(238, 242, 255),
	accentLine:  rx(198, 212, 255),
	ok:          rx(34, 182, 92),
	okSoft:      rx(226, 247, 235),
	warn:        rx(232, 150, 20),
	warnSoft:    rx(254, 244, 224),
	bad:         rx(230, 68, 68),
	badSoft:     rx(254, 235, 235),
	chip:        rx(242, 245, 250),
	chipText:    rx(95, 105, 124),
	toggleOff:   rx(214, 220, 231),
	logBg:       rx(17, 22, 34),
	logText:     rx(206, 215, 229),
	connectFrom: rx(79, 118, 255),
	connectTo:   rx(56, 86, 232),
}

var paletteDark = uiPalette{
	window:      rx(15, 19, 30),
	sidebar:     rx(20, 25, 38),
	card:        rx(26, 32, 47),
	cardHover:   rx(33, 40, 58),
	line:        rx(38, 45, 63),
	input:       rx(22, 28, 42),
	text:        rx(232, 236, 244),
	dim:         rx(139, 148, 167),
	faint:       rx(104, 113, 132),
	accent:      rx(88, 126, 250),
	accentText:  rx(255, 255, 255),
	accentSoft:  rx(31, 41, 68),
	accentLine:  rx(54, 72, 120),
	ok:          rx(52, 199, 123),
	okSoft:      rx(24, 48, 38),
	warn:        rx(240, 171, 54),
	warnSoft:    rx(54, 44, 22),
	bad:         rx(240, 84, 84),
	badSoft:     rx(56, 30, 32),
	chip:        rx(32, 39, 56),
	chipText:    rx(150, 159, 178),
	toggleOff:   rx(52, 60, 80),
	logBg:       rx(10, 13, 21),
	logText:     rx(198, 208, 224),
	connectFrom: rx(70, 105, 240),
	connectTo:   rx(52, 80, 214),
}

func rx(r, g, b byte) walk.Color { return walk.RGB(r, g, b) }

// th is the active palette. The paint code reads it on every frame, so the
// theme is a global rather than a parameter; applyTheme is its only writer.
var th = paletteLight

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
