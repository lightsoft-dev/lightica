package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/multica-ai/multica/server/internal/lightsoftguest"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Outside collaborators (Lightsoft fork, LIGH-282).
//
// A guest is a workspace member whose email the workspace lists under
// lightsoftguest.SettingsKey. What narrows a guest, in one place each:
//
//   - visibility: projectaccess hides every project the guest is not a
//     project member of, and everything outside a project;
//   - API surface: LightsoftGuestGate below admits a guest's request to their
//     guest workspace only on guestRoutes, an allow-list, so an endpoint added
//     upstream stays closed to guests until someone opens it here;
//   - agent runs: invokeAgentDecision refuses every run a guest would start;
//   - realtime: the hub keeps guests out of the workspace room and
//     cmd/server's guest fanout sends them only their projects' issue events.

const guestDeniedMessage = "not available to outside collaborators"

func (h *Handler) lightsoftGuestDirectory() *lightsoftguest.Directory {
	if h == nil || h.Queries == nil {
		return nil
	}
	return lightsoftguest.Shared(h.Queries)
}

// isLightsoftGuestRequest reports whether the request's user is a guest of
// workspaceID. It answers true when the lookup fails: callers use it to
// decide whether a visibility check may be skipped, so true only costs the
// full check.
func (h *Handler) isLightsoftGuestRequest(r *http.Request, workspaceID string) bool {
	guest, err := h.lightsoftGuestDirectory().IsGuest(r.Context(), workspaceID, requestUserID(r))
	if err != nil {
		slog.Warn("lightsoft guest lookup failed", append(logger.RequestAttrs(r), "workspace_id", workspaceID, "error", err)...)
		return true
	}
	return guest
}

// lightsoftGuestBlocksInvoke reports whether userID may not start agent runs
// in workspaceID because they are a guest. A failed lookup blocks.
func (h *Handler) lightsoftGuestBlocksInvoke(ctx context.Context, workspaceID, userID string) bool {
	if userID == "" {
		return false
	}
	guest, err := h.lightsoftGuestDirectory().IsGuest(ctx, workspaceID, userID)
	if err != nil {
		slog.WarnContext(ctx, "lightsoft guest lookup failed; refusing agent invocation", "workspace_id", workspaceID, "error", err)
		return true
	}
	return guest
}

// guestRoute is one entry of the allow-list a guest's request must match.
type guestRoute struct {
	method string
	path   *regexp.Regexp
	// check, when set, inspects the request and writes the refusal itself.
	check func(w http.ResponseWriter, r *http.Request) bool
	// scrub, when set, rewrites a successful JSON response.
	scrub func(h *Handler, r *http.Request, workspaceID string, body []byte) []byte
	// canned, when set, answers without calling the handler: the empty result
	// of a list the client loads on every page but a guest may not read.
	canned       string
	cannedStatus int
}

func route(method, pattern string) guestRoute {
	return guestRoute{method: method, path: regexp.MustCompile("^" + pattern + "$")}
}

func (g guestRoute) withCheck(fn func(w http.ResponseWriter, r *http.Request) bool) guestRoute {
	g.check = fn
	return g
}

func (g guestRoute) answer(status int, body string) guestRoute {
	g.cannedStatus, g.canned = status, body
	return g
}

func (g guestRoute) withScrub(fn func(h *Handler, r *http.Request, workspaceID string, body []byte) []byte) guestRoute {
	g.scrub = fn
	return g
}

const seg = `[^/]+`

// guestAccountRoutes are the user-scoped routes (no workspace target) open to
// someone who is a guest anywhere. Everything else without a workspace target
// is refused, since such a handler resolves the workspace on its own.
var guestAccountRoutes = []guestRoute{
	route("GET", `/api/me`),
	route("PATCH", `/api/me`),
	route("PATCH", `/api/me/onboarding`),
	route("POST", `/api/me/onboarding/complete`),
	route("POST", `/api/me/whats-new/seen`),
	route("POST", `/api/me/star-prompt/(claim|starred)`),
	route("POST", `/api/cli-token`),
	route("POST", `/api/auth/refresh`),
	route("POST", `/api/client-usage`),
	route("GET", `/api/theme-store/themes(/.*)?`),
	route("GET", `/api/workspaces`).withScrub(scrubGuestWorkspaceList),
	route("GET", `/api/invitations`),
	route("GET", `/api/invitations/`+seg),
	route("POST", `/api/invitations/`+seg+`/(accept|decline)`),
	route("GET", `/api/tokens`),
	route("POST", `/api/tokens`),
	route("POST", `/api/tokens/current/renew`),
	route("DELETE", `/api/tokens/`+seg),
	// Attachment routes resolve the workspace from the row and apply the
	// guest's hidden projects there.
	route("GET", `/api/attachments/`+seg+`/(download|thumbnail|original|download-metadata)`),
}

