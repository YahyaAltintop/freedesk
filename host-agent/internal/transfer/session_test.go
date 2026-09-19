package transfer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeChannel records what the host sends, standing in for the DataChannel.
type fakeChannel struct {
	mu   sync.Mutex
	sent []Msg
}

func (c *fakeChannel) Send([]byte) error { return nil }
func (c *fakeChannel) SendText(s string) error {
	var m Msg
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return err
	}
	c.mu.Lock()
	c.sent = append(c.sent, m)
	c.mu.Unlock()
	return nil
}
func (c *fakeChannel) BufferedAmount() uint64               { return 0 }
func (c *fakeChannel) SetBufferedAmountLowThreshold(uint64) {}
func (c *fakeChannel) OnBufferedAmountLow(func())           {}

// types returns the frames sent so far, progress omitted: it is timing
// dependent and not what these tests are about.
func (c *fakeChannel) types() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, m := range c.sent {
		if m.T == TypeProgress {
			continue
		}
		out = append(out, m.T)
	}
	return out
}

func (c *fakeChannel) last() (Msg, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) == 0 {
		return Msg{}, false
	}
	return c.sent[len(c.sent)-1], true
}

// waitFor polls until cond holds, so a test never depends on a fixed sleep.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

type fakeApprover struct {
	answer  bool
	asked   chan []FileOffer
	folder  string
	blockOn chan struct{} // if set, Ask waits on it before answering
}

func (a *fakeApprover) AskFiles(ctx context.Context, files []FileOffer, folder string) bool {
	a.folder = folder
	if a.asked != nil {
		a.asked <- files
	}
	if a.blockOn != nil {
		select {
		case <-a.blockOn:
		case <-ctx.Done():
			return false
		}
	}
	return a.answer
}

// newTestSession wires a session onto a throwaway directory.
func newTestSession(t *testing.T, ap Approver) (*Session, *fakeChannel, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), folderName)
	ch := &fakeChannel{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := NewSession(ctx, ap, func(string, ...any) {})
	s.dest = &Dest{root: root}
	s.Attach(ch)
	t.Cleanup(s.Close)
	return s, ch, root
}

func offer(id string, files ...FileMeta) []byte {
	return []byte(Msg{T: TypeOffer, ID: id, Dir: DirUp, Files: files}.Encode())
}

func TestAcceptedBatchIsWrittenToDisk(t *testing.T) {
	s, ch, root := newTestSession(t, &fakeApprover{answer: true})

	body := bytes.Repeat([]byte("x"), 100)
	s.Handle(offer("b1", FileMeta{Name: "report.pdf", Size: int64(len(body))}), true)
	waitFor(t, "the accept", func() bool { m, ok := ch.last(); return ok && m.T == TypeAccept })

	s.Handle(body[:60], false)
	s.Handle(body[60:], false)
	waitFor(t, "the file to be finished", func() bool { m, ok := ch.last(); return ok && m.T == TypeDone })

	got, err := os.ReadFile(filepath.Join(root, "report.pdf"))
	if err != nil {
		t.Fatalf("the file should be on disk: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("contents differ: %d bytes written, %d expected", len(got), len(body))
	}
	assertNoPartials(t, root)
}

func TestBatchOfSeveralFilesIsWrittenInOrder(t *testing.T) {
	s, ch, root := newTestSession(t, &fakeApprover{answer: true})

	s.Handle(offer("b1",
		FileMeta{Name: "a.txt", Size: 3},
		FileMeta{Name: "b.txt", Size: 5},
	), true)
	waitFor(t, "the accept", func() bool { m, ok := ch.last(); return ok && m.T == TypeAccept })

	s.Handle([]byte("aaa"), false)
	waitFor(t, "the first file", func() bool { m, ok := ch.last(); return ok && m.T == TypeDone && m.Index == 0 })
	s.Handle([]byte("bbbbb"), false)
	waitFor(t, "the second file", func() bool { m, ok := ch.last(); return ok && m.T == TypeDone && m.Index == 1 })

	for name, want := range map[string]string{"a.txt": "aaa", "b.txt": "bbbbb"} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(got) != want {
			t.Fatalf("%s contains %q, expected %q", name, got, want)
		}
	}
	assertNoPartials(t, root)
}

