// Package config loads the host agent's runtime configuration from environment
// variables (optionally seeded from a local .env file).
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Build-time defaults, injected by the release build with
//
//	-ldflags "-X .../internal/config.BuildAPIKey=... -X .../internal/config.BuildProjectID=... -X .../internal/config.BuildDatabaseURL=..."
//
// so end users never touch a .env file. Environment variables still override
// them (developers pointing a release binary at another project or the
// emulator).
var (
	BuildAPIKey      string
	BuildProjectID   string
	BuildDatabaseURL string
	// BuildHostingSite is the Firebase Hosting site id when it differs from
	// the project id (firebase.json "hosting.site"); it adds the site's
	// web.app / firebaseapp.com origins to the loopback allow-list.
	BuildHostingSite string
)

// Config holds all runtime configuration for the host agent.
type Config struct {
	APIKey            string
	ProjectID         string
	DatabaseURL       string
	DatabaseNamespace string

	HostName     string // defaults to the OS hostname
	LocalAPIPort int    // loopback port serving the pairing code to the local web UI
	FFmpegPath   string // ffmpeg executable for screen capture (empty = next to the exe, else PATH)
	ApprovalMode string // "dialog" (Windows default) or "console"
	HostingSite  string // Firebase Hosting site id if it differs from the project id

	// WebOrigins are the browser origins allowed to read the pairing code from
	// the loopback endpoint: the deployed web app plus the local dev server.
	WebOrigins []string

	HeartbeatInterval time.Duration

	// Service endpoints — overridable for the Firebase Emulator Suite.
	AuthBaseURL  string
	TokenBaseURL string
}

const (
	defaultAuthBaseURL  = "https://identitytoolkit.googleapis.com"
	defaultTokenBaseURL = "https://securetoken.googleapis.com"

	// The viewer treats a host as offline after 90 s without a heartbeat
	// (frontend/src/utils/presence.ts); three beats fit in that window.
	defaultHeartbeatInterval = 30 * time.Second

	// defaultLocalAPIPort must match the frontend's local-agent endpoint
	// (frontend/src/services/localAgent.ts).
	defaultLocalAPIPort = 47800
)

// Load reads configuration from the given .env file (if present) and the
// process environment. Environment variables take precedence over the file.
func Load(envFilePath string) (*Config, error) {
	if err := loadDotEnv(envFilePath); err != nil {
		return nil, err
	}

	cfg := &Config{
		APIKey:            firstNonEmpty(os.Getenv("RC_FIREBASE_API_KEY"), BuildAPIKey),
		ProjectID:         firstNonEmpty(os.Getenv("RC_FIREBASE_PROJECT_ID"), BuildProjectID),
		DatabaseURL:       firstNonEmpty(os.Getenv("RC_FIREBASE_DB_URL"), BuildDatabaseURL),
		DatabaseNamespace: os.Getenv("RC_FIREBASE_DB_NAMESPACE"),
		HostName:          os.Getenv("RC_HOST_NAME"),
		FFmpegPath:        os.Getenv("RC_FFMPEG_PATH"),
		ApprovalMode:      strings.ToLower(strings.TrimSpace(os.Getenv("RC_APPROVAL"))),
		HostingSite:       firstNonEmpty(os.Getenv("RC_HOSTING_SITE"), BuildHostingSite),
		LocalAPIPort:      defaultLocalAPIPort,
		HeartbeatInterval: defaultHeartbeatInterval,
		AuthBaseURL:       defaultAuthBaseURL,
		TokenBaseURL:      defaultTokenBaseURL,
	}
	if cfg.ApprovalMode != "" && cfg.ApprovalMode != "dialog" && cfg.ApprovalMode != "console" {
		return nil, fmt.Errorf("RC_APPROVAL invalid: %q (must be dialog or console)", cfg.ApprovalMode)
	}

	if raw := os.Getenv("RC_LOCAL_PORT"); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("RC_LOCAL_PORT invalid: %q (must be between 1 and 65535)", raw)
		}
		cfg.LocalAPIPort = port
	}

	if cfg.HostName == "" {
		if name, err := os.Hostname(); err == nil && name != "" {
			cfg.HostName = name
		} else {
			cfg.HostName = "Windows Host"
		}
	}
	// The host record's name field is validated to at most 64 characters.
	if len(cfg.HostName) > 64 {
		cfg.HostName = cfg.HostName[:64]
	}

	applyEmulatorOverrides(cfg)

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	cfg.WebOrigins = webOrigins(cfg.ProjectID, cfg.HostingSite, os.Getenv("RC_WEB_ORIGINS"))
	return cfg, nil
}

// webOrigins lists the Firebase Hosting origins of this project (the default
// site named after the project id, plus a differently named site if one is
// configured) and the Vite dev server, extended by the comma-separated
// RC_WEB_ORIGINS.
func webOrigins(projectID, hostingSite, extra string) []string {
	origins := []string{
		"https://" + projectID + ".web.app",
		"https://" + projectID + ".firebaseapp.com",
	}
	if site := strings.TrimSpace(hostingSite); site != "" && site != projectID {
		origins = append(origins,
			"https://"+site+".web.app",
			"https://"+site+".firebaseapp.com",
		)
	}
	origins = append(origins, "http://localhost:9205", "http://127.0.0.1:9205")
	for o := range strings.SplitSeq(extra, ",") {
		if o = strings.TrimSpace(o); o != "" {
			origins = append(origins, strings.TrimRight(o, "/"))
		}
	}
	return origins
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// applyEmulatorOverrides honours the standard Firebase emulator environment
// variables, mirroring the official SDKs.
func applyEmulatorOverrides(cfg *Config) {
	if host := os.Getenv("FIREBASE_AUTH_EMULATOR_HOST"); host != "" {
		cfg.AuthBaseURL = "http://" + host + "/identitytoolkit.googleapis.com"
		cfg.TokenBaseURL = "http://" + host + "/securetoken.googleapis.com"
	}
	if host := os.Getenv("FIREBASE_DATABASE_EMULATOR_HOST"); host != "" {
		cfg.DatabaseURL = "http://" + host
		if cfg.DatabaseNamespace == "" {
			cfg.DatabaseNamespace = cfg.ProjectID + "-default-rtdb"
		}
	}
}

func (c *Config) validate() error {
	missing := make([]string, 0, 3)
	if c.APIKey == "" {
		missing = append(missing, "RC_FIREBASE_API_KEY")
	}
	if c.ProjectID == "" {
		missing = append(missing, "RC_FIREBASE_PROJECT_ID")
	}
	if c.DatabaseURL == "" {
		missing = append(missing, "RC_FIREBASE_DB_URL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing configuration: %s (this build has no embedded Firebase project; set them in .env or the environment)", strings.Join(missing, ", "))
	}
	return nil
}

// loadDotEnv parses a simple KEY=VALUE file and sets any variable that is not
// already present in the environment. A missing file is not an error.
func loadDotEnv(path string) error {
	if path == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}
