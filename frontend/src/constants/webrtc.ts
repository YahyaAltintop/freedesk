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

// DataChannel labels (docs/PROTOCOL.md §2). Both are created by the host
// before the offer — this codebase never renegotiates, so a channel that does
// not exist by then cannot be added later. The viewer routes on these labels
// and ignores anything else a newer host might offer.
export const DATA_CHANNEL_INPUT = 'input'
export const DATA_CHANNEL_FILE = 'file'

// One binary frame's payload. The ceiling is the browser's advertised
// max-message-size; the practical limit is the host's 64 KiB read buffer,
// which a 64 KiB chunk pushes just past. Must match ChunkBytes in the Go
// transfer package.
export const CHUNK_BYTES = 32 * 1024

// Send-queue bounds. Without them a few hundred MB would be handed to the
// channel as fast as the disk can read it and sit in the browser's send buffer
// — `send()` never blocks. Stop at the high mark, resume at the low one.
export const SEND_HIGH_WATER = 1024 * 1024
export const SEND_LOW_WATER = 256 * 1024

// How long to wait for the other person to answer. Deliberately longer than
// the host's own 45 s prompt (host-agent cmd/host approvalTimeout), so their
// "no" always arrives before this gives up — otherwise a row would report
// failure while the real answer is still in flight.
export const TRANSFER_APPROVAL_TIMEOUT_MS = 60_000

// Limits, mirroring the Go transfer package. Checked here only to fail a file
// immediately instead of after a round trip; the host enforces them for real.
export const MAX_FILE_BYTES = 512 * 1024 * 1024
export const MAX_BATCH_FILES = 32
export const MAX_BATCH_BYTES = 1024 * 1024 * 1024
