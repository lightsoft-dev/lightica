package service

// Lightsoft fork (LIGH-104): a chat session can run on a model / reasoning
// effort other than its agent's saved defaults.
//
// The selection is recorded on each chat task's `context` JSONB (key
// chatModelSelectionContextKey), not on chat_session, so the fork keeps the
// upstream schema. A task without its own selection (a send from a client that
// does not know the field) follows the latest selection recorded in the same
// session; retries copy `context` and therefore keep theirs. The claim
// substitutes the resolved values for the agent's model / thinking_level, and
// the daemon applies and validates them exactly as it does the agent's own.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const chatModelSelectionContextKey = "lightsoft_model_selection"

// ChatModelSelection is a chat session's run configuration. An empty field
// means "use the agent's saved value".
type ChatModelSelection struct {
	Model         string `json:"model"`
	ThinkingLevel string `json:"thinking_level"`
}

// IsDefault reports whether the selection leaves both agent values in place.
func (s ChatModelSelection) IsDefault() bool {
	return s.Model == "" && s.ThinkingLevel == ""
}

// ChatModelSelectionSupported lists the runtimes this fork has verified the
// per-session override against.
func ChatModelSelectionSupported(provider string) bool {
	switch provider {
	case "claude", "codex", "pi":
		return true
	}
	return false
}

type chatModelSelectionCtxKey struct{}

// WithChatModelSelection carries a validated selection into
// SendDirectChatMessage, which records it on the task it creates inside the
// send transaction. Recording it there (and not after the send returns) keeps
// a daemon that claims the task right after commit from missing it. A context
// value keeps the fork's footprint in the upstream send path to one call.
func WithChatModelSelection(ctx context.Context, sel ChatModelSelection) context.Context {
	return context.WithValue(ctx, chatModelSelectionCtxKey{}, sel)
}

func recordChatModelSelection(ctx context.Context, q *db.Queries, taskID pgtype.UUID) error {
	sel, ok := ctx.Value(chatModelSelectionCtxKey{}).(ChatModelSelection)
	if !ok {
		return nil
	}
	encoded, err := json.Marshal(sel)
	if err != nil {
		return fmt.Errorf("encode chat model selection: %w", err)
	}
	if err := q.SetChatTaskModelSelection(ctx, db.SetChatTaskModelSelectionParams{
		Selection: encoded,
		ID:        taskID,
	}); err != nil {
		return fmt.Errorf("record chat model selection: %w", err)
	}
	return nil
}

// chatModelSelectionFromTaskContext reads the selection recorded on a task.
func chatModelSelectionFromTaskContext(raw []byte) (ChatModelSelection, bool) {
	if len(raw) == 0 {
		return ChatModelSelection{}, false
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return ChatModelSelection{}, false
	}
	value, ok := wrapper[chatModelSelectionContextKey]
	if !ok {
		return ChatModelSelection{}, false
	}
	return decodeChatModelSelection(value)
}

func decodeChatModelSelection(raw []byte) (ChatModelSelection, bool) {
	if len(raw) == 0 {
		return ChatModelSelection{}, false
	}
	var sel ChatModelSelection
	if err := json.Unmarshal(raw, &sel); err != nil {
		return ChatModelSelection{}, false
	}
	return sel, true
}

// LatestChatSessionModelSelection returns the selection most recently
// recorded in a chat session. ok is false when none was ever recorded.
func LatestChatSessionModelSelection(ctx context.Context, q *db.Queries, chatSessionID pgtype.UUID) (sel ChatModelSelection, ok bool, err error) {
	raw, err := q.GetLatestChatSessionModelSelection(ctx, chatSessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ChatModelSelection{}, false, nil
	}
	if err != nil {
		return ChatModelSelection{}, false, err
	}
	sel, ok = decodeChatModelSelection(raw)
	return sel, ok, nil
}

// ResolveChatTaskModelSelection is the selection a claimed chat task runs
// with: its own when recorded, otherwise its session's latest.
func ResolveChatTaskModelSelection(ctx context.Context, q *db.Queries, task db.AgentTaskQueue) (ChatModelSelection, bool, error) {
	if !task.ChatSessionID.Valid {
		return ChatModelSelection{}, false, nil
	}
	if sel, ok := chatModelSelectionFromTaskContext(task.Context); ok {
		return sel, true, nil
	}
	return LatestChatSessionModelSelection(ctx, q, task.ChatSessionID)
}
