package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAllowed(t *testing.T) {
	for dll, want := range map[string]bool{
		"KERNEL32.dll":                      true,
		"api-ms-win-crt-runtime-l1-1-0.dll": true,
		"api-ms-win-core-synch-l1-2-0.dll":  true,
		"msvcrt.dll":                        true,
		"USER32.DLL":                        true,
		"libwinpthread-1.dll":               false,
		"libgcc_s_seh-1.dll":                false,
		"libstdc++-6.dll":                   false,
		"libvpx-9.dll":                      false,
		"ffmpeg.dll":                        false,
	} {
		if got := allowed(dll); got != want {
			t.Errorf("allowed(%q) = %v, want %v", dll, got, want)
		}
	}
}

// TestPassesThisTestBinary runs the check on a real Go executable: the test
// binary itself, whose imports are Windows' own.
func TestPassesThisTestBinary(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PE files only")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	imports, problems, err := check(exe, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(imports) == 0 {
		t.Fatal("no imports read from a Windows executable")
	}
	if len(problems) > 0 {
		t.Fatalf("the test binary was refused: %v", problems)
	}
}

// TestRefusesADynamicMinGWRuntime proves the check fires on a real PE file:
// a small C program linked against winpthreads the default, dynamic way,
// exactly the mistake the check exists for. It needs the MinGW gcc the agent
// is built with (CC or gcc on PATH) and is skipped without one.
func TestRefusesADynamicMinGWRuntime(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PE files only")
	}
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "gcc"
	}
	if _, err := exec.LookPath(cc); err != nil {
		t.Skipf("no C compiler (%s): %v", cc, err)
	}
	dir := t.TempDir()
	src, exe := filepath.Join(dir, "dyn.c"), filepath.Join(dir, "dyn.exe")
	program := "#include <pthread.h>\n" +
		"static void *work(void *p) { return p; }\n" +
		"int main(void) { pthread_t t; pthread_create(&t, 0, work, 0); return pthread_join(t, 0); }\n"
	if err := os.WriteFile(src, []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(cc, "-o", exe, src, "-lpthread").CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", cc, err, out)
	}
	_, problems, err := check(exe, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) == 0 || !strings.Contains(strings.Join(problems, "\n"), "libwinpthread") {
		t.Fatalf("a dynamically linked winpthreads was not refused: %v", problems)
	}

	lic := filepath.Join(dir, "LICENSES.txt")
	if err := os.WriteFile(lic, []byte("license texts this program does not carry\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-licenses", lic, exe}, &stdout, &stderr); code != 1 {
		t.Fatalf("run exited %d, want 1\n%s%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "statically") {
		t.Errorf("the refusal should say how to fix it:\n%s", stderr.String())
	}
}

// licenseProbe is text this test binary carries, standing in for the license
// texts a release exe has embedded.
var licenseProbe = "importcheck test: license text that is inside this test binary\r\n"

// TestLicensesCheck: the generated texts must be inside the exe as a whole; a
// text the exe does not carry is refused, and an empty file is an error, never
// a pass.
func TestLicensesCheck(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name, text string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	if problem, err := checkLicenses(exe, write("inside.txt", licenseProbe)); err != nil || problem != "" {
		t.Fatalf("texts the exe carries were refused: %q, %v", problem, err)
	}
	// Built at run time, so this exact text exists nowhere in the binary.
	outside := licenseProbe + "and a line it does not have " + strconv.FormatInt(time.Now().UnixNano(), 10)
	if problem, err := checkLicenses(exe, write("outside.txt", outside)); err != nil || problem == "" {
		t.Fatalf("texts the exe does not carry passed: %q, %v", problem, err)
	}
	if _, err := checkLicenses(exe, write("empty.txt", "")); err == nil {
		t.Fatal("an empty license file must be an error, not a pass")
	}
}

func TestUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("run() = %d, want 2 for missing arguments", code)
	}
	// The license texts are not optional: a release check without them is a
	// misconfigured release, not a pass.
	if code := run([]string{"freedesk.exe"}, &stdout, &stderr); code != 2 {
		t.Fatalf("run(exe) without -licenses = %d, want 2", code)
	}
}
