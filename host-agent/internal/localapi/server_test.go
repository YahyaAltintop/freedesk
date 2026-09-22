package localapi

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"testing"
)

func startTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	// Pick a free port first; Start binds it right after.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	srv, err := Start(port, Identity{Code: "123456", Name: "pc", Version: "t"}, []string{"https://app.example"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv, "http://127.0.0.1:" + strconv.Itoa(port) + "/identity"
}

func get(t *testing.T, url, origin string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestIdentityServedOnlyToAllowedOrigin(t *testing.T) {
	_, url := startTestServer(t)

	resp := get(t, url, "https://app.example")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("allowed origin: expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Fatalf("expected the origin to be echoed, got %q", got)
	}
	var id Identity
	if err := json.NewDecoder(resp.Body).Decode(&id); err != nil || id.Code != "123456" {
		t.Fatalf("unexpected body: %+v err=%v", id, err)
	}

	for _, origin := range []string{"https://evil.example", ""} {
		resp := get(t, url, origin)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("origin %q: expected 403, got %d", origin, resp.StatusCode)
		}
		if resp.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("origin %q: no CORS header expected", origin)
		}
	}
}

// The code the endpoint hands out has to follow the code the agent is
// actually published under, or the home page would show one that no longer
// reaches this machine.
func TestUpdatedIdentityIsServed(t *testing.T) {
	srv, url := startTestServer(t)
	srv.Update(Identity{Code: "987654321", Name: "pc", Version: "t"})

	resp := get(t, url, "https://app.example")
	var id Identity
	if err := json.NewDecoder(resp.Body).Decode(&id); err != nil {
		t.Fatal(err)
	}
	if id.Code != "987654321" {
		t.Fatalf("served %q after Update, expected the new code", id.Code)
	}
}
