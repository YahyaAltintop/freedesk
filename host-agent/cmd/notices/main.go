// Command notices writes the license texts freedesk.exe carries: FreeDesk's
// own license, then the license of every piece of other people's code linked
// into it, taken from where the build got it, so the text cannot fall behind
// the dependencies. That is the Go modules and the Go standard library,
// libvpx, and the parts of the MinGW-w64 and GCC runtimes the -static link
// puts in the exe.
//
// The release is a single exe, so the text is embedded into it
// (internal/licenses), which means it is written before the build:
//
//	go run ./cmd/notices -o internal/licenses/generated/LICENSES.txt
//
// It needs the build's environment: CC naming MSYS2's UCRT64 gcc (its
// installation holds the runtime licenses) and libvpx built into
// third_party/libvpx by scripts/build-libvpx.sh.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// section is one notice: what it covers and the license text.
type section struct {
	title, about, text string
}

func main() {
	out := flag.String("o", "", "file to write (default: standard output)")
	flag.Parse()
	text, err := build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "notices: %v\n", err)
		os.Exit(1)
	}
	if *out == "" {
		_, _ = os.Stdout.WriteString(text)
		return
	}
	if err := os.WriteFile(*out, []byte(text), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "notices: %v\n", err)
		os.Exit(1)
	}
}

func build() (string, error) {
	root, err := goOutput("list", "-m", "-f", "{{.Dir}}")
	if err != nil {
		return "", err
	}
	modulePath, err := goOutput("list", "-m")
	if err != nil {
		return "", err
	}
	var sections []section

	own, err := freedeskSection(root, modulePath)
	if err != nil {
		return "", err
	}
	sections = append(sections, own)

	vpx, err := libvpxSection(filepath.Join(root, "third_party", "libvpx"))
	if err != nil {
		return "", err
	}
	sections = append(sections, vpx)

	runtime, err := mingwSections()
	if err != nil {
		return "", err
	}
	sections = append(sections, runtime...)

	goroot, err := goOutput("env", "GOROOT")
	if err != nil {
		return "", err
	}
	goLicense, err := os.ReadFile(filepath.Join(goroot, "LICENSE"))
	if err != nil {
		return "", fmt.Errorf("the Go license: %w", err)
	}
	sections = append(sections, section{"The Go standard library", "https://go.dev", string(goLicense)})

	mods, err := modules()
	if err != nil {
		return "", err
	}
	sections = append(sections, mods...)

	var b strings.Builder
	b.WriteString("FreeDesk - licenses\n\n")
	b.WriteString("freedesk.exe is FreeDesk, under the MIT license below. It also contains\n")
	b.WriteString("code from the projects after it, each used under its own license,\n")
	b.WriteString("reproduced here as that license asks.\n")
	rule := strings.Repeat("=", 78)
	for _, s := range sections {
		fmt.Fprintf(&b, "\n%s\n%s\n%s\n%s\n\n%s\n", rule, s.title, s.about, rule, strings.TrimSpace(s.text))
	}
	// Notepad-friendly line endings: every license file normalised to CRLF.
	text := strings.ReplaceAll(b.String(), "\r\n", "\n")
	return strings.ReplaceAll(text, "\n", "\r\n"), nil
}

// freedeskSection is FreeDesk's own license, from the repository root the
// host-agent module sits in.
func freedeskSection(root, modulePath string) (section, error) {
	text, err := os.ReadFile(filepath.Join(root, "..", "LICENSE"))
	if err != nil {
		return section{}, fmt.Errorf("FreeDesk's own license (LICENSE at the repository root): %w", err)
	}
	return section{
		title: "FreeDesk (this program)",
		about: "https://" + strings.TrimSuffix(modulePath, "/host-agent"),
		text:  string(text),
	}, nil
}

// libvpxSection reads the license and patent grant the build script copies
// next to the library, with the commit it was built from.
func libvpxSection(dir string) (section, error) {
	commit, err := os.ReadFile(filepath.Join(dir, "BUILD-COMMIT"))
	if err != nil {
		return section{}, fmt.Errorf("libvpx is not built (run scripts/build-libvpx.sh): %w", err)
	}
	license, err := os.ReadFile(filepath.Join(dir, "LICENSE"))
	if err != nil {
		return section{}, err
	}
	patents, err := os.ReadFile(filepath.Join(dir, "PATENTS"))
	if err != nil {
		return section{}, err
	}
	return section{
		title: "libvpx (the VP8 video encoder), commit " + strings.TrimSpace(string(commit)),
		about: "https://chromium.googlesource.com/webm/libvpx",
		text:  string(license) + "\n" + string(patents),
	}, nil
}

// mingwSections reads the licenses of the runtime parts a -static MinGW link
// puts in the exe, from the gcc installation CC names.
func mingwSections() ([]section, error) {
	cc := os.Getenv("CC")
	if cc == "" {
		return nil, errors.New("CC is not set: it must name the MSYS2 UCRT64 gcc the exe is built with")
	}
	if !filepath.IsAbs(cc) {
		if cc, _ = exec.LookPath(cc); cc == "" {
			return nil, fmt.Errorf("CC=%q is not on PATH", os.Getenv("CC"))
		}
	}
	licenses := filepath.Join(filepath.Dir(filepath.Dir(cc)), "share", "licenses")
	parts := []struct{ title, about, file string }{
		{"MinGW-w64 runtime", "https://www.mingw-w64.org (startup code and C runtime support)", filepath.Join("crt", "COPYING.MinGW-w64-runtime.txt")},
		{"winpthreads (MinGW-w64 POSIX threads)", "https://www.mingw-w64.org (threads used by libvpx)", filepath.Join("winpthreads", "COPYING")},
		{"GCC runtime library (libgcc)", "https://gcc.gnu.org (covered by the GCC Runtime Library Exception)", filepath.Join("gcc", "COPYING.RUNTIME")},
	}
	var out []section
	for _, p := range parts {
		text, err := os.ReadFile(filepath.Join(licenses, p.file))
		if err != nil {
			return nil, fmt.Errorf("%s license: %w", p.title, err)
		}
		out = append(out, section{p.title, p.about, string(text)})
	}
	return out, nil
}

// modules returns a section per Go module linked into the agent.
func modules() ([]section, error) {
	list, err := goOutput("list", "-deps", "-f",
		"{{with .Module}}{{if not .Main}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}{{end}}", "./cmd/host")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []section
	sc := bufio.NewScanner(strings.NewReader(list))
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) != 3 || seen[f[0]] {
			continue
		}
		seen[f[0]] = true
		text, err := licenseIn(f[2])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f[0], err)
		}
		out = append(out, section{f[0] + " " + f[1], "https://" + f[0], text})
	}
	slices.SortFunc(out, func(a, b section) int { return strings.Compare(a.title, b.title) })
	return out, nil
}

func licenseIn(dir string) (string, error) {
	for _, name := range []string{"LICENSE", "LICENSE.md", "LICENSE.txt", "LICENCE", "COPYING"} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			return string(b), nil
		}
	}
	return "", fmt.Errorf("no license file in %s", dir)
}

func goOutput(args ...string) (string, error) {
	var stderr bytes.Buffer
	cmd := exec.Command("go", args...)
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(string(b)), nil
}
