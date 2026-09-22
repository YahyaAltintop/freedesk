package webrtc

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/pion/interceptor"
	pion "github.com/pion/webrtc/v4"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/signaling"
)

const (
	// InputChannelLabel carries mouse/keyboard input and the small control
	// messages that go with a session (viewer → host, plus the host's hello).
	InputChannelLabel = "input"
	// FileChannelLabel carries file transfers in both directions: JSON control
	// frames and raw binary chunks. File bytes get a channel of their own so a
	// transfer cannot queue ahead of a mouse move on the reliable, ordered
	// input channel and make remote control unusable for its duration.
	FileChannelLabel = "file"
)

// hostChannelLabels are created, in this order, before the offer. There is no
// renegotiation anywhere in this codebase, so every channel a session will ever
// use has to exist by the time the offer is built.
//
// "input" is created LAST on purpose. A viewer from before the file channel
// existed assigns every channel it is offered to its single input reference,
// so the last one to arrive is the one it keeps. Channel announcements travel
// on separate SCTP streams and are not ordered against each other, so this is
// defence in depth rather than a guarantee: the actual protection is releasing
// the web app before the agent that offers two channels.
var hostChannelLabels = []string{FileChannelLabel, InputChannelLabel}

// Hooks lets the caller observe the connection without owning negotiation.
type Hooks struct {
	// OnDataChannel receives each DataChannel of the session: the host's own
	// channels for the offerer, or the remotely-created ones for the answerer.
	// It fires once per channel, so callers dispatch on dc.Label().
	OnDataChannel func(*pion.DataChannel)
	// OnState is invoked on every peer-connection state transition.
	OnState func(pion.PeerConnectionState)
	// OnTrack receives an inbound remote media track (used by the viewer side).
	OnTrack func(*pion.TrackRemote)
}

// Peer is a peer connection between being built and being negotiated: its
// tracks and channels are attached and, for the host, its ICE candidates are
// already being gathered, but nothing has reached the other side. The host
// prepares one while its operator is still deciding and negotiates it once
// they say yes, so the STUN round trips and the connection set-up overlap
// with the prompt instead of following it. A prepared peer that is never
// negotiated must still be Closed.
type Peer struct {
	pc    *pion.PeerConnection
	role  signaling.Role
	hooks Hooks

	connected chan struct{}
	failed    chan error
	closed    chan struct{}
	closeOnce sync.Once

	mu sync.Mutex
	// publish is where local candidates go once Negotiate has a transport;
	// until then they wait in pending.
	publish func(signaling.ICECandidate)
	pending []signaling.ICECandidate
	// offer is the host's, created by Prepare and published by Negotiate.
	offer *pion.SessionDescription

	stats candidateStats
}

// candidateStats counts the candidates each side produced, by type. When a
// connection fails, which kinds were there says most of what there is to
// know: no server-reflexive candidate means the STUN server was out of reach;
// only mDNS names from the viewer means its LAN addresses could not be
// resolved; and a full set on both sides with no connection points at a
// firewall.
type candidateStats struct {
	mu     sync.Mutex
	local  map[string]int
	remote map[string]int
}

func (s *candidateStats) note(side *map[string]int, candidate string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if *side == nil {
		*side = make(map[string]int)
	}
	(*side)[candidateType(candidate)]++
}

func (s *candidateStats) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return "local: " + describe(s.local) + "; remote: " + describe(s.remote)
}

func describe(counts map[string]int) string {
	if len(counts) == 0 {
		return "none"
	}
	types := make([]string, 0, len(counts))
	for t := range counts {
		types = append(types, t)
	}
	sort.Strings(types)
	parts := make([]string, len(types))
	for i, t := range types {
		parts[i] = fmt.Sprintf("%s=%d", t, counts[t])
	}
	return strings.Join(parts, " ")
}

