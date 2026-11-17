package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

//go:embed web
var webFS embed.FS

const (
	pingInterval = 30 * time.Second
	readTimeout  = 75 * time.Second
	writeTimeout = 10 * time.Second
	maxCols      = 1000
	maxRows      = 500
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type server struct {
	cfg   Config
	api   *multicaClient
	audit *auditLog

	mu       sync.Mutex
	perUser  map[string]int
	total    int
	sessions map[*shellSession]struct{}

	upgrader websocket.Upgrader
}

func newServer(cfg Config) *server {
	return &server{
		cfg:      cfg,
		api:      newMulticaClient(cfg.APIURL),
		audit:    &auditLog{path: cfg.AuditLogPath},
		perUser:  map[string]int{},
		sessions: map[*shellSession]struct{}{},
		upgrader: websocket.Upgrader{
			ReadBufferSize:  32 * 1024,
			WriteBufferSize: 32 * 1024,
			// The Origin is checked in handleWS before authorization.
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
}

func (s *server) routes() http.Handler {
	static, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+PathPrefix+"/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.Handle("GET "+PathPrefix+"/static/", http.StripPrefix(PathPrefix+"/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET "+PathPrefix+"/p/{project}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFileFS(w, r, static, "index.html")
	})
	mux.HandleFunc("GET "+PathPrefix+"/api/info", s.handleInfo)
	mux.HandleFunc("GET "+PathPrefix+"/ws", s.handleWS)
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// access is an authorized request to open a terminal in one project.
type access struct {
	user        apiUser
	role        string
	workspaceID string
	project     apiProject
	dir         string
	source      string
}

type denial struct {
	status int
	msg    string
	user   *apiUser
}

func (s *server) authorize(ctx context.Context, r *http.Request) (*access, *denial) {
	q := r.URL.Query()
	wsID, projectID := q.Get("ws"), q.Get("project")
	if !uuidPattern.MatchString(wsID) || !uuidPattern.MatchString(projectID) {
		return nil, &denial{status: http.StatusBadRequest, msg: "워크스페이스나 프로젝트 ID가 올바르지 않습니다."}
	}
	token := credentialFromRequest(r)
	if token == "" {
		return nil, &denial{status: http.StatusUnauthorized, msg: "Multica에 로그인한 브라우저에서 열어 주세요."}
	}
	user, err := s.api.me(ctx, token)
	if err != nil {
		return nil, apiDenial(err)
	}
	members, err := s.api.members(ctx, token, wsID)
	if err != nil {
		d := apiDenial(err)
		d.user = &user
		return nil, d
	}
	role := ""
	for _, m := range members {
		if m.UserID == user.ID {
			role = m.Role
			break
		}
	}
	if !s.cfg.AllowedRoles[role] {
		return nil, &denial{status: http.StatusForbidden, msg: "워크스페이스 owner/admin만 터미널을 열 수 있습니다.", user: &user}
	}
	project, err := s.api.project(ctx, token, wsID, projectID)
	if err != nil {
		d := apiDenial(err)
		d.user = &user
		return nil, d
	}
	resources, err := s.api.resources(ctx, token, wsID, projectID)
	if err != nil {
		// The description still names the folder; resources are a refinement.
		log.Printf("project %s resources: %v", projectID, err)
	}
	description := ""
	if project.Description != nil {
		description = *project.Description
	}
	dir, source := resolveStartDir(s.cfg.Home, s.cfg.DaemonID, resources, description)
	return &access{user: user, role: role, workspaceID: wsID, project: project, dir: dir, source: source}, nil
}

func apiDenial(err error) *denial {
	switch {
	case errors.Is(err, errUnauthenticated):
		return &denial{status: http.StatusUnauthorized, msg: "로그인이 만료됐습니다. 팀 클라이언트에서 다시 로그인해 주세요."}
	case errors.Is(err, errNotFound):
		return &denial{status: http.StatusNotFound, msg: "워크스페이스나 프로젝트를 찾을 수 없습니다."}
	default:
		log.Printf("multica api: %v", err)
		return &denial{status: http.StatusBadGateway, msg: "Multica API에 연결하지 못했습니다."}
	}
}

func (s *server) handleInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a, d := s.authorize(r.Context(), r)
	if d != nil {
		writeJSON(w, d.status, map[string]string{"error": d.msg})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":                 map[string]string{"name": a.user.Name, "email": a.user.Email},
		"project":              map[string]string{"id": a.project.ID, "title": a.project.Title},
		"dir":                  a.dir,
		"dir_source":           a.source,
		"idle_timeout_seconds": int(s.cfg.IdleTimeout.Seconds()),
		"max_lifetime_seconds": int(s.cfg.MaxLifetime.Seconds()),
	})
}

