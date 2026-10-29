package core

import (
	"errors"
	"strings"
	"testing"
)

// The crash code decides what the user is told, so the mapping is pinned: a
// generic "内核启动失败" sends people to the wrong fix.
func TestCrashCodeNames(t *testing.T) {
	cases := map[uint32]string{
		0xC0000005: "访问冲突",
		0xC000001D: "非法指令",
		0xC0000135: "缺少 DLL",
		0xC0000142: "DLL 初始化失败",
		0xC0000409: "栈保护",
		0xC0000094: "0xC0000094",
	}
	for code, want := range cases {
		if got := CrashCodeName(code); !strings.Contains(got, want) {
			t.Errorf("CrashCodeName(%#x) = %q, want it to mention %q", code, got, want)
		}
	}
}

func TestCrashErrorCarriesAdvice(t *testing.T) {
	err := &CrashError{Code: 0xC0000005}
	var target *CrashError
	if !errors.As(error(err), &target) {
		t.Fatal("CrashError must survive errors.As")
	}
	if !strings.Contains(err.Error(), "0xC0000005") {
		t.Fatalf("message should name the code: %q", err.Error())
	}
	if !strings.Contains(err.Hint(), "白名单") {
		t.Fatalf("hint should mention the allowlist: %q", err.Hint())
	}
}

// A normal exit code must never be reported as a crash.
func TestCrashCodeOnlyForExceptions(t *testing.T) {
	c := &Core{}
	if _, ok := c.CrashCode(); ok {
		t.Fatal("a core that never ran has no crash code")
	}
	c.exitKnown = true
	c.exitCode = 0
	if _, ok := c.CrashCode(); ok {
		t.Fatal("a clean exit is not a crash")
	}
	c.exitCode = -1073741819 // 0xC0000005 as a signed int
	if code, ok := c.CrashCode(); !ok || code != 0xC0000005 {
		t.Fatalf("CrashCode = %#x, %v", code, ok)
	}
}
