import { ref, shallowRef } from 'vue'
import { CONNECTION_TIMEOUT_MS, DISCONNECT_GRACE_MS, ICE_SERVERS } from '@/constants/webrtc'
import {
  createSession,
  removeSession,
  setSessionStatus,
  subscribeSession,
} from '@/services/session.service'
import { addViewerCandidate, onHostCandidates, setAnswer } from '@/services/signaling.service'
import { toFriendlyError } from '@/utils/firebaseErrors'
import type { Description, IceCandidate } from '@/types/signaling'
import type { Host } from '@/types/host'
import type { SessionStatus } from '@/types/session'

export type ConnectionState = 'idle' | 'connecting' | 'connected' | 'failed' | 'closed'

const REJECTED_MESSAGE = 'The host rejected or ended the connection request.'
const TIMEOUT_MESSAGE =
  'The connection timed out. The host may be offline or did not approve the request.'

// Drives the viewer (answerer) side of a WebRTC connection: it creates the
// session (plus the host's inbox entry), builds the RTCPeerConnection, and
// runs the offer → answer → ICE exchange. Video surfaces via `remoteStream`;
// the host's input DataChannel arrives via `ondatachannel`. `sessionStatus`
// mirrors the RTDB lifecycle so the UI can narrate the host-approval step.
export function useViewerConnection() {
  const state = ref<ConnectionState>('idle')
  const error = ref<string | null>(null)
  const remoteStream = shallowRef<MediaStream | null>(null)
  const dataChannel = shallowRef<RTCDataChannel | null>(null)
  const inputReady = ref(false)
  const sessionStatus = ref<SessionStatus | null>(null)

  let pc: RTCPeerConnection | null = null
  // The target host: cleanup needs its owner uid to find the inbox entry.
  let host: Host | null = null
  let sessionId: string | null = null
  let cleanups: Array<() => void> = []
  let pending: RTCIceCandidateInit[] = []
  let remoteReady = false
  let answered = false
  let finished = false
  // Set once the session node is known to be deleted: writing to it would only
  // bounce off the rules, so cleanup skips the final status update.
  let sessionGone = false
  let approvalTimerStarted = false
  let timer: ReturnType<typeof setTimeout> | null = null
  let graceTimer: ReturnType<typeof setTimeout> | null = null

  async function connect(target: Host, viewerUid: string): Promise<void> {
    if (state.value === 'connecting' || state.value === 'connected') {
      return
    }
    reset()
    state.value = 'connecting'
    host = target

    try {
      const id = await createSession(target, viewerUid)
      if (finished) {
        // Torn down (page left) while the session was being created: nothing
        // will listen to it, so take the request back straight away.
        await removeSession(id, target.ownerUid)
        return
      }
      sessionId = id

      pc = new RTCPeerConnection({ iceServers: ICE_SERVERS })
      pc.ontrack = (event) => {
        remoteStream.value = event.streams[0] ?? null
      }
      pc.ondatachannel = (event) => {
        const channel = event.channel
        dataChannel.value = channel
        channel.onopen = () => {
          inputReady.value = true
        }
        channel.onclose = () => {
          inputReady.value = false
        }
      }
      pc.onicecandidate = (event) => {
        if (event.candidate && sessionId) {
          const json = event.candidate.toJSON()
          void addViewerCandidate(sessionId, {
            candidate: json.candidate ?? '',
            sdpMid: json.sdpMid ?? null,
            sdpMLineIndex: json.sdpMLineIndex ?? null,
            usernameFragment: json.usernameFragment ?? null,
          })
        }
      }
      pc.onconnectionstatechange = handleStateChange

      cleanups.push(onHostCandidates(id, addRemoteCandidate))

      // One listener covers the lifecycle AND the offer. `ended` before a
      // connection is established means the request was rejected (or the host
      // gave up). The node disappearing means the same while we are still
      // connecting, and "the host is gone" once connected.
      cleanups.push(
        subscribeSession(
          id,
          (snapshot) => {
            sessionStatus.value = snapshot.status
            if (snapshot.status === 'connecting' && !approvalTimerStarted) {
              // The host is alive and the operator is now deciding (a 45 s
              // window on their side): give the approval its own full timeout
              // instead of whatever was left after the request was noticed.
              approvalTimerStarted = true
              startTimer()
            }
            if (snapshot.offer && !answered) {
              void handleOffer(snapshot.offer)
            }
            if (snapshot.status === 'ended' && state.value === 'connecting') {
              fail(REJECTED_MESSAGE)
            }
          },
          () => {
            sessionGone = true
            if (state.value === 'connecting') {
              fail(REJECTED_MESSAGE)
            } else {
              void teardown('closed')
            }
          },
        ),
      )

      startTimer()
    } catch (err) {
      fail(toFriendlyError(err))
    }
  }

  function handleStateChange(): void {
    if (!pc) {
      return
    }
    switch (pc.connectionState) {
      case 'connected':
        clearTimer()
        clearGraceTimer()
        // Also reached when a `disconnected` blip recovers; only announce once.
        if (state.value !== 'connected') {
          state.value = 'connected'
          if (sessionId) {
            void setSessionStatus(sessionId, 'connected')
          }
        }
        break
      case 'failed':
        fail('Could not establish the peer-to-peer connection.')
        break
      case 'disconnected':
        // Usually transient (ICE consent lapse, network switch): give it a
        // moment to come back before declaring the session over.
        startGraceTimer()
        break
      case 'closed':
        void teardown('closed')
        break
    }
  }

  function addRemoteCandidate(candidate: IceCandidate): void {
    const init: RTCIceCandidateInit = {
      candidate: candidate.candidate,
      sdpMid: candidate.sdpMid ?? undefined,
      sdpMLineIndex: candidate.sdpMLineIndex ?? undefined,
      usernameFragment: candidate.usernameFragment ?? undefined,
    }
    if (remoteReady && pc) {
      void pc.addIceCandidate(init)
    } else {
      pending.push(init)
    }
  }

  async function handleOffer(offer: Description): Promise<void> {
    if (answered || !pc || !sessionId) {
      return
    }
    answered = true
    try {
      await pc.setRemoteDescription({ type: offer.type, sdp: offer.sdp })
      remoteReady = true
      for (const init of pending) {
        void pc.addIceCandidate(init)
      }
      pending = []

      const answer = await pc.createAnswer()
      await pc.setLocalDescription(answer)
      await setAnswer(sessionId, { type: answer.type ?? 'answer', sdp: answer.sdp ?? '' })
    } catch (err) {
      fail(toFriendlyError(err))
    }
  }

  function fail(message: string): void {
    error.value = message
    state.value = 'failed'
    void teardown('failed')
  }

  async function disconnect(): Promise<void> {
    await teardown('closed')
  }

  async function teardown(finalState: ConnectionState): Promise<void> {
    if (finished) {
      return
    }
    finished = true
    clearTimer()
    clearGraceTimer()
    for (const unsubscribe of cleanups) {
      unsubscribe()
    }
    cleanups = []
    if (pc) {
      pc.onconnectionstatechange = null
      pc.close()
      pc = null
    }
    remoteStream.value = null
    dataChannel.value = null
    inputReady.value = false
    if (state.value !== 'failed') {
      state.value = finalState
    }
    const id = sessionId
    const target = host
    sessionId = null
    if (!id || !target) {
      return
    }
    // Best-effort: tell the host we are done (a clean signal, as opposed to the
    // node vanishing under it), then take both nodes down ourselves instead of
    // leaving that to the onDisconnect handlers.
    if (!sessionGone) {
      try {
        await setSessionStatus(id, 'ended')
      } catch {
        // The host may have deleted the node already; removal is idempotent.
      }
    }
    await removeSession(id, target.ownerUid)
  }

  function reset(): void {
    error.value = null
    dataChannel.value = null
    inputReady.value = false
    sessionStatus.value = null
    host = null
    sessionId = null
    pending = []
    remoteReady = false
    answered = false
    finished = false
    sessionGone = false
    approvalTimerStarted = false
  }

  // (Re)starts the overall connection deadline.
  function startTimer(): void {
    clearTimer()
    timer = setTimeout(() => {
      timer = null
      if (state.value === 'connecting') {
        fail(TIMEOUT_MESSAGE)
      }
    }, CONNECTION_TIMEOUT_MS)
  }

  function clearTimer(): void {
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
  }

  // Tears down only if the peer connection has not recovered by the deadline.
  function startGraceTimer(): void {
    if (graceTimer !== null) {
      return
    }
    graceTimer = setTimeout(() => {
      graceTimer = null
      if (pc && pc.connectionState !== 'connected') {
        void teardown('closed')
      }
    }, DISCONNECT_GRACE_MS)
  }

  function clearGraceTimer(): void {
    if (graceTimer !== null) {
      clearTimeout(graceTimer)
      graceTimer = null
    }
  }

  return { state, error, remoteStream, dataChannel, inputReady, sessionStatus, connect, disconnect }
}
