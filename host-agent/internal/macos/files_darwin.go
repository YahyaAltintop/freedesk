//go:build darwin && cgo

package macos

/*
#cgo LDFLAGS: -framework CoreServices
#include <stdlib.h>
#include "files_darwin.h"
*/
import "C"

import (
	"errors"
	"unsafe"
)

// Quarantine marks the file at path as come from another computer, the way a
// browser marks a download. Opening it is then up to Gatekeeper, as for any
// download: an app or an installer is checked, and the operator asked,
// before anything of it runs.
func Quarantine(path string) error {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var errbuf [256]C.char
	if C.fd_quarantine(cpath, &errbuf[0], C.int(len(errbuf))) == 0 {
		return errors.New(C.GoString(&errbuf[0]))
	}
	return nil
}

// VolumeCapacity reports how many bytes a file the operator asked for may
// still take on the volume path is on, counting the room macOS frees on
// demand, as the Finder's "available" does. path must exist.
func VolumeCapacity(path string) (uint64, bool) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var bytes C.uint64_t
	if C.fd_volume_capacity(cpath, &bytes) == 0 {
		return 0, false
	}
	return uint64(bytes), true
}

// quarantineOf reads the mark Quarantine puts on a file: what kind of
// download it says the file is, and which program saved it. For tests.
func quarantineOf(path string) (kind, agent string, ok bool) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var ckind, cagent [128]C.char
	if C.fd_quarantine_read(cpath, &ckind[0], C.int(len(ckind)), &cagent[0], C.int(len(cagent))) == 0 {
		return "", "", false
	}
	return C.GoString(&ckind[0]), C.GoString(&cagent[0]), true
}
