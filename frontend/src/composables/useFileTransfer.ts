import { computed, onScopeDispose, ref, watch, type ComputedRef, type Ref } from 'vue'
import {
  CHUNK_BYTES,
  MAX_BATCH_BYTES,
  MAX_BATCH_FILES,
  MAX_FILE_BYTES,
  SEND_HIGH_WATER,
  SEND_LOW_WATER,
  TRANSFER_APPROVAL_TIMEOUT_MS,
} from '@/constants/webrtc'
import {
  parseTransferMessage,
  type TransferOutbound,
  type TransferReason,
  type TransferRow,
} from '@/types/transfer'

export interface FileTransfer {
  // One row per file, newest batch last.
  rows: Ref<TransferRow[]>
  // Files still queued, offered or sending.
  busyCount: ComputedRef<number>
  failedCount: ComputedRef<number>
  // Offers a batch. Rejected files never leave the browser.
  send: (files: File[]) => void
  // Stops a batch, whether it is waiting for an answer or already sending.
  cancel: (batchId: string) => void
  // Forgets finished rows.
  clearFinished: () => void
}

// A batch in flight, from the offer to the last file.
interface Batch {
  id: string
  files: File[]
  abort: AbortController
  // Resolved by the host's f-accept, rejected by f-reject or the timeout.
  settle?: (accepted: boolean, reason?: TransferReason) => void
}

let nextBatch = 0

