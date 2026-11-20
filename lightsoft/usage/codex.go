package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Codex writes every session to $CODEX_HOME/sessions/YYYY/MM/DD/*.jsonl, and
// each turn's `token_count` event carries the account's rate limits. Reading
// the newest one costs no request and no token, at the price of only moving
// while Codex runs — which is also when the numbers change.

const (
	// codexDayDirs bounds the scan to recent session folders. A session is
	// filed under the day it started, so a long one is still found as long as
	// it started within this many days.
	codexDayDirs = 14
	// codexCandidateFiles is how many of the most recently written session
	// files are searched for the latest rate-limit event.
	codexCandidateFiles = 5
	// codexTailBytes / codexMaxTailBytes: token_count events follow every
	// turn, so the tail normally holds one; a turn that wrote huge tool output
	// may push it further back.
	codexTailBytes    = 1 << 20
	codexMaxTailBytes = 16 << 20
	// codexMainLimitID is the bucket the plan's session/weekly windows belong
	// to. Model-specific buckets carry other ids and are not shown (same rule
	// as upstream's daemon).
	codexMainLimitID = "codex"
)

var errNoCodexData = errors.New("최근 Codex 세션 기록에 한도 정보가 없습니다. Codex가 다음에 실행되면 표시됩니다.")

type codexSource struct {
	home string
}

type codexLogLine struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type       string              `json:"type"`
		RateLimits *codexLogRateLimits `json:"rate_limits"`
	} `json:"payload"`
}

type codexLogRateLimits struct {
	LimitID   *string         `json:"limit_id"`
	Primary   *codexLogWindow `json:"primary"`
	Secondary *codexLogWindow `json:"secondary"`
	Credits   *struct {
		HasCredits bool    `json:"has_credits"`
		Unlimited  bool    `json:"unlimited"`
		Balance    *string `json:"balance"`
	} `json:"credits"`
	PlanType             *string `json:"plan_type"`
	RateLimitReachedType *string `json:"rate_limit_reached_type"`
}

type codexLogWindow struct {
	UsedPercent   *float64 `json:"used_percent"`
	WindowMinutes *int64   `json:"window_minutes"`
	ResetsAt      *int64   `json:"resets_at"`
}

func (c codexSource) Read(ctx context.Context) (RateLimits, error) {
	files, err := recentCodexSessionFiles(filepath.Join(c.home, "sessions"))
	if err != nil {
		return RateLimits{}, err
	}
	var best *RateLimits
	for _, f := range files {
		if ctx.Err() != nil {
			return RateLimits{}, ctx.Err()
		}
		rl, ok, err := lastCodexRateLimits(f)
		if err != nil || !ok {
			continue
		}
		if best == nil || rl.ObservedAt.After(best.ObservedAt) {
			r := rl
			best = &r
		}
	}
	if best == nil {
		return RateLimits{}, errNoCodexData
	}
	return *best, nil
}

// recentCodexSessionFiles returns the most recently written session files
// under the newest day folders, newest first.
func recentCodexSessionFiles(root string) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errNoCodexData
		}
		return nil, err
	}
	var days []string
	for _, y := range sortedDirsDesc(root) {
		for _, m := range sortedDirsDesc(y) {
			for _, d := range sortedDirsDesc(m) {
				days = append(days, d)
				if len(days) == codexDayDirs {
					break
				}
			}
			if len(days) == codexDayDirs {
				break
			}
		}
		if len(days) == codexDayDirs {
			break
		}
	}
	type file struct {
		path string
		mod  time.Time
	}
	var files []file
	for _, d := range days {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			files = append(files, file{filepath.Join(d, e.Name()), info.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	if len(files) > codexCandidateFiles {
		files = files[:codexCandidateFiles]
	}
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.path
	}
	return out, nil
}

// sortedDirsDesc lists the subdirectories of dir, newest name first. Codex
// names them by zero-padded date parts, so name order is date order.
func sortedDirsDesc(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}

// lastCodexRateLimits finds the newest main-bucket rate-limit event in a
// session file, reading only its tail.
func lastCodexRateLimits(path string) (RateLimits, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return RateLimits{}, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return RateLimits{}, false, err
	}
	for size := int64(codexTailBytes); ; size *= 4 {
		if size > info.Size() {
			size = info.Size()
		}
		buf := make([]byte, size)
		if _, err := f.ReadAt(buf, info.Size()-size); err != nil && !errors.Is(err, io.EOF) {
			return RateLimits{}, false, err
		}
		lines := bytes.Split(buf, []byte("\n"))
		if size < info.Size() {
			// The first line was cut by the window.
			lines = lines[1:]
		}
		for i := len(lines) - 1; i >= 0; i-- {
			if rl, ok := parseCodexLogLine(lines[i]); ok {
				return rl, true, nil
			}
		}
		if size == info.Size() || size >= codexMaxTailBytes {
			return RateLimits{}, false, nil
		}
	}
}

func parseCodexLogLine(line []byte) (RateLimits, bool) {
	if !bytes.Contains(line, []byte(`"rate_limits"`)) {
		return RateLimits{}, false
	}
	var l codexLogLine
	if err := json.Unmarshal(line, &l); err != nil {
		return RateLimits{}, false
	}
	if l.Type != "event_msg" || l.Payload.Type != "token_count" || l.Payload.RateLimits == nil {
		return RateLimits{}, false
	}
	observed, err := time.Parse(time.RFC3339Nano, l.Timestamp)
	if err != nil {
		return RateLimits{}, false
	}
	src := l.Payload.RateLimits
	if src.LimitID != nil && *src.LimitID != "" && *src.LimitID != codexMainLimitID {
		return RateLimits{}, false
	}
	rl := RateLimits{Windows: []Window{}, ObservedAt: observed.UTC()}
	if src.PlanType != nil {
		rl.PlanType = *src.PlanType
	}
	if src.RateLimitReachedType != nil && *src.RateLimitReachedType != "" {
		rl.Status = statusRejected
	}
	if w, ok := codexWindow(src.Primary, windowSession); ok {
		rl.Windows = append(rl.Windows, w)
	}
	if w, ok := codexWindow(src.Secondary, windowWeekly); ok {
		rl.Windows = append(rl.Windows, w)
	}
	sortWindows(rl.Windows)
	if src.Credits != nil {
		rl.Credits = &Credits{HasCredits: src.Credits.HasCredits, Unlimited: src.Credits.Unlimited}
		if src.Credits.Balance != nil {
			rl.Credits.Balance = *src.Credits.Balance
		}
	}
	if len(rl.Windows) == 0 && rl.Credits == nil {
		return RateLimits{}, false
	}
	return rl, true
}

// codexWindow labels a window by its length, like upstream's daemon: the
// primary/secondary slots change meaning between plans (Pro reports only the
// weekly window, as primary).
func codexWindow(w *codexLogWindow, fallback string) (Window, bool) {
	if w == nil || w.UsedPercent == nil {
		return Window{}, false
	}
	used := finitePercent(*w.UsedPercent)
	if used == nil {
		return Window{}, false
	}
	var minutes int64
	if w.WindowMinutes != nil {
		minutes = *w.WindowMinutes
	}
	kind := fallback
	switch {
	case minutes <= 0:
	case minutes <= 24*60:
		kind = windowSession
	case minutes >= 6*24*60 && minutes <= 8*24*60:
		kind = windowWeekly
	default:
		kind = windowOther
	}
	return Window{Kind: kind, UsedPercent: used, WindowMinutes: minutes, ResetsAt: unixSeconds(w.ResetsAt)}, true
}
