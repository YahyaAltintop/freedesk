// Domain model for a host machine (mirrors the RTDB `/hosts/{code}` node).
// `hostId` IS the 9-digit pairing code, i.e. the node key. Nothing about a
// host persists between launches: the agent mints a fresh anonymous uid and a
// fresh code every time it starts, so a record simply disappears (or goes
// stale) once the agent is gone.
export interface Host {
  hostId: string
  ownerUid: string
  name: string
  version: string
  // Server timestamp (epoch ms) of the last heartbeat; null if the record is
  // malformed. Compare it against SERVER time (services/serverTime.ts).
  lastSeen: number | null
}
