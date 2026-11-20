package main

import (
	"context"
	"log"
	"sync"
	"time"
)

type limitsSource interface {
	Read(ctx context.Context) (RateLimits, error)
}

// collector polls every account on an interval, stores each new snapshot,
// and remembers the last failure so the page can say why a number is old.
type collector struct {
	accounts []Account
	sources  map[string]limitsSource
	history  *historyStore
	now      func() time.Time

	mu    sync.Mutex
	state map[string]accountState
}

type accountState struct {
	CheckedAt time.Time
	// Err is the last poll's failure, shown to readers; empty after a
	// successful read.
	Err string
}

func newCollector(accounts []Account, history *historyStore, now func() time.Time) *collector {
	sources := map[string]limitsSource{}
	for _, a := range accounts {
		switch a.Provider {
		case providerClaude:
			sources[a.ID] = newClaudeSource(a.KeychainService)
		case providerCodex:
			sources[a.ID] = codexSource{home: a.CodexHome}
		}
	}
	return &collector{accounts: accounts, sources: sources, history: history, now: now, state: map[string]accountState{}}
}

func (c *collector) run(ctx context.Context, interval time.Duration) {
	c.pollOnce(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.pollOnce(ctx)
		}
	}
}

func (c *collector) pollOnce(ctx context.Context) {
	for _, a := range c.accounts {
		src := c.sources[a.ID]
		if src == nil {
			continue
		}
		readCtx, cancel := context.WithTimeout(ctx, time.Minute)
		rl, err := src.Read(readCtx)
		cancel()
		st := accountState{CheckedAt: c.now().UTC()}
		if err != nil {
			st.Err = err.Error()
			log.Printf("account %s: %v", a.ID, err)
		} else if err := c.history.Append(a.ID, rl); err != nil {
			log.Printf("account %s: store snapshot: %v", a.ID, err)
		}
		c.mu.Lock()
		c.state[a.ID] = st
		c.mu.Unlock()
	}
}

func (c *collector) stateOf(accountID string) (accountState, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, ok := c.state[accountID]
	return st, ok
}
