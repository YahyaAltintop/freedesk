<script setup lang="ts">
import { computed } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import { LATEST_HOST_ASSET_URL, RELEASES_URL } from '@/constants/links'
import { useLatestRelease } from '@/services/releases'

// Links to the Windows host agent zip. Shows version and size once the GitHub
// release lookup has answered; works (via the generic "latest" link) before
// and without it.
const props = withDefaults(defineProps<{ compact?: boolean; variant?: 'primary' | 'light' }>(), {
  compact: false,
  variant: 'primary',
})

const lookup = useLatestRelease()

const href = computed(() => {
  const current = lookup.value
  if (current?.state === 'found') {
    return current.release.downloadUrl
  }
  if (current?.state === 'none') {
    return RELEASES_URL
  }
  return LATEST_HOST_ASSET_URL
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
    parts.push('zip, no installer')
    return parts.join(' · ')
  }
  if (current?.state === 'none') {
    return 'No build has been published yet'
  }
  return 'Windows 10/11 · zip, no installer'
})

function formatSize(bytes: number): string {
  const mb = bytes / 1048576
  return mb >= 10 ? `${Math.round(mb)} MB` : `${mb.toFixed(1)} MB`
}
</script>

<template>
  <a
    v-if="compact"
    class="btn btn-sm d-inline-flex align-items-center gap-1"
    :class="`btn-${variant}`"
    :href="href"
  >
    <AppIcon name="download" :size="16" />
    {{ label }}
  </a>
  <div v-else class="fd-download">
    <a
      class="btn btn-lg d-inline-flex align-items-center gap-2 px-4"
      :class="`btn-${variant}`"
      :href="href"
    >
      <AppIcon name="windows" :size="18" />
      {{ label }}
    </a>
    <div class="fd-download-meta small">{{ details }}</div>
  </div>
</template>
