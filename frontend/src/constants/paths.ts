// Realtime Database top-level paths — single source of truth (no magic strings).
//
//   /hosts/{code}                  host record, written by the host agent
//   /inbox/{ownerUid}/{sessionId}  connection requests addressed to a host owner
//   /sessions/{sessionId}          session lifecycle + WebRTC signaling
export const DbPaths = {
  hosts: 'hosts',
  inbox: 'inbox',
  sessions: 'sessions',
} as const
