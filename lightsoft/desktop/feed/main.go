// Command lightica-feed serves the Lightica release folder (LIGH-191) to the
// tailnet: Tailscale serve proxies https://<host>:3444/lightica/ here. The
// folder holds what lightsoft/desktop/release.sh publishes — dmg, zip,
// blockmaps, latest-mac.yml for electron-updater, releases.json and the
// index.html release page. Read-only; it never writes to the folder.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// PathPrefix is the Tailscale serve mount. The proxy keeps it on the request
// (`http://127.0.0.1:13400/lightica`), like the other :3444 team services.
const PathPrefix = "/lightica/"

func main() {
	addr := envOr("LIGHTICA_FEED_ADDR", "127.0.0.1:13400")
	dir := os.Getenv("LIGHTICA_FEED_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		dir = filepath.Join(home, ".multica", "lightica", "feed")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           newHandler(dir),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("lightica-feed listening on %s%s (dir %s)", addr, PathPrefix, dir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func newHandler(dir string) http.Handler {
	files := http.StripPrefix(strings.TrimSuffix(PathPrefix, "/"), http.FileServer(http.Dir(dir)))
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+PathPrefix+"health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /lightica", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, PathPrefix, http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET "+PathPrefix, func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, PathPrefix)
		// release.sh stages files under dot names before renaming them in.
		for _, part := range strings.Split(name, "/") {
			if strings.HasPrefix(part, ".") {
				http.NotFound(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", cacheControl(name))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		files.ServeHTTP(w, r)
	})
	return mux
}

// cacheControl keeps the metadata fresh (latest-mac.yml decides what installed
// apps download) and lets versioned artifacts be cached: their names carry the
// version, so their bytes never change.
func cacheControl(name string) string {
	switch filepath.Ext(name) {
	case ".dmg", ".zip", ".blockmap":
		return "public, max-age=31536000, immutable"
	default:
		return "no-cache"
	}
}
