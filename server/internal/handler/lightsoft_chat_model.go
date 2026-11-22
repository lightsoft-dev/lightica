package handler

// Lightsoft fork (LIGH-104): per-chat-session model / reasoning effort.
// Storage and resolution live in service/lightsoft_chat_model.go.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/service"
	agentpkg "github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const maxChatModelSelectionModelLen = 200

// ChatSessionModelSelectionResponse is GET /api/chat/sessions/{id}/model-selection.
// ModelSelection is null until a send in the session records one.
type ChatSessionModelSelectionResponse struct {
	ModelSelection *service.ChatModelSelection `json:"model_selection"`
}

// normalizeChatModelSelection validates a selection against the agent's
// runtime provider. The daemon still checks the exact model / level pair
// against its local catalog at launch and drops a level the model lacks.
func normalizeChatModelSelection(provider string, sel service.ChatModelSelection) (service.ChatModelSelection, error) {
	sel.Model = strings.TrimSpace(sel.Model)
	sel.ThinkingLevel = strings.TrimSpace(sel.ThinkingLevel)
	if sel.IsDefault() {
		return sel, nil
	}
	if !service.ChatModelSelectionSupported(provider) {
		return sel, fmt.Errorf("runtime %q does not support a per-chat model selection", provider)
	}
	if sel.Model != "" {
		if len(sel.Model) > maxChatModelSelectionModelLen {
			return sel, fmt.Errorf("model is longer than %d characters", maxChatModelSelectionModelLen)
		}
		// The model is handed to the CLI as an argument value.
		if strings.HasPrefix(sel.Model, "-") || strings.IndexFunc(sel.Model, func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsControl(r)
		}) >= 0 {
			return sel, fmt.Errorf("model %q is not a valid model id", sel.Model)
		}
	}
	if !agentpkg.IsKnownThinkingValue(provider, sel.ThinkingLevel) {
		return sel, fmt.Errorf("%s", thinkingLevelRejection(provider, sel.ThinkingLevel))
	}
	return sel, nil
}

// withChatModelSelection validates a send's selection and returns the request
// carrying it for SendDirectChatMessage to record on the new task. No
// selection leaves the request unchanged: the task then follows the session's
// latest one.
func (h *Handler) withChatModelSelection(w http.ResponseWriter, r *http.Request, agent db.Agent, sel *service.ChatModelSelection) (*http.Request, bool) {
	if sel == nil {
		return r, true
	}
	runtime, err := h.Queries.GetAgentRuntime(r.Context(), agent.RuntimeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load chat agent runtime")
		return r, false
	}
	normalized, err := normalizeChatModelSelection(runtime.Provider, *sel)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return r, false
	}
	return r.WithContext(service.WithChatModelSelection(r.Context(), normalized)), true
}

// applyChatModelSelection swaps the chat session's selection into a claimed
// chat task's agent payload. A failed lookup keeps the agent's values: the
// run still happens, on the agent's defaults.
func (h *Handler) applyChatModelSelection(ctx context.Context, task *db.AgentTaskQueue, provider string, agentData *TaskAgentData) {
	if task == nil || agentData == nil || !task.ChatSessionID.Valid || !service.ChatModelSelectionSupported(provider) {
		return
	}
	sel, ok, err := service.ResolveChatTaskModelSelection(ctx, h.Queries, *task)
	if err != nil {
		slog.Warn("daemon claim: load chat model selection failed; using agent defaults",
			"task_id", uuidToString(task.ID), "chat_session_id", uuidToString(task.ChatSessionID), "error", err)
		return
	}
	if !ok || sel.IsDefault() {
		return
	}
	if sel.Model != "" {
		agentData.Model = sel.Model
	}
	if sel.ThinkingLevel != "" {
		agentData.ThinkingLevel = sel.ThinkingLevel
	}
	slog.Info("daemon claim: applied chat model selection",
		"task_id", uuidToString(task.ID), "chat_session_id", uuidToString(task.ChatSessionID),
		"model", agentData.Model, "thinking_level", agentData.ThinkingLevel)
}

// LightsoftCapabilitiesResponse is GET /api/lightsoft/capabilities. An
// upstream backend has no such route, so the team client reads its 404 as
// "no fork features" and keeps fork-only controls hidden.
type LightsoftCapabilitiesResponse struct {
	ChatModelSelection bool `json:"chat_model_selection"`
	// Guest is true when the caller is an outside collaborator of this
	// workspace (LIGH-282); the team client then shows only their projects.
	Guest bool `json:"guest"`
	// GuestManagement is true for owners and admins, who manage the list.
	GuestManagement bool `json:"guest_management"`
}

func (h *Handler) GetLightsoftCapabilities(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	guest := h.isLightsoftGuestRequest(r, workspaceID)
	manager := false
	if m, ok := ctxMember(r.Context()); ok && !guest {
		manager = roleAllowed(m.Role, "owner", "admin")
	}
	writeJSON(w, http.StatusOK, LightsoftCapabilitiesResponse{ChatModelSelection: !guest, Guest: guest, GuestManagement: manager})
}

// GetChatSessionModelSelection returns the session's current selection.
func (h *Handler) GetChatSessionModelSelection(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	session, ok := h.gatePublicChatSessionForUser(w, r, userID, workspaceID, chi.URLParam(r, "sessionId"))
	if !ok {
		return
	}
	sel, found, err := service.LatestChatSessionModelSelection(r.Context(), h.Queries, session.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load chat model selection")
		return
	}
	resp := ChatSessionModelSelectionResponse{}
	if found && !sel.IsDefault() {
		resp.ModelSelection = &sel
	}
	writeJSON(w, http.StatusOK, resp)
}
