//go:build darwin && cgo

package macos

// #include <stdlib.h>
// #include "pasteboard_darwin.h"
import "C"

import "unsafe"

// PasteAccess says whether the agent may read a pasteboard's contents
// without macOS asking the operator first. From macOS 15.4 a program reading
// the general pasteboard, the clipboard, is asked about, unless the operator
// has chosen otherwise for it in System Settings > Privacy & Security > Paste
// from Other Apps.
type PasteAccess int

const (
	// PasteAllowed: reads go ahead silently. Every pasteboard before macOS
	// 15.4, any but the general one, or the operator chose Always Allow.
	PasteAllowed PasteAccess = C.FD_PB_ALLOWED
	// PasteNeverAsked: the first read makes macOS ask, after which the agent
	// is listed in System Settings, where Always Allow can be chosen.
	PasteNeverAsked PasteAccess = C.FD_PB_NEVER_ASKED
	// PasteAsk: macOS asks at every read.
	PasteAsk PasteAccess = C.FD_PB_ASK
	// PasteDenied: macOS refuses every read.
	PasteDenied PasteAccess = C.FD_PB_DENIED
)

// typeFileURL is the type a file on the pasteboard comes as
// (NSPasteboardTypeFileURL).
const typeFileURL = "public.file-url"

// Pasteboard is one macOS pasteboard. Its methods must not be called
// concurrently.
type Pasteboard struct {
	pb C.fd_pb
}

// OpenPasteboard opens the pasteboard of that name, or for "" the general
// one: the clipboard every application copies to and pastes from. A named
// pasteboard is the agent's own, which macOS never asks about.
func OpenPasteboard(name string) *Pasteboard {
	var cname *C.char
	if name != "" {
		cname = C.CString(name)
		defer C.free(unsafe.Pointer(cname))
	}
	return &Pasteboard{pb: C.fd_pb_open(cname)}
}

// ReleasePasteboard deletes a named pasteboard from the system.
func ReleasePasteboard(name string) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	C.fd_pb_release_globally(cname)
}

// Close lets go of this pasteboard; the pasteboard itself stays.
func (p *Pasteboard) Close() {
	C.fd_pb_close(p.pb)
	p.pb = nil
}

// ChangeCount changes whenever anything puts something on the pasteboard.
// It reads none of the contents, so macOS never asks about it.
func (p *Pasteboard) ChangeCount() int64 { return int64(C.fd_pb_change_count(p.pb)) }

// HasType reports whether the contents come in type (a UTI). It reads none
// of them.
func (p *Pasteboard) HasType(uti string) bool {
	ctype := C.CString(uti)
	defer C.free(unsafe.Pointer(ctype))
	return C.fd_pb_has_type(p.pb, ctype) != 0
}

// HasFiles reports whether the pasteboard holds files, as a copy in the
// Finder leaves it. It reads none of the contents.
func (p *Pasteboard) HasFiles() bool { return p.HasType(typeFileURL) }

// Access says whether reading the contents would make macOS ask first.
func (p *Pasteboard) Access() PasteAccess { return PasteAccess(C.fd_pb_access(p.pb)) }

// ReadText returns the plain text on the pasteboard, and false if it holds
// none.
func (p *Pasteboard) ReadText() (string, bool) {
	var n C.size_t
	s := C.fd_pb_read_text(p.pb, &n)
	if s == nil {
		return "", false
	}
	defer C.free(unsafe.Pointer(s))
	return C.GoStringN(s, C.int(n)), true
}

// WriteText replaces the contents with plain text.
func (p *Pasteboard) WriteText(text string) bool {
	return C.fd_pb_write_text(p.pb, cBytes(text), C.size_t(len(text))) != 0
}

// WriteStrings replaces the contents with one item holding each type's
// string: what another application's copy looks like, a password manager's
// with its markers, say.
func (p *Pasteboard) WriteStrings(values map[string]string) bool {
	types, strs := make([]string, 0, len(values)), make([]string, 0, len(values))
	for t, v := range values {
		types, strs = append(types, t), append(strs, v)
	}
	ctypes, cvalues := cStrings(types), cStrings(strs)
	defer freeCStrings(ctypes)
	defer freeCStrings(cvalues)
	return C.fd_pb_write_strings(p.pb, cArray(ctypes), cArray(cvalues), C.int(len(values))) != 0
}

// ReadFiles returns the paths of the files on the pasteboard.
func (p *Pasteboard) ReadFiles() []string {
	var n C.int
	v := C.fd_pb_read_files(p.pb, &n)
	if v == nil {
		return nil
	}
	defer C.fd_pb_free_strings(v, n)
	paths := make([]string, 0, int(n))
	for _, s := range unsafe.Slice(v, int(n)) {
		paths = append(paths, C.GoString(s))
	}
	return paths
}

// WriteFiles replaces the contents with files, for pasting in the Finder.
// The files must keep existing: their bytes are read when they are pasted.
func (p *Pasteboard) WriteFiles(paths []string) bool {
	cpaths := cStrings(paths)
	defer freeCStrings(cpaths)
	return C.fd_pb_write_files(p.pb, cArray(cpaths), C.int(len(paths))) != 0
}

// cBytes points C at a Go string's bytes for the length of one call.
func cBytes(s string) *C.char {
	if s == "" {
		return nil
	}
	return (*C.char)(unsafe.Pointer(unsafe.StringData(s)))
}

// cStrings copies strings into C memory, for freeCStrings to release.
func cStrings(ss []string) []*C.char {
	out := make([]*C.char, len(ss))
	for i, s := range ss {
		out[i] = C.CString(s)
	}
	return out
}

func freeCStrings(cs []*C.char) {
	for _, c := range cs {
		C.free(unsafe.Pointer(c))
	}
}

// cArray is the array C takes; nil for none. Its elements point at C memory,
// so Go may hand it over.
func cArray(cs []*C.char) **C.char {
	if len(cs) == 0 {
		return nil
	}
	return &cs[0]
}
