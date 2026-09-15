/// <reference types="vite/client" />

// SFC module declaration so TypeScript understands `*.vue` imports.
declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<Record<string, never>, Record<string, never>, unknown>
  export default component
}

// Strongly-typed Firebase Web configuration coming from Vite env files.
// All values are public Firebase Web keys (safe to ship to the browser);
// access is still gated by Firebase Security Rules.
interface ImportMetaEnv {
  readonly VITE_FIREBASE_API_KEY: string
  readonly VITE_FIREBASE_AUTH_DOMAIN: string
  readonly VITE_FIREBASE_PROJECT_ID: string
  readonly VITE_FIREBASE_STORAGE_BUCKET: string
  readonly VITE_FIREBASE_MESSAGING_SENDER_ID: string
  readonly VITE_FIREBASE_APP_ID: string
  readonly VITE_FIREBASE_DATABASE_URL: string
  // Optional dev switch: '1' routes Auth + Realtime Database to the local emulators.
  readonly VITE_USE_EMULATORS?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
