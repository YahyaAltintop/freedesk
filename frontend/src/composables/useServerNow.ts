import { computed, type ComputedRef } from 'vue'
import { useNow } from '@/composables/useNow'
import { serverTimeOffset } from '@/services/serverTime'

// Reactive estimate of the server clock (epoch ms). Re-evaluates on every tick
// of the shared app clock AND the moment the server offset becomes known, so a
// presence badge computed from it stays correct without any data change.
export function useServerNow(): ComputedRef<number> {
  const now = useNow()
  return computed(() => now.value + serverTimeOffset.value)
}
