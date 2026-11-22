package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	maxValueBytes       = 64 * 1024
	maxDescriptionRunes = 1000
	maxBodyBytes        = 256 * 1024
	// csrfHeader must accompany every state-changing browser request. A
	// cross-site form cannot set it, and a cross-site fetch that sets it needs
	// a CORS preflight this service only answers for allowed origins.
	csrfHeader = "X-Lightsoft-Secrets"
	// envToken and envURL are the custom_env keys an enrolled agent receives.
	envToken = "LIGHTSOFT_SECRETS_TOKEN"
	envURL   = "LIGHTSOFT_SECRETS_URL"
	// upstreamKeepSentinel is the value PUT /api/agents/{id}/env reads as
	// "keep the existing value for this key".
	upstreamKeepSentinel = "****"
)

var (
	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	namePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)
)

type server struct {
	cfg   Config
	api   *multicaClient
	store *store
	box   *sealer
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+PathPrefix+"/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok\n")
	})

	w := PathPrefix + "/api/w/{ws}"
	mux.HandleFunc("GET "+w+"/secrets", s.human(s.handleList))
	mux.HandleFunc("POST "+w+"/secrets", s.human(s.handleCreate))
	mux.HandleFunc("PATCH "+w+"/secrets/{id}", s.human(s.handleUpdate))
	mux.HandleFunc("DELETE "+w+"/secrets/{id}", s.human(s.handleDelete))
	mux.HandleFunc("PUT "+w+"/secrets/{id}/grants", s.human(s.handleGrants))
	mux.HandleFunc("POST "+w+"/secrets/{id}/reveal", s.human(s.handleReveal))
	mux.HandleFunc("GET "+w+"/agents", s.human(s.handleEnrollments))
	mux.HandleFunc("POST "+w+"/agents/{agent}/enroll", s.human(s.handleEnroll))
	mux.HandleFunc("DELETE "+w+"/agents/{agent}/enroll", s.human(s.handleUnenroll))
	mux.HandleFunc("GET "+w+"/audit", s.human(s.handleAudit))
	mux.HandleFunc("GET "+w+"/audit/agents", s.human(s.handleAgentSummaries))

	mux.HandleFunc("GET "+PathPrefix+"/v1/secrets", s.agentOnly(s.handleAgentList))
	mux.HandleFunc("GET "+PathPrefix+"/v1/secrets/{name}", s.agentOnly(s.handleAgentRead))

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
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, "+csrfHeader)
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

// logRequests logs method, path and status only. Bodies and headers carry
// values and tokens and are never logged.
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

type apiError struct {
	status int
	msg    string
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, e *apiError) {
	writeJSON(w, e.status, map[string]string{"error": e.msg})
}

func internalError(where string, err error) *apiError {
	log.Printf("%s: %v", where, err)
	return &apiError{http.StatusInternalServerError, "서버 오류가 발생했습니다. 잠시 후 다시 시도해 주세요."}
}

// apiFailure maps an error from the production API onto a response.
func apiFailure(where string, err error) *apiError {
	switch {
	case errors.Is(err, errUnauthenticated):
		return &apiError{http.StatusUnauthorized, "Multica 로그인이 필요합니다."}
	case errors.Is(err, errForbidden), errors.Is(err, errNotFound):
		return &apiError{http.StatusForbidden, "이 워크스페이스에 접근할 수 없습니다."}
	default:
		log.Printf("%s: %v", where, err)
		return &apiError{http.StatusBadGateway, "Multica API에 연결하지 못했습니다."}
	}
}

func decodeBody(r *http.Request, v any) *apiError {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return &apiError{http.StatusBadRequest, "요청 형식이 올바르지 않습니다."}
	}
	return nil
}

func clientMeta(r *http.Request) (tailscaleLogin, remote string) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return r.Header.Get("Tailscale-User-Login"), host
}

func strPtr(s string) *string { return &s }

// ── human callers ──────────────────────────────────────────────────────────

// viewer is a person signed in to the team client, resolved against the
// production API on every request.
type viewer struct {
	user        apiUser
	role        string
	token       string
	workspaceID string
	members     []apiMember
	// squadCache memoizes squad membership lookups for one request.
	squadCache map[string][]apiSquadMember
}

func (v *viewer) isAdmin() bool { return v.role == "owner" || v.role == "admin" }

