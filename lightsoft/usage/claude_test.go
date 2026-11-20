package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeCredentials struct {
	cred claudeCredential
	err  error
}

func (f fakeCredentials) Credential(context.Context) (claudeCredential, error) { return f.cred, f.err }

// Trimmed from a real response: percent utilization, a null five_hour window
// (no session in progress), and fields this service ignores.
const claudeUsageBody = `{"five_hour":null,"seven_day":{"utilization":27.0,"resets_at":"2026-11-21T22:00:00.460432+00:00","limit_dollars":null,"locked_reason":null},"seven_day_opus":null,"seven_day_sonnet":{"utilization":3.5,"resets_at":"2026-11-21T22:00:00+00:00","locked_reason":"limit"},"extra_usage":{"is_enabled":false},"limits":[{"kind":"weekly_all","percent":27}]}`

const testAccessToken = "sk-ant-oat01-test-token"

func claudeTestSource(t *testing.T, status int, body string, cred claudeCredential) (claudeSource, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer "+testAccessToken {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("anthropic-beta"); got != "oauth-2025-04-20" {
			t.Errorf("anthropic-beta = %q", got)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	now := time.Date(2026, 11, 20, 6, 0, 0, 0, time.UTC)
	return claudeSource{
		creds:    fakeCredentials{cred: cred},
		http:     srv.Client(),
		endpoint: srv.URL,
		now:      func() time.Time { return now },
	}, &calls
}

func validCred() claudeCredential {
	return claudeCredential{
		AccessToken:      testAccessToken,
		ExpiresAt:        time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC),
		SubscriptionType: "max",
		RateLimitTier:    "default_claude_max_20x",
	}
}

func TestClaudeReadsUsageWindows(t *testing.T) {
	src, _ := claudeTestSource(t, http.StatusOK, claudeUsageBody, validCred())
	rl, err := src.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rl.PlanType != "max_20x" {
		t.Fatalf("plan = %q", rl.PlanType)
	}
	if len(rl.Windows) != 2 {
		t.Fatalf("windows = %+v, want weekly + weekly_sonnet (null windows dropped)", rl.Windows)
	}
	weekly, sonnet := rl.Windows[0], rl.Windows[1]
	if weekly.Kind != windowWeekly || *weekly.UsedPercent != 27 || weekly.WindowMinutes != weeklyWindowMinutes || weekly.Status != "" {
		t.Fatalf("weekly = %+v (utilization is already a percent)", weekly)
	}
	if got := weekly.ResetsAt.Format(time.RFC3339); got != "2026-11-21T22:00:00Z" {
		t.Fatalf("weekly reset = %s", got)
	}
	if sonnet.Kind != windowWeeklySonnet || sonnet.Status != statusRejected {
		t.Fatalf("sonnet = %+v, a locked window is rejected", sonnet)
	}
	if !rl.ObservedAt.Equal(src.now()) {
		t.Fatalf("observed = %s", rl.ObservedAt)
	}
}

func TestClaudeExpiredTokenIsNotSent(t *testing.T) {
	cred := validCred()
	cred.ExpiresAt = time.Date(2026, 11, 20, 6, 0, 30, 0, time.UTC)
	src, calls := claudeTestSource(t, http.StatusOK, claudeUsageBody, cred)
	if _, err := src.Read(context.Background()); !errors.Is(err, errClaudeTokenExpired) {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("an expiring token must not be sent (and is never refreshed here)")
	}
}

func TestClaudeErrorsNeverCarryTheToken(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusInternalServerError} {
		src, _ := claudeTestSource(t, status, `{"error":"x"}`, validCred())
		_, err := src.Read(context.Background())
		if err == nil {
			t.Fatalf("status %d: want an error", status)
		}
		if strings.Contains(err.Error(), testAccessToken) {
			t.Fatalf("status %d: error leaks the token: %v", status, err)
		}
	}
}

func TestParseClaudeCredential(t *testing.T) {
	c, err := parseClaudeCredential([]byte(`{"claudeAiOauth":{"accessToken":"tok","refreshToken":"r","expiresAt":1795600000000,"subscriptionType":"max","rateLimitTier":"default_claude_max_5x"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.AccessToken != "tok" || c.ExpiresAt.UnixMilli() != 1795600000000 || claudePlan(c) != "max_5x" {
		t.Fatalf("cred = %+v plan=%q", c, claudePlan(c))
	}
	if _, err := parseClaudeCredential([]byte(`{"other":{}}`)); !errors.Is(err, errClaudeNoCredential) {
		t.Fatalf("err = %v", err)
	}
	if got := claudePlan(claudeCredential{SubscriptionType: "Pro"}); got != "pro" {
		t.Fatalf("plan fallback = %q", got)
	}
}
