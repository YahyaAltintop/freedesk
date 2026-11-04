//go:build darwin && cgo

package macos

// #include <stdlib.h>
// #include "dialogs_darwin.h"
import "C"

import (
	"context"
	"sync"
	"time"
	"unsafe"
)

// Answer is how a question in an alert ended.
type Answer int

const (
	// AnswerNo: the operator pressed No, or there was no application to show
	// the question in.
	AnswerNo Answer = iota
	// AnswerYes: the operator pressed Yes. Nothing else is.
	AnswerYes
	// AnswerTimedOut: nobody answered in time.
	AnswerTimedOut
	// AnswerWithdrawn: the question stopped mattering before it was answered.
	AnswerWithdrawn
)

// dialogGrace bounds two waits on the main thread: for a dialog it was told
// to close to go, and for one it has not got round to showing at all, past
// the dialog's own timeout.
const dialogGrace = 3 * time.Second

// dialogs are the dialogs started and not yet ended, by token.
var dialogs = struct {
	sync.Mutex
	last    uint64
	waiting map[uint64]chan dialogEnd
}{waiting: map[uint64]chan dialogEnd{}}

// dialogEnd is how a dialog ended: one of the FD_DLG_ results, and the files
// chosen.
type dialogEnd struct {
	result C.int
	paths  []string
}

func startDialog() (uint64, <-chan dialogEnd) {
	dialogs.Lock()
	defer dialogs.Unlock()
	dialogs.last++
	ended := make(chan dialogEnd, 1)
	dialogs.waiting[dialogs.last] = ended
	return dialogs.last, ended
}

// forgetDialog stops waiting for a dialog; how it ends, if it still does, is
// dropped.
func forgetDialog(token uint64) {
	dialogs.Lock()
	delete(dialogs.waiting, token)
	dialogs.Unlock()
}

//export fdDialogEnded
func fdDialogEnded(token C.uint64_t, result C.int, paths **C.char, n C.int) {
	end := dialogEnd{result: result}
	for _, p := range unsafe.Slice(paths, int(n)) {
		end.paths = append(end.paths, C.GoString(p))
	}
	dialogs.Lock()
	ended := dialogs.waiting[uint64(token)]
	delete(dialogs.waiting, uint64(token))
	dialogs.Unlock()
	if ended != nil {
		ended <- end // buffered for exactly this
	}
}

// withdraw closes a dialog, or keeps it from opening, and waits a little for
// it to be gone.
func withdraw(token uint64, ended <-chan dialogEnd) {
	C.fd_dlg_withdraw(C.uint64_t(token))
	select {
	case <-ended:
	case <-time.After(dialogGrace):
	}
}

// Ask asks the operator a Yes/No question in an alert: title in bold, text
// below it as written, No the button Return presses. The alert shows above
// every window without making FreeDesk the active application, so nothing
// typed elsewhere can answer it, and the Dock icon bounces until the operator
// turns to it. It ends on an answer, after timeout, or when ctx ends, which
// closes it. Only an open application (AppOpen) can show it.
func Ask(ctx context.Context, title, text string, timeout time.Duration) Answer {
	if !AppOpen() {
		return AnswerNo
	}
	if ctx.Err() != nil {
		return AnswerWithdrawn
	}
	token, ended := startDialog()
	defer forgetDialog(token)
	ctitle, ctext := cText(title), cText(text)
	C.fd_dlg_ask(C.uint64_t(token), ctitle, ctext, C.double(timeout.Seconds()))
	C.free(unsafe.Pointer(ctitle))
	C.free(unsafe.Pointer(ctext))

	// The alert keeps its own time; this is for one never shown.
	late := time.NewTimer(timeout + dialogGrace)
	defer late.Stop()
	select {
	case end := <-ended:
		switch end.result {
		case C.FD_DLG_YES:
			return AnswerYes
		case C.FD_DLG_TIMED_OUT:
			return AnswerTimedOut
		case C.FD_DLG_WITHDRAWN:
			return AnswerWithdrawn
		}
		return AnswerNo
	case <-ctx.Done():
		withdraw(token, ended)
		return AnswerWithdrawn
	case <-late.C:
		withdraw(token, ended)
		return AnswerTimedOut
	}
}

// ChooseFiles lets the operator choose files in the Open panel: any number,
// never a folder. message is shown at the top of the panel and prompt on its
// button. ok is false when the operator cancelled, when ctx ended first,
// which closes the panel, or when the application is not open.
func ChooseFiles(ctx context.Context, title, message, prompt string) (paths []string, ok bool) {
	if !AppOpen() {
		return nil, false
	}
	token, ended := startDialog()
	defer forgetDialog(token)
	ctitle, cmessage, cprompt := cText(title), cText(message), cText(prompt)
	C.fd_dlg_choose_files(C.uint64_t(token), ctitle, cmessage, cprompt)
	C.free(unsafe.Pointer(ctitle))
	C.free(unsafe.Pointer(cmessage))
	C.free(unsafe.Pointer(cprompt))

	select {
	case end := <-ended:
		return end.paths, end.result == C.FD_DLG_CHOSEN && len(end.paths) > 0
	case <-ctx.Done():
		withdraw(token, ended)
		return nil, false
	}
}

// For tests.

// dialogState is the dialog on screen.
type dialogState struct {
	shown, allSpaces, appActive bool
	level                       int
	title                       string
	returnButton, otherButton   string
}

// readDialogState reads the dialog on screen, if there is one, on the main
// thread.
func readDialogState() dialogState {
	var s C.fd_dlg_state
	C.fd_dlg_read_state(&s)
	return dialogState{
		shown:        s.shown != 0,
		allSpaces:    s.all_spaces != 0,
		appActive:    s.app_active != 0,
		level:        int(s.level),
		title:        C.GoString(&s.title[0]),
		returnButton: C.GoString(&s.return_button[0]),
		otherButton:  C.GoString(&s.other_button[0]),
	}
}

// pressDialogButton presses the alert's button with that title.
func pressDialogButton(title string) {
	ctitle := C.CString(title)
	defer C.free(unsafe.Pointer(ctitle))
	C.fd_dlg_press(ctitle)
}

// choosable reports whether the Open panel lets the file at path be chosen.
func choosable(path string) bool {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	return C.fd_dlg_choosable(cpath) != 0
}

// snapshotDialog draws the dialog on screen into a PNG file.
func snapshotDialog(path string) bool {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	return C.fd_dlg_snapshot(cpath) != 0
}
