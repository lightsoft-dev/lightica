package main

import (
	"math"
	"sort"
	"time"
)

// RateLimits mirrors the snapshot upstream Multica stores per runtime
// (server/pkg/agent/rate_limits.go, MUL-9427), so the team client renders it
// with the same helpers as the runtime pages
// (packages/core/runtimes/rate-limits.ts).
type RateLimits struct {
	PlanType string   `json:"plan_type"`
	Status   string   `json:"status"`
	Windows  []Window `json:"windows"`
	Credits  *Credits `json:"credits,omitempty"`
	// ObservedAt is when the provider reported these numbers: the Codex log
	// line's time, or the moment the Claude usage request answered.
	ObservedAt time.Time `json:"observed_at"`
}

type Window struct {
	Kind string `json:"kind"`
	// UsedPercent is 0-100 and can run slightly past 100.
	UsedPercent   *float64   `json:"used_percent,omitempty"`
	WindowMinutes int64      `json:"window_minutes"`
	ResetsAt      *time.Time `json:"resets_at,omitempty"`
	Status        string     `json:"status"`
}

type Credits struct {
	HasCredits bool   `json:"has_credits"`
	Unlimited  bool   `json:"unlimited"`
	Balance    string `json:"balance,omitempty"`
}

// Window kinds and statuses, as upstream names them. The UI labels windows by
// kind, so provider window names are mapped onto these.
const (
	windowSession      = "session"
	windowWeekly       = "weekly"
	windowWeeklyOpus   = "weekly_opus"
	windowWeeklySonnet = "weekly_sonnet"
	windowOther        = "other"

	statusRejected = "rejected"

	sessionWindowMinutes = 5 * 60
	weeklyWindowMinutes  = 7 * 24 * 60
)

var windowOrder = map[string]int{
	windowSession:      0,
	windowWeekly:       1,
	windowWeeklyOpus:   2,
	windowWeeklySonnet: 3,
	windowOther:        4,
}

func sortWindows(ws []Window) {
	sort.SliceStable(ws, func(i, j int) bool {
		a, b := ws[i], ws[j]
		if windowOrder[a.Kind] != windowOrder[b.Kind] {
			return windowOrder[a.Kind] < windowOrder[b.Kind]
		}
		return a.WindowMinutes < b.WindowMinutes
	})
}

func finitePercent(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return nil
	}
	return &v
}

func unixSeconds(sec *int64) *time.Time {
	if sec == nil || *sec <= 0 {
		return nil
	}
	t := time.Unix(*sec, 0).UTC()
	return &t
}
