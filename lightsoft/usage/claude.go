package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Claude Code keeps no limit information on disk, so the account is read
// from the endpoint Claude Code's own /usage screen calls, with the
// subscription OAuth token Claude Code stores in the login keychain.
//
// The token is only ever read. It is never refreshed here: refreshing rotates
// the refresh token and would sign Claude Code out. When it has expired, the
// next Claude Code run on this Mac refreshes it and the read recovers.
// The token is never logged or returned.

const claudeUsageURL = "https://api.anthropic.com/api/oauth/usage"

var (
	errClaudeTokenExpired = errors.New("Claude 로그인 토큰이 만료됐습니다. 이 맥에서 Claude Code가 다음에 실행되면 다시 읽습니다.")
	errClaudeNoCredential = errors.New("키체인에서 Claude Code 로그인 정보를 찾지 못했습니다. 이 맥에서 Claude Code에 구독 계정으로 로그인해 주세요.")

	claudeTierPattern = regexp.MustCompile(`claude_(max|pro|team|enterprise)(?:_(\d+x))?$`)
)

type claudeCredential struct {
	AccessToken      string
	ExpiresAt        time.Time
	SubscriptionType string
	RateLimitTier    string
}

type claudeCredentialSource interface {
	Credential(ctx context.Context) (claudeCredential, error)
}

// keychainCredentialSource reads Claude Code's keychain item with
// /usr/bin/security, the same tool Claude Code uses, so the item's access
// list already allows it.
type keychainCredentialSource struct {
	service string
}

func (k keychainCredentialSource) Credential(ctx context.Context) (claudeCredential, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// Output() keeps stderr out of the error text; stdout (the credential) is
	// never logged.
	out, err := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", k.service, "-w").Output()
	if err != nil {
		return claudeCredential{}, errClaudeNoCredential
	}
	return parseClaudeCredential(out)
}

func parseClaudeCredential(raw []byte) (claudeCredential, error) {
	var item struct {
		OAuth *struct {
			AccessToken      string `json:"accessToken"`
			ExpiresAt        int64  `json:"expiresAt"`
			SubscriptionType string `json:"subscriptionType"`
			RateLimitTier    string `json:"rateLimitTier"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(raw, &item); err != nil || item.OAuth == nil || item.OAuth.AccessToken == "" {
		return claudeCredential{}, errClaudeNoCredential
	}
	c := claudeCredential{
		AccessToken:      item.OAuth.AccessToken,
		SubscriptionType: item.OAuth.SubscriptionType,
		RateLimitTier:    item.OAuth.RateLimitTier,
	}
	if item.OAuth.ExpiresAt > 0 {
		c.ExpiresAt = time.UnixMilli(item.OAuth.ExpiresAt).UTC()
	}
	return c, nil
}

type claudeSource struct {
	creds    claudeCredentialSource
	http     *http.Client
	endpoint string
	now      func() time.Time
}

func newClaudeSource(service string) claudeSource {
	return claudeSource{
		creds:    keychainCredentialSource{service: service},
		http:     &http.Client{Timeout: 20 * time.Second},
		endpoint: claudeUsageURL,
		now:      time.Now,
	}
}

type claudeUsageWindow struct {
	Utilization  *float64 `json:"utilization"`
	ResetsAt     *string  `json:"resets_at"`
	LockedReason *string  `json:"locked_reason"`
}

type claudeUsageResponse struct {
	FiveHour     *claudeUsageWindow `json:"five_hour"`
	SevenDay     *claudeUsageWindow `json:"seven_day"`
	SevenDayOpus *claudeUsageWindow `json:"seven_day_opus"`
	SevenDaySonn *claudeUsageWindow `json:"seven_day_sonnet"`
}

func (c claudeSource) Read(ctx context.Context) (RateLimits, error) {
	cred, err := c.creds.Credential(ctx)
	if err != nil {
		return RateLimits{}, err
	}
	now := c.now()
	if !cred.ExpiresAt.IsZero() && !cred.ExpiresAt.After(now.Add(time.Minute)) {
		return RateLimits{}, errClaudeTokenExpired
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return RateLimits{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "lightsoft-usage")
	resp, err := c.http.Do(req)
	if err != nil {
		// The transport error names the URL only, never the header.
		return RateLimits{}, fmt.Errorf("Claude 사용량 조회에 실패했습니다: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return RateLimits{}, errClaudeTokenExpired
	case resp.StatusCode == http.StatusTooManyRequests:
		return RateLimits{}, errors.New("Claude 사용량 조회가 잠시 제한됐습니다. 다음 주기에 다시 읽습니다.")
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return RateLimits{}, fmt.Errorf("Claude 사용량 조회가 실패했습니다 (HTTP %d).", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return RateLimits{}, err
	}
	return parseClaudeUsage(body, cred, now)
}

func parseClaudeUsage(body []byte, cred claudeCredential, observed time.Time) (RateLimits, error) {
	var u claudeUsageResponse
	if err := json.Unmarshal(body, &u); err != nil {
		return RateLimits{}, errors.New("Claude 사용량 응답을 읽지 못했습니다.")
	}
	rl := RateLimits{PlanType: claudePlan(cred), Windows: []Window{}, ObservedAt: observed.UTC()}
	for _, w := range []struct {
		src     *claudeUsageWindow
		kind    string
		minutes int64
	}{
		{u.FiveHour, windowSession, sessionWindowMinutes},
		{u.SevenDay, windowWeekly, weeklyWindowMinutes},
		{u.SevenDayOpus, windowWeeklyOpus, weeklyWindowMinutes},
		{u.SevenDaySonn, windowWeeklySonnet, weeklyWindowMinutes},
	} {
		if win, ok := claudeWindow(w.src, w.kind, w.minutes); ok {
			rl.Windows = append(rl.Windows, win)
		}
	}
	sortWindows(rl.Windows)
	return rl, nil
}

// claudeWindow converts one window. Unlike the stream-json event (a 0-1
// fraction), this endpoint reports utilization in percent. A null window is
// one the account has not used in its current period.
func claudeWindow(w *claudeUsageWindow, kind string, minutes int64) (Window, bool) {
	if w == nil {
		return Window{}, false
	}
	out := Window{Kind: kind, WindowMinutes: minutes}
	if w.Utilization != nil {
		out.UsedPercent = finitePercent(*w.Utilization)
	}
	if w.ResetsAt != nil {
		if t, err := time.Parse(time.RFC3339Nano, *w.ResetsAt); err == nil {
			t = t.UTC()
			out.ResetsAt = &t
		}
	}
	if w.LockedReason != nil && *w.LockedReason != "" {
		out.Status = statusRejected
	}
	if out.UsedPercent == nil && out.ResetsAt == nil && out.Status == "" {
		return Window{}, false
	}
	return out, true
}

// claudePlan names the plan the way the runtime pages print plan_type
// ("max_20x" → "Max 20x").
func claudePlan(c claudeCredential) string {
	if m := claudeTierPattern.FindStringSubmatch(c.RateLimitTier); m != nil {
		if m[2] != "" {
			return m[1] + "_" + m[2]
		}
		return m[1]
	}
	return strings.ToLower(c.SubscriptionType)
}
