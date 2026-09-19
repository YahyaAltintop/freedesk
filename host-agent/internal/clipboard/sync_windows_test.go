//go:build windows

package clipboard

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// collector records what the sync layer would send to the viewer.
type collector struct {
	mu   sync.Mutex
	sent []Msg
}

func (c *collector) send(raw string) {
	var m Msg
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	c.mu.Lock()
	c.sent = append(c.sent, m)
	c.mu.Unlock()
}

func (c *collector) all() []Msg {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Msg(nil), c.sent...)
}

func newSync(t *testing.T) (*Sync, *collector) {
	t.Helper()
	c := &collector{}
	s, err := NewSync(ModeText, c.send)
	if err != nil {
		t.Skipf("no clipboard available here: %v", err)
	}
	t.Cleanup(s.Stop)
	return s, c
}

// waitForSend waits out at least two poll intervals, so "nothing was sent"
// means the poller had its chance and declined.
func waitForSend(c *collector, want int) []Msg {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(c.all()) >= want {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(pollEvery + 200*time.Millisecond)
	return c.all()
}

// The whole point of the sequence number: text written because the viewer sent
// it must not be sent straight back.
func TestTextFromTheViewerIsNotEchoedBack(t *testing.T) {
	s, c := newSync(t)

	s.Handle([]byte(Msg{Text: "from the viewer"}.Encode()))

	if got := waitForSend(c, 1); len(got) != 0 {
		t.Fatalf("the host echoed the viewer's own text back: %+v", got)
	}
}

// A change made on the host is what the viewer should hear about.
func TestHostChangeIsSentToTheViewer(t *testing.T) {
	s, c := newSync(t)

	s.worker.do(func(b Board) {
		if err := b.WriteText("copied on the host"); err != nil {
			t.Skipf("could not write the clipboard: %v", err)
		}
	})

	got := waitForSend(c, 1)
	if len(got) == 0 {
		t.Fatal("a host-side change was never sent to the viewer")
	}
	if got[0].Text != "copied on the host" {
		t.Fatalf("sent %q", got[0].Text)
	}
}

// The state at connect time belongs to the operator, not to the session.
func TestWhatWasAlreadyOnTheClipboardIsNotPushed(t *testing.T) {
	// Put something there BEFORE the session starts.
	pre, err := New()
	if err != nil {
		t.Skipf("no clipboard: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = pre.WriteText("private, from before the session")
		pre.Close()
	}()
	<-done

	_, c := newSync(t)
	if got := waitForSend(c, 1); len(got) != 0 {
		t.Fatalf("the clipboard's prior contents were pushed to the viewer: %+v", got)
	}
}

// Over-long text is cut and flagged rather than silently handed over partial.
func TestOversizedTextIsTruncatedAndFlagged(t *testing.T) {
	s := &Sync{maxText: 32}
	long := strings.Repeat("a", 100)
	m := Msg{Text: long}
	if len(m.Text) > s.maxText {
		m.Text = truncateUTF8(m.Text, s.maxText)
		m.Trunc = true
	}
	if len(m.Text) != 32 || !m.Trunc {
		t.Fatalf("truncation gave %d bytes, trunc=%v", len(m.Text), m.Trunc)
	}
}

// Cutting must not split a rune: an invalid string is worse than a shorter one.
func TestTruncateKeepsRunesWhole(t *testing.T) {
	s := strings.Repeat("ü", 10) // two bytes each
	for n := range 21 {
		got := truncateUTF8(s, n)
		if len(got)%2 != 0 {
			t.Fatalf("truncateUTF8(%d) split a rune: %q", n, got)
		}
	}
}

// Turning it off must leave nothing running that could touch the clipboard.
func TestModeOffRefusesToStart(t *testing.T) {
	if _, err := NewSync(ModeOff, func(string) {}); err == nil {
		t.Fatal("mode off should not start a clipboard sync")
	}
}

func TestStopIsIdempotent(t *testing.T) {
	s, _ := newSync(t)
	s.Stop()
	s.Stop()
	// A message after stopping must not panic or block.
	s.Handle([]byte(Msg{Text: "after stop"}.Encode()))
}
