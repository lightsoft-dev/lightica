// Command lightsoft-terminal serves temporary project shells for the
// Lightsoft team client. It runs on the Mac Studio host next to production
// Multica, authenticates callers against the production API with their own
// login, and never touches the Multica database.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	s := newServer(cfg)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		log.Printf("shutting down: closing open terminals")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	// Shutdown does not wait for hijacked WebSocket connections, so stop the
	// shells explicitly once the listener is closed.
	defer s.stopAll()

	log.Printf("lightsoft-terminal listening on %s%s (api %s, roles %v)", cfg.Addr, PathPrefix, cfg.APIURL, keys(cfg.AllowedRoles))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
