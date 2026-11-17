package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestGetLightsoftCapabilities(t *testing.T) {
	var out LightsoftCapabilitiesResponse
	testutil.Call(t, (&Handler{}).GetLightsoftCapabilities, httptest.NewRequest(http.MethodGet, "/api/lightsoft/capabilities", nil)).Want(http.StatusOK).JSON(&out)
	if !out.ChatModelSelection {
		t.Fatal("capabilities must advertise chat_model_selection")
	}
}

func TestNormalizeChatModelSelection(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		in       service.ChatModelSelection
		want     service.ChatModelSelection
		wantErr  bool
	}{
		{name: "default on any runtime", provider: "hermes", in: service.ChatModelSelection{Model: " ", ThinkingLevel: ""}},
		{name: "claude model and effort", provider: "claude", in: service.ChatModelSelection{Model: " claude-opus-4-1 ", ThinkingLevel: "high"}, want: service.ChatModelSelection{Model: "claude-opus-4-1", ThinkingLevel: "high"}},
		{name: "codex catalog effort", provider: "codex", in: service.ChatModelSelection{Model: "gpt-5.1-codex", ThinkingLevel: "xhigh"}, want: service.ChatModelSelection{Model: "gpt-5.1-codex", ThinkingLevel: "xhigh"}},
		{name: "pi provider-qualified model", provider: "pi", in: service.ChatModelSelection{Model: "anthropic/claude-sonnet-4-5", ThinkingLevel: "low"}, want: service.ChatModelSelection{Model: "anthropic/claude-sonnet-4-5", ThinkingLevel: "low"}},
		{name: "unsupported runtime", provider: "hermes", in: service.ChatModelSelection{Model: "x"}, wantErr: true},
		{name: "unknown claude effort", provider: "claude", in: service.ChatModelSelection{ThinkingLevel: "turbo"}, wantErr: true},
		{name: "flag-like model", provider: "claude", in: service.ChatModelSelection{Model: "--dangerously-skip-permissions"}, wantErr: true},
		{name: "model with whitespace", provider: "claude", in: service.ChatModelSelection{Model: "claude opus"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeChatModelSelection(tc.provider, tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// newChatModelSelectionSession seeds a Claude-runtime agent with saved
// defaults and a chat session on it.
func newChatModelSelectionSession(t *testing.T) (sessionID, runtimeID string) {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	runtimeID = dbfx.Runtime(t, "Chat model selection runtime", testutil.Cols{"provider": "claude"})
	agentID := createHandlerTestAgent(t, "ChatModelSelectionAgent", []byte("[]"))
	dbfx.Exec(t, `UPDATE agent SET runtime_id = $2, model = 'claude-sonnet-4-6', thinking_level = 'medium' WHERE id = $1`, agentID, runtimeID)
	return createHandlerTestChatSession(t, agentID), runtimeID
}

func sendChatWithSelection(t *testing.T, sessionID string, selection any, wantStatus int) {
	t.Helper()
	body := map[string]any{"content": "hello"}
	if selection != nil {
		body["model_selection"] = selection
	}
	req := withURLParam(newRequest("POST", "/api/chat/sessions/"+sessionID+"/messages", body), "sessionId", sessionID)
	testutil.Call(t, testHandler.SendChatMessage, withChatTestWorkspaceCtx(t, req)).Want(wantStatus)
}

// claimChatSelection claims the session's next task and settles it so the
// session's next send becomes claimable.
func claimChatSelection(t *testing.T, runtimeID string) (model, thinkingLevel string) {
	t.Helper()
	task := claimTaskSource(t, runtimeID)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, task.ID)
	if task.Agent == nil {
		t.Fatal("claimed task carries no agent payload")
	}
	return task.Agent.Model, task.Agent.ThinkingLevel
}

func getChatModelSelection(t *testing.T, sessionID string) *service.ChatModelSelection {
	t.Helper()
	req := withURLParam(newRequest("GET", "/api/chat/sessions/"+sessionID+"/model-selection", nil), "sessionId", sessionID)
	var out ChatSessionModelSelectionResponse
	testutil.Call(t, testHandler.GetChatSessionModelSelection, withChatTestWorkspaceCtx(t, req)).Want(http.StatusOK).JSON(&out)
	return out.ModelSelection
}

func TestChatModelSelection_SendRecordsAndClaimApplies(t *testing.T) {
	sessionID, runtimeID := newChatModelSelectionSession(t)

	if got := getChatModelSelection(t, sessionID); got != nil {
		t.Fatalf("new session selection = %+v, want null", got)
	}

	// No selection yet: the agent's saved values.
	sendChatWithSelection(t, sessionID, nil, http.StatusCreated)
	if model, level := claimChatSelection(t, runtimeID); model != "claude-sonnet-4-6" || level != "medium" {
		t.Fatalf("no selection claimed %q/%q, want agent defaults", model, level)
	}

	sendChatWithSelection(t, sessionID, map[string]string{"model": "claude-opus-4-1", "thinking_level": "high"}, http.StatusCreated)
	if got := getChatModelSelection(t, sessionID); got == nil || got.Model != "claude-opus-4-1" || got.ThinkingLevel != "high" {
		t.Fatalf("session selection = %+v, want opus/high", got)
	}
	if model, level := claimChatSelection(t, runtimeID); model != "claude-opus-4-1" || level != "high" {
		t.Fatalf("selected claim = %q/%q, want claude-opus-4-1/high", model, level)
	}

	// A send without the field (another client) follows the session's latest.
	sendChatWithSelection(t, sessionID, nil, http.StatusCreated)
	if model, level := claimChatSelection(t, runtimeID); model != "claude-opus-4-1" || level != "high" {
		t.Fatalf("follow-up claim = %q/%q, want the session selection", model, level)
	}

	// An effort-only selection keeps the agent's model.
	sendChatWithSelection(t, sessionID, map[string]string{"thinking_level": "low"}, http.StatusCreated)
	if model, level := claimChatSelection(t, runtimeID); model != "claude-sonnet-4-6" || level != "low" {
		t.Fatalf("effort-only claim = %q/%q, want claude-sonnet-4-6/low", model, level)
	}

	// An explicit default returns the session to the agent's values.
	sendChatWithSelection(t, sessionID, map[string]string{"model": "", "thinking_level": ""}, http.StatusCreated)
	if got := getChatModelSelection(t, sessionID); got != nil {
		t.Fatalf("selection after reset = %+v, want null", got)
	}
	if model, level := claimChatSelection(t, runtimeID); model != "claude-sonnet-4-6" || level != "medium" {
		t.Fatalf("reset claim = %q/%q, want agent defaults", model, level)
	}
}

func TestChatModelSelection_RejectsInvalidBeforePersisting(t *testing.T) {
	sessionID, _ := newChatModelSelectionSession(t)

	sendChatWithSelection(t, sessionID, map[string]string{"thinking_level": "turbo"}, http.StatusBadRequest)
	sendChatWithSelection(t, sessionID, map[string]string{"model": "-p"}, http.StatusBadRequest)

	var messages int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM chat_message WHERE chat_session_id = $1`, sessionID).Scan(&messages); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if messages != 0 {
		t.Fatalf("rejected sends persisted %d messages", messages)
	}
}
