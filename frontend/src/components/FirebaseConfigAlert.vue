<script setup lang="ts">
import { isFirebaseConfigured, missingFirebaseKeys } from '@/firebase/config'
</script>

<template>
  <div v-if="!isFirebaseConfigured" class="container mt-3">
    <div class="alert alert-warning mb-0" role="alert">
      <h6 class="alert-heading fw-bold">Firebase configuration is missing</h6>
      <p class="mb-1">
        For the app to work, create a <code>frontend/.env</code> file and enter your Firebase Web
        keys (copy the <code>frontend/.env.example</code> template).
      </p>
      <p class="mb-0 small">
        Missing keys:
        <span class="monospace">{{ missingFirebaseKeys.join(', ') }}</span>
      </p>
    </div>
  </div>
</template>
