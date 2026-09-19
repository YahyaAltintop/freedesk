<script setup lang="ts">
import { computed } from 'vue'
import { isRetryable, reasonText, type TransferRow } from '@/types/transfer'

const props = defineProps<{
  rows: TransferRow[]
  refusal: string | null
  canReceive: boolean
  // Batches whose files the browser is still holding.
  retryable: string[]
}>()

const emit = defineEmits<{
  close: []
  pick: []
  request: []
  save: [row: TransferRow]
  cancel: [batchId: string]
  retry: [batchId: string]
  clear: []
}>()

const hasFinished = computed(() => props.rows.some((r) => !isBusy(r) && r.status !== 'ready'))

// Newest first: a panel that grows downwards pushes what just happened out of
// sight.
const ordered = computed(() => [...props.rows].reverse())

function percent(row: TransferRow): number {
  return row.size === 0 ? 0 : Math.min(100, Math.round((row.sent / row.size) * 100))
}

function sizeText(bytes: number): string {
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(1)} GB`
  if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(1)} MB`
  if (bytes >= 1024) return `${Math.round(bytes / 1024)} KB`
  return `${bytes} bytes`
}

function statusText(row: TransferRow): string {
  switch (row.status) {
    case 'awaiting':
      return 'Waiting for the other person to accept…'
    case 'ready':
      return `Ready to save · ${sizeText(row.size)}`
    case 'sending':
    case 'receiving':
      return `${sizeText(row.sent)} of ${sizeText(row.size)}`
    case 'done':
      return row.dir === 'down' ? `Saved · ${sizeText(row.size)}` : `Sent · ${sizeText(row.size)}`
    case 'cancelled':
      return 'Cancelled'
    case 'timeout':
      return 'No answer on the other computer'
    case 'declined':
      return 'The other person declined'
    default:
      return reasonText(row.reason)
  }
}

function isBusy(row: TransferRow): boolean {
  return row.status === 'awaiting' || row.status === 'sending' || row.status === 'receiving'
}

// Two conditions, and both are needed: the way it ended has to be worth
// another attempt, and the browser has to still be holding the file.
function canRetry(row: TransferRow): boolean {
  return isRetryable(row) && props.retryable.includes(row.batchId)
}
</script>

<template>
  <aside class="fd-panel d-flex flex-column" role="dialog" aria-label="File transfer">
    <header class="fd-panel-head d-flex align-items-center justify-content-between px-3 py-2">
      <span class="fd-panel-title">Send files</span>
      <button
        class="btn btn-sm fd-panel-close"
        type="button"
        aria-label="Close"
        @mousedown.prevent
        @click="emit('close')"
      >
        ✕
      </button>
    </header>

    <div class="fd-panel-section px-3 py-2">
      <div class="d-flex gap-2">
        <button
          class="btn btn-fd-ghost btn-sm flex-fill"
          type="button"
          @mousedown.prevent
          @click="emit('pick')"
        >
          Send files…
        </button>
        <button
          class="btn btn-fd-ghost btn-sm flex-fill"
          type="button"
          :disabled="!canReceive"
          :title="
            canReceive
              ? 'A file picker opens on the other computer'
              : 'The other computer cannot open a file picker'
          "
          @mousedown.prevent
          @click="emit('request')"
        >
          Get files…
        </button>
      </div>
      <p class="fd-panel-note mb-0 mt-2">
        Drag files onto the screen to send them. To receive, the person at the other computer
        chooses what to share.
      </p>
    </div>

    <p v-if="refusal" class="fd-panel-refusal mb-0 px-3 py-2">{{ refusal }}</p>

    <ul v-if="rows.length" class="fd-panel-list list-unstyled mb-0 px-2 py-2">
      <li v-for="row in ordered" :key="`${row.batchId}-${row.index}`" class="fd-row px-2 py-2">
        <div class="d-flex align-items-center justify-content-between gap-2">
          <span class="fd-row-name" :title="row.name">{{ row.name }}</span>
          <button
            v-if="row.status === 'ready'"
            class="btn btn-sm fd-row-save"
            type="button"
            @mousedown.prevent
            @click="emit('save', row)"
          >
            Save
          </button>
          <button
            v-else-if="isBusy(row) && row.dir === 'up'"
            class="btn btn-sm fd-row-cancel"
            type="button"
            @mousedown.prevent
            @click="emit('cancel', row.batchId)"
          >
            Cancel
          </button>
          <!-- Acts on the whole batch, like Cancel above it: the other person
               is asked once per batch, so a batch is what can be tried again.
               Every unfinished row of one therefore carries the button, and
               the first click settles it for all of them. -->
          <button
            v-else-if="canRetry(row)"
            class="btn btn-sm fd-row-retry"
            type="button"
            title="Send the files of this batch that did not arrive"
            @mousedown.prevent
            @click="emit('retry', row.batchId)"
          >
            Retry
          </button>
        </div>
        <div
          v-if="row.status === 'sending' || row.status === 'receiving'"
          class="progress mt-1"
          role="progressbar"
          :aria-valuenow="percent(row)"
          aria-valuemin="0"
          aria-valuemax="100"
        >
          <div class="progress-bar" :style="{ width: `${percent(row)}%` }"></div>
        </div>
        <span class="fd-row-meta d-block mt-1" :class="{ 'is-bad': row.status === 'failed' }">
          {{ statusText(row) }}
        </span>
      </li>
    </ul>
    <p v-else class="fd-panel-empty mb-0 px-3 py-3">Nothing sent yet.</p>

    <footer v-if="hasFinished" class="fd-panel-foot px-3 py-2">
      <button
        class="btn btn-sm fd-panel-clear"
        type="button"
        @mousedown.prevent
        @click="emit('clear')"
      >
        Clear finished
      </button>
    </footer>
  </aside>
