// Package consent asks the local operator to approve what a viewer wants to
// do: start a session, and send files once one is running. Without an explicit
// approval within the timeout the answer is no — so an unattended machine
// never silently accepts a connection, and never silently accepts a file.
package consent

import (
	"context"
	"time"
)

// Approval modes.
const (
	// ModeDialog shows a native Yes/No window (Windows only; the default there).
	ModeDialog = "dialog"
	// ModeConsole asks "y + Enter" on the console (the only mode elsewhere).
	ModeConsole = "console"
)

// Answer is how a prompt ended.
//
// "No" and "nobody answered" are different facts about the world, and every
// caller that collapses them into one bool throws away the only evidence that
// the operator has walked away from the machine. Keeping them apart is what
// lets a policy react to an absent operator without punishing a present one
// who simply said no.
type Answer int

const (
	// Refused is an explicit no — and also a withdrawn request, because the
	// question stopped mattering rather than going unheard.
	Refused Answer = iota
	// Allowed is an explicit yes. Nothing else is.
	Allowed
	// Unanswered is the timeout passing with nobody there.
	Unanswered
)

// OK reports whether what was asked about may go ahead. Only Allowed does.
func (a Answer) OK() bool { return a == Allowed }

// String names the answer, so a log line or a failed test says which of the
// three happened rather than printing an integer.
func (a Answer) String() string {
	switch a {
	case Allowed:
		return "allowed"
	case Unanswered:
		return "unanswered"
	default:
		return "refused"
	}
}

// Approver decides whether one request may proceed. Implementations must
// return Refused when ctx is cancelled (the viewer withdrew the request), and
// must only answer Allowed on an explicit yes.
//
// One question at a time, process-wide: two native dialogs stacked on top of
// each other is worse than waiting for the first to be answered.
type Approver interface {
	Ask(ctx context.Context, p Prompt) Answer
}

// New returns the approver for the given mode ("dialog" or "console"; empty
// picks the platform default).
func New(mode string, timeout time.Duration) Approver {
	return newPlatformApprover(mode, timeout)
}
