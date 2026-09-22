<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import AppIcon from '@/components/AppIcon.vue'
import DownloadButton from '@/components/DownloadButton.vue'
import type { IconName } from '@/components/icons'
import { useAuthStore } from '@/stores/auth.store'
import { fetchHostByCode, removeStaleHost } from '@/services/host.service'
import { fetchLocalIdentity, type LocalIdentity } from '@/services/localAgent'
import { useLatestRelease } from '@/services/releases'
import { serverNow } from '@/services/serverTime'
import { isHostOnline, isHostStaleForGc } from '@/utils/presence'
import { toFriendlyError } from '@/utils/firebaseErrors'
import { isNewerVersion } from '@/utils/version'
import { RouteName } from '@/constants/routes'
import { GITHUB_URL } from '@/constants/links'
import {
  PAIRING_CODE_LENGTH,
  formatPairingCode,
  normalizePairingCode,
} from '@/utils/pairingCode'

const router = useRouter()
const authStore = useAuthStore()

// The address the host program tells people to open (this very site).
const siteOrigin = window.location.origin

// ---- This computer (code served by the local host agent) -------------------

const identity = ref<LocalIdentity | null>(null)
const identityChecked = ref(false)
const copied = ref(false)
const isWindows = /Windows/i.test(navigator.userAgent)

// The host program running here reports its version; the download button's
// release lookup knows the newest one. When they differ, say so right where
// the code is shown — the person who can update is sitting at this computer.
const latestRelease = useLatestRelease()
const hostUpdate = computed(() => {
  const lookup = latestRelease.value
  const current = identity.value?.version
  if (lookup?.state !== 'found' || !current) {
    return null
  }
  return isNewerVersion(lookup.release.version, current) ? lookup.release : null
})

// The agent is probed again every few seconds (loopback, cheap) so the code
// shows up by itself once the user starts freedesk.exe and disappears
// when they close it. Paused while the tab is hidden.
const REPROBE_IDLE_MS = 4_000
const REPROBE_RUNNING_MS = 10_000
let reprobeTimer: ReturnType<typeof setTimeout> | undefined
let probing = false

async function loadIdentity(): Promise<void> {
  if (probing) {
    return
  }
  probing = true
  try {
    identity.value = await fetchLocalIdentity()
  } finally {
    probing = false
    identityChecked.value = true
  }
  scheduleReprobe()
}

function scheduleReprobe(): void {
  clearTimeout(reprobeTimer)
  if (document.visibilityState !== 'visible') {
    return
  }
  const delay = identity.value ? REPROBE_RUNNING_MS : REPROBE_IDLE_MS
  reprobeTimer = setTimeout(() => void loadIdentity(), delay)
}

function onVisibilityChange(): void {
  if (document.visibilityState === 'visible') {
    void loadIdentity()
  } else {
    clearTimeout(reprobeTimer)
  }
}

async function copyCode(): Promise<void> {
  if (!identity.value) {
    return
  }
  try {
    await navigator.clipboard.writeText(identity.value.code)
    copied.value = true
    setTimeout(() => {
      copied.value = false
    }, 2000)
  } catch {
    // Clipboard access denied — the code is still visible to copy manually.
  }
}

onMounted(() => {
  void loadIdentity()
  document.addEventListener('visibilitychange', onVisibilityChange)
})

onBeforeUnmount(() => {
  clearTimeout(reprobeTimer)
  document.removeEventListener('visibilitychange', onVisibilityChange)
})

// ---- Connect to a remote computer ------------------------------------------

const code = ref('')
const busy = ref(false)
const connectError = ref<string | null>(null)

const formattedInput = computed(() => formatPairingCode(code.value))

function onCodeInput(event: Event): void {
  const input = event.target as HTMLInputElement
  code.value = normalizePairingCode(input.value)
  // Reflect the normalised value so stray characters never linger in the box.
  input.value = formatPairingCode(code.value)
}

async function handleConnect(): Promise<void> {
  connectError.value = null
  if (code.value.length !== PAIRING_CODE_LENGTH) {
    connectError.value = `The code must be ${PAIRING_CODE_LENGTH} digits.`
    return
  }
  if (identity.value && code.value === identity.value.code) {
    connectError.value = "That's this computer's own code."
    return
  }
  if (!authStore.uid) {
    connectError.value = authStore.error ?? 'Authentication failed.'
    return
  }
  busy.value = true
  try {
    const host = await fetchHostByCode(code.value)
    if (!host) {
      connectError.value = 'No computer is registered to this code.'
      return
    }
    // `lastSeen` is a server timestamp, so presence is judged against server time.
    const now = serverNow()
    if (!isHostOnline(host, now)) {
      // A record this old belongs to an agent that died without cleaning up;
      // the rules let anyone remove it, so tidy up in passing (fire-and-forget).
      if (isHostStaleForGc(host, now)) {
        void removeStaleHost(code.value)
      }
      connectError.value = 'The computer appears to be offline. Is the host agent running on the other side?'
      return
    }
    await router.push({ name: RouteName.Connect, params: { hostId: code.value } })
  } catch (error) {
    connectError.value = toFriendlyError(error)
  } finally {
    busy.value = false
  }
}

