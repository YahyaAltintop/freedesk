// Package ui shows the operator the one thing they need — this computer's
// code — and a record of what the agent did. It never asks a question; the
// consent package does that. On Windows it is a small native window; elsewhere
// there is no window and the agent keeps using the console.
package ui

import (
	"errors"
	"io"
	"strings"
)

// ErrUnavailable means this platform has no status window.
var ErrUnavailable = errors.New("no status window on this platform")

// Options configures the window.
type Options struct {
	// Title is the window title.
	Title string
	// OnClose runs once, on the UI thread, the first time the operator asks
	// the window to close (or Windows ends the session). It must not block;
	// cancelling the agent's context is the intended use. The window stays
	// open, saying "Stopping…", until Done is called.
	OnClose func()
}

// Window is the status window. Every method except Loop may be called from
// any goroutine, and none of them waits for the UI thread.
type Window interface {
	// ShowCode shows the pairing code, already formatted for reading, and the
	// web page the other side should open.
	ShowCode(code, site string)
	// SetStatus replaces the one-line status under the code.
	SetStatus(text string)
	// Append adds one log record to the activity pane. A trailing newline is
	// dropped; embedded newlines start new lines.
	Append(record string)
	// ShowUpdate offers a newer release: a button that opens its page in the
	// browser. The window never downloads anything itself.
	ShowUpdate(version, url string)
	// Done reports that the agent has finished with the given exit code. Zero
	// closes the window. Anything else keeps it open, marked as failed, until
	// the operator closes it, so the reason in the activity pane can be read.
	Done(exitCode int)
	// Loop runs the window until it is destroyed and returns the exit code
	// passed to Done. It must run on the thread that called Open.
	Loop() int
}

// Tee returns a writer for log.SetOutput that shows every record in the window
// and still writes it to echo (a console or a pipe) when there is one. Errors
// from echo are ignored on purpose: under -H=windowsgui stderr exists but
// cannot be written, and a log line must never fail because of that.
func Tee(w Window, echo io.Writer) io.Writer {
	return &tee{win: w, echo: echo}
}

type tee struct {
	win  Window
	echo io.Writer
}

func (t *tee) Write(p []byte) (int, error) {
	if t.echo != nil {
		_, _ = t.echo.Write(p)
	}
	t.win.Append(string(p))
	return len(p), nil
}

// splitRecord turns one log record into display lines: the trailing newline
// goes, embedded newlines split (a multi-line error keeps its shape), and
// carriage returns and NUL bytes go — NUL cannot be shown and breaks the
// UTF-16 conversion the window needs.
func splitRecord(s string) []string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// The activity pane keeps this many lines and, when full, drops this many of
// the oldest at once, so it is redrawn once per chunk rather than per line.
const (
	maxActivityLines = 400
	activityDrop     = 100
)

// lineRing is the text of the activity pane.
type lineRing struct {
	max, drop int
	lines     []string
}

func newLineRing() *lineRing {
	return &lineRing{max: maxActivityLines, drop: activityDrop}
}

// push appends lines and reports whether old ones were dropped, in which case
// the pane has to be redrawn from text() instead of appended to.
func (r *lineRing) push(lines ...string) (trimmed bool) {
	r.lines = append(r.lines, lines...)
	if len(r.lines) <= r.max {
		return false
	}
	n := max(r.drop, len(r.lines)-r.max)
	// A fresh slice, so the dropped strings can actually be collected.
	r.lines = append([]string(nil), r.lines[n:]...)
	return true
}

// text is every kept line, separated the way a Windows edit control wants.
func (r *lineRing) text() string {
	return strings.Join(r.lines, "\r\n")
}
