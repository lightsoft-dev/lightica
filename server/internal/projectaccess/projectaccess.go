// Package projectaccess decides which private projects (MUL-8472) a viewer may
// see. It is the single source of the rule; handlers, the task service and the
// realtime fanout all call into it instead of re-deriving visibility.
//
// The rule: a project with visibility "private" is visible to its
// project_member rows and to nobody else. A workspace role grants no
// visibility: an owner or admin sees a private project only as one of its
// members (MUL-9284). Every other project is visible to every workspace
// member. An issue's visibility is its project's.
//
// A viewer is a set of principals (human user ids). A member request has one
// principal: the member. An agent run has two: the human who triggered it and
// the agent's owner, and sees a private project only when both can. A viewer
// with no principal sees no private project.
//
// Lightsoft fork (LIGH-282): an outside collaborator ("guest") of a workspace
// sees only the projects they are a project member of, whatever the project's
// visibility, and nothing outside a project. Their hidden set lists every
// other project plus UnscopedID, which hides objects with no project.
package projectaccess

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	VisibilityWorkspace = "workspace"
	VisibilityPrivate   = "private"
)

// ValidVisibility reports whether v is a storable project visibility.
func ValidVisibility(v string) bool {
	return v == VisibilityWorkspace || v == VisibilityPrivate
}

// UnscopedID stands for "outside any project" in a hidden set (LIGH-282). No
// project has the nil uuid, so a member's hidden set never holds it; a guest's
// always does, which is how the SQL filters (COALESCE(project_id, nil uuid))
// and CanSeeProject hide project-less objects from guests only.
var UnscopedID = pgtype.UUID{Valid: true}

// guestQuerier is the Lightsoft guest lookup. *db.Queries implements it; a
// test double that does not sees no guests.
type guestQuerier interface {
	ListLightsoftGuestHiddenProjectIDs(ctx context.Context, arg db.ListLightsoftGuestHiddenProjectIDsParams) ([]pgtype.UUID, error)
	CountLightsoftGuestsAmong(ctx context.Context, arg db.CountLightsoftGuestsAmongParams) (int32, error)
}

// IsManagerRole reports whether a workspace role may manage the access of a
// project its holder can see. It grants no visibility of its own.
func IsManagerRole(role string) bool {
	return role == "owner" || role == "admin"
}

// Querier is the subset of *db.Queries this package reads.
type Querier interface {
	GetMemberByUserAndWorkspace(ctx context.Context, arg db.GetMemberByUserAndWorkspaceParams) (db.Member, error)
	ListHiddenProjectIDsForUser(ctx context.Context, arg db.ListHiddenProjectIDsForUserParams) ([]pgtype.UUID, error)
	ListPrivateProjectIDs(ctx context.Context, workspaceID pgtype.UUID) ([]pgtype.UUID, error)
}

// Hidden is the set of private projects a viewer may not see. The zero value
// hides nothing.
type Hidden struct {
	ids []pgtype.UUID
	set map[[16]byte]struct{}
}

func newHidden() Hidden { return Hidden{set: map[[16]byte]struct{}{}} }

func (h *Hidden) add(ids []pgtype.UUID) {
	for _, id := range ids {
		if !id.Valid {
			continue
		}
		if _, ok := h.set[id.Bytes]; ok {
			continue
		}
		h.set[id.Bytes] = struct{}{}
		h.ids = append(h.ids, id)
	}
}

// Empty reports whether the viewer sees every project.
func (h Hidden) Empty() bool { return len(h.ids) == 0 }

// CanSeeProject reports whether an object in projectID is visible. Objects
// outside any project (invalid projectID) are visible unless the viewer is a
// guest (UnscopedID in the set).
func (h Hidden) CanSeeProject(projectID pgtype.UUID) bool {
	if len(h.set) == 0 {
		return true
	}
	if !projectID.Valid {
		projectID = UnscopedID
	}
	_, hidden := h.set[projectID.Bytes]
	return !hidden
}

