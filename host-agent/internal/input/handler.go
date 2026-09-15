// Package input parses viewer input events (mouse/keyboard) received over the
// WebRTC DataChannel and injects them into Windows via the user32 API.
package input

import (
	"slices"
	"sync"
)

// injector is the platform layer that performs the actual injection. The
// Windows build talks to SendInput; other platforms are no-ops.
type injector interface {
	moveMouse(x, y float64)
	clickMouse(button int, down bool, x, y float64)
	scrollWheel(dx, dy int)
	pressKey(code string, down bool)
}

// systemInjector routes to the platform functions in inject_*.go.
type systemInjector struct{}

func (systemInjector) moveMouse(x, y float64)                    { moveMouse(x, y) }
func (systemInjector) clickMouse(b int, down bool, x, y float64) { clickMouse(b, down, x, y) }
func (systemInjector) scrollWheel(dx, dy int)                    { scrollWheel(dx, dy) }
func (systemInjector) pressKey(code string, down bool)           { pressKey(code, down) }

// Handler applies input messages for ONE session and remembers what is
// currently held down, so everything can be released when the viewer goes
// away. Without this a key or mouse button pressed just before a tab switch,
// focus loss or connection drop would stay pressed on the host forever.
type Handler struct {
	inj injector

	mu      sync.Mutex
	keys    []string // held keys in press order (KeyboardEvent.code)
	keySet  map[string]struct{}
	buttons [3]bool // held mouse buttons by protocol code (0 left, 1 right, 2 middle)
	lastX   float64
	lastY   float64
}

// NewHandler returns a handler that injects into the local system.
func NewHandler() *Handler {
	return newHandler(systemInjector{})
}

func newHandler(inj injector) *Handler {
	return &Handler{inj: inj, keySet: make(map[string]struct{})}
}

// Handle parses one input message, records the held state and injects it.
// Malformed messages are ignored.
func (h *Handler) Handle(data []byte) {
	m, err := Parse(data)
	if err != nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	switch m.T {
	case "m":
		h.lastX, h.lastY = m.X, m.Y
		h.inj.moveMouse(m.X, m.Y)
	case "md":
		if !validButton(m.B) {
			return
		}
		h.lastX, h.lastY = m.X, m.Y
		h.buttons[m.B] = true
		h.inj.clickMouse(m.B, true, m.X, m.Y)
	case "mu":
		if !validButton(m.B) {
			return
		}
		h.lastX, h.lastY = m.X, m.Y
		h.buttons[m.B] = false
		h.inj.clickMouse(m.B, false, m.X, m.Y)
	case "w":
		h.inj.scrollWheel(m.DX, m.DY)
	case "kd":
		// Browser auto-repeat delivers repeated key-downs; track the key once.
		if _, held := h.keySet[m.Code]; !held {
			h.keySet[m.Code] = struct{}{}
			h.keys = append(h.keys, m.Code)
		}
		h.inj.pressKey(m.Code, true)
	case "ku":
		h.forgetKey(m.Code)
		h.inj.pressKey(m.Code, false)
	}
}

// ReleaseAll releases every key and mouse button that is still held: keys in
// reverse press order (so modifiers, usually pressed first, go up last) and
// buttons at the last known pointer position. Calling it again is a no-op.
func (h *Handler) ReleaseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, code := range slices.Backward(h.keys) {
		h.inj.pressKey(code, false)
	}
	h.keys = nil
	h.keySet = make(map[string]struct{})

	for b, down := range h.buttons {
		if down {
			h.inj.clickMouse(b, false, h.lastX, h.lastY)
			h.buttons[b] = false
		}
	}
}

func (h *Handler) forgetKey(code string) {
	if _, held := h.keySet[code]; !held {
		return
	}
	delete(h.keySet, code)
	for i, k := range h.keys {
		if k == code {
			h.keys = append(h.keys[:i], h.keys[i+1:]...)
			break
		}
	}
}

func validButton(b int) bool {
	return b >= 0 && b < 3
}
