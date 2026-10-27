package main

import (
	"os"
	"path/filepath"
	"strings"
)

// envPath is the settings file the agent reads: the .env in the working
// directory, which is the exe's own folder when Explorer starts it. An app
// the Finder starts works in /, so for the agent inside FreeDesk.app it is the
// .env next to FreeDesk.app instead: where the operator put the app, which is
// where the README says the file goes.
func envPath() string {
	exe, err := os.Executable()
	if err != nil {
		return envFile
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if dir, ok := appBundleParent(exe); ok {
		return filepath.Join(dir, envFile)
	}
	return envFile
}

// appBundleParent returns the folder that holds the app bundle exe is the
// program of (<folder>/<name>.app/Contents/MacOS/<exe>), and false for an exe
// that is not inside one.
func appBundleParent(exe string) (string, bool) {
	macOS := filepath.Dir(exe)
	contents := filepath.Dir(macOS)
	app := filepath.Dir(contents)
	if filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" || !strings.HasSuffix(app, ".app") {
		return "", false
	}
	return filepath.Dir(app), true
}
