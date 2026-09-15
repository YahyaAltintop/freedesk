import { get, onValue, ref, remove, type Unsubscribe } from 'firebase/database'
import { db } from '@/firebase'
import { DbPaths } from '@/constants/paths'
import type { Host } from '@/types/host'

// Raw shape of a `/hosts/{code}` node as it comes off the wire (untrusted).
interface RawHost {
  ownerUid?: unknown
  name?: unknown
  version?: unknown
  lastSeen?: unknown
}

// Maps a raw RTDB value into a Host domain object with safe defaults.
function toHost(code: string, value: unknown): Host {
  const raw = (typeof value === 'object' && value !== null ? value : {}) as RawHost
  return {
    hostId: code,
    ownerUid: typeof raw.ownerUid === 'string' ? raw.ownerUid : '',
    name: typeof raw.name === 'string' ? raw.name : '',
    version: typeof raw.version === 'string' ? raw.version : '',
    lastSeen: typeof raw.lastSeen === 'number' ? raw.lastSeen : null,
  }
}

function hostRef(code: string) {
  return ref(db, `${DbPaths.hosts}/${code}`)
}

// Resolves a pairing code to its host with a single point read — the code IS
// the node key. Listing `/hosts` is forbidden by the security rules; this
// lookup is the only way to find a machine.
export async function fetchHostByCode(code: string): Promise<Host | null> {
  const snapshot = await get(hostRef(code))
  return snapshot.exists() ? toHost(code, snapshot.val()) : null
}

// Watches a host record in real time: `null` when the node does not exist (or
// disappears — the agent removes it on shutdown). Heartbeats arrive as updated
// `lastSeen` values. `onError` fires if the read is refused, i.e. we are not
// (or no longer) signed in; the SDK cancels the listener at that point.
export function subscribeHost(
  code: string,
  onHost: (host: Host | null) => void,
  onError?: (error: Error) => void,
): Unsubscribe {
  return onValue(
    hostRef(code),
    (snapshot) => {
      onHost(snapshot.exists() ? toHost(code, snapshot.val()) : null)
    },
    (error) => {
      onError?.(error)
    },
  )
}

// Opportunistic garbage collection of a host that died without cleaning up.
// The rules only allow this once `lastSeen` is old enough, and someone else
// may have beaten us to it, so failure is expected and silently ignored.
export async function removeStaleHost(code: string): Promise<void> {
  try {
    await remove(hostRef(code))
  } catch {
    // Best-effort cleanup; the rules decide, not us.
  }
}
