package consent

import (
	"strings"
	"testing"
	"time"
)

const testTimeout = 45 * time.Second

// What the operator reads is the whole control: it is the only thing standing
// between a stranger and this machine. These are written out in full so that
// changing the wording is a deliberate edit in the diff rather than a side
// effect of some other change.

func TestConnectPromptText(t *testing.T) {
	got := ConnectRequest("0QiE1KFGS1S7RXm7ASKnXApwz1a5").Text(testTimeout)
	want := "Someone entered this computer's code and wants to connect.\n\n" +
		"Allow them to see your screen and control this computer?\n\n" +
		"This request is rejected automatically in 45 seconds.\n" +
		"(viewer 0QiE1KFG…)"
	if got != want {
		t.Fatalf("connection prompt changed.\n got: %q\nwant: %q", got, want)
	}
}

func TestIncomingFilesPromptText(t *testing.T) {
	files := []FileOffer{
		{Name: "invoice-2024.pdf", Size: 1153434},
		{Name: "screenshot.png", Size: 860160},
		{Name: "setup.exe", Size: 11010048},
	}
	got := IncomingFiles("0QiE1KFGS1S7RXm7ASKnXApwz1a5", files, `C:\Users\Yahya\Downloads\FreeDesk`).
		Text(testTimeout)
	want := "The person connected to this computer wants to send 3 files (12.4 MB).\n\n" +
		"    invoice-2024.pdf  (1.1 MB)\n" +
		"    screenshot.png  (840 KB)\n" +
		"    setup.exe  (10.5 MB)\n" +
		"\nThey will be saved to:\n" +
		`C:\Users\Yahya\Downloads\FreeDesk` + "\n\n" +
		"Existing files are never replaced. Accept these files?\n\n" +
		"This request is rejected automatically in 45 seconds.\n" +
		"(viewer 0QiE1KFG…)"
	if got != want {
		t.Fatalf("file prompt changed.\n got: %q\nwant: %q", got, want)
	}
}

// The extension is the operator's only signal about what they are accepting,
// so a name is never shortened, however many there are.
func TestFilePromptNeverElidesAName(t *testing.T) {
	files := []FileOffer{{Name: strings.Repeat("a", 180) + ".exe", Size: 10}}
	text := IncomingFiles("v", files, "C:\\dl").Text(testTimeout)
	if !strings.Contains(text, files[0].Name) {
		t.Fatal("the full file name must appear in the prompt")
	}
	if strings.Contains(text, "…"+".exe") {
		t.Fatal("the name was elided")
	}
}

// A list nobody finishes reading is not consent, so a long batch is summarised
// — but only when summarising actually saves lines.
func TestFilePromptSummarisesLongBatches(t *testing.T) {
	var files []FileOffer
	for i := range 12 {
		files = append(files, FileOffer{Name: string(rune('a'+i)) + ".txt", Size: 1024})
	}
	text := IncomingFiles("v", files, "C:\\dl").Text(testTimeout)
	if !strings.Contains(text, "… and 7 more files") {
		t.Fatalf("expected the tail to be summarised, got:\n%s", text)
	}
	if strings.Contains(text, "f.txt") {
		t.Fatal("only the first five names should be listed")
	}
	if !strings.Contains(text, "e.txt") {
		t.Fatal("the first five names must be listed")
	}
}

// Summarising one file would not save a line, so it is listed instead.
func TestFilePromptListsSixFilesRatherThanSummarisingOne(t *testing.T) {
	var files []FileOffer
	for i := range 6 {
		files = append(files, FileOffer{Name: string(rune('a'+i)) + ".txt", Size: 1024})
	}
	text := IncomingFiles("v", files, "C:\\dl").Text(testTimeout)
	if strings.Contains(text, "more files") {
		t.Fatalf("six files should all be listed, got:\n%s", text)
	}
	if !strings.Contains(text, "f.txt") {
		t.Fatal("the sixth file should be listed")
	}
}

// The operator must never mistake one question for the other.
func TestTitlesDifferPerKind(t *testing.T) {
	connect := ConnectRequest("v").Title(7)
	files := IncomingFiles("v", []FileOffer{{Name: "a.txt", Size: 1}}, "C:\\dl").Title(7)
	if connect == files {
		t.Fatalf("both prompts use the title %q", connect)
	}
	// The sequence number is what dismiss() matches on to close exactly this box.
	for _, title := range []string{connect, files} {
		if !strings.HasSuffix(title, "(#7)") {
			t.Fatalf("title %q must carry its sequence number", title)
		}
		if !strings.HasPrefix(title, "FreeDesk - ") {
			t.Fatalf("title %q lost its prefix", title)
		}
	}
}

func TestHumanSize(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 bytes"},
		{999, "999 bytes"},
		{1024, "1 KB"},
		{1048576, "1.0 MB"},
		{11010048, "10.5 MB"},
		{1610612736, "1.5 GB"},
	}
	for _, tt := range tests {
		if got := humanSize(tt.in); got != tt.want {
			t.Errorf("humanSize(%d) = %q, expected %q", tt.in, got, tt.want)
		}
	}
}

func TestTotalSize(t *testing.T) {
	p := IncomingFiles("v", []FileOffer{{Size: 10}, {Size: 32}}, "C:\\dl")
	if got := p.TotalSize(); got != 42 {
		t.Fatalf("TotalSize() = %d, expected 42", got)
	}
}
