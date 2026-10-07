package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/firebase"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/firewall"
	"github.com/YahyaAltintop/freedesk/host-agent/internal/licenses"
)

func TestLicensesRequested(t *testing.T) {
	for _, c := range []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"--licenses"}, true},
		{[]string{"-licenses"}, true},
		{[]string{"/LICENSES"}, true},
		{[]string{"something", "--Licenses"}, true},
		{[]string{"--license"}, false},
		{[]string{"licenses"}, false},
	} {
		if got := licensesRequested(c.args); got != c.want {
			t.Errorf("licensesRequested(%q) = %v, want %v", c.args, got, c.want)
		}
	}
}

// TestPrintLicenses: the texts the build carries come out whole; a developer
// build without them says so and fails, never prints an empty success.
func TestPrintLicenses(t *testing.T) {
	var out bytes.Buffer
	code := printLicenses(&out)
	text, ok := licenses.Text()
	if ok {
		if code != 0 || out.String() != text {
			t.Fatalf("exit %d, %d bytes; want 0 and the %d bytes of the build's texts", code, out.Len(), len(text))
		}
		return
	}
	if code != 1 || !strings.Contains(out.String(), "developer build") {
		t.Fatalf("a build without texts: exit %d, %q; want 1 and an explanation", code, out.String())
	}
}

func TestFormatPairingCode(t *testing.T) {
	if got := formatPairingCode("123456"); got != "123 - 456" {
		t.Errorf("formatPairingCode = %q", got)
	}
	if got := formatPairingCode("12345"); got != "12345" {
		t.Errorf("odd length must pass through, got %q", got)
	}
}

func TestNewPairingCodeIsSixDigits(t *testing.T) {
	for range 20 {
		code, err := newPairingCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != pairingCodeDigits || strings.Trim(code, "0123456789") != "" {
			t.Errorf("newPairingCode = %q", code)
		}
	}
}

// The operator reads a plain sentence; the diagnosis — which names the
// project and the console command that fixes it — is for the developer's
// channel only, and the cause stays unwrappable.
func TestRegistrationDeniedErrorKeepsTheDiagnosisOffTheOperator(t *testing.T) {
	cause := &firebase.StatusError{Code: http.StatusUnauthorized, Method: "PUT", Path: "hosts/123456", Body: `{"error":"Permission denied"}`}
	err := registrationDeniedError(3, "my-project", cause)
	if !errors.Is(err, cause) {
		t.Fatal("the original error must stay unwrappable")
	}
	text := err.Error()
	for _, forbidden := range []string{"firebase", "my-project", "Permission denied", "401", "rules"} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(forbidden)) {
			t.Errorf("the operator's message must not carry %q:\n%s", forbidden, text)
		}
	}
	var se *startupError
	if !errors.As(err, &se) {
		t.Fatalf("expected a startupError, got %T", err)
	}
	for _, want := range []string{"3 times in a row", `"my-project"`, "firebase deploy --only database", "Permission denied"} {
		if !strings.Contains(se.detail, want) {
			t.Errorf("the diagnosis lacks %q:\n%s", want, se.detail)
		}
	}
}

// fakeFirewall makes checkFirewall answer st for the rest of the test, and
// captures what the operator would read.
func fakeFirewall(t *testing.T, st firewall.Status, onCheck func()) *bytes.Buffer {
	t.Helper()
	origCheck, origLog := checkFirewall, log.Writer()
	checkFirewall = func(context.Context, string) (firewall.Status, error) {
		onCheck()
		return st, nil
	}
	var out bytes.Buffer
	log.SetOutput(&out)
	t.Cleanup(func() {
		checkFirewall = origCheck
		log.SetOutput(origLog)
	})
	return &out
}

// With a security product in charge of the firewall Windows' rules are not
// enforced, so their absence is no reason to tell the operator they are
// blocked — on such a machine that warning came up on every start while
// connections worked. The product's name is passed on instead, at once.
func TestWatchFirewallStandsDownForAnotherProduct(t *testing.T) {
	out := fakeFirewall(t, firewall.Status{Owner: "Norton 360", Enabled: true, Network: "Private"}, func() {})

	var owner string
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchFirewall(t.Context(), nil, func(name string) { owner = name })
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watchFirewall kept checking although another product runs the firewall")
	}
	if owner != "Norton 360" {
		t.Errorf("owner passed on = %q, want Norton 360", owner)
	}
	if out.Len() != 0 {
		t.Errorf("the operator was told something about Windows' firewall:\n%s", out)
	}
}

// Windows' own firewall with no rule for the program is still worth a
// warning, and nobody is named as the firewall's owner.
func TestWatchFirewallStillWarnsForWindows(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	// Cancelled during the first check: the warning is written, then the
	// wait for the next check ends at once.
	out := fakeFirewall(t, firewall.Status{Enabled: true, Network: "Private"}, cancel)

	named := false
	watchFirewall(ctx, nil, func(string) { named = true })
	if named {
		t.Error("an owner was named for Windows' own firewall")
	}
	if !strings.Contains(out.String(), "Windows has not yet allowed this program to receive connections") {
		t.Errorf("no warning for a program Windows' firewall has no rule for:\n%s", out)
	}
}

// runMain never reaches run() when the configuration failed, which makes the
// exit-code mapping testable without Firebase.
func TestRunMainExitCodes(t *testing.T) {
	boom := errors.New("missing configuration")

	if got := runMain(context.Background(), nil, boom, nil); got != 1 {
		t.Errorf("a failed run must exit 1, got %d", got)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if got := runMain(cancelled, nil, boom, nil); got != 0 {
		t.Errorf("a run the operator stopped is a clean exit, got %d", got)
	}
}
