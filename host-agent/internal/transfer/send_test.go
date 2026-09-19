package transfer

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakePicker stands in for the operator's file dialog.
type fakePicker struct {
	paths     []string
	ok        bool
	available bool
	calls     int
}

func (p *fakePicker) Pick(context.Context) ([]string, bool) {
	p.calls++
	return p.paths, p.ok
}
func (p *fakePicker) Available() bool { return p.available }

// writeTemp creates a file to be offered, and returns its path.
func writeTemp(t *testing.T, name string, size int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, bytes.Repeat([]byte("z"), size), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRequestOffersWhatTheOperatorPicked(t *testing.T) {
	path := writeTemp(t, "secret-plans.pdf", 4096)
	pk := &fakePicker{paths: []string{path}, ok: true, available: true}
	s, ch, _ := newTestSessionWith(t, &fakeApprover{}, pk)

	s.Handle([]byte(Msg{T: TypeRequest, ID: "r1"}.Encode()), true)
	waitFor(t, "the offer", func() bool { m, ok := ch.last(); return ok && m.T == TypeOffer })

	m, _ := ch.last()
	if m.Dir != DirDown || m.ID != "r1" {
		t.Fatalf("offer was %+v", m)
	}
	if len(m.Files) != 1 || m.Files[0].Size != 4096 {
		t.Fatalf("files = %+v", m.Files)
	}
	// This is the whole point of the download direction being safe: the viewer
	// learns a name, never a path.
	if m.Files[0].Name != "secret-plans.pdf" {
		t.Fatalf("name = %q, expected the base name only", m.Files[0].Name)
	}
}

// A full path would hand over the operator's user name and folder layout.
func TestOfferNeverLeaksAHostPath(t *testing.T) {
	path := writeTemp(t, "doc.txt", 10)
	pk := &fakePicker{paths: []string{path}, ok: true, available: true}
	s, ch, _ := newTestSessionWith(t, &fakeApprover{}, pk)

	s.Handle([]byte(Msg{T: TypeRequest, ID: "r1"}.Encode()), true)
	waitFor(t, "the offer", func() bool { m, ok := ch.last(); return ok && m.T == TypeOffer })

	m, _ := ch.last()
	wire := m.Encode()
	dir := filepath.Dir(path)
	if strings.Contains(wire, dir) || strings.Contains(wire, `\`) || strings.Contains(wire, "/") {
		t.Fatalf("the offer carried a path: %s", wire)
	}
}

func TestAcceptStreamsTheFileThenCompletes(t *testing.T) {
	const size = 70 * 1024 // more than two chunks
	path := writeTemp(t, "payload.bin", size)
	pk := &fakePicker{paths: []string{path}, ok: true, available: true}
	s, ch, _ := newTestSessionWith(t, &fakeApprover{}, pk)

	s.Handle([]byte(Msg{T: TypeRequest, ID: "r1"}.Encode()), true)
	waitFor(t, "the offer", func() bool { m, ok := ch.last(); return ok && m.T == TypeOffer })

	s.Handle([]byte(Msg{T: TypeAccept, ID: "r1", Index: 0}.Encode()), true)
	waitFor(t, "the completion", func() bool { m, ok := ch.last(); return ok && m.T == TypeComplete })

	if got := ch.binaryBytes(); got != size {
		t.Fatalf("streamed %d bytes, expected %d", got, size)
	}
	m, _ := ch.last()
	if m.Size != size || m.Index != 0 {
		t.Fatalf("completion was %+v", m)
	}
	// Chunks must stay within what the far end will accept.
	if max := ch.largestBinary(); max > ChunkBytes {
		t.Fatalf("a chunk was %d bytes, over the %d limit", max, ChunkBytes)
	}
}

func TestCancelledPickerIsADecline(t *testing.T) {
	pk := &fakePicker{ok: false, available: true}
	s, ch, _ := newTestSessionWith(t, &fakeApprover{}, pk)

	s.Handle([]byte(Msg{T: TypeRequest, ID: "r1"}.Encode()), true)
	waitFor(t, "the rejection", func() bool { m, ok := ch.last(); return ok && m.T == TypeReject })

	m, _ := ch.last()
	if m.Reason != ReasonDenied {
		t.Fatalf("reason = %q, expected %q", m.Reason, ReasonDenied)
	}
}

// Without a picker the host must not pretend it can offer anything.
func TestRequestWithoutAPickerIsRefused(t *testing.T) {
	s, ch, _ := newTestSessionWith(t, &fakeApprover{}, &fakePicker{available: false})

	s.Handle([]byte(Msg{T: TypeRequest, ID: "r1"}.Encode()), true)
	waitFor(t, "the refusal", func() bool { m, ok := ch.last(); return ok && m.T == TypeReject })
}

// An accept for something this host never offered must do nothing at all.
func TestAcceptForAnUnknownOfferIsIgnored(t *testing.T) {
	s, ch, _ := newTestSessionWith(t, &fakeApprover{}, &fakePicker{available: true})

	s.Handle([]byte(Msg{T: TypeAccept, ID: "nope", Index: 0}.Encode()), true)
	s.Handle([]byte(Msg{T: TypeAccept, ID: "nope", Index: 99}.Encode()), true)
	waitForQuiet()
	if got := ch.types(); len(got) != 0 {
		t.Fatalf("expected no reply, got %v", got)
	}
	if ch.binaryBytes() != 0 {
		t.Fatal("bytes were sent for an offer that does not exist")
	}
}

// The picker is what protects the operator, so it must be asked once per
// request and never re-run from a stale accept.
func TestPickerIsAskedOncePerRequest(t *testing.T) {
	path := writeTemp(t, "a.txt", 16)
	pk := &fakePicker{paths: []string{path}, ok: true, available: true}
	s, ch, _ := newTestSessionWith(t, &fakeApprover{}, pk)

	s.Handle([]byte(Msg{T: TypeRequest, ID: "r1"}.Encode()), true)
	waitFor(t, "the offer", func() bool { m, ok := ch.last(); return ok && m.T == TypeOffer })

	s.Handle([]byte(Msg{T: TypeAccept, ID: "r1", Index: 0}.Encode()), true)
	waitFor(t, "the completion", func() bool { m, ok := ch.last(); return ok && m.T == TypeComplete })

	if pk.calls != 1 {
		t.Fatalf("the picker ran %d times for one request", pk.calls)
	}
}

// A second request while one is outstanding is refused rather than queued: the
// operator cannot be shown two dialogs at once.
func TestSecondRequestWhileOneIsOutstandingIsRefused(t *testing.T) {
	path := writeTemp(t, "a.txt", 16)
	pk := &fakePicker{paths: []string{path}, ok: true, available: true}
	s, ch, _ := newTestSessionWith(t, &fakeApprover{}, pk)

	s.Handle([]byte(Msg{T: TypeRequest, ID: "r1"}.Encode()), true)
	waitFor(t, "the offer", func() bool { m, ok := ch.last(); return ok && m.T == TypeOffer })

	s.Handle([]byte(Msg{T: TypeRequest, ID: "r2"}.Encode()), true)
	waitFor(t, "the refusal", func() bool {
		m, ok := ch.last()
		return ok && m.T == TypeReject && m.ID == "r2"
	})
	m, _ := ch.last()
	if m.Reason != ReasonBusy {
		t.Fatalf("reason = %q, expected %q", m.Reason, ReasonBusy)
	}
}

// Files the operator picked that cannot be sent are dropped, and a selection
// with nothing left in it is refused rather than offered empty.
func TestUnsendableSelectionsAreRefused(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	pk := &fakePicker{paths: []string{empty, filepath.Join(dir, "gone.txt"), dir}, ok: true, available: true}
	s, ch, _ := newTestSessionWith(t, &fakeApprover{}, pk)

	s.Handle([]byte(Msg{T: TypeRequest, ID: "r1"}.Encode()), true)
	waitFor(t, "the refusal", func() bool { m, ok := ch.last(); return ok && m.T == TypeReject })
}