// guestRoutes are the routes a guest may call in their guest workspace.
var guestRoutes = []guestRoute{
	route("GET", `/api/workspaces/`+seg).withScrub(scrubGuestWorkspace),
	route("GET", `/api/workspaces/`+seg+`/members`).withScrub(scrubGuestMembers),
	route("GET", `/api/workspaces/`+seg+`/features`),
	route("POST", `/api/workspaces/`+seg+`/leave`),
	route("PUT", `/api/workspaces/`+seg+`/pin`),
	route("POST", `/api/upload-file`),
	route("GET", `/api/lightsoft/capabilities`),

	route("GET", `/api/projects`),
	route("GET", `/api/projects/search`),
	route("GET", `/api/projects/`+seg),
	route("GET", `/api/projects/`+seg+`/members`).withScrub(scrubGuestProjectMembers),
	route("GET", `/api/projects/`+seg+`/subscription`),
	route("PUT", `/api/projects/`+seg+`/subscription`),
	route("DELETE", `/api/projects/`+seg+`/subscription`),
	route("GET", `/api/projects/`+seg+`/resources`),

	route("GET", `/api/issues`),
	route("GET", `/api/issues/(search|child-progress|pull-requests|children|grouped|unread|subscribed)`),
	route("POST", `/api/issues/table/(groups|rows|facets)`),
	route("POST", `/api/issues/query`),
	route("POST", `/api/issues`).withCheck(checkGuestIssueCreate),
	route("GET", `/api/issues/`+seg),
	route("PUT", `/api/issues/`+seg).withCheck(checkGuestIssueUpdate),
	route("POST", `/api/issues/`+seg+`/move`).withCheck(checkGuestIssueUpdate),
	route("GET", `/api/issues/`+seg+`/(comments|timeline|subscribers|attachments|children|duplicates|labels|metadata|pull-requests)`),
	route("POST", `/api/issues/`+seg+`/comments`),
	route("POST", `/api/issues/`+seg+`/(subscribe|unsubscribe|unsubscribe/subtree|unsubscribe/undo|read|unread)`),
	route("POST", `/api/issues/`+seg+`/reactions`),
	route("DELETE", `/api/issues/`+seg+`/reactions`),
	route("POST", `/api/issues/`+seg+`/labels`),
	route("DELETE", `/api/issues/`+seg+`/labels/`+seg),
	route("PUT", `/api/issues/`+seg+`/properties/`+seg),
	route("DELETE", `/api/issues/`+seg+`/properties/`+seg),

	route("PUT", `/api/comments/`+seg),
	route("DELETE", `/api/comments/`+seg),
	route("DELETE", `/api/comments/`+seg+`/keep-replies`),
	route("POST", `/api/comments/`+seg+`/(resolve|reactions)`),
	route("DELETE", `/api/comments/`+seg+`/(resolve|reactions)`),

	route("GET", `/api/attachments/`+seg),
	route("GET", `/api/attachments/`+seg+`/content`),
	route("DELETE", `/api/attachments/`+seg),

	route("GET", `/api/labels`),
	route("GET", `/api/labels/`+seg),
	route("GET", `/api/properties`),
	route("GET", `/api/properties/`+seg),
	route("GET", `/api/issue-statuses`),
	route("GET", `/api/issue-workflows`),
	route("GET", `/api/issue-workflows/`+seg),
	route("GET", `/api/issue-views`),
	route("GET", `/api/issue-views/`+seg),
	route("GET", `/api/issue-views/`+seg+`/issues`),
	route("GET", `/api/issue-view-preferences`),
	route("PUT", `/api/issue-view-preferences`),
	route("GET", `/api/assignee-frequency`),

	route("GET", `/api/pins`),
	route("POST", `/api/pins`),
	route("PUT", `/api/pins/reorder`),
	route("DELETE", `/api/pins/`+seg+`/`+seg),

	route("GET", `/api/inbox(/.*)?`),
	route("POST", `/api/inbox/.+`),
	route("DELETE", `/api/inbox/`+seg),
	route("GET", `/api/notification-preferences`),
	route("PATCH", `/api/notification-preferences`),
	route("PUT", `/api/notification-preferences`),

	// Names and avatars only, so issues, comments and assignees render.
	route("GET", `/api/agents`).withScrub(scrubGuestAgents),
	route("GET", `/api/squads`).withScrub(scrubGuestSquads),

	// Lists the client prefetches on every page: empty for a guest, so the
	// shell renders instead of retrying errors. Shapes match an empty
	// workspace's real responses.
	route("GET", `/api/runtimes`).answer(http.StatusOK, `[]`),
	route("GET", `/api/skills`).answer(http.StatusOK, `[]`),
	route("GET", `/api/project-folders`).answer(http.StatusOK, `{"folders":[],"filed":[]}`),
	route("GET", `/api/chat/sessions`).answer(http.StatusOK, `[]`),
	route("GET", `/api/chat/pending-tasks`).answer(http.StatusOK, `{"tasks":[]}`),
	route("GET", `/api/chat/pending-tasks/has-any`).answer(http.StatusOK, `{"has_pending":false}`),
	route("GET", `/api/autopilots`).answer(http.StatusOK, `{"autopilots":[],"total":0}`),
	route("GET", `/api/(agent-task-snapshot|agent-run-counts|agent-activity-30d|working-agents)`).answer(http.StatusOK, `[]`),
	route("GET", `/api/(issue-wakeup-summaries|issue-wakeup-paused)`).answer(http.StatusOK, `[]`),
	route("GET", `/api/quick-actions`).answer(http.StatusOK, `{"quick_actions":[]}`),
	route("GET", `/api/issues/`+seg+`/(wakeups|task-runs|system-wakeups)`).answer(http.StatusOK, `[]`),
	route("GET", `/api/issues/`+seg+`/active-task`).answer(http.StatusOK, `{"tasks":[]}`),
	route("GET", `/api/issues/`+seg+`/environments`).answer(http.StatusOK, `{"environments":[],"configs":[],"pending_starts":[],"failed_starts":[]}`),
	route("GET", `/api/issues/`+seg+`/workflow-handoff`).answer(http.StatusOK, `{"handoff":false,"from_status":"","to_status":"","previous_assignee_type":null,"previous_assignee_id":null,"previous_runs":[]}`),
	// What the server answers when the local index is off: the client then
	// searches through /api/issues/search, which applies the guest's view.
	route("GET", `/api/search-index/.+`).answer(http.StatusNotFound, `{"error":"local search index is disabled"}`),
	route("POST", `/api/search-index/changes`).answer(http.StatusNotFound, `{"error":"local search index is disabled"}`),
}

