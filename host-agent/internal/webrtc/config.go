// Package webrtc wraps Pion to negotiate a peer connection over a pluggable
// signaling transport.
package webrtc

// DefaultICEServers lists the public STUN servers used for NAT discovery.
// Per the project's "no external server" rule we use STUN only — STUN is not a
// relay; media/data still flow peer-to-peer. A TURN entry can be appended here
// later (with credentials) without touching any other code.
var DefaultICEServers = []string{
	"stun:stun.l.google.com:19302",
	"stun:stun1.l.google.com:19302",
}

// Config holds the ICE configuration for a peer connection.
type Config struct {
	ICEServers []string
}

// DefaultConfig returns the STUN-only configuration.
func DefaultConfig() Config {
	return Config{ICEServers: DefaultICEServers}
}
