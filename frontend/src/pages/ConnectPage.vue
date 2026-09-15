<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth.store'
import { subscribeHost } from '@/services/host.service'
import { useViewerConnection } from '@/composables/useViewerConnection'
import { useInputCapture } from '@/composables/useInputCapture'
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

const videoEl = ref<HTMLVideoElement | null>(null)

// Forward input only once the connection is up and the channel is open.
const inputActive = computed(() => state.value === 'connected' && inputReady.value)

function sendInput(message: InputMessage): void {
  const channel = dataChannel.value
  if (channel && channel.readyState === 'open') {
    channel.send(JSON.stringify(message))
  }
}

useInputCapture(videoEl, sendInput, inputActive)

// Bind the remote media stream to the <video> element once it arrives.
watch(remoteStream, (stream) => {
  if (videoEl.value) {
    videoEl.value.srcObject = stream
  }
})

// Focus the video when control becomes active so keyboard events flow.
watch(inputActive, (isActive) => {
  if (isActive) {
    videoEl.value?.focus()
  }
})

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
  stopWatchingHost()
  void disconnect()
})
</script>

<template>
  <div class="d-flex flex-column min-vh-100 bg-dark">
    <!-- Toolbar -->
    <div class="d-flex align-items-center justify-content-between px-3 py-2 bg-black text-white">
      <div class="d-flex align-items-center gap-2">
        <span class="badge" :class="online ? 'text-bg-success' : 'text-bg-secondary'">
          {{ online ? 'Online' : 'Offline' }}
        </span>
        <span class="fw-semibold">{{ host?.name ?? formattedCode }}</span>
        <span class="badge text-bg-dark border font-monospace">{{ formattedCode }}</span>
        <span class="badge text-bg-info">{{ statusLabel }}</span>
        <span v-if="inputActive" class="badge text-bg-success">Control active</span>
      </div>
      <button class="btn btn-danger btn-sm" type="button" @click="handleDisconnect">
        End Session
      </button>
    </div>

    <!-- Video stage -->
    <div class="flex-grow-1 d-flex align-items-center justify-content-center p-3 position-relative">
      <template v-if="loadError">
        <div class="text-center text-white-50">
          <h5 class="text-white">Could not connect</h5>
          <p class="text-danger mb-3">{{ loadError }}</p>
          <button class="btn btn-outline-light btn-sm" type="button" @click="goHome">
            Return to home
          </button>
        </div>
      </template>

      <template v-else>
        <video
          ref="videoEl"
          class="w-100"
          style="max-height: 100%; object-fit: contain; background-color: #000; outline: none"
          autoplay
          playsinline
          muted
          tabindex="0"
        ></video>

        <div v-if="!remoteStream" class="position-absolute text-center text-white-50">
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
          v-else
          class="position-absolute bottom-0 start-50 translate-middle-x mb-2 small text-white-50 bg-black bg-opacity-50 px-2 py-1 rounded"
        >
          Click the video area to control
        </div>
      </template>
    </div>
  </div>
</template>