func matchGuestRoute(routes []guestRoute, method, path string) (guestRoute, bool) {
	for _, g := range routes {
		if g.method == method && g.path.MatchString(path) {
			return g, true
		}
	}
	return guestRoute{}, false
}

var workspacePathRe = regexp.MustCompile(`^/api/workspaces/([^/]+)`)

// guestAnywhereDenied are user-scoped routes that resolve their workspace from
// a token or row and lead to agent runs or connected accounts: refused to
// anyone who is a guest of any workspace, whatever workspace they name.
var guestAnywhereDenied = regexp.MustCompile(`^/api/((lark|slack|dingtalk|wecom|telegram)/binding/redeem|integrations/composio(/.*)?)$`)

// lightsoftFullMemberOfAll reports whether userID is a member, and not a
// guest, of every workspace in targets.
func (h *Handler) lightsoftFullMemberOfAll(r *http.Request, userID string, targets []string) bool {
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		return false
	}
	for _, id := range targets {
		wsUUID, err := util.ParseUUID(id)
		if err != nil {
			return false
		}
		if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: userUUID, WorkspaceID: wsUUID}); err != nil {
			return false
		}
		if guest, err := h.lightsoftGuestDirectory().IsGuest(r.Context(), id, userID); err != nil || guest {
			return false
		}
	}
	return true
}

