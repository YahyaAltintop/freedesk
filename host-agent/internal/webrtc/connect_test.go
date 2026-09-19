package webrtc

import (
	"context"
	"sync"
	"testing"
	"time"

	pion "github.com/pion/webrtc/v4"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/signaling"
)

// memTransport is an in-memory signaling.Transport that connects two peers in
// the same process (no network), exercising the full offer/answer/ICE flow.
type memTransport struct {
	localDesc  chan signaling.Description
	remoteDesc chan signaling.Description
	localCand  chan signaling.ICECandidate
	remoteCand chan signaling.ICECandidate
}

func newMemPair() (host, viewer *memTransport) {
	offer := make(chan signaling.Description, 1)
	answer := make(chan signaling.Description, 1)
	hostCand := make(chan signaling.ICECandidate, 64)
	viewerCand := make(chan signaling.ICECandidate, 64)
	host = &memTransport{localDesc: offer, remoteDesc: answer, localCand: hostCand, remoteCand: viewerCand}
	viewer = &memTransport{localDesc: answer, remoteDesc: offer, localCand: viewerCand, remoteCand: hostCand}
	return host, viewer
}

func (m *memTransport) PublishLocalDescription(_ context.Context, d signaling.Description) error {
	m.localDesc <- d
	return nil
}

func (m *memTransport) AwaitRemoteDescription(ctx context.Context) (signaling.Description, error) {
	select {
	case d := <-m.remoteDesc:
		return d, nil
	case <-ctx.Done():
		return signaling.Description{}, ctx.Err()
	}
}

func (m *memTransport) PublishLocalCandidate(_ context.Context, c signaling.ICECandidate) error {
	m.localCand <- c
	return nil
}

func (m *memTransport) RemoteCandidates(_ context.Context) (<-chan signaling.ICECandidate, error) {
	return m.remoteCand, nil
}

func (m *memTransport) Close() error { return nil }

// frame is one DataChannel message, keeping the text/binary flag the receiving
// side has to dispatch on.
type frame struct {
	data   string
	isText bool
}

// channelSet collects the channels one peer ends up with, keyed by label, so a
// test can wait for each of them by name instead of by arrival order.
type channelSet struct {
	mu     sync.Mutex
	opened map[string]chan struct{}
	got    map[string]*pion.DataChannel
}

func newChannelSet(labels ...string) *channelSet {
	s := &channelSet{opened: map[string]chan struct{}{}, got: map[string]*pion.DataChannel{}}
	for _, l := range labels {
		s.opened[l] = make(chan struct{})
	}
	return s
}

// add registers a channel and closes its "opened" gate once it opens. Unknown
// labels are ignored rather than panicking, so a future channel added to
// hostChannelLabels does not break every test at once.
func (s *channelSet) add(dc *pion.DataChannel) {
	s.mu.Lock()
	gate, known := s.opened[dc.Label()]
	if known {
		s.got[dc.Label()] = dc
	}
	s.mu.Unlock()
	if known {
		dc.OnOpen(func() { close(gate) })
	}
}

func (s *channelSet) channel(label string) *pion.DataChannel {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.got[label]
}

// TestConnectInMemory connects two real Pion peers through the in-memory
// transport and verifies that BOTH DataChannels open and that text and binary
// frames arrive with IsString set correctly — the flag the session router
// dispatches on to tell a control frame from a file chunk.
func TestConnectInMemory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	hostT, viewerT := newMemPair()
	cfg := Config{} // localhost host candidates connect directly; no STUN needed

	hostChans := newChannelSet(hostChannelLabels...)
	viewerChans := newChannelSet(hostChannelLabels...)
	inputMsg := make(chan frame, 4)
	fileMsg := make(chan frame, 4)

	var (
		wg                 sync.WaitGroup
		hostPC, viewerPC   *pion.PeerConnection
		hostErr, viewerErr error
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		hostPC, hostErr = Connect(ctx, signaling.Host, hostT, cfg, Hooks{
			OnDataChannel: hostChans.add,
		})
	}()
	go func() {
		defer wg.Done()
		viewerPC, viewerErr = Connect(ctx, signaling.Viewer, viewerT, cfg, Hooks{
			OnDataChannel: func(dc *pion.DataChannel) {
				viewerChans.add(dc)
				sink := inputMsg
				if dc.Label() == FileChannelLabel {
					sink = fileMsg
				}
				dc.OnMessage(func(msg pion.DataChannelMessage) {
					sink <- frame{data: string(msg.Data), isText: msg.IsString}
				})
			},
		})
	}()
	wg.Wait()

	if hostErr != nil {
		t.Fatalf("host could not connect: %v", hostErr)
	}
	if viewerErr != nil {
		t.Fatalf("viewer could not connect: %v", viewerErr)
	}
	defer hostPC.Close()
	defer viewerPC.Close()

	for _, label := range hostChannelLabels {
		mustClose(t, "host DataChannel "+label, hostChans.opened[label])
		mustClose(t, "viewer DataChannel "+label, viewerChans.opened[label])
	}

	if err := hostChans.channel(InputChannelLabel).SendText("hello"); err != nil {
		t.Fatalf("could not send on the input channel: %v", err)
	}
	mustReceive(t, "input", inputMsg, frame{data: "hello", isText: true})

	// The file channel carries both kinds, and the receiver can only tell them
	// apart by IsString.
	fileDC := hostChans.channel(FileChannelLabel)
	if err := fileDC.SendText(`{"t":"f-offer"}`); err != nil {
		t.Fatalf("could not send a control frame: %v", err)
	}
	mustReceive(t, "file control", fileMsg, frame{data: `{"t":"f-offer"}`, isText: true})

	if err := fileDC.Send([]byte{0x00, 0x01, 0x02}); err != nil {
		t.Fatalf("could not send a binary chunk: %v", err)
	}
	mustReceive(t, "file chunk", fileMsg, frame{data: "\x00\x01\x02", isText: false})
}

// mustReceive waits for one frame and fails unless it matches, flag included.
func mustReceive(t *testing.T, name string, ch <-chan frame, want frame) {
	t.Helper()
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("%s: expected %q (isText=%v), got %q (isText=%v)",
				name, want.data, want.isText, got.data, got.isText)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: no message arrived in time", name)
	}
}

// mustClose waits for a channel to close, failing the test on timeout.
func mustClose(t *testing.T, name string, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(8 * time.Second):
		t.Fatalf("%s did not open in time", name)
	}
}
