//go:build windows

package main

import (
	"fmt"
	"strings"
	"testing"
)

// TestLogCopyKeepsEveryLine pins the copy contract: the panel may show one
// screenful, but the clipboard text must contain the whole session, including
// lines far older than the old 400-entry cap and the level/core tags.
func TestLogCopyKeepsEveryLine(t *testing.T) {
	u := &natUI{m: newNatModel()}
	for i := 0; i < 1200; i++ {
		u.appendLog(fmt.Sprintf("line %d", i), 0, false, false)
	}
	u.addLogFull("core error", 2, true)

	if got := len(u.m.logs); got != 1201 {
		t.Fatalf("retained lines = %d, want 1201", got)
	}
	text := u.logText()
	if !strings.Contains(text, "line 0\r\n") || !strings.Contains(text, "line 1199\r\n") {
		t.Fatalf("logText dropped old lines")
	}
	if !strings.Contains(text, "ERR  core error\r\n") {
		t.Fatalf("logText lacks the ERR tag: %q", text[len(text)-40:])
	}
	if got := strings.Count(text, "\r\n"); got != 1201 {
		t.Fatalf("copied lines = %d, want 1201", got)
	}
}
