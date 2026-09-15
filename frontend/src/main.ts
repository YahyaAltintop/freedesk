import { createApp } from 'vue'
import { createPinia } from 'pinia'

import 'bootstrap/dist/css/bootstrap.min.css'
import '@/assets/main.css'

import App from '@/App.vue'
import router from '@/router'
import { useAuthStore } from '@/stores/auth.store'
import { startServerClock } from '@/services/serverTime'

// Bootstraps the application. Auth state is resolved BEFORE the first
// navigation so route guards always see a known authentication status.
async function bootstrap(): Promise<void> {
  const app = createApp(App)

  const pinia = createPinia()
  app.use(pinia)

  await useAuthStore(pinia).init()

  // Host presence compares server timestamps against server time; start the
  // clock now so the offset is (almost always) known by the first lookup.
  startServerClock()

  app.use(router)
  await router.isReady()

  app.mount('#app')
}

void bootstrap()
