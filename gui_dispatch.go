package main

import (
	"context"
)

// runGUICmd is the `gui` command (and the bare double-click) entry point: the
// native Windows window. Builds without a native front-end report that
// instead of silently doing nothing.
func runGUICmd(ctx context.Context, opts options) error {
	return runNativeGUI(ctx, opts)
}
