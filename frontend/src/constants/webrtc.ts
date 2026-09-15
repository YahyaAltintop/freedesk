// ICE servers — the single configuration point for NAT traversal (mirrors the
// Go host's STUN-only strategy; see docs/ARCHITECTURE.md §5.2). To add TURN
// later, append an entry here — no other code changes.
export const ICE_SERVERS: RTCIceServer[] = [
  { urls: ['stun:stun.l.google.com:19302', 'stun:stun1.l.google.com:19302'] },
]

// If the peer connection is not established within this window, give up. Kept
// above the host's 45s approval window (host-agent cmd/host approvalTimeout)
// so an operator rejection always arrives before this local timeout fires.
export const CONNECTION_TIMEOUT_MS = 60_000

// A `disconnected` peer state is often transient (Wi-Fi roaming, a brief ICE
// consent lapse) and recovers by itself; wait this long before treating it as
// the end of the session.
export const DISCONNECT_GRACE_MS = 5_000
