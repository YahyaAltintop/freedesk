<script setup lang="ts">
import { computed } from 'vue'
import { ICON_PATHS, type IconName } from '@/components/icons'

const props = withDefaults(defineProps<{ name: IconName; size?: number }>(), { size: 20 })

const markup = computed(() => ICON_PATHS[props.name])
</script>

<template>
  <svg
    xmlns="http://www.w3.org/2000/svg"
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
    class="fd-icon"
    v-html="markup"
  ></svg>
</template>