// candidateType reads the type off an ICE candidate line ("... typ host ..."),
// reporting a host candidate that carries a multicast-DNS name rather than an
// address as "mdns", which is how browsers hide LAN addresses.
func candidateType(candidate string) string {
	fields := strings.Fields(candidate)
	typ := "unknown"
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "typ" {
			typ = fields[i+1]
			break
		}
	}
	if typ == "host" && len(fields) > 4 && strings.HasSuffix(fields[4], ".local") {
		return "mdns"
	}
	return typ
}

// Summary describes the candidates seen so far, for the developer's channel
// when a connection could not be made.
func (p *Peer) Summary() string { return p.stats.String() }

// settingEngine turns the parts of Config that pion takes through its
// SettingEngine into one. Everything not mentioned keeps pion's default.
func settingEngine(cfg Config) pion.SettingEngine {
	var se pion.SettingEngine
	if cfg.UDPMux != nil {
		se.SetICEUDPMux(cfg.UDPMux)
	}
	return se
}

// Connect builds a peer for the given role, performs the full offer/answer/ICE
// exchange over the transport, and blocks until the connection is established
// (or ctx is cancelled / the connection fails). It is Prepare followed at once
// by Negotiate; a caller with something to do in between calls the two itself.
func Connect(ctx context.Context, role signaling.Role, transport signaling.Transport, cfg Config, hooks Hooks, tracks ...pion.TrackLocal) (*pion.PeerConnection, error) {
	peer, err := Prepare(ctx, role, cfg, hooks, tracks...)
	if err != nil {
		return nil, err
	}
	return peer.Negotiate(ctx, transport)
}

