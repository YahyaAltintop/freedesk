<script setup lang="ts">
import { computed } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import { RELEASES_URL } from '@/constants/links'
import { useLatestRelease } from '@/services/releases'

// Links to the Windows host agent zip once the GitHub release lookup has
// confirmed one (with version and size). Before that, when the lookup fails
// or when no release exists, it links to the Releases page, which never 404s.
const props = withDefaults(defineProps<{ compact?: boolean; variant?: 'primary' | 'ghost' }>(), {
  compact: false,
  variant: 'primary',
})

const lookup = useLatestRelease()

const href = computed(() => {
  const current = lookup.value
  return current?.state === 'found' ? current.release.downloadUrl : RELEASES_URL
})

const label = computed(() => {
  if (lookup.value?.state === 'none') {
    return 'See releases'
  }
  return props.compact ? 'Download' : 'Download for Windows'
})

const details = computed(() => {
  const current = lookup.value
  if (current?.state === 'found') {
    const parts = [`v${current.release.version}`]
    if (current.release.sizeBytes) {
      parts.push(formatSize(current.release.sizeBytes))
    }
    parts.push('zip · no installer')
    return parts.join(' · ')
  }
  if (current?.state === 'none') {
    return 'no build published yet'
  }
  return 'windows 10/11 · zip · no installer'
})

const buttonClass = computed(() => (props.variant === 'ghost' ? 'btn-fd-ghost' : 'btn-fd-primary'))

function formatSize(bytes: number): string {
  const mb = bytes / 1048576
  return mb >= 10 ? `${Math.round(mb)} MB` : `${mb.toFixed(1)} MB`
}
</script>

<template>
  <a
    v-if="compact"
    class="btn btn-sm d-inline-flex align-items-center gap-1"
    :class="buttonClass"
    :href="href"
  >
    <AppIcon name="download" :size="16" />
    {{ label }}
  </a>
  <div v-else class="fd-download">
    <a
      class="btn btn-lg d-inline-flex align-items-center gap-2 px-4"
      :class="buttonClass"
      :href="href"
    >
      <AppIcon name="windows" :size="18" />
      {{ label }}
    </a>
    <div class="fd-download-meta">{{ details }}</div>
  </div>
</template>
