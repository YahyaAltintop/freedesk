//go:build windows

package consent

import (
	"context"
	"log"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	ofnReadOnly         = 0x00000001
	ofnHideReadOnly     = 0x00000004
	ofnNoChangeDir      = 0x00000008
	ofnAllowMultiSelect = 0x00000200
	ofnPathMustExist    = 0x00000800
	ofnFileMustExist    = 0x00001000
	ofnExplorer         = 0x00080000

	// pathBufferChars holds the result. With multi-select the dialog writes the
	// directory, then each file name, each NUL-terminated and the list closed by
	// a second NUL — so a long selection needs room.
	pathBufferChars = 32 * 1024
)

var (
	comdlg32           = windows.NewLazySystemDLL("comdlg32.dll")
	procGetOpenFileNam = comdlg32.NewProc("GetOpenFileNameW")
	procCommDlgError   = comdlg32.NewProc("CommDlgExtendedError")
)

// openFileNameW mirrors OPENFILENAMEW. Field order and types match the C
// struct exactly; Go's natural alignment produces the same padding the compiler
// does on x64, so this must not be reordered.
type openFileNameW struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	flagsEx           uint32
}

// nativePicker shows the Windows "Open" dialog.
type nativePicker struct {
	mu       sync.Mutex
	lastShow time.Time
}

func newPlatformPicker() FilePicker { return &nativePicker{} }

func (p *nativePicker) Available() bool { return true }

// Pick shows the dialog and returns what the operator chose.
func (p *nativePicker) Pick(ctx context.Context) ([]string, bool) {
	if !p.claim() {
		return nil, false
	}
	// Serialised against every other prompt: the operator must never be shown
	// a picker stacked on top of a question they have not answered.
	var (
		paths []string
		ok    bool
	)
	exclusive(func() {
		if ctx.Err() != nil {
			return
		}
		paths, ok = p.show(ctx)
	})
	return paths, ok
}

// claim enforces one picker at a time plus a cooldown between them.
func (p *nativePicker) claim() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Since(p.lastShow) < pickerCooldown {
		log.Printf("[consent] ignoring a file picker request: too soon after the last one")
		return false
	}
	p.lastShow = time.Now()
	return true
}

func (p *nativePicker) show(ctx context.Context) ([]string, bool) {
	buf := make([]uint16, pathBufferChars)
	title := windows.StringToUTF16Ptr("FreeDesk - choose files to send")
	// Two NULs close the filter list.
	filter := &utf16Filter("All files\x00*.*\x00")[0]

	ofn := openFileNameW{
		lStructSize: uint32(unsafe.Sizeof(openFileNameW{})),
		lpstrFilter: filter,
		lpstrFile:   &buf[0],
		nMaxFile:    uint32(len(buf)),
		lpstrTitle:  title,
		flags: ofnExplorer | ofnAllowMultiSelect | ofnFileMustExist |
			ofnPathMustExist | ofnNoChangeDir | ofnHideReadOnly | ofnReadOnly,
	}

	log.Printf("[consent] the viewer asked for files — choose them in the dialog window")

	result := make(chan bool, 1)
	go func() {
		// Common dialogs are thread-affine, exactly like the message box.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		ret, _, _ := procGetOpenFileNam.Call(uintptr(unsafe.Pointer(&ofn)))
		if ret == 0 {
			// 0 with no extended error is the operator pressing Cancel, which
			// is a decision, not a fault.
			if code, _, _ := procCommDlgError.Call(); code != 0 {
				log.Printf("[consent] the file dialog could not be shown (error %d)", code)
			}
		}
		result <- ret != 0
	}()

	select {
	case ok := <-result:
		if !ok {
			return nil, false
		}
		paths := parseSelection(buf)
		log.Printf("[consent] the operator chose %d file(s) to send", len(paths))
		return paths, len(paths) > 0
	case <-ctx.Done():
		// The session ended while the dialog was open. There is no supported
		// way to close someone else's common dialog, so the operator's own
		// choice ends it; the result is dropped because nothing is listening.
		log.Printf("[consent] the session ended while the file dialog was open — close it to continue")
		go func() { <-result }()
		return nil, false
	}
}

// parseSelection reads the dialog's result buffer.
//
// One file gives a single full path. Several give the directory first, then
// each file name, every entry NUL-terminated and the list closed by an empty
// entry.
func parseSelection(buf []uint16) []string {
	var parts []string
	start := 0
	for i := 0; i < len(buf); i++ {
		if buf[i] != 0 {
			continue
		}
		if i == start {
			break // the empty entry that closes the list
		}
		parts = append(parts, windows.UTF16ToString(buf[start:i]))
		start = i + 1
	}
	switch len(parts) {
	case 0:
		return nil
	case 1:
		return parts
	default:
		dir := parts[0]
		files := make([]string, 0, len(parts)-1)
		for _, name := range parts[1:] {
			files = append(files, filepath.Join(dir, name))
		}
		return files
	}
}

// utf16Filter converts a filter string whose sections are separated by NULs,
// which StringToUTF16 refuses to do because it treats a NUL as the end.
func utf16Filter(s string) []uint16 {
	out := make([]uint16, 0, len(s)+1)
	for _, r := range s {
		out = append(out, uint16(r))
	}
	return append(out, 0)
}
