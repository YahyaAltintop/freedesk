// Command importcheck refuses an exe that is not fit to publish: one that would
// ask for a DLL Windows does not ship, that was built without the video
// encoder, or that does not carry the license texts generated for it. It
// checks the macOS agent the same way: a library macOS does not ship, no
// encoder, or no license texts, in every architecture of a universal binary.
//
// The agent links libvpx, libgcc and winpthreads into freedesk.exe statically
// (the -static in internal/vpx's cgo flags). If that ever stopped working, the
// exe would start fine on a developer's machine, where MSYS2 is on PATH, and
// fail on everyone else's with "libwinpthread-1.dll was not found". This reads
// the exe's import table and fails on any DLL outside a list of Windows' own.
// On macOS the same mistake is a libvpx from Homebrew, linked dynamically:
// fine where Homebrew is, missing everywhere else. There it reads the
// libraries the binary loads and fails on any outside macOS's own.
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
	"debug/macho"
	"debug/pe"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
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

// allowedDylib reports whether a library a macOS binary loads is part of
// macOS: the system's own libraries in /usr/lib and its public frameworks,
// both of which only Apple installs.
func allowedDylib(lib string) bool {
	return strings.HasPrefix(lib, "/usr/lib/") || strings.HasPrefix(lib, "/System/Library/Frameworks/")
}

// dylibHint explains the usual causes of a library that should not be there.
func dylibHint(lib string) string {
	switch {
	case strings.HasPrefix(strings.ToLower(path.Base(lib)), "libvpx"):
		return "libvpx as a dynamic library: build it with scripts/build-libvpx.sh (--disable-shared)"
	case strings.HasPrefix(lib, "@"):
		return "a library expected next to the program: the release is the program alone"
	case strings.HasPrefix(lib, "/opt/homebrew/"), strings.HasPrefix(lib, "/usr/local/"):
		return "a Homebrew or locally installed library: other Macs do not have it"
	case strings.HasPrefix(lib, "/System/Library/PrivateFrameworks/"):
		return "a private framework: not an interface macOS keeps from one release to the next"
	default:
		return "not part of macOS"
	}
}

// format is the kind of executable a file is.
type format int

const (
	unknownFormat format = iota
	peFormat             // a Windows exe
	machoFormat          // a macOS binary, of one architecture or several
)

// formatOf tells a Windows exe from a macOS binary by its first bytes.
func formatOf(file string) (format, error) {
	f, err := os.Open(file)
	if err != nil {
		return unknownFormat, err
	}
	defer f.Close()
	var head [4]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return unknownFormat, fmt.Errorf("reading the header: %w", err)
	}
	le, be := binary.LittleEndian.Uint32(head[:]), binary.BigEndian.Uint32(head[:])
	switch {
	case head[0] == 'M' && head[1] == 'Z':
		return peFormat, nil
	case le == macho.Magic64, le == macho.Magic32, be == macho.MagicFat:
		return machoFormat, nil
	}
	return unknownFormat, nil
}

// check returns the libraries the executable loads (the DLLs a Windows exe
// imports, the libraries every architecture of a macOS binary links) and what
// is wrong with it.
func check(file string, needCgo bool) (imports, problems []string, err error) {
	f, err := formatOf(file)
	if err != nil {
		return nil, nil, err
	}
	switch f {
	case peFormat:
		return checkPE(file, needCgo)
	case machoFormat:
		return checkMachO(file, needCgo)
	}
	return nil, nil, errors.New("neither a Windows exe nor a macOS binary")
}

// checkPE returns the exe's imported DLLs and what is wrong with it.
func checkPE(file string, needCgo bool) (imports, problems []string, err error) {
	f, err := pe.Open(file)
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
		if p := cgoProblem(buildinfo.ReadFile(file)); p != "" {
			problems = append(problems, p)
		}
	}
	return imports, problems, nil
}

// checkMachO returns the libraries a macOS binary links and what is wrong
// with it, architecture by architecture: a universal binary is two programs in
// one file, and each must pass on its own.
func checkMachO(file string, needCgo bool) (imports, problems []string, err error) {
	archs, done, err := openMachO(file)
	if err != nil {
		return nil, nil, err
	}
	defer done()
	for _, a := range archs {
		if a.file.Type != macho.TypeExec {
			return nil, nil, fmt.Errorf("%s: not an executable (Mach-O file type %d)", a.name, a.file.Type)
		}
		libs := linkedLibraries(a.file)
		if len(libs) == 0 {
			// Every macOS program links at least libSystem: an empty list
			// means the load commands were not read, and must not pass as
			// clean.
			return nil, nil, fmt.Errorf("%s: no linked libraries found; not a macOS executable?", a.name)
		}
		for _, lib := range libs {
			imports = append(imports, lib)
			if !allowedDylib(lib) {
				problems = append(problems, fmt.Sprintf("%s: links %s: %s", a.name, lib, dylibHint(lib)))
			}
		}
		if needCgo {
			if p := cgoProblem(buildinfo.Read(a.data)); p != "" {
				problems = append(problems, a.name+": "+p)
			}
		}
	}
	slices.Sort(imports)
	return slices.Compact(imports), problems, nil
}

