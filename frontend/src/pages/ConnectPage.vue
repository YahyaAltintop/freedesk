<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth.store'
import { subscribeHost } from '@/services/host.service'
import { useViewerConnection } from '@/composables/useViewerConnection'
import { useInputCapture } from '@/composables/useInputCapture'
import { useFullscreen } from '@/composables/useFullscreen'
import { useServerNow } from '@/composables/useServerNow'
import { isHostOnline } from '@/utils/presence'
import { toFriendlyError } from '@/utils/firebaseErrors'
import { RouteName } from '@/constants/routes'
import type { Host } from '@/types/host'
import type { InputMessage } from '@/types/input'

// `hostId` is the target's pairing code, provided as a route prop.
const props = defineProps<{ hostId: string }>()

const router = useRouter()
const authStore = useAuthStore()
const { state, error, remoteStream, dataChannel, inputReady, sessionStatus, connect, disconnect } =
  useViewerConnection()

const host = ref<Host | null>(null)
const loadError = ref<string | null>(null)
// The host record vanished after we found it: the agent shut down or crashed.
const hostGone = ref(false)

// `lastSeen` is a server timestamp, so presence is judged against server time.
const serverNow = useServerNow()
const online = computed(
  () => host.value !== null && !hostGone.value && isHostOnline(host.value, serverNow.value),
)

const stageEl = ref<HTMLElement | null>(null)
const videoEl = ref<HTMLVideoElement | null>(null)

// The session can take input: connected, with the channel open. Whether input
// is actually forwarded also depends on the video holding the focus, which
// useInputCapture tracks and reports back as `controlling`.
const sessionReady = computed(() => state.value === 'connected' && inputReady.value)

function sendInput(message: InputMessage): void {
  const channel = dataChannel.value
  if (channel && channel.readyState === 'open') {
    channel.send(JSON.stringify(message))
  }
}

const { controlling, focus: focusStage } = useInputCapture(videoEl, sendInput, sessionReady)
const { isFullscreen, supported: fullscreenSupported, toggle: toggleFullscreen } =
  useFullscreen(stageEl)

// Bind the remote media stream to the <video> element once it arrives.
watch(remoteStream, (stream) => {
  if (videoEl.value) {
    videoEl.value.srcObject = stream
  }
})

// Hand the video the focus as soon as the session is ready, so the viewer can
// type straight away instead of having to click first.
watch(sessionReady, (isReady) => {
  if (isReady) {
    focusStage()
  }
})

// The button takes the focus away from the video, which would drop control the
// moment the viewer went fullscreen; give it straight back.
async function handleFullscreen(): Promise<void> {
  await toggleFullscreen()
  focusStage()
}

const statusLabel = computed(() => {
  if (hostGone.value) {
    return 'The host went offline'
  }
  switch (state.value) {
    case 'connecting':
      // The host marks the session `connecting` the moment it sees the
      // request; from then on we are waiting for the operator to approve.
      return sessionStatus.value === 'connecting'
        ? 'Waiting for host approval…'
        : 'Connecting…'
    case 'connected':
      return 'Connected'
    case 'failed':
      return 'Connection failed'
    case 'closed':
      return 'Connection closed'
    default:
      return 'Preparing…'
  }
})

// When the host itself is gone, that is the story: the session-level error
// that follows (rejected / closed) is merely a consequence of it.
const displayedError = computed(() => (hostGone.value ? null : error.value))

// "123456789" -> "123 456 789" for display.
const formattedCode = computed(() => props.hostId.replace(/(\d{3})(?=\d)/g, '$1 ').trim())

let unsubscribeHost: (() => void) | null = null

function stopWatchingHost(): void {
  if (unsubscribeHost) {
    unsubscribeHost()
    unsubscribeHost = null
  }
}

// Watch the code's host record. The first time it resolves we start
// connecting (exactly once); afterwards it keeps the presence badge honest and
// tells us if the agent goes away — the session listener and the peer state
// take care of the actual teardown in that case.
onMounted(() => {
  // The stage pins itself to the viewport; keep the app shell behind it from
  // adding a scrollbar of its own.
  document.documentElement.classList.add('fd-no-scroll')

  const viewerUid = authStore.uid
  if (!viewerUid) {
    loadError.value = authStore.error ?? 'Authentication failed.'
    return
  }
  let connectStarted = false
  unsubscribeHost = subscribeHost(
    props.hostId,
    (found) => {
      if (found) {
        host.value = found
        hostGone.value = false
        if (!connectStarted) {
          connectStarted = true
          void connect(found, viewerUid)
        }
        return
      }
      if (connectStarted) {
        hostGone.value = true
      } else {
        loadError.value = 'No computer is registered to this code.'
        stopWatchingHost()
      }
    },
    (err) => {
      loadError.value = toFriendlyError(err)
    },
  )
})

