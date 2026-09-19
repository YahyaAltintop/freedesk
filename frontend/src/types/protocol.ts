// The host's greeting on the `input` channel (docs/PROTOCOL.md §2.2), and the
// capability names features are gated on.

// Capabilities a host may advertise. Features check for these rather than
// comparing version numbers, so a newer agent can add one without this viewer
// needing to know the numbering.
export const CAP_FILE_SEND = 'file.send' // we may send files to the host
export const CAP_FILE_RECV = 'file.recv' // the host can send files to us
export const CAP_CLIP_TEXT = 'clip.text' // clipboard text is shared

export interface HostHello {
  version: number
  caps: Set<string>
  agent: string
}

// parseHello reads the host's greeting, or returns null for anything else on
// the channel. The viewer receives nothing else there today, but an unknown
// message must be ignored rather than treated as a fault — that is what lets a
// later agent add one.
export function parseHello(raw: string): HostHello | null {
  let value: unknown
  try {
    value = JSON.parse(raw)
  } catch {
    return null
  }
  if (typeof value !== 'object' || value === null) {
    return null
  }
  const m = value as Record<string, unknown>
  if (m.t !== 'hello' || typeof m.v !== 'number') {
    return null
  }
  const caps = Array.isArray(m.caps) ? m.caps.filter((c): c is string => typeof c === 'string') : []
  return {
    version: m.v,
    caps: new Set(caps),
    agent: typeof m.agent === 'string' ? m.agent : '',
  }
}
