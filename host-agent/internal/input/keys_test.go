package input

import "testing"

func TestCodeToVK(t *testing.T) {
	cases := map[string]uint16{
		"KeyA":         0x41,
		"KeyZ":         0x5A,
		"Digit0":       0x30,
		"Digit9":       0x39,
		"Enter":        0x0D,
		"Escape":       0x1B,
		"Space":        0x20,
		"ArrowUp":      0x26,
		"F1":           0x70,
		"F12":          0x7B,
		"ShiftLeft":    0xA0,
		"ControlRight": 0xA3,
		"Numpad0":      0x60,
		"NumpadEnter":  0x0D,
		"Slash":        0xBF,
	}
	for code, want := range cases {
		got, ok := codeToVK(code)
		if !ok || got != want {
			t.Errorf("%s: expected 0x%X, got 0x%X (ok=%v)", code, want, got, ok)
		}
	}

	if _, ok := codeToVK("Unknown"); ok {
		t.Error("an unknown key should not map")
	}
	if _, ok := codeToVK("F25"); ok {
		t.Error("F25 should be invalid")
	}
}
