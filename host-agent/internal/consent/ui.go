package consent

import (
	"sync"
	"sync/atomic"
)

// uiMu serialises everything this package puts in front of the operator.
//
// One question at a time, process-wide. Two always-on-top windows stacked on
// each other is worse than waiting for the first to be answered, and an
// operator facing a pile of prompts stops reading them — which would undo the
// only real control in the system.
//
// This does not deadlock against a connection request: the coordinator refuses
// a second session before it ever asks, so the only contention is between
// prompts inside one live session. The wait is bounded by the prompt's own
// timeout.
var uiMu sync.Mutex

// attention counts what is in front of the operator right now. uiMu keeps it
// at 0 or 1, but a counter is the honest shape of the question "is anything on
// screen", and it can be read without taking the lock.
var attention atomic.Int32

// exclusive runs fn with the operator's attention to itself. Every prompt and
// every picker goes through here, so Busy cannot miss one.
func exclusive(fn func()) {
	uiMu.Lock()
	defer uiMu.Unlock()
	attention.Add(1)
	defer attention.Add(-1)
	fn()
}

// Busy reports whether a question is in front of the operator right now.
//
// The session asks this before applying anything the viewer sends. A prompt is
// drawn on the very screen the viewer is watching, and input injected with
// SendInput lands on it exactly as a local click would. So without this the
// viewer could answer the question that exists only for the operator to
// answer. That is not a supposition: session/inputgate_windows_test.go clicks
// the Yes button through the input path and checks that nothing happens.
func Busy() bool { return attention.Load() > 0 }
