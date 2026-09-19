// Package transfer receives and sends files over the session's "file"
// DataChannel (see docs/PROTOCOL.md §2). Nothing in the protocol carries a path:
// the viewer supplies a name, this package decides where it may be written.
package transfer

import (
	"errors"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ErrBadName is returned for a name that cannot be made safe to write. It is
// deliberately not recoverable by rewriting: a name that had to be rescued is a
// name the operator was not shown, and the prompt has to say what will actually
// land on disk.
var ErrBadName = errors.New("unsafe file name")

// maxNameBytes leaves room inside Windows' 255-byte component limit for the
// ".part" suffix a transfer in progress carries and for a " (12)" collision
// suffix on top of it.
const maxNameBytes = 200

// reservedStems are device names that Windows resolves from ANY directory, with
// or without an extension: a file called CON.txt is still the console.
var reservedStems = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// SafeName turns a viewer-supplied file name into one that is safe to create
// inside a fixed directory, or reports that it cannot.
//
// The name arrives from the other end of the connection and is never trusted.
// Everything here is rejected rather than repaired, with one exception: trailing
// dots and spaces are stripped, because Windows strips them itself when creating
// the file and a check that ran before that would be checking a name that never
// reaches the filesystem.
func SafeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", ErrBadName
	}
	if !utf8.ValidString(name) {
		return "", ErrBadName
	}

	// Separators are rejected outright rather than left to filepath.Base, whose
	// idea of a separator depends on the platform it was compiled for: on Linux
	// "..\\..\\evil" is one perfectly ordinary file name.
	if strings.ContainsAny(name, `/\`) {
		return "", ErrBadName
	}
	// A colon is both "C:evil" (relative to a drive's current directory) and
	// "note.txt:hidden.exe" (an NTFS alternate data stream).
	if strings.Contains(name, ":") {
		return "", ErrBadName
	}
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7F:
			return "", ErrBadName
		case strings.ContainsRune(`<>"|?*`, r):
			return "", ErrBadName
		case isBidiControl(r):
			// "photo‮gnp.exe" is displayed as "photoexe.png". The operator
			// approves what they can read, so a name that reads as something
			// other than what it is must not get as far as the prompt.
			return "", ErrBadName
		}
	}

	// Windows silently drops these at creation time, so strip them before every
	// remaining check: otherwise "CON. " passes the reserved-name test and then
	// becomes CON on disk.
	name = strings.TrimRight(name, ". ")
	if name == "" {
		return "", ErrBadName
	}

	// Windows matches a device name against everything before the FIRST dot, so
	// LPT9.tar.gz is still LPT9 — which is not what an extension split gives.
	if head, _, _ := strings.Cut(name, "."); reservedStems[strings.ToUpper(head)] {
		return "", ErrBadName
	}

	stem, ext := splitExt(name)

	if len(name) > maxNameBytes {
		name = truncate(stem, ext)
		if name == "" {
			return "", ErrBadName
		}
	}

	// Belt and braces: whatever survived must still be a plain component.
	if name != filepath.Base(name) || name == "." || name == ".." {
		return "", ErrBadName
	}
	return name, nil
}

// splitExt divides a name into the part before the LAST dot and the extension
// (including the dot). A leading dot belongs to the stem, so ".gitignore" keeps
// its name instead of becoming an extension with nothing in front of it.
func splitExt(name string) (stem, ext string) {
	i := strings.LastIndex(name, ".")
	if i <= 0 {
		return name, ""
	}
	return name[:i], name[i:]
}

// truncate shortens the stem so stem+ext fits in maxNameBytes, cutting on a rune
// boundary. An extension long enough to fill the budget by itself is dropped —
// at that point it is not an extension, it is the name.
func truncate(stem, ext string) string {
	if len(ext) > maxNameBytes/2 {
		stem, ext = stem+ext, ""
	}
	budget := maxNameBytes - len(ext)
	for len(stem) > budget {
		_, size := utf8.DecodeLastRuneInString(stem)
		stem = stem[:len(stem)-size]
	}
	stem = strings.TrimRight(stem, ". ")
	if stem == "" {
		return ""
	}
	return stem + ext
}

// isBidiControl reports whether r reorders the text around it. These have no
// business in a file name and every business in a disguised one.
func isBidiControl(r rune) bool {
	switch r {
	case 0x200E, 0x200F, // LRM, RLM
		0x202A, 0x202B, 0x202C, 0x202D, 0x202E, // embeddings and overrides
		0x2066, 0x2067, 0x2068, 0x2069: // isolates
		return true
	}
	return false
}
