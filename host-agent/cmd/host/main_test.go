package main

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/YahyaAltintop/freedesk/host-agent/internal/firebase"
)

func TestFormatPairingCode(t *testing.T) {
	if got := formatPairingCode("123456789"); got != "123 456 789" {
		t.Errorf("formatPairingCode = %q", got)
	}
	if got := formatPairingCode("12345"); got != "12345" {
		t.Errorf("odd length must pass through, got %q", got)
	}
}

func TestRegistrationDeniedErrorPointsAtTheRules(t *testing.T) {
	cause := &firebase.StatusError{Code: http.StatusUnauthorized, Method: "PUT", Path: "hosts/123456789", Body: `{"error":"Permission denied"}`}
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
