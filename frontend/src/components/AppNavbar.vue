<script setup lang="ts">
import { RouteName } from '@/constants/routes'
</script>

<template>
  <nav class="navbar navbar-dark bg-primary">
    <div class="container">
      <RouterLink class="navbar-brand fw-bold" :to="{ name: RouteName.Home }">
        🖥️ FreeDesk
      </RouterLink>
    </div>
  </nav>
</template>
