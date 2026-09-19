// Input events sent to the host on the `input` channel (docs/PROTOCOL.md §3).
// The greeting that travels the other way on the same channel is not here: see
// types/protocol.ts.
export type InputMessage =
  | { t: 'm'; x: number; y: number }
  | { t: 'md'; b: number; x: number; y: number }
  | { t: 'mu'; b: number; x: number; y: number }
  | { t: 'w'; dx: number; dy: number }
  | { t: 'kd'; code: string }
  | { t: 'ku'; code: string }
