//go:build windows

package main

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// machineSeed is the stable identity a control code is derived from. It is the
// Windows install's MachineGuid: readable without elevation, unchanged by
// moving the program folder, and — the point of the exercise — unaffected by
// deleting settings.json, so a stopped machine cannot mint itself a new code by
// wiping its own files.
func machineSeed() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Cryptography`,
		registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return ""
	}
	defer k.Close()
	guid, _, err := k.GetStringValue("MachineGuid")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(guid)
}
