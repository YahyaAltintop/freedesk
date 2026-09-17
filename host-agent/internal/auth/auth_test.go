package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const referrerBlockedBody = `{"error":{"code":403,"message":"Requests from referer <empty> are blocked.","status":"PERMISSION_DENIED",` +
	`"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"API_KEY_HTTP_REFERRER_BLOCKED","domain":"googleapis.com"}]}}`

func TestSignUpAnonymousExplainsReferrerRestriction(t *testing.T) {
	var gotReferer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReferer = r.Header.Get("Referer")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, referrerBlockedBody)
	}))
	defer srv.Close()

	_, err := NewClient("key", srv.URL, srv.URL).SignUpAnonymous(context.Background())
	var authErr *Error
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if authErr.Status != http.StatusForbidden || authErr.Reason != "API_KEY_HTTP_REFERRER_BLOCKED" {
		t.Fatalf("unexpected error fields: %+v", authErr)
	}
	if gotReferer != "" {
		t.Fatalf("the agent must not pretend to be a web site, but sent Referer %q", gotReferer)
	}
	for _, want := range []string{"HTTP 403", "API_KEY_HTTP_REFERRER_BLOCKED", "Application restrictions", "FIREBASE_AGENT_API_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error text lacks %q:\n%s", want, err)
		}
	}
}

func TestSignUpAnonymousParsesToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/accounts:signUp") || r.URL.Query().Get("key") != "key" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		_, _ = io.WriteString(w, `{"idToken":"id","refreshToken":"refresh","expiresIn":"3600","localId":"uid-1"}`)
	}))
	defer srv.Close()

	tok, err := NewClient("key", srv.URL, srv.URL).SignUpAnonymous(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok.IDToken != "id" || tok.RefreshToken != "refresh" || tok.UID != "uid-1" || tok.ExpiresAt.IsZero() {
		t.Fatalf("unexpected token: %+v", tok)
	}
}

func TestNewErrorReasonsAndHints(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantReason string
		wantHint   string // substring; "" = no hint
	}{
		{
			name:       "api restrictions exclude identity toolkit",
			status:     403,
			body:       `{"error":{"code":403,"message":"Requests to this API identitytoolkit method google.cloud.identitytoolkit.v1.AccountManagementService.GetAccountInfo are blocked.","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"API_KEY_SERVICE_BLOCKED"}]}}`,
			wantReason: "API_KEY_SERVICE_BLOCKED",
			wantHint:   "Token Service API",
		},
		{
			name:       "anonymous provider disabled",
			status:     400,
			body:       `{"error":{"code":400,"message":"ADMIN_ONLY_OPERATION","errors":[{"message":"ADMIN_ONLY_OPERATION","domain":"global","reason":"invalid"}]}}`,
			wantReason: "ADMIN_ONLY_OPERATION",
			wantHint:   "Anonymous",
		},
		{
			name:       "identity toolkit code followed by prose",
			status:     400,
			body:       `{"error":{"code":400,"message":"TOO_MANY_ATTEMPTS_TRY_LATER : Please try again later."}}`,
			wantReason: "TOO_MANY_ATTEMPTS_TRY_LATER",
			wantHint:   "wait",
		},
		{
			name:       "invalid key",
			status:     400,
			body:       `{"error":{"code":400,"message":"API key not valid. Please pass a valid API key.","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"API_KEY_INVALID"}]}}`,
			wantReason: "API_KEY_INVALID",
			wantHint:   "not valid",
		},
		{
			name:       "prose message without a code",
			status:     400,
			body:       `{"error":{"code":400,"message":"API key not valid. Please pass a valid API key."}}`,
			wantReason: "",
			wantHint:   "",
		},
		{
			name:       "not json at all",
			status:     502,
			body:       "<html>\n  <body>Bad   Gateway</body>\n</html>",
			wantReason: "",
			wantHint:   "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newError(tc.status, []byte(tc.body))
			if e.Status != tc.status {
				t.Errorf("status = %d, want %d", e.Status, tc.status)
			}
			if e.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", e.Reason, tc.wantReason)
			}
			if hint := e.Hint(); (tc.wantHint == "") != (hint == "") || !strings.Contains(hint, tc.wantHint) {
				t.Errorf("hint = %q, want substring %q", hint, tc.wantHint)
			}
			if strings.Contains(e.Error(), "\n") != (tc.wantHint != "") {
				t.Errorf("only errors with a hint should span two lines:\n%s", e.Error())
			}
		})
	}

	if got := newError(502, []byte("<html>\n  <body>Bad   Gateway</body>\n</html>")).Message; got != "<html> <body>Bad Gateway</body> </html>" {
		t.Errorf("non-JSON body not compacted: %q", got)
	}
}
