package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const (
	wsID    = "11111111-1111-4111-8111-111111111111"
	otherWS = "22222222-2222-4222-8222-222222222222"
	alice   = "a0000000-0000-4000-8000-000000000001" // member, owns agentA
	bob     = "b0000000-0000-4000-8000-000000000002" // member, owns agentB
	carol   = "c0000000-0000-4000-8000-000000000003" // admin
	erin    = "e0000000-0000-4000-8000-000000000005" // not in the workspace
	agentA  = "aa000000-0000-4000-8000-00000000000a"
	agentB  = "bb000000-0000-4000-8000-00000000000b"
	agentC  = "cc000000-0000-4000-8000-00000000000c" // archived
	squadS  = "55000000-0000-4000-8000-000000000005" // members: agentB, bob

	origin = "https://mac-studio.taile83671.ts.net:3444"
)

// session tokens are JWT-shaped like real login cookies.
func session(user string) string { return "eyJhbGciOiJIUzI1NiJ9." + user + ".sig" }

// fakeMultica imitates the parts of the production API the service reads.
type fakeMultica struct {
	mu   sync.Mutex
	env  map[string]map[string]string // agent -> custom_env
	puts int
	// taskTokens maps mat_ tokens to their runtime owner and bound workspace.
	taskTokens map[string][2]string
}

func newFakeMultica() *fakeMultica {
	return &fakeMultica{
		env: map[string]map[string]string{agentA: {"EXISTING": "keep-me"}, agentB: {}},
		taskTokens: map[string][2]string{
			"mat_taskA":     {alice, wsID},
			"mat_taskB":     {bob, wsID},
			"mat_taskOther": {alice, otherWS},
		},
	}
}

