package webrtc

import (
	"context"
	"fmt"
	"sync"

	"github.com/pion/interceptor"
	pion "github.com/pion/webrtc/v4"

	"github.com/yahya/remote-control-app/host-agent/internal/signaling"
)

// inputChannelLabel is the DataChannel that carries mouse/keyboard input
// (created by the host/offerer; used from Phase 6).
const inputChannelLabel = "input"

// Hooks lets the caller observe the connection without owning negotiation.
type Hooks struct {
	// OnDataChannel receives the input channel: the host's own channel for the
	// offerer, or the remotely-created channel for the answerer.
	OnDataChannel func(*pion.DataChannel)
	// OnState is invoked on every peer-connection state transition.
	OnState func(pion.PeerConnectionState)
	// OnTrack receives an inbound remote media track (used by the viewer side).
	OnTrack func(*pion.TrackRemote)
}

// Connect builds a peer for the given role, performs the full offer/answer/ICE
// exchange over the transport, and blocks until the connection is established
// (or ctx is cancelled / the connection fails).
func Connect(ctx context.Context, role signaling.Role, transport signaling.Transport, cfg Config, hooks Hooks, tracks ...pion.TrackLocal) (*pion.PeerConnection, error) {
	pc, err := pion.NewPeerConnection(pion.Configuration{ICEServers: toICEServers(cfg.ICEServers)})
	if err != nil {
		return nil, err
	}

	// Attach outbound media (the host's screen track) before negotiation so it
	// is advertised in the offer.
	for _, track := range tracks {
		sender, err := pc.AddTrack(track)
		if err != nil {
			_ = pc.Close()
			return nil, err
		}
		go drainRTCP(sender)
	}

	if hooks.OnTrack != nil {
		pc.OnTrack(func(remote *pion.TrackRemote, receiver *pion.RTPReceiver) {
			go drainRTCP(receiver)
			hooks.OnTrack(remote)
		})
	}

	connected := make(chan struct{})
	failed := make(chan error, 1)
	var once sync.Once

	pc.OnConnectionStateChange(func(s pion.PeerConnectionState) {
		if hooks.OnState != nil {
			hooks.OnState(s)
		}
		switch s {
		case pion.PeerConnectionStateConnected:
			once.Do(func() { close(connected) })
		case pion.PeerConnectionStateFailed:
			select {
			case failed <- fmt.Errorf("peer-to-peer connection could not be established (failed)"):
			default:
			}
		}
	})

	// Trickle local ICE candidates to the remote peer as they are gathered.
	pc.OnICECandidate(func(c *pion.ICECandidate) {
		if c == nil {
			return // gathering finished
		}
		init := c.ToJSON()
		go func() {
			_ = transport.PublishLocalCandidate(ctx, signaling.ICECandidate{
				Candidate:        init.Candidate,
				SDPMid:           init.SDPMid,
				SDPMLineIndex:    init.SDPMLineIndex,
				UsernameFragment: init.UsernameFragment,
			})
		}()
	})

	if role == signaling.Viewer {
		pc.OnDataChannel(func(dc *pion.DataChannel) {
			if hooks.OnDataChannel != nil {
				hooks.OnDataChannel(dc)
			}
		})
	}

	if err := negotiate(ctx, pc, role, transport, hooks); err != nil {
		_ = pc.Close()
		return nil, err
	}

	// Remote description is set by now, so it is safe to add remote candidates.
	startRemoteCandidates(ctx, pc, transport)

	select {
	case <-connected:
		return pc, nil
	case err := <-failed:
		_ = pc.Close()
		return nil, err
	case <-ctx.Done():
		_ = pc.Close()
		return nil, ctx.Err()
	}
}

func negotiate(ctx context.Context, pc *pion.PeerConnection, role signaling.Role, transport signaling.Transport, hooks Hooks) error {
	if role == signaling.Host {
		dc, err := pc.CreateDataChannel(inputChannelLabel, nil)
		if err != nil {
			return err
		}
		if hooks.OnDataChannel != nil {
			hooks.OnDataChannel(dc)
		}

		offer, err := pc.CreateOffer(nil)
		if err != nil {
			return err
		}
		if err := pc.SetLocalDescription(offer); err != nil {
			return err
		}
		if err := transport.PublishLocalDescription(ctx, signaling.Description{Type: offer.Type.String(), SDP: offer.SDP}); err != nil {
			return err
		}

		remote, err := transport.AwaitRemoteDescription(ctx)
		if err != nil {
			return err
		}
		return pc.SetRemoteDescription(pion.SessionDescription{Type: pion.NewSDPType(remote.Type), SDP: remote.SDP})
	}

	// Viewer (answerer): wait for the offer, then answer.
	remote, err := transport.AwaitRemoteDescription(ctx)
	if err != nil {
		return err
	}
	if err := pc.SetRemoteDescription(pion.SessionDescription{Type: pion.NewSDPType(remote.Type), SDP: remote.SDP}); err != nil {
		return err
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return err
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		return err
	}
	return transport.PublishLocalDescription(ctx, signaling.Description{Type: answer.Type.String(), SDP: answer.SDP})
}

func startRemoteCandidates(ctx context.Context, pc *pion.PeerConnection, transport signaling.Transport) {
	candidates, err := transport.RemoteCandidates(ctx)
	if err != nil {
		return
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case c, ok := <-candidates:
				if !ok {
					return
				}
				_ = pc.AddICECandidate(pion.ICECandidateInit{
					Candidate:        c.Candidate,
					SDPMid:           c.SDPMid,
					SDPMLineIndex:    c.SDPMLineIndex,
					UsernameFragment: c.UsernameFragment,
				})
			}
		}
	}()
}

// rtcpReader is satisfied by both *pion.RTPSender and *pion.RTPReceiver.
type rtcpReader interface {
	Read([]byte) (int, interceptor.Attributes, error)
}

// drainRTCP keeps reading inbound RTCP for a sender/receiver until it is
// stopped. Pion only runs its RTCP interceptors (NACK retransmission, receiver
// reports, ...) while somebody reads; without this loop packet loss on the
// video track is never repaired. Read returns an error once the peer
// connection is closed, so the goroutine cannot leak.
func drainRTCP(r rtcpReader) {
	buf := make([]byte, 1500)
	for {
		if _, _, err := r.Read(buf); err != nil {
			return
		}
	}
}

func toICEServers(urls []string) []pion.ICEServer {
	if len(urls) == 0 {
		return nil
	}
	return []pion.ICEServer{{URLs: urls}}
}
