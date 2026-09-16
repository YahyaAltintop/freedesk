// Looks up the latest GitHub release so the download button can show the
// version and size and link straight to the zip. Everything degrades to the
// generic ".../releases/latest/download/<asset>" link when the API is
// unreachable, and to the Releases page when no release exists yet.

import { ref, type Ref } from 'vue'
import {
  HOST_ASSET_NAME,
  LATEST_HOST_ASSET_URL,
  LATEST_RELEASE_API_URL,
  RELEASES_URL,
} from '@/constants/links'

export interface HostRelease {
  /** Tag name without the leading "v". */
  version: string
  downloadUrl: string
  sizeBytes: number | null
  publishedAt: string | null
}

export type ReleaseLookup =
  | { state: 'found'; release: HostRelease }
  /** The repository has no published release yet. */
  | { state: 'none' }
  /** API unreachable or rate-limited: use the generic "latest" link. */
  | { state: 'unknown' }

const CACHE_KEY = 'freedesk.latestRelease'
const CACHE_TTL_MS = 10 * 60 * 1000

interface CacheEntry {
  at: number
  lookup: ReleaseLookup
}

function readCache(): ReleaseLookup | null {
  try {
    const raw = sessionStorage.getItem(CACHE_KEY)
    if (!raw) {
      return null
    }
    const entry = JSON.parse(raw) as Partial<CacheEntry>
    if (typeof entry.at !== 'number' || !entry.lookup || Date.now() - entry.at > CACHE_TTL_MS) {
      return null
    }
    return entry.lookup
  } catch {
    return null
  }
}

function writeCache(lookup: ReleaseLookup): void {
  try {
    const entry: CacheEntry = { at: Date.now(), lookup }
    sessionStorage.setItem(CACHE_KEY, JSON.stringify(entry))
  } catch {
    // Storage blocked (private mode, quota): the lookup simply repeats next time.
  }
}

interface GitHubAsset {
  name?: string
  browser_download_url?: string
  size?: number
}

interface GitHubRelease {
  tag_name?: string
  html_url?: string
  published_at?: string
  assets?: GitHubAsset[]
}

export async function fetchLatestHostRelease(timeoutMs = 5000): Promise<ReleaseLookup> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  try {
    const response = await fetch(LATEST_RELEASE_API_URL, {
      signal: controller.signal,
      headers: { Accept: 'application/vnd.github+json' },
    })
    if (response.status === 404) {
      return { state: 'none' }
    }
    if (!response.ok) {
      return { state: 'unknown' }
    }
    const data = (await response.json()) as GitHubRelease
    const asset = data.assets?.find((a) => a.name === HOST_ASSET_NAME)
    return {
      state: 'found',
      release: {
        version: (data.tag_name ?? '').replace(/^v/, ''),
        // A release without the zip (build failed) still gets a working link.
        downloadUrl: asset?.browser_download_url ?? data.html_url ?? RELEASES_URL,
        sizeBytes: typeof asset?.size === 'number' ? asset.size : null,
        publishedAt: data.published_at ?? null,
      },
    }
  } catch {
    return { state: 'unknown' }
  } finally {
    clearTimeout(timer)
  }
}

// One shared lookup for every download button on the page.
const latest: Ref<ReleaseLookup | null> = ref(null)
let started = false

export function useLatestRelease(): Ref<ReleaseLookup | null> {
  if (!started) {
    started = true
    const cached = readCache()
    if (cached) {
      latest.value = cached
    } else {
      void fetchLatestHostRelease().then((lookup) => {
        latest.value = lookup
        if (lookup.state !== 'unknown') {
          writeCache(lookup)
        }
      })
    }
  }
  return latest
}

export { LATEST_HOST_ASSET_URL }
