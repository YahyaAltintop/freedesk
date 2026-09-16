// External links the viewer shows. The GitHub repository is configurable so a
// fork's deployment points at its own releases (VITE_GITHUB_REPO=owner/repo).
// The asset name must match what .github/workflows/release.yml publishes.

const DEFAULT_GITHUB_REPO = 'YahyaAltintop/freedesk'

export const GITHUB_REPO: string =
  (import.meta.env.VITE_GITHUB_REPO ?? '').trim() || DEFAULT_GITHUB_REPO

export const GITHUB_URL = `https://github.com/${GITHUB_REPO}`
export const RELEASES_URL = `${GITHUB_URL}/releases`
export const LICENSE_URL = `${GITHUB_URL}/blob/main/LICENSE`
export const ARCHITECTURE_DOC_URL = `${GITHUB_URL}/blob/main/docs/ARCHITECTURE.md`

// The Windows host agent bundle (exe + ffmpeg) attached to every release.
export const HOST_ASSET_NAME = 'freedesk-host-windows-x64.zip'

// GitHub redirects this to that asset of the most recent full release.
export const LATEST_HOST_ASSET_URL = `${RELEASES_URL}/latest/download/${HOST_ASSET_NAME}`

// Unauthenticated REST endpoint (60 requests/hour per IP; answers are cached).
export const LATEST_RELEASE_API_URL = `https://api.github.com/repos/${GITHUB_REPO}/releases/latest`
