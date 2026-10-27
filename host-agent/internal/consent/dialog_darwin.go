//go:build darwin && cgo

package consent

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/macos"
)

// Dialog asks the operator with a native Yes/No alert, so nobody has to find
// a terminal and type. One question at a time.
//
// The alert shows above every window without making FreeDesk the active
// application: a key the operator was already typing elsewhere never lands
// on it. They click it to answer, and the Dock icon bounces until they turn
// to it. Return presses No.
type Dialog struct {
	timeout time.Duration
	mu      sync.Mutex // guards seq only; the operator's attention is uiMu
	seq     uint64
}

// NewDialog returns the alert approver. The alert runs in the application
// the status window opened (macos.AppOpen).
func NewDialog(timeout time.Duration) *Dialog {
	return &Dialog{timeout: timeout}
}

// Ask blocks until the operator clicks Yes or No, the timeout passes, or ctx
// ends (the viewer withdrew the request), which closes the alert. Only an
// explicit Yes approves.
func (d *Dialog) Ask(ctx context.Context, p Prompt) Answer {
	answer := Refused
	exclusive(func() { answer = d.ask(ctx, p) })
	return answer
}

// ask shows the alert. It runs inside exclusive, which is what makes the
// session hold remote input back for as long as the alert is up.
func (d *Dialog) ask(ctx context.Context, p Prompt) Answer {
	d.mu.Lock()
	d.seq++
	seq := d.seq
	d.mu.Unlock()
	// Echo the question to the log too: it shows in the status window's
	// activity pane, and it leaves a record of what was asked.
	log.Printf("%s>>> Answer in the dialog window.", p.ConsoleHeader())

	switch macos.Ask(ctx, p.Title(seq), p.Text(d.timeout), d.timeout) {
	case macos.AnswerYes:
		return Allowed
	case macos.AnswerTimedOut:
		log.Println(">>> no answer; request rejected")
		return Unanswered
	}
	return Refused
}

// newPlatformApprover picks the alert unless the operator asked for the
// console prompt (headless or scripted runs), or there is no application to
// show it in: without the status window (a login over ssh, say) the console
// is all there is.
func newPlatformApprover(mode string, timeout time.Duration) Approver {
	if mode == ModeConsole || !macos.AppOpen() {
		return NewConsole(timeout)
	}
	return NewDialog(timeout)
}
