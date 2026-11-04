//go:build !windows && !darwin && !linux

package transfer

import (
	"errors"
	"syscall"
)

// freeSpace has no answer on the remaining systems, which the caller treats
// as "allow". The agent runs on Windows and macOS; this file exists so the
// package builds and its platform-independent logic stays testable elsewhere.
func freeSpace(string) (uint64, bool) { return 0, false }

// diskFull is the portable spelling of the same question.
func diskFull(err error) bool { return errors.Is(err, syscall.ENOSPC) }
