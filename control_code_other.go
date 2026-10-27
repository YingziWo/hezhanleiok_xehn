//go:build !windows

package main

// machineSeed has no portable equivalent, so a non-Windows build falls back to
// a random code that is persisted in the settings file. The GUI is Windows-only
// anyway; this keeps the package buildable everywhere.
func machineSeed() string { return "" }
