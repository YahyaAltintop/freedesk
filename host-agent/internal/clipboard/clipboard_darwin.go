//go:build darwin && cgo

package clipboard

import (
	"errors"
	"log"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/macos"
)

// Supported: this build shares the clipboard.
const Supported = true

// pasteShortcut is what pastes on this system.
const pasteShortcut = "Command+V"

// pasteboardName is the pasteboard the board works on: "" for the general
// one, the operator's clipboard. Tests use one of their own (TestMain).
var pasteboardName = ""

// The markers of contents not to be shared, by the nspasteboard.org
// convention password managers follow: concealed (a password, say) and
// transient (for no history and no sync). They say what Windows' two
// exclusion formats say.
const (
	concealedType = "org.nspasteboard.ConcealedType"
	transientType = "org.nspasteboard.TransientType"
)

// errNotAllowed: macOS would ask the operator before the clipboard is read.
var errNotAllowed = errors.New("macOS asks before FreeDesk may read the clipboard")

// macBoard is the macOS pasteboard. Unlike Windows' clipboard it belongs to
// no thread and no window; the worker's thread serves it all the same.
type macBoard struct {
	pb     *macos.Pasteboard
	access func() macos.PasteAccess // the pasteboard's own; tests replace it
	ask    func()                   // makes macOS ask about the agent; tests replace it
	asked  bool                     // ask has run
	warned bool                     // the operator was told why copies stay here
}

func newPlatformBoard() (Board, error) {
	pb := macos.OpenPasteboard(pasteboardName)
	return &macBoard{pb: pb, access: pb.Access, ask: askToRead}, nil
}

// pumpMessages has nothing to do: the pasteboard sends the agent no messages.
func pumpMessages() {}

// WriteTextAs puts text on the clipboard; the window handle Windows needs
// means nothing here.
func WriteTextAs(_ uintptr, s string) error {
	b, _ := newPlatformBoard()
	defer b.Close()
	return b.WriteText(s)
}

func (b *macBoard) Sequence() uint32 { return uint32(b.pb.ChangeCount()) }

func (b *macBoard) Excluded() bool {
	return b.pb.HasType(concealedType) || b.pb.HasType(transientType)
}

func (b *macBoard) ReadText() (string, bool, error) {
	// A copy in the Finder carries the files' names as text too. It is a
	// copy of files, not of text, as on Windows: its names never go to the
	// viewer as if typed.
	if b.pb.HasFiles() {
		return "", false, nil
	}
	if !b.mayRead() {
		return "", false, errNotAllowed
	}
	text, ok := b.pb.ReadText()
	return text, ok, nil
}

func (b *macBoard) WriteText(s string) error {
	if !b.pb.WriteText(s) {
		return errors.New("the pasteboard did not take the text")
	}
	return nil
}

func (b *macBoard) ReadFiles() ([]string, bool, error) {
	if !b.pb.HasFiles() {
		return nil, false, nil
	}
	if !b.mayRead() {
		return nil, false, errNotAllowed
	}
	paths := b.pb.ReadFiles()
	return paths, len(paths) > 0, nil
}

func (b *macBoard) WriteFiles(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	if !b.pb.WriteFiles(paths) {
		return errors.New("the pasteboard did not take the files")
	}
	return nil
}

func (b *macBoard) Close() { b.pb.Close() }

// mayRead reports whether the clipboard's contents may be read, which is
// whether macOS lets the agent read them without asking the operator. From
// macOS 15.4 it asks about every read of the clipboard by a program, unless
// the operator has chosen Always Allow for it. Asking at every copy the
// operator makes would be worse than not sharing them, and a read waits for
// the answer, which would hold up the viewer's pastes behind it; so copies on
// this Mac reach the viewer only once Always Allow is chosen. The viewer's
// copies reach this Mac either way: writing is never asked about.
//
// macOS lists a program in System Settings only once it has asked about it.
// So the first time a copy is held back, and macOS has never asked, it is
// made to ask, in the background: the answer is not waited for.
func (b *macBoard) mayRead() bool {
	access := b.access()
	if access == macos.PasteAllowed {
		return true
	}
	if access == macos.PasteNeverAsked && !b.asked {
		b.asked = true
		go b.ask()
	}
	if !b.warned {
		b.warned = true
		if access == macos.PasteDenied {
			log.Printf("[clipboard] what is copied on this Mac does not reach the viewer, because FreeDesk is set to Deny in System Settings > Privacy & Security > Paste from Other Apps: choose Always Allow for it there to share it")
		} else {
			log.Printf("[clipboard] what is copied on this Mac does not reach the viewer, because macOS asks each time before FreeDesk may read it: to share it, choose Always Allow for FreeDesk in System Settings > Privacy & Security > Paste from Other Apps")
		}
	}
	return false
}

// askToRead reads the clipboard once, on a pasteboard handle of its own, so
// that macOS asks the operator about FreeDesk and lists it in System
// Settings. What it reads is thrown away.
func askToRead() {
	pb := macos.OpenPasteboard("")
	defer pb.Close()
	_, _ = pb.ReadText()
}