// lightsoftRequestWorkspaces returns every workspace the request names, by
// any of the means the workspace middleware or a handler may read: the URL
// path, the slug header or query, the id header or query. The gate applies
// the guest rules when any of them is a guest workspace, so a request cannot
// slip past by naming two workspaces and relying on which one wins.
func (h *Handler) lightsoftRequestWorkspaces(r *http.Request) []string {
	var out []string
	add := func(id string) {
		if id != "" {
			out = append(out, lightsoftguest.CanonicalID(id))
		}
	}
	if m := workspacePathRe.FindStringSubmatch(r.URL.EscapedPath()); m != nil {
		add(m[1])
	}
	for _, slug := range []string{r.Header.Get("X-Workspace-Slug"), r.URL.Query().Get("workspace_slug")} {
		if slug == "" {
			continue
		}
		if ws, err := h.Queries.GetWorkspaceBySlug(r.Context(), slug); err == nil {
			add(util.UUIDToString(ws.ID))
		}
	}
	add(r.Header.Get("X-Workspace-ID"))
	add(r.URL.Query().Get("workspace_id"))
	return out
}

// LightsoftGuestGate narrows the API for guests (see the package comment
// above). Requests from anyone who is not a guest anywhere pass untouched
// after one cached lookup.
func (h *Handler) LightsoftGuestGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := requestUserID(r)
		dir := h.lightsoftGuestDirectory()
		if userID == "" || dir == nil {
			next.ServeHTTP(w, r)
			return
		}
		guestOf, err := dir.GuestWorkspaces(r.Context(), userID)
		if err != nil {
			slog.Warn("lightsoft guest lookup failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusServiceUnavailable, "failed to resolve access")
			return
		}
		if len(guestOf) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		// The escaped form is what chi routes on when the path carries
		// escapes (%2F), so the gate judges the same route the router picks.
		path := r.URL.EscapedPath()
		if len(path) > 1 {
			path = strings.TrimSuffix(path, "/")
		}
		if guestAnywhereDenied.MatchString(path) {
			writeError(w, http.StatusForbidden, guestDeniedMessage)
			return
		}
		targets := h.lightsoftRequestWorkspaces(r)
		guestWorkspace := ""
		for _, id := range targets {
			if guestOf[id] {
				guestWorkspace = id
				break
			}
		}
		var allowed guestRoute
		var ok bool
		switch {
		case guestWorkspace != "":
			allowed, ok = matchGuestRoute(guestRoutes, r.Method, path)
			if !ok {
				// Clients send the workspace header on account routes too.
				allowed, ok = matchGuestRoute(guestAccountRoutes, r.Method, path)
			}
		case len(targets) == 0:
			allowed, ok = matchGuestRoute(guestAccountRoutes, r.Method, path)
		case h.lightsoftFullMemberOfAll(r, userID, targets):
			// Every workspace the request names is one where this user is a
			// full member; that workspace's own checks apply.
			next.ServeHTTP(w, r)
			return
		default:
			// A misspelt, foreign or unknown workspace id: judged as no
			// workspace, since a handler may resolve its own from a row.
			allowed, ok = matchGuestRoute(guestAccountRoutes, r.Method, path)
		}
		if !ok {
			writeError(w, http.StatusForbidden, guestDeniedMessage)
			return
		}
		if allowed.check != nil && !allowed.check(w, r) {
			return
		}
		if allowed.canned != "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(allowed.cannedStatus)
			_, _ = w.Write([]byte(allowed.canned))
			return
		}
		if allowed.scrub == nil {
			next.ServeHTTP(w, r)
			return
		}
		buf := &bufferedResponse{header: http.Header{}, status: http.StatusOK}
		next.ServeHTTP(buf, r)
		body := buf.body.Bytes()
		if buf.status >= 200 && buf.status < 300 && len(body) > 0 {
			scrubbed := allowed.scrub(h, r, guestWorkspace, body)
			if scrubbed == nil {
				writeError(w, http.StatusInternalServerError, "failed to prepare response")
				return
			}
			body = scrubbed
		}
		for k, v := range buf.header {
			w.Header()[k] = v
		}
		w.Header().Del("Content-Length")
		w.WriteHeader(buf.status)
		_, _ = w.Write(body)
	})
}