// Drives file transfers over the `file` DataChannel: offers a batch, waits for
// the person at the other end to accept, then streams the files in order.
//
// The host asks its operator once per batch and answers with one accept, so a
// batch is the unit here too — rows of the same batch resolve together.
export function useFileTransfer(
  channel: Ref<RTCDataChannel | null>,
  ready: Ref<boolean>,
): FileTransfer {
  const rows = ref<TransferRow[]>([])
  const batches = new Map<string, Batch>()

  const busyCount = computed(
    () => rows.value.filter((r) => r.status === 'awaiting' || r.status === 'sending').length,
  )
  const failedCount = computed(
    () =>
      rows.value.filter(
        (r) => r.status === 'failed' || r.status === 'declined' || r.status === 'timeout',
      ).length,
  )

  function rowsOf(batchId: string): TransferRow[] {
    return rows.value.filter((r) => r.batchId === batchId)
  }

  // Finishes every unfinished row of a batch the same way: the host decides per
  // batch, so the rows cannot disagree.
  function settleBatch(batchId: string, status: TransferRow['status'], reason?: TransferReason) {
    for (const row of rowsOf(batchId)) {
      if (row.status === 'awaiting' || row.status === 'sending') {
        row.status = status
        row.reason = reason
      }
    }
  }

  function post(message: TransferOutbound): void {
    const ch = channel.value
    if (ch && ch.readyState === 'open') {
      ch.send(JSON.stringify(message))
    }
  }

  function onMessage(event: MessageEvent): void {
    if (typeof event.data !== 'string') {
      return // the host sends no payload in this direction yet
    }
    const message = parseTransferMessage(event.data)
    if (!message) {
      return
    }
    const batch = batches.get(message.id)
    switch (message.t) {
      case 'f-accept':
        batch?.settle?.(true)
        break
      case 'f-reject':
        batch?.settle?.(false, message.reason)
        break
      case 'f-progress': {
        // The host's count is the honest one: ours only says what has been
        // handed to the channel, which runs ahead of what has reached the disk.
        const row = rowsOf(message.id)[message.index]
        if (row && row.status === 'sending') {
          row.sent = Math.min(message.sent, row.size)
        }
        break
      }
      case 'f-done': {
        const row = rowsOf(message.id)[message.index]
        if (row) {
          row.sent = row.size
          row.status = 'done'
        }
        break
      }
      case 'f-error':
        batch?.abort.abort()
        settleBatch(message.id, 'failed', message.reason)
        break
    }
  }

  function send(files: File[]): void {
    const ch = channel.value
    if (!ready.value || !ch || ch.readyState !== 'open' || files.length === 0) {
      return
    }

    const id = `b${++nextBatch}-${Date.now().toString(36)}`
    const batch: Batch = { id, files, abort: new AbortController() }
    batches.set(id, batch)

    for (const [index, file] of files.entries()) {
      rows.value.push({
        batchId: id,
        index,
        name: file.name,
        size: file.size,
        sent: 0,
        status: 'awaiting',
      })
    }

    // Refuse locally what the host would refuse anyway, so the operator is not
    // asked about something that cannot work.
    const total = files.reduce((sum, f) => sum + f.size, 0)
    const tooBig = files.some((f) => f.size > MAX_FILE_BYTES || f.size === 0)
    if (files.length > MAX_BATCH_FILES || total > MAX_BATCH_BYTES || tooBig) {
      settleBatch(id, 'failed', 'too-large')
      batches.delete(id)
      return
    }

    post({
      t: 'f-offer',
      id,
      dir: 'up',
      files: files.map((f) => ({ name: f.name, size: f.size })),
    })

    void runBatch(batch)
  }

  async function runBatch(batch: Batch): Promise<void> {
    try {
      await waitForAnswer(batch)
      for (const [index, file] of batch.files.entries()) {
        await sendFile(batch, index, file)
      }
    } catch (err) {
      const reason = err instanceof TransferError ? err.reason : undefined
      const status = err instanceof TransferError ? err.status : 'failed'
      settleBatch(batch.id, status, reason)
      if (status === 'cancelled' || status === 'timeout') {
        post({ t: 'f-cancel', id: batch.id })
      }
    } finally {
      batches.delete(batch.id)
    }
  }

  // waitForAnswer resolves when the host accepts, and throws otherwise. The
  // wait is longer than the host's own prompt so their answer always wins the
  // race against this timeout.
  function waitForAnswer(batch: Batch): Promise<void> {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        batch.settle = undefined
        reject(new TransferError('timeout'))
      }, TRANSFER_APPROVAL_TIMEOUT_MS)

      batch.settle = (accepted, reason) => {
        clearTimeout(timer)
        batch.settle = undefined
        if (!accepted) {
          reject(new TransferError(reason === 'denied' ? 'declined' : 'failed', reason))
          return
        }
        for (const row of rowsOf(batch.id)) {
          row.status = 'sending'
        }
        resolve()
      }

      batch.abort.signal.addEventListener('abort', () => {
        clearTimeout(timer)
        reject(new TransferError('cancelled'))
      })
    })
  }

  async function sendFile(batch: Batch, index: number, file: File): Promise<void> {
    const row = rowsOf(batch.id)[index]
    for (let offset = 0; offset < file.size; ) {
      await drain(batch)
      const ch = channel.value
      if (!ch || ch.readyState !== 'open') {
        throw new TransferError('failed', 'gone')
      }
      // slice() is a lazy view; only this chunk is ever read into memory, which
      // is what keeps a few hundred MB from landing in the tab's heap.
      const chunk = await file.slice(offset, offset + CHUNK_BYTES).arrayBuffer()
      if (batch.abort.signal.aborted) {
        throw new TransferError('cancelled')
      }
      ch.send(chunk)
      offset += chunk.byteLength
      if (row && row.status === 'sending') {
        // Optimistic until the host's own count arrives: subtract what is still
        // queued in the browser so the bar does not run to 100% while the
        // network is still working through it.
        row.sent = Math.max(row.sent, Math.max(0, offset - ch.bufferedAmount))
      }
    }
  }

  // drain waits until the channel's send queue has room. `send()` never blocks
  // and the queue is unbounded, so without this the whole file would be handed
  // over as fast as the disk can read it.
  function drain(batch: Batch): Promise<void> {
    const ch = channel.value
    if (!ch || ch.bufferedAmount < SEND_HIGH_WATER) {
      return Promise.resolve()
    }
    return new Promise((resolve, reject) => {
      const done = () => {
        ch.removeEventListener('bufferedamountlow', onLow)
        batch.abort.signal.removeEventListener('abort', onAbort)
      }
      const onLow = () => {
        done()
        resolve()
      }
      const onAbort = () => {
        done()
        reject(new TransferError('cancelled'))
      }
      ch.addEventListener('bufferedamountlow', onLow)
      batch.abort.signal.addEventListener('abort', onAbort)
      // The event only fires on a downward crossing, so a queue that already
      // drained would otherwise wait forever.
      if (ch.bufferedAmount < SEND_HIGH_WATER || ch.readyState !== 'open') {
        onLow()
      }
    })
  }

  function cancel(batchId: string): void {
    batches.get(batchId)?.abort.abort()
    settleBatch(batchId, 'cancelled')
    post({ t: 'f-cancel', id: batchId })
  }

  function clearFinished(): void {
    rows.value = rows.value.filter((r) => r.status === 'awaiting' || r.status === 'sending')
  }

  function abortAll(reason: TransferReason): void {
    for (const batch of batches.values()) {
      batch.abort.abort()
      settleBatch(batch.id, 'failed', reason)
    }
    batches.clear()
  }

  watch(
    channel,
    (ch, previous) => {
      previous?.removeEventListener('message', onMessage)
      if (!ch) {
        return
      }
      ch.bufferedAmountLowThreshold = SEND_LOW_WATER
      ch.addEventListener('message', onMessage)
    },
    { immediate: true },
  )

  // The channel closing ends everything in flight. Note this is NOT the same as
  // a brief `disconnected`: the channel stays open through that, the send pump
  // simply stalls on a queue that is not draining, and picks up if ICE recovers.
  watch(ready, (isReady) => {
    if (!isReady) {
      abortAll('gone')
    }
  })

  onScopeDispose(() => {
    channel.value?.removeEventListener('message', onMessage)
    abortAll('gone')
  })

  return { rows, busyCount, failedCount, send, cancel, clearFinished }
}

// TransferError carries how a row should end, so one catch can settle a batch.
class TransferError extends Error {
  constructor(
    readonly status: TransferRow['status'],
    readonly reason?: TransferReason,
  ) {
    super(status)
  }
}
