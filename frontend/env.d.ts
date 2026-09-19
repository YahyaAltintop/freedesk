/// <reference types="vite/client" />

// SFC module declaration so TypeScript understands `*.vue` imports.
declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<Record<string, never>, Record<string, never>, unknown>
  export default component
}

// Keyboard Lock API (https://wicg.github.io/keyboard-lock/): lets a fullscreen
// page receive the shortcuts the browser normally keeps, so the session view
// can forward Ctrl+W and friends to the host. Chromium-only and not in
// TypeScript's DOM library, hence the optional member.
interface Keyboard {
  lock(keyCodes?: string[]): Promise<void>
  unlock(): void
}

interface Navigator {
  readonly keyboard?: Keyboard
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
  // Optional "owner/repo" whose GitHub Releases hold the host agent zip the
  // home page links to (defaults to the upstream repository).
  readonly VITE_GITHUB_REPO?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

// File System Access API (https://wicg.github.io/file-system-access/): lets a
// page stream a download straight to disk instead of holding it in memory.
// Chromium-only and not in TypeScript's DOM library, hence the optional member.
interface FileSystemWritableFileStream {
  write(data: ArrayBuffer | Blob | string): Promise<void>
  close(): Promise<void>
  abort(reason?: unknown): Promise<void>
}

interface FileSystemFileHandleWithWrite {
  createWritable(): Promise<FileSystemWritableFileStream>
}

interface SaveFilePickerOptions {
  suggestedName?: string
}

interface Window {
  showSaveFilePicker?(options?: SaveFilePickerOptions): Promise<FileSystemFileHandleWithWrite>
}
