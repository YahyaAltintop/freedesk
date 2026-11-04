package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAllowedDylib(t *testing.T) {
	for lib, want := range map[string]bool{
		"/usr/lib/libSystem.B.dylib": true,
		"/usr/lib/libresolv.9.dylib": true,
		"/System/Library/Frameworks/CoreFoundation.framework/Versions/A/CoreFoundation":     true,
		"/System/Library/Frameworks/ScreenCaptureKit.framework/Versions/A/ScreenCaptureKit": true,
		"/opt/homebrew/opt/libvpx/lib/libvpx.9.dylib":                                       false,
		"/usr/local/lib/libvpx.dylib":                                                       false,
		"@rpath/libvpx.dylib":                                                               false,
		"@executable_path/../Frameworks/Foo.framework/Foo":                                  false,
		"/System/Library/PrivateFrameworks/SkyLight.framework/Versions/A/SkyLight":          false,
	} {
		if got := allowedDylib(lib); got != want {
			t.Errorf("allowedDylib(%q) = %v, want %v", lib, got, want)
		}
	}
}

// TestPassesThisMacTestBinary runs the check on a real Go executable: the
// test binary itself, which links only macOS's own libraries.
func TestPassesThisMacTestBinary(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Mach-O files only")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	imports, problems, err := check(exe, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(imports, "/usr/lib/libSystem.B.dylib") {
		t.Fatalf("libSystem is not among the libraries the test binary links: %v", imports)
	}
	if len(problems) > 0 {
		t.Fatalf("the test binary was refused: %v", problems)
	}
}

// cTools returns the C compiler and lipo, skipping the test where there are
// none (or where it cannot make macOS binaries).
func cTools(t *testing.T) (cc, lipo string) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("Mach-O files only")
	}
	cc = os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	var err error
	if cc, err = exec.LookPath(cc); err != nil {
		t.Skipf("no C compiler: %v", err)
	}
	if lipo, err = exec.LookPath("lipo"); err != nil {
		t.Skipf("no lipo: %v", err)
	}
	return cc, lipo
}

// command runs a tool in dir, failing the test if it fails.
func command(t *testing.T, dir, tool string, args ...string) {
	t.Helper()
	cmd := exec.Command(tool, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", tool, strings.Join(args, " "), err, out)
	}
}

func writeFile(t *testing.T, dir, name, text string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeHomebrewLibvpx builds a universal dynamic library that says it lives
// where Homebrew installs libvpx, and a program for main.c linked against it
// with the extra linker arguments.
func fakeHomebrewLibvpx(t *testing.T, cc, dir string, link ...string) (exe string) {
	t.Helper()
	writeFile(t, dir, "vpx.c", "int vpx_answer(void) { return 42; }\n")
	writeFile(t, dir, "main.c", "int vpx_answer(void);\nint main(void) { return vpx_answer() == 42 ? 0 : 1; }\n")
	command(t, dir, cc, "-dynamiclib", "-arch", "arm64", "-arch", "x86_64",
		"-install_name", "/opt/homebrew/opt/libvpx/lib/libvpx.9.dylib", "-o", "libvpx.9.dylib", "vpx.c")
	args := append([]string{"-arch", "arm64", "-arch", "x86_64", "-o", "linked", "main.c"}, link...)
	command(t, dir, cc, args...)
	return filepath.Join(dir, "linked")
}

// TestRefusesALibraryOtherMacsLack proves the check fires on real macOS
// binaries: a small universal C program linked against a libvpx where
// Homebrew installs it, the macOS form of the mistake the check exists for.
// Each architecture is refused on its own.
func TestRefusesALibraryOtherMacsLack(t *testing.T) {
	cc, _ := cTools(t)
	dir := t.TempDir()
	exe := fakeHomebrewLibvpx(t, cc, dir, "libvpx.9.dylib")
	_, problems, err := check(exe, false)
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(problems, "\n")
	for _, arch := range []string{"arm64", "x86_64"} {
		if !strings.Contains(all, arch+": links /opt/homebrew/opt/libvpx/lib/libvpx.9.dylib") {
			t.Errorf("the Homebrew libvpx in the %s slice was not refused:\n%s", arch, all)
		}
	}

	lic := writeFile(t, dir, "LICENSES.txt", "license texts this program does not carry\n")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-licenses", lic, exe}, &stdout, &stderr); code != 1 {
		t.Fatalf("run exited %d, want 1\n%s%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "build-libvpx.sh") {
		t.Errorf("the refusal should say how to fix it:\n%s", stderr.String())
	}
}

// TestReadsWeakLinks: a library linked weakly is loaded wherever it exists,
// and debug/macho does not report it, so linkedLibraries must.
func TestReadsWeakLinks(t *testing.T) {
	cc, _ := cTools(t)
	exe := fakeHomebrewLibvpx(t, cc, t.TempDir(), "-Wl,-weak_library,libvpx.9.dylib")
	_, problems, err := check(exe, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(problems, "\n"), "libvpx.9.dylib") {
		t.Fatalf("a weakly linked Homebrew libvpx was not refused: %v", problems)
	}
}

// TestLicensesInEveryArchitecture: each architecture of a universal binary is
// a program of its own and must carry the texts; one slice that carries them
// does not make the binary pass.
func TestLicensesInEveryArchitecture(t *testing.T) {
	cc, lipo := cTools(t)
	dir := t.TempDir()
	text := "importcheck test: license text " + strconv.FormatInt(time.Now().UnixNano(), 10)
	writeFile(t, dir, "with.c", "__attribute__((used)) static const char text[] = \""+text+"\";\nint main(void) { return 0; }\n")
	writeFile(t, dir, "without.c", "int main(void) { return 0; }\n")
	command(t, dir, cc, "-arch", "arm64", "-o", "with-arm64", "with.c")
	command(t, dir, cc, "-arch", "x86_64", "-o", "without-x86_64", "without.c")
	command(t, dir, lipo, "-create", "with-arm64", "without-x86_64", "-output", "half")
	command(t, dir, cc, "-arch", "arm64", "-arch", "x86_64", "-o", "both", "with.c")
	lic := writeFile(t, dir, "LICENSES.txt", text)

	problem, err := checkLicenses(filepath.Join(dir, "half"), lic)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(problem, "x86_64: does not carry") {
		t.Fatalf("texts in the arm64 slice alone passed for the whole binary: %q", problem)
	}
	if problem, err := checkLicenses(filepath.Join(dir, "both"), lic); err != nil || problem != "" {
		t.Fatalf("texts in every slice were refused: %q, %v", problem, err)
	}
}
