//go:build cgo

package clipboard

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/macos"
)

// TestMain points every board at a pasteboard of the tests' own. The general
// one is the operator's clipboard, which a test run must not overwrite, and
// reading it can make macOS ask the operator about it.
// RC_CLIPBOARD_INTEGRATION=1 runs the tests on the real clipboard instead.
func TestMain(m *testing.M) {
	if os.Getenv("RC_CLIPBOARD_INTEGRATION") == "" {
		pasteboardName = fmt.Sprintf("app.freedesk.clipboard-test.%d", os.Getpid())
	}
	code := m.Run()
	if pasteboardName != "" {
		macos.ReleasePasteboard(pasteboardName)
	}
	os.Exit(code)
}

// testBoard is the tests' board, its pasteboard emptied.
func testBoard(t *testing.T) *macBoard {
	t.Helper()
	b, err := newPlatformBoard()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	mb := b.(*macBoard)
	if !mb.pb.WriteText("") {
		t.Fatal("could not empty the test pasteboard")
	}
	return mb
}

// TestFinderCopyIsNotText: the Finder puts a copied file's name on the
// pasteboard as text beside the file itself. It is a copy of a file, and its
// name must not reach the viewer as if it had been copied as text.
func TestFinderCopyIsNotText(t *testing.T) {
	b := testBoard(t)
	p := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !b.pb.WriteStrings(map[string]string{
		"public.file-url":        "file://" + p,
		"public.utf8-plain-text": "report.pdf",
	}) {
		t.Fatal("could not put a Finder-style copy on the pasteboard")
	}
	if text, ok, _ := b.ReadText(); ok {
		t.Fatalf("a file copy was read as the text %q", text)
	}
	paths, ok, err := b.ReadFiles()
	if err != nil || !ok || len(paths) != 1 || paths[0] != p {
		t.Fatalf("ReadFiles = %q, %v, %v; want [%s]", paths, ok, err, p)
	}
}

// TestPasswordManagerCopiesAreExcluded: what a password manager copies is
// marked concealed or transient, and never shared.
func TestPasswordManagerCopiesAreExcluded(t *testing.T) {
	b := testBoard(t)
	for _, marker := range []string{concealedType, transientType} {
		if !b.pb.WriteStrings(map[string]string{marker: "", "public.utf8-plain-text": "hunter2"}) {
			t.Fatalf("could not write a copy marked %s", marker)
		}
		if !b.Excluded() {
			t.Errorf("a copy marked %s was not excluded", marker)
		}
	}
}

// TestCopiesStayUntilAlwaysAllow: unless macOS lets the agent read the
// clipboard without asking, nothing is read, the operator is told why once,
// and where macOS has never asked it is made to, once, in the background.
func TestCopiesStayUntilAlwaysAllow(t *testing.T) {
	b := testBoard(t)
	if !b.pb.WriteText("copied on the host") {
		t.Fatal("could not write the test pasteboard")
	}
	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)
	asked := make(chan struct{}, 4)
	b.ask = func() { asked <- struct{}{} }

	for _, access := range []macos.PasteAccess{macos.PasteAsk, macos.PasteDenied} {
		b.access = func() macos.PasteAccess { return access }
		if text, ok, err := b.ReadText(); ok || err == nil {
			t.Fatalf("access %d: read %q (%v, %v), want nothing read", access, text, ok, err)
		}
	}
	b.access = func() macos.PasteAccess { return macos.PasteNeverAsked }
	b.ReadText()
	b.ReadText()
	select {
	case <-asked:
	case <-time.After(time.Second):
		t.Fatal("macOS was never made to ask about the agent")
	}
	time.Sleep(50 * time.Millisecond)
	if len(asked) != 0 {
		t.Error("macOS was made to ask more than once")
	}
	if n := strings.Count(logged.String(), "Always Allow"); n != 1 {
		t.Errorf("the operator was told %d times, want once:\n%s", n, logged.String())
	}

	b.access = func() macos.PasteAccess { return macos.PasteAllowed }
	if text, ok, err := b.ReadText(); err != nil || !ok || text != "copied on the host" {
		t.Fatalf("with Always Allow: read %q, %v, %v", text, ok, err)
	}
}

// TestWriteTextAsUsesTheSamePasteboard: the status window's Copy code button
// writes where the clipboard is read.
func TestWriteTextAsUsesTheSamePasteboard(t *testing.T) {
	b := testBoard(t)
	if err := WriteTextAs(0, "123 - 456"); err != nil {
		t.Fatal(err)
	}
	if text, ok, err := b.ReadText(); err != nil || !ok || text != "123 - 456" {
		t.Fatalf("read back %q, %v, %v", text, ok, err)
	}
}
