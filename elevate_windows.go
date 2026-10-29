//go:build windows

package main

import (
	"fmt"
	"os"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// relaunchElevated starts a second copy of this program behind a UAC prompt.
// TUN capture is the only feature that needs administrator rights, and asking
// the user to close the window, find the exe and pick "run as administrator"
// is the kind of friction that makes people think the client is broken.
//
// The caller quits right after: the elevated copy takes over the session, and
// two clients on one mixed port would fight.
func relaunchElevated() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	if !win.ShellExecute(0, verb, file, nil, nil, win.SW_SHOWNORMAL) {
		return fmt.Errorf("UAC 被取消或无法启动")
	}
	return nil
}
