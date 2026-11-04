//go:build darwin && cgo

package macos

/*
// -mmacosx-version-min with the availability check as an error: every macOS
// API this package reaches must exist on macOS 13, the oldest the agent runs
// on, whatever macOS builds it.
#cgo CFLAGS: -fobjc-arc -mmacosx-version-min=13.0 -Werror=unguarded-availability-new
#cgo LDFLAGS: -framework ScreenCaptureKit -framework CoreMedia -framework CoreVideo -framework CoreGraphics -framework Foundation -framework AppKit
#include "sck_darwin.h"
*/
import "C"

import (
	"errors"
	"unsafe"
)

// ScreenRecordingAllowed reports whether macOS lets the agent record the
// screen. With prompt, macOS asks the operator if it never has; an answer
// given later, in System Settings, takes effect when the agent next starts.
func ScreenRecordingAllowed(prompt bool) bool {
	return C.fd_sck_access(cBool(prompt)) != 0
}

// MainDisplay is the main display, the one at the top-left of the desktop,
// with its size in pixels.
func MainDisplay() (id uint32, w, h int, ok bool) {
	var cid C.uint32_t
	var cw, ch C.int
	if C.fd_sck_display(&cid, &cw, &ch) == 0 {
		return 0, 0, 0, false
	}
	return uint32(cid), int(cw), int(ch), true
}

// ScreenShowable reports whether the screen can be shown now: this user's
// session at the console, not locked, and the main display awake.
func ScreenShowable() bool { return C.fd_session_readable() != 0 }

// ErrNoPicture means a screen stream has no picture now: none yet, or the
// screen shows nothing (asleep, say).
var ErrNoPicture = errors.New("the screen stream has no picture now")

// Picture is one top-down BGRA frame, W x H pixels, Stride bytes from one row
// to the next. Its pixels are the stream's own, read where the GPU left them:
// valid and unchanged until the stream's next Grab or Close.
type Picture struct {
	Pixels       unsafe.Pointer
	W, H, Stride int
}

// ScreenStream is ScreenCaptureKit capturing one display: scaled on the GPU
// to the size asked for, the pointer drawn in, as BGRA. Its methods must not
// be called concurrently.
type ScreenStream struct {
	s *C.fd_sck
}

// OpenScreenStream starts capturing display at width x height pixels. It
// blocks for a few seconds at most, and never asks for the permission.
func OpenScreenStream(display uint32, width, height int) (*ScreenStream, error) {
	var errbuf [256]C.char
	s := C.fd_sck_open(C.uint32_t(display), C.int(width), C.int(height), &errbuf[0], C.int(len(errbuf)))
	if s == nil {
		return nil, errors.New(C.GoString(&errbuf[0]))
	}
	return &ScreenStream{s: s}, nil
}

// Grab returns the newest picture the stream has delivered; changed is false
// when it is the one the last Grab returned (the screen is still). It reports
// ErrNoPicture while there is none, and any other error once the stream has
// ended.
func (s *ScreenStream) Grab() (p Picture, changed bool, err error) {
	var f C.fd_sck_frame
	var errbuf [256]C.char
	switch C.fd_sck_grab(s.s, &f, &errbuf[0], C.int(len(errbuf))) {
	case C.FD_SCK_CHANGED:
		changed = true
	case C.FD_SCK_UNCHANGED:
	case C.FD_SCK_UNAVAILABLE:
		return Picture{}, false, ErrNoPicture
	default:
		return Picture{}, false, errors.New(C.GoString(&errbuf[0]))
	}
	return Picture{Pixels: f.bits, W: int(f.w), H: int(f.h), Stride: int(f.stride)}, changed, nil
}

// Close stops the stream and lets go of its pictures. It is safe to call
// more than once.
func (s *ScreenStream) Close() {
	C.fd_sck_close(s.s)
	s.s = nil
}

func cBool(b bool) C.int {
	if b {
		return 1
	}
	return 0
}
