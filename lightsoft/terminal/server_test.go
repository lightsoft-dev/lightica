package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const (
	testWS      = "11111111-1111-1111-1111-111111111111"
	testProject = "22222222-2222-2222-2222-222222222222"
	testOrigin  = "https://team.example:3444"
)

// fakeMultica serves the four read endpoints the gateway uses. Tokens map to
// users; "admin-token" is a workspace admin, "member-token" a plain member.
func fakeMultica(t *testing.T, projectDir string) *httptest.Server {
	t.Helper()
	users := map[string]apiUser{
		"admin-token":  {ID: "u-admin", Name: "Admin", Email: "admin@example.com"},
		"member-token": {ID: "u-member", Name: "Member", Email: "member@example.com"},
	}
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if _, ok := users[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]; !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			writeJSON(w, 200, users[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")])
		}
	})
	mux.HandleFunc("GET /api/workspaces/{id}/members", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		if r.PathValue("id") != testWS {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, 200, []apiMember{{UserID: "u-admin", Role: "admin"}, {UserID: "u-member", Role: "member"}})
	})
	mux.HandleFunc("GET /api/projects/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		if r.Header.Get("X-Workspace-ID") != testWS || r.PathValue("id") != testProject {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		desc := "개요: 테스트\n로컬(사용자 작업 폴더): " + projectDir + " — 참고"
		writeJSON(w, 200, apiProject{ID: testProject, Title: "테스트 프로젝트", Description: &desc})
	})
	mux.HandleFunc("GET /api/projects/{id}/resources", func(w http.ResponseWriter, r *http.Request) {
		if auth(w, r) {
			writeJSON(w, 200, map[string]any{"resources": []any{}, "total": 0})
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

type harness struct {
	gw         *httptest.Server
	auditPath  string
	projectDir string
}

func newHarness(t *testing.T, tweak func(*Config)) *harness {
	t.Helper()
	home := t.TempDir()
	projectDir := filepath.Join(home, "lightsoft-projects", "demo")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	api := fakeMultica(t, projectDir)
	cfg := Config{
		APIURL:         api.URL,
		AllowedOrigins: map[string]bool{testOrigin: true},
		AllowedRoles:   map[string]bool{"owner": true, "admin": true},
		IdleTimeout:    time.Minute,
		MaxLifetime:    time.Hour,
		MaxPerUser:     3,
		MaxTotal:       10,
		AuditLogPath:   filepath.Join(home, "audit.log"),
		Shell:          "/bin/sh",
		Home:           home,
	}
	if tweak != nil {
		tweak(&cfg)
	}
	s := newServer(cfg)
	gw := httptest.NewServer(s.routes())
	t.Cleanup(func() {
		gw.Close()
		s.stopAll()
	})
	return &harness{gw: gw, auditPath: cfg.AuditLogPath, projectDir: projectDir}
}

func (h *harness) url(scheme, path string) string {
	return scheme + strings.TrimPrefix(h.gw.URL, "http") + PathPrefix + path + "?ws=" + testWS + "&project=" + testProject
}

func (h *harness) dial(t *testing.T, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	hdr := http.Header{"Origin": {testOrigin}}
	if token != "" {
		hdr.Set("Cookie", authCookieName+"="+token)
	}
	return websocket.DefaultDialer.Dial(h.url("ws", "/ws"), hdr)
}

func TestInfoRequiresAdmin(t *testing.T) {
	h := newHarness(t, nil)
	cases := []struct {
		token string
		want  int
	}{
		{"", http.StatusUnauthorized},
		{"bogus", http.StatusUnauthorized},
		{"member-token", http.StatusForbidden},
		{"admin-token", http.StatusOK},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest(http.MethodGet, h.url("http", "/api/info"), nil)
		if tc.token != "" {
			req.AddCookie(&http.Cookie{Name: authCookieName, Value: tc.token})
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Fatalf("token %q: status %d, want %d (%v)", tc.token, resp.StatusCode, tc.want, body)
		}
		if tc.want == http.StatusOK && body["dir"] != h.projectDir {
			t.Fatalf("dir = %v, want %s", body["dir"], h.projectDir)
		}
	}
}

func TestWebSocketRejectsForeignOriginAndMembers(t *testing.T) {
	h := newHarness(t, nil)

	hdr := http.Header{"Origin": {"https://evil.example"}, "Cookie": {authCookieName + "=admin-token"}}
	if _, resp, err := websocket.DefaultDialer.Dial(h.url("ws", "/ws"), hdr); err == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin: err=%v status=%v, want 403", err, statusOf(resp))
	}
	if _, resp, err := h.dial(t, "member-token"); err == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member: err=%v status=%v, want 403", err, statusOf(resp))
	}
	audit := readAudit(t, h.auditPath)
	if len(audit) != 2 || audit[0]["event"] != "deny" || audit[1]["user_email"] != "member@example.com" {
		t.Fatalf("audit = %v", audit)
	}
}

// TestSessionRunsInProjectDirAndCleansUp opens a real shell, runs commands
// in the project folder, starts a background job, closes the browser side,
// and checks that the job is gone and the session is audited.
func TestSessionRunsInProjectDirAndCleansUp(t *testing.T) {
	h := newHarness(t, nil)
	conn, _, err := h.dial(t, "admin-token")
	if err != nil {
		t.Fatal(err)
	}
	r := newReader(t, conn)
	r.waitControl("ready")

	send(t, conn, "pwd; echo sum-$((40+2)); sleep 300 & echo bgpid-$!\n")
	out := r.waitOutput(regexp.MustCompile(`bgpid-(\d+)`))
	if !strings.Contains(out, h.projectDir) || !strings.Contains(out, "sum-42") {
		t.Fatalf("unexpected output: %q", out)
	}
	pid, _ := strconv.Atoi(regexp.MustCompile(`bgpid-(\d+)`).FindStringSubmatch(out)[1])
	if syscall.Kill(pid, 0) != nil {
		t.Fatalf("background job %d not running", pid)
	}

	conn.Close()
	closed := time.Now()
	deadline := time.Now().Add(3 * hangupGrace)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("background job %d survived the closed terminal", pid)
		}
		time.Sleep(100 * time.Millisecond)
	}

	waitFor(t, func() bool { return len(readAudit(t, h.auditPath)) == 2 })
	// The shell answers SIGHUP, so cleanup must not wait out the kill grace
	// period (regression: an undrained pty kept the exiting shell alive).
	if took := time.Since(closed); took >= hangupGrace {
		t.Fatalf("session cleanup took %s", took)
	}
	audit := readAudit(t, h.auditPath)
	if audit[0]["event"] != "open" || audit[1]["event"] != "close" ||
		audit[1]["reason"] != "client_closed" || audit[1]["dir"] != h.projectDir {
		t.Fatalf("audit = %v", audit)
	}
}

