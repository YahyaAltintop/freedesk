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
  | { t: 'f-offer'; id: string; dir: TransferDirection; files: FileMeta[] }
  | { t: 'f-accept'; id: string }
  | { t: 'f-cancel'; id: string; reason?: TransferReason }

// Inbound: what the host sends back.
export type TransferInbound =
  | { t: 'f-accept'; id: string }
  | { t: 'f-reject'; id: string; reason: TransferReason }
  | { t: 'f-progress'; id: string; index: number; sent: number }
  | { t: 'f-done'; id: string; index: number }
  | { t: 'f-error'; id: string; reason: TransferReason }

// What a transfer row is doing, in the order it usually happens.
export type TransferStatus =
  | 'awaiting' // offered; the other person is deciding
  | 'sending'
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