func (f *fakeMultica) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	var user string
	machine := false
	if tt, ok := f.taskTokens[token]; ok {
		if h := r.Header.Get("X-Workspace-ID"); h != "" && h != tt[1] {
			http.Error(w, `{"error":"task token is bound to a different workspace"}`, http.StatusForbidden)
			return
		}
		user, machine = tt[0], true
	} else if strings.HasPrefix(token, "eyJ") {
		user = strings.Split(token, ".")[1]
	} else {
		http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
		return
	}
	members := []apiMember{{UserID: alice, Role: "member"}, {UserID: bob, Role: "member"}, {UserID: carol, Role: "admin"}}
	isMember := false
	role := ""
	for _, m := range members {
		if m.UserID == user {
			isMember, role = true, m.Role
		}
	}
	agents := []apiAgent{
		{ID: agentA, WorkspaceID: wsID, Name: "agent-a", OwnerID: strPtr(alice)},
		{ID: agentB, WorkspaceID: wsID, Name: "agent-b", OwnerID: strPtr(bob)},
		{ID: agentC, WorkspaceID: wsID, Name: "agent-c", OwnerID: strPtr(alice), ArchivedAt: strPtr("2026-01-01T00:00:00Z")},
	}
	p := r.URL.Path
	switch {
	case p == "/api/me":
		writeJSON(w, 200, apiUser{ID: user})
		return
	case !isMember:
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	case p == "/api/workspaces/"+wsID+"/members":
		writeJSON(w, 200, members)
	case p == "/api/agents":
		writeJSON(w, 200, agents)
	case p == "/api/squads":
		writeJSON(w, 200, []apiSquad{{ID: squadS}})
	case p == "/api/squads/"+squadS+"/members":
		writeJSON(w, 200, []apiSquadMember{{MemberType: "agent", MemberID: agentB}, {MemberType: "member", MemberID: bob}})
	case strings.HasPrefix(p, "/api/agents/") && strings.HasSuffix(p, "/env"):
		id := strings.TrimSuffix(strings.TrimPrefix(p, "/api/agents/"), "/env")
		var ag *apiAgent
		for i := range agents {
			if agents[i].ID == id {
				ag = &agents[i]
			}
		}
		if ag == nil {
			http.Error(w, `{}`, 404)
			return
		}
		if machine || (role != "admin" && role != "owner" && *ag.OwnerID != user) {
			http.Error(w, `{}`, 403)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method == http.MethodGet {
			writeJSON(w, 200, map[string]any{"agent_id": id, "custom_env": f.env[id]})
			return
		}
		var req struct {
			CustomEnv map[string]string `json:"custom_env"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		next := map[string]string{}
		for k, v := range req.CustomEnv {
			if v == "****" {
				if old, ok := f.env[id][k]; ok {
					next[k] = old
				}
				continue
			}
			next[k] = v
		}
		f.env[id] = next
		f.puts++
		writeJSON(w, 200, map[string]any{})
	case strings.HasPrefix(p, "/api/agents/"):
		id := strings.TrimPrefix(p, "/api/agents/")
		for _, a := range agents {
			if a.ID == id {
				writeJSON(w, 200, a)
				return
			}
		}
		http.Error(w, `{}`, 404)
	default:
		http.Error(w, `{}`, 404)
	}
}

func (f *fakeMultica) agentEnv(agent string) map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for k, v := range f.env[agent] {
		out[k] = v
	}
	return out
}

type harness struct {
	t     *testing.T
	srv   *httptest.Server
	s     *server
	fake  *fakeMultica
	dbDir string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	fake := newFakeMultica()
	api := httptest.NewServer(fake)
	t.Cleanup(api.Close)

	dir := t.TempDir()
	key := make([]byte, keySize)
	_, _ = rand.Read(key)
	keyPath := filepath.Join(dir, "master.key")
	if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(key)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	box, err := newSealer(loaded)
	if err != nil {
		t.Fatal(err)
	}
	st, err := openStore(filepath.Join(dir, "secrets.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := Config{
		APIURL:         api.URL,
		AllowedOrigins: map[string]bool{origin: true},
		PublicURL:      "https://example.test/lightsoft-secrets",
	}
	s := &server{cfg: cfg, api: newMulticaClient(api.URL), store: st, box: box}
	srv := httptest.NewServer(s.routes())
	t.Cleanup(srv.Close)
	return &harness{t: t, srv: srv, s: s, fake: fake, dbDir: dir}
}

type resp struct {
	status int
	body   []byte
}

func (r resp) json(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

// browser sends a request the way the team client does.
func (h *harness) browser(user, method, path string, body any) resp {
	h.t.Helper()
	return h.request(method, path, body, func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: authCookieName, Value: session(user)})
		if method != http.MethodGet {
			req.Header.Set("Origin", origin)
			req.Header.Set(csrfHeader, "1")
		}
	})
}

func (h *harness) agentCall(token, taskToken, path string) resp {
	h.t.Helper()
	return h.request(http.MethodGet, path, nil, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+token)
		if taskToken != "" {
			req.Header.Set("X-Multica-Token", taskToken)
		}
		req.Header.Set("X-Task-ID", "99999999-9999-4999-8999-999999999999")
	})
}

func (h *harness) request(method, path string, body any, mod func(*http.Request)) resp {
	h.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, h.srv.URL+PathPrefix+path, rd)
	if err != nil {
		h.t.Fatal(err)
	}
	if mod != nil {
		mod(req)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{status: res.StatusCode, body: b}
}

func ws(path string) string { return "/api/w/" + wsID + path }

func (h *harness) create(user, name, value string, grants []grant) secretMeta {
	h.t.Helper()
	r := h.browser(user, http.MethodPost, ws("/secrets"), map[string]any{"name": name, "value": value, "description": "d", "grants": grants})
	if r.status != http.StatusCreated {
		h.t.Fatalf("create %s: %d %s", name, r.status, r.body)
	}
	var m secretMeta
	r.json(h.t, &m)
	return m
}

func (h *harness) reveal(user, id string) resp {
	h.t.Helper()
	return h.browser(user, http.MethodPost, ws("/secrets/"+id+"/reveal"), nil)
}

func revealed(t *testing.T, r resp) string {
	t.Helper()
	if r.status != http.StatusOK {
		t.Fatalf("reveal: %d %s", r.status, r.body)
	}
	var out struct{ Value string }
	r.json(t, &out)
	return out.Value
}

func (h *harness) enroll(user, agent string) resp {
	h.t.Helper()
	return h.browser(user, http.MethodPost, ws("/agents/"+agent+"/enroll"), nil)
}

func TestCreateListRevealKeepsValuesEncrypted(t *testing.T) {
	h := newHarness(t)
	m := h.create(alice, "GITHUB_TOKEN", "ghp_super_secret_value", nil)
	if !m.CanRead || !m.CanManage || m.Version != 1 || m.OwnerID != alice {
		t.Fatalf("unexpected meta: %+v", m)
	}

	list := h.browser(bob, http.MethodGet, ws("/secrets"), nil)
	if list.status != 200 || bytes.Contains(list.body, []byte("ghp_super_secret_value")) {
		t.Fatalf("list must not contain the value: %d %s", list.status, list.body)
	}
	var out struct {
		Viewer  map[string]string
		Secrets []secretMeta
	}
	list.json(t, &out)
	if len(out.Secrets) != 1 || out.Secrets[0].CanRead || out.Secrets[0].CanManage || out.Viewer["role"] != "member" {
		t.Fatalf("bob should see metadata only: %+v", out)
	}

	if got := revealed(t, h.reveal(alice, m.ID)); got != "ghp_super_secret_value" {
		t.Fatalf("reveal = %q", got)
	}

	if r := h.browser(alice, http.MethodPost, ws("/secrets"), map[string]any{"name": "GITHUB_TOKEN", "value": "x"}); r.status != http.StatusConflict {
		t.Fatalf("duplicate name: %d %s", r.status, r.body)
	}
	if r := h.browser(alice, http.MethodPost, ws("/secrets"), map[string]any{"name": "lower_case", "value": "x"}); r.status != http.StatusBadRequest {
		t.Fatalf("bad name: %d", r.status)
	}

	// Nothing on disk holds the plaintext (database, WAL, shared memory).
	if err := h.s.store.db.Close(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(h.dbDir)
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join(h.dbDir, e.Name()))
		if bytes.Contains(b, []byte("ghp_super_secret_value")) {
			t.Fatalf("%s contains the plaintext value", e.Name())
		}
	}
}

func TestReadPermissions(t *testing.T) {
	h := newHarness(t)
	m := h.create(alice, "DB_PASSWORD", "pw", nil)

	if r := h.reveal(bob, m.ID); r.status != http.StatusForbidden {
		t.Fatalf("bob without grant: %d", r.status)
	}
	// Admins manage but do not read without a grant.
	if r := h.reveal(carol, m.ID); r.status != http.StatusForbidden {
		t.Fatalf("admin without grant: %d", r.status)
	}
	if r := h.browser(carol, http.MethodPut, ws("/secrets/"+m.ID+"/grants"), map[string]any{"grants": []grant{{"member", carol}}}); r.status != 200 {
		t.Fatalf("admin grants self: %d %s", r.status, r.body)
	}
	if got := revealed(t, h.reveal(carol, m.ID)); got != "pw" {
		t.Fatalf("admin after grant = %q", got)
	}
	// bob is in squadS.
	if r := h.browser(alice, http.MethodPut, ws("/secrets/"+m.ID+"/grants"), map[string]any{"grants": []grant{{"squad", squadS}}}); r.status != 200 {
		t.Fatalf("grant squad: %d %s", r.status, r.body)
	}
	if got := revealed(t, h.reveal(bob, m.ID)); got != "pw" {
		t.Fatalf("bob via squad = %q", got)
	}
	if r := h.reveal(carol, m.ID); r.status != http.StatusForbidden {
		t.Fatalf("carol grant was replaced: %d", r.status)
	}
	if r := h.browser(alice, http.MethodPut, ws("/secrets/"+m.ID+"/grants"), map[string]any{"grants": []grant{{"member", erin}}}); r.status != http.StatusBadRequest {
		t.Fatalf("grant to non-member: %d", r.status)
	}
	if r := h.browser(alice, http.MethodPut, ws("/secrets/"+m.ID+"/grants"), map[string]any{"grants": []grant{{"agent", agentC}}}); r.status != http.StatusBadRequest {
		t.Fatalf("grant to archived agent: %d", r.status)
	}

	events := h.auditEvents(carol, "?secret_id="+m.ID)
	var denied int
	for _, ev := range events {
		if ev.Action == actionReveal && ev.Result == resultDenied {
			denied++
		}
	}
	if denied != 3 {
		t.Fatalf("denied reveals recorded = %d, want 3", denied)
	}
}

func TestManagePermissions(t *testing.T) {
	h := newHarness(t)
	m := h.create(alice, "API_KEY", "v1", []grant{{"member", bob}})

	if r := h.browser(bob, http.MethodPatch, ws("/secrets/"+m.ID), map[string]any{"value": "hacked"}); r.status != http.StatusForbidden {
		t.Fatalf("reader must not update: %d", r.status)
	}
	if r := h.browser(bob, http.MethodDelete, ws("/secrets/"+m.ID), nil); r.status != http.StatusForbidden {
		t.Fatalf("reader must not delete: %d", r.status)
	}
	r := h.browser(carol, http.MethodPatch, ws("/secrets/"+m.ID), map[string]any{"value": "v2", "description": "rotated"})
	if r.status != 200 {
		t.Fatalf("admin update: %d %s", r.status, r.body)
	}
	var updated secretMeta
	r.json(t, &updated)
	if updated.Version != 2 || updated.Description != "rotated" || updated.CanRead {
		t.Fatalf("updated meta: %+v", updated)
	}
	if got := revealed(t, h.reveal(bob, m.ID)); got != "v2" {
		t.Fatalf("bob reads new value: %q", got)
	}
	if r := h.browser(alice, http.MethodDelete, ws("/secrets/"+m.ID), nil); r.status != http.StatusNoContent {
		t.Fatalf("owner delete: %d", r.status)
	}
	if r := h.reveal(alice, m.ID); r.status != http.StatusNotFound {
		t.Fatalf("deleted: %d", r.status)
	}
}

func TestBrowserGuards(t *testing.T) {
	h := newHarness(t)
	body := map[string]any{"name": "X", "value": "y"}

	noCSRF := h.request(http.MethodPost, ws("/secrets"), body, func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: authCookieName, Value: session(alice)})
		req.Header.Set("Origin", origin)
	})
	if noCSRF.status != http.StatusForbidden {
		t.Fatalf("missing csrf header: %d", noCSRF.status)
	}
	badOrigin := h.request(http.MethodPost, ws("/secrets"), body, func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: authCookieName, Value: session(alice)})
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set(csrfHeader, "1")
	})
	if badOrigin.status != http.StatusForbidden {
		t.Fatalf("foreign origin: %d", badOrigin.status)
	}
	// Machine credentials (agent task token, PAT) must not pass as a person,
	// as cookie or bearer.
	for _, mod := range []func(*http.Request){
		func(req *http.Request) { req.AddCookie(&http.Cookie{Name: authCookieName, Value: "mat_taskA"}) },
		func(req *http.Request) { req.Header.Set("Authorization", "Bearer mat_taskA") },
		func(req *http.Request) { req.Header.Set("Authorization", "Bearer mul_personal") },
		func(req *http.Request) { req.Header.Set("Authorization", session(alice)) },
	} {
		if r := h.request(http.MethodGet, ws("/secrets"), nil, mod); r.status != http.StatusUnauthorized {
			t.Fatalf("machine credential as person: %d", r.status)
		}
	}
	if r := h.browser(erin, http.MethodGet, ws("/secrets"), nil); r.status != http.StatusForbidden {
		t.Fatalf("non-member: %d", r.status)
	}
}

// Lightica (desktop) holds the login session as a bearer token and runs from
// a file:// page, so it sends no cookie and no allowed Origin. A deliberate
// bearer credential needs no CSRF guard.
func TestLighticaBearerSession(t *testing.T) {
	h := newHarness(t)
	bearer := func(req *http.Request) { req.Header.Set("Authorization", "Bearer "+session(alice)) }

	created := h.request(http.MethodPost, ws("/secrets"), map[string]any{"name": "DESKTOP_KEY", "value": "from-lightica"}, bearer)
	if created.status != http.StatusCreated {
		t.Fatalf("create with bearer: %d %s", created.status, created.body)
	}
	var m secretMeta
	created.json(t, &m)
	if m.OwnerID != alice {
		t.Fatalf("owner = %q", m.OwnerID)
	}
	revealed := h.request(http.MethodPost, ws("/secrets/"+m.ID+"/reveal"), nil, bearer)
	var out struct{ Value string }
	revealed.json(t, &out)
	if revealed.status != 200 || out.Value != "from-lightica" {
		t.Fatalf("reveal with bearer: %d %s", revealed.status, revealed.body)
	}
	if r := h.request(http.MethodPost, ws("/agents/"+agentA+"/enroll"), nil, bearer); r.status != 200 {
		t.Fatalf("enroll with bearer: %d %s", r.status, r.body)
	}
	bobBearer := func(req *http.Request) { req.Header.Set("Authorization", "Bearer "+session(bob)) }
	if r := h.request(http.MethodPost, ws("/secrets/"+m.ID+"/reveal"), nil, bobBearer); r.status != http.StatusForbidden {
		t.Fatalf("bob with bearer and no grant: %d", r.status)
	}
	events := h.auditEvents(carol, "?secret_id="+m.ID)
	if len(events) != 3 || events[0].ActorID != bob || events[0].Result != resultDenied {
		t.Fatalf("bearer actions must be recorded like browser ones: %+v", events)
	}
}

func TestAgentEnrollmentAndReads(t *testing.T) {
	h := newHarness(t)
	granted := h.create(alice, "OPENAI_KEY", "sk-agent", []grant{{"agent", agentA}})
	other := h.create(alice, "PRIVATE", "nope", nil)
	viaSquad := h.create(alice, "SQUAD_KEY", "squad-value", []grant{{"squad", squadS}})

	if r := h.enroll(bob, agentA); r.status != http.StatusForbidden {
		t.Fatalf("bob must not enroll alice's agent: %d", r.status)
	}
	if r := h.enroll(alice, agentC); r.status != http.StatusBadRequest {
		t.Fatalf("archived agent: %d", r.status)
	}
	if r := h.enroll(alice, agentA); r.status != 200 {
		t.Fatalf("enroll: %d %s", r.status, r.body)
	}
	env := h.fake.agentEnv(agentA)
	tokenA := env[envToken]
	if !strings.HasPrefix(tokenA, agentTokenPrefix) || env[envURL] != "https://example.test/lightsoft-secrets" || env["EXISTING"] != "keep-me" {
		t.Fatalf("agent env after enroll: keys=%v url=%q existing=%q", len(env), env[envURL], env["EXISTING"])
	}
	// Admins may enroll any agent.
	if r := h.enroll(carol, agentB); r.status != 200 {
		t.Fatalf("admin enroll: %d %s", r.status, r.body)
	}
	tokenB := h.fake.agentEnv(agentB)[envToken]

	r := h.agentCall(tokenA, "mat_taskA", "/v1/secrets/OPENAI_KEY")
	var got struct{ Name, Value string }
	r.json(t, &got)
	if r.status != 200 || got.Value != "sk-agent" {
		t.Fatalf("agent read: %d %s", r.status, r.body)
	}
	if r := h.agentCall(tokenA, "mat_taskA", "/v1/secrets/PRIVATE"); r.status != http.StatusNotFound {
		t.Fatalf("ungranted read: %d", r.status)
	}
	if r := h.agentCall(tokenA, "mat_taskA", "/v1/secrets/NOPE"); r.status != http.StatusNotFound {
		t.Fatalf("missing secret: %d", r.status)
	}
	if r := h.agentCall(tokenA, "mat_taskA", "/v1/secrets/SQUAD_KEY"); r.status != http.StatusNotFound {
		t.Fatalf("agentA is not in the squad: %d", r.status)
	}
	if r := h.agentCall(tokenB, "mat_taskB", "/v1/secrets/SQUAD_KEY"); r.status != 200 {
		t.Fatalf("agentB via squad: %d %s", r.status, r.body)
	}
	if r := h.agentCall(tokenA, "", "/v1/secrets/OPENAI_KEY"); r.status != http.StatusUnauthorized {
		t.Fatalf("without task token: %d", r.status)
	}
	if r := h.agentCall(tokenA, "mat_taskOther", "/v1/secrets/OPENAI_KEY"); r.status != http.StatusUnauthorized {
		t.Fatalf("task token of another workspace: %d", r.status)
	}
	if r := h.agentCall("lss_forged", "mat_taskA", "/v1/secrets/OPENAI_KEY"); r.status != http.StatusUnauthorized {
		t.Fatalf("unknown token: %d", r.status)
	}

	var list struct{ Secrets []struct{ Name string } }
	h.agentCall(tokenA, "mat_taskA", "/v1/secrets").json(t, &list)
	if len(list.Secrets) != 1 || list.Secrets[0].Name != "OPENAI_KEY" {
		t.Fatalf("agent list: %+v", list)
	}

	// Rotation invalidates the old token.
	if r := h.enroll(alice, agentA); r.status != 200 {
		t.Fatalf("rotate: %d", r.status)
	}
	if r := h.agentCall(tokenA, "mat_taskA", "/v1/secrets/OPENAI_KEY"); r.status != http.StatusUnauthorized {
		t.Fatalf("old token after rotation: %d", r.status)
	}
	tokenA2 := h.fake.agentEnv(agentA)[envToken]
	if tokenA2 == tokenA || h.agentCall(tokenA2, "mat_taskA", "/v1/secrets/OPENAI_KEY").status != 200 {
		t.Fatal("new token should work")
	}

	// Unenroll revokes and removes only our keys.
	if r := h.browser(alice, http.MethodDelete, ws("/agents/"+agentA+"/enroll"), nil); r.status != http.StatusNoContent {
		t.Fatalf("unenroll: %d %s", r.status, r.body)
	}
	if r := h.agentCall(tokenA2, "mat_taskA", "/v1/secrets/OPENAI_KEY"); r.status != http.StatusUnauthorized {
		t.Fatalf("after unenroll: %d", r.status)
	}
	env = h.fake.agentEnv(agentA)
	if _, ok := env[envToken]; ok || env["EXISTING"] != "keep-me" {
		t.Fatalf("env after unenroll: has token=%v existing=%q", ok, env["EXISTING"])
	}

	var enr struct{ Enrollments []enrollment }
	h.browser(bob, http.MethodGet, ws("/agents"), nil).json(t, &enr)
	if len(enr.Enrollments) != 1 || enr.Enrollments[0].AgentID != agentB || enr.Enrollments[0].LastUsedAt == nil {
		t.Fatalf("enrollments: %+v", enr)
	}
	_ = granted
	_ = other
	_ = viaSquad
}

func (h *harness) auditEvents(user, query string) []auditEvent {
	h.t.Helper()
	r := h.browser(user, http.MethodGet, ws("/audit"+query), nil)
	if r.status != 200 {
		h.t.Fatalf("audit: %d %s", r.status, r.body)
	}
	var out struct {
		Events     []auditEvent
		NextBefore *string `json:"next_before"`
	}
	r.json(h.t, &out)
	return out.Events
}

func TestAuditVisibilityAndAgentSummaries(t *testing.T) {
	h := newHarness(t)
	a := h.create(alice, "ALICE_KEY", "1", []grant{{"agent", agentA}, {"agent", agentB}})
	b := h.create(bob, "BOB_KEY", "2", nil)
	h.enroll(alice, agentA)
	h.enroll(bob, agentB)
	tokA := h.fake.agentEnv(agentA)[envToken]
	tokB := h.fake.agentEnv(agentB)[envToken]
	h.agentCall(tokA, "mat_taskA", "/v1/secrets/ALICE_KEY")
	h.agentCall(tokA, "mat_taskA", "/v1/secrets/ALICE_KEY")
	h.agentCall(tokA, "mat_taskA", "/v1/secrets/BOB_KEY") // denied
	h.agentCall(tokB, "mat_taskB", "/v1/secrets/ALICE_KEY")
	h.reveal(bob, b.ID)

	all := h.auditEvents(carol, "")
	if len(all) != 9 { // 2 creates, 2 enrolls, 4 agent reads, 1 reveal
		t.Fatalf("admin sees %d events", len(all))
	}
	if all[0].Action != actionReveal || all[len(all)-1].Action != actionCreate {
		t.Fatalf("events must be newest first: %s ... %s", all[0].Action, all[len(all)-1].Action)
	}
	agentReads := h.auditEvents(carol, "?actor_type=agent&actor_id="+agentA)
	if len(agentReads) != 3 || agentReads[0].TaskID == nil || *agentReads[0].TaskID != "99999999-9999-4999-8999-999999999999" {
		t.Fatalf("agentA events: %+v", agentReads)
	}

	// bob owns BOB_KEY and agentB; he sees agentA's denied read of his secret,
	// agentB's read of alice's secret, and his own actions, nothing else.
	bobView := h.auditEvents(bob, "")
	for _, ev := range bobView {
		ownSecret := ev.SecretID != nil && *ev.SecretID == b.ID
		ownActor := ev.ActorID == bob || ev.ActorID == agentB
		if !ownSecret && !ownActor {
			t.Fatalf("bob must not see %+v", ev)
		}
	}
	if len(bobView) != 5 {
		t.Fatalf("bob sees %d events, want 5", len(bobView))
	}

	page := h.auditEvents(carol, "?limit=4")
	next := h.auditEvents(carol, "?limit=4&before="+page[3].ID)
	last, _ := strconv.Atoi(page[3].ID)
	first, _ := strconv.Atoi(next[0].ID)
	if len(page) != 4 || len(next) != 4 || first >= last {
		t.Fatalf("pagination: %d then %d (%d after %d)", len(page), len(next), first, last)
	}

	var sum struct{ Agents []agentAccessSummary }
	h.browser(carol, http.MethodGet, ws("/audit/agents"), nil).json(t, &sum)
	byAgent := map[string]agentAccessSummary{}
	for _, s := range sum.Agents {
		byAgent[s.AgentID] = s
	}
	if s := byAgent[agentA]; s.ReadCount != 2 || s.DeniedCount != 1 || len(s.SecretNames) != 1 || s.SecretNames[0] != "ALICE_KEY" || s.LastReadAt == nil {
		t.Fatalf("agentA summary: %+v", s)
	}
	if s := byAgent[agentB]; s.ReadCount != 1 || s.DeniedCount != 0 {
		t.Fatalf("agentB summary: %+v", s)
	}

	var list struct{ Secrets []secretMeta }
	h.browser(alice, http.MethodGet, ws("/secrets"), nil).json(t, &list)
	for _, m := range list.Secrets {
		if m.ID == a.ID && (m.LastReadActorID == nil || *m.LastReadActorID != agentB) {
			t.Fatalf("ALICE_KEY last read: %+v", m)
		}
	}
}

func TestReadsFailClosedWithoutAudit(t *testing.T) {
	h := newHarness(t)
	m := h.create(alice, "KEY", "value-xyz", []grant{{"agent", agentA}})
	h.enroll(alice, agentA)
	tok := h.fake.agentEnv(agentA)[envToken]
	if _, err := h.s.store.db.Exec(`DROP TABLE audit`); err != nil {
		t.Fatal(err)
	}
	for _, r := range []resp{h.reveal(alice, m.ID), h.agentCall(tok, "mat_taskA", "/v1/secrets/KEY")} {
		if r.status != http.StatusInternalServerError || bytes.Contains(r.body, []byte("value-xyz")) {
			t.Fatalf("read without audit must fail closed: %d %s", r.status, r.body)
		}
	}
}

func TestCiphertextIsBoundToItsSecret(t *testing.T) {
	h := newHarness(t)
	a := h.create(alice, "A_KEY", "aaa", nil)
	b := h.create(alice, "B_KEY", "bbb", nil)
	if _, err := h.s.store.db.Exec(`UPDATE secrets SET ciphertext = (SELECT ciphertext FROM secrets WHERE id = ?) WHERE id = ?`, a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if r := h.reveal(alice, b.ID); r.status != http.StatusInternalServerError || bytes.Contains(r.body, []byte("aaa")) {
		t.Fatalf("swapped ciphertext must not open: %d %s", r.status, r.body)
	}
	if got := revealed(t, h.reveal(alice, a.ID)); got != "aaa" {
		t.Fatalf("A_KEY = %q", got)
	}
}

func TestLoadKeyRejectsReadableKeyFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "master.key")
	key := base64.StdEncoding.EncodeToString(make([]byte, keySize))
	if err := os.WriteFile(p, []byte(key), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKey(p); err == nil {
		t.Fatal("0644 key file must be rejected")
	}
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKey(p); err != nil {
		t.Fatalf("0600 key file: %v", err)
	}
	if err := os.WriteFile(p, []byte(base64.StdEncoding.EncodeToString(make([]byte, 16))), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKey(p); err == nil {
		t.Fatal("short key must be rejected")
	}
}

func TestStoreRejectsDuplicateNamesPerWorkspaceOnly(t *testing.T) {
	st, err := openStore(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	mk := func(ws string) error {
		ts := now()
		return st.createSecret(ctx, secretRow{ID: newID(), WorkspaceID: ws, Name: "SAME", OwnerID: alice, Ciphertext: []byte{1}, Version: 1,
			CreatedAt: ts, UpdatedAt: ts, UpdatedBy: alice, ValueUpdatedAt: ts}, auditEvent{WorkspaceID: ws, ActorType: "member", ActorID: alice, Action: actionCreate, Result: resultOK})
	}
	if err := mk(wsID); err != nil {
		t.Fatal(err)
	}
	if err := mk(wsID); err != errNameConflict {
		t.Fatalf("same workspace: %v", err)
	}
	if err := mk(otherWS); err != nil {
		t.Fatalf("other workspace: %v", err)
	}
}
