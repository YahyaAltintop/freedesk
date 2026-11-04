//go:build darwin && cgo

package macos

// #include <stdlib.h>
// #include "window_darwin.h"
import "C"

import (
	"errors"
	"log"
	"strings"
	"sync/atomic"
	"unsafe"
)

// The status window, and the application around it: its Dock icon, its menu
// and its main-thread loop, which the alerts and the file panel run in too.

var (
	appOpen       atomic.Bool
	windowOnClose atomic.Pointer[func()]
)

// OpenStatusWindow creates the application and shows the agent's window:
// this computer's code, a status line, an update button and the activity
// log, with Privacy and Licenses buttons when there is something for them to
// show. onClose runs once, on the main thread, the first time the operator
// asks to stop: the window's close button, Quit, or logging out. It must not
// block. The window then says "Stopping…" until FinishStatusWindow.
//
// It must be called on the main thread, which then has to run RunApp. It
// fails where there is no window server to show a window on, a login over
// ssh for instance.
func OpenStatusWindow(title, licenses, privacyURL string, onClose func()) error {
	return openStatusWindow(title, licenses, privacyURL, onClose, true, false)
}

// openStatusWindow is OpenStatusWindow; without visible the application has
// no Dock icon and the window is never shown, which is how tests run it.
// fromTests opens it on the main thread from another.
func openStatusWindow(title, licenses, privacyURL string, onClose func(), visible, fromTests bool) error {
	ctitle, clicenses, cprivacy := cText(title), cText(licenses), cText(privacyURL)
	defer C.free(unsafe.Pointer(ctitle))
	defer C.free(unsafe.Pointer(clicenses))
	defer C.free(unsafe.Pointer(cprivacy))
	windowOnClose.Store(&onClose)
	var errbuf [256]C.char
	var ok C.int
	if fromTests {
		ok = C.fd_win_open_from_tests(ctitle, clicenses, cprivacy, cBool(visible), &errbuf[0], C.int(len(errbuf)))
	} else {
		ok = C.fd_win_open(ctitle, clicenses, cprivacy, cBool(visible), &errbuf[0], C.int(len(errbuf)))
	}
	if ok == 0 {
		return errors.New(C.GoString(&errbuf[0]))
	}
	appOpen.Store(true)
	return nil
}

// AppOpen reports whether the application is open, so that the main thread
// runs, or is about to run, its loop: only then can an alert or a file panel
// be shown.
func AppOpen() bool { return appOpen.Load() }

// RunApp runs the application on the main thread until the window is done
// with, and returns the exit code FinishStatusWindow was given.
func RunApp() int { return int(C.fd_app_run()) }

// SetCode shows the pairing code, formatted for reading, and the web page
// the other side opens.
func SetCode(code, site string) {
	ccode, csite := cText(code), cText(site)
	defer C.free(unsafe.Pointer(ccode))
	defer C.free(unsafe.Pointer(csite))
	C.fd_win_set_code(ccode, csite)
}

// SetStatus replaces the status line under the code.
func SetStatus(text string) {
	ctext := cText(text)
	defer C.free(unsafe.Pointer(ctext))
	C.fd_win_set_status(ctext)
}

// SetLog replaces the whole activity log.
func SetLog(text string) {
	ctext := cText(text)
	defer C.free(unsafe.Pointer(ctext))
	C.fd_win_set_log(ctext)
}

// ShowUpdate shows the button that opens a newer release's page.
func ShowUpdate(version, url string) {
	cversion, curl := cText(version), cText(url)
	defer C.free(unsafe.Pointer(cversion))
	defer C.free(unsafe.Pointer(curl))
	C.fd_win_show_update(cversion, curl)
}

// FinishStatusWindow reports that the agent has finished. A zero exitCode,
// or an end the operator asked for, closes the window and ends RunApp; any
// other keeps it open, marked as failed, until the operator closes it.
func FinishStatusWindow(exitCode int) { C.fd_win_finish(C.int(exitCode)) }

//export fdStatusWindowCloseRequested
func fdStatusWindowCloseRequested() {
	if f := windowOnClose.Load(); f != nil && *f != nil {
		(*f)()
	}
}

//export fdStatusWindowNote
func fdStatusWindowNote(line *C.char) {
	log.Printf("[host-agent] %s", C.GoString(line))
}

// cText is s for AppKit, which takes UTF-8 without NUL bytes: a string that
// is not would otherwise show as nothing at all.
func cText(s string) *C.char {
	return C.CString(strings.ReplaceAll(strings.ToValidUTF8(s, "�"), "\x00", ""))
}

// For tests.

// windowState is what the window shows.
type windowState struct {
	code, site, status, updateTitle          string
	copyEnabled, closeEnabled, updateShown   bool
	hasPrivacy, hasLicenses                  bool
	privacyX, privacyY, licensesX, licensesY float64
	closing, done, failed                    bool
	logLength                                int
	logLast                                  string
	visible                                  bool
	licensesShown                            bool
	licensesLength                           int
}

// readWindowState reads the window on the main thread, which must be running
// the application's loop.
func readWindowState() windowState {
	var s C.fd_win_state
	C.fd_win_read_state(&s)
	return windowState{
		code:           C.GoString(&s.code[0]),
		site:           C.GoString(&s.site[0]),
		status:         C.GoString(&s.status[0]),
		updateTitle:    C.GoString(&s.update_title[0]),
		copyEnabled:    s.copy_enabled != 0,
		closeEnabled:   s.close_enabled != 0,
		updateShown:    s.update_visible != 0,
		hasPrivacy:     s.has_privacy != 0,
		hasLicenses:    s.has_licenses != 0,
		privacyX:       float64(s.privacy_x),
		privacyY:       float64(s.privacy_y),
		licensesX:      float64(s.licenses_x),
		licensesY:      float64(s.licenses_y),
		closing:        s.closing != 0,
		done:           s.done != 0,
		failed:         s.failed != 0,
		logLength:      int(s.log_length),
		logLast:        C.GoString(&s.log_last[0]),
		visible:        s.visible != 0,
		licensesShown:  s.licenses_shown != 0,
		licensesLength: int(s.licenses_length),
	}
}

// snapshotWindow draws the window's contents into a PNG file.
func snapshotWindow(path string) bool {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	return C.fd_win_snapshot(cpath) != 0
}

// pressClose presses the window's close button.
func pressClose() { C.fd_win_request_close() }

// Appearances to draw in.
const (
	systemAppearance = 0
	lightAppearance  = 1
	darkAppearance   = 2
)

// setAppearance draws everything in one appearance.
func setAppearance(which int) { C.fd_app_set_appearance(C.int(which)) }

// pressLicenses presses the Licenses button; closeLicenses closes the window
// it opened.
func pressLicenses() { C.fd_win_press_licenses() }
func closeLicenses() { C.fd_win_close_licenses() }