func (s *server) handleWS(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" && !s.cfg.AllowedOrigins[origin] {
		s.deny(w, r, &denial{status: http.StatusForbidden, msg: "허용되지 않은 출처입니다."}, nil)
		return
	}
	a, d := s.authorize(r.Context(), r)
	if d != nil {
		s.deny(w, r, d, nil)
		return
	}
	if !s.reserve(a.user.ID) {
		s.deny(w, r, &denial{status: http.StatusTooManyRequests, msg: "열 수 있는 터미널 수를 넘었습니다. 다른 터미널 탭을 닫고 다시 시도해 주세요.", user: &a.user}, a)
		return
	}
	defer s.release(a.user.ID)

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	cols, rows := sizeParam(r, "cols", 80, maxCols), sizeParam(r, "rows", 24, maxRows)
	env := shellEnv(processEnv(), s.cfg.Home, s.cfg.Shell, a.dir, a.user.Email)
	sh, err := startShell(s.cfg.Shell, a.dir, env, cols, rows)
	if err != nil {
		log.Printf("start shell: %v", err)
		_ = conn.WriteJSON(map[string]any{"type": "error", "message": "셸을 시작하지 못했습니다."})
		return
	}
	s.track(sh, true)
	defer s.track(sh, false)

	id := newSessionID()
	started := time.Now()
	base := auditFields(r, a)
	base["session"] = id
	s.audit.write("open", base)

	reason := s.serveSession(conn, sh, a)

	sh.stop()
	closeFields := auditFields(r, a)
	closeFields["session"] = id
	closeFields["reason"] = reason
	closeFields["exit_code"] = sh.exitCode()
	closeFields["duration_seconds"] = int(time.Since(started).Seconds())
	s.audit.write("close", closeFields)
}

