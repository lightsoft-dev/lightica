// Command lightsoft-usage-server tracks the subscription limits of the agent
// CLI accounts on the Lightsoft Mac Studio (Claude Max, Codex) for the team
// client's 구독 한도 page. It reads Codex session logs and Claude Code's
// usage endpoint on an interval, keeps the snapshots in its own JSON-lines
// file, and never touches the Multica database.
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
	syscall.Umask(0o077)

	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		log.Fatal(err)
	}
	accounts, err := loadAccounts(cfg.AccountsPath)
	if err != nil {
		log.Fatal(err)
	}
	history, err := openHistory(cfg.HistoryPath, cfg.Retention, time.Now)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	col := newCollector(accounts.Accounts, history, time.Now)
	go col.run(ctx, cfg.Interval)

	s := &server{cfg: cfg, api: newMulticaClient(cfg.APIURL), accounts: accounts, collector: col, history: history, now: time.Now}
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("lightsoft-usage listening on %s%s (api %s, %d accounts, every %s)",
		cfg.Addr, PathPrefix, cfg.APIURL, len(accounts.Accounts), cfg.Interval)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