// isSessionToken reports whether a cookie value looks like a login session
// JWT. Agent task tokens (mat_), personal access tokens (mul_) and other
// machine credentials are rejected so they cannot pose as a person here.
func isSessionToken(v string) bool {
	return strings.HasPrefix(v, "eyJ") && strings.Count(v, ".") == 2
}

// sessionCredential returns a person's login session: the team client's
// cookie, or the bearer session token Lightica (desktop) holds. Either way it
// must be a session JWT; machine credentials are refused.
func sessionCredential(r *http.Request) (token string, fromCookie, ok bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		return token, false, strings.HasPrefix(h, "Bearer ") && isSessionToken(token)
	}
	c, err := r.Cookie(authCookieName)
	if err != nil {
		return "", true, false
	}
	return c.Value, true, isSessionToken(c.Value)
}

func (s *server) human(fn func(http.ResponseWriter, *http.Request, *viewer)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wsID := r.PathValue("ws")
		if !uuidPattern.MatchString(wsID) {
			writeError(w, &apiError{http.StatusBadRequest, "워크스페이스 ID가 올바르지 않습니다."})
			return
		}
		token, fromCookie, ok := sessionCredential(r)
		if !ok {
			writeError(w, &apiError{http.StatusUnauthorized, "Multica에 로그인한 브라우저나 Lightica에서 열어 주세요."})
			return
		}
		// Only the cookie is sent by the browser on its own, so only cookie
		// writes need the CSRF guard. A bearer token (Lightica) has to be
		// attached by the caller deliberately.
		if fromCookie && r.Method != http.MethodGet {
			if r.Header.Get(csrfHeader) != "1" || !s.cfg.AllowedOrigins[r.Header.Get("Origin")] {
				writeError(w, &apiError{http.StatusForbidden, "허용되지 않은 요청입니다. 팀 클라이언트에서 다시 시도해 주세요."})
				return
			}
		}
		ctx := r.Context()
		user, err := s.api.me(ctx, token)
		if err != nil {
			writeError(w, apiFailure("me", err))
			return
		}
		members, err := s.api.members(ctx, token, wsID)
		if err != nil {
			writeError(w, apiFailure("members", err))
			return
		}
		v := &viewer{user: user, token: token, workspaceID: wsID, members: members, squadCache: map[string][]apiSquadMember{}}
		for _, m := range members {
			if m.UserID == user.ID {
				v.role = m.Role
				break
			}
		}
		switch v.role {
		case "owner", "admin", "member":
		case "":
			writeError(w, &apiError{http.StatusForbidden, "이 워크스페이스의 구성원이 아닙니다."})
			return
		default:
			// An outside collaborator reads as role "guest" (LIGH-282); any
			// role this service does not know gets nothing either.
			writeError(w, &apiError{http.StatusForbidden, "외부 협업자는 시크릿을 볼 수 없습니다."})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		fn(w, r, v)
	}
}

func (s *server) squadMembersFor(ctx context.Context, token, workspaceID, squadID string, cache map[string][]apiSquadMember) ([]apiSquadMember, error) {
	if m, ok := cache[squadID]; ok {
		return m, nil
	}
	m, err := s.api.squadMembers(ctx, token, workspaceID, squadID)
	if errors.Is(err, errNotFound) || errors.Is(err, errForbidden) {
		// A deleted squad grants nothing.
		m, err = nil, nil
	}
	if err != nil {
		return nil, err
	}
	cache[squadID] = m
	return m, nil
}

