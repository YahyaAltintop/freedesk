import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// Vite build/dev configuration. The `@` alias maps to `src/` so imports stay
// short and refactor-safe across the app.
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 9205,
  },
  build: {
    // Firebase Hosting requires the public directory to live under the folder
    // that holds firebase.json (firebase/), so build straight into firebase/public.
    // `emptyOutDir` is needed because the target is outside this package root.
    outDir: fileURLToPath(new URL('../firebase/public', import.meta.url)),
    emptyOutDir: true,
  },
})
