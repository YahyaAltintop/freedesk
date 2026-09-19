package consent

import "sync"

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

// exclusive runs fn with the operator's attention to itself.
func exclusive(fn func()) {
	uiMu.Lock()
	defer uiMu.Unlock()
	fn()
}
