package input

import (
	"strconv"
	"strings"
)

// vkMap maps KeyboardEvent.code values to Windows virtual-key codes for keys
// that cannot be derived programmatically.
var vkMap = map[string]uint16{
	"Enter": 0x0D, "NumpadEnter": 0x0D, "Escape": 0x1B, "Backspace": 0x08,
	"Tab": 0x09, "Space": 0x20, "CapsLock": 0x14,
	"ShiftLeft": 0xA0, "ShiftRight": 0xA1,
	"ControlLeft": 0xA2, "ControlRight": 0xA3,
	"AltLeft": 0xA4, "AltRight": 0xA5,
	"MetaLeft": 0x5B, "MetaRight": 0x5C, "ContextMenu": 0x5D,
	"ArrowLeft": 0x25, "ArrowUp": 0x26, "ArrowRight": 0x27, "ArrowDown": 0x28,
	"Home": 0x24, "End": 0x23, "PageUp": 0x21, "PageDown": 0x22,
	"Insert": 0x2D, "Delete": 0x2E,
	"Minus": 0xBD, "Equal": 0xBB, "BracketLeft": 0xDB, "BracketRight": 0xDD,
	"Backslash": 0xDC, "Semicolon": 0xBA, "Quote": 0xDE, "Backquote": 0xC0,
	"Comma": 0xBC, "Period": 0xBE, "Slash": 0xBF,
	"NumpadAdd": 0x6B, "NumpadSubtract": 0x6D, "NumpadMultiply": 0x6A,
	"NumpadDivide": 0x6F, "NumpadDecimal": 0x6E,
	"PrintScreen": 0x2C, "ScrollLock": 0x91, "Pause": 0x13, "NumLock": 0x90,
}

// codeToVK converts a KeyboardEvent.code into a Windows virtual-key code.
func codeToVK(code string) (uint16, bool) {
	if vk, ok := vkMap[code]; ok {
		return vk, true
	}
	// KeyA..KeyZ  ('A' == 0x41 == VK_A)
	if len(code) == 4 && strings.HasPrefix(code, "Key") {
		if c := code[3]; c >= 'A' && c <= 'Z' {
			return uint16(c), true
		}
	}
	// Digit0..Digit9  ('0' == 0x30 == VK_0)
	if len(code) == 6 && strings.HasPrefix(code, "Digit") {
		if c := code[5]; c >= '0' && c <= '9' {
			return uint16(c), true
		}
	}
	// Numpad0..Numpad9  (VK_NUMPAD0 == 0x60)
	if len(code) == 7 && strings.HasPrefix(code, "Numpad") {
		if c := code[6]; c >= '0' && c <= '9' {
			return uint16(0x60 + (c - '0')), true
		}
	}
	// F1..F24  (VK_F1 == 0x70)
	if strings.HasPrefix(code, "F") {
		if n, err := strconv.Atoi(code[1:]); err == nil && n >= 1 && n <= 24 {
			return uint16(0x70 + n - 1), true
		}
	}
	return 0, false
}
