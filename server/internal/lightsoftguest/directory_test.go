package lightsoftguest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeQ struct {
	workspaces []db.ListLightsoftGuestWorkspacesRow
	users      map[string]string
	loads      int
	err        error
}

func (f *fakeQ) ListLightsoftGuestWorkspaces(context.Context) ([]db.ListLightsoftGuestWorkspacesRow, error) {
	f.loads++
	return f.workspaces, f.err
}

func (f *fakeQ) GetUser(_ context.Context, id pgtype.UUID) (db.User, error) {
	email, ok := f.users[util.UUIDToString(id)]
	if !ok {
		return db.User{}, errors.New("no user")
	}
	return db.User{ID: id, Email: email}, nil
}

const (
	ws1   = "11111111-1111-4111-8111-111111111111"
	ws2   = "22222222-2222-4222-8222-222222222222"
	guest = "90000000-0000-4000-8000-000000000007"
	staff = "a0000000-0000-4000-8000-000000000001"
)

func TestParseEmails(t *testing.T) {
	got := ParseEmails([]byte(`[" A@Example.com ", "a@example.com", "", 3, "b@x.io"]`))
	if len(got) != 2 || got[0] != "a@example.com" || got[1] != "b@x.io" {
		t.Fatalf("ParseEmails = %v", got)
	}
	if ParseEmails([]byte(`{"a":1}`)) != nil || ParseEmails(nil) != nil {
		t.Fatal("non-array should read as empty")
	}
}

func TestDirectory(t *testing.T) {
	q := &fakeQ{
		workspaces: []db.ListLightsoftGuestWorkspacesRow{{ID: util.MustParseUUID(ws1), Guests: []byte(`["Partner@Example.com"]`)}},
		users:      map[string]string{guest: "partner@example.com", staff: "staff@lightsoft.dev"},
	}
	d := NewDirectory(q)
	ctx := context.Background()
	if ok, err := d.IsGuest(ctx, ws1, guest); err != nil || !ok {
		t.Fatalf("guest in ws1 = %v %v", ok, err)
	}
	// Other spellings of the same id are the same workspace.
	if ok, _ := d.IsGuest(ctx, strings.ToUpper(ws1), guest); !ok {
		t.Fatal("upper-case workspace id escaped the guest check")
	}
	if ok, _ := d.IsGuest(ctx, strings.ReplaceAll(ws1, "-", ""), guest); !ok {
		t.Fatal("dashless workspace id escaped the guest check")
	}
	if ok, _ := d.IsGuest(ctx, ws2, guest); ok {
		t.Fatal("guest status leaked to another workspace")
	}
	if ok, _ := d.IsGuest(ctx, ws1, staff); ok {
		t.Fatal("staff read as guest")
	}
	ws, _ := d.GuestWorkspaces(ctx, guest)
	if len(ws) != 1 || !ws[ws1] {
		t.Fatalf("GuestWorkspaces = %v", ws)
	}
	if has, _ := d.HasGuests(ctx, ws2); has {
		t.Fatal("ws2 has no guests")
	}
	loads := q.loads
	_, _ = d.IsGuest(ctx, ws1, guest)
	if q.loads != loads {
		t.Fatal("snapshot not cached")
	}
	InvalidateAll()
	q.workspaces = nil
	if ok, _ := d.IsGuest(ctx, ws1, guest); ok {
		t.Fatal("InvalidateAll did not reload the list")
	}
	q.err = errors.New("db down")
	InvalidateAll()
	if _, err := d.IsGuest(ctx, ws1, guest); err == nil {
		t.Fatal("lookup failure must surface so callers fail closed")
	}
}