// LightsoftGuestDeny refuses a route group to anyone who is a guest of any
// workspace: daemon registration and the plugin bridge resolve their
// workspace on their own, and guests have no use for either.
func (h *Handler) LightsoftGuestDeny(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := requestUserID(r)
		if userID != "" {
			guestOf, err := h.lightsoftGuestDirectory().GuestWorkspaces(r.Context(), userID)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "failed to resolve access")
				return
			}
			if len(guestOf) > 0 {
				writeError(w, http.StatusForbidden, guestDeniedMessage)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
	wrote  bool
}

func (b *bufferedResponse) Header() http.Header { return b.header }
func (b *bufferedResponse) WriteHeader(status int) {
	if !b.wrote {
		b.status, b.wrote = status, true
	}
}
func (b *bufferedResponse) Write(p []byte) (int, error) {
	b.wrote = true
	return b.body.Write(p)
}

const maxGuestBody = 8 << 20

// peekJSONBody reads the request body as a JSON object and puts it back.
func peekJSONBody(w http.ResponseWriter, r *http.Request) (map[string]json.RawMessage, bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxGuestBody+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return nil, false
	}
	if len(raw) > maxGuestBody {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return nil, false
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var body map[string]json.RawMessage
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]json.RawMessage{}, true
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		// Let the handler answer malformed JSON as it always does.
		return map[string]json.RawMessage{}, true
	}
	// encoding/json fills a struct field from any case-folded spelling of
	// its key, and the last one wins, so {"project_id":"p","PROJECT_ID":null}
	// would pass a check of "project_id" and still clear the project.
	for key := range body {
		for _, name := range guestCheckedFields {
			if key != name && strings.EqualFold(key, name) {
				writeError(w, http.StatusBadRequest, "invalid field "+name)
				return nil, false
			}
		}
	}
	return body, true
}

var guestCheckedFields = []string{"project_id", "assignee_type", "assignee_id"}

func guestJSONString(raw json.RawMessage) (string, bool) {
	var s *string
	if json.Unmarshal(raw, &s) != nil || s == nil {
		return "", false
	}
	return *s, true
}

// refuseGuestAgentAssignee rejects handing an issue to an agent or squad:
// that would start a run on a teammate's machine.
func refuseGuestAgentAssignee(w http.ResponseWriter, body map[string]json.RawMessage) bool {
	if raw, ok := body["assignee_type"]; ok {
		if t, _ := guestJSONString(raw); t == "agent" || t == "squad" {
			writeError(w, http.StatusForbidden, "outside collaborators cannot assign issues to agents")
			return false
		}
	}
	return true
}

// checkGuestIssueCreate requires a project: an issue outside every project
// would be invisible to its own guest author and visible to the whole team.
func checkGuestIssueCreate(w http.ResponseWriter, r *http.Request) bool {
	body, ok := peekJSONBody(w, r)
	if !ok {
		return false
	}
	if raw, ok := body["project_id"]; !ok || func() bool { s, ok := guestJSONString(raw); return !ok || s == "" }() {
		writeError(w, http.StatusBadRequest, "project_id is required for outside collaborators")
		return false
	}
	return refuseGuestAgentAssignee(w, body)
}

// checkGuestIssueUpdate refuses taking an issue out of its project.
func checkGuestIssueUpdate(w http.ResponseWriter, r *http.Request) bool {
	body, ok := peekJSONBody(w, r)
	if !ok {
		return false
	}
	if raw, ok := body["project_id"]; ok {
		if s, ok := guestJSONString(raw); !ok || s == "" {
			writeError(w, http.StatusBadRequest, "outside collaborators cannot remove an issue from its project")
			return false
		}
	}
	return refuseGuestAgentAssignee(w, body)
}

// --- response scrubbers ---

// zeroJSON replaces a value with the empty value of its JSON type, so a
// client schema still parses the object while its content is gone.
func zeroJSON(v any) any {
	switch v.(type) {
	case string:
		return ""
	case float64:
		return 0
	case bool:
		return false
	case []any:
		return []any{}
	case map[string]any:
		return map[string]any{}
	default:
		return nil
	}
}

