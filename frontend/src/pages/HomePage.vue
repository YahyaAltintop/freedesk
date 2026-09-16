<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import AppIcon from '@/components/AppIcon.vue'
import DownloadButton from '@/components/DownloadButton.vue'
import type { IconName } from '@/components/icons'
import { useAuthStore } from '@/stores/auth.store'
import { fetchHostByCode, removeStaleHost } from '@/services/host.service'
import { fetchLocalIdentity, type LocalIdentity } from '@/services/localAgent'
import { serverNow } from '@/services/serverTime'
import { isHostOnline, isHostStaleForGc } from '@/utils/presence'
import { toFriendlyError } from '@/utils/firebaseErrors'
import { RouteName } from '@/constants/routes'
import { GITHUB_URL } from '@/constants/links'

const router = useRouter()
const authStore = useAuthStore()

// The address the host program tells people to open (this very site).
const siteOrigin = window.location.origin

// ---- This computer (code served by the local host agent) -------------------

const identity = ref<LocalIdentity | null>(null)
const identityChecked = ref(false)
const copied = ref(false)
const isWindows = /Windows/i.test(navigator.userAgent)

// The agent is probed again every few seconds (loopback, cheap) so the code
// shows up by itself once the user starts freedesk-host.exe and disappears
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

// "123456789" -> "123 456 789" (partial input groups as far as it goes).
function formatCode(value: string): string {
  return value.replace(/(\d{3})(?=\d)/g, '$1 ').trim()
}

const formattedInput = computed(() => formatCode(code.value))

function onCodeInput(event: Event): void {
  const input = event.target as HTMLInputElement
  code.value = input.value.replace(/\D/g, '').slice(0, 9)
  // Reflect the normalised value so stray characters never linger in the box.
  input.value = formatCode(code.value)
}

