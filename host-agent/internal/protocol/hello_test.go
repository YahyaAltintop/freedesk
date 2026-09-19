package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

// The greeting is the only thing a viewer has to go on, so its shape is pinned
// here rather than left to whatever json.Marshal happens to produce.
func TestHelloEncodesTheAgreedShape(t *testing.T) {
	got := NewHello("0.3.0", CapFileSend).Encode()
	want := `{"t":"hello","v":2,"caps":["file.send"],"agent":"0.3.0"}`
	if got != want {
		t.Fatalf("greeting changed.\n got: %s\nwant: %s", got, want)
	}
}

// A viewer reads caps without a nil check, so the field must always be a list.
func TestHelloWithNoCapabilitiesSendsAnEmptyList(t *testing.T) {
	got := NewHello("0.3.0").Encode()
	if strings.Contains(got, "null") {
		t.Fatalf("caps must never be null, got: %s", got)
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("the greeting must be valid JSON: %v", err)
	}
	caps, ok := back["caps"].([]any)
	if !ok {
		t.Fatalf("caps decoded as %T, expected a list", back["caps"])
	}
	if len(caps) != 0 {
		t.Fatalf("expected no capabilities, got %v", caps)
	}
}

func TestHelloCarriesEveryCapability(t *testing.T) {
	h := NewHello("1.2.3", CapFileSend, CapFileRecv, CapClipText)
	if h.V != Version {
		t.Fatalf("version = %d, expected %d", h.V, Version)
	}
	if h.Agent != "1.2.3" {
		t.Fatalf("agent = %q", h.Agent)
	}
	if len(h.Caps) != 3 {
		t.Fatalf("caps = %v", h.Caps)
	}
}

// The capability names are the contract with the viewer; a rename here is a
// protocol change, not a refactor.
func TestCapabilityNames(t *testing.T) {
	for name, got := range map[string]string{
		"file.send": CapFileSend,
		"file.recv": CapFileRecv,
		"clip.text": CapClipText,
	} {
		if got != name {
			t.Fatalf("capability renamed: expected %q, got %q", name, got)
		}
	}
}

// A version-1 host is silent, so a viewer can only conclude "old" from getting
// nothing. That only works while this side actually speaks 2 or more.
func TestVersionIsAtLeastTwo(t *testing.T) {
	if Version < 2 {
		t.Fatalf("Version = %d; the greeting only exists from 2 onwards", Version)
	}
}
