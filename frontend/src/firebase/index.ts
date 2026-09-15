import { initializeApp, type FirebaseApp } from 'firebase/app'
import { connectAuthEmulator, getAuth, type Auth } from 'firebase/auth'
import { connectDatabaseEmulator, getDatabase, type Database } from 'firebase/database'
import { firebaseConfig } from './config'

// Central Firebase initialization. The viewer uses exactly two Firebase
// products: Authentication (the invisible anonymous identity) and the Realtime
// Database, which holds the host records, the per-owner session inbox and the
// WebRTC signaling data. There is no Firestore.
const app: FirebaseApp = initializeApp(firebaseConfig)

export const auth: Auth = getAuth(app)
export const db: Database = getDatabase(app)

// Local development against the Firebase Emulator Suite is opt-in
// (VITE_USE_EMULATORS=1) so a production build can never hit the emulators by
// accident. Both connections must be redirected BEFORE anything else touches
// them, which is why this lives here at init and nowhere else.
if (import.meta.env.VITE_USE_EMULATORS === '1') {
  connectAuthEmulator(auth, 'http://127.0.0.1:9099', { disableWarnings: true })
  connectDatabaseEmulator(db, '127.0.0.1', 9000)
}

export { app }
