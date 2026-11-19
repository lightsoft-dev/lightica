package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// historyStore keeps every snapshot in an append-only JSON-lines file for the
// trend chart. Volumes are small (one line per account per poll), so the
// whole retention window lives in memory and the file is rewritten only to
// drop expired lines.
type historyStore struct {
	path      string
	retention time.Duration
	now       func() time.Time

	mu        sync.Mutex
	records   []historyRecord // ascending ObservedAt per account
	compacted time.Time
}

type historyRecord struct {
	AccountID string `json:"account_id"`
	RateLimits
}

// HistorySeries is one window kind's usage over time, bucketed by hour.
type HistorySeries struct {
	Kind          string         `json:"kind"`
	WindowMinutes int64          `json:"window_minutes"`
	Points        []HistoryPoint `json:"points"`
}

type HistoryPoint struct {
	T           time.Time `json:"t"`
	UsedPercent float64   `json:"used_percent"`
}

func openHistory(path string, retention time.Duration, now func() time.Time) (*historyStore, error) {
	h := &historyStore{path: path, retention: retention, now: now}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return h, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var r historyRecord
		// A torn last line (crash mid-write) is skipped, not fatal.
		if json.Unmarshal(sc.Bytes(), &r) == nil && r.AccountID != "" && !r.ObservedAt.IsZero() {
			h.records = append(h.records, r)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	sort.SliceStable(h.records, func(i, j int) bool { return h.records[i].ObservedAt.Before(h.records[j].ObservedAt) })
	if err := h.compactLocked(); err != nil {
		return nil, err
	}
	return h, nil
}

// Append stores a snapshot unless it repeats the account's latest one (a
// Codex log that has not moved since the last poll).
func (h *historyStore) Append(accountID string, rl RateLimits) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if last := h.latestLocked(accountID); last != nil && !rl.ObservedAt.After(last.ObservedAt) {
		return nil
	}
	rec := historyRecord{AccountID: accountID, RateLimits: rl}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(line, '\n'))
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	if cerr != nil {
		return cerr
	}
	h.records = append(h.records, rec)
	if h.now().Sub(h.compacted) > 24*time.Hour {
		return h.compactLocked()
	}
	return nil
}

func (h *historyStore) Latest(accountID string) *RateLimits {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.latestLocked(accountID)
}

func (h *historyStore) latestLocked(accountID string) *RateLimits {
	for i := len(h.records) - 1; i >= 0; i-- {
		if h.records[i].AccountID == accountID {
			rl := h.records[i].RateLimits
			return &rl
		}
	}
	return nil
}

// Series returns the account's windows since `since`, one point per hour:
// the highest share seen in that hour, so a burst between polls is not
// averaged away.
func (h *historyStore) Series(accountID string, since time.Time) []HistorySeries {
	h.mu.Lock()
	defer h.mu.Unlock()
	type key struct {
		kind    string
		minutes int64
	}
	byKey := map[key]map[time.Time]float64{}
	var order []key
	for _, r := range h.records {
		if r.AccountID != accountID || r.ObservedAt.Before(since) {
			continue
		}
		hour := r.ObservedAt.UTC().Truncate(time.Hour)
		for _, w := range r.Windows {
			if w.UsedPercent == nil {
				continue
			}
			k := key{w.Kind, w.WindowMinutes}
			points, ok := byKey[k]
			if !ok {
				points = map[time.Time]float64{}
				byKey[k] = points
				order = append(order, k)
			}
			if v, seen := points[hour]; !seen || *w.UsedPercent > v {
				points[hour] = *w.UsedPercent
			}
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		if windowOrder[order[i].kind] != windowOrder[order[j].kind] {
			return windowOrder[order[i].kind] < windowOrder[order[j].kind]
		}
		return order[i].minutes < order[j].minutes
	})
	out := make([]HistorySeries, 0, len(order))
	for _, k := range order {
		s := HistorySeries{Kind: k.kind, WindowMinutes: k.minutes, Points: make([]HistoryPoint, 0, len(byKey[k]))}
		for t, v := range byKey[k] {
			s.Points = append(s.Points, HistoryPoint{T: t, UsedPercent: v})
		}
		sort.Slice(s.Points, func(i, j int) bool { return s.Points[i].T.Before(s.Points[j].T) })
		out = append(out, s)
	}
	return out
}

// compactLocked drops records past retention and rewrites the file when it
// dropped any.
func (h *historyStore) compactLocked() error {
	now := h.now()
	h.compacted = now
	cutoff := now.Add(-h.retention)
	kept := h.records[:0]
	for _, r := range h.records {
		if !r.ObservedAt.Before(cutoff) {
			kept = append(kept, r)
		}
	}
	if len(kept) == len(h.records) {
		return nil
	}
	h.records = kept
	tmp := h.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, r := range h.records {
		line, err := json.Marshal(r)
		if err != nil {
			f.Close()
			return err
		}
		w.Write(line)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, h.path)
}
