-- Lightsoft fork (LIGH-104): per-chat-session model / reasoning-effort
-- selection. The selection lives on the chat task's existing `context` JSONB
-- under its own key, so the fork needs no schema change and an upstream
-- backend simply ignores it.

-- name: SetChatTaskModelSelection :exec
UPDATE agent_task_queue
SET context = COALESCE(context, '{}'::jsonb)
    || jsonb_build_object('lightsoft_model_selection', sqlc.arg(selection)::jsonb)
WHERE id = sqlc.arg(id)
  AND chat_session_id IS NOT NULL;

-- name: GetLatestChatSessionModelSelection :one
-- The session's current selection is the one recorded on its most recent
-- task that carries one.
SELECT (context -> 'lightsoft_model_selection')::jsonb AS selection
FROM agent_task_queue
WHERE chat_session_id = $1
  AND context -> 'lightsoft_model_selection' IS NOT NULL
ORDER BY created_at DESC, id DESC
LIMIT 1;
