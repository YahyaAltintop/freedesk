// Talks to the host agent running on THIS machine over its loopback-only
// endpoint (host-agent internal/localapi). This is how the home page learns
// "this computer's code" — a browser cannot read local processes or files.
// If the agent is not running the request simply fails and the UI says so.

// Must match the agent's default RC_LOCAL_PORT (host-agent internal/config).
const LOCAL_AGENT_IDENTITY_URL = 'http://127.0.0.1:47800/identity'

export interface LocalIdentity {
  code: string
  name: string
  version: string
}

export async function fetchLocalIdentity(timeoutMs = 1500): Promise<LocalIdentity | null> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  try {
    const response = await fetch(LOCAL_AGENT_IDENTITY_URL, { signal: controller.signal })
    if (!response.ok) {
      return null
    }
    const data = (await response.json()) as Partial<LocalIdentity>
    if (typeof data.code !== 'string' || data.code === '') {
      return null
    }
    return { code: data.code, name: data.name ?? '', version: data.version ?? '' }
  } catch {
    return null
  } finally {
    clearTimeout(timer)
  }
}
