<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AppNavbar from '@/components/AppNavbar.vue'
import FirebaseConfigAlert from '@/components/FirebaseConfigAlert.vue'
import { RouteName } from '@/constants/routes'

const route = useRoute()

// The full-screen connection view renders without the app chrome.
const showChrome = computed(() => route.name !== RouteName.Connect)
</script>

<template>
  <div class="app-shell d-flex flex-column min-vh-100">
    <template v-if="showChrome">
      <AppNavbar />
      <FirebaseConfigAlert />
    </template>
    <main class="flex-grow-1">
      <RouterView />
    </main>
  </div>
</template>
