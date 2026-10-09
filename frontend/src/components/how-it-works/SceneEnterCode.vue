<script setup lang="ts">
import { computed } from 'vue'
import AppIcon from '@/components/AppIcon.vue'

// How it works, step 2, in any browser: the code goes into this site's
// "Connect to a computer" card digit by digit, Connect is pressed, and the
// page waits for the other side. Beats: --at / --to, see assets/how-it-works.css.
const props = defineProps<{ code: string; site: string }>()

// When each digit is typed. The " - " between the halves comes with the
// fourth, the way the real input formats the code.
const TYPE_AT = [0.16, 0.205, 0.25, 0.295, 0.34, 0.385]

const keys = computed(() => {
  const typed = [...props.code].map((digit, index) => ({
    text: digit,
    at: TYPE_AT[Math.min(index, TYPE_AT.length - 1)],
  }))
  typed.splice(3, 0, { text: ' - ', at: TYPE_AT[3] })
  return typed
})
</script>

<template>
  <div class="hs" aria-hidden="true">
    <div class="hs-cam hs-cam-enter">
      <div class="hs-backdrop"></div>

      <div class="hs-bw" style="left: 3em; top: 2.9em; width: 44em; height: 25.6em">
        <div class="hs-bw-bar">
          <span class="hs-bw-dot"></span>
          <span class="hs-bw-dot"></span>
          <span class="hs-bw-dot"></span>
          <span class="hs-bw-addr"><AppIcon name="lock" /><span>{{ site }}</span></span>
          <span class="hs-bw-end"></span>
        </div>
        <div class="hs-bw-page">
          <div class="hs-site-nav">
            <span class="hs-site-mark"><AppIcon name="monitor" /></span>
            <span class="hs-site-name">FreeDesk</span>
          </div>
        </div>

        <div class="hs-hero" style="left: 2.6em; top: 10.2em">
          <div class="hs-hero-eyebrow"><span>free · open source · no account</span></div>
          <div class="hs-hero-h"><span>Remote desktop,</span><span class="is-grad">as simple as a code.</span></div>
          <div class="hs-hero-lead"><span>Control a Windows PC from your browser.</span></div>
          <div class="hs-hero-btns">
            <span class="hs-hero-btn"><span>Download for Windows</span></span>
            <span class="hs-hero-btn is-ghost"><span>How it works</span></span>
          </div>
        </div>

        <div class="hs-card" style="left: 23em; top: 8.3em; width: 19em; height: 12.4em">
          <div class="hs-card-badge"><AppIcon name="monitor" /></div>
          <div class="hs-card-title"><span>Connect to a computer</span></div>
          <div class="hs-card-sub"><span>Type the code from the other PC.</span></div>
          <div class="hs-card-label"><span>remote computer code</span></div>
          <div class="hs-input" style="left: 1.2em; top: 4.9em; width: 16.6em; height: 2.6em">
            <div class="hs-input-focus hs-in-out" style="--at: 0.11; --to: 0.555"></div>
            <span class="hs-input-ph hs-out" style="--to: 0.16">000 - 000</span>
            <span class="hs-input-txt">
              <span
                v-for="(key, index) in keys"
                :key="index"
                class="hs-ch hs-type"
                :style="{ '--at': key.at }"
                >{{ key.text }}</span
              >
              <span class="hs-in-out" style="--at: 0.11; --to: 0.555"><span class="hs-caret"></span></span>
            </span>
          </div>
          <div
            class="hs-connect"
            style="left: 1.2em; top: 8.1em; width: 16.6em; height: 2.1em; --at: 0.4; --press: 0.555"
          >
            <span class="hs-connect-l hs-out" style="--to: 0.555">Connect <AppIcon name="arrow-right" /></span>
            <span class="hs-connect-l hs-in" style="--at: 0.555"><span class="hs-spin"></span></span>
          </div>
          <div class="hs-card-hint hs-out" style="--to: 0.58">
            <span>The other side clicks <b>Yes</b>. Then you're in.</span>
          </div>
          <div class="hs-card-hint hs-in" style="--at: 0.58">
            <span class="hs-pulse"></span><span>Waiting for host approval…</span>
          </div>
        </div>
      </div>

      <div class="hs-ring" style="left: 42.2em; top: 17.9em; --at: 0.11"></div>
      <div class="hs-ring" style="left: 38.6em; top: 20.7em; --at: 0.555"></div>
      <div class="hs-cur hs-cur-enter">
        <svg viewBox="0 0 14 22">
          <path
            d="M1 1v16.2l4.1-3.9 2.9 6.6 2.6-1.1-2.8-6.5h5.7z"
            fill="#fff"
            stroke="#111"
            stroke-width="1.1"
            stroke-linejoin="round"
          />
        </svg>
      </div>
    </div>

    <div class="hs-chip is-viewer"><span>Your browser</span></div>
  </div>
</template>