// Prepare builds the peer connection: tracks attached, the host's channels
// created and — for the host — the offer set as the local description, which
// is what starts ICE gathering. Nothing is published: candidates are held
// until Negotiate has a transport to put them on.
func Prepare(ctx context.Context, role signaling.Role, cfg Config, hooks Hooks, tracks ...pion.TrackLocal) (*Peer, error) {
	api := pion.NewAPI(pion.WithSettingEngine(settingEngine(cfg)))
	pc, err := api.NewPeerConnection(pion.Configuration{ICEServers: toICEServers(cfg.ICEServers)})
	if err != nil {
		return nil, err
	}
	p := &Peer{
		pc:        pc,
		role:      role,
		hooks:     hooks,
		connected: make(chan struct{}),
		failed:    make(chan error, 1),
		closed:    make(chan struct{}),
	}

	// Attach outbound media (the host's screen track) before negotiation so it
	// is advertised in the offer.
	for _, track := range tracks {
		sender, err := pc.AddTrack(track)
		if err != nil {
			p.Close()
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

	var once sync.Once
	pc.OnConnectionStateChange(func(s pion.PeerConnectionState) {
		if hooks.OnState != nil {
			hooks.OnState(s)
		}
		switch s {
		case pion.PeerConnectionStateConnected:
			once.Do(func() { close(p.connected) })
		case pion.PeerConnectionStateFailed:
			select {
			case p.failed <- fmt.Errorf("peer-to-peer connection could not be established (failed)"):
			default:
			}
		}
	})

	// Trickle local ICE candidates to the remote peer as they are gathered —
	// or hold them, while there is nowhere to send them yet.
	pc.OnICECandidate(func(c *pion.ICECandidate) {
		if c == nil {
			return // gathering finished
		}
		init := c.ToJSON()
		p.candidate(signaling.ICECandidate{
			Candidate:        init.Candidate,
			SDPMid:           init.SDPMid,
			SDPMLineIndex:    init.SDPMLineIndex,
			UsernameFragment: init.UsernameFragment,
		})
	})

	if role == signaling.Viewer {
		pc.OnDataChannel(func(dc *pion.DataChannel) {
			if hooks.OnDataChannel != nil {
				hooks.OnDataChannel(dc)
			}
		})
		return p, nil
	}

	for _, label := range hostChannelLabels {
		dc, err := pc.CreateDataChannel(label, nil)
		if err != nil {
			p.Close()
			return nil, err
		}
		if hooks.OnDataChannel != nil {
			hooks.OnDataChannel(dc)
		}
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		p.Close()
		return nil, err
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		p.Close()
		return nil, err
	}
	p.offer = &offer
	return p, nil
}

// candidate takes one locally gathered candidate: straight to the transport
// once there is one, otherwise into the queue Negotiate flushes.
func (p *Peer) candidate(c signaling.ICECandidate) {
	p.stats.note(&p.stats.local, c.Candidate)
	p.mu.Lock()
	publish := p.publish
	if publish == nil {
		p.pending = append(p.pending, c)
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	go publish(c)
}

// startPublishing routes candidates to the transport from now on and sends
// the ones gathered so far. Called only once the local description has been
// published: a candidate must never overtake the description it belongs to.
func (p *Peer) startPublishing(ctx context.Context, transport signaling.Transport) {
	publish := func(c signaling.ICECandidate) {
		_ = transport.PublishLocalCandidate(ctx, c)
	}
	p.mu.Lock()
	p.publish = publish
	pending := p.pending
	p.pending = nil
	p.mu.Unlock()
	for _, c := range pending {
		go publish(c)
	}
}

// Negotiate performs the offer/answer exchange over the transport, trickles
// candidates both ways and blocks until the connection is established (or ctx
// is cancelled / the connection fails). The peer is closed on any failure.
func (p *Peer) Negotiate(ctx context.Context, transport signaling.Transport) (*pion.PeerConnection, error) {
	if err := p.negotiate(ctx, transport); err != nil {
		p.Close()
		return nil, err
	}

	// Remote description is set by now, so it is safe to add remote candidates.
	p.startRemoteCandidates(ctx, transport)

	select {
	case <-p.connected:
		return p.pc, nil
	case err := <-p.failed:
		p.Close()
		return nil, err
	case <-ctx.Done():
		p.Close()
		return nil, ctx.Err()
	}
}

func (p *Peer) negotiate(ctx context.Context, transport signaling.Transport) error {
	if p.role == signaling.Host {
		if err := transport.PublishLocalDescription(ctx, signaling.Description{Type: p.offer.Type.String(), SDP: p.offer.SDP}); err != nil {
			return err
		}
		p.startPublishing(ctx, transport)

		remote, err := transport.AwaitRemoteDescription(ctx)
		if err != nil {
			return err
		}
		return p.pc.SetRemoteDescription(pion.SessionDescription{Type: pion.NewSDPType(remote.Type), SDP: remote.SDP})
	}

	// Viewer (answerer): wait for the offer, then answer.
	remote, err := transport.AwaitRemoteDescription(ctx)
	if err != nil {
		return err
	}
	if err := p.pc.SetRemoteDescription(pion.SessionDescription{Type: pion.NewSDPType(remote.Type), SDP: remote.SDP}); err != nil {
		return err
	}

	answer, err := p.pc.CreateAnswer(nil)
	if err != nil {
		return err
	}
	if err := p.pc.SetLocalDescription(answer); err != nil {
		return err
	}
	if err := transport.PublishLocalDescription(ctx, signaling.Description{Type: answer.Type.String(), SDP: answer.SDP}); err != nil {
		return err
	}
	p.startPublishing(ctx, transport)
	return nil
}

// Close releases the peer connection. Safe to call more than once, and after
// a successful Negotiate — which is how a session ends.
func (p *Peer) Close() {
	p.closeOnce.Do(func() {
		close(p.closed)
		_ = p.pc.Close()
	})
}

// Closed reports whether Close has been called.
func (p *Peer) Closed() bool {
	select {
	case <-p.closed:
		return true
	default:
		return false
	}
}

func (p *Peer) startRemoteCandidates(ctx context.Context, transport signaling.Transport) {
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
				p.stats.note(&p.stats.remote, c.Candidate)
				_ = p.pc.AddICECandidate(pion.ICECandidateInit{
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
