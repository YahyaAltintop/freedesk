import { ref, type Ref } from 'vue'

// A shared, app-wide "current time" (epoch ms) that advances on a single timer.
// Components and stores read this so time-based UI — like host presence — can
// update without any data change. One interval serves the whole app.

const TICK_MS = 15_000

const now: Ref<number> = ref(Date.now())
let timer: ReturnType<typeof setInterval> | null = null

function ensureTicking(): void {
  if (timer === null) {
    timer = setInterval(() => {
      now.value = Date.now()
    }, TICK_MS)
  }
}

// The raw reactive clock — read `nowValue.value` inside getters/computed.
export const nowValue = now

// Ensures the clock is ticking and returns it (for use in components).
export function useNow(): Ref<number> {
  ensureTicking()
  return now
}
