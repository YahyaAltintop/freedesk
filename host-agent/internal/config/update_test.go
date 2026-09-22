package config

import (
	"strings"
	"testing"
)

// minimal sets what Load needs to succeed; Load("") reads no .env file.
func minimal(t *testing.T) {
	t.Helper()
	t.Setenv("RC_FIREBASE_API_KEY", "k")
	t.Setenv("RC_FIREBASE_PROJECT_ID", "p")
	t.Setenv("RC_FIREBASE_DB_URL", "https://p.firebaseio.com")
	t.Setenv("RC_UPDATE_CHECK", "")
	t.Setenv("RC_GITHUB_REPO", "")
}

func TestUpdateCheckIsOnUnlessTurnedOff(t *testing.T) {
	minimal(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.UpdateCheck || cfg.GitHubRepo != defaultRepo {
		t.Fatalf("defaults: check=%v repo=%q", cfg.UpdateCheck, cfg.GitHubRepo)
	}

	t.Setenv("RC_UPDATE_CHECK", " OFF ")
	t.Setenv("RC_GITHUB_REPO", "someone/fork")
	cfg, err = Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UpdateCheck || cfg.GitHubRepo != "someone/fork" {
		t.Fatalf("overrides: check=%v repo=%q", cfg.UpdateCheck, cfg.GitHubRepo)
	}
}

func TestUpdateSettingsAreValidated(t *testing.T) {
	minimal(t)
	t.Setenv("RC_UPDATE_CHECK", "maybe")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "RC_UPDATE_CHECK") {
		t.Fatalf("RC_UPDATE_CHECK=maybe must be refused, got %v", err)
	}

	t.Setenv("RC_UPDATE_CHECK", "")
	for _, bad := range []string{"owner", "owner/name/extra", "owner/na me", "../x", "owner/..", "-owner/name", "owner/"} {
		t.Setenv("RC_GITHUB_REPO", bad)
		if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "RC_GITHUB_REPO") {
			t.Errorf("RC_GITHUB_REPO=%q must be refused, got %v", bad, err)
		}
	}
	for _, good := range []string{"owner/name", "my-org/.github", "a/b_c.d-e"} {
		t.Setenv("RC_GITHUB_REPO", good)
		if _, err := Load(""); err != nil {
			t.Errorf("RC_GITHUB_REPO=%q must be accepted, got %v", good, err)
		}
	}
}
