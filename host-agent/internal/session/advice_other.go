//go:build !darwin

package session

// localNetworkAdvice: only macOS asks before a program may reach the local
// network.
const localNetworkAdvice = ""
