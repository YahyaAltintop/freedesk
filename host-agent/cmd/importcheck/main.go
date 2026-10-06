// Command importcheck refuses an exe that is not fit to publish: one that would
// ask for a DLL Windows does not ship, that was built without the video
// encoder, or that does not carry the license texts generated for it.
//
// The agent links libvpx, libgcc and winpthreads into freedesk.exe statically
// (the -static in internal/vpx's cgo flags). If that ever stopped working, the
// exe would start fine on a developer's machine, where MSYS2 is on PATH, and
// fail on everyone else's with "libwinpthread-1.dll was not found". This reads
// the exe's import table and fails on any DLL outside a list of Windows' own.
// It also checks the exe was built with cgo: without it there is no encoder
// and every session would come up without a picture.
//
// The release is the exe alone, so the licenses of the code in it travel
// inside it (internal/licenses). The check is that the whole text cmd/notices
// generated for this build is in the exe, byte for byte, as go:embed stores
// it: an exe built before the text was written, or from an older one, fails.
//
//	go run ./cmd/importcheck -licenses internal/licenses/generated/LICENSES.txt path/to/freedesk.exe
package main

import (
	"bytes"
	"debug/buildinfo"
	"debug/pe"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

// systemDLLs are DLLs every supported Windows (10 and later) has in System32.
// The agent itself only imports kernel32 and the UCRT; the rest are here so
// that a later change linking one of them does not fail for no reason. A DLL
// not on this list must be a deliberate, reviewed addition.
var systemDLLs = []string{
	"advapi32.dll", "bcrypt.dll", "comctl32.dll", "comdlg32.dll", "crypt32.dll",
	"d3d11.dll", "dwmapi.dll", "dxgi.dll", "gdi32.dll", "imm32.dll",
	"iphlpapi.dll", "kernel32.dll", "msvcrt.dll", "ntdll.dll", "ole32.dll",
	"oleaut32.dll", "powrprof.dll", "secur32.dll", "setupapi.dll", "shell32.dll",
	"shlwapi.dll", "ucrtbase.dll", "user32.dll", "userenv.dll", "uxtheme.dll",
	"version.dll", "winmm.dll", "ws2_32.dll", "wtsapi32.dll",
}

// allowed reports whether an imported DLL is part of Windows. The api-ms-win-*
// names are API sets, Windows' own forwarding names (the Universal C Runtime
// is imported through api-ms-win-crt-*).
func allowed(dll string) bool {
	name := strings.ToLower(dll)
	return strings.HasPrefix(name, "api-ms-win-") || slices.Contains(systemDLLs, name)
}

// hint explains the usual causes of a DLL that should not be there.
func hint(dll string) string {
	name := strings.ToLower(dll)
	switch {
	case strings.HasPrefix(name, "libwinpthread"), strings.HasPrefix(name, "libgcc"),
		strings.HasPrefix(name, "libstdc++"), strings.HasPrefix(name, "libssp"):
		return "a MinGW runtime library: it must be linked statically (-static)"
	case strings.HasPrefix(name, "libvpx"), strings.HasPrefix(name, "vpx"):
		return "libvpx as a DLL: build it with scripts/build-libvpx.sh (--disable-shared)"
	default:
		return "not part of Windows"
	}
}

// check returns the exe's imported DLLs and what is wrong with it.
func check(path string, needCgo bool) (imports, problems []string, err error) {
	f, err := pe.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	// debug/pe's ImportedLibraries is not implemented for PE files (it always
	// returns nothing), so the DLLs come from the imported symbols, which it
	// reports as "function:DLL".
	symbols, err := f.ImportedSymbols()
	if err != nil {
		return nil, nil, fmt.Errorf("reading the import table: %w", err)
	}
	for _, sym := range symbols {
		if i := strings.LastIndexByte(sym, ':'); i >= 0 {
			imports = append(imports, sym[i+1:])
		}
	}
	slices.SortFunc(imports, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	imports = slices.CompactFunc(imports, strings.EqualFold)
	if len(imports) == 0 {
		// Every Windows executable imports at least kernel32: an empty list
		// means the table was not read, and must not pass as clean.
		return nil, nil, fmt.Errorf("no imported DLLs found; not a Windows executable?")
	}
	for _, dll := range imports {
		if !allowed(dll) {
			problems = append(problems, fmt.Sprintf("imports %s: %s", dll, hint(dll)))
		}
	}
	if needCgo {
		info, err := buildinfo.ReadFile(path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("no Go build information (%v): not the agent", err))
			return imports, problems, nil
		}
		cgo := ""
		for _, s := range info.Settings {
			if s.Key == "CGO_ENABLED" {
				cgo = s.Value
			}
		}
		if cgo != "1" {
			problems = append(problems, fmt.Sprintf("built with CGO_ENABLED=%q: this build has no video encoder", cgo))
		}
	}
	return imports, problems, nil
}

// checkLicenses reports what is wrong with the license texts the exe should
// carry: the whole file generated for this build must be inside it.
func checkLicenses(exePath, licensesPath string) (problem string, err error) {
	want, err := os.ReadFile(licensesPath)
	if err != nil {
		return "", fmt.Errorf("reading the license texts: %w", err)
	}
	if len(want) == 0 {
		return "", fmt.Errorf("the license texts in %s are empty", licensesPath)
	}
	exe, err := os.ReadFile(exePath)
	if err != nil {
		return "", err
	}
	if !bytes.Contains(exe, want) {
		return fmt.Sprintf("does not carry the license texts in %s: write them (go run ./cmd/notices -o %s) before building", licensesPath, licensesPath), nil
	}
	return "", nil
}

func run(args []string, stdout, stderr io.Writer) int {
	const usage = "usage: importcheck -licenses path/to/LICENSES.txt path/to/freedesk.exe"
	fs := flag.NewFlagSet("importcheck", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	licensesPath := fs.String("licenses", "", "the license texts generated for this build")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 || *licensesPath == "" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	exe := fs.Arg(0)
	imports, problems, err := check(exe, true)
	if err != nil {
		fmt.Fprintf(stderr, "importcheck: %s: %v\n", exe, err)
		return 1
	}
	problem, err := checkLicenses(exe, *licensesPath)
	if err != nil {
		fmt.Fprintf(stderr, "importcheck: %s: %v\n", exe, err)
		return 1
	}
	if problem != "" {
		problems = append(problems, problem)
	}
	fmt.Fprintf(stdout, "%s imports %d DLLs:\n", exe, len(imports))
	for _, dll := range imports {
		fmt.Fprintf(stdout, "  %s\n", dll)
	}
	if len(problems) > 0 {
		fmt.Fprintf(stderr, "importcheck: %s is not fit to publish:\n", exe)
		for _, p := range problems {
			fmt.Fprintf(stderr, "  %s\n", p)
		}
		return 1
	}
	fmt.Fprintln(stdout, "importcheck: only Windows' own DLLs, built with cgo, carries its licenses: ok")
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
