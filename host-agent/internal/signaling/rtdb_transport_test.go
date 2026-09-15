package signaling

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/yahya/remote-control-app/host-agent/internal/firebase"
)

func ev(kind, path, data string) firebase.StreamEvent {
	return firebase.StreamEvent{Event: kind, Path: path, Data: json.RawMessage(data)}
}

// newOfflineTransport returns a transport whose Start is already "done", so
// the demux logic can be driven with synthetic events and no database.
func newOfflineTransport(role Role) *RTDBTransport {
	tr := NewRTDBTransport(nil, "s1", role)
	tr.startOnce.Do(func() {})
	return tr
}

func recv[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

func TestDemuxHostRole(t *testing.T) {
	tr := newOfflineTransport(Host)
	ctx := context.Background()

	// Initial sync: identity + status, no answer yet.
	tr.demux(ctx, ev("put", "/", `{"viewerUid":"v","ownerUid":"o","status":"waiting","createdAt":1}`))
	snap := recv(t, tr.Snapshot(), "snapshot")
	if snap.ViewerUID != "v" || snap.OwnerUID != "o" || snap.Status != "waiting" {
		t.Fatalf("unexpected snapshot %+v", snap)
	}
	if st := recv(t, tr.Status(), "status"); st != "waiting" {
		t.Fatalf("expected status waiting, got %q", st)
	}

	// Our own offer must be ignored; the viewer's answer must surface once.
	tr.demux(ctx, ev("put", "/offer", `{"type":"offer","sdp":"x"}`))
	tr.demux(ctx, ev("put", "/answer", `{"type":"answer","sdp":"y"}`))
	tr.demux(ctx, ev("put", "/answer", `{"type":"answer","sdp":"y"}`))
	d, err := tr.AwaitRemoteDescription(ctx)
	if err != nil || d.Type != "answer" || d.SDP != "y" {
		t.Fatalf("unexpected remote description %+v err=%v", d, err)
	}
	select {
	case extra := <-tr.remoteDesc:
		t.Fatalf("description delivered twice: %+v", extra)
	default:
	}

	// Candidates: own side ignored, remote side deduplicated by key, and a
	// full-map put (as sent on reconnect) must not replay known ones.
	tr.demux(ctx, ev("put", "/hostCandidates/h1", `{"candidate":"host-own"}`))
	tr.demux(ctx, ev("put", "/viewerCandidates/c1", `{"candidate":"cand-1"}`))
	tr.demux(ctx, ev("put", "/viewerCandidates/c1", `{"candidate":"cand-1"}`))
	tr.demux(ctx, ev("put", "/", `{"viewerUid":"v","ownerUid":"o","status":"connecting","viewerCandidates":{"c1":{"candidate":"cand-1"},"c2":{"candidate":"cand-2"}}}`))
	tr.demux(ctx, ev("patch", "/", `{"viewerCandidates/c3":{"candidate":"cand-3"},"status":"connected"}`))

	cands, _ := tr.RemoteCandidates(ctx)
	var got []string
	for range 3 {
		got = append(got, recv(t, cands, "candidate").Candidate)
	}
	if got[0] != "cand-1" || got[1] != "cand-2" || got[2] != "cand-3" {
		t.Fatalf("unexpected candidates %v", got)
	}
	select {
	case extra := <-cands:
		t.Fatalf("unexpected extra candidate %+v", extra)
	default:
	}

	// Latest status wins even though nobody drained the channel in between.
	var last string
	for len(tr.Status()) > 0 {
		last = <-tr.Status()
	}
	if last != "connected" {
		t.Fatalf("expected latest status connected, got %q", last)
	}

	// Node removed → gone.
	tr.demux(ctx, ev("put", "/", `null`))
	recv(t, tr.Gone(), "gone")
}

func TestDemuxViewerRoleReadsOffer(t *testing.T) {
	tr := newOfflineTransport(Viewer)
	ctx := context.Background()
	tr.demux(ctx, ev("put", "/", `{"viewerUid":"v","ownerUid":"o","status":"connecting","offer":{"type":"offer","sdp":"o1"},"hostCandidates":{"a":{"candidate":"hc"}}}`))
	d, err := tr.AwaitRemoteDescription(ctx)
	if err != nil || d.Type != "offer" || d.SDP != "o1" {
		t.Fatalf("unexpected remote description %+v err=%v", d, err)
	}
	cands, _ := tr.RemoteCandidates(ctx)
	if c := recv(t, cands, "candidate"); c.Candidate != "hc" {
		t.Fatalf("unexpected candidate %+v", c)
	}
}

func TestStatusChannelIsLossyButKeepsNewest(t *testing.T) {
	tr := newOfflineTransport(Host)
	for i := range 40 {
		tr.emitStatus(string(rune('a' + i%26)))
	}
	tr.emitStatus("final")
	var last string
	for len(tr.status) > 0 {
		last = <-tr.status
	}
	if last != "final" {
		t.Fatalf("expected newest status to survive, got %q", last)
	}
}