func TestShellExitEndsSession(t *testing.T) {
	h := newHarness(t, nil)
	conn, _, err := h.dial(t, "admin-token")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r := newReader(t, conn)
	r.waitControl("ready")
	send(t, conn, "exit 3\n")
	msg := r.waitControl("exit")
	if msg["code"] != float64(3) {
		t.Fatalf("exit message = %v", msg)
	}
}

func TestIdleTimeoutClosesSession(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.IdleTimeout = time.Second })
	conn, _, err := h.dial(t, "admin-token")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r := newReader(t, conn)
	r.waitControl("ready")
	if msg := r.waitControl("closing"); msg["reason"] != "idle" {
		t.Fatalf("closing message = %v", msg)
	}
}

func TestPerUserLimit(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.MaxPerUser = 1 })
	first, _, err := h.dial(t, "admin-token")
	if err != nil {
		t.Fatal(err)
	}
	newReader(t, first).waitControl("ready")
	if _, resp, err := h.dial(t, "admin-token"); err == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second session: err=%v status=%v, want 429", err, statusOf(resp))
	}
	first.Close()
	waitFor(t, func() bool {
		c, _, err := h.dial(t, "admin-token")
		if err != nil {
			return false
		}
		c.Close()
		return true
	})
}

type wsReader struct {
	t      *testing.T
	ch     chan wsFrame
	output strings.Builder
}

type wsFrame struct {
	kind int
	data []byte
	err  error
}

func newReader(t *testing.T, conn *websocket.Conn) *wsReader {
	r := &wsReader{t: t, ch: make(chan wsFrame, 256)}
	go func() {
		for {
			kind, data, err := conn.ReadMessage()
			r.ch <- wsFrame{kind, data, err}
			if err != nil {
				return
			}
		}
	}()
	return r
}

func (r *wsReader) next() wsFrame {
	r.t.Helper()
	select {
	case f := <-r.ch:
		if f.err != nil {
			r.t.Fatalf("connection ended: %v (output so far %q)", f.err, r.output.String())
		}
		return f
	case <-time.After(15 * time.Second):
		r.t.Fatalf("timed out (output so far %q)", r.output.String())
		return wsFrame{}
	}
}

func (r *wsReader) waitControl(kind string) map[string]any {
	r.t.Helper()
	for {
		f := r.next()
		if f.kind == websocket.BinaryMessage {
			r.output.Write(f.data)
			continue
		}
		var msg map[string]any
		if json.Unmarshal(f.data, &msg) == nil && msg["type"] == kind {
			return msg
		}
	}
}

func (r *wsReader) waitOutput(re *regexp.Regexp) string {
	r.t.Helper()
	for !re.MatchString(r.output.String()) {
		if f := r.next(); f.kind == websocket.BinaryMessage {
			r.output.Write(f.data)
		}
	}
	return r.output.String()
}

func send(t *testing.T, conn *websocket.Conn, s string) {
	t.Helper()
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte(s)); err != nil {
		t.Fatal(err)
	}
}

func readAudit(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec map[string]any
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatal(err)
		}
		out = append(out, rec)
	}
	return out
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func statusOf(resp *http.Response) any {
	if resp == nil {
		return nil
	}
	return resp.StatusCode
}
