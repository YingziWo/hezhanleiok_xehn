package main

import (
	"strings"
	"testing"
	"time"
)

// The code is read aloud, retyped and pasted into an email, so its shape is
// part of the contract: four groups of four, upper case, no look-alike
// characters.
func TestNewControlCodeShape(t *testing.T) {
	seen := make(map[string]bool, 64)
	for i := 0; i < 64; i++ {
		code := newControlCode()
		if len(code) != 19 {
			t.Fatalf("code %q: want 19 chars (16 + 3 dashes), got %d", code, len(code))
		}
		groups := strings.Split(code, "-")
		if len(groups) != 4 {
			t.Fatalf("code %q: want 4 groups, got %d", code, len(groups))
		}
		for _, g := range groups {
			if len(g) != 4 {
				t.Fatalf("code %q: group %q is not 4 chars", code, g)
			}
			for _, r := range g {
				if !strings.ContainsRune(controlAlphabet, r) {
					t.Fatalf("code %q: %q is outside the alphabet", code, r)
				}
				switch r {
				case 'O', '0', 'I', '1', 'L':
					t.Fatalf("code %q: %q is a look-alike character", code, r)
				}
			}
		}
		if seen[code] {
			t.Fatalf("code %q repeated within 64 draws", code)
		}
		seen[code] = true
	}
}

func TestNormalizeControlCode(t *testing.T) {
	const want = "ABCD-EFGH-JKMN-PQRS"
	cases := []string{
		"ABCD-EFGH-JKMN-PQRS",
		"abcd-efgh-jkmn-pqrs",
		"abcd efgh jkmn pqrs",
		"  ABCD EFGH JKMN PQRS  ",
		"ABCD_EFGH_JKMN_PQRS",
		"ABCDEFGHJKMNPQRS",
		"ABCD-EFGH-JKMN-PQRS-XYZ",
	}
	for _, in := range cases {
		if got := normalizeControlCode(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
	if got := normalizeControlCode(""); got != "" {
		t.Errorf("normalize(\"\") = %q, want empty", got)
	}
}

func TestEnsureControlCode(t *testing.T) {
	var s clientSettings
	if !s.ensureControlCode() {
		t.Fatal("empty settings: expected a code")
	}
	first := s.ControlCode
	if first == "" {
		t.Fatal("no code was produced")
	}
	if normalizeControlCode(first) != first {
		t.Fatalf("code %q is not normalised", first)
	}
	if s.ensureControlCode() {
		t.Fatal("existing code: expected no change")
	}
	if s.ControlCode != first {
		t.Fatalf("code changed on second call: %q -> %q", first, s.ControlCode)
	}

	// A file carrying another code must not win over the machine's own: the
	// stop list matches machines, and a copied settings.json would otherwise
	// let one install answer as another.
	s.ControlCode = "ZZZZ-ZZZZ-ZZZZ-ZZZZ"
	if machineControlCode() != "" {
		if !s.ensureControlCode() {
			t.Fatal("expected the machine code to replace the file's")
		}
		if s.ControlCode == "ZZZZ-ZZZZ-ZZZZ-ZZZZ" {
			t.Fatal("the file's code survived")
		}
		return
	}

	// No machine identity (non-Windows, or an unreadable registry): the file's
	// code is folded into the one spelling and kept.
	s.ControlCode = "  abcd efgh jkmn pqrs "
	if !s.ensureControlCode() {
		t.Fatal("un-normalised code: expected a rewrite")
	}
	if s.ControlCode != "ABCD-EFGH-JKMN-PQRS" {
		t.Fatalf("got %q, want the grouped upper-case form", s.ControlCode)
	}
}

// TestMachineControlCodeStable pins the property the stop list depends on: the
// same machine derives the same code on every run.
func TestMachineControlCodeStable(t *testing.T) {
	first := machineControlCode()
	if first == "" {
		t.Skip("no machine identity on this platform")
	}
	if len(first) != 19 || normalizeControlCode(first) != first {
		t.Fatalf("derived code %q is not in the XXXX-XXXX-XXXX-XXXX form", first)
	}
	if again := machineControlCode(); again != first {
		t.Fatalf("code is not stable: %q then %q", first, again)
	}
}

func TestContainsControlCode(t *testing.T) {
	list := []string{"abcd-efgh-jkmn-pqrs", "ZZZZ-ZZZZ-ZZZZ-ZZZZ"}
	if !containsControlCode(list, "ABCD-EFGH-JKMN-PQRS") {
		t.Fatal("expected a match across formatting differences")
	}
	if containsControlCode(list, "ABCD-EFGH-JKMN-PQRT") {
		t.Fatal("unexpected match for a different code")
	}
	// An install with no code must never be stopped by a list entry that is
	// itself empty (a hand-edited file with a stray "").
	if containsControlCode([]string{""}, "") {
		t.Fatal("empty code must never match")
	}
}

func TestApplyControlList(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.Local)
	code := "ABCD-EFGH-JKMN-PQRS"

	t.Run("not listed", func(t *testing.T) {
		st := applyControlList(&controlList{Codes: []string{"ZZZZ-ZZZZ-ZZZZ-ZZZZ"}}, code, controlState{}, now)
		if st.Stopped {
			t.Fatal("code is not listed but was stopped")
		}
		if !st.Reached {
			t.Fatal("a successful fetch must mark the state reached")
		}
		if st.Message != "" {
			t.Fatalf("an active code carries no message, got %q", st.Message)
		}
	})

	t.Run("listed", func(t *testing.T) {
		st := applyControlList(&controlList{Codes: []string{code}}, code, controlState{}, now)
		if !st.Stopped {
			t.Fatal("listed code was not stopped")
		}
		if st.Message != defaultStopMessage {
			t.Fatalf("want the default message, got %q", st.Message)
		}
	})

	t.Run("stop all with custom message", func(t *testing.T) {
		st := applyControlList(&controlList{StopAll: true, Message: "  停止分发  "}, code, controlState{}, now)
		if !st.Stopped {
			t.Fatal("stop_all did not stop the client")
		}
		if st.Message != "停止分发" {
			t.Fatalf("custom message not trimmed/applied: %q", st.Message)
		}
	})

	t.Run("recovers when the code leaves the list", func(t *testing.T) {
		prev := controlState{Code: code, Stopped: true, Message: defaultStopMessage}
		st := applyControlList(&controlList{}, code, prev, now)
		if st.Stopped {
			t.Fatal("client stayed stopped after leaving the list")
		}
		if st.Message != "" {
			t.Fatalf("stale message survived: %q", st.Message)
		}
	})
}
