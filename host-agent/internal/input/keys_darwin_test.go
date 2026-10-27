package input

import (
	"strconv"
	"testing"
)

func TestCodeToKeycode(t *testing.T) {
	cases := map[string]uint16{
		"KeyA":          0x00,
		"KeyZ":          0x06,
		"Digit0":        0x1D,
		"Digit9":        0x19,
		"Enter":         0x24,
		"Escape":        0x35,
		"Space":         0x31,
		"ArrowUp":       0x7E,
		"F1":            0x7A,
		"F12":           0x6F,
		"Numpad0":       0x52,
		"NumpadEnter":   0x4C,
		"Slash":         0x2C,
		"Backspace":     0x33, // kVK_Delete: the key a Mac labels "delete"
		"Delete":        0x75, // kVK_ForwardDelete
		"IntlBackslash": 0x0A,
	}
	for code, want := range cases {
		if got, ok := codeToKeycode(code); !ok || got != want {
			t.Errorf("%s: got 0x%02X (ok=%v), want 0x%02X", code, got, ok, want)
		}
	}
	for _, code := range []string{"Unknown", "F21", "F24", "PrintScreen", "ScrollLock", "Pause"} {
		if kc, ok := codeToKeycode(code); ok {
			t.Errorf("%s has no key on a Mac but maps to 0x%02X", code, kc)
		}
	}
}

// TestModifierCodesAreTheirKeys: the modifiers the injector treats as flags
// are the keys the browser codes name.
func TestModifierCodesAreTheirKeys(t *testing.T) {
	for code, want := range map[string]uint16{
		"ShiftLeft": kvkShift, "ShiftRight": kvkRightShift,
		"ControlLeft": kvkControl, "ControlRight": kvkRightControl,
		"AltLeft": kvkOption, "AltRight": kvkRightOption,
		"MetaLeft": kvkCommand, "MetaRight": kvkRightCommand,
		"CapsLock": kvkCapsLock, "Fn": kvkFunction,
	} {
		if got, ok := codeToKeycode(code); !ok || got != want {
			t.Errorf("%s: got 0x%02X (ok=%v), want 0x%02X", code, got, ok, want)
		}
	}
}

// TestPCKeyboardCodesMap: a viewer on a PC keyboard sends these codes, the
// ones the Windows map knows; every one with a key on a Mac must press it.
func TestPCKeyboardCodesMap(t *testing.T) {
	codes := []string{
		"Enter", "NumpadEnter", "Escape", "Backspace", "Tab", "Space", "CapsLock",
		"ShiftLeft", "ShiftRight", "ControlLeft", "ControlRight", "AltLeft", "AltRight",
		"MetaLeft", "MetaRight", "ContextMenu",
		"ArrowLeft", "ArrowUp", "ArrowRight", "ArrowDown", "Home", "End", "PageUp", "PageDown",
		"Insert", "Delete", "Minus", "Equal", "BracketLeft", "BracketRight", "Backslash",
		"Semicolon", "Quote", "Backquote", "Comma", "Period", "Slash",
		"NumpadAdd", "NumpadSubtract", "NumpadMultiply", "NumpadDivide", "NumpadDecimal", "NumLock",
	}
	for c := 'A'; c <= 'Z'; c++ {
		codes = append(codes, "Key"+string(c))
	}
	for d := range 10 {
		codes = append(codes, "Digit"+strconv.Itoa(d), "Numpad"+strconv.Itoa(d))
	}
	for f := 1; f <= 20; f++ {
		codes = append(codes, "F"+strconv.Itoa(f))
	}
	for _, code := range codes {
		if _, ok := codeToKeycode(code); !ok {
			t.Errorf("%s does not map to a Mac key", code)
		}
	}
}

// TestEveryCodePressesItsOwnKey: no two codes share a key code, which a typo
// in the table would make them do.
func TestEveryCodePressesItsOwnKey(t *testing.T) {
	seen := map[uint16]string{}
	for code, kc := range keycodeMap {
		if other, dup := seen[kc]; dup {
			t.Errorf("%s and %s both press 0x%02X", code, other, kc)
		}
		seen[kc] = code
	}
}
