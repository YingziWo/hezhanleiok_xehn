//go:build !windows

package main

import "os/exec"

// restartDetached is the portable fallback: start the new program and let the
// caller exit. There is no cmd.exe to delay it, so it may briefly share the
// port with the old process.
func restartDetached(exe string) error {
	return exec.Command(exe).Start()
}
