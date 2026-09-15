import { onValue, ref as dbRef } from 'firebase/database'
import { ref, type Ref } from 'vue'
import { db } from '@/firebase'

// Host presence is judged by comparing `lastSeen` (a SERVER timestamp written
// by the host) against the current time. The browser's own clock can be off by
// minutes, which would make every host look offline (or never offline), so the
// SDK's `.info/serverTimeOffset` is used to estimate the server clock instead.

// Estimated (server − local) clock difference in ms; 0 until the SDK reports it.
const offset: Ref<number> = ref(0)
let started = false

// Subscribes to the server clock offset exactly once for the app's lifetime.
export function startServerClock(): void {
  if (started) {
    return
  }
  started = true
  onValue(
    dbRef(db, '.info/serverTimeOffset'),
    (snapshot) => {
      const value: unknown = snapshot.val()
      offset.value = typeof value === 'number' ? value : 0
    },
    () => {
      // Not expected (the path is always readable); keep the last known offset.
    },
  )
}

// Best estimate of the server's current time (epoch ms).
export function serverNow(): number {
  return Date.now() + offset.value
}

// Reactive form of the offset, for composables deriving a ticking server clock.
export const serverTimeOffset: Readonly<Ref<number>> = offset
