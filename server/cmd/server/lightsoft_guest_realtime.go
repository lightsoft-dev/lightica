package main

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/lightsoftguest"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Realtime for outside collaborators (Lightsoft fork, LIGH-282). The hub keeps
// a guest's socket out of the workspace room, so a guest receives:
//
//   - what is addressed to them (inbox, member-private, their chat scope);
//   - from a private project's audience, only guestEventAllowed events;
//   - from workspace fanout, guestEventAllowed events about projects they are
//     a project member of, sent here through their workspace-member scope.
//
// Anything without a resolvable project (workspace settings, agents, runs,
// project-less issues) never reaches a guest.

type guestRealtimeQuerier interface {
	ListLightsoftGuestProjectMemberships(ctx context.Context, workspaceID pgtype.UUID) ([]db.ListLightsoftGuestProjectMembershipsRow, error)
}

type lightsoftGuestRealtime struct {
	dir      *lightsoftguest.Directory
	q        guestRealtimeQuerier
	audience *dbPrivateProjectAudience
	now      func() time.Time

	mu    sync.Mutex
	cache map[string]guestProjects
}

type guestProjects struct {
	loadedAt time.Time
	// projects maps a guest user id to the project ids they are a member of.
	projects map[string]map[string]bool
}

// guestRealtime is installed by main; nil (tests) leaves fanout upstream's.
var guestRealtime *lightsoftGuestRealtime

// installLightsoftGuests wires guest handling into realtime: the hub marks
// guest sockets at connect, and listeners fan guests their projects' events.
func installLightsoftGuests(q *db.Queries, audience *dbPrivateProjectAudience) {
	dir := lightsoftguest.Shared(q)
	realtime.SetLightsoftGuestChecker(dir.IsGuest)
	guestRealtime = newLightsoftGuestRealtime(q, audience)
}

func newLightsoftGuestRealtime(q *db.Queries, audience *dbPrivateProjectAudience) *lightsoftGuestRealtime {
	return &lightsoftGuestRealtime{dir: lightsoftguest.Shared(q), q: q, audience: audience, now: time.Now, cache: map[string]guestProjects{}}
}

// guestEventAllowed lists the events about an issue or project a guest may
// receive for a project they can see. Runs (task:*), agents, autopilots and
// workspace configuration are left out.
func guestEventAllowed(eventType string) bool {
	switch {
	case strings.HasPrefix(eventType, "issue:"),
		strings.HasPrefix(eventType, "comment:"),
		strings.HasPrefix(eventType, "reaction:"),
		strings.HasPrefix(eventType, "issue_reaction:"),
		strings.HasPrefix(eventType, "subscriber:"),
		strings.HasPrefix(eventType, "project_resource:"):
		return true
	}
	switch eventType {
	case "issue_attachments:changed", "issue_labels:changed", "issue_metadata:changed", "issue_properties:changed",
		"activity:created", "project:updated", "project:deleted", "project:members_changed":
		return true
	}
	return false
}

func (g *lightsoftGuestRealtime) guests(ctx context.Context, workspaceID string, e events.Event) (map[string]map[string]bool, error) {
	if strings.HasPrefix(e.Type, "project:") || strings.HasPrefix(e.Type, "member:") || e.Type == "workspace:updated" {
		g.mu.Lock()
		delete(g.cache, workspaceID)
		g.mu.Unlock()
	}
	g.mu.Lock()
	cached, ok := g.cache[workspaceID]
	g.mu.Unlock()
	if ok && g.now().Sub(cached.loadedAt) < audienceTTL {
		return cached.projects, nil
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return nil, nil
	}
	rows, err := g.q.ListLightsoftGuestProjectMemberships(ctx, wsUUID)
	if err != nil {
		return nil, err
	}
	projects := map[string]map[string]bool{}
	for _, row := range rows {
		uid := util.UUIDToString(row.UserID)
		if projects[uid] == nil {
			projects[uid] = map[string]bool{}
		}
		if row.ProjectID.Valid {
			projects[uid][util.UUIDToString(row.ProjectID)] = true
		}
	}
	g.mu.Lock()
	if len(g.cache) >= maxTaskProjectCache {
		clear(g.cache)
	}
	g.cache[workspaceID] = guestProjects{loadedAt: g.now(), projects: projects}
	g.mu.Unlock()
	return projects, nil
}

// filterPrivate drops guests from a private project's audience for events
// guests may not receive.
func (g *lightsoftGuestRealtime) filterPrivate(e events.Event, recipients []string) []string {
	if g == nil || guestEventAllowed(e.Type) {
		return recipients
	}
	ctx, cancel := context.WithTimeout(context.Background(), audienceResolveTimeout)
	defer cancel()
	if has, err := g.dir.HasGuests(ctx, e.WorkspaceID); err == nil && !has {
		return recipients
	}
	guests, err := g.guests(ctx, e.WorkspaceID, e)
	if err != nil {
		// Unknown guests: send to nobody rather than to a possible guest.
		slog.Warn("realtime: dropping private event, guest lookup failed", "event_type", e.Type, "workspace_id", e.WorkspaceID, "error", err)
		return nil
	}
	out := recipients[:0:0]
	for _, id := range recipients {
		if _, guest := guests[id]; !guest {
			out = append(out, id)
		}
	}
	return out
}

// fanout sends a workspace-room event to the guests who can see its project.
func (g *lightsoftGuestRealtime) fanout(b realtime.Broadcaster, e events.Event, data []byte) {
	if g == nil || e.WorkspaceID == "" || !guestEventAllowed(e.Type) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), audienceResolveTimeout)
	defer cancel()
	has, err := g.dir.HasGuests(ctx, e.WorkspaceID)
	if err != nil || !has {
		return
	}
	guests, err := g.guests(ctx, e.WorkspaceID, e)
	if err != nil || len(guests) == 0 {
		return
	}
	projectIDs, err := g.audience.eventProjectIDs(ctx, e)
	if err != nil || len(projectIDs) == 0 {
		return
	}
	for userID, visible := range guests {
		sees := true
		for _, pid := range projectIDs {
			if pid == "" || !visible[pid] {
				sees = false
				break
			}
		}
		if sees {
			b.BroadcastToScope(realtime.ScopeWorkspaceMember, realtime.WorkspaceMemberScopeID(e.WorkspaceID, userID), data)
		}
	}
}
