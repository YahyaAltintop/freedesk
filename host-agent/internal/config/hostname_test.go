package config

import (
	"runtime"
	"testing"
)

func TestShortHostName(t *testing.T) {
	for name, want := range map[string]string{
		"Yahyas-MacBook-Pro.local": "Yahyas-MacBook-Pro",
		"studio.LOCAL":             "studio",
		"DESKTOP-4F2K9QX":          "DESKTOP-4F2K9QX",
		"build.example.com":        "build.example.com",
		"localhost":                "localhost",
		".local":                   ".local", // nothing would be left
	} {
		if got := shortHostName(name); got != want {
			t.Errorf("shortHostName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestFallbackHostNameNamesThePlatform(t *testing.T) {
	want := "Windows Host"
	if runtime.GOOS == "darwin" {
		want = "Mac Host"
	}
	if got := fallbackHostName(); got != want {
		t.Errorf("fallbackHostName() = %q, want %q on %s", got, want, runtime.GOOS)
	}
}
