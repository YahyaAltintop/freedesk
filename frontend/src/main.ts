import { createApp } from 'vue'
import { createPinia } from 'pinia'

import 'bootstrap/dist/css/bootstrap.min.css'
// Self-hosted variable fonts (no third-party request): Inter for text,
// JetBrains Mono for headings, codes and labels.
import '@fontsource-variable/inter'
import '@fontsource-variable/jetbrains-mono'
import '@/assets/main.css'

import App from '@/App.vue'
import router from '@/router'
import { useAuthStore } from '@/stores/auth.store'
import { startServerClock } from '@/services/serverTime'

// Bootstraps the application. The anonymous identity is resolved in the
// background: the home page renders without it, and the one action that needs
// a uid — connecting — waits for it itself, as does the router before the
// session page. Awaiting it here held the first paint behind a sign-in round
// trip, over a second on a cold visit, for a page that only shows text.
async function bootstrap(): Promise<void> {
  const app = createApp(App)

  const pinia = createPinia()
  app.use(pinia)

  void useAuthStore(pinia).init()

  // Host presence compares server timestamps against server time; start the
  // clock now so the offset is (almost always) known by the first lookup.
  startServerClock()

  app.use(router)
  await router.isReady()

  app.mount('#app')
}

void bootstrap()
