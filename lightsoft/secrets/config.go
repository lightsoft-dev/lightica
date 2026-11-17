package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PathPrefix is the URL prefix the service answers under. Tailscale serve
// maps the same path on the team client origin (:3444) to this process, so
// the secrets page shares the team client's login cookie.
const PathPrefix = "/lightsoft-secrets"

type Config struct {
	Addr           string
	APIURL         string
	AllowedOrigins map[string]bool
	DataDir        string
	DBPath         string
	KeyPath        string
	// PublicURL is written into an enrolled agent's custom_env as
	// LIGHTSOFT_SECRETS_URL; agents on any tailnet machine reach the service
	// through it.
	PublicURL string
}

func loadConfig() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolve home: %w", err)
	}
	dataDir := env("LIGHTSOFT_SECRETS_DATA_DIR", filepath.Join(home, ".multica", "secrets"))
	cfg := Config{
		Addr:   env("LIGHTSOFT_SECRETS_ADDR", "127.0.0.1:13300"),
		APIURL: strings.TrimRight(env("LIGHTSOFT_SECRETS_API_URL", "http://127.0.0.1:8080"), "/"),
		AllowedOrigins: csvSet(env("LIGHTSOFT_SECRETS_ALLOWED_ORIGINS",
			"https://mac-studio.taile83671.ts.net:3444,http://localhost:13100")),
		DataDir:   dataDir,
		DBPath:    env("LIGHTSOFT_SECRETS_DB", filepath.Join(dataDir, "secrets.db")),
		KeyPath:   env("LIGHTSOFT_SECRETS_KEY_FILE", filepath.Join(dataDir, "master.key")),
		PublicURL: strings.TrimRight(env("LIGHTSOFT_SECRETS_PUBLIC_URL", "https://mac-studio.taile83671.ts.net:3444"+PathPrefix), "/"),
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
