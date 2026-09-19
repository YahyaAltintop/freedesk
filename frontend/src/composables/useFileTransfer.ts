import {
  computed,
  onScopeDispose,
  ref,
  shallowReactive,
  watch,
  type ComputedRef,
  type Ref,
} from 'vue'
import {
  CHUNK_BYTES,
  MAX_BATCH_BYTES,
  MAX_BATCH_FILES,
  MAX_FILE_BYTES,
  SEND_HIGH_WATER,
  SEND_LOW_WATER,
  TRANSFER_APPROVAL_TIMEOUT_MS,
} from '@/constants/webrtc'
import { createSink, SinkTooLargeError, type FileSink } from '@/utils/fileSink'
import {
  isRetryable,
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
  // Batches that can be sent again: their files are still held and at least
  // one row of them is worth another attempt.
  retryable: ComputedRef<string[]>
  // Offers a batch. Rejected files never leave the browser. `pasted` marks
  // files that came from a Ctrl+V rather than a drop, which asks the host to
  // put them on its own clipboard once they are saved.
  send: (files: File[], pasted?: boolean) => void
  // Stops a batch, whether it is waiting for an answer or already sending.
  cancel: (batchId: string) => void
  // Asks the host's operator to pick files to send here.
  request: () => void
  // Saves one offered file. MUST be called from a click: the browser's save
  // dialog is refused without a user gesture.
  save: (row: TransferRow) => void
  // Sends the unfinished files of a batch again, as a new batch.
  retry: (batchId: string) => void
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

  // Files kept for as long as their batch has rows on screen.
  //
  // Held from the moment a batch is offered rather than from the point one
  // fails, because there is no single moment where a batch "has failed": the
  // host can report a write error long after this side pushed the last byte
  // and stopped watching, by which time runBatch has returned normally and
  // there is no catch left to run. Deriving this from the rows instead means a
  // late f-error and an early f-reject are handled by the same code.
  //
  // Entries are therefore kept for batches that succeeded too, until the rows
  // go: "Clear finished", a retry, or the session ending. A File is a handle to
  // something on disk rather than its bytes, so holding one costs nothing until
  // it is read — the same property that lets sendFile slice a few hundred
  // megabytes without the tab noticing — and a session is capped at 200 files.
  //
  // Reactive because `retryable` reads it. A plain Map would leave that
  // computed depending on `rows` alone, so clearing this without also touching
  // a row — which is exactly what happens when the session ends — would leave
  // the stale answer cached and a Retry button on screen with no files behind
  // it. Shallow, so the File arrays are handed back unwrapped.
  const held = shallowReactive(new Map<string, File[]>())

  const busyCount = computed(
    () =>
      rows.value.filter(
        (r) => r.status === 'awaiting' || r.status === 'sending' || r.status === 'receiving',
      ).length,
  )
  const failedCount = computed(
    () =>
      rows.value.filter(
        (r) => r.status === 'failed' || r.status === 'declined' || r.status === 'timeout',
      ).length,
  )
  const retryable = computed(() => {
    // Nothing can be sent again without a channel to send it over, whatever
    // the rows say: a row that failed before the session ended keeps its own
    // reason, so it would otherwise still look retryable.
    if (!ready.value) {
      return []
    }
    const ids = new Set<string>()
    for (const row of rows.value) {
      if (held.has(row.batchId) && isRetryable(row)) {
        ids.add(row.batchId)
      }
    }
    return [...ids]
  })

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
      void onChunk(event.data as ArrayBuffer)
      return
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
      case 'f-offer':
        if (message.dir === 'down') {
          offered(message.id, message.files)
        }
        break
      case 'f-complete':
        void finishIncoming(message.id, message.index, message.size)
        break
      case 'f-error':
        batch?.abort.abort()
        settleBatch(message.id, 'failed', message.reason)
        // The same frame ends an incoming file: the host uses it for whichever
        // direction was in flight.
        void abortIncoming(message.reason)
        break
    }
  }

  // --- host → viewer ------------------------------------------------------

  // The file currently arriving. One at a time: the viewer accepts a file, the
  // host streams it, and only then is the next one asked for.
  let incoming: { batchId: string; index: number; sink: FileSink; row: TransferRow } | null = null
  let pendingRequest: string | null = null

  function request(): void {
    const ch = channel.value
    if (!ready.value || !ch || ch.readyState !== 'open' || pendingRequest) {
      return
    }
    pendingRequest = `r${++nextBatch}-${Date.now().toString(36)}`
    post({ t: 'f-request', id: pendingRequest })
  }

  // The operator picked files. They are listed but nothing is fetched until
  // someone clicks Save — so a request never pulls bytes on its own.
  function offered(batchId: string, files: { name: string; size: number }[]): void {
    pendingRequest = null
    for (const [index, file] of files.entries()) {
      rows.value.push({
        batchId,
        index,
        name: file.name,
        size: file.size,
        sent: 0,
        status: 'ready',
        dir: 'down',
      })
    }
  }

  async function save(row: TransferRow): Promise<void> {
    if (row.dir !== 'down' || row.status !== 'ready' || incoming) {
      return
    }
    let sink: FileSink
    try {
      // Inside the click, so the save dialog is allowed to open.
      sink = await createSink(row.name, row.size)
    } catch (err) {
      if (err instanceof SinkTooLargeError) {
        row.status = 'failed'
        row.reason = 'no-fsa'
      }
      // Anything else is the viewer dismissing the save dialog: leave the row
      // as it was so they can try again.
      return
    }
    row.status = 'receiving'
    row.sent = 0
    incoming = { batchId: row.batchId, index: row.index, sink, row }
    post({ t: 'f-accept', id: row.batchId, index: row.index })
  }

  async function onChunk(chunk: ArrayBuffer): Promise<void> {
    if (!incoming) {
      return // payload with nothing accepted behind it
    }
    const active = incoming
    try {
      // Awaited: this is what stops the writes queueing up and putting the
      // memory back that streaming to disk just saved.
      await active.sink.write(chunk)
    } catch {
      await abortIncoming('io')
      return
    }
    if (incoming === active) {
      active.row.sent = Math.min(active.row.sent + chunk.byteLength, active.row.size)
    }
  }

  async function finishIncoming(batchId: string, index: number, size: number): Promise<void> {
    if (!incoming || incoming.batchId !== batchId || incoming.index !== index) {
      return
    }
    const active = incoming
    incoming = null
    if (active.row.sent !== size) {
      // Fewer bytes arrived than the host says it sent.
      await active.sink.abort()
      active.row.status = 'failed'
      active.row.reason = 'io'
      return
    }
    try {
      await active.sink.close()
    } catch {
      active.row.status = 'failed'
      active.row.reason = 'io'
      return
    }
    active.row.status = 'done'
    post({ t: 'f-done', id: batchId, index })
  }

  async function abortIncoming(reason: TransferReason): Promise<void> {
    if (!incoming) {
      return
    }
    const active = incoming
    incoming = null
    await active.sink.abort()
    active.row.status = 'failed'
    active.row.reason = reason
  }

  function send(files: File[], pasted = false): void {
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
        dir: 'up',
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

    held.set(id, files)
    post({
      t: 'f-offer',
      id,
      dir: 'up',
      files: files.map((f) => ({ name: f.name, size: f.size })),
      ...(pasted ? { clip: true } : {}),
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

  // retry sends the files of a failed batch again, as a new batch with a new
  // id — this is not resume. Files that did land keep their rows and are not
  // sent twice: the host would store them beside the originals as " (2)",
  // which is not what anyone clicking Retry is asking for.
  function retry(batchId: string): void {
    const files = held.get(batchId)
    if (!files || !ready.value) {
      return
    }
    const finished = new Set(
      rowsOf(batchId)
        .filter((r) => r.status === 'done')
        .map((r) => r.index),
    )
    held.delete(batchId)
    rows.value = rows.value.filter((r) => r.batchId !== batchId || r.status === 'done')
    const again = files.filter((_, index) => !finished.has(index))
    if (again.length > 0) {
      send(again)
    }
  }

  function clearFinished(): void {
    rows.value = rows.value.filter(
      (r) =>
        r.status === 'awaiting' ||
        r.status === 'sending' ||
        r.status === 'receiving' ||
        r.status === 'ready',
    )
    // Nothing can offer those retries any more, so nothing should still be
    // holding the files behind them.
    for (const id of [...held.keys()]) {
      if (!rows.value.some((r) => r.batchId === id)) {
        held.delete(id)
      }
    }
  }

  function abortAll(reason: TransferReason): void {
    for (const batch of batches.values()) {
      batch.abort.abort()
      settleBatch(batch.id, 'failed', reason)
    }
    batches.clear()
    held.clear()
    pendingRequest = null
    void abortIncoming(reason)
    // A file that was offered but never saved can no longer be fetched.
    for (const row of rows.value) {
      if (row.status === 'ready') {
        row.status = 'failed'
        row.reason = reason
      }
    }
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

  return {
    rows,
    busyCount,
    failedCount,
    retryable,
    send,
    cancel,
    retry,
    clearFinished,
    request,
    save: (r) => void save(r),
  }
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
