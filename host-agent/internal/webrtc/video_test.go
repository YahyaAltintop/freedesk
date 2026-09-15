package webrtc

import (
	"context"
	"sync"
	"testing"
	"time"

	pion "github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"github.com/yahya/remote-control-app/host-agent/internal/signaling"
)

// TestConnectWithVideoTrack verifies that a video track added on the host is
// negotiated and that the viewer receives VP8 RTP for it.
func TestConnectWithVideoTrack(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	hostT, viewerT := newMemPair()
	cfg := Config{}

	track, err := NewVideoTrack()
	if err != nil {
		t.Fatalf("could not create track: %v", err)
	}

	gotCodec := make(chan string, 1)

	var (
		wg                 sync.WaitGroup
		hostPC, viewerPC   *pion.PeerConnection
		hostErr, viewerErr error
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		hostPC, hostErr = Connect(ctx, signaling.Host, hostT, cfg, Hooks{}, track)
	}()
	go func() {
		defer wg.Done()
		viewerPC, viewerErr = Connect(ctx, signaling.Viewer, viewerT, cfg, Hooks{
			OnTrack: func(remote *pion.TrackRemote) {
				select {
				case gotCodec <- remote.Codec().MimeType:
				default:
				}
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

	// Push fake VP8 samples so the viewer receives RTP and fires OnTrack.
	go func() {
		sample := media.Sample{Data: []byte{0x10, 0x00, 0x00, 0x00, 0x00}, Duration: 33 * time.Millisecond}
		ticker := time.NewTicker(33 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := track.WriteSample(sample); err != nil {
					return
				}
			}
		}
	}()

	select {
	case codec := <-gotCodec:
		if codec != pion.MimeTypeVP8 {
			t.Fatalf("expected %s, got %s", pion.MimeTypeVP8, codec)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("viewer did not receive the video track in time")
	}
}
