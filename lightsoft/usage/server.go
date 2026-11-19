package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHistoryDays = 7
	maxHistoryDays     = 30
)

type server struct {
	cfg       Config
	api       *multicaClient
	accounts  AccountsFile
	collector *collector
	history   *historyStore
	now       func() time.Time
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+PathPrefix+"/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("GET "+PathPrefix+"/api/w/{ws}/limits", s.handleLimits)
	return s.withCORS(securityHeaders(logRequests(mux)))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// withCORS only matters for local web development, where the team client
// (:13100) and this service run on different ports. On :3444 both share one
// origin and browsers never send a preflight.
func (s *server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && s.cfg.AllowedOrigins[origin] {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET")
				h.Set("Access-Control-Allow-Headers", "Authorization")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// logRequests logs method, path and status only; headers carry the session.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if r.URL.Path != PathPrefix+"/health" {
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
		}
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// isSessionToken reports whether a credential looks like a login session
// JWT. Agent task tokens (mat_), personal access tokens (mul_) and other
// machine credentials are refused: the page is for people.
func isSessionToken(v string) bool {
	return strings.HasPrefix(v, "eyJ") && strings.Count(v, ".") == 2
}

// sessionCredential returns a person's login session: the team client's
// cookie, or the bearer session token Lightica (desktop) holds.
func sessionCredential(r *http.Request) (string, bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		return token, strings.HasPrefix(h, "Bearer ") && isSessionToken(token)
	}
	c, err := r.Cookie(authCookieName)
	if err != nil {
		return "", false
	}
	return c.Value, isSessionToken(c.Value)
}

type limitsResponse struct {
	IntervalSeconds int            `json:"interval_seconds"`
	Accounts        []accountLimit `json:"accounts"`
}

type accountLimit struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Provider   string   `json:"provider"`
	RuntimeIDs []string `json:"runtime_ids"`
	// RateLimits is the latest stored snapshot, kept when a later read fails.
	RateLimits *RateLimits     `json:"rate_limits"`
	CheckedAt  *time.Time      `json:"checked_at"`
	Error      *string         `json:"error"`
	History    []HistorySeries `json:"history"`
}

func (s *server) handleLimits(w http.ResponseWriter, r *http.Request) {
	wsID := strings.ToLower(r.PathValue("ws"))
	if !uuidPattern.MatchString(wsID) {
		writeError(w, http.StatusBadRequest, "워크스페이스 ID가 올바르지 않습니다.")
		return
	}
	days := defaultHistoryDays
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxHistoryDays {
			writeError(w, http.StatusBadRequest, "days는 1~30 사이여야 합니다.")
			return
		}
		days = n
	}
	token, ok := sessionCredential(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "Multica에 로그인한 브라우저나 Lightica에서 열어 주세요.")
		return
	}
	ctx := r.Context()
	user, err := s.api.me(ctx, token)
	if err != nil {
		s.writeAPIFailure(w, "me", err)
		return
	}
	members, err := s.api.members(ctx, token, wsID)
	if err != nil {
		s.writeAPIFailure(w, "members", err)
		return
	}
	member := false
	for _, m := range members {
		if m.UserID == user.ID {
			member = true
			break
		}
	}
	if !member {
		writeError(w, http.StatusForbidden, "이 워크스페이스의 구성원이 아닙니다.")
		return
	}

	resp := limitsResponse{IntervalSeconds: int(s.cfg.Interval / time.Second), Accounts: []accountLimit{}}
	// The accounts belong to one workspace; members of another see none.
	if wsID == s.accounts.WorkspaceID {
		since := s.now().Add(-time.Duration(days) * 24 * time.Hour)
		for _, a := range s.accounts.Accounts {
			out := accountLimit{
				ID:         a.ID,
				Name:       a.Name,
				Provider:   a.Provider,
				RuntimeIDs: a.RuntimeIDs,
				RateLimits: s.history.Latest(a.ID),
				History:    s.history.Series(a.ID, since),
			}
			if st, ok := s.collector.stateOf(a.ID); ok {
				checked := st.CheckedAt
				out.CheckedAt = &checked
				if st.Err != "" {
					msg := st.Err
					out.Error = &msg
				}
			}
			resp.Accounts = append(resp.Accounts, out)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) writeAPIFailure(w http.ResponseWriter, where string, err error) {
	switch {
	case errors.Is(err, errUnauthenticated):
		writeError(w, http.StatusUnauthorized, "Multica 로그인이 필요합니다.")
	case errors.Is(err, errForbidden):
		writeError(w, http.StatusForbidden, "이 워크스페이스에 접근할 수 없습니다.")
	default:
		log.Printf("%s: %v", where, err)
		writeError(w, http.StatusBadGateway, "Multica API에 연결하지 못했습니다.")
	}
}