func keepOnly(obj map[string]any, keep map[string]bool) {
	for k, v := range obj {
		if !keep[k] {
			obj[k] = zeroJSON(v)
		}
	}
}

// eachObject applies fn to every object of a top-level array, or of the
// array under listKey when the response is an object.
func eachObject(body []byte, listKey string, fn func(map[string]any)) []byte {
	var top any
	if json.Unmarshal(body, &top) != nil {
		return nil
	}
	var list []any
	switch t := top.(type) {
	case []any:
		list = t
	case map[string]any:
		if l, ok := t[listKey].([]any); ok {
			list = l
		}
	}
	for _, item := range list {
		if obj, ok := item.(map[string]any); ok {
			fn(obj)
		}
	}
	out, err := json.Marshal(top)
	if err != nil {
		return nil
	}
	return out
}

func (h *Handler) guestUserIDs(ctx context.Context, workspaceID string) map[string]bool {
	ids := map[string]bool{}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return ids
	}
	rows, err := h.Queries.ListLightsoftGuestProjectMemberships(ctx, wsUUID)
	if err != nil {
		return ids
	}
	for _, row := range rows {
		ids[util.UUIDToString(row.UserID)] = true
	}
	return ids
}

// scrubGuestMembers drops emails, and shows guests (the caller included) as
// role "guest" so sidecar services and the team client can tell.
func scrubGuestMembers(h *Handler, r *http.Request, workspaceID string, body []byte) []byte {
	guests := h.guestUserIDs(r.Context(), workspaceID)
	guests[requestUserID(r)] = true
	return eachObject(body, "members", func(m map[string]any) {
		m["email"] = ""
		if id, _ := m["user_id"].(string); guests[id] {
			m["role"] = "guest"
		}
	})
}

func scrubGuestProjectMembers(_ *Handler, _ *http.Request, _ string, body []byte) []byte {
	return eachObject(body, "members", func(m map[string]any) {
		m["email"] = ""
	})
}

func scrubWorkspaceObject(ws map[string]any) {
	ws["context"] = nil
	ws["repos"] = []any{}
	ws["description"] = nil
	ws["settings"] = map[string]any{}
}

func scrubGuestWorkspace(_ *Handler, _ *http.Request, _ string, body []byte) []byte {
	var ws map[string]any
	if json.Unmarshal(body, &ws) != nil {
		return nil
	}
	scrubWorkspaceObject(ws)
	out, err := json.Marshal(ws)
	if err != nil {
		return nil
	}
	return out
}

func scrubGuestWorkspaceList(h *Handler, r *http.Request, _ string, body []byte) []byte {
	guestOf, err := h.lightsoftGuestDirectory().GuestWorkspaces(r.Context(), requestUserID(r))
	if err != nil {
		return nil
	}
	return eachObject(body, "workspaces", func(ws map[string]any) {
		if id, _ := ws["id"].(string); guestOf[id] {
			scrubWorkspaceObject(ws)
		}
	})
}

var guestAgentFields = map[string]bool{
	"id": true, "workspace_id": true, "name": true, "avatar_url": true,
	"archived_at": true, "kind": true, "system_key": true, "created_at": true, "updated_at": true,
}

func scrubGuestAgents(_ *Handler, _ *http.Request, _ string, body []byte) []byte {
	return eachObject(body, "agents", func(a map[string]any) { keepOnly(a, guestAgentFields) })
}

var guestSquadFields = map[string]bool{
	"id": true, "workspace_id": true, "name": true, "avatar_url": true,
	"archived_at": true, "created_at": true, "updated_at": true,
}

func scrubGuestSquads(_ *Handler, _ *http.Request, _ string, body []byte) []byte {
	return eachObject(body, "squads", func(s map[string]any) { keepOnly(s, guestSquadFields) })
}

// --- workspace settings: the guest list is not part of the settings API ---

// stripLightsoftGuestSettings hides the guest list from every workspace
// response; owners and admins read it from /api/lightsoft/guests.
func stripLightsoftGuestSettings(settings any) {
	if m, ok := settings.(map[string]any); ok {
		delete(m, lightsoftguest.SettingsKey)
	}
}
