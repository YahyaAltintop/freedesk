//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetConsoleProcessList = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList")

// ownsConsole reports whether this process is the only one attached to its
// console: true when the exe was started by double-click (the window vanishes
// the moment we exit), false when started from a terminal or without a console.
func ownsConsole() bool {
	var pids [2]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return n == 1
}
