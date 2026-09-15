import type { Host } from '@/types/host'

// The host agent refreshes `lastSeen` (a server timestamp) with a periodic
// heartbeat. A crashed agent never gets to remove its record, so presence is
// derived purely from the age of that heartbeat. Ages are computed against
// SERVER time (services/serverTime.ts): the host wrote a server timestamp, so
// comparing it with the browser clock would add the local clock skew.

// Allows a few missed heartbeats before a host is shown as offline.
export const HOST_STALE_AFTER_MS = 90_000

// Past this age a record is garbage: the security rules let ANY signed-in user
// delete it (opportunistic cleanup of hosts that died without cleaning up).
// Must match the 300000 ms window in firebase/database.rules.json.
export const HOST_GC_AFTER_MS = 300_000

export function isHostOnline(host: Host, serverNowMs: number): boolean {
  return host.lastSeen !== null && serverNowMs - host.lastSeen < HOST_STALE_AFTER_MS
}

export function isHostStaleForGc(host: Host, serverNowMs: number): boolean {
  return host.lastSeen !== null && serverNowMs - host.lastSeen > HOST_GC_AFTER_MS
}
