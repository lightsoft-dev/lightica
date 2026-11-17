// Command lightsoft-secrets-server stores team secrets for the Lightsoft
// team client. It runs on the Mac Studio next to production Multica, keeps
// values AES-256-GCM encrypted in its own SQLite file, authenticates people
// with their Multica login and agents with an enrollment token, and never
// touches the Multica database.
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
	// Files this process creates (database, WAL, logs) stay private to the
	// service user.
	syscall.Umask(0o077)

	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		log.Fatal(err)
	}
	key, err := loadKey(cfg.KeyPath)
	if err != nil {
		log.Fatal(err)
	}
	box, err := newSealer(key)
	if err != nil {
		log.Fatal(err)
	}
	st, err := openStore(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	s := &server{cfg: cfg, api: newMulticaClient(cfg.APIURL), store: st, box: box}
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("lightsoft-secrets listening on %s%s (api %s, public %s)", cfg.Addr, PathPrefix, cfg.APIURL, cfg.PublicURL)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
