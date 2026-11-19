package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// PathPrefix is the URL prefix the gateway serves under. Tailscale serve maps
// the same path on the team client origin (:3444) to this process, so the
// terminal page shares the origin, and therefore the login cookie, of the
// team client.
const PathPrefix = "/lightsoft-term"

type Config struct {
	Addr           string
	APIURL         string
	AllowedOrigins map[string]bool
	AllowedRoles   map[string]bool
	DaemonID       string
	IdleTimeout    time.Duration
	MaxLifetime    time.Duration
	MaxPerUser     int
	MaxTotal       int
	AuditLogPath   string
	Shell          string
	Home           string
}

func loadConfig() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolve home: %w", err)
	}
	cfg := Config{
		Addr:   env("LIGHTSOFT_TERM_ADDR", "127.0.0.1:13200"),
		APIURL: strings.TrimRight(env("LIGHTSOFT_TERM_API_URL", "http://127.0.0.1:8080"), "/"),
		AllowedOrigins: csvSet(env("LIGHTSOFT_TERM_ALLOWED_ORIGINS",
			"https://mac-studio.taile83671.ts.net:3444,http://localhost:13200,http://127.0.0.1:13200")),
		AllowedRoles: csvSet(env("LIGHTSOFT_TERM_ALLOWED_ROLES", "owner,admin")),
		DaemonID:     os.Getenv("LIGHTSOFT_TERM_DAEMON_ID"),
		AuditLogPath: env("LIGHTSOFT_TERM_AUDIT_LOG", filepath.Join(home, ".multica", "terminal", "audit.log")),
		Shell:        env("LIGHTSOFT_TERM_SHELL", env("SHELL", "/bin/zsh")),
		Home:         home,
	}
	if cfg.IdleTimeout, err = durationEnv("LIGHTSOFT_TERM_IDLE_TIMEOUT", 15*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.MaxLifetime, err = durationEnv("LIGHTSOFT_TERM_MAX_LIFETIME", 4*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.MaxPerUser, err = intEnv("LIGHTSOFT_TERM_MAX_PER_USER", 3); err != nil {
		return Config{}, err
	}
	if cfg.MaxTotal, err = intEnv("LIGHTSOFT_TERM_MAX_TOTAL", 10); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func csvSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out[p] = true
		}
	}
	return out
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s: invalid duration %q", key, v)
	}
	return d, nil
}

func intEnv(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s: invalid positive integer %q", key, v)
	}
	return n, nil
}