</template>

<style scoped>
/* Chrome, not stage: this uses the app's own tokens rather than the stage's
   fixed black, so it follows the theme like everything outside the video. */
.fd-panel {
  width: min(340px, calc(100vw - 1.5rem));
  max-height: min(46vh, 420px);
  background: var(--fd-bg-elev);
  border: 1px solid var(--fd-border);
  border-radius: var(--fd-radius-sm);
  box-shadow: var(--fd-card-shadow);
  color: var(--fd-ink);
  overflow: hidden;
}

.fd-panel-title {
  font-family: var(--fd-font-mono);
  font-size: 0.82rem;
  font-weight: 700;
  letter-spacing: -0.01em;
}

.fd-panel-close,
.fd-panel-clear {
  color: var(--fd-muted);
  padding: 0 0.35rem;
  line-height: 1.2;
}
.fd-panel-close:hover,
.fd-panel-clear:hover {
  color: var(--fd-ink);
}

.fd-panel-head {
  border-bottom: 1px solid var(--fd-border);
}

.fd-panel-section + .fd-panel-section,
.fd-panel-foot {
  border-top: 1px solid var(--fd-border);
}

.fd-panel-note,
.fd-panel-empty {
  color: var(--fd-faint);
  font-size: 0.72rem;
  line-height: 1.5;
}

.fd-panel-refusal {
  color: var(--fd-badge-pink-color);
  background: var(--fd-badge-pink-bg);
  font-size: 0.75rem;
}

.fd-panel-list {
  overflow-y: auto;
}

.fd-row + .fd-row {
  border-top: 1px solid var(--fd-border);
}

.fd-row-name {
  font-size: 0.82rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fd-row-save {
  font-size: 0.72rem;
  padding: 0.05rem 0.5rem;
  flex-shrink: 0;
  color: #fff;
  background-image: var(--fd-gradient);
  border: 0;
  border-radius: 8px;
}

.fd-row-cancel,
.fd-row-retry {
  color: var(--fd-muted);
  font-size: 0.72rem;
  padding: 0 0.35rem;
  flex-shrink: 0;
}
.fd-row-cancel:hover,
.fd-row-retry:hover {
  color: var(--fd-ink);
}

.fd-row-meta {
  font-family: var(--fd-font-mono);
  font-size: 0.68rem;
  color: var(--fd-muted);
}
.fd-row-meta.is-bad {
  color: var(--fd-badge-pink-color);
}

.progress {
  height: 4px;
  background: var(--fd-surface-strong);
}
.progress-bar {
  background-image: var(--fd-gradient);
}
</style>
