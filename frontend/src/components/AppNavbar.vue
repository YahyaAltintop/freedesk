<script setup lang="ts">
import AppIcon from '@/components/AppIcon.vue'
import DownloadButton from '@/components/DownloadButton.vue'
import ThemeToggle from '@/components/ThemeToggle.vue'
import { GITHUB_URL } from '@/constants/links'
import { RouteName } from '@/constants/routes'
</script>

<template>
  <header class="fd-navbar">
    <nav class="container d-flex align-items-center justify-content-between py-2">
      <RouterLink class="fd-brand" :to="{ name: RouteName.Home }">
        <span class="fd-brand-mark"><AppIcon name="monitor" :size="18" /></span>
        <span>FreeDesk</span>
      </RouterLink>
      <div class="d-flex align-items-center gap-1 gap-sm-2">
        <a
          class="btn btn-sm btn-link fd-nav-link d-inline-flex align-items-center gap-1"
          :href="GITHUB_URL"
          target="_blank"
          rel="noopener"
        >
          <AppIcon name="github" :size="16" />
          <span class="d-none d-sm-inline">GitHub</span>
        </a>
        <ThemeToggle />
        <DownloadButton compact />
      </div>
    </nav>
  </header>
</template>
