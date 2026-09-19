package consent

import (
	"context"
	"time"
)

// pickerCooldown is the minimum gap between two file pickers. Without it a
// viewer could reopen the dialog as fast as the operator can close it, which is
// a way to make the machine unusable rather than a way to fetch a file.
const pickerCooldown = 5 * time.Second

// FilePicker asks the operator which files to send to the viewer.
//
// There is no Yes/No in front of this on purpose: the picker IS the consent.
// The viewer cannot name a path — all it can say is "choose something for me" —
// and what leaves the machine is whatever the operator selects, file by file,
// in a dialog they can cancel. A confirmation before it would carry no
// information the picker does not ("do you want to open a file picker?") while
// adding a click, and that is exactly the prompt fatigue that erodes the
// meaning of the one prompt that matters.
type FilePicker interface {
	// Pick returns absolute paths the operator chose. ok is false when they
	// cancelled, when another picker is already open, or when this build has no
	// picker at all.
	Pick(ctx context.Context) (paths []string, ok bool)
	// Available reports whether this host can show a picker, so the agent only
	// advertises the capability when it can honour it.
	Available() bool
}

// NewFilePicker returns the picker for the given approval mode. Console mode
// has none: typing paths on stdin is a worse experience and a needless surface,
// so the agent simply does not offer downloads there.
func NewFilePicker(mode string) FilePicker {
	if mode == ModeConsole {
		return unavailable{}
	}
	return newPlatformPicker()
}

// unavailable stands in wherever there is no native picker.
type unavailable struct{}

func (unavailable) Pick(context.Context) ([]string, bool) { return nil, false }
func (unavailable) Available() bool                       { return false }
