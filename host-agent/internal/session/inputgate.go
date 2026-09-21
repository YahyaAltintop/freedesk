package session

import "github.com/YahyaAltintop/freedesk/host-agent/internal/consent"

// inputSink is the part of input.Handler the gate needs.
type inputSink interface {
	Handle([]byte)
	ReleaseAll()
}

// applyInput hands one frame from the input channel to the handler — unless
// the operator is being asked something.
//
// A consent prompt is drawn on the screen the viewer is watching, and input
// injected with SendInput lands on it exactly as a local click would. Without
// this gate the viewer could click Yes on the "Incoming files" window, or
// drive the file picker to any file on the disk, and the prompt would be
// approving nothing but itself. Frames arriving during a prompt are dropped
// rather than queued: replaying a click after the window has gone would land
// it on whatever is underneath.
//
// Anything held is released on the way past, so a key the viewer was holding
// when the prompt appeared does not stay pressed for its duration.
func applyInput(h inputSink, data []byte) {
	if consent.Busy() {
		h.ReleaseAll()
		return
	}
	h.Handle(data)
}
