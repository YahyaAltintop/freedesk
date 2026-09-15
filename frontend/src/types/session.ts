import type { Description } from '@/types/signaling'

// Lifecycle status of a connection session.
export type SessionStatus = 'waiting' | 'connecting' | 'connected' | 'ended'

// Domain model for a connection session (mirrors `/sessions/{sessionId}`,
// plus the pairing code that only lives in the owner's inbox entry). Part of
// the shared data model; the viewer itself only ever reads `SessionSnapshot`.
export interface Session {
  sessionId: string
  hostId: string
  viewerUid: string
  ownerUid: string
  status: SessionStatus
  createdAt: number | null
}

// What the viewer watches on `/sessions/{sessionId}` in real time: the
// lifecycle status the host advances, and the host's SDP offer once present.
export interface SessionSnapshot {
  status: SessionStatus
  offer?: Description
}
