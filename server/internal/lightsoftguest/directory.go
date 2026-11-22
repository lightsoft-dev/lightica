// Package lightsoftguest knows who is an outside collaborator ("guest") of a
// workspace (Lightsoft fork, LIGH-282).
//
// A workspace lists its guests by email in workspace.settings under
// SettingsKey. A guest keeps the "member" role in the member table, so the
// schema stays upstream's; everything that narrows a guest asks this package
// (or the matching SQL in lightsoft_guest.sql) instead. Listing an email
// before inviting it means the account is a guest from its first request.
package lightsoftguest

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// SettingsKey is the workspace.settings key holding the guest email list.
const SettingsKey = "lightsoft_guests"

// NormalizeEmail is the form emails are stored and compared in.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Querier is the subset of *db.Queries the directory reads.
type Querier interface {
	ListLightsoftGuestWorkspaces(ctx context.Context) ([]db.ListLightsoftGuestWorkspacesRow, error)
	GetUser(ctx context.Context, id pgtype.UUID) (db.User, error)
}

const (
	// snapshotTTL bounds how long a guest list edited outside this process
	// (or by another replica) takes to apply. Edits through the guest API
	// invalidate immediately.
	snapshotTTL = 3 * time.Second
	emailTTL    = 10 * time.Minute
	maxEmails   = 4096
)

// generation is bumped by InvalidateAll so every directory in the process
// reloads its snapshot on the next lookup.
var generation atomic.Uint64

// InvalidateAll makes every directory reload on its next lookup. The guest
// API calls it after changing a list.
func InvalidateAll() { generation.Add(1) }

var shared sync.Map // Querier -> *Directory

// Shared returns the process-wide directory over q, so the HTTP gate, the
// handlers and realtime fanout share one cache per database handle.
func Shared(q Querier) *Directory {
	if q == nil {
		return nil
	}
	if d, ok := shared.Load(q); ok {
		return d.(*Directory)
	}
	d, _ := shared.LoadOrStore(q, NewDirectory(q))
	return d.(*Directory)
}

// Directory caches which workspaces name guests and each user's email. It is
// safe for concurrent use. A lookup that cannot be answered returns an error,
// and callers fail closed.
type Directory struct {
	q   Querier
	now func() time.Time

	mu       sync.Mutex
	loadedAt time.Time
	gen      uint64
	// guests maps a workspace id to its guest emails. Absent = no guests.
	guests map[string]map[string]struct{}
	emails map[string]emailEntry

	refresh sync.Mutex // one snapshot load at a time
}

type emailEntry struct {
	email    string
	loadedAt time.Time
}

// NewDirectory builds a directory over q.
func NewDirectory(q Querier) *Directory {
	return &Directory{q: q, now: time.Now, emails: map[string]emailEntry{}}
}

// Invalidate drops the snapshot so the next lookup reloads it.
func (d *Directory) Invalidate() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.guests = nil
	d.mu.Unlock()
}

func (d *Directory) fresh() (map[string]map[string]struct{}, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.guests != nil && d.gen == generation.Load() && d.now().Sub(d.loadedAt) < snapshotTTL {
		return d.guests, true
	}
	return nil, false
}

func (d *Directory) snapshot(ctx context.Context) (map[string]map[string]struct{}, error) {
	if g, ok := d.fresh(); ok {
		return g, nil
	}
	d.refresh.Lock()
	defer d.refresh.Unlock()
	if g, ok := d.fresh(); ok {
		return g, nil
	}
	gen := generation.Load()

	rows, err := d.q.ListLightsoftGuestWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	loaded := make(map[string]map[string]struct{}, len(rows))
	for _, row := range rows {
		emails := ParseEmails(row.Guests)
		if len(emails) == 0 {
			continue
		}
		set := make(map[string]struct{}, len(emails))
		for _, e := range emails {
			set[e] = struct{}{}
		}
		loaded[util.UUIDToString(row.ID)] = set
	}
	d.mu.Lock()
	d.guests = loaded
	d.loadedAt = d.now()
	d.gen = gen
	d.mu.Unlock()
	return loaded, nil
}

func (d *Directory) email(ctx context.Context, userID string) (string, error) {
	d.mu.Lock()
	cached, ok := d.emails[userID]
	d.mu.Unlock()
	if ok && d.now().Sub(cached.loadedAt) < emailTTL {
		return cached.email, nil
	}
	uid, err := util.ParseUUID(userID)
	if err != nil {
		return "", nil
	}
	user, err := d.q.GetUser(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		// No such account: no guest either; membership checks answer it.
		return "", nil
	}
	if err != nil {
		return "", err
	}
	email := NormalizeEmail(user.Email)
	d.mu.Lock()
	if len(d.emails) >= maxEmails {
		clear(d.emails)
	}
	d.emails[userID] = emailEntry{email: email, loadedAt: d.now()}
	d.mu.Unlock()
	return email, nil
}

// IsGuest reports whether userID is listed as a guest of workspaceID. It does
// not check membership; callers already have.
func (d *Directory) IsGuest(ctx context.Context, workspaceID, userID string) (bool, error) {
	if d == nil || workspaceID == "" || userID == "" {
		return false, nil
	}
	snap, err := d.snapshot(ctx)
	if err != nil {
		return false, err
	}
	set, ok := snap[workspaceID]
	if !ok {
		return false, nil
	}
	email, err := d.email(ctx, userID)
	if err != nil {
		return false, err
	}
	_, guest := set[email]
	return guest && email != "", nil
}

// GuestWorkspaces returns the workspaces that list userID as a guest.
func (d *Directory) GuestWorkspaces(ctx context.Context, userID string) (map[string]bool, error) {
	if d == nil || userID == "" {
		return nil, nil
	}
	snap, err := d.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if len(snap) == 0 {
		return nil, nil
	}
	email, err := d.email(ctx, userID)
	if err != nil {
		return nil, err
	}
	if email == "" {
		return nil, nil
	}
	var out map[string]bool
	for wsID, set := range snap {
		if _, ok := set[email]; ok {
			if out == nil {
				out = map[string]bool{}
			}
			out[wsID] = true
		}
	}
	return out, nil
}

// HasGuests reports whether workspaceID lists any guest.
func (d *Directory) HasGuests(ctx context.Context, workspaceID string) (bool, error) {
	if d == nil {
		return false, nil
	}
	snap, err := d.snapshot(ctx)
	if err != nil {
		return false, err
	}
	return len(snap[workspaceID]) > 0, nil
}

// ParseEmails reads a stored guest list, normalized, deduplicated and with
// blanks dropped. Anything that is not a JSON array of strings reads as empty.
func ParseEmails(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var list []any
	if json.Unmarshal(raw, &list) != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, v := range list {
		s, ok := v.(string)
		if !ok {
			continue
		}
		e := NormalizeEmail(s)
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}
