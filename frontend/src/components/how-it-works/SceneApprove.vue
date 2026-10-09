<script setup lang="ts">
import { computed } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import { formatPairingCode } from '@/utils/pairingCode'

// How it works, step 3, back on the PC to share: the consent box gets a Yes,
// then the viewer's browser shows that desktop and moves a window on it. The
// box's title and text are the program's own (host-agent/internal/consent);
// 45 seconds is approvalTimeout in host-agent/cmd/host/main.go. Beats: --at /
// --to, see assets/how-it-works.css. This step runs longer than the others
// (HowItWorks.vue) so the box can be read before Yes.
const props = defineProps<{ code: string; site: string }>()

const shown = computed(() => formatPairingCode(props.code))
</script>

<template>
  <div class="hs" aria-hidden="true">
    <div class="hs-cam hs-cam-approve">
      <div class="hs-wall"></div>

      <div class="hs-win" style="left: 2.6em; top: 2.6em; width: 20em; height: 18em">
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
        <div class="hs-row" style="top: 3.5em"><span class="hs-t-code">{{ shown }}</span></div>
        <div class="hs-wbtn" style="left: 7em; top: 7.1em; width: 6em; height: 1.5em">
          <span class="hs-wbtn-t">Copy code</span>
        </div>
        <div class="hs-row" style="top: 9.25em"><span class="hs-t-line">Web page: {{ site }}</span></div>
        <div class="hs-row" style="top: 10.25em"><span class="hs-t-line">Waiting for connection requests…</span></div>
        <div class="hs-act"><span>Activity</span></div>
        <div class="hs-log">
          <div>[host-agent] host registered</div>
          <div>[host-agent] this computer's code: {{ shown }}</div>
          <div>[host-agent] waiting for connection requests…</div>
        </div>
      </div>

      <div class="hs-dlg hs-pop-out" style="left: 14em; top: 7em; width: 22em; height: 11.8em; --at: 0.04; --to: 0.42">
        <div class="hs-dlg-bar">
          <span class="hs-dlg-title">FreeDesk - Connection request (#1)</span>
          <span class="hs-win-cap"><AppIcon name="x" /></span>
        </div>
        <div class="hs-dlg-body">
          <div class="hs-dlg-ico">?</div>
          <div class="hs-dlg-txt">
            <p>Someone entered this computer's code and wants to connect.</p>
            <p>
              Allow them to see your screen, control this computer, exchange files with it, and share
              copied text?
            </p>
            <p>This request is rejected automatically in 45 seconds.<br />(viewer 7f3a)</p>
          </div>
        </div>
        <div class="hs-dlg-foot">
          <div class="hs-dlg-btn hs-press" style="left: 11.8em; --at: 0.38"><span>Yes</span></div>
          <div class="hs-dlg-btn is-default" style="left: 16.6em"><span>No</span></div>
        </div>
      </div>

      <div class="hs-taskbar">
        <span class="hs-tb-start"><AppIcon name="windows" /></span>
        <span class="hs-tb-search"></span>
        <span class="hs-tb-app"></span>
        <span class="hs-tb-app"></span>
        <span class="hs-tb-app"></span>
        <span class="hs-tb-fd hs-flash" style="--at: 0.05">
          <span class="hs-tb-mark"><AppIcon name="monitor" /></span>
        </span>
      </div>
      <div class="hs-ring" style="left: 28.2em; top: 17.6em; --at: 0.38"></div>
      <div class="hs-cur hs-cur-host">
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

      <div class="hs-backdrop is-cover hs-fade" style="--at: 0.45"></div>
      <div class="hs-bw hs-rise" style="left: 5em; top: 3.2em; width: 40em; height: 24em; --at: 0.46">
        <div class="hs-bw-bar">
          <span class="hs-bw-dot"></span>
          <span class="hs-bw-dot"></span>
          <span class="hs-bw-dot"></span>
          <span class="hs-bw-addr"><AppIcon name="lock" /><span>{{ site }}</span></span>
          <span class="hs-bw-live"><span>connected</span></span>
        </div>
        <div class="hs-bw-page is-desktop">
          <div class="hs-wall"></div>
          <div class="hs-mini-icon"></div>
          <div class="hs-mini-win hs-drag" style="left: 4em; top: 3em; width: 13em; height: 8em">
            <div class="hs-mini-bar"><span>FreeDesk</span></div>
            <div class="hs-row" style="top: 1.9em"><span class="hs-t-label">This computer's code</span></div>
            <div class="hs-row" style="top: 2.55em"><span class="hs-t-code">{{ shown }}</span></div>
            <div class="hs-wbtn" style="left: 4.4em; top: 5.3em; width: 4.2em; height: 1.1em">
              <span class="hs-wbtn-t">Copy code</span>
            </div>
          </div>
          <div class="hs-note-w">
            <div class="hs-note hs-in" style="--at: 0.62">
              <span>You're in: mouse, keyboard, clipboard, files.</span>
            </div>
          </div>
          <div class="hs-mini-task"><span></span><span></span><span></span><span></span></div>
        </div>
      </div>
      <div class="hs-ring" style="left: 13.5em; top: 8.9em; --at: 0.7"></div>
      <div class="hs-cur hs-cur-remote">
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

    <div class="hs-chip hs-out" style="--to: 0.48"><span>Host PC</span></div>
    <div class="hs-chip is-viewer hs-in" style="--at: 0.48"><span>Your browser</span></div>
  </div>
</template>
