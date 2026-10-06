package webrtc

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtcp"
	pion "github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/signaling"
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

// TestHostReceivesPLI covers the keyframe-on-demand path: the host's offer
// advertises PLI feedback (without which Chrome would never send it), and a
// Picture Loss Indication arriving on the video sender reaches the OnPLI
// hook. The browser's own decision to send PLI on loss is not exercised here;
// the two-PC manual test watches chrome://webrtc-internals for that.
func TestHostReceivesPLI(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	hostT, viewerT := newMemPair()
	cfg := Config{}

	track, err := NewVideoTrack()
	if err != nil {
		t.Fatalf("could not create track: %v", err)
	}

	pli := make(chan struct{}, 1)
	remoteSSRC := make(chan uint32, 1)

	var (
		wg                 sync.WaitGroup
		hostPC, viewerPC   *pion.PeerConnection
		hostErr, viewerErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		hostPC, hostErr = Connect(ctx, signaling.Host, hostT, cfg, Hooks{
			OnPLI: func() {
				select {
				case pli <- struct{}{}:
				default:
				}
			},
		}, track)
	}()
	go func() {
		defer wg.Done()
		viewerPC, viewerErr = Connect(ctx, signaling.Viewer, viewerT, cfg, Hooks{
			OnTrack: func(remote *pion.TrackRemote) {
				select {
				case remoteSSRC <- uint32(remote.SSRC()):
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

	// The host is the offerer: its offer must advertise PLI feedback, or a
	// real browser would never send the report the rest of this exercises.
	if sdp := hostPC.LocalDescription().SDP; !strings.Contains(sdp, "nack pli") {
		t.Fatal("the host's offer does not advertise 'nack pli' feedback; Chrome would never send a PLI")
	}

	// Media so the viewer receives the track and learns its SSRC.
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

	var ssrc uint32
	select {
	case ssrc = <-remoteSSRC:
	case <-time.After(10 * time.Second):
		t.Fatal("viewer never received the track")
	}

	// Send the loss report repeatedly: the first may race the host's RTCP
	// read loop coming up, and a lost one would just be resent in reality.
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = viewerPC.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: ssrc}})
			}
		}
	}()

	select {
	case <-pli:
	case <-time.After(10 * time.Second):
		t.Fatal("the host never saw the PLI")
	}
}
