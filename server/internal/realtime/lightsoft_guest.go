package realtime

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// Outside collaborators (Lightsoft fork, LIGH-282). A guest's socket is
// marked at connect and then skipped by workspace-room and global fanout;
// it still receives what is addressed to the guest (user and workspace-member
// scopes), and cmd/server sends it the issue events of its own projects
// through the workspace-member scope.

// GuestChecker reports whether userID is a guest of workspaceID.
type GuestChecker func(ctx context.Context, workspaceID, userID string) (bool, error)

var lightsoftGuestChecker atomic.Pointer[GuestChecker]

// SetLightsoftGuestChecker installs the guest lookup. Without one no socket
// is marked.
func SetLightsoftGuestChecker(fn GuestChecker) {
	if fn == nil {
		lightsoftGuestChecker.Store(nil)
		return
	}
	lightsoftGuestChecker.Store(&fn)
}

// markLightsoftGuest sets client.lightsoftGuest. It returns false when the
// lookup failed; the caller refuses the connection rather than admit a
// possible guest as a member.
func markLightsoftGuest(client *Client) bool {
	check := lightsoftGuestChecker.Load()
	if check == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), membershipLookupTimeout)
	defer cancel()
	guest, err := (*check)(ctx, client.workspaceID, client.userID)
	if err != nil {
		slog.Warn("ws lightsoft guest lookup failed", "user_id", client.userID, "workspace_id", client.workspaceID, "error", err)
		return false
	}
	client.lightsoftGuest = guest
	return true
}

// LightsoftDisconnect closes userID's sockets in workspaceID so they
// reconnect and are marked again, after their guest status changed.
func (h *Hub) LightsoftDisconnect(workspaceID, userID string) {
	h.mu.RLock()
	var clients []*Client
	for client := range h.clients {
		if client.workspaceID == workspaceID && client.userID == userID {
			clients = append(clients, client)
		}
	}
	h.mu.RUnlock()
	deadline := time.Now().Add(removalNoticeWait)
	for _, client := range clients {
		h.stopClient(client, nil, deadline)
	}
}
