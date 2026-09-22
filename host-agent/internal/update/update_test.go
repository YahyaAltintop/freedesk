package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/releases/latest" {
			t.Errorf("asked %s", r.URL.Path)
		}
		if r.Header.Get("User-Agent") == "" || r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("GitHub needs a User-Agent and the JSON Accept header, got %q / %q", r.Header.Get("User-Agent"), r.Header.Get("Accept"))
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCheckReportsANewerRelease(t *testing.T) {
	srv := serve(t, http.StatusOK, `{"tag_name":"v0.4.0","html_url":"https://evil.example/"}`)
	latest, ok, err := check(context.Background(), srv.URL, "owner/repo", "0.3.0")
	if err != nil || !ok {
		t.Fatalf("check = %v, %v, %v", latest, ok, err)
	}
	if latest.Version != "0.4.0" {
		t.Errorf("version %q", latest.Version)
	}
	if latest.URL != "https://github.com/owner/repo/releases/latest" {
		t.Errorf("the link must come from the configured repository, not the answer: %q", latest.URL)
	}
}

func TestCheckIsQuietWhenNothingIsNewer(t *testing.T) {
	for _, current := range []string{"0.3.0", "0.3.0-dev", "0.3.1", "1.0.0"} {
		srv := serve(t, http.StatusOK, `{"tag_name":"v0.3.0"}`)
		if _, ok, err := check(context.Background(), srv.URL, "owner/repo", current); ok || err != nil {
			t.Errorf("current %s: ok=%v err=%v; want nothing to say", current, ok, err)
		}
	}
}

func TestCheckTreatsRateLimitAndOddAnswersAsNothing(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"rate limit":  {http.StatusForbidden, `{"message":"API rate limit exceeded"}`},
		"too many":    {http.StatusTooManyRequests, ``},
		"no release":  {http.StatusNotFound, `{"message":"Not Found"}`},
		"not json":    {http.StatusOK, `<html>`},
		"tagless":     {http.StatusOK, `{"name":"x"}`},
		"server down": {http.StatusBadGateway, ``},
	}
	for name, c := range cases {
		srv := serve(t, c.status, c.body)
		latest, ok, err := check(context.Background(), srv.URL, "owner/repo", "0.3.0")
		if ok {
			t.Errorf("%s: reported %v as an update", name, latest)
		}
		if err == nil {
			t.Errorf("%s: the reason must still be available to whoever asks", name)
		}
	}
}

func TestCheckGivesUpWithTheContext(t *testing.T) {
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-blocked }))
	defer func() { close(blocked); srv.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok, err := check(ctx, srv.URL, "owner/repo", "0.3.0"); ok || err == nil {
		t.Fatalf("a cancelled context must end the check quietly, got ok=%v err=%v", ok, err)
	}
}

func TestCheckNeedsARepository(t *testing.T) {
	if _, ok, err := check(context.Background(), "http://127.0.0.1:1", "", "0.3.0"); ok || err == nil {
		t.Fatal("an empty repository must not be asked about")
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.4.0", "0.3.0", 1},
		{"0.3.0", "0.4.0", -1},
		{"v0.3.0", "0.3.0", 0},
		{"0.3.0", "0.3.0-dev", 0},
		{"0.3.1", "0.3.0-dev", 1},
		{"0.10.0", "0.9.9", 1},
		{"1.2", "1.2.0", 0},
		{"1.2.0.1", "1.2", 1},
		{"0.3.0-rc1", "0.3.0", 0},
		{"junk", "0.0.1", -1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	if !strings.HasPrefix("v1", "v") { // keeps the import honest if the table shrinks
		t.Fatal("unreachable")
	}
}
