import { onScopeDispose, readonly, ref, type Ref } from 'vue'

// Fullscreen for the session view, and with it the Keyboard Lock API.
//
// preventDefault() on a keydown cannot stop the shortcuts the browser and the
// window manager claim for themselves — Ctrl+W, Ctrl+T, Ctrl+N, F11, Escape —
// so those keep acting on the viewer's own machine no matter how carefully the
// page handles them. Keyboard Lock is the one web API that routes them to the
// page instead, and it only takes effect while the page is fullscreen. The two
// therefore travel together: enter fullscreen, lock; leave fullscreen, unlock.
//
// Lock is Chromium-only (and refuses on some platforms); elsewhere fullscreen
// still works and those few shortcuts stay local. Alt+Tab and the Windows key
// are taken by the OS before any browser sees them and cannot be forwarded at
// all — the on-screen hint says so.
export interface FullscreenControl {
  // True while this document is fullscreen.
  isFullscreen: Readonly<Ref<boolean>>
  // False in browsers without the Fullscreen API, so the button can be hidden.
  supported: boolean
  // Enters fullscreen on `target`, or leaves it. Must run from a user gesture.
  toggle: () => Promise<void>
}

function lockKeyboard(): void {
  // Locking every key the browser is willing to give up; a rejection (no
  // permission, unsupported platform) just leaves those shortcuts local.
  void navigator.keyboard?.lock().catch(() => undefined)
}

function unlockKeyboard(): void {
  navigator.keyboard?.unlock()
}

export function useFullscreen(target: Ref<HTMLElement | null>): FullscreenControl {
  const isFullscreen = ref(document.fullscreenElement !== null)
  const supported = typeof document.documentElement.requestFullscreen === 'function'

  // Covers our own toggle() as well as F11 and the long-press on Escape that
  // leaves fullscreen behind our back.
  const onFullscreenChange = (): void => {
    isFullscreen.value = document.fullscreenElement !== null
    if (isFullscreen.value) {
      lockKeyboard()
    } else {
      unlockKeyboard()
    }
  }

  document.addEventListener('fullscreenchange', onFullscreenChange)

  async function toggle(): Promise<void> {
    const el = target.value
    if (!supported || !el) {
      return
    }
    try {
      if (document.fullscreenElement) {
        await document.exitFullscreen()
      } else {
        await el.requestFullscreen({ navigationUI: 'hide' })
      }
    } catch {
      // The request can be refused (no user gesture left, a policy blocks it);
      // `fullscreenchange` never fires, so the state stays honest as it is.
    }
  }

  onScopeDispose(() => {
    document.removeEventListener('fullscreenchange', onFullscreenChange)
    unlockKeyboard()
    // Leaving the session should not leave the browser fullscreen behind.
    if (document.fullscreenElement) {
      void document.exitFullscreen().catch(() => undefined)
    }
  })

  return { isFullscreen: readonly(isFullscreen), supported, toggle }
}
