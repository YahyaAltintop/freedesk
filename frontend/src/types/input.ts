// Input event messages sent to the host over the DataChannel (docs/PROTOCOL.md).
export type InputMessage =
  | { t: 'm'; x: number; y: number }
  | { t: 'md'; b: number; x: number; y: number }
  | { t: 'mu'; b: number; x: number; y: number }
  | { t: 'w'; dx: number; dy: number }
  | { t: 'kd'; code: string }
  | { t: 'ku'; code: string }
  | { t: 'hello'; v: number }
