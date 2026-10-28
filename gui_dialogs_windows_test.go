//go:build windows

package main

import "testing"

// TestNormalizeFieldInput pins the IME fold: a Chinese IME types full-width
// digits that look identical to ASCII ones, and every numeric parser in the
// settings page would reject them with "must be a number" — which reads as
// nonsense to someone who just typed a number.
func TestNormalizeFieldInput(t *testing.T) {
	cases := []struct{ in, want string }{
		{"5200", "5200"},
		{"  5200  ", "5200"},
		{"５２００", "5200"},           // full-width digits
		{"７８９０", "7890"},           // full-width digits
		{"２２３．５．５．５", "223.5.5.5"}, // full-width digits + dots
		{"223.5.5.5，1.1.1.1", "223.5.5.5,1.1.1.1"}, // full-width comma
		{"1.1.1.1、8.8.8.8", "1.1.1.1,8.8.8.8"},     // enumeration comma
		{"", ""},
		{"   ", ""},
		{"example.com", "example.com"},
	}
	for _, c := range cases {
		if got := normalizeFieldInput(c.in); got != c.want {
			t.Errorf("normalizeFieldInput(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
