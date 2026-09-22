package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/firebase"
)

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

func TestRegistrationDeniedErrorPointsAtTheRules(t *testing.T) {
	cause := &firebase.StatusError{Code: http.StatusUnauthorized, Method: "PUT", Path: "hosts/123456", Body: `{"error":"Permission denied"}`}
	err := registrationDeniedError(3, "my-project", cause)
	if !errors.Is(err, cause) {
		t.Fatal("the original error must stay unwrappable")
	}
	for _, want := range []string{"3 times in a row", `"my-project"`, "firebase deploy --only database", "Permission denied"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error text lacks %q:\n%s", want, err)
		}
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
