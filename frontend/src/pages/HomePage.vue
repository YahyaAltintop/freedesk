<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth.store'
import { fetchHostByCode, removeStaleHost } from '@/services/host.service'
import { fetchLocalIdentity, type LocalIdentity } from '@/services/localAgent'
import { serverNow } from '@/services/serverTime'
import { isHostOnline, isHostStaleForGc } from '@/utils/presence'
import { toFriendlyError } from '@/utils/firebaseErrors'
import { RouteName } from '@/constants/routes'

const router = useRouter()
const authStore = useAuthStore()

// ---- This computer (code served by the local host agent) -------------------

const identity = ref<LocalIdentity | null>(null)
const identityLoading = ref(true)
const copied = ref(false)

async function loadIdentity(): Promise<void> {
  identityLoading.value = true
  identity.value = await fetchLocalIdentity()
  identityLoading.value = false
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

onMounted(loadIdentity)

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
</script>

<template>
  <div class="container py-5" style="max-width: 960px">
    <div class="text-center mb-5">
      <h1 class="fw-bold">FreeDesk</h1>
      <p class="text-secondary mb-0">No account, no password — just the computer code.</p>
    </div>

    <div v-if="authStore.error" class="alert alert-danger">{{ authStore.error }}</div>

    <div class="row g-4">
      <!-- This computer -->
      <div class="col-md-5">
        <div class="card h-100 shadow-sm">
          <div class="card-body d-flex flex-column">
            <h5 class="card-title">This computer</h5>

            <div v-if="identityLoading" class="text-center py-4">
              <div class="spinner-border text-primary" role="status" aria-hidden="true"></div>
            </div>

            <template v-else-if="identity">
              <p class="text-secondary small mb-1">{{ identity.name }}</p>
              <div class="display-6 fw-bold font-monospace mb-2">
                {{ formatCode(identity.code) }}
              </div>
              <p class="small text-secondary">
                Give this code to whoever wants to connect to you. Every incoming request must
                be approved on this computer before a connection is established. The code
                changes each time the host agent starts.
              </p>
              <button
                class="btn btn-outline-primary btn-sm mt-auto align-self-start"
                type="button"
                @click="copyCode"
              >
                {{ copied ? 'Copied ✓' : 'Copy Code' }}
              </button>
            </template>

            <template v-else>
              <div class="alert alert-warning py-2">The host agent is not running on this computer.</div>
              <p class="small text-secondary">
                To allow connections to this computer, start the <code>host-agent</code> app;
                your code will appear here. You don't need it if you only want to connect out.
              </p>
              <button
                class="btn btn-outline-secondary btn-sm mt-auto align-self-start"
                type="button"
                @click="loadIdentity"
              >
                Try Again
              </button>
            </template>
          </div>
        </div>
      </div>

      <!-- Connect to a remote computer -->
      <div class="col-md-7">
        <div class="card h-100 shadow-sm">
          <div class="card-body d-flex flex-column">
            <h5 class="card-title">Connect to a remote computer</h5>
            <p class="text-secondary small">Enter the remote computer's 9-digit code.</p>

            <form @submit.prevent="handleConnect">
              <div class="input-group input-group-lg mb-3">
                <input
                  class="form-control font-monospace text-center"
                  :value="formattedInput"
                  placeholder="000 000 000"
                  inputmode="numeric"
                  autocomplete="off"
                  :disabled="busy"
                  @input="onCodeInput"
                />
                <button
                  class="btn btn-primary px-4"
                  type="submit"
                  :disabled="busy || code.length !== 9"
                >
                  <span
                    v-if="busy"
                    class="spinner-border spinner-border-sm me-2"
                    aria-hidden="true"
                  ></span>
                  Connect
                </button>
              </div>
            </form>

            <div v-if="connectError" class="alert alert-danger py-2">{{ connectError }}</div>

            <p class="small text-secondary mt-auto mb-0">
              Your connection request is established once the person at the remote computer
              clicks Yes in the approval window.
            </p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
