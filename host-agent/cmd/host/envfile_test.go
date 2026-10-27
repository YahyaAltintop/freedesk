package main

import (
	"path/filepath"
	"testing"
)

func TestAppBundleParent(t *testing.T) {
	root := string(filepath.Separator)
	inside := func(elem ...string) string { return filepath.Join(append([]string{root}, elem...)...) }
	cases := []struct {
		exe  string
		want string // "" for an exe outside any app bundle
	}{
		{inside("Applications", "FreeDesk.app", "Contents", "MacOS", "freedesk"), inside("Applications")},
		{inside("Users", "a", "Downloads", "FreeDesk.app", "Contents", "MacOS", "freedesk"), inside("Users", "a", "Downloads")},
		{inside("Users", "a", "src", "freedesk", "host-agent", "bin", "freedesk"), ""},
		{inside("Users", "a", "Downloads", "freedesk.exe"), ""},
		{inside("FreeDesk.app", "MacOS", "freedesk"), ""},                 // no Contents
		{inside("Tools", "Contents", "MacOS", "freedesk"), ""},            // not in a .app
		{inside("FreeDesk.app", "Contents", "Resources", "freedesk"), ""}, // not the program
	}
	for _, c := range cases {
		got, ok := appBundleParent(c.exe)
		if ok != (c.want != "") || got != c.want {
			t.Errorf("appBundleParent(%q) = %q, %v; want %q", c.exe, got, ok, c.want)
		}
	}
}
