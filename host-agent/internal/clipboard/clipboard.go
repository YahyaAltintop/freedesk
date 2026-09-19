// Package clipboard shares clipboard text between the operator's machine and
// the viewer's browser.
//
// The whole package funnels through one goroutine that holds an OS thread for
// its lifetime. That is not tidiness: the Windows clipboard belongs to the
// THREAD that opened it, and Go moves goroutines between threads at will, so a
// close can land on a different thread than its open and fail. The same thread
// owns a message-only window, because opening the clipboard with no window
// makes every write fail.
package clipboard

import "errors"

// Modes for RC_CLIPBOARD.
const (
	// ModeOff shares nothing. The capability is not advertised, and the
	// viewer's clipboard UI disappears with an explanation.
	ModeOff = "off"
	// ModeText shares plain text in both directions. The default.
	ModeText = "text"
)

// MaxTextBytes caps one clipboard payload. Big enough for the code and log
// snippets people actually move around; small enough that a runaway buffer
// cannot be pushed through the session.
const MaxTextBytes = 256 << 10

// ErrUnavailable means this build or this machine has no clipboard to share.
var ErrUnavailable = errors.New("no clipboard on this system")

// ErrBusy means another process held the clipboard for longer than we are
// willing to block the session's message handling.
var ErrBusy = errors.New("the clipboard was busy")

// Board is the platform clipboard. Implementations are NOT safe for concurrent
// use — every call has to come from the worker's locked thread.
type Board interface {
	// Sequence changes whenever anything puts something on the clipboard. It is
	// cheap, takes no lock and does not open the clipboard, so it is safe to
	// poll.
	Sequence() uint32
	// ReadText returns the clipboard's text, and false if it holds none.
	ReadText() (string, bool, error)
	// WriteText replaces the clipboard with text.
	WriteText(s string) error
	// ReadFiles returns the absolute paths of files on the clipboard, and false
	// if it holds none. Copying a file in Explorer is what puts them there.
	ReadFiles() ([]string, bool, error)
	// WriteFiles puts files on the clipboard so they can be pasted in Explorer.
	// The paths must be absolute and must keep existing: the bytes are read at
	// PASTE time, not now.
	WriteFiles(paths []string) error
	// Excluded reports whether the current contents are marked as not for
	// history or sync. Password managers set this; honouring it means a
	// password copied from one never reaches the viewer.
	Excluded() bool
	// Close releases the window and any handles.
	Close()
}

// New returns the platform clipboard, or an unavailable one away from Windows.
func New() (Board, error) { return newPlatformBoard() }

// unavailable stands in where there is no clipboard.
type unavailable struct{}

func (unavailable) Sequence() uint32                   { return 0 }
func (unavailable) ReadText() (string, bool, error)    { return "", false, ErrUnavailable }
func (unavailable) WriteText(string) error             { return ErrUnavailable }
func (unavailable) ReadFiles() ([]string, bool, error) { return nil, false, ErrUnavailable }
func (unavailable) WriteFiles([]string) error          { return ErrUnavailable }
func (unavailable) Excluded() bool                     { return false }
func (unavailable) Close()                             {}
