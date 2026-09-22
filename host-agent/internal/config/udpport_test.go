package config

import "testing"

func TestParseUDPPort(t *testing.T) {
	cases := []struct {
		raw  string
		want int
		ok   bool
	}{
		{"", defaultUDPPort, true},
		{"0", 0, true}, // any free port
		{" 50000 ", 50000, true},
		{"65535", 65535, true},
		{"65536", 0, false},
		{"-1", 0, false},
		{"port", 0, false},
	}
	for _, c := range cases {
		got, err := parseUDPPort(c.raw)
		if c.ok != (err == nil) || got != c.want {
			t.Errorf("parseUDPPort(%q) = %d, %v; want %d, ok=%v", c.raw, got, err, c.want, c.ok)
		}
	}
}
