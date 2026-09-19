// Where an incoming file's bytes go while it arrives.
//
// Two ways, because browsers differ. The File System Access API streams
// straight to the disk with nothing held in memory, which is what a few hundred
// MB needs — but it is Chromium-only and it needs a user gesture to open its
// save dialog. Everywhere else the bytes accumulate in memory and are handed
// over as a download at the end, which is fine for something small and
// disastrous for something large. So the fallback has a hard limit and refuses
// loudly rather than quietly eating a tab's memory.

import { BLOB_FALLBACK_MAX_BYTES } from '@/constants/webrtc'

export interface FileSink {
  write: (chunk: ArrayBuffer) => Promise<void>
  // Finishes the file. For the in-memory path this is what triggers the
  // browser's download.
  close: () => Promise<void>
  // Throws the partial file away.
  abort: () => Promise<void>
}

export class SinkTooLargeError extends Error {
  constructor() {
    super('too large for this browser to save')
  }
}

// streamingSupported reports whether the browser can write to disk as the file
// arrives, rather than holding all of it first.
export function streamingSupported(): boolean {
  return typeof window !== 'undefined' && 'showSaveFilePicker' in window
}

// createSink asks the viewer where to put the file and returns somewhere to
// write it.
//
// MUST be called from a user gesture (a click): the save dialog is refused
// without one, which is why the UI puts a Save button on each row instead of
// starting the download by itself.
export async function createSink(name: string, size: number): Promise<FileSink> {
  if (streamingSupported()) {
    return createStreamingSink(name)
  }
  if (size > BLOB_FALLBACK_MAX_BYTES) {
    throw new SinkTooLargeError()
  }
  return createMemorySink(name)
}

async function createStreamingSink(name: string): Promise<FileSink> {
  const pick = window.showSaveFilePicker
  if (!pick) {
    throw new SinkTooLargeError()
  }
  const handle = await pick.call(window, { suggestedName: name })
  const writable = await handle.createWritable()
  return {
    // Awaited per chunk on purpose: this is the receiving side's backpressure.
    // Without it the writes queue up and the memory saved by streaming goes
    // straight back into the queue.
    write: (chunk) => writable.write(chunk),
    close: () => writable.close(),
    abort: async () => {
      try {
        await writable.abort()
      } catch {
        // Already closed, or the file was removed underneath us.
      }
    },
  }
}

function createMemorySink(name: string): FileSink {
  let chunks: ArrayBuffer[] | null = []
  return {
    write: async (chunk) => {
      chunks?.push(chunk)
    },
    close: async () => {
      if (!chunks) {
        return
      }
      const url = URL.createObjectURL(new Blob(chunks))
      chunks = null // let the pieces go; the Blob owns a copy now
      const link = document.createElement('a')
      link.href = url
      link.download = name
      link.click()
      // Revoked on the next tick: revoking immediately can cancel the download
      // in some browsers before it has read the URL.
      setTimeout(() => URL.revokeObjectURL(url), 60_000)
    },
    abort: async () => {
      chunks = null
    },
  }
}
