import { onChildAdded, push, ref, set, type Unsubscribe } from 'firebase/database'
import { db } from '@/firebase'
import { DbPaths } from '@/constants/paths'
import type { Description, IceCandidate } from '@/types/signaling'

// Realtime Database signaling for the viewer (answerer) side, following the
// layout in docs/PROTOCOL.md. The session node itself — and the host's offer,
// which lives on it — is handled by session.service.ts; this module only
// exchanges the answer and the ICE candidates underneath it.

function nodeRef(sessionId: string, child: string) {
  return ref(db, `${DbPaths.sessions}/${sessionId}/${child}`)
}

// Publishes the viewer's answer.
export async function setAnswer(sessionId: string, answer: Description): Promise<void> {
  await set(nodeRef(sessionId, 'answer'), answer)
}

// Appends one of the viewer's ICE candidates.
export async function addViewerCandidate(sessionId: string, candidate: IceCandidate): Promise<void> {
  await push(nodeRef(sessionId, 'viewerCandidates'), candidate)
}

// Subscribes to the host's ICE candidates as they are added.
export function onHostCandidates(
  sessionId: string,
  callback: (candidate: IceCandidate) => void,
): Unsubscribe {
  return onChildAdded(nodeRef(sessionId, 'hostCandidates'), (snapshot) => {
    const value = snapshot.val() as IceCandidate | null
    if (value && value.candidate) {
      callback(value)
    }
  })
}
