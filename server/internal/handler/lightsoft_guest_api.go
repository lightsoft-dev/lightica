package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/lightsoftguest"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Guest list API (LIGH-282), for workspace owners and admins:
//
//	GET    /api/lightsoft/guests                       list
//	POST   /api/lightsoft/guests {"email": "..."}      add
//	DELETE /api/lightsoft/guests?email=...[&promote=1] remove
//
// Adding an email does not invite it; the normal invitation does that, and
// listing the email first makes the account a guest from its first request.
// Which projects a guest sees is their project membership, managed on each
// project as for anyone.

type LightsoftGuestProject struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type LightsoftGuestResponse struct {
	Email    string                  `json:"email"`
	UserID   *string                 `json:"user_id"`
	Name     *string                 `json:"name"`
	MemberID *string                 `json:"member_id"`
	JoinedAt *string                 `json:"joined_at"`
	Projects []LightsoftGuestProject `json:"projects"`
}

type lightsoftGuestsResponse struct {
	Guests []LightsoftGuestResponse `json:"guests"`
}

// requireGuestManager admits workspace owners and admins who are not guests.
func (h *Handler) requireGuestManager(w http.ResponseWriter, r *http.Request) (db.Member, bool) {
	workspaceID := ctxWorkspaceID(r.Context())
	if actorType, _ := h.resolveActor(r, requestUserID(r), workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot manage outside collaborators")
		return db.Member{}, false
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return db.Member{}, false
	}
	if !roleAllowed(member.Role, "owner", "admin") || h.isLightsoftGuestRequest(r, workspaceID) {
		writeError(w, http.StatusForbidden, "only workspace owners and admins can manage outside collaborators")
		return db.Member{}, false
	}
	return member, true
}

func (h *Handler) listLightsoftGuests(r *http.Request, workspaceID pgtype.UUID) ([]LightsoftGuestResponse, error) {
	rows, err := h.Queries.ListLightsoftGuestMembers(r.Context(), workspaceID)
	if err != nil {
		return nil, err
	}
	memberships, err := h.Queries.ListLightsoftGuestProjectMemberships(r.Context(), workspaceID)
	if err != nil {
		return nil, err
	}
	projects := map[string][]LightsoftGuestProject{}
	for _, m := range memberships {
		if !m.ProjectID.Valid {
			continue
		}
		uid := uuidToString(m.UserID)
		projects[uid] = append(projects[uid], LightsoftGuestProject{ID: uuidToString(m.ProjectID), Title: m.ProjectTitle.String})
	}
	out := make([]LightsoftGuestResponse, 0, len(rows))
	for _, row := range rows {
		g := LightsoftGuestResponse{Email: row.Email, Projects: []LightsoftGuestProject{}}
		if row.UserID.Valid {
			g.UserID = uuidToPtr(row.UserID)
			g.Name = textToPtr(row.UserName)
		}
		if row.MemberID.Valid {
			g.MemberID = uuidToPtr(row.MemberID)
			joined := timestampToString(row.JoinedAt)
			g.JoinedAt = &joined
			if p := projects[uuidToString(row.UserID)]; p != nil {
				g.Projects = p
			}
		}
		out = append(out, g)
	}
	return out, nil
}

func (h *Handler) writeLightsoftGuests(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID) {
	guests, err := h.listLightsoftGuests(r, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list outside collaborators")
		return
	}
	writeJSON(w, http.StatusOK, lightsoftGuestsResponse{Guests: guests})
}

// ListLightsoftGuests — GET /api/lightsoft/guests
func (h *Handler) ListLightsoftGuests(w http.ResponseWriter, r *http.Request) {
	member, ok := h.requireGuestManager(w, r)
	if !ok {
		return
	}
	h.writeLightsoftGuests(w, r, member.WorkspaceID)
}

func (h *Handler) storedLightsoftGuests(r *http.Request, workspaceID pgtype.UUID) ([]string, error) {
	raw, err := h.Queries.GetLightsoftGuestEmails(r.Context(), workspaceID)
	if err != nil {
		return nil, err
	}
	return lightsoftguest.ParseEmails(raw), nil
}

