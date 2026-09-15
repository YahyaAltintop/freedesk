import {
  onAuthStateChanged,
  signInAnonymously,
  type User,
  type Unsubscribe,
} from 'firebase/auth'
import { auth } from '@/firebase'

// Thin wrapper around Firebase Authentication. There is no login UI in this
// app: every browser gets an invisible ANONYMOUS identity, which satisfies the
// `request.auth != null` requirement of the security rules. The uid persists
// per browser (IndexedDB), so the same machine keeps the same viewer identity.

export async function signInAnonymouslyOnce(): Promise<User> {
  const credential = await signInAnonymously(auth)
  return credential.user
}

// Subscribes to Firebase auth-state changes. Returns the unsubscribe function.
export function observeAuthState(callback: (user: User | null) => void): Unsubscribe {
  return onAuthStateChanged(auth, callback)
}
