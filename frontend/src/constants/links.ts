// External links the viewer shows. The GitHub repository is configurable so a
// fork's deployment points at its own releases (VITE_GITHUB_REPO=owner/repo).
// The asset names must match what .github/workflows/release.yml publishes.

const DEFAULT_GITHUB_REPO = 'YahyaAltintop/freedesk'

export const GITHUB_REPO: string =
  (import.meta.env.VITE_GITHUB_REPO ?? '').trim() || DEFAULT_GITHUB_REPO

export const GITHUB_URL = `https://github.com/${GITHUB_REPO}`
export const RELEASES_URL = `${GITHUB_URL}/releases`
export const LICENSE_URL = `${GITHUB_URL}/blob/main/LICENSE`
export const ARCHITECTURE_DOC_URL = `${GITHUB_URL}/blob/main/docs/ARCHITECTURE.md`
// What leaves the computer, where it goes and for how long. The host's Privacy
// button opens the same file (host-agent internal/config PrivacyURL).
export const PRIVACY_URL = `${GITHUB_URL}/blob/main/PRIVACY.md`

// The host programs attached to every release, with the licenses of the code
// in them built in: for Windows the program itself, one file; for macOS a disk
// image that opens on the app.
export const HOST_ASSET_NAMES = {
  windows: 'freedesk.exe',
  mac: 'FreeDesk.dmg',
} as const

// Unauthenticated REST endpoint (60 requests/hour per IP; answers are cached).
export const LATEST_RELEASE_API_URL = `https://api.github.com/repos/${GITHUB_REPO}/releases/latest`
