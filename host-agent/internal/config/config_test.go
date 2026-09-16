package config

import (
	"reflect"
	"testing"
)

func TestWebOriginsIncludeProjectSiteAndExtras(t *testing.T) {
	got := webOrigins("my-project", "free-desk", " https://staging.example/ ,, http://other.example")
	want := []string{
		"https://my-project.web.app",
		"https://my-project.firebaseapp.com",
		"https://free-desk.web.app",
		"https://free-desk.firebaseapp.com",
		"http://localhost:9205",
		"http://127.0.0.1:9205",
		"https://staging.example",
		"http://other.example",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("webOrigins:\n got %v\nwant %v", got, want)
	}
}

func TestSiteURLPrefersHostingSite(t *testing.T) {
	cfg := &Config{ProjectID: "my-project", HostingSite: " free-desk "}
	if got, want := cfg.SiteURL(), "https://free-desk.web.app"; got != want {
		t.Fatalf("SiteURL with site: got %q want %q", got, want)
	}
	cfg.HostingSite = ""
	if got, want := cfg.SiteURL(), "https://my-project.web.app"; got != want {
		t.Fatalf("SiteURL without site: got %q want %q", got, want)
	}
}

func TestWebOriginsSkipSiteEqualToProject(t *testing.T) {
	got := webOrigins("my-project", "my-project", "")
	want := []string{
		"https://my-project.web.app",
		"https://my-project.firebaseapp.com",
		"http://localhost:9205",
		"http://127.0.0.1:9205",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("webOrigins:\n got %v\nwant %v", got, want)
	}
}
