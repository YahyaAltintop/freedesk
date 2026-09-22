package session

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	pion "github.com/pion/webrtc/v4"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/clipboard"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/signaling"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/webrtc"
)

// fakeClip stands in for the clipboard worker and only remembers whether it
// was stopped.
type fakeClip struct{ stopped atomic.Bool }

func (f *fakeClip) OnFiles(func([]string)) {}
func (f *fakeClip) PutFiles([]string)      {}
func (f *fakeClip) Handle([]byte)          {}
func (f *fakeClip) Stop()                  { f.stopped.Store(true) }

// A connection that never comes up must still let go of everything the
// session started for it. pion fires a channel's OnClose only for a channel
// that opened, so a session that relied on those handlers leaked its
// clipboard worker — a locked OS thread with a 25 ms ticker — on every
// attempt the viewer withdrew or ICE could not complete.
func TestFailedNegotiationReleasesTheSession(t *testing.T) {
	orig := negotiatePeer
	negotiatePeer = func(context.Context, *webrtc.Peer, signaling.Transport) (*pion.PeerConnection, error) {
		return nil, errors.New("peer-to-peer connection could not be established")
	}
	defer func() { negotiatePeer = orig }()

	clip := &fakeClip{}
	c := &Coordinator{
		clipboardMode: clipboard.ModeText,
		newClipboard:  func(func(string)) (clipboardSync, error) { return clip, nil },
	}
	req := Request{ID: "s1", ViewerUID: "viewer"}
	ctx := context.Background()

	s, err := c.prepare(ctx, req, nil)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if s.peer.Closed() {
		t.Fatal("the peer must stay open while the operator decides")
	}

	// The transport is never touched: negotiation fails before it is used.
	c.run(ctx, req, signaling.NewRTDBTransport(nil, req.ID, signaling.Host), s)

	if !clip.stopped.Load() {
		t.Fatal("the clipboard worker kept running after the connection failed")
	}
	if !s.peer.Closed() {
		t.Fatal("the peer connection was left open after the connection failed")
	}
	// Releasing again — Run's own deferred release — must be harmless.
	s.release()
}

// A request that is refused, or withdrawn while the prompt is up, closes the
// peer that was prepared for it.
func TestReleasingAPreparedSessionClosesItsPeer(t *testing.T) {
	c := &Coordinator{clipboardMode: clipboard.ModeOff}
	s, err := c.prepare(context.Background(), Request{ID: "s2", ViewerUID: "viewer"}, nil)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	s.release()
	if !s.peer.Closed() {
		t.Fatal("release left the peer open")
	}
}

// Three connection prompts in a row without a yes is the signal; it is raised
// once, and the count starts over so the next streak is judged on its own.
func TestRepeatedRefusalsAreReportedOncePerStreak(t *testing.T) {
	c := &Coordinator{}
	for i := 1; i < maxUnapprovedConnects; i++ {
		if c.noteConnectOutcome(false) {
			t.Fatalf("reported after %d refusal(s), expected %d", i, maxUnapprovedConnects)
		}
	}
	if !c.noteConnectOutcome(false) {
		t.Fatalf("not reported at %d refusals in a row", maxUnapprovedConnects)
	}
	if c.noteConnectOutcome(false) {
		t.Fatal("reported again on the very next refusal; a streak must start over")
	}
}

// A yes in the middle means the earlier refusals were not a campaign. Only
// refusals that follow one another count.
func TestAnApprovalEndsTheStreak(t *testing.T) {
	c := &Coordinator{}
	c.noteConnectOutcome(false)
	c.noteConnectOutcome(false)
	if c.noteConnectOutcome(true) {
		t.Fatal("a yes can never be the refusal that reaches the limit")
	}
	// Two more refusals after the yes. Both calls must run (kept out of a
	// short-circuiting ||) so the count actually reaches two, and neither may
	// report the limit yet.
	first := c.noteConnectOutcome(false)
	second := c.noteConnectOutcome(false)
	if first || second {
		t.Fatal("the refusals before the yes were still being counted")
	}
	if !c.noteConnectOutcome(false) {
		t.Fatal("three refusals after the yes should have reached the limit")
	}
}