// serveSession pumps bytes between the WebSocket and the pty until the shell
// exits, the client leaves, or a timeout fires, and returns why it ended.
func (s *server) serveSession(conn *websocket.Conn, sh *shellSession, a *access) string {
	var writeMu sync.Mutex
	write := func(kind int, data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
		return conn.WriteMessage(kind, data)
	}
	writeJSONMsg := func(v any) {
		b, _ := json.Marshal(v)
		_ = write(websocket.TextMessage, b)
	}

	writeJSONMsg(map[string]any{
		"type":    "ready",
		"dir":     a.dir,
		"source":  a.source,
		"project": a.project.Title,
	})

	clientGone := make(chan struct{})
	go func() {
		defer close(clientGone)
		_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
		conn.SetPongHandler(func(string) error {
			return conn.SetReadDeadline(time.Now().Add(readTimeout))
		})
		for {
			kind, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
			switch kind {
			case websocket.BinaryMessage:
				sh.touch()
				if _, err := sh.ptmx.Write(data); err != nil {
					return
				}
			case websocket.TextMessage:
				var msg struct {
					Type string `json:"type"`
					Cols int    `json:"cols"`
					Rows int    `json:"rows"`
				}
				if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" &&
					msg.Cols > 0 && msg.Cols <= maxCols && msg.Rows > 0 && msg.Rows <= maxRows {
					_ = sh.resize(uint16(msg.Cols), uint16(msg.Rows))
				}
			}
		}
	}()

	// The pump reads until the pty closes, discarding output once the client
	// is gone: on macOS an exiting shell blocks until its terminal output is
	// drained, so it would otherwise outlive SIGKILL until the pty is closed.
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		buf := make([]byte, 32*1024)
		clientOK := true
		for {
			n, err := sh.ptmx.Read(buf)
			if n > 0 && clientOK {
				sh.touch()
				clientOK = write(websocket.BinaryMessage, buf[:n]) == nil
			}
			if err != nil {
				return
			}
		}
	}()

	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	idleCheck := time.NewTicker(min(15*time.Second, s.cfg.IdleTimeout/2))
	defer idleCheck.Stop()
	lifetime := time.NewTimer(s.cfg.MaxLifetime)
	defer lifetime.Stop()

	end := func(reason, message string) string {
		writeJSONMsg(map[string]any{"type": "closing", "reason": reason, "message": message})
		return reason
	}
	for {
		select {
		case <-clientGone:
			return "client_closed"
		case <-sh.exited:
			// Flush output the shell wrote just before exiting.
			select {
			case <-outputDone:
			case <-time.After(time.Second):
			}
			writeJSONMsg(map[string]any{"type": "exit", "code": sh.exitCode()})
			return "shell_exited"
		case <-lifetime.C:
			return end("max_lifetime", "최대 사용 시간("+s.cfg.MaxLifetime.String()+")이 지나 터미널을 닫습니다.")
		case now := <-idleCheck.C:
			if sh.idleFor(now) >= s.cfg.IdleTimeout {
				return end("idle", s.cfg.IdleTimeout.String()+" 동안 입출력이 없어 터미널을 닫습니다.")
			}
		case <-ping.C:
			writeMu.Lock()
			err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeTimeout))
			writeMu.Unlock()
			if err != nil {
				return "client_closed"
			}
		}
	}
}

func (s *server) deny(w http.ResponseWriter, r *http.Request, d *denial, a *access) {
	fields := map[string]any{
		"status":          d.status,
		"reason":          d.msg,
		"workspace_id":    r.URL.Query().Get("ws"),
		"project_id":      r.URL.Query().Get("project"),
		"tailscale_login": r.Header.Get("Tailscale-User-Login"),
	}
	if d.user != nil {
		fields["user_id"], fields["user_email"] = d.user.ID, d.user.Email
	}
	if a != nil {
		fields["dir"] = a.dir
	}
	s.audit.write("deny", fields)
	writeJSON(w, d.status, map[string]string{"error": d.msg})
}

func auditFields(r *http.Request, a *access) map[string]any {
	return map[string]any{
		"user_id":         a.user.ID,
		"user_email":      a.user.Email,
		"role":            a.role,
		"workspace_id":    a.workspaceID,
		"project_id":      a.project.ID,
		"project_title":   a.project.Title,
		"dir":             a.dir,
		"dir_source":      a.source,
		"tailscale_login": r.Header.Get("Tailscale-User-Login"),
	}
}

func (s *server) reserve(userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.total >= s.cfg.MaxTotal || s.perUser[userID] >= s.cfg.MaxPerUser {
		return false
	}
	s.total++
	s.perUser[userID]++
	return true
}

func (s *server) release(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total--
	if s.perUser[userID]--; s.perUser[userID] <= 0 {
		delete(s.perUser, userID)
	}
}

func (s *server) track(sh *shellSession, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if add {
		s.sessions[sh] = struct{}{}
	} else {
		delete(s.sessions, sh)
	}
}

// stopAll ends every open session; used on gateway shutdown so no shell
// outlives the process that was supervising it.
func (s *server) stopAll() {
	s.mu.Lock()
	open := make([]*shellSession, 0, len(s.sessions))
	for sh := range s.sessions {
		open = append(open, sh)
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	for _, sh := range open {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sh.stop()
		}()
	}
	wg.Wait()
}

func sizeParam(r *http.Request, key string, fallback, limit int) uint16 {
	n, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil || n <= 0 || n > limit {
		return uint16(fallback)
	}
	return uint16(n)
}

func newSessionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// processEnv is a seam for tests.
var processEnv = os.Environ
