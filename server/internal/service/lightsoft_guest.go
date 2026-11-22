package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// guardLightsoftGuestRun refuses, at enqueue, a run whose human (originator,
// else accountable user) is an outside collaborator of the workspace
// (Lightsoft, LIGH-282). The HTTP layer already refuses the invocations it
// sees; this catches every enqueue path that records the guest as the run's
// human, such as moving an agent-assigned issue out of backlog. A failed
// lookup refuses.
func (s *TaskService) guardLightsoftGuestRun(ctx context.Context, workspaceID, originatorUserID, accountableUserID pgtype.UUID) error {
	human := originatorUserID
	if !human.Valid {
		human = accountableUserID
	}
	if !human.Valid {
		return nil
	}
	guest, err := lightsoftGuestOf(ctx, s.Queries, workspaceID, human)
	if err != nil {
		return fmt.Errorf("resolve outside collaborator: %w", err)
	}
	if guest {
		slog.Info("task enqueue refused: outside collaborator",
			"workspace_id", util.UUIDToString(workspaceID), "user_id", util.UUIDToString(human))
		return fmt.Errorf("%w: outside collaborators cannot start agent runs", ErrProjectAccessDenied)
	}
	return nil
}

func lightsoftGuestOf(ctx context.Context, q *db.Queries, workspaceID, userID pgtype.UUID) (bool, error) {
	if q == nil || !workspaceID.Valid || !userID.Valid {
		return false, nil
	}
	return q.IsLightsoftGuest(ctx, db.IsLightsoftGuestParams{WorkspaceID: workspaceID, UserID: userID})
}