async function handleConnect(): Promise<void> {
  connectError.value = null
  if (code.value.length !== 9) {
    connectError.value = 'The code must be 9 digits.'
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
    text: 'No sign-up and no password. The code is the whole handshake, and it changes every time the host starts.',
  },
  {
    icon: 'zap',
    title: 'Direct connection',
    text: 'Screen and input travel straight between the two computers over encrypted WebRTC, never through a server.',
  },
  {
    icon: 'eye-off',
    title: 'Nothing stored',
    text: 'The only records are the temporary code and the connection handshake, deleted the moment the session ends.',
  },
  {
    icon: 'code',
    title: 'Open source',
    text: 'MIT licensed. Read the code, build it yourself, or host your own copy on a free Firebase project.',
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
            See and control another Windows PC straight from your browser. One side runs a small
            program and reads out a 9-digit code; the other types it in here. Nothing to install,
            nothing to sign up for, nothing stored.
          </p>
          <div class="d-flex flex-wrap align-items-start gap-3 mb-4 fd-rise" style="--d: 3">
            <DownloadButton />
            <a class="btn btn-fd-ghost btn-lg" href="#how">How it works</a>
          </div>
          <ul class="fd-trust fd-rise" style="--d: 4">
            <li><AppIcon name="shield" :size="15" /> encrypted, direct connection</li>
            <li><AppIcon name="thumbs-up" :size="15" /> host approves every session</li>
            <li><AppIcon name="code" :size="15" /> MIT licensed</li>
          </ul>
        </div>

        <div class="col-lg-5 fd-rise" style="--d: 2">
          <div id="connect" class="fd-card fd-card-glow">
            <div class="fd-card-head">
              <span class="fd-icon-badge"><AppIcon name="monitor" /></span>
              <div>
                <h2 class="fd-card-title">Connect to a computer</h2>
                <p class="fd-card-sub">Enter the 9-digit code shown on the other computer.</p>
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
                placeholder="000 000 000"
                inputmode="numeric"
                autocomplete="off"
                spellcheck="false"
                :disabled="busy"
                @input="onCodeInput"
              />
              <button
                class="btn btn-fd-primary btn-lg w-100 mt-3 d-inline-flex align-items-center justify-content-center gap-2"
                type="submit"
                :disabled="busy || code.length !== 9"
              >
                <span v-if="busy" class="spinner-border spinner-border-sm" aria-hidden="true"></span>
                <template v-else>Connect <AppIcon name="arrow-right" :size="18" /></template>
              </button>
            </form>

            <div v-if="connectError" class="alert alert-danger py-2 small mt-3 mb-0">
              {{ connectError }}
            </div>

            <p class="fd-hint mt-3 mb-0">
              The person at the other computer has to click <strong>Yes</strong> before you see
              anything.
            </p>
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
                <p class="fd-card-sub">Let someone see and control this PC.</p>
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
              <span class="ms-2">freedesk-host.exe</span>
            </div>
            <div class="fd-term-body">
              <div v-if="!identityChecked">
                <span class="fd-term-k">$</span> looking for the host agent on this computer
                <span class="fd-cursor"></span>
              </div>
              <template v-else-if="identity">
                <div>
                  <span class="fd-term-k">[host-agent]</span> host registered (name="{{ identity.name }}")
                </div>
                <div><span class="fd-term-k">[host-agent]</span> THIS COMPUTER'S CODE:</div>
                <div class="fd-term-code">{{ formatCode(identity.code) }}</div>
                <div><span class="fd-term-k">[host-agent]</span> web page: {{ siteOrigin }}</div>
                <div>
                  <span class="fd-term-k">[host-agent]</span> waiting for connection requests
                  <span class="fd-cursor"></span>
                </div>
              </template>
              <template v-else>
                <div><span class="fd-term-k">$</span> freedesk-host.exe</div>
                <div class="fd-term-dim">
                  waiting for the program to start on this computer <span class="fd-cursor"></span>
                </div>
              </template>
            </div>
          </div>

          <template v-if="identity">
            <p class="fd-hint">
              Give this code to the person who should connect. You will be asked to click
              <strong>Yes</strong> before they see anything, and the code changes every time the
              agent starts.
            </p>
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
                <span><strong>Download</strong> the zip and unpack it anywhere.</span>
              </li>
              <li>
                <span>
                  <strong>Run <code>freedesk-host.exe</code></strong> — no installation. If Windows
                  asks about network access, allow it.
                </span>
              </li>
              <li>
                <span>
                  <strong>Tell the code</strong> that appears here (and in the program's window) to
                  the person who should connect.
                </span>
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
              <template v-if="isWindows">
                This page re-checks by itself once the program is running.
              </template>
              <template v-else>
                The host program runs on Windows 10/11; from this device you can still connect to a
                Windows PC.
              </template>
              You don't need it if you only want to connect to someone else.
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
              <p class="fd-card-sub">Three steps, about a minute.</p>
            </div>
          </div>
          <div class="fd-how">
            <div class="fd-how-step">
              <span class="fd-how-num">01</span>
              <div>
                <strong>Run the host program</strong>
                <p>
                  On the computer to be controlled, run <code>freedesk-host.exe</code>. It shows a
                  9-digit code.
                </p>
              </div>
            </div>
            <div class="fd-how-step">
              <span class="fd-how-num">02</span>
              <div>
                <strong>Enter the code</strong>
                <p>On any other computer, open this page, type the code and press Connect.</p>
              </div>
            </div>
            <div class="fd-how-step">
              <span class="fd-how-num">03</span>
              <div>
                <strong>Click Yes on the host</strong>
                <p>
                  A window pops up on the shared computer. After Yes, its screen appears in your
                  browser and your mouse and keyboard control it. End the session or close the
                  program at any time.
                </p>
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
      Windows 10/11 hosts, primary monitor only, no audio or file transfer, and some networks
      (mobile data, CGNAT) cannot connect directly.
      <a :href="GITHUB_URL" target="_blank" rel="noopener">Full details and source on GitHub.</a>
    </p>
  </section>
</template>