// ---- Static content ----------------------------------------------------------

interface Feature {
  icon: IconName
  title: string
  text: string
}

const features: Feature[] = [
  {
    icon: 'user',
    title: 'No account',
    text: 'No sign-up, no password. The code is the key.',
  },
  {
    icon: 'zap',
    title: 'Direct connection',
    text: 'Peer to peer over encrypted WebRTC. No server in between.',
  },
  {
    icon: 'eye-off',
    title: 'Nothing stored',
    text: 'A temporary code, a temporary handshake. Gone when the session ends.',
  },
  {
    icon: 'code',
    title: 'Open source',
    text: 'MIT licensed. Read it, build it, host your own.',
  },
]
</script>

<template>
  <!-- Hero: what FreeDesk is, plus the connect-by-code form -->
  <section class="fd-hero">
    <div class="container fd-hero-inner">
      <div class="row align-items-center g-5">
        <div class="col-lg-7">
          <span class="fd-eyebrow fd-rise" style="--d: 0">free · open source · no account</span>
          <h1 class="fd-h1 fd-rise" style="--d: 1">
            Remote desktop,<br />
            <span class="fd-gradient-text">as simple as a code.</span>
          </h1>
          <p class="fd-lead fd-rise" style="--d: 2">
            Control a Windows PC from your browser. Share a 6-digit code, click Yes, done.
          </p>
          <div class="d-flex flex-wrap align-items-start gap-3 mb-4 fd-rise" style="--d: 3">
            <DownloadButton />
            <a class="btn btn-fd-ghost btn-lg" href="#how">How it works</a>
          </div>
          <ul class="fd-trust fd-rise" style="--d: 4">
            <li><AppIcon name="shield" :size="15" /> peer to peer, encrypted</li>
            <li><AppIcon name="eye-off" :size="15" /> nothing stored</li>
            <li><AppIcon name="code" :size="15" /> MIT licensed</li>
          </ul>
        </div>

        <div class="col-lg-5 fd-rise" style="--d: 2">
          <div id="connect" class="fd-card fd-card-glow">
            <div class="fd-card-head">
              <span class="fd-icon-badge"><AppIcon name="monitor" /></span>
              <div>
                <h2 class="fd-card-title">Connect to a computer</h2>
                <p class="fd-card-sub">Type the code from the other PC.</p>
              </div>
            </div>

            <div v-if="authStore.error" class="alert alert-danger py-2 small">
              {{ authStore.error }}
            </div>

            <form @submit.prevent="handleConnect">
              <label class="fd-label" for="remote-code">remote computer code</label>
              <input
                id="remote-code"
                class="form-control fd-code-input"
                :value="formattedInput"
                placeholder="000 - 000"
                inputmode="numeric"
                autocomplete="off"
                spellcheck="false"
                :disabled="busy"
                @input="onCodeInput"
              />
              <button
                class="btn btn-fd-primary btn-lg w-100 mt-3 d-inline-flex align-items-center justify-content-center gap-2"
                type="submit"
                :disabled="busy || code.length !== PAIRING_CODE_LENGTH"
              >
                <span v-if="busy" class="spinner-border spinner-border-sm" aria-hidden="true"></span>
                <template v-else>Connect <AppIcon name="arrow-right" :size="18" /></template>
              </button>
            </form>

            <div v-if="connectError" class="alert alert-danger py-2 small mt-3 mb-0">
              {{ connectError }}
            </div>

            <p class="fd-hint mt-3 mb-0">The other side clicks <strong>Yes</strong>. Then you're in.</p>
          </div>
        </div>
      </div>
    </div>
  </section>

  <!-- Share this computer + how it works -->
  <section id="share" class="container fd-section">
    <div class="row g-4">
      <div class="col-lg-6">
        <div class="fd-card h-100 d-flex flex-column">
          <div class="fd-card-head fd-card-head-split">
            <div class="d-flex align-items-center gap-3">
              <span class="fd-icon-badge" :class="{ 'is-success': identity }">
                <AppIcon name="hash" />
              </span>
              <div>
                <h2 class="fd-card-title">Share this computer</h2>
                <p class="fd-card-sub">Let someone control this PC.</p>
              </div>
            </div>
            <span v-if="identityChecked" class="fd-status" :class="identity ? 'is-on' : 'is-off'">
              <span class="fd-status-dot"></span>
              {{ identity ? 'agent running' : 'agent not detected' }}
            </span>
          </div>

          <!-- What the host program's window shows, live when it is running here. -->
          <div class="fd-term mb-3" aria-live="polite">
            <div class="fd-term-bar">
              <span class="fd-term-dot"></span>
              <span class="fd-term-dot"></span>
              <span class="fd-term-dot"></span>
              <span class="ms-2">freedesk.exe</span>
            </div>
            <div class="fd-term-body">
              <div v-if="!identityChecked">
                <span class="fd-term-k">$</span> looking for the host agent
                <span class="fd-cursor"></span>
              </div>
              <template v-else-if="identity">
                <div>
                  <span class="fd-term-k">[host-agent]</span> host registered (name="{{ identity.name }}")
                </div>
                <div><span class="fd-term-k">[host-agent]</span> THIS COMPUTER'S CODE:</div>
                <div class="fd-term-code">{{ formatPairingCode(identity.code) }}</div>
                <div><span class="fd-term-k">[host-agent]</span> web page: {{ siteOrigin }}</div>
                <div>
                  <span class="fd-term-k">[host-agent]</span> waiting for connection requests
                  <span class="fd-cursor"></span>
                </div>
              </template>
              <template v-else>
                <div><span class="fd-term-k">$</span> freedesk.exe</div>
                <div class="fd-term-dim">waiting for the program to start <span class="fd-cursor"></span></div>
              </template>
            </div>
          </div>

          <template v-if="identity">
            <p class="fd-hint">
              Share this code. You click <strong>Yes</strong> on every connection. New code on
              every start.
            </p>
            <div v-if="hostUpdate" class="fd-update mb-3">
              <AppIcon name="download" :size="16" />
              <span>
                <strong>Update available.</strong> This computer runs {{ identity.version }};
                {{ hostUpdate.version }} is out.
              </span>
              <a class="btn btn-fd-primary btn-sm" :href="hostUpdate.downloadUrl">
                Get {{ hostUpdate.version }}
              </a>
            </div>
            <div class="mt-auto">
              <button
                class="btn btn-fd-ghost d-inline-flex align-items-center gap-2"
                type="button"
                @click="copyCode"
              >
                <AppIcon :name="copied ? 'check' : 'copy'" :size="16" />
                {{ copied ? 'Copied' : 'Copy code' }}
              </button>
            </div>
          </template>

          <template v-else-if="identityChecked">
            <ol class="fd-steps mb-4">
              <li>
                <span><strong>Download</strong> the zip. Unpack it anywhere.</span>
              </li>
              <li>
                <span>
                  <strong>Run <code>freedesk.exe</code>.</strong> No install. Allow network
                  access if asked.
                </span>
              </li>
              <li>
                <span><strong>Share the code</strong> it shows.</span>
              </li>
            </ol>
            <div class="d-flex flex-wrap align-items-start gap-3 mt-auto">
              <DownloadButton />
              <button
                class="btn btn-fd-ghost btn-lg d-inline-flex align-items-center gap-2"
                type="button"
                @click="loadIdentity"
              >
                <AppIcon name="refresh" :size="16" /> Check again
              </button>
            </div>
            <p class="fd-hint mt-3 mb-0">
              <template v-if="isWindows">This page updates by itself once it runs.</template>
              <template v-else>The host runs on Windows 10/11. Connecting works from anywhere.</template>
              Only connecting out? You don't need it.
            </p>
            <p class="fd-hint mt-2 mb-0">
              <AppIcon name="github" :size="14" /> Open source, MIT licensed.
              <a :href="GITHUB_URL" target="_blank" rel="noopener">Read the code on GitHub</a>.
              Windows may warn about an unknown publisher: the build isn't signed yet. Choose
              <strong>More info → Run anyway</strong>, or check the SHA-256 on the release page.
            </p>
          </template>
        </div>
      </div>

      <div id="how" class="col-lg-6">
        <div class="fd-card h-100">
          <div class="fd-card-head">
            <span class="fd-icon-badge is-pink"><AppIcon name="mouse-pointer" /></span>
            <div>
              <h2 class="fd-card-title">How it works</h2>
              <p class="fd-card-sub">Three steps. One minute.</p>
            </div>
          </div>
          <div class="fd-how">
            <div class="fd-how-step">
              <span class="fd-how-num">01</span>
              <div>
                <strong>Run the host program</strong>
                <p>On the PC to share. It shows a 6-digit code.</p>
              </div>
            </div>
            <div class="fd-how-step">
              <span class="fd-how-num">02</span>
              <div>
                <strong>Enter the code</strong>
                <p>Open this page anywhere. Type it. Connect.</p>
              </div>
            </div>
            <div class="fd-how-step">
              <span class="fd-how-num">03</span>
              <div>
                <strong>Click Yes on the host</strong>
                <p>A window pops up there. Yes, and the screen is yours.</p>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </section>

  <!-- Why FreeDesk -->
  <section class="container pt-5 pb-5">
    <div class="fd-section-head">
      <span class="fd-eyebrow">why freedesk</span>
      <h2 class="fd-h2">Small on purpose.</h2>
    </div>
    <div class="row g-3">
      <div v-for="feature in features" :key="feature.title" class="col-md-6 col-lg-3">
        <div class="fd-feature">
          <span class="fd-icon-badge"><AppIcon :name="feature.icon" /></span>
          <h3>{{ feature.title }}</h3>
          <p>{{ feature.text }}</p>
        </div>
      </div>
    </div>
    <p class="fd-hint mt-4 mb-0">
      Windows 10/11 hosts · primary monitor · no audio · some networks (CGNAT) can't connect
      directly.
      <a :href="GITHUB_URL" target="_blank" rel="noopener">Details on GitHub.</a>
    </p>
  </section>
</template>
