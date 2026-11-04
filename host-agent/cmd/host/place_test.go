package main

import (
	"path/filepath"
	"testing"
)

func TestTranslocated(t *testing.T) {
	root := string(filepath.Separator)
	inside := func(elem ...string) string { return filepath.Join(append([]string{root}, elem...)...) }
	cases := []struct {
		exe  string
		want bool
	}{
		{inside("private", "var", "folders", "x1", "y2", "T", "AppTranslocation", "0F2A-9C", "d", "FreeDesk.app", "Contents", "MacOS", "freedesk"), true},
		{inside("Applications", "FreeDesk.app", "Contents", "MacOS", "freedesk"), false},
		{inside("Volumes", "FreeDesk", "FreeDesk.app", "Contents", "MacOS", "freedesk"), false}, // read-only, not translocated
		{inside("Users", "a", "AppTranslocationNotes", "FreeDesk.app", "Contents", "MacOS", "freedesk"), false},
	}
	for _, c := range cases {
		if got := translocated(c.exe); got != c.want {
			t.Errorf("translocated(%q) = %v; want %v", c.exe, got, c.want)
		}
	}
}

func TestPlaceWarningForTestBinary(t *testing.T) {
	// The test binary is no app, so there is never a warning, whatever
	// volume it runs from.
	if msg, ok := placeWarning(); ok {
		t.Errorf("placeWarning() = %q, true for a program outside an app", msg)
	}
}
