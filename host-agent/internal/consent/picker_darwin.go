//go:build darwin && cgo

package consent

import (
	"context"
	"log"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/macos"
)

// nativePicker shows macOS's Open panel.
type nativePicker struct{ pickerGate }

// newPlatformPicker returns the Open panel. It runs in the application the
// status window opened; without one there is no picker, and the agent does
// not offer downloads.
func newPlatformPicker() FilePicker {
	if !macos.AppOpen() {
		return unavailable{}
	}
	return &nativePicker{}
}

func (p *nativePicker) Available() bool { return true }

// Pick shows the panel and returns what the operator chose.
func (p *nativePicker) Pick(ctx context.Context) ([]string, bool) {
	if !p.claim() {
		return nil, false
	}
	// Serialised against every other prompt: the operator must never be shown
	// a picker stacked on top of a question they have not answered.
	var (
		paths []string
		ok    bool
	)
	exclusive(func() {
		if ctx.Err() != nil {
			return
		}
		paths, ok = p.show(ctx)
	})
	return paths, ok
}

func (p *nativePicker) show(ctx context.Context) ([]string, bool) {
	log.Printf("[consent] the viewer asked for files — choose them in the dialog window")
	// The panel shows no title; the message at its top says why it opened.
	paths, ok := macos.ChooseFiles(ctx, "FreeDesk - choose files to send",
		"The person connected to this computer asked for files. Only the files you choose here are sent.",
		"Send")
	if ctx.Err() != nil {
		log.Printf("[consent] the session ended while the file dialog was open; it was closed")
		return nil, false
	}
	if !ok {
		return nil, false
	}
	log.Printf("[consent] the operator chose %d file(s) to send", len(paths))
	return paths, true
}
