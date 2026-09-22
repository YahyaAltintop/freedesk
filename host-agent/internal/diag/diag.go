// Package diag is the developer's channel: the technical detail behind the
// plain sentences the operator sees.
//
// The status window is read by someone who double-clicked an exe. It tells
// them what happened and what they can do about it, and nothing else — no
// error codes, no HTTP statuses, no console commands, because none of those
// is something they can act on. Whatever a developer would want to see
// instead goes here. It reaches a console when the agent was started from
// one, and the window as well when RC_DIAG=on.
package diag

import (
	"io"
	"log"
)

var logger = log.New(io.Discard, "", log.LstdFlags)

// SetOutput directs the channel; nil discards it.
func SetOutput(w io.Writer) {
	if w == nil {
		w = io.Discard
	}
	logger.SetOutput(w)
}

// Printf writes one record.
func Printf(format string, args ...any) {
	logger.Printf(format, args...)
}
