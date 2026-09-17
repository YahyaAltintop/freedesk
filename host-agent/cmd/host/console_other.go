//go:build !windows

package main

// ownsConsole is Windows-only: elsewhere the agent is started from a shell
// that keeps the output visible.
func ownsConsole() bool { return false }
