// Builds the Firebase Web configuration from Vite environment variables and
// reports any missing required keys, so the UI can guide first-time setup
// instead of crashing on an unconfigured project.

export interface FirebaseWebConfig {
  apiKey: string
  authDomain: string
  projectId: string
  storageBucket: string
  messagingSenderId: string
  appId: string
  databaseURL: string
}

export const firebaseConfig: FirebaseWebConfig = {
  apiKey: import.meta.env.VITE_FIREBASE_API_KEY,
  authDomain: import.meta.env.VITE_FIREBASE_AUTH_DOMAIN,
  projectId: import.meta.env.VITE_FIREBASE_PROJECT_ID,
  storageBucket: import.meta.env.VITE_FIREBASE_STORAGE_BUCKET,
  messagingSenderId: import.meta.env.VITE_FIREBASE_MESSAGING_SENDER_ID,
  appId: import.meta.env.VITE_FIREBASE_APP_ID,
  databaseURL: import.meta.env.VITE_FIREBASE_DATABASE_URL,
}

// Keys required for the app to work at all: Authentication plus the Realtime
// Database, which is the only data store (host records, inbox, signaling).
const requiredKeys: Array<keyof FirebaseWebConfig> = [
  'apiKey',
  'authDomain',
  'projectId',
  'appId',
  'databaseURL',
]

export const missingFirebaseKeys: string[] = requiredKeys.filter((key) => !firebaseConfig[key])

export const isFirebaseConfigured: boolean = missingFirebaseKeys.length === 0
