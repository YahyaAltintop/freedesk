import {
  onDisconnect,
  onValue,
  push,
  ref,
  serverTimestamp,
  set,
  update,
  type Unsubscribe,
} from 'firebase/database'
import { db } from '@/firebase'
import { DbPaths } from '@/constants/paths'
import type { Host } from '@/types/host'
import type { SessionSnapshot, SessionStatus } from '@/types/session'
import type { Description } from '@/types/signaling'

// Realtime Database session lifecycle for the viewer side. A session is two
// nodes written together: `/sessions/{id}` (lifecycle + signaling, shared with
// the host) and `/inbox/{ownerUid}/{id}` (the request the host agent watches —
// only the owner can read its inbox, so this is how the host learns about us).
// Neither node outlives the viewer: server-side onDisconnect handlers remove
// both if the tab dies, and `removeSession` removes them on a clean exit.

function sessionPath(sessionId: string): string {
  return `${DbPaths.sessions}/${sessionId}`
}

function inboxPath(ownerUid: string, sessionId: string): string {
  return `${DbPaths.inbox}/${ownerUid}/${sessionId}`
}

function isPermissionDenied(error: unknown): boolean {
  const message = error instanceof Error ? error.message : String(error)
  return /permission[_ ]denied/i.test(message)
}

const delay = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))

// On a cold page load the RTDB WebSocket authenticates a beat after
// `auth.currentUser` is set, so the very first write can race ahead of the
// token and bounce with permission_denied even though the rules would allow
// it. A short bounded retry rides out that window. This is safe because our
// own writes always satisfy the rules (viewerUid === auth.uid), so a genuine
// denial would not resolve by waiting — it just exhausts the retries and
// surfaces.
async function withColdStartRetry(operation: () => Promise<void>): Promise<void> {
  const backoffMs = [150, 300, 600, 1000]
  for (let attempt = 0; ; attempt++) {
    try {
      await operation()
      return
    } catch (error) {
      if (attempt >= backoffMs.length || !isPermissionDenied(error)) {
        throw error
      }
      await delay(backoffMs[attempt])
    }
  }
}

// Creates a session (status: waiting) addressed to `host` and returns its id.
export async function createSession(host: Host, viewerUid: string): Promise<string> {
  const sessionRef = push(ref(db, DbPaths.sessions))
  const sessionId = sessionRef.key
  if (!sessionId) {
    throw new Error('Could not allocate a session id.')
  }
  const inboxRef = ref(db, inboxPath(host.ownerUid, sessionId))

  // The onDisconnect registration is itself the first authenticated write of a
  // cold page load, so it sits inside the retry together with the data write.
  await withColdStartRetry(async () => {
    // Register the server-side cleanup BEFORE the data exists, so there is no
    // window in which a dying tab leaves an orphaned request behind. Removing
    // a node that does not exist (yet) is allowed by the rules.
    await Promise.all([onDisconnect(sessionRef).remove(), onDisconnect(inboxRef).remove()])

    // One atomic multi-path write: the host must never see an inbox entry
    // without its session (or vice versa).
    await update(ref(db), {
      [sessionPath(sessionId)]: {
        viewerUid,
        ownerUid: host.ownerUid,
        status: 'waiting',
        createdAt: serverTimestamp(),
      },
      [inboxPath(host.ownerUid, sessionId)]: {
        viewerUid,
        code: host.hostId,
        createdAt: serverTimestamp(),
      },
    })
  })

  return sessionId
}

// Advances the session lifecycle status.
export async function setSessionStatus(sessionId: string, status: SessionStatus): Promise<void> {
  await set(ref(db, `${sessionPath(sessionId)}/status`), status)
}

const SESSION_STATUSES: ReadonlySet<string> = new Set<SessionStatus>([
  'waiting',
  'connecting',
  'connected',
  'ended',
])

function isSessionStatus(value: unknown): value is SessionStatus {
  return typeof value === 'string' && SESSION_STATUSES.has(value)
}

function toDescription(value: unknown): Description | undefined {
  if (typeof value !== 'object' || value === null) {
    return undefined
  }
  const { type, sdp } = value as { type?: unknown; sdp?: unknown }
  if (typeof type !== 'string' || typeof sdp !== 'string' || sdp === '') {
    return undefined
  }
  return { type: type as RTCSdpType, sdp }
}

// Maps a raw `/sessions/{id}` value into the part of it the viewer acts on.
function toSessionSnapshot(value: unknown): SessionSnapshot {
  const raw = (typeof value === 'object' && value !== null ? value : {}) as {
    status?: unknown
    offer?: unknown
  }
  const offer = toDescription(raw.offer)
  return {
    status: isSessionStatus(raw.status) ? raw.status : 'waiting',
    ...(offer ? { offer } : {}),
  }
}

// Watches the session in real time. The host advances `status` (`connecting`
// while the operator decides, `ended` on a rejection) and publishes its SDP
// `offer` on the same node, so one listener covers both.
//
// Reading `/sessions/{id}` is only allowed to its participants, so once the
// node is deleted (by us, the host, or our own onDisconnect handler) the
// listener does NOT see a null snapshot — the read is refused and the SDK
// cancels it. Both a missing snapshot and the error callback therefore mean
// "gone", reported exactly once via `onGone`.
export function subscribeSession(
  sessionId: string,
  onSnapshot: (snapshot: SessionSnapshot) => void,
  onGone: () => void,
): Unsubscribe {
  let gone = false
  const reportGone = (): void => {
    if (!gone) {
      gone = true
      onGone()
    }
  }
  return onValue(
    ref(db, sessionPath(sessionId)),
    (snapshot) => {
      if (gone) {
        return
      }
      if (!snapshot.exists()) {
        reportGone()
        return
      }
      onSnapshot(toSessionSnapshot(snapshot.val()))
    },
    reportGone,
  )
}

// Removes both session nodes and then cancels their onDisconnect handlers.
// Best effort: deleting an already-deleted node is allowed by the rules, and
// if the delete itself fails the handlers stay armed as the safety net.
export async function removeSession(sessionId: string, ownerUid: string): Promise<void> {
  const sessionRef = ref(db, sessionPath(sessionId))
  const inboxRef = ref(db, inboxPath(ownerUid, sessionId))
  try {
    await update(ref(db), {
      [sessionPath(sessionId)]: null,
      [inboxPath(ownerUid, sessionId)]: null,
    })
    await Promise.all([onDisconnect(sessionRef).cancel(), onDisconnect(inboxRef).cancel()])
  } catch {
    // Best-effort cleanup; the server-side handlers cover whatever is left.
  }
}
