//go:build !darwin

package webrtc

// AskLocalNetwork: only macOS asks before a program may reach the local
// network.
func AskLocalNetwork() {}
