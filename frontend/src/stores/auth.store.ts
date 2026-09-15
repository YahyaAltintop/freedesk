import { defineStore } from 'pinia'
import { observeAuthState, signInAnonymouslyOnce } from '@/services/auth.service'
import { toFriendlyError } from '@/utils/firebaseErrors'

interface AuthState {
  uid: string | null
  initialized: boolean
  error: string | null
}

// Module-level promise guarantees the auth listener is wired up exactly once,
// no matter how many times init() is called (router guard + bootstrap).
let readyPromise: Promise<void> | null = null

// Silent anonymous identity: init() resolves once the browser either has a
// signed-in user (existing or freshly created) or anonymous sign-in failed
// (e.g. the provider is disabled in the Firebase console).
export const useAuthStore = defineStore('auth', {
  state: (): AuthState => ({
    uid: null,
    initialized: false,
    error: null,
  }),

  actions: {
    init(): Promise<void> {
      if (readyPromise) {
        return readyPromise
      }
      readyPromise = new Promise<void>((resolve) => {
        const finish = (): void => {
          if (!this.initialized) {
            this.initialized = true
            resolve()
          }
        }
        observeAuthState((user) => {
          if (user) {
            this.uid = user.uid
            this.error = null
            finish()
          } else {
            this.uid = null
            void signInAnonymouslyOnce().catch((error) => {
              this.error = toFriendlyError(error)
              finish()
            })
          }
        })
      })
      return readyPromise
    },
  },
})
