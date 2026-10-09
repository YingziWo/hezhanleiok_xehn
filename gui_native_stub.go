//go:build !windows

package main

import (
	"context"
	"errors"
)

// guiNativeAvailable reports whether this build has a native window front-end.
const guiNativeAvailable = false

// runNativeGUI is unavailable off-Windows; runGUICmd falls back to the web
// dashboard there.
func runNativeGUI(_ context.Context, _ options) error {
	return errors.New("native GUI is only available on Windows")
}
