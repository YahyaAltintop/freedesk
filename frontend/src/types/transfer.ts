// Control messages on the `file` DataChannel (docs/PROTOCOL.md §2). Binary
// frames on the same channel are payload and never appear here.

export type TransferDirection = 'up' | 'down'

// Why a transfer ended without completing. The host decides these; the viewer
// only turns them into a sentence.
export type TransferReason =
  | 'denied'
  | 'busy'
  | 'too-large'
  | 'bad-name'
  | 'no-space'
  | 'io'
  | 'gone'
  | 'no-fsa'

export interface FileMeta {
  name: string
  size: number
}

// Outbound: what this viewer sends.
export type TransferOutbound =
  | { t: 'f-offer'; id: string; dir: TransferDirection; files: FileMeta[]; clip?: boolean }
  | { t: 'f-request'; id: string }
  | { t: 'f-accept'; id: string; index?: number }
  | { t: 'f-done'; id: string; index: number }
  | { t: 'f-cancel'; id: string; reason?: TransferReason }

// Inbound: what the host sends back.
export type TransferInbound =
  | { t: 'f-accept'; id: string }
  | { t: 'f-offer'; id: string; dir: TransferDirection; files: FileMeta[] }
  | { t: 'f-complete'; id: string; index: number; size: number }
  | { t: 'f-reject'; id: string; reason: TransferReason }
  | { t: 'f-progress'; id: string; index: number; sent: number }
  | { t: 'f-done'; id: string; index: number }
  | { t: 'f-error'; id: string; reason: TransferReason }

// What a transfer row is doing, in the order it usually happens.
export type TransferStatus =
  | 'awaiting' // we offered it; the other person is deciding
  | 'ready' // they offered it; waiting for a click to save it here
  | 'sending'
  | 'receiving'
  | 'done'
  | 'declined' // they said no
  | 'timeout' // they never answered
  | 'cancelled' // we stopped it
  | 'failed'

// One file in the panel.
export interface TransferRow {
  batchId: string
  index: number
  name: string
  size: number
  sent: number
  status: TransferStatus
  reason?: TransferReason
  // 'down' rows came from the host and are saved with a click.
  dir: TransferDirection
}

// parseTransferMessage narrows an inbound frame, or returns null for anything
// unrecognised. Unknown types are not an error — that is what lets a later host
// add one without this viewer treating it as a fault.
export function parseTransferMessage(raw: string): TransferInbound | null {
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
  if (typeof m.t !== 'string' || typeof m.id !== 'string') {
    return null
  }
  switch (m.t) {
    case 'f-accept':
      return { t: 'f-accept', id: m.id }
    case 'f-offer': {
      const files = Array.isArray(m.files) ? m.files.map(asFileMeta).filter(isFileMeta) : []
      if (files.length === 0 || (m.dir !== 'up' && m.dir !== 'down')) {
        return null
      }
      return { t: 'f-offer', id: m.id, dir: m.dir, files }
    }
    case 'f-complete':
      return { t: 'f-complete', id: m.id, index: asIndex(m.index), size: asCount(m.size) }
    case 'f-reject':
    case 'f-error':
      return { t: m.t, id: m.id, reason: asReason(m.reason) }
    case 'f-progress':
      return { t: 'f-progress', id: m.id, index: asIndex(m.index), sent: asCount(m.sent) }
    case 'f-done':
      return { t: 'f-done', id: m.id, index: asIndex(m.index) }
    default:
      return null
  }
}

function asFileMeta(value: unknown): FileMeta | null {
  if (typeof value !== 'object' || value === null) {
    return null
  }
  const f = value as Record<string, unknown>
  if (typeof f.name !== 'string' || f.name === '') {
    return null
  }
  if (typeof f.size !== 'number' || !Number.isFinite(f.size) || f.size <= 0) {
    return null
  }
  return { name: f.name, size: f.size }
}

function isFileMeta(value: FileMeta | null): value is FileMeta {
  return value !== null
}

function asReason(value: unknown): TransferReason {
  const known: TransferReason[] = [
    'denied',
    'busy',
    'too-large',
    'bad-name',
    'no-space',
    'io',
    'gone',
    'no-fsa',
  ]
  return known.includes(value as TransferReason) ? (value as TransferReason) : 'io'
}

// Omitted numbers are absent from the wire rather than zero, so a missing one
// means zero rather than a malformed frame.
function asIndex(value: unknown): number {
  return typeof value === 'number' && Number.isInteger(value) && value >= 0 ? value : 0
}

function asCount(value: unknown): number {
  return typeof value === 'number' && value >= 0 ? value : 0
}

// isRetryable decides whether a row that did not finish is worth offering to
// send again. One place, because "can this be tried again" is a judgement
// about each way a transfer ends and it should not be re-derived in a template.
//
// Only uploads. A download is asked for again with "Get files…", and the host
// would have to re-offer it in any case.
export function isRetryable(row: TransferRow): boolean {
  if (row.dir !== 'up') {
    return false
  }
  switch (row.status) {
    // 'declined' is deliberately absent: putting the question back in front of
    // somebody who just said no is the same prompt fatigue the host guards
    // against from its own side.
    case 'timeout':
    case 'cancelled':
      return true
    case 'failed':
      switch (row.reason) {
        // These are decided by what the file is, so a second attempt is
        // refused in exactly the same way.
        case 'bad-name':
        case 'too-large':
        // The session that could have carried it is over, and these rows do
        // not outlive it — a Retry here could only ever be a dead button.
        case 'gone':
          return false
        default:
          return true
      }
    default:
      return false
  }
}

// The sentence shown on a row that did not finish. Phrased as what happened to
// the person waiting, not as the code path that produced it.
export function reasonText(reason: TransferReason | undefined): string {
  switch (reason) {
    case 'denied':
      return 'The other person declined'
    case 'busy':
      return 'Another transfer is already waiting'
    case 'too-large':
      return 'Too large to send'
    case 'bad-name':
      return 'That file name cannot be saved on Windows'
    case 'no-space':
      return 'Not enough space on the other computer'
    case 'gone':
      return 'The session ended'
    case 'no-fsa':
      return 'This browser cannot save a file that large'
    default:
      return 'The other computer could not save it'
  }
}
