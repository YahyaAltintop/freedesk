package config

import "testing"

func TestParseMaxWidth(t *testing.T) {
	cases := []struct {
		raw  string
		want int
		ok   bool
	}{
		{"", 0, true}, // unset: the capture package's default
		{"  1280 ", 1280, true},
		{"320", 320, true},
		{"319", 0, false}, // too narrow to read anything
		{"0", 0, false},
		{"-1920", 0, false},
		{"wide", 0, false},
		{"1920.5", 0, false},
	}
	for _, c := range cases {
		got, err := parseMaxWidth(c.raw)
		if c.ok != (err == nil) || got != c.want {
			t.Errorf("parseMaxWidth(%q) = %d, %v; want %d, ok=%v", c.raw, got, err, c.want, c.ok)
		}
	}
}