// The operator sees the batch exactly once, with the sanitised names and the
// destination folder.
func TestOperatorIsAskedOncePerBatch(t *testing.T) {
	ap := &fakeApprover{answer: true, asked: make(chan []FileOffer, 4)}
	s, ch, root := newTestSession(t, ap)

	s.Handle(offer("b1",
		FileMeta{Name: "a.txt", Size: 1},
		FileMeta{Name: "b.txt", Size: 1},
		FileMeta{Name: "c.txt", Size: 1},
	), true)
	waitFor(t, "the accept", func() bool { m, ok := ch.last(); return ok && m.T == TypeAccept })

	select {
	case files := <-ap.asked:
		if len(files) != 3 {
			t.Fatalf("the operator was shown %d files, expected 3", len(files))
		}
	default:
		t.Fatal("the operator was not asked")
	}
	select {
	case <-ap.asked:
		t.Fatal("the operator was asked more than once for one batch")
	default:
	}
	if ap.folder != root {
		t.Fatalf("the prompt named %q, expected %q", ap.folder, root)
	}
}

func TestDeclinedBatchWritesNothing(t *testing.T) {
	s, ch, root := newTestSession(t, &fakeApprover{answer: false})

	s.Handle(offer("b1", FileMeta{Name: "report.pdf", Size: 10}), true)
	waitFor(t, "the rejection", func() bool { m, ok := ch.last(); return ok && m.T == TypeReject })

	m, _ := ch.last()
	if m.Reason != ReasonDenied {
		t.Fatalf("reason = %q, expected %q", m.Reason, ReasonDenied)
	}
	// Payload arriving anyway must go nowhere.
	s.Handle([]byte("0123456789"), false)
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("nothing should have been created for a declined batch")
	}
}

// The declared size is a claim. What is enforced is what actually arrives.
func TestOverlongStreamIsAbortedAtTheDeclaredSize(t *testing.T) {
	s, ch, root := newTestSession(t, &fakeApprover{answer: true})

	s.Handle(offer("b1", FileMeta{Name: "small.bin", Size: 10}), true)
	waitFor(t, "the accept", func() bool { m, ok := ch.last(); return ok && m.T == TypeAccept })

	s.Handle(bytes.Repeat([]byte("x"), 8), false)
	s.Handle(bytes.Repeat([]byte("x"), 8), false) // 16 > 10
	waitFor(t, "the abort", func() bool { m, ok := ch.last(); return ok && m.T == TypeError })

	m, _ := ch.last()
	if m.Reason != ReasonTooLarge {
		t.Fatalf("reason = %q, expected %q", m.Reason, ReasonTooLarge)
	}
	if _, err := os.Stat(filepath.Join(root, "small.bin")); !os.IsNotExist(err) {
		t.Fatal("no file should exist after an over-long stream")
	}
	assertNoPartials(t, root)
}

// A batch that stops halfway leaves nothing behind, not even a partial.
func TestCancelMidTransferLeavesNothing(t *testing.T) {
	s, ch, root := newTestSession(t, &fakeApprover{answer: true})

	s.Handle(offer("b1", FileMeta{Name: "big.bin", Size: 1000}), true)
	waitFor(t, "the accept", func() bool { m, ok := ch.last(); return ok && m.T == TypeAccept })
	s.Handle(bytes.Repeat([]byte("x"), 400), false)

	s.Handle([]byte(Msg{T: TypeCancel, ID: "b1"}.Encode()), true)

	if _, err := os.Stat(filepath.Join(root, "big.bin")); !os.IsNotExist(err) {
		t.Fatal("a cancelled file must not exist")
	}
	assertNoPartials(t, root)
}

// Ending the session is the same promise as the held-key rule: nothing of the
// viewer's is left on this machine.
func TestCloseDiscardsAPartialFile(t *testing.T) {
	s, ch, root := newTestSession(t, &fakeApprover{answer: true})

	s.Handle(offer("b1", FileMeta{Name: "big.bin", Size: 1000}), true)
	waitFor(t, "the accept", func() bool { m, ok := ch.last(); return ok && m.T == TypeAccept })
	s.Handle(bytes.Repeat([]byte("x"), 400), false)

	s.Close()
	s.Close() // idempotent: the channel closing and the session ending both call it

	if _, err := os.Stat(filepath.Join(root, "big.bin")); !os.IsNotExist(err) {
		t.Fatal("an interrupted file must not exist")
	}
	assertNoPartials(t, root)
}