// The load commands, besides LC_LOAD_DYLIB, that name a library for the
// loader to load: weakly, re-exported, lazily or upward linked.
const (
	lcLoadWeakDylib   = 0x80000018
	lcReexportDylib   = 0x8000001f
	lcLazyLoadDylib   = 0x20
	lcLoadUpwardDylib = 0x80000023
)

// linkedLibraries returns every library a macOS binary asks the loader for.
// debug/macho reads the plain LC_LOAD_DYLIB commands only (its
// ImportedLibraries), so the other kinds are read here from their raw bytes:
// a library linked weakly is loaded all the same wherever it exists.
func linkedLibraries(f *macho.File) []string {
	var libs []string
	for _, l := range f.Loads {
		if d, ok := l.(*macho.Dylib); ok {
			libs = append(libs, d.Name)
			continue
		}
		raw := l.Raw()
		if len(raw) < 12 {
			continue
		}
		switch f.ByteOrder.Uint32(raw[0:4]) {
		case lcLoadWeakDylib, lcReexportDylib, lcLazyLoadDylib, lcLoadUpwardDylib:
			// struct dylib_command: cmd, cmdsize, then the offset of the
			// library's name, a NUL-terminated string within the command.
			if off := f.ByteOrder.Uint32(raw[8:12]); int(off) < len(raw) {
				name := raw[off:]
				if i := bytes.IndexByte(name, 0); i >= 0 {
					name = name[:i]
				}
				libs = append(libs, string(name))
			}
		}
	}
	return libs
}

// machoArch is one architecture of a macOS binary: the whole file, or one
// slice of a universal binary.
type machoArch struct {
	name string
	file *macho.File
	data *io.SectionReader // its bytes
}

// openMachO opens every architecture of a macOS binary; done closes them.
func openMachO(file string) (archs []machoArch, done func(), err error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, nil, err
	}
	fat, err := macho.NewFatFile(f)
	if err == nil {
		for _, a := range fat.Arches {
			archs = append(archs, machoArch{archName(a.Cpu), a.File, io.NewSectionReader(f, int64(a.Offset), int64(a.Size))})
		}
		return archs, func() { _ = fat.Close(); _ = f.Close() }, nil
	}
	if !errors.Is(err, macho.ErrNotFat) {
		_ = f.Close()
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	thin, err := macho.NewFile(f)
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	archs = []machoArch{{archName(thin.Cpu), thin, io.NewSectionReader(f, 0, info.Size())}}
	return archs, func() { _ = thin.Close(); _ = f.Close() }, nil
}

// archName is the name Apple's own tools give an architecture.
func archName(cpu macho.Cpu) string {
	switch cpu {
	case macho.CpuArm64:
		return "arm64"
	case macho.CpuAmd64:
		return "x86_64"
	default:
		return cpu.String()
	}
}

// cgoProblem reports a build without cgo, which has no video encoder, from
// the build information read out of the binary.
func cgoProblem(info *buildinfo.BuildInfo, err error) string {
	if err != nil {
		return fmt.Sprintf("no Go build information (%v): not the agent", err)
	}
	cgo := ""
	for _, s := range info.Settings {
		if s.Key == "CGO_ENABLED" {
			cgo = s.Value
		}
	}
	if cgo != "1" {
		return fmt.Sprintf("built with CGO_ENABLED=%q: this build has no video encoder", cgo)
	}
	return ""
}

// checkLicenses reports what is wrong with the license texts the exe should
// carry: the whole file generated for this build must be inside it, inside
// every architecture of a universal macOS binary.
func checkLicenses(exePath, licensesPath string) (problem string, err error) {
	want, err := os.ReadFile(licensesPath)
	if err != nil {
		return "", fmt.Errorf("reading the license texts: %w", err)
	}
	if len(want) == 0 {
		return "", fmt.Errorf("the license texts in %s are empty", licensesPath)
	}
	missing := fmt.Sprintf("does not carry the license texts in %s: write them (go run ./cmd/notices -o %s) before building", licensesPath, licensesPath)
	if f, err := formatOf(exePath); err == nil && f == machoFormat {
		archs, done, err := openMachO(exePath)
		if err != nil {
			return "", err
		}
		defer done()
		for _, a := range archs {
			data, err := io.ReadAll(a.data)
			if err != nil {
				return "", fmt.Errorf("%s: %w", a.name, err)
			}
			if !bytes.Contains(data, want) {
				return a.name + ": " + missing, nil
			}
		}
		return "", nil
	}
	exe, err := os.ReadFile(exePath)
	if err != nil {
		return "", err
	}
	if !bytes.Contains(exe, want) {
		return missing, nil
	}
	return "", nil
}

func run(args []string, stdout, stderr io.Writer) int {
	const usage = "usage: importcheck -licenses path/to/LICENSES.txt path/to/freedesk.exe (or the agent inside FreeDesk.app)"
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
	verb, noun, ok := "imports", "DLLs", "only Windows' own DLLs, built with cgo, carries its licenses: ok"
	if f, _ := formatOf(exe); f == machoFormat {
		verb, noun, ok = "links", "libraries", "only macOS's own libraries, built with cgo, carries its licenses: ok"
	}
	fmt.Fprintf(stdout, "%s %s %d %s:\n", exe, verb, len(imports), noun)
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
	fmt.Fprintln(stdout, "importcheck: "+ok)
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
