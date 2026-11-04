package main

import (
	"os"
	"path/filepath"
	"strings"
)

// placeWarning tells the operator when FreeDesk.app runs from somewhere the
// permissions it is about to ask for will not stick: the disk image it was
// downloaded as, or the read-only copy macOS makes of a downloaded app that
// is opened where it was unpacked (app translocation, under a folder named
// AppTranslocation). macOS keeps Screen Recording, Accessibility and the
// rest by the app's identity and place, and a translocated app has a new
// place on every start, so each start asks again; and the .env the README
// says to put next to the app is not next to that copy. Dragging the app to
// Applications in the Finder ends both. Nothing to say on Windows, or for an
// app already where it belongs.
func placeWarning() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if _, inApp := appBundleParent(exe); !inApp {
		return "", false
	}
	if !translocated(exe) && !volumeReadOnly(exe) {
		return "", false
	}
	return "FreeDesk is running from its disk image, or from a temporary copy macOS made of it. " +
		"Drag FreeDesk to Applications, eject the disk image and open it from there: " +
		"the permissions you give it then stay given.", true
}

// translocated reports whether exe is inside the read-only copy macOS runs a
// downloaded app from until the Finder has moved it (app translocation).
func translocated(exe string) bool {
	return strings.Contains(exe, string(filepath.Separator)+"AppTranslocation"+string(filepath.Separator))
}
