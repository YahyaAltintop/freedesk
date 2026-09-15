import { onScopeDispose, watch, type Ref } from 'vue'
import type { InputMessage } from '@/types/input'

// Browser wheel deltas come in pixels (0), lines (1) or pages (2); scale the
// latter two to an approximate pixel delta before forwarding.
const WHEEL_LINE_PX = 16
const WHEEL_PAGE_PX = 100

function round4(value: number): number {
  return Math.round(value * 10000) / 10000
}

function clamp01(value: number): number {
  return value < 0 ? 0 : value > 1 ? 1 : value
}

// Maps a browser MouseEvent.button to the protocol button (0=left,1=right,2=middle).
function mapButton(button: number): number | null {
  switch (button) {
    case 0:
      return 0
    case 2:
      return 1
    case 1:
      return 2
    default:
      return null
  }
}

// Attaches pointer/keyboard listeners to the video element and forwards them
// as protocol messages while `active` is true. Coordinates are normalised
// against the actual (letterboxed) video content, not the element box.
//
// Everything the viewer holds down is remembered so it can be released when
// the events that would normally end it can no longer reach us: the window
// loses focus (Alt+Tab, tab switch), the page is hidden, control is disabled,
// or the composable is disposed. Otherwise the host would keep the key or
// button pressed indefinitely.
export function useInputCapture(
  video: Ref<HTMLVideoElement | null>,
  send: (message: InputMessage) => void,
  active: Ref<boolean>,
): void {
  let el: HTMLVideoElement | null = null
  const bound: Array<[EventTarget, string, EventListener]> = []

  const heldKeys = new Set<string>()
  const heldButtons = new Set<number>()
  let lastX = 0.5
  let lastY = 0.5

  function videoCoords(event: MouseEvent, clamp: boolean): { x: number; y: number } | null {
    if (!el) {
      return null
    }
    const rect = el.getBoundingClientRect()
    const vw = el.videoWidth
    const vh = el.videoHeight
    if (!vw || !vh || !rect.width || !rect.height) {
      return null
    }
    // object-fit: contain → the video is centred and letterboxed in the element.
    const scale = Math.min(rect.width / vw, rect.height / vh)
    const displayW = vw * scale
    const displayH = vh * scale
    const offsetX = rect.left + (rect.width - displayW) / 2
    const offsetY = rect.top + (rect.height - displayH) / 2
    let x = (event.clientX - offsetX) / displayW
    let y = (event.clientY - offsetY) / displayH
    if (clamp) {
      // While a button is held the pointer is captured, so keep a drag alive
      // up to the edge of the remote screen instead of dropping the events.
      x = clamp01(x)
      y = clamp01(y)
    } else if (x < 0 || x > 1 || y < 0 || y > 1) {
      return null
    }
    return { x: round4(x), y: round4(y) }
  }

  function remember(c: { x: number; y: number }): void {
    lastX = c.x
    lastY = c.y
  }

  // Releases everything currently held on the host, most recent key first so
  // modifiers (usually pressed first) go up last, like lifting a real hand.
  function releaseAll(): void {
    const keys = Array.from(heldKeys).reverse()
    heldKeys.clear()
    for (const code of keys) {
      send({ t: 'ku', code })
    }
    const buttons = Array.from(heldButtons)
    heldButtons.clear()
    for (const b of buttons) {
      send({ t: 'mu', b, x: lastX, y: lastY })
    }
  }

  const onPointerMove = (event: PointerEvent): void => {
    if (!active.value) {
      return
    }
    const c = videoCoords(event, heldButtons.size > 0)
    if (c) {
      remember(c)
      send({ t: 'm', x: c.x, y: c.y })
    }
  }

  const onPointerDown = (event: PointerEvent): void => {
    if (!active.value) {
      return
    }
    el?.focus()
    const b = mapButton(event.button)
    const c = videoCoords(event, false)
    if (b === null || !c) {
      return
    }
    // Capture so the matching pointerup reaches us even outside the video.
    try {
      el?.setPointerCapture(event.pointerId)
    } catch {
      // Capture is best-effort (e.g. the pointer is already gone).
    }
    remember(c)
    heldButtons.add(b)
    send({ t: 'md', b, x: c.x, y: c.y })
  }

  const onPointerUp = (event: PointerEvent): void => {
    const b = mapButton(event.button)
    if (b === null || !heldButtons.has(b)) {
      return
    }
    heldButtons.delete(b)
    const c = videoCoords(event, true) ?? { x: lastX, y: lastY }
    remember(c)
    if (active.value) {
      send({ t: 'mu', b, x: c.x, y: c.y })
    }
  }

  // pointercancel / lostpointercapture end the gesture without a pointerup.
  const onPointerLost = (): void => {
    if (heldButtons.size === 0) {
      return
    }
    const buttons = Array.from(heldButtons)
    heldButtons.clear()
    if (active.value) {
      for (const b of buttons) {
        send({ t: 'mu', b, x: lastX, y: lastY })
      }
    }
  }

  const onWheel = (event: WheelEvent): void => {
    if (!active.value) {
      return
    }
    event.preventDefault()
    const factor = event.deltaMode === 1 ? WHEEL_LINE_PX : event.deltaMode === 2 ? WHEEL_PAGE_PX : 1
    const dy = Math.round(-event.deltaY * factor)
    const dx = Math.round(event.deltaX * factor)
    if (dx !== 0 || dy !== 0) {
      send({ t: 'w', dx, dy })
    }
  }

  const onContextMenu = (event: MouseEvent): void => {
    event.preventDefault() // forward right-click instead of opening the browser menu
  }

  const onKeyDown = (event: KeyboardEvent): void => {
    if (!active.value) {
      return
    }
    event.preventDefault()
    // Auto-repeat re-sends the key-down (the host repeats too) but the key is
    // only held once.
    heldKeys.add(event.code)
    send({ t: 'kd', code: event.code })
  }

  const onKeyUp = (event: KeyboardEvent): void => {
    if (!active.value) {
      return
    }
    event.preventDefault()
    heldKeys.delete(event.code)
    send({ t: 'ku', code: event.code })
  }

  const onFocusLost = (): void => {
    releaseAll()
  }

  const onVisibilityChange = (): void => {
    if (document.visibilityState === 'hidden') {
      releaseAll()
    }
  }

  function listen<K extends keyof HTMLElementEventMap>(
    target: HTMLElement,
    type: K,
    handler: (event: HTMLElementEventMap[K]) => void,
    options?: AddEventListenerOptions,
  ): void
  function listen<K extends keyof WindowEventMap>(
    target: Window,
    type: K,
    handler: (event: WindowEventMap[K]) => void,
  ): void
  function listen<K extends keyof DocumentEventMap>(
    target: Document,
    type: K,
    handler: (event: DocumentEventMap[K]) => void,
  ): void
  function listen(
    target: EventTarget,
    type: string,
    handler: (event: never) => void,
    options?: AddEventListenerOptions,
  ): void {
    target.addEventListener(type, handler as EventListener, options)
    bound.push([target, type, handler as EventListener])
  }

  function attach(target: HTMLVideoElement): void {
    detach()
    el = target
    listen(target, 'pointermove', onPointerMove)
    listen(target, 'pointerdown', onPointerDown)
    listen(target, 'pointerup', onPointerUp)
    listen(target, 'pointercancel', onPointerLost)
    listen(target, 'lostpointercapture', onPointerLost)
    listen(target, 'wheel', onWheel, { passive: false })
    listen(target, 'contextmenu', onContextMenu)
    listen(target, 'keydown', onKeyDown)
    listen(target, 'keyup', onKeyUp)
    listen(target, 'blur', onFocusLost)
    listen(window, 'blur', onFocusLost)
    listen(document, 'visibilitychange', onVisibilityChange)
  }

  function detach(): void {
    releaseAll()
    for (const [target, type, handler] of bound) {
      target.removeEventListener(type, handler)
    }
    bound.length = 0
    el = null
  }

  watch(video, (value) => (value ? attach(value) : detach()), { immediate: true })
  // Control switched off (session ending): let go of everything first, while
  // the channel may still be open. The host releases on its side as well.
  watch(active, (isActive) => {
    if (!isActive) {
      releaseAll()
    }
  })
  onScopeDispose(detach)
}
