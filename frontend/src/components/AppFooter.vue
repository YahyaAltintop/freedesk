<script setup lang="ts">
import { ARCHITECTURE_DOC_URL, GITHUB_URL, LICENSE_URL, RELEASES_URL } from '@/constants/links'
</script>

<template>
  <footer class="fd-footer">
    <div
      class="container d-flex flex-column flex-md-row align-items-md-center justify-content-between gap-2 pt-4 pb-2 small"
    >
      <div>
        <span class="fd-footer-brand">FreeDesk</span>
        · free, open-source remote desktop ·
        <a :href="LICENSE_URL" target="_blank" rel="noopener">MIT License</a>
      </div>
      <nav class="d-flex flex-wrap gap-3">
        <a :href="GITHUB_URL" target="_blank" rel="noopener">GitHub</a>
        <a :href="RELEASES_URL" target="_blank" rel="noopener">Releases</a>
        <a :href="ARCHITECTURE_DOC_URL" target="_blank" rel="noopener">Architecture</a>
      </nav>
    </div>
    <div class="container small pb-4">
      Only for computers you own or are allowed to control. Every connection needs a Yes on the
      host.
    </div>
  </footer>
</template>
