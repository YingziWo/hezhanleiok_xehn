//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"
)

// restartDetached starts the freshly installed program once this process has
// exited. The delay (about five seconds) is what makes it work: the new instance must not race
// the old one for the mixed port and the health-owner file. CREATE_NO_WINDOW
// keeps the helper from flashing a console window.
func restartDetached(exe string) error {
	script := fmt.Sprintf(`ping -n 6 127.0.0.1 >nul & start "" "%s"`, exe)
	cmd := exec.Command("cmd", "/c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd.Start()
}