// HidesUnscoped reports whether objects outside any project are hidden, which
// is true only for a guest (LIGH-282).
func (h Hidden) HidesUnscoped() bool {
	_, hidden := h.set[UnscopedID.Bytes]
	return hidden
}

// Param is the value for a `hidden_project_ids` query argument: nil when
// nothing is hidden, so the SQL predicate short-circuits.
func (h Hidden) Param() []pgtype.UUID {
	if len(h.ids) == 0 {
		return nil
	}
	return h.ids
}

// IDs returns the hidden project ids (never nil, for dynamic SQL builders).
func (h Hidden) IDs() []pgtype.UUID {
	if h.ids == nil {
		return []pgtype.UUID{}
	}
	return h.ids
}

// ForMember computes the hidden set for one current workspace member: every
// private project they are not a member of.
func ForMember(ctx context.Context, q Querier, workspaceID, userID pgtype.UUID) (Hidden, error) {
	h := newHidden()
	ids, err := q.ListHiddenProjectIDsForUser(ctx, db.ListHiddenProjectIDsForUserParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	})
	if err != nil {
		return Hidden{}, err
	}
	h.add(ids)
	if gq, ok := q.(guestQuerier); ok {
		guestIDs, err := gq.ListLightsoftGuestHiddenProjectIDs(ctx, db.ListLightsoftGuestHiddenProjectIDsParams{
			WorkspaceID: workspaceID,
			UserID:      userID,
		})
		if err != nil {
			return Hidden{}, err
		}
		h.add(guestIDs)
	}
	return h, nil
}

// ForPrincipals computes the hidden set for a viewer whose access is bounded
// by every listed principal: a project is visible only when each of them can
// see it. Invalid ids are ignored; when none remain, every private project is
// hidden. A principal who is no longer a workspace member sees no private
// project.
func ForPrincipals(ctx context.Context, q Querier, workspaceID pgtype.UUID, principals ...pgtype.UUID) (Hidden, error) {
	h := newHidden()
	private, err := q.ListPrivateProjectIDs(ctx, workspaceID)
	if err != nil {
		return Hidden{}, err
	}
	if len(private) == 0 {
		guests, err := countGuests(ctx, q, workspaceID, principals)
		if err != nil {
			return Hidden{}, err
		}
		if guests == 0 {
			return h, nil
		}
	}
	all := newHidden()
	all.add(private)
	seen := map[[16]byte]struct{}{}
	var valid []pgtype.UUID
	for _, p := range principals {
		if !p.Valid {
			continue
		}
		if _, ok := seen[p.Bytes]; ok {
			continue
		}
		seen[p.Bytes] = struct{}{}
		valid = append(valid, p)
	}
	if len(valid) == 0 {
		return all, nil
	}
	for _, p := range valid {
		_, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
			UserID:      p,
			WorkspaceID: workspaceID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return all, nil
		}
		if err != nil {
			return Hidden{}, err
		}
		one, err := ForMember(ctx, q, workspaceID, p)
		if err != nil {
			return Hidden{}, err
		}
		h.add(one.ids)
	}
	return h, nil
}

// countGuests counts the guests among principals; a Querier without the
// guest lookup has none.
func countGuests(ctx context.Context, q Querier, workspaceID pgtype.UUID, principals []pgtype.UUID) (int32, error) {
	gq, ok := q.(guestQuerier)
	if !ok {
		return 0, nil
	}
	var ids []pgtype.UUID
	for _, p := range principals {
		if p.Valid {
			ids = append(ids, p)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return gq.CountLightsoftGuestsAmong(ctx, db.CountLightsoftGuestsAmongParams{WorkspaceID: workspaceID, UserIds: ids})
}

// RunPrincipals returns the humans that bound an agent run's access: the
// run's originator (falling back to its accountable user, which is how
// owner_fallback and autopilot runs record their human) and the agent owner.
func RunPrincipals(task db.AgentTaskQueue, agentOwnerID pgtype.UUID) []pgtype.UUID {
	originator := task.OriginatorUserID
	if !originator.Valid {
		originator = task.AccountableUserID
	}
	return []pgtype.UUID{originator, agentOwnerID}
}
