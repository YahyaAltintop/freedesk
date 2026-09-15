package webrtc

import (
	"context"
	"sync"
	"testing"
	"time"

	pion "github.com/pion/webrtc/v4"

	"github.com/yahya/remote-control-app/host-agent/internal/signaling"
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

// TestConnectInMemory connects two real Pion peers through the in-memory
// transport and verifies the DataChannel opens and carries a message.
func TestConnectInMemory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	hostT, viewerT := newMemPair()
	cfg := Config{} // localhost host candidates connect directly; no STUN needed

	hostOpen := make(chan struct{})
	viewerOpen := make(chan struct{})
	viewerMsg := make(chan string, 1)

	var (
		wg                 sync.WaitGroup
		hostPC, viewerPC   *pion.PeerConnection
		hostErr, viewerErr error
		hostDC             *pion.DataChannel
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		hostPC, hostErr = Connect(ctx, signaling.Host, hostT, cfg, Hooks{
			OnDataChannel: func(dc *pion.DataChannel) {
				hostDC = dc
				dc.OnOpen(func() { close(hostOpen) })
			},
		})
	}()
	go func() {
		defer wg.Done()
		viewerPC, viewerErr = Connect(ctx, signaling.Viewer, viewerT, cfg, Hooks{
			OnDataChannel: func(dc *pion.DataChannel) {
				dc.OnOpen(func() { close(viewerOpen) })
				dc.OnMessage(func(msg pion.DataChannelMessage) { viewerMsg <- string(msg.Data) })
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

	mustClose(t, "host DataChannel", hostOpen)
	mustClose(t, "viewer DataChannel", viewerOpen)

	if err := hostDC.SendText("hello"); err != nil {
		t.Fatalf("could not send message: %v", err)
	}
	select {
	case got := <-viewerMsg:
		if got != "hello" {
			t.Fatalf("expected %q, got %q", "hello", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("viewer did not receive the message in time")
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
