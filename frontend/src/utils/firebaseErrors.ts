import { FirebaseError } from 'firebase/app'

const PERMISSION_DENIED_MESSAGE = "You don't have permission to perform this operation."

// Maps Firebase error codes to user-facing English messages.
const messages: Record<string, string> = {
  // Authentication (silent anonymous sign-in)
  'auth/admin-restricted-operation':
    'Anonymous sign-in is disabled for this Firebase project. Enable the Anonymous provider under Console → Authentication → Sign-in method.',
  'auth/operation-not-allowed':
    'Anonymous sign-in is not enabled for this Firebase project. Enable it under Console → Authentication → Sign-in method.',
  'auth/network-request-failed': 'Network error. Check your connection.',
  'auth/too-many-requests': 'Too many attempts. Please try again later.',
  // Data access
  'permission-denied': PERMISSION_DENIED_MESSAGE,
  unavailable: 'The service is currently unreachable. Check your connection.',
}

// Realtime Database SDK errors are plain `Error`s rather than `FirebaseError`s;
// a refused read or write reads e.g. "PERMISSION_DENIED: Permission denied" or
// "permission_denied at /sessions/x: Client doesn't have permission ...".
function isRtdbPermissionDenied(error: Error): boolean {
  return /^permission[_ ]denied/i.test(error.message)
}

// Converts any thrown value into a user-facing English message.
export function toFriendlyError(error: unknown): string {
  if (error instanceof FirebaseError) {
    return messages[error.code] ?? `An error occurred (${error.code}).`
  }
  if (error instanceof Error) {
    return isRtdbPermissionDenied(error) ? PERMISSION_DENIED_MESSAGE : error.message
  }
  return 'An unknown error occurred.'
}
