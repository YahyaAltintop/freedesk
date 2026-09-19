//go:build windows

package consent

import (
	"reflect"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows validates lStructSize and refuses the dialog if it disagrees, so a
// field reordered or mistyped here shows up as "the picker silently never
// opens". 152 bytes is the documented x64 size of OPENFILENAMEW.
func TestOpenFileNameLayoutMatchesWindows(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skipf("the expected size is architecture specific; this is %s", runtime.GOARCH)
	}
	if got := unsafe.Sizeof(openFileNameW{}); got != 152 {
		t.Fatalf("OPENFILENAMEW is %d bytes, expected 152 — a field was reordered or retyped", got)
	}
}

func TestParseSelection(t *testing.T) {
	// One file: the buffer holds a single full path.
	single := encode(`C:\Users\Yahya\Documents\report.pdf`, "")
	if got := parseSelection(single); !reflect.DeepEqual(got, []string{`C:\Users\Yahya\Documents\report.pdf`}) {
		t.Fatalf("single selection parsed as %q", got)
	}

	// Several: the directory first, then each name.
	multi := encode(`C:\Users\Yahya\Documents`, "a.txt", "b with space.pdf", "")
	want := []string{
		`C:\Users\Yahya\Documents\a.txt`,
		`C:\Users\Yahya\Documents\b with space.pdf`,
	}
	if got := parseSelection(multi); !reflect.DeepEqual(got, want) {
		t.Fatalf("multi selection parsed as %q, expected %q", got, want)
	}

	// A directory with a trailing separator (the dialog does this at a drive
	// root) must not produce a doubled separator.
	root := encode(`D:\`, "x.bin", "")
	if got := parseSelection(root); !reflect.DeepEqual(got, []string{`D:\x.bin`}) {
		t.Fatalf("drive-root selection parsed as %q", got)
	}

	if got := parseSelection(encode("")); got != nil {
		t.Fatalf("an empty buffer parsed as %q", got)
	}
}

// encode lays out NUL-terminated entries the way the dialog writes them.
func encode(entries ...string) []uint16 {
	buf := make([]uint16, 0, 256)
	for _, e := range entries {
		buf = append(buf, windows.StringToUTF16(e)...)
	}
	// Room past the end, as the real buffer has.
	for len(buf) < 256 {
		buf = append(buf, 0)
	}
	return buf
}

// The filter has to keep the NULs that separate its sections; the usual
// conversion stops at the first one.
func TestUtf16FilterKeepsItsSeparators(t *testing.T) {
	got := utf16Filter("All files\x00*.*\x00")
	zeros := 0
	for _, c := range got {
		if c == 0 {
			zeros++
		}
	}
	if zeros != 3 {
		t.Fatalf("expected 3 NULs (two separators and the terminator), got %d in %v", zeros, got)
	}
}

// Console mode has no picker, so the agent must not advertise downloads there.
func TestConsoleModeHasNoPicker(t *testing.T) {
	if NewFilePicker(ModeConsole).Available() {
		t.Fatal("console mode must not report a picker")
	}
	if !NewFilePicker(ModeDialog).Available() {
		t.Fatal("dialog mode on Windows should have a picker")
	}
}
