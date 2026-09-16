import { readonly, ref, type Ref } from 'vue'

// Light/dark theme. The browser's preference (prefers-color-scheme) is the
// default; a manual choice is remembered per browser in localStorage. The
// inline script in index.html applies the same rule before the first paint
// so the page never flashes the wrong theme; this module keeps it in sync
// afterwards and reacts to OS changes while no manual choice is stored.

export type Theme = 'light' | 'dark'

export const THEME_STORAGE_KEY = 'freedesk.theme'

const THEME_COLOR: Record<Theme, string> = { dark: '#0b0d14', light: '#f6f7fb' }

const media = window.matchMedia('(prefers-color-scheme: dark)')

function readStored(): Theme | null {
  try {
    const value = localStorage.getItem(THEME_STORAGE_KEY)
    return value === 'light' || value === 'dark' ? value : null
  } catch {
    return null
  }
}

function systemTheme(): Theme {
  return media.matches ? 'dark' : 'light'
}

function apply(theme: Theme): void {
  document.documentElement.setAttribute('data-bs-theme', theme)
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', THEME_COLOR[theme])
}

const theme: Ref<Theme> = ref(readStored() ?? systemTheme())
apply(theme.value)

media.addEventListener('change', () => {
  if (readStored() === null) {
    theme.value = systemTheme()
    apply(theme.value)
  }
})

export function useTheme() {
  function setTheme(next: Theme): void {
    theme.value = next
    apply(next)
    try {
      localStorage.setItem(THEME_STORAGE_KEY, next)
    } catch {
      // Storage blocked: the choice still applies for this page view.
    }
  }

  function toggle(): void {
    setTheme(theme.value === 'dark' ? 'light' : 'dark')
  }

  return { theme: readonly(theme), setTheme, toggle }
}
