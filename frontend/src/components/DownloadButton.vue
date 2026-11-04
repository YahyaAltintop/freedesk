<script setup lang="ts">
import { computed } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import { RELEASES_URL } from '@/constants/links'
import { downloadFor, useLatestRelease, type HostSystem } from '@/services/releases'
import { localPlatform } from '@/utils/platform'

// Links to the host program for this computer — freedesk.exe on Windows,
// FreeDesk.dmg on a Mac — once the GitHub release lookup has confirmed
// one (with version and size). Anywhere else, where neither runs, it offers
// the Windows one. Before the lookup, when it fails or when no release exists,
// it links to the Releases page, which never 404s. The full button names the
// other system's program too, for whoever downloads it for another computer.
const props = withDefaults(defineProps<{ compact?: boolean; variant?: 'primary' | 'ghost' }>(), {
  compact: false,
  variant: 'primary',
})

const lookup = useLatestRelease()

const system: HostSystem = localPlatform() === 'mac' ? 'mac' : 'windows'
const other: HostSystem = system === 'mac' ? 'windows' : 'mac'

const SYSTEMS = {
  windows: { name: 'Windows', icon: 'windows', needs: 'windows 10/11 · one file', note: 'one file' },
  mac: { name: 'Mac', icon: 'command', needs: 'macOS 13+ · apple silicon & intel', note: 'macOS 13+' },
} as const

function hrefFor(target: HostSystem): string {
  const current = lookup.value
  return current?.state === 'found' ? downloadFor(current.release, target).url : RELEASES_URL
}

const href = computed(() => hrefFor(system))
const otherHref = computed(() => hrefFor(other))

const label = computed(() => {
  if (lookup.value?.state === 'none') {
    return 'See releases'
  }
  return props.compact ? 'Download' : `Download for ${SYSTEMS[system].name}`
})

const details = computed(() => {
  const current = lookup.value
  if (current?.state === 'found') {
    const parts = [`v${current.release.version}`]
    const size = downloadFor(current.release, system).sizeBytes
    if (size) {
      parts.push(formatSize(size))
    }
    parts.push(SYSTEMS[system].note, 'no installer')
    return parts.join(' · ')
  }
  if (current?.state === 'none') {
    return 'no build published yet'
  }
  return `${SYSTEMS[system].needs} · no installer`
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
      <AppIcon :name="SYSTEMS[system].icon" :size="18" />
      {{ label }}
    </a>
    <div class="fd-download-meta">
      {{ details }}
      <template v-if="lookup?.state !== 'none'">
        · <a :href="otherHref">also for {{ SYSTEMS[other].name }}</a>
      </template>
    </div>
  </div>
</template>
