// The pairing code is the host's address: six digits, shown as "123 - 456".
// The host agent generates it (host-agent/cmd/host/main.go) and the database
// rules check it (firebase/database.rules.json); all three agree on the length.
export const PAIRING_CODE_LENGTH = 6

/** Keeps only digits, and at most a whole code. */
export function normalizePairingCode(input: string): string {
  return input.replace(/\D/g, '').slice(0, PAIRING_CODE_LENGTH)
}

/** "123456" -> "123 - 456"; a partial code is grouped as far as it goes. */
export function formatPairingCode(digits: string): string {
  if (digits.length <= 3) {
    return digits
  }
  return `${digits.slice(0, 3)} - ${digits.slice(3)}`
}
