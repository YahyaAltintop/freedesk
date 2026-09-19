import { onScopeDispose, ref, watch, type Ref } from 'vue'
import { MAX_CLIPBOARD_BYTES } from '@/constants/webrtc'

export interface ClipboardSync {
  // Text the host sent that could not be put on this machine's clipboard
  // without a click. Null when there is nothing waiting.
  pending: Ref<string | null>
  // Puts the waiting text on the local clipboard. MUST be called from a click:
  // that gesture is what browsers require.
  copyPending: () => Promise<void>
}

// The clipboard message, the same shape in both directions. It travels on the
// file channel rather than the input one: clipboard text runs to a couple of
// hundred kilobytes, and on the ordered input channel a large paste would queue
// ahead of every mouse move behind it.
interface ClipboardMessage {
  t: 'cb'
  text: string
  trunc?: boolean
}

// Keeps the clipboards in step while a session is connected.
//
// This is a SYNC, not an interception. Ctrl+C and Ctrl+V keep being forwarded
// to the host exactly as before — and because the text is already on the host's
// clipboard by the time the keystroke lands, the forwarded Ctrl+V pastes it
// natively. Intercepting the shortcut instead would mean a viewer talking to an
// agent that does not understand the replacement kills Ctrl+V outright, which
// is a worse regression than not having the feature.
//
// Reading the local clipboard is attempted two ways, because browsers differ
// and neither is guaranteed:
//   - the `paste` event, which needs no permission but only fires on a real
//     Ctrl+V and may not carry text on a non-editable element in every browser;
//   - navigator.clipboard.readText(), which is exact but permission-gated and
//     refused outright in Firefox and Safari without a gesture.
// Whichever works, works. If neither does, the host→viewer direction still
// functions on its own.
export function useClipboardSync(
  channel: Ref<RTCDataChannel | null>,
  enabled: Ref<boolean>,
  focused: Ref<boolean>,
): ClipboardSync {
  const pending = ref<string | null>(null)

  // What the host last sent us, and what we last sent the host. Compared to
  // stop a value bouncing between the two machines.
  let lastFromHost = ''
  let lastToHost = ''

  function post(text: string): void {
    const ch = channel.value
    if (!enabled.value || !ch || ch.readyState !== 'open') {
      return
    }
    if (text === '' || text === lastFromHost || text === lastToHost) {
      return
    }
    if (new Blob([text]).size > MAX_CLIPBOARD_BYTES) {
      return // the host would refuse it anyway
    }
    lastToHost = text
    const message: ClipboardMessage = { t: 'cb', text }
    ch.send(JSON.stringify(message))
  }

  // --- this machine → host -------------------------------------------------

  const onPaste = (event: ClipboardEvent): void => {
    if (!enabled.value) {
      return
    }
    const text = event.clipboardData?.getData('text/plain') ?? ''
    if (text) {
      post(text)
    }
    // The event is NOT cancelled: nothing on this page consumes a paste, and
    // cancelling it would be a way to break something later without noticing.
  }

  // readLocal asks for the clipboard outright. Refused in most browsers
  // without a prompt the user has already answered, which is fine — it is the
  // second of two ways in, not the only one.
  async function readLocal(): Promise<void> {
    if (!enabled.value || !navigator.clipboard?.readText) {
      return
    }
    try {
      post(await navigator.clipboard.readText())
    } catch {
      // Not permitted here. The paste event still covers the common case.
    }
  }

  // --- host → this machine -------------------------------------------------

  function onMessage(event: MessageEvent): void {
    if (typeof event.data !== 'string' || !enabled.value) {
      return
    }
    let message: unknown
    try {
      message = JSON.parse(event.data)
    } catch {
      return
    }
    if (typeof message !== 'object' || message === null) {
      return
    }
    const m = message as Record<string, unknown>
    if (m.t !== 'cb' || typeof m.text !== 'string' || m.text === '') {
      return
    }
    void applyLocal(m.text)
  }

  async function applyLocal(text: string): Promise<void> {
    lastFromHost = text
    try {
      await navigator.clipboard.writeText(text)
      pending.value = null
    } catch {
      // Firefox and Safari want a gesture, and any browser refuses when the
      // page is not focused. Keep it so a click can finish the job rather than
      // dropping what the other person copied.
      pending.value = text
    }
  }

  async function copyPending(): Promise<void> {
    const text = pending.value
    if (text === null) {
      return
    }
    try {
      await navigator.clipboard.writeText(text)
      pending.value = null
      return
    } catch {
      // Fall through to the way that needs no permission at all.
    }
    const area = document.createElement('textarea')
    area.value = text
    area.setAttribute('readonly', '')
    area.style.cssText = 'position:fixed;top:-1000px;opacity:0'
    document.body.appendChild(area)
    area.select()
    try {
      document.execCommand('copy')
      pending.value = null
    } catch {
      // Nothing left to try; the chip stays so the text is not lost.
    } finally {
      area.remove()
    }
  }

  // Taking control is the moment the local clipboard is most likely to have
  // something the viewer wants over there.
  watch(focused, (isFocused) => {
    if (isFocused) {
      void readLocal()
    }
  })

  watch(
    channel,
    (ch, previous) => {
      previous?.removeEventListener('message', onMessage)
      ch?.addEventListener('message', onMessage)
    },
    { immediate: true },
  )

  watch(enabled, (on) => {
    if (!on) {
      pending.value = null
      lastFromHost = ''
      lastToHost = ''
    }
  })

  document.addEventListener('paste', onPaste)
  onScopeDispose(() => {
    document.removeEventListener('paste', onPaste)
    channel.value?.removeEventListener('message', onMessage)
  })

  return { pending, copyPending }
}