// granted reports whether the principal (a member's user id or an agent id)
// is named by the grants directly or through a squad.
func (s *server) granted(ctx context.Context, grants []grant, principalType, principalID, token, workspaceID string, cache map[string][]apiSquadMember) (bool, error) {
	for _, g := range grants {
		if g.PrincipalType == principalType && g.PrincipalID == principalID {
			return true, nil
		}
	}
	for _, g := range grants {
		if g.PrincipalType != "squad" {
			continue
		}
		members, err := s.squadMembersFor(ctx, token, workspaceID, g.PrincipalID, cache)
		if err != nil {
			return false, err
		}
		for _, m := range members {
			if m.MemberType == principalType && m.MemberID == principalID {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s *server) viewerCanRead(ctx context.Context, v *viewer, sec secretRow) (bool, error) {
	if sec.OwnerID == v.user.ID {
		return true, nil
	}
	return s.granted(ctx, sec.Grants, actorTypeMember, v.user.ID, v.token, v.workspaceID, v.squadCache)
}

// viewerCanManage: owners and admins manage every secret but read only what
// they are granted (they can grant themselves; that change is audited).
func viewerCanManage(v *viewer, sec secretRow) bool {
	return sec.OwnerID == v.user.ID || v.isAdmin()
}

type secretMeta struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Description       string  `json:"description"`
	OwnerID           string  `json:"owner_id"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
	UpdatedBy         string  `json:"updated_by"`
	ValueUpdatedAt    string  `json:"value_updated_at"`
	Version           int     `json:"version"`
	Grants            []grant `json:"grants"`
	LastReadAt        *string `json:"last_read_at"`
	LastReadActorType *string `json:"last_read_actor_type"`
	LastReadActorID   *string `json:"last_read_actor_id"`
	CanRead           bool    `json:"can_read"`
	CanManage         bool    `json:"can_manage"`
}

func (s *server) meta(ctx context.Context, v *viewer, sec secretRow, lr *lastRead) (secretMeta, error) {
	canRead, err := s.viewerCanRead(ctx, v, sec)
	if err != nil {
		return secretMeta{}, err
	}
	m := secretMeta{
		ID: sec.ID, Name: sec.Name, Description: sec.Description, OwnerID: sec.OwnerID,
		CreatedAt: sec.CreatedAt, UpdatedAt: sec.UpdatedAt, UpdatedBy: sec.UpdatedBy, ValueUpdatedAt: sec.ValueUpdatedAt,
		Version: sec.Version, Grants: sec.Grants, CanRead: canRead, CanManage: viewerCanManage(v, sec),
	}
	if m.Grants == nil {
		m.Grants = []grant{}
	}
	if lr != nil {
		m.LastReadAt, m.LastReadActorType, m.LastReadActorID = strPtr(lr.At), strPtr(lr.ActorType), strPtr(lr.ActorID)
	}
	return m, nil
}

func (s *server) handleList(w http.ResponseWriter, r *http.Request, v *viewer) {
	ctx := r.Context()
	secrets, err := s.store.listSecrets(ctx, v.workspaceID)
	if err != nil {
		writeError(w, internalError("list secrets", err))
		return
	}
	reads, err := s.store.lastReads(ctx, v.workspaceID)
	if err != nil {
		writeError(w, internalError("last reads", err))
		return
	}
	out := make([]secretMeta, 0, len(secrets))
	for _, sec := range secrets {
		var lr *lastRead
		if x, ok := reads[sec.ID]; ok {
			lr = &x
		}
		m, err := s.meta(ctx, v, sec, lr)
		if err != nil {
			writeError(w, apiFailure("squad members", err))
			return
		}
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"viewer":  map[string]string{"user_id": v.user.ID, "role": v.role},
		"secrets": out,
	})
}

// validateGrants normalizes grants and checks that every principal exists in
// the workspace.
func (s *server) validateGrants(ctx context.Context, v *viewer, in []grant) ([]grant, *apiError) {
	if len(in) == 0 {
		return []grant{}, nil
	}
	if len(in) > 200 {
		return nil, &apiError{http.StatusBadRequest, "권한 대상이 너무 많습니다."}
	}
	known := map[string]map[string]bool{"member": {}, "agent": {}, "squad": {}}
	for _, m := range v.members {
		known["member"][m.UserID] = true
	}
	needAgents, needSquads := false, false
	for _, g := range in {
		needAgents = needAgents || g.PrincipalType == "agent"
		needSquads = needSquads || g.PrincipalType == "squad"
	}
	if needAgents {
		agents, err := s.api.agents(ctx, v.token, v.workspaceID)
		if err != nil {
			return nil, apiFailure("agents", err)
		}
		for _, a := range agents {
			if a.ArchivedAt == nil {
				known["agent"][a.ID] = true
			}
		}
	}
	if needSquads {
		squads, err := s.api.squads(ctx, v.token, v.workspaceID)
		if err != nil {
			return nil, apiFailure("squads", err)
		}
		for _, sq := range squads {
			known["squad"][sq.ID] = true
		}
	}
	seen := map[grant]bool{}
	out := []grant{}
	for _, g := range in {
		ids, ok := known[g.PrincipalType]
		if !ok {
			return nil, &apiError{http.StatusBadRequest, "권한 대상 종류는 member, agent, squad 중 하나여야 합니다."}
		}
		if !ids[g.PrincipalID] {
			return nil, &apiError{http.StatusBadRequest, "워크스페이스에 없는 권한 대상이 있습니다."}
		}
		if !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	return out, nil
}

func validValue(value string) *apiError {
	if value == "" {
		return &apiError{http.StatusBadRequest, "값을 입력해 주세요."}
	}
	if len(value) > maxValueBytes {
		return &apiError{http.StatusBadRequest, "값은 64KB를 넘을 수 없습니다."}
	}
	return nil
}

func validDescription(d string) *apiError {
	if len([]rune(d)) > maxDescriptionRunes {
		return &apiError{http.StatusBadRequest, "설명은 1000자를 넘을 수 없습니다."}
	}
	return nil
}

func (s *server) memberEvent(r *http.Request, v *viewer, action, result string, sec *secretRow, detail map[string]any) auditEvent {
	ts, remote := clientMeta(r)
	ev := auditEvent{
		WorkspaceID: v.workspaceID, ActorType: actorTypeMember, ActorID: v.user.ID,
		Action: action, Result: result, Detail: detail, TailscaleLogin: ts, RemoteAddr: remote,
	}
	if sec != nil {
		ev.SecretID, ev.SecretName, ev.SecretOwnerID = strPtr(sec.ID), strPtr(sec.Name), strPtr(sec.OwnerID)
	}
	return ev
}

func (s *server) handleCreate(w http.ResponseWriter, r *http.Request, v *viewer) {
	var req struct {
		Name        string  `json:"name"`
		Description string  `json:"description"`
		Value       string  `json:"value"`
		Grants      []grant `json:"grants"`
	}
	if e := decodeBody(r, &req); e != nil {
		writeError(w, e)
		return
	}
	if !namePattern.MatchString(req.Name) {
		writeError(w, &apiError{http.StatusBadRequest, "이름은 영문 대문자로 시작하고 대문자·숫자·밑줄(_)만 쓸 수 있습니다."})
		return
	}
	if e := validValue(req.Value); e != nil {
		writeError(w, e)
		return
	}
	if e := validDescription(req.Description); e != nil {
		writeError(w, e)
		return
	}
	ctx := r.Context()
	grants, e := s.validateGrants(ctx, v, req.Grants)
	if e != nil {
		writeError(w, e)
		return
	}
	ts := now()
	sec := secretRow{
		ID: newID(), WorkspaceID: v.workspaceID, Name: req.Name, Description: strings.TrimSpace(req.Description),
		OwnerID: v.user.ID, Version: 1, CreatedAt: ts, UpdatedAt: ts, UpdatedBy: v.user.ID, ValueUpdatedAt: ts, Grants: grants,
	}
	ct, err := s.box.seal([]byte(req.Value), secretAAD(sec.WorkspaceID, sec.ID, sec.Version))
	if err != nil {
		writeError(w, internalError("seal", err))
		return
	}
	sec.Ciphertext = ct
	ev := s.memberEvent(r, v, actionCreate, resultOK, &sec, map[string]any{"grants": grants})
	if err := s.store.createSecret(ctx, sec, ev); err != nil {
		if errors.Is(err, errNameConflict) {
			writeError(w, &apiError{http.StatusConflict, "같은 이름의 시크릿이 이미 있습니다."})
			return
		}
		writeError(w, internalError("create secret", err))
		return
	}
	m, err := s.meta(ctx, v, sec, nil)
	if err != nil {
		writeError(w, apiFailure("squad members", err))
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

// loadManaged loads a secret the viewer may manage.
func (s *server) loadManaged(w http.ResponseWriter, r *http.Request, v *viewer) (secretRow, bool) {
	sec, err := s.store.getSecret(r.Context(), v.workspaceID, r.PathValue("id"))
	if errors.Is(err, errNoRows) {
		writeError(w, &apiError{http.StatusNotFound, "시크릿을 찾을 수 없습니다."})
		return sec, false
	}
	if err != nil {
		writeError(w, internalError("get secret", err))
		return sec, false
	}
	if !viewerCanManage(v, sec) {
		writeError(w, &apiError{http.StatusForbidden, "시크릿 소유자나 워크스페이스 owner/admin만 바꿀 수 있습니다."})
		return sec, false
	}
	return sec, true
}

func (s *server) handleUpdate(w http.ResponseWriter, r *http.Request, v *viewer) {
	var req struct {
		Description *string `json:"description"`
		Value       *string `json:"value"`
	}
	if e := decodeBody(r, &req); e != nil {
		writeError(w, e)
		return
	}
	sec, ok := s.loadManaged(w, r, v)
	if !ok {
		return
	}
	ts := now()
	detail := map[string]any{"value_changed": false, "description_changed": false}
	if req.Description != nil {
		d := strings.TrimSpace(*req.Description)
		if e := validDescription(d); e != nil {
			writeError(w, e)
			return
		}
		detail["description_changed"] = d != sec.Description
		sec.Description = d
	}
	if req.Value != nil {
		if e := validValue(*req.Value); e != nil {
			writeError(w, e)
			return
		}
		sec.Version++
		ct, err := s.box.seal([]byte(*req.Value), secretAAD(sec.WorkspaceID, sec.ID, sec.Version))
		if err != nil {
			writeError(w, internalError("seal", err))
			return
		}
		sec.Ciphertext, sec.ValueUpdatedAt = ct, ts
		detail["value_changed"] = true
		detail["version"] = sec.Version
	}
	sec.UpdatedAt, sec.UpdatedBy = ts, v.user.ID
	ev := s.memberEvent(r, v, actionUpdate, resultOK, &sec, detail)
	if err := s.store.updateSecret(r.Context(), sec, ev); err != nil {
		writeError(w, internalError("update secret", err))
		return
	}
	m, err := s.meta(r.Context(), v, sec, nil)
	if err != nil {
		writeError(w, apiFailure("squad members", err))
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *server) handleDelete(w http.ResponseWriter, r *http.Request, v *viewer) {
	sec, ok := s.loadManaged(w, r, v)
	if !ok {
		return
	}
	ev := s.memberEvent(r, v, actionDelete, resultOK, &sec, nil)
	if err := s.store.deleteSecret(r.Context(), v.workspaceID, sec.ID, ev); err != nil {
		writeError(w, internalError("delete secret", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleGrants(w http.ResponseWriter, r *http.Request, v *viewer) {
	var req struct {
		Grants []grant `json:"grants"`
	}
	if e := decodeBody(r, &req); e != nil {
		writeError(w, e)
		return
	}
	sec, ok := s.loadManaged(w, r, v)
	if !ok {
		return
	}
	grants, e := s.validateGrants(r.Context(), v, req.Grants)
	if e != nil {
		writeError(w, e)
		return
	}
	before := map[grant]bool{}
	for _, g := range sec.Grants {
		before[g] = true
	}
	after := map[grant]bool{}
	added, removed := []grant{}, []grant{}
	for _, g := range grants {
		after[g] = true
		if !before[g] {
			added = append(added, g)
		}
	}
	for _, g := range sec.Grants {
		if !after[g] {
			removed = append(removed, g)
		}
	}
	sec.Grants = grants
	ev := s.memberEvent(r, v, actionGrants, resultOK, &sec, map[string]any{"added": added, "removed": removed})
	if err := s.store.replaceGrants(r.Context(), sec, v.user.ID, ev); err != nil {
		writeError(w, internalError("replace grants", err))
		return
	}
	updated, err := s.store.getSecret(r.Context(), v.workspaceID, sec.ID)
	if err != nil {
		writeError(w, internalError("get secret", err))
		return
	}
	m, err := s.meta(r.Context(), v, updated, nil)
	if err != nil {
		writeError(w, apiFailure("squad members", err))
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// handleReveal returns a value to a person. The audit row is written before
// the value leaves the process; if it cannot be written, nothing is revealed.
func (s *server) handleReveal(w http.ResponseWriter, r *http.Request, v *viewer) {
	ctx := r.Context()
	sec, err := s.store.getSecret(ctx, v.workspaceID, r.PathValue("id"))
	if errors.Is(err, errNoRows) {
		writeError(w, &apiError{http.StatusNotFound, "시크릿을 찾을 수 없습니다."})
		return
	}
	if err != nil {
		writeError(w, internalError("get secret", err))
		return
	}
	ok, err := s.viewerCanRead(ctx, v, sec)
	if err != nil {
		writeError(w, apiFailure("squad members", err))
		return
	}
	if !ok {
		if err := s.store.audit(ctx, s.memberEvent(r, v, actionReveal, resultDenied, &sec, nil)); err != nil {
			log.Printf("audit denied reveal: %v", err)
		}
		writeError(w, &apiError{http.StatusForbidden, "이 시크릿을 볼 권한이 없습니다."})
		return
	}
	plain, err := s.box.open(sec.Ciphertext, secretAAD(sec.WorkspaceID, sec.ID, sec.Version))
	if err != nil {
		writeError(w, internalError("open secret "+sec.ID, err))
		return
	}
	if err := s.store.audit(ctx, s.memberEvent(r, v, actionReveal, resultOK, &sec, map[string]any{"version": sec.Version})); err != nil {
		writeError(w, internalError("audit reveal", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"value": string(plain)})
}

// ── agent enrollment ───────────────────────────────────────────────────────

func (s *server) handleEnrollments(w http.ResponseWriter, r *http.Request, v *viewer) {
	list, err := s.store.listEnrollments(r.Context(), v.workspaceID)
	if err != nil {
		writeError(w, internalError("list enrollments", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enrollments": list})
}

// loadEnrollableAgent mirrors the upstream env rule: the agent's owner or a
// workspace owner/admin may change its custom_env.
func (s *server) loadEnrollableAgent(w http.ResponseWriter, r *http.Request, v *viewer) (apiAgent, bool) {
	agentID := r.PathValue("agent")
	if !uuidPattern.MatchString(agentID) {
		writeError(w, &apiError{http.StatusBadRequest, "에이전트 ID가 올바르지 않습니다."})
		return apiAgent{}, false
	}
	agent, err := s.api.agent(r.Context(), v.token, v.workspaceID, agentID)
	if err != nil {
		if errors.Is(err, errNotFound) || errors.Is(err, errForbidden) {
			writeError(w, &apiError{http.StatusNotFound, "에이전트를 찾을 수 없습니다."})
			return apiAgent{}, false
		}
		writeError(w, apiFailure("agent", err))
		return apiAgent{}, false
	}
	owner := agent.OwnerID != nil && *agent.OwnerID != "" && *agent.OwnerID == v.user.ID
	if !owner && !v.isAdmin() {
		writeError(w, &apiError{http.StatusForbidden, "에이전트 소유자나 워크스페이스 owner/admin만 연결할 수 있습니다."})
		return apiAgent{}, false
	}
	return agent, true
}

// handleEnroll mints a token for the agent and stores it in the agent's
// custom_env through the upstream env API, keeping every other key.
func (s *server) handleEnroll(w http.ResponseWriter, r *http.Request, v *viewer) {
	agent, ok := s.loadEnrollableAgent(w, r, v)
	if !ok {
		return
	}
	if agent.ArchivedAt != nil {
		writeError(w, &apiError{http.StatusBadRequest, "보관된 에이전트는 연결할 수 없습니다."})
		return
	}
	ctx := r.Context()
	current, err := s.api.agentEnv(ctx, v.token, v.workspaceID, agent.ID)
	if err != nil {
		writeError(w, apiFailure("agent env", err))
		return
	}
	token, err := newAgentToken()
	if err != nil {
		writeError(w, internalError("mint token", err))
		return
	}
	next := make(map[string]string, len(current)+2)
	for k := range current {
		next[k] = upstreamKeepSentinel
	}
	next[envToken] = token
	next[envURL] = s.cfg.PublicURL
	if err := s.api.putAgentEnv(ctx, v.token, v.workspaceID, agent.ID, next); err != nil {
		writeError(w, apiFailure("put agent env", err))
		return
	}
	e := enrollment{AgentID: agent.ID, WorkspaceID: v.workspaceID, EnrolledBy: v.user.ID, EnrolledAt: now()}
	_, rotated := current[envToken]
	ev := s.memberEvent(r, v, actionEnroll, resultOK, nil, map[string]any{"agent_id": agent.ID, "agent_name": agent.Name, "rotated": rotated})
	if err := s.store.upsertEnrollment(ctx, e, hashToken(token), ev); err != nil {
		writeError(w, internalError("store enrollment", err))
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *server) handleUnenroll(w http.ResponseWriter, r *http.Request, v *viewer) {
	agent, ok := s.loadEnrollableAgent(w, r, v)
	if !ok {
		return
	}
	ctx := r.Context()
	ev := s.memberEvent(r, v, actionUnenroll, resultOK, nil, map[string]any{"agent_id": agent.ID, "agent_name": agent.Name})
	// Revoke first: once the row is gone the token no longer works, whatever
	// happens to the env cleanup below.
	if err := s.store.deleteEnrollment(ctx, v.workspaceID, agent.ID, ev); err != nil && !errors.Is(err, errNoRows) {
		writeError(w, internalError("delete enrollment", err))
		return
	}
	current, err := s.api.agentEnv(ctx, v.token, v.workspaceID, agent.ID)
	if err != nil {
		writeError(w, apiFailure("agent env", err))
		return
	}
	if _, has := current[envToken]; has || current[envURL] != "" {
		next := make(map[string]string, len(current))
		for k := range current {
			if k != envToken && k != envURL {
				next[k] = upstreamKeepSentinel
			}
		}
		if err := s.api.putAgentEnv(ctx, v.token, v.workspaceID, agent.ID, next); err != nil {
			writeError(w, apiFailure("put agent env", err))
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── audit views ────────────────────────────────────────────────────────────

// auditScopeFor: owners/admins see the whole workspace; everyone else sees
// events on secrets they own, their own actions, and their agents' actions.
func (s *server) auditScopeFor(ctx context.Context, v *viewer) (*auditScope, *apiError) {
	if v.isAdmin() {
		return nil, nil
	}
	agents, err := s.api.agents(ctx, v.token, v.workspaceID)
	if err != nil {
		return nil, apiFailure("agents", err)
	}
	scope := &auditScope{userID: v.user.ID}
	for _, a := range agents {
		if a.OwnerID != nil && *a.OwnerID == v.user.ID {
			scope.ownedAgents = append(scope.ownedAgents, a.ID)
		}
	}
	return scope, nil
}

func (s *server) handleAudit(w http.ResponseWriter, r *http.Request, v *viewer) {
	q := r.URL.Query()
	f := auditFilter{
		SecretID: q.Get("secret_id"), ActorType: q.Get("actor_type"), ActorID: q.Get("actor_id"),
		Action: q.Get("action"), Result: q.Get("result"), Limit: 50,
	}
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
		f.Limit = min(n, 200)
	}
	if b := q.Get("before"); b != "" {
		n, err := strconv.ParseInt(b, 10, 64)
		if err != nil || n <= 0 {
			writeError(w, &apiError{http.StatusBadRequest, "before 값이 올바르지 않습니다."})
			return
		}
		f.Before = n
	}
	scope, e := s.auditScopeFor(r.Context(), v)
	if e != nil {
		writeError(w, e)
		return
	}
	events, err := s.store.listAudit(r.Context(), v.workspaceID, f, scope)
	if err != nil {
		writeError(w, internalError("list audit", err))
		return
	}
	var next *string
	if len(events) == f.Limit {
		next = strPtr(events[len(events)-1].ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "next_before": next})
}

func (s *server) handleAgentSummaries(w http.ResponseWriter, r *http.Request, v *viewer) {
	scope, e := s.auditScopeFor(r.Context(), v)
	if e != nil {
		writeError(w, e)
		return
	}
	list, err := s.store.agentSummaries(r.Context(), v.workspaceID, scope)
	if err != nil {
		writeError(w, internalError("agent summaries", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": list})
}

// ── agent callers ──────────────────────────────────────────────────────────

// agentCaller is an enrolled agent calling from inside a task.
type agentCaller struct {
	enrollment enrollment
	taskToken  string
	taskID     *string
	squadCache map[string][]apiSquadMember
}

// agentOnly authenticates the enrollment token and additionally requires the
// task's own Multica token, which proves the call comes from a live task in
// the enrollment's workspace and is used to read squad membership.
func (s *server) agentOnly(fn func(http.ResponseWriter, *http.Request, *agentCaller)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if !strings.HasPrefix(auth, "Bearer ") || !strings.HasPrefix(token, agentTokenPrefix) {
			writeError(w, &apiError{http.StatusUnauthorized, "LIGHTSOFT_SECRETS_TOKEN이 필요합니다. 팀 클라이언트의 시크릿 > 에이전트 연결에서 이 에이전트를 연결해 주세요."})
			return
		}
		taskToken := strings.TrimSpace(r.Header.Get("X-Multica-Token"))
		if !strings.HasPrefix(taskToken, "mat_") {
			writeError(w, &apiError{http.StatusUnauthorized, "Multica 태스크 안에서만 쓸 수 있습니다(MULTICA_TOKEN 없음)."})
			return
		}
		ctx := r.Context()
		e, err := s.store.enrollmentByToken(ctx, token)
		if errors.Is(err, errNoRows) {
			writeError(w, &apiError{http.StatusUnauthorized, "연결이 해제됐거나 재발급된 토큰입니다. 에이전트의 다음 실행부터 새 토큰이 적용됩니다."})
			return
		}
		if err != nil {
			writeError(w, internalError("enrollment", err))
			return
		}
		// The backend rejects a task token whose bound workspace differs from
		// X-Workspace-ID, so this read fails unless the caller holds a live
		// task token for the enrollment's workspace.
		if _, err := s.api.members(ctx, taskToken, e.WorkspaceID); err != nil {
			if errors.Is(err, errUnauthenticated) || errors.Is(err, errForbidden) || errors.Is(err, errNotFound) {
				writeError(w, &apiError{http.StatusUnauthorized, "Multica 태스크 토큰을 확인하지 못했습니다."})
				return
			}
			writeError(w, apiFailure("members", err))
			return
		}
		c := &agentCaller{enrollment: e, taskToken: taskToken, squadCache: map[string][]apiSquadMember{}}
		if t := r.Header.Get("X-Task-ID"); uuidPattern.MatchString(t) {
			c.taskID = strPtr(t)
		}
		s.store.touchEnrollment(ctx, e.AgentID)
		fn(w, r, c)
	}
}

func (s *server) agentCanRead(ctx context.Context, c *agentCaller, sec secretRow) (bool, error) {
	return s.granted(ctx, sec.Grants, actorTypeAgent, c.enrollment.AgentID, c.taskToken, c.enrollment.WorkspaceID, c.squadCache)
}

func (s *server) handleAgentList(w http.ResponseWriter, r *http.Request, c *agentCaller) {
	ctx := r.Context()
	secrets, err := s.store.listSecrets(ctx, c.enrollment.WorkspaceID)
	if err != nil {
		writeError(w, internalError("list secrets", err))
		return
	}
	type item struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	out := []item{}
	for _, sec := range secrets {
		ok, err := s.agentCanRead(ctx, c, sec)
		if err != nil {
			writeError(w, apiFailure("squad members", err))
			return
		}
		if ok {
			out = append(out, item{sec.Name, sec.Description})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"secrets": out})
}

func (s *server) handleAgentRead(w http.ResponseWriter, r *http.Request, c *agentCaller) {
	ctx := r.Context()
	name := r.PathValue("name")
	ts, remote := clientMeta(r)
	ev := auditEvent{
		WorkspaceID: c.enrollment.WorkspaceID, ActorType: actorTypeAgent, ActorID: c.enrollment.AgentID,
		TaskID: c.taskID, Action: actionRead, TailscaleLogin: ts, RemoteAddr: remote,
	}
	sec, err := s.store.getSecretByName(ctx, c.enrollment.WorkspaceID, name)
	if errors.Is(err, errNoRows) {
		ev.SecretName, ev.Result, ev.Detail = strPtr(name), resultDenied, map[string]any{"reason": "not_found"}
		if err := s.store.audit(ctx, ev); err != nil {
			log.Printf("audit missing read: %v", err)
		}
		writeError(w, &apiError{http.StatusNotFound, "시크릿 " + name + "이(가) 없거나 이 에이전트에게 권한이 없습니다."})
		return
	}
	if err != nil {
		writeError(w, internalError("get secret", err))
		return
	}
	ev.SecretID, ev.SecretName, ev.SecretOwnerID = strPtr(sec.ID), strPtr(sec.Name), strPtr(sec.OwnerID)
	ok, err := s.agentCanRead(ctx, c, sec)
	if err != nil {
		writeError(w, apiFailure("squad members", err))
		return
	}
	if !ok {
		ev.Result, ev.Detail = resultDenied, map[string]any{"reason": "no_grant"}
		if err := s.store.audit(ctx, ev); err != nil {
			log.Printf("audit denied read: %v", err)
		}
		// Same answer as a missing secret, so an agent cannot probe names.
		writeError(w, &apiError{http.StatusNotFound, "시크릿 " + name + "이(가) 없거나 이 에이전트에게 권한이 없습니다."})
		return
	}
	plain, err := s.box.open(sec.Ciphertext, secretAAD(sec.WorkspaceID, sec.ID, sec.Version))
	if err != nil {
		writeError(w, internalError("open secret "+sec.ID, err))
		return
	}
	ev.Result, ev.Detail = resultOK, map[string]any{"version": sec.Version}
	if err := s.store.audit(ctx, ev); err != nil {
		writeError(w, internalError("audit read", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": sec.Name, "value": string(plain)})
}