func TestUnsafeNameRefusesTheWholeBatch(t *testing.T) {
	ap := &fakeApprover{answer: true, asked: make(chan []FileOffer, 1)}
	s, ch, root := newTestSession(t, ap)

	s.Handle(offer("b1",
		FileMeta{Name: "fine.txt", Size: 1},
		FileMeta{Name: `..\..\evil.exe`, Size: 1},
	), true)
	waitFor(t, "the rejection", func() bool { m, ok := ch.last(); return ok && m.T == TypeReject })

	m, _ := ch.last()
	if m.Reason != ReasonBadName {
		t.Fatalf("reason = %q, expected %q", m.Reason, ReasonBadName)
	}
	select {
	case <-ap.asked:
		t.Fatal("the operator must not be asked about a batch that cannot be written")
	default:
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("nothing should have been created")
	}
}

// The operator can only answer one question at a time.
func TestSecondOfferWhileOneIsPendingIsRefused(t *testing.T) {
	release := make(chan struct{})
	ap := &fakeApprover{answer: true, asked: make(chan []FileOffer, 1), blockOn: release}
	s, ch, _ := newTestSession(t, ap)

	s.Handle(offer("b1", FileMeta{Name: "a.txt", Size: 1}), true)
	waitFor(t, "the first prompt", func() bool { return len(ap.asked) == 1 })

	s.Handle(offer("b2", FileMeta{Name: "b.txt", Size: 1}), true)
	waitFor(t, "the refusal", func() bool {
		m, ok := ch.last()
		return ok && m.T == TypeReject && m.ID == "b2"
	})
	m, _ := ch.last()
	if m.Reason != ReasonBusy {
		t.Fatalf("reason = %q, expected %q", m.Reason, ReasonBusy)
	}
	close(release)
}

// A viewer that gives up mid-prompt must not leave the question on screen.
func TestClosingCancelsAPendingPrompt(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	ap := &fakeApprover{answer: true, asked: make(chan []FileOffer, 1), blockOn: release}
	s, ch, _ := newTestSession(t, ap)

	s.Handle(offer("b1", FileMeta{Name: "a.txt", Size: 1}), true)
	waitFor(t, "the prompt", func() bool { return len(ap.asked) == 1 })

	s.Close()
	waitFor(t, "the batch to be refused", func() bool {
		m, ok := ch.last()
		return ok && m.T == TypeReject
	})
}

// Payload with nothing accepted behind it is dropped, not written.
func TestChunksWithoutAnAcceptedBatchAreDropped(t *testing.T) {
	s, _, root := newTestSession(t, &fakeApprover{answer: false})
	s.Handle([]byte("stray payload"), false)
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("a stray chunk must not create anything")
	}
}

func TestExistingFileIsNeverReplaced(t *testing.T) {
	s, ch, root := newTestSession(t, &fakeApprover{answer: true})
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(root, "report.pdf")
	if err := os.WriteFile(existing, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	s.Handle(offer("b1", FileMeta{Name: "report.pdf", Size: 3}), true)
	waitFor(t, "the accept", func() bool { m, ok := ch.last(); return ok && m.T == TypeAccept })
	s.Handle([]byte("new"), false)
	waitFor(t, "the file", func() bool { m, ok := ch.last(); return ok && m.T == TypeDone })

	got, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("the existing file was overwritten: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "report (2).pdf")); err != nil {
		t.Fatalf("the new file should have stepped aside: %v", err)
	}
}

// A malformed or foreign control frame is ignored, never acted on.
func TestMalformedControlFramesAreIgnored(t *testing.T) {
	s, ch, root := newTestSession(t, &fakeApprover{answer: true})
	for _, raw := range []string{
		`not json`,
		`{"t":"f-offer","id":"b1","dir":"up"}`,
		`{"t":"m","x":0.5,"y":0.5}`,
		fmt.Sprintf(`{"t":"f-offer","id":"b1","dir":"up","files":[{"name":"a.bin","size":%d}]}`, int64(MaxFileBytes)+1),
	} {
		s.Handle([]byte(raw), true)
	}
	time.Sleep(50 * time.Millisecond)
	if got := ch.types(); len(got) != 0 {
		t.Fatalf("expected no reply to malformed frames, got %v", got)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("nothing should have been created")
	}
}

func assertNoPartials(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		return // never created, which is also fine
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), partSuffix) {
			t.Fatalf("a partial file was left behind: %s", e.Name())
		}
	}
}
