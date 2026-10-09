<script setup lang="ts">
import { computed } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import { formatPairingCode } from '@/utils/pairingCode'

// How it works, step 1, on the PC to share: freedesk.exe is double-clicked,
// its window publishes the code, Copy code puts it on the clipboard to send.
// The window and its texts follow the real program (host-agent/internal/ui
// and the agent's log lines). Beats: --at / --to, see assets/how-it-works.css.
const props = defineProps<{ code: string; site: string }>()

const shown = computed(() => formatPairingCode(props.code))
</script>

<template>
  <div class="hs" aria-hidden="true">
    <div class="hs-cam hs-cam-run">
      <div class="hs-wall"></div>

      <div class="hs-dicon">
        <div class="hs-dicon-sel hs-in" style="--at: 0.095"></div>
        <div class="hs-tile"><AppIcon name="monitor" /></div>
        <span class="hs-dicon-label">freedesk.exe</span>
      </div>
      <div class="hs-ring" style="left: 11.1em; top: 6.1em; --at: 0.095"></div>
      <div class="hs-ring" style="left: 11.1em; top: 6.1em; --at: 0.125"></div>

      <div class="hs-win hs-pop" style="left: 15em; top: 3em; width: 20em; height: 18em; --at: 0.15">
        <div class="hs-win-bar">
          <span class="hs-win-ico"></span>
          <span class="hs-win-name">FreeDesk</span>
          <span class="hs-win-caps">
            <span class="hs-win-cap"><AppIcon name="minus" /></span>
            <span class="hs-win-cap"><AppIcon name="square" /></span>
            <span class="hs-win-cap"><AppIcon name="x" /></span>
          </span>
        </div>
        <div class="hs-row" style="top: 2.65em"><span class="hs-t-label">This computer's code</span></div>
        <div class="hs-row hs-in-out" style="top: 4.6em; --at: 0.19; --to: 0.31">
          <span class="hs-t-note">Publishing this computer's code…</span>
        </div>
        <div class="hs-row hs-in" style="top: 3.5em; --at: 0.31"><span class="hs-t-code">{{ shown }}</span></div>
        <div class="hs-wbtn hs-press" style="left: 7em; top: 7.1em; width: 6em; height: 1.5em; --at: 0.56">
          <span class="hs-wbtn-t hs-out" style="--to: 0.56">Copy code</span>
          <span class="hs-wbtn-t is-ok hs-in" style="--at: 0.56"><AppIcon name="check" />Copied</span>
        </div>
        <div class="hs-row hs-in" style="top: 9.25em; --at: 0.33">
          <span class="hs-t-line">Web page: {{ site }}</span>
        </div>
        <div class="hs-row hs-in" style="top: 10.25em; --at: 0.37">
          <span class="hs-t-line">Waiting for connection requests…</span>
        </div>
        <div class="hs-act"><span>Activity</span></div>
        <div class="hs-log">
          <div class="hs-in" style="--at: 0.2">[host-agent] host registered</div>
          <div class="hs-in" style="--at: 0.32">[host-agent] this computer's code: {{ shown }}</div>
          <div class="hs-in" style="--at: 0.37">[host-agent] waiting for connection requests…</div>
        </div>
      </div>

      <div class="hs-share hs-in" style="left: 36.3em; top: 7.2em; --at: 0.64">
        <div class="hs-share-k"><AppIcon name="send" /><span>Share the code</span></div>
        <div class="hs-share-code"><span>{{ shown }}</span></div>
        <div class="hs-share-sub"><span>New code on every start.</span></div>
      </div>

      <div class="hs-taskbar">
        <span class="hs-tb-start"><AppIcon name="windows" /></span>
        <span class="hs-tb-search"></span>
        <span class="hs-tb-app"></span>
        <span class="hs-tb-app"></span>
        <span class="hs-tb-app"></span>
        <span class="hs-tb-fd hs-in" style="--at: 0.16">
          <span class="hs-tb-mark"><AppIcon name="monitor" /></span>
        </span>
      </div>

      <div class="hs-ring" style="left: 25.3em; top: 10.9em; --at: 0.56"></div>
      <div class="hs-cur hs-cur-run">
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

    <div class="hs-chip"><span>Host PC</span></div>
  </div>
</template>
