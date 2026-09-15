// Package signaling defines the WebRTC signaling contract (SDP + ICE exchange)
// and a Firebase Realtime Database implementation of it.
package signaling

import "context"

// Role identifies which side of the handshake a peer plays.
type Role int

const (
	// Host is the offerer: it owns the media and creates the offer.
	Host Role = iota
	// Viewer is the answerer.
	Viewer
)

// Description is an SDP offer or answer (matches the browser's RTCSessionDescription JSON).
type Description struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

// ICECandidate matches the browser's RTCIceCandidate JSON so candidates can be
// exchanged verbatim between the Go host and the Vue viewer.
type ICECandidate struct {
	Candidate        string  `json:"candidate"`
	SDPMid           *string `json:"sdpMid,omitempty"`
	SDPMLineIndex    *uint16 `json:"sdpMLineIndex,omitempty"`
	UsernameFragment *string `json:"usernameFragment,omitempty"`
}

// Transport carries the local SDP/ICE to the remote peer and surfaces the
// remote peer's SDP/ICE. Implementations map the abstract local/remote concepts
// onto concrete paths based on the peer's Role.
type Transport interface {
	PublishLocalDescription(ctx context.Context, d Description) error
	AwaitRemoteDescription(ctx context.Context) (Description, error)
	PublishLocalCandidate(ctx context.Context, c ICECandidate) error
	RemoteCandidates(ctx context.Context) (<-chan ICECandidate, error)
	Close() error
}
