package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	wsID    = "11111111-1111-4111-8111-111111111111"
	otherWS = "22222222-2222-4222-8222-222222222222"
	alice   = "a0000000-0000-4000-8000-000000000001" // member
	erin    = "e0000000-0000-4000-8000-000000000005" // not in the workspace
	rtA     = "aa000000-0000-4000-8000-00000000000a"
)

func session(user string) string { return "eyJhbGciOiJIUzI1NiJ9." + user + ".sig" }

// fakeMultica answers /api/me and the member list the way production does.
func fakeMultica(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !strings.HasPrefix(token, "eyJ") {
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		user := strings.Split(token, ".")[1]
		switch {
		case r.URL.Path == "/api/me":
			_ = json.NewEncoder(w).Encode(apiUser{ID: user})
		case strings.HasPrefix(r.URL.Path, "/api/workspaces/") && strings.HasSuffix(r.URL.Path, "/members"):
			if user != alice {
				http.Error(w, `{"error":"not a member"}`, http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode([]apiMember{{UserID: alice, Role: "member"}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type stubSource struct {
	rl  RateLimits
	err error
}

func (s stubSource) Read(context.Context) (RateLimits, error) { return s.rl, s.err }

var testNow = time.Date(2026, 11, 20, 6, 30, 0, 0, time.UTC)

func pct(v float64) *float64 { return &v }

func testServer(t *testing.T) (*server, http.Handler) {
	t.Helper()
	now := func() time.Time { return testNow }
	hist, err := openHistory(filepath.Join(t.TempDir(), "history.jsonl"), 35*24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	accounts := AccountsFile{
		WorkspaceID: wsID,
		Accounts: []Account{
			{ID: "claude-max", Name: "Claude Max", Provider: providerClaude, RuntimeIDs: []string{rtA}},
			{ID: "codex-main", Name: "Codex", Provider: providerCodex, CodexHome: "/nonexistent", RuntimeIDs: []string{}},
		},
	}
	col := newCollector(accounts.Accounts, hist, now)
	reset := testNow.Add(40 * time.Hour)
	col.sources = map[string]limitsSource{
		"claude-max": stubSource{rl: RateLimits{PlanType: "max_20x", ObservedAt: testNow, Windows: []Window{{Kind: windowWeekly, UsedPercent: pct(27), WindowMinutes: weeklyWindowMinutes, ResetsAt: &reset}}}},
		"codex-main": stubSource{err: errNoCodexData},
	}
	col.pollOnce(context.Background())

	s := &server{
		cfg:       Config{Interval: 5 * time.Minute, AllowedOrigins: map[string]bool{"http://localhost:13100": true}},
		api:       newMulticaClient(fakeMultica(t).URL),
		accounts:  accounts,
		collector: col,
		history:   hist,
		now:       now,
	}
	return s, s.routes()
}

func get(t *testing.T, h http.Handler, path string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func withCookie(user string) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: authCookieName, Value: session(user)}) }
}

func TestLimitsForWorkspaceMember(t *testing.T) {
	_, h := testServer(t)
	rec := get(t, h, PathPrefix+"/api/w/"+wsID+"/limits", withCookie(alice))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var body limitsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.IntervalSeconds != 300 || len(body.Accounts) != 2 {
		t.Fatalf("body = %+v", body)
	}
	claude, codex := body.Accounts[0], body.Accounts[1]
	if claude.RateLimits == nil || claude.RateLimits.PlanType != "max_20x" || claude.Error != nil {
		t.Fatalf("claude = %+v", claude)
	}
	if len(claude.History) != 1 || len(claude.History[0].Points) != 1 || claude.History[0].Points[0].UsedPercent != 27 {
		t.Fatalf("claude history = %+v", claude.History)
	}
	if codex.RateLimits != nil || codex.Error == nil || *codex.Error != errNoCodexData.Error() || codex.CheckedAt == nil {
		t.Fatalf("codex = %+v", codex)
	}
}

func TestLimitsBearerSessionForLightica(t *testing.T) {
	_, h := testServer(t)
	rec := get(t, h, PathPrefix+"/api/w/"+wsID+"/limits", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+session(alice))
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestLimitsRefusals(t *testing.T) {
	_, h := testServer(t)
	cases := []struct {
		name   string
		path   string
		mutate func(*http.Request)
		want   int
	}{
		{"no session", "/api/w/" + wsID + "/limits", nil, http.StatusUnauthorized},
		{"machine token", "/api/w/" + wsID + "/limits", func(r *http.Request) { r.Header.Set("Authorization", "Bearer mat_task") }, http.StatusUnauthorized},
		{"not a member", "/api/w/" + wsID + "/limits", withCookie(erin), http.StatusForbidden},
		{"bad workspace id", "/api/w/nope/limits", withCookie(alice), http.StatusBadRequest},
		{"bad days", "/api/w/" + wsID + "/limits?days=90", withCookie(alice), http.StatusBadRequest},
	}
	for _, c := range cases {
		rec := get(t, h, PathPrefix+c.path, c.mutate)
		if rec.Code != c.want {
			t.Errorf("%s: status %d, want %d (%s)", c.name, rec.Code, c.want, rec.Body)
		}
	}
}

func TestLimitsOtherWorkspaceSeesNoAccounts(t *testing.T) {
	_, h := testServer(t)
	rec := get(t, h, PathPrefix+"/api/w/"+otherWS+"/limits", withCookie(alice))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body limitsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Accounts) != 0 {
		t.Fatalf("accounts = %+v", body.Accounts)
	}
}

func TestFailedReadKeepsLastSnapshot(t *testing.T) {
	s, h := testServer(t)
	s.collector.sources["claude-max"] = stubSource{err: errClaudeTokenExpired}
	s.collector.pollOnce(context.Background())

	rec := get(t, h, PathPrefix+"/api/w/"+wsID+"/limits", withCookie(alice))
	var body limitsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	claude := body.Accounts[0]
	if claude.RateLimits == nil || *claude.RateLimits.Windows[0].UsedPercent != 27 {
		t.Fatalf("last snapshot lost: %+v", claude.RateLimits)
	}
	if claude.Error == nil || *claude.Error != errClaudeTokenExpired.Error() {
		t.Fatalf("error = %v", claude.Error)
	}
}

func TestHistoryDedupesRetainsAndBuckets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	now := testNow
	h, err := openHistory(path, 7*24*time.Hour, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	snap := func(at time.Time, used float64) RateLimits {
		return RateLimits{ObservedAt: at, Windows: []Window{{Kind: windowWeekly, UsedPercent: pct(used), WindowMinutes: weeklyWindowMinutes}}}
	}
	old := now.Add(-8 * 24 * time.Hour)
	for _, rl := range []RateLimits{
		snap(old, 1),
		snap(now.Add(-50*time.Minute).Truncate(time.Hour).Add(5*time.Minute), 10),
		snap(now.Add(-50*time.Minute).Truncate(time.Hour).Add(20*time.Minute), 12),
		snap(now.Add(-50*time.Minute).Truncate(time.Hour).Add(20*time.Minute), 99), // same observation again: skipped
		snap(now, 14),
	} {
		if err := h.Append("a", rl); err != nil {
			t.Fatal(err)
		}
	}
	series := h.Series("a", now.Add(-7*24*time.Hour))
	if len(series) != 1 {
		t.Fatalf("series = %+v", series)
	}
	var got []float64
	for _, p := range series[0].Points {
		got = append(got, p.UsedPercent)
	}
	if len(got) != 2 || got[0] != 12 || got[1] != 14 {
		t.Fatalf("points = %v, want hourly max [12 14] without the expired or repeated record", got)
	}

	// Reopening drops the expired line from the file.
	h2, err := openHistory(path, 7*24*time.Hour, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Count(string(raw), "\n") != 3 || h2.Latest("a") == nil || *h2.Latest("a").Windows[0].UsedPercent != 14 {
		t.Fatalf("file after reopen:\n%s", raw)
	}
}

func TestAccountsValidation(t *testing.T) {
	ok := AccountsFile{WorkspaceID: strings.ToUpper(wsID), Accounts: []Account{
		{ID: "claude-max", Provider: providerClaude},
		{ID: "codex-main", Provider: providerCodex, CodexHome: "~/.codex", RuntimeIDs: []string{rtA}},
	}}
	if err := ok.normalize("/Users/z"); err != nil {
		t.Fatal(err)
	}
	if ok.WorkspaceID != wsID || ok.Accounts[0].KeychainService != defaultClaudeKeychainService ||
		ok.Accounts[0].Name != "claude-max" || ok.Accounts[1].CodexHome != "/Users/z/.codex" {
		t.Fatalf("normalized = %+v", ok)
	}
	for name, f := range map[string]AccountsFile{
		"workspace":    {WorkspaceID: "x"},
		"duplicate":    {WorkspaceID: wsID, Accounts: []Account{{ID: "a", Provider: providerClaude}, {ID: "a", Provider: providerClaude}}},
		"codex home":   {WorkspaceID: wsID, Accounts: []Account{{ID: "a", Provider: providerCodex}}},
		"provider":     {WorkspaceID: wsID, Accounts: []Account{{ID: "a", Provider: "grok"}}},
		"runtime uuid": {WorkspaceID: wsID, Accounts: []Account{{ID: "a", Provider: providerClaude, RuntimeIDs: []string{"rt"}}}},
	} {
		if err := f.normalize("/Users/z"); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
