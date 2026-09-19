package transfer

import (
	"errors"
	"strings"
	"testing"
)

func TestSafeName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string // "" means the name must be rejected
	}{
		// Ordinary names survive untouched.
		{"plain", "report.pdf", "report.pdf"},
		{"no extension", "Makefile", "Makefile"},
		{"dotfile", ".gitignore", ".gitignore"},
		{"spaces inside", "my holiday photo.jpg", "my holiday photo.jpg"},
		{"unicode", "ünïcode belgesi.pdf", "ünïcode belgesi.pdf"},
		{"emoji", "🎉 party.png", "🎉 party.png"},
		{"double extension", "archive.tar.gz", "archive.tar.gz"},
		{"surrounding spaces trimmed", "  notes.txt  ", "notes.txt"},

		// Path traversal, in both platforms' spelling.
		{"unix traversal", "../../etc/passwd", ""},
		{"windows traversal", `..\..\evil.exe`, ""},
		{"bare parent", "..", ""},
		{"bare dot", ".", ""},
		{"leading slash", "/etc/passwd", ""},
		{"inner backslash", `sub\file.txt`, ""},

		// Drive-relative paths and NTFS alternate data streams.
		{"drive relative", "C:evil.exe", ""},
		{"absolute drive", `C:\evil.exe`, ""},
		{"alternate data stream", "note.txt:hidden.exe", ""},

		// Windows device names resolve from any directory.
		{"device", "CON", ""},
		{"device lowercase with ext", "con.txt", ""},
		{"device mixed case", "PrN.log", ""},
		{"device com", "COM1", ""},
		{"device lpt", "LPT9.tar.gz", ""},
		{"device-like but not", "CONSOLE.txt", "CONSOLE.txt"},
		{"device-like com10", "COM10.txt", "COM10.txt"},

		// Trailing dots and spaces are stripped, because Windows strips them.
		{"trailing dot", "evil.exe.", "evil.exe"},
		{"trailing dots", "evil.exe...", "evil.exe"},
		{"trailing dot and space", "evil.exe. ", "evil.exe"},
		{"device after stripping", "CON. ", ""},
		{"only dots", "...", ""},

		// Characters Windows forbids, plus control characters.
		{"wildcard", "what*.txt", ""},
		{"pipe", "a|b.txt", ""},
		{"quote", `a"b.txt`, ""},
		{"angle bracket", "a<b.txt", ""},
		{"question mark", "a?.txt", ""},
		{"bell", "\abell.txt", ""},
		{"newline", "a\nb.txt", ""},
		{"del", "a\x7fb.txt", ""},

		// Right-to-left override: renders as "photoexe.png" but is an .exe.
		{"rtl override", "photo\u202Egnp.exe", ""},
		{"rtl mark", "photo\u200Fgnp.exe", ""},
		{"bidi isolate", "photo\u2066gnp.exe", ""},

		{"empty", "", ""},
		{"only spaces", "   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeName(tt.in)
			if tt.want == "" {
				if err == nil {
					t.Fatalf("SafeName(%q) = %q, expected a rejection", tt.in, got)
				}
				if !errors.Is(err, ErrBadName) {
					t.Fatalf("SafeName(%q) returned %v, expected ErrBadName", tt.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("SafeName(%q) was rejected: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("SafeName(%q) = %q, expected %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSafeNameTruncatesToFit(t *testing.T) {
	long := strings.Repeat("a", 300) + ".pdf"
	got, err := SafeName(long)
	if err != nil {
		t.Fatalf("a long name should be shortened, not rejected: %v", err)
	}
	if len(got) > maxNameBytes {
		t.Fatalf("name is %d bytes, expected at most %d", len(got), maxNameBytes)
	}
	if !strings.HasSuffix(got, ".pdf") {
		t.Fatalf("the extension must survive truncation, got %q", got)
	}
}

// A multi-byte name must not be cut through the middle of a rune: an invalid
// name on disk is worse than a shorter one.
func TestSafeNameTruncatesOnRuneBoundary(t *testing.T) {
	got, err := SafeName(strings.Repeat("ü", 300) + ".txt")
	if err != nil {
		t.Fatalf("unexpected rejection: %v", err)
	}
	if len(got) > maxNameBytes {
		t.Fatalf("name is %d bytes, expected at most %d", len(got), maxNameBytes)
	}
	for i, r := range got {
		if r == '\uFFFD' {
			t.Fatalf("truncation split a rune at byte %d: %q", i, got)
		}
	}
}

// An "extension" long enough to fill the whole budget is not an extension.
func TestSafeNameHandlesAbsurdExtension(t *testing.T) {
	got, err := SafeName("a." + strings.Repeat("b", 300))
	if err != nil {
		t.Fatalf("unexpected rejection: %v", err)
	}
	if len(got) > maxNameBytes {
		t.Fatalf("name is %d bytes, expected at most %d", len(got), maxNameBytes)
	}
}
