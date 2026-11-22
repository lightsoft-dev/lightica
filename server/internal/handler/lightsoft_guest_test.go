package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func guestAllowed(method, path string) bool {
	if _, ok := matchGuestRoute(guestRoutes, method, path); ok {
		return true
	}
	_, ok := matchGuestRoute(guestAccountRoutes, method, path)
	return ok
}

// The allow-list is the guest's whole API surface (LIGH-282): everything not
// listed, including routes added upstream later, is refused.
func TestLightsoftGuestRoutes(t *testing.T) {
	const id = "0193a9b8-0000-7000-8000-000000000001"
	cases := []struct {
		method, path string
		allowed      bool
	}{
		{"GET", "/api/me", true},
		{"GET", "/api/issues", true},
		{"GET", "/api/issues/LSUI-1", true},
		{"PUT", "/api/issues/" + id, true},
		{"POST", "/api/issues/" + id + "/comments", true},
		{"GET", "/api/projects/" + id, true},
		{"GET", "/api/workspaces/" + id + "/members", true},
		{"GET", "/api/inbox/unread-count", true},
		{"GET", "/api/agents", true},
		{"GET", "/api/runtimes", true}, // answered empty

		{"DELETE", "/api/issues/" + id, false},
		{"POST", "/api/issues/batch-update", false},
		{"POST", "/api/issues/quick-create", false},
		{"POST", "/api/projects", false},
		{"PUT", "/api/projects/" + id, false},
		{"POST", "/api/projects/" + id + "/members", false},
		{"POST", "/api/projects/" + id + "/join", false},
		{"GET", "/api/agents/" + id, false},
		{"PUT", "/api/agents/" + id, false},
		{"POST", "/api/chat/sessions", false},
		{"GET", "/api/chat/sessions/" + id + "/messages", false},
		{"POST", "/api/autopilots", false},
		{"GET", "/api/autopilots/" + id, false},
		{"GET", "/api/skills/" + id, false},
		{"GET", "/api/workspaces/" + id + "/invitations", false},
		{"POST", "/api/workspaces/" + id + "/members", false},
		{"PATCH", "/api/workspaces/" + id, false},
		{"GET", "/api/lightsoft/guests", false},
		{"POST", "/api/lightsoft/guests", false},
		{"GET", "/api/dashboard/usage/daily", false},
		{"POST", "/api/tasks/" + id + "/rerun", false},
		{"GET", "/api/docs", false},
		{"POST", "/api/workspaces", false},
		{"POST", "/api/feedback", false},
	}
	for _, c := range cases {
		if got := guestAllowed(c.method, c.path); got != c.allowed {
			t.Errorf("%s %s allowed=%v, want %v", c.method, c.path, got, c.allowed)
		}
	}
}

func TestLightsoftGuestIssueBodyChecks(t *testing.T) {
	cases := []struct {
		name  string
		check func(http.ResponseWriter, *http.Request) bool
		body  string
		want  int // 0 = passes
	}{
		{"create without project", checkGuestIssueCreate, `{"title":"x"}`, http.StatusBadRequest},
		{"create with null project", checkGuestIssueCreate, `{"title":"x","project_id":null}`, http.StatusBadRequest},
		{"create in project", checkGuestIssueCreate, `{"title":"x","project_id":"p"}`, 0},
		{"create for agent", checkGuestIssueCreate, `{"title":"x","project_id":"p","assignee_type":"agent","assignee_id":"a"}`, http.StatusForbidden},
		{"create for squad", checkGuestIssueCreate, `{"title":"x","project_id":"p","assignee_type":"squad","assignee_id":"s"}`, http.StatusForbidden},
		{"create for member", checkGuestIssueCreate, `{"title":"x","project_id":"p","assignee_type":"member","assignee_id":"m"}`, 0},
		{"update title", checkGuestIssueUpdate, `{"title":"y"}`, 0},
		{"update clears project", checkGuestIssueUpdate, `{"project_id":null}`, http.StatusBadRequest},
		{"update empties project", checkGuestIssueUpdate, `{"project_id":""}`, http.StatusBadRequest},
		{"update moves project", checkGuestIssueUpdate, `{"project_id":"q"}`, 0},
		{"update to agent", checkGuestIssueUpdate, `{"assignee_type":"agent","assignee_id":"a"}`, http.StatusForbidden},
		// encoding/json matches keys case-insensitively, last one wins.
		{"case-variant project key", checkGuestIssueUpdate, `{"project_id":"p","PROJECT_ID":null}`, http.StatusBadRequest},
		{"folded assignee key", checkGuestIssueCreate, `{"project_id":"p","aſſignee_type":"agent"}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		req := httptest.NewRequest("POST", "/api/issues", strings.NewReader(c.body))
		rec := httptest.NewRecorder()
		ok := c.check(rec, req)
		if c.want == 0 {
			if !ok {
				t.Errorf("%s: refused with %d %s", c.name, rec.Code, rec.Body)
				continue
			}
			// The handler still reads the whole body.
			var again map[string]any
			if err := json.NewDecoder(req.Body).Decode(&again); err != nil {
				t.Errorf("%s: body not restored: %v", c.name, err)
			}
			continue
		}
		if ok || rec.Code != c.want {
			t.Errorf("%s: ok=%v status=%d, want refusal %d", c.name, ok, rec.Code, c.want)
		}
	}
}

func TestLightsoftGuestScrubAgents(t *testing.T) {
	body := `[{"id":"a1","name":"Builder","avatar_url":null,"instructions":"SECRET","runtime_config":{"model":"x"},"custom_args":["--y"],"max_concurrent_tasks":3,"runtime_bound":true}]`
	out := scrubGuestAgents(nil, nil, "", []byte(body))
	var agents []map[string]any
	if err := json.Unmarshal(out, &agents); err != nil {
		t.Fatal(err)
	}
	a := agents[0]
	if a["name"] != "Builder" || a["id"] != "a1" {
		t.Fatalf("kept fields lost: %v", a)
	}
	if strings.Contains(string(out), "SECRET") || strings.Contains(string(out), "--y") || strings.Contains(string(out), `"model"`) {
		t.Fatalf("agent configuration leaked: %s", out)
	}
	// Types survive so client schemas still parse.
	if _, ok := a["custom_args"].([]any); !ok {
		t.Fatalf("custom_args type changed: %T", a["custom_args"])
	}
	if a["max_concurrent_tasks"] != float64(0) || a["runtime_bound"] != false {
		t.Fatalf("zero values wrong: %v", a)
	}
}

func TestLightsoftGuestSettingsStrip(t *testing.T) {
	settings := map[string]any{"lightsoft_guests": []any{"a@b.c"}, "other": true}
	stripLightsoftGuestSettings(settings)
	if _, ok := settings["lightsoft_guests"]; ok || settings["other"] != true {
		t.Fatalf("strip = %v", settings)
	}
}

func TestLightsoftGuestDeniedAnywhere(t *testing.T) {
	for _, p := range []string{"/api/slack/binding/redeem", "/api/lark/binding/redeem", "/api/integrations/composio/connect/init"} {
		if !guestAnywhereDenied.MatchString(p) {
			t.Errorf("%s should be refused to guests", p)
		}
	}
	if guestAnywhereDenied.MatchString("/api/issues") {
		t.Error("issues must not be in the anywhere-deny list")
	}
}
