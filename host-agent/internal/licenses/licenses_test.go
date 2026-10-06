package licenses

import (
	"strings"
	"testing"
)

// TestText holds in both kinds of build: a developer build has no texts and
// says so; a build after cmd/notices carries FreeDesk's own license first and
// the libvpx one among the rest.
func TestText(t *testing.T) {
	text, ok := Text()
	if !ok {
		if text != "" {
			t.Fatalf("no licenses reported, but text %q", text)
		}
		t.Log("developer build: no LICENSES.txt generated (go run ./cmd/notices -o internal/licenses/generated/LICENSES.txt)")
		return
	}
	for _, want := range []string{"FreeDesk", "MIT License", "libvpx"} {
		if !strings.Contains(text, want) {
			t.Errorf("the license texts do not mention %q", want)
		}
	}
	if !strings.Contains(text, "\r\n") {
		t.Error("the texts must have Windows line endings, for the window and for Notepad")
	}
}
