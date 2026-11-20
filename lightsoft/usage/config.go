package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// PathPrefix is the URL prefix the service answers under. Tailscale serve
// maps the same path on the team client origin (:3444) to this process, so
// the page shares the team client's login cookie.
const PathPrefix = "/lightsoft-usage"

type Config struct {
	Addr           string
	APIURL         string
	AllowedOrigins map[string]bool
	DataDir        string
	AccountsPath   string
	HistoryPath    string
	// Interval is how often every account is read. Codex reads are local
	// files; the Claude read is one HTTPS request per account.
	Interval time.Duration
	// Retention bounds how long snapshots are kept for the trend chart.
	Retention time.Duration
}

func loadConfig() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolve home: %w", err)
	}
	dataDir := env("LIGHTSOFT_USAGE_DATA_DIR", filepath.Join(home, ".multica", "usage"))
	interval, err := time.ParseDuration(env("LIGHTSOFT_USAGE_INTERVAL", "5m"))
	if err != nil || interval < 30*time.Second {
		return Config{}, fmt.Errorf("LIGHTSOFT_USAGE_INTERVAL must be a duration of at least 30s")
	}
	days, err := strconv.Atoi(env("LIGHTSOFT_USAGE_RETENTION_DAYS", "35"))
	if err != nil || days < 1 {
		return Config{}, fmt.Errorf("LIGHTSOFT_USAGE_RETENTION_DAYS must be a positive number of days")
	}
	return Config{
		Addr:   env("LIGHTSOFT_USAGE_ADDR", "127.0.0.1:13500"),
		APIURL: strings.TrimRight(env("LIGHTSOFT_USAGE_API_URL", "http://127.0.0.1:8080"), "/"),
		AllowedOrigins: csvSet(env("LIGHTSOFT_USAGE_ALLOWED_ORIGINS",
			"https://mac-studio.taile83671.ts.net:3444,http://localhost:13100")),
		DataDir:      dataDir,
		AccountsPath: env("LIGHTSOFT_USAGE_ACCOUNTS", filepath.Join(dataDir, "accounts.json")),
		HistoryPath:  env("LIGHTSOFT_USAGE_HISTORY", filepath.Join(dataDir, "history.jsonl")),
		Interval:     interval,
		Retention:    time.Duration(days) * 24 * time.Hour,
	}, nil
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
