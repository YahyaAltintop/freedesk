//go:build !darwin

package main

// volumeReadOnly: only a Mac runs the agent from a disk image.
func volumeReadOnly(string) bool { return false }