async function goHome(): Promise<void> {
  await router.push({ name: RouteName.Home })
}

async function handleDisconnect(): Promise<void> {
  await disconnect()
  await goHome()
}

onBeforeUnmount(() => {
  document.documentElement.classList.remove('fd-no-scroll')
  stopWatchingHost()
  void disconnect()
})
</script>

<template>
  <div ref="stageEl" class="fd-stage">
    <!-- Toolbar -->
    <div class="fd-stage-bar d-flex align-items-center justify-content-between gap-2 px-3 py-2">
      <div class="d-flex align-items-center gap-2 flex-wrap">
        <span class="badge" :class="online ? 'text-bg-success' : 'text-bg-secondary'">
          {{ online ? 'Online' : 'Offline' }}
        </span>
        <span class="fw-semibold">{{ host?.name ?? formattedCode }}</span>
        <span class="badge text-bg-dark border font-monospace">{{ formattedCode }}</span>
        <span class="badge text-bg-info">{{ statusLabel }}</span>
        <span v-if="controlling" class="badge text-bg-success">Control active</span>
        <span v-else-if="sessionReady" class="badge text-bg-warning">Click the screen to control</span>
      </div>
      <div class="d-flex align-items-center gap-2">
        <button
          v-if="fullscreenSupported"
          class="btn btn-outline-light btn-sm text-nowrap"
          type="button"
          :title="
            isFullscreen
              ? 'Leave fullscreen'
              : 'Fullscreen also sends browser shortcuts such as Ctrl+W to the remote computer'
          "
          @click="handleFullscreen"
        >
          {{ isFullscreen ? 'Exit fullscreen' : 'Fullscreen' }}
        </button>
        <button class="btn btn-danger btn-sm text-nowrap" type="button" @click="handleDisconnect">
          End Session
        </button>
      </div>
    </div>

    <!-- Video stage -->
    <div class="fd-stage-body d-flex align-items-center justify-content-center">
      <template v-if="loadError">
        <div class="text-center text-white-50 p-3">
          <h5 class="text-white">Could not connect</h5>
          <p class="text-danger mb-3">{{ loadError }}</p>
          <button class="btn btn-outline-light btn-sm" type="button" @click="goHome">
            Return to home
          </button>
        </div>
      </template>

      <template v-else>
        <video ref="videoEl" class="fd-stage-video" autoplay playsinline muted tabindex="0"></video>

        <div v-if="!remoteStream" class="position-absolute text-center text-white-50 p-3">
          <div
            v-if="state === 'connecting'"
            class="spinner-border mb-3"
            role="status"
            aria-hidden="true"
          ></div>
          <h5 class="text-white">{{ statusLabel }}</h5>
          <p v-if="displayedError" class="text-danger mb-1">{{ displayedError }}</p>
          <p class="small mb-0 font-monospace">Code: {{ formattedCode }}</p>
          <p v-if="state === 'connecting' && sessionStatus === 'connecting'" class="small mb-0">
            The person at the remote computer needs to approve the request.
          </p>
          <button
            v-if="state === 'failed' || state === 'closed'"
            class="btn btn-outline-light btn-sm mt-3"
            type="button"
            @click="goHome"
          >
            Return to home
          </button>
        </div>

        <div
          v-else-if="!controlling"
          class="fd-stage-hint position-absolute bottom-0 start-50 translate-middle-x mb-2 small text-white-50 px-2 py-1 rounded"
        >
          Click the screen to control it
        </div>
        <div
          v-else-if="!isFullscreen && fullscreenSupported"
          class="fd-stage-hint position-absolute bottom-0 start-50 translate-middle-x mb-2 small text-white-50 px-2 py-1 rounded"
        >
          Keys go to the remote computer. Go fullscreen to send browser shortcuts too — Alt+Tab and
          the Windows key always stay on this computer.
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
/* The session fills the viewport exactly and never scrolls: the toolbar takes
   what it needs, the video gets the rest. `position: fixed` takes the stage out
   of the app shell, whose own min-height would otherwise stack with the
   toolbar and push the bottom of the remote screen — the Windows taskbar —
   below the fold. */
.fd-stage {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  height: 100vh;
  height: 100dvh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background-color: #000;
  color: #fff;
}

.fd-stage-bar {
  flex: 0 0 auto;
  background-color: #000;
}

/* min-height: 0 is what actually lets the video shrink to the space left over:
   a flex item refuses to go below its content size without it, and then
   max-height/height percentages on the video have nothing to resolve against. */
.fd-stage-body {
  position: relative;
  flex: 1 1 auto;
  min-height: 0;
}

.fd-stage-video {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: contain;
  background-color: #000;
  outline: none;
}

.fd-stage-hint {
  max-width: 90%;
  background-color: rgba(0, 0, 0, 0.6);
  pointer-events: none; /* never swallow the click that takes control */
}
</style>
