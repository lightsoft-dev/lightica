package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeSession(t *testing.T, home, day, name string, mod time.Time, lines ...string) string {
	t.Helper()
	dir := filepath.Join(append([]string{home, "sessions"}, strings.Split(day, "/")...)...)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
	return p
}

const (
	codexMeta        = `{"timestamp":"2026-11-19T10:00:00.000Z","type":"session_meta","payload":{"id":"x"}}`
	codexWeekly6     = `{"timestamp":"2026-11-19T10:05:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{}},"rate_limits":{"limit_id":"codex","limit_name":null,"primary":{"used_percent":6.0,"window_minutes":10080,"resets_at":1795588819},"secondary":null,"credits":{"has_credits":true,"unlimited":false,"balance":"62500.0000000000"},"plan_type":"pro","rate_limit_reached_type":null}}}`
	codexModelBucket = `{"timestamp":"2026-11-19T10:06:00.000Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex_other_model","primary":{"used_percent":90.0,"window_minutes":300,"resets_at":1795588819}}}}`
	codexNoLimits    = `{"timestamp":"2026-11-19T10:07:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{},"rate_limits":null}}`
	codexTwoWindows  = `{"timestamp":"2026-11-19T12:00:00.000Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":40.5,"window_minutes":300,"resets_at":1795500000},"secondary":{"used_percent":17.0,"window_minutes":10080,"resets_at":1795588819},"credits":{"has_credits":false,"unlimited":false,"balance":"0"},"plan_type":"promax","rate_limit_reached_type":"primary"}}}`
)

func TestCodexReadsLatestMainBucketEvent(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	writeSession(t, home, "2026/11/19", "rollout-a.jsonl", now, codexMeta, codexWeekly6, codexModelBucket, codexNoLimits)

	rl, err := codexSource{home: home}.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rl.PlanType != "pro" || rl.Status != "" {
		t.Fatalf("plan/status = %q/%q", rl.PlanType, rl.Status)
	}
	if len(rl.Windows) != 1 {
		t.Fatalf("windows = %+v, want only the main bucket's weekly window", rl.Windows)
	}
	w := rl.Windows[0]
	if w.Kind != windowWeekly || *w.UsedPercent != 6 || w.WindowMinutes != 10080 || w.ResetsAt.Unix() != 1795588819 {
		t.Fatalf("window = %+v", w)
	}
	if rl.Credits == nil || !rl.Credits.HasCredits || rl.Credits.Balance != "62500.0000000000" {
		t.Fatalf("credits = %+v", rl.Credits)
	}
	if got := rl.ObservedAt.Format(time.RFC3339); got != "2026-11-19T10:05:00Z" {
		t.Fatalf("observed = %s", got)
	}
}

func TestCodexPicksNewestEventAcrossFiles(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	// The most recently written file holds the older event: a long session
	// filed under an earlier day can still be the one Codex wrote last.
	writeSession(t, home, "2026/11/19", "rollout-old.jsonl", now, codexWeekly6)
	writeSession(t, home, "2026/11/18", "rollout-new.jsonl", now.Add(-time.Minute), codexTwoWindows)

	rl, err := codexSource{home: home}.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rl.PlanType != "promax" || rl.Status != statusRejected {
		t.Fatalf("plan/status = %q/%q", rl.PlanType, rl.Status)
	}
	if len(rl.Windows) != 2 || rl.Windows[0].Kind != windowSession || rl.Windows[1].Kind != windowWeekly {
		t.Fatalf("windows = %+v, want session then weekly", rl.Windows)
	}
	if *rl.Windows[0].UsedPercent != 40.5 {
		t.Fatalf("session = %v", *rl.Windows[0].UsedPercent)
	}
}

func TestCodexFindsEventBeyondFirstTailWindow(t *testing.T) {
	home := t.TempDir()
	big := `{"timestamp":"2026-11-19T10:06:00.000Z","type":"response_item","payload":{"output":"` + strings.Repeat("x", codexTailBytes+1024) + `"}}`
	writeSession(t, home, "2026/11/19", "rollout-a.jsonl", time.Now(), codexWeekly6, big)

	rl, err := codexSource{home: home}.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rl.Windows) != 1 || *rl.Windows[0].UsedPercent != 6 {
		t.Fatalf("windows = %+v", rl.Windows)
	}
}

func TestCodexWithoutLimitData(t *testing.T) {
	home := t.TempDir()
	if _, err := (codexSource{home: home}).Read(context.Background()); !errors.Is(err, errNoCodexData) {
		t.Fatalf("missing sessions dir: err = %v", err)
	}
	writeSession(t, home, "2026/11/19", "rollout-a.jsonl", time.Now(), codexMeta, codexNoLimits, codexModelBucket)
	if _, err := (codexSource{home: home}).Read(context.Background()); !errors.Is(err, errNoCodexData) {
		t.Fatalf("no main-bucket event: err = %v", err)
	}
}