func (h *Handler) saveLightsoftGuests(r *http.Request, workspaceID pgtype.UUID, emails []string) error {
	if emails == nil {
		emails = []string{}
	}
	raw, err := json.Marshal(emails)
	if err != nil {
		return err
	}
	if err := h.Queries.SetLightsoftGuestEmails(r.Context(), db.SetLightsoftGuestEmailsParams{WorkspaceID: workspaceID, Guests: raw}); err != nil {
		return err
	}
	lightsoftguest.InvalidateAll()
	return nil
}

// memberByEmail finds the workspace member using email, if any.
func (h *Handler) memberByEmail(r *http.Request, workspaceID pgtype.UUID, email string) (db.User, db.Member, bool, error) {
	user, err := h.Queries.GetUserByEmail(r.Context(), email)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, db.Member{}, false, nil
	}
	if err != nil {
		return db.User{}, db.Member{}, false, err
	}
	member, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: user.ID, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return user, db.Member{}, false, nil
	}
	if err != nil {
		return db.User{}, db.Member{}, false, err
	}
	return user, member, true, nil
}

// AddLightsoftGuest — POST /api/lightsoft/guests {"email": "..."}
func (h *Handler) AddLightsoftGuest(w http.ResponseWriter, r *http.Request) {
	manager, ok := h.requireGuestManager(w, r)
	if !ok {
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	email, valid := canonicalBusinessEmail(req.Email)
	if !valid {
		writeError(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	user, member, isMember, err := h.memberByEmail(r, manager.WorkspaceID, email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up member")
		return
	}
	if isMember && member.Role != "member" {
		writeError(w, http.StatusConflict, "change this member's role to member before making them an outside collaborator")
		return
	}
	emails, err := h.storedLightsoftGuests(r, manager.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read outside collaborators")
		return
	}
	if !slices.Contains(emails, email) {
		if err := h.saveLightsoftGuests(r, manager.WorkspaceID, append(emails, email)); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save outside collaborators")
			return
		}
		slog.Info("lightsoft guest added", append(logger.RequestAttrs(r), "workspace_id", uuidToString(manager.WorkspaceID), "existing_member", isMember)...)
		if isMember && h.Hub != nil {
			// An open socket joined the workspace room as a member; make it
			// reconnect as a guest.
			h.Hub.LightsoftDisconnect(uuidToString(manager.WorkspaceID), uuidToString(user.ID))
		}
	}
	h.writeLightsoftGuests(w, r, manager.WorkspaceID)
}

// RemoveLightsoftGuest — DELETE /api/lightsoft/guests?email=...
//
// Taking a current member off the list makes them a full member, so it needs
// promote=1; to revoke access, remove the member from the workspace instead
// (the entry can stay, so a later re-invite is a guest again).
func (h *Handler) RemoveLightsoftGuest(w http.ResponseWriter, r *http.Request) {
	manager, ok := h.requireGuestManager(w, r)
	if !ok {
		return
	}
	email := lightsoftguest.NormalizeEmail(r.URL.Query().Get("email"))
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	emails, err := h.storedLightsoftGuests(r, manager.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read outside collaborators")
		return
	}
	idx := slices.Index(emails, email)
	if idx < 0 {
		writeError(w, http.StatusNotFound, "outside collaborator not found")
		return
	}
	_, _, isMember, err := h.memberByEmail(r, manager.WorkspaceID, email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up member")
		return
	}
	promote := r.URL.Query().Get("promote")
	if isMember && promote != "1" && promote != "true" {
		writeError(w, http.StatusConflict, "this outside collaborator is still a workspace member: remove them from the workspace to revoke access, or pass promote=1 to make them a full member")
		return
	}
	if err := h.saveLightsoftGuests(r, manager.WorkspaceID, slices.Delete(emails, idx, idx+1)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save outside collaborators")
		return
	}
	slog.Info("lightsoft guest removed", append(logger.RequestAttrs(r), "workspace_id", uuidToString(manager.WorkspaceID), "promoted", isMember)...)
	if isMember && h.Hub != nil {
		if user, err := h.Queries.GetUserByEmail(r.Context(), email); err == nil {
			h.Hub.LightsoftDisconnect(uuidToString(manager.WorkspaceID), uuidToString(user.ID))
		}
	}
	h.writeLightsoftGuests(w, r, manager.WorkspaceID)
}
