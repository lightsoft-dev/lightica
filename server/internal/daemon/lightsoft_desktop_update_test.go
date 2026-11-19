package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// Lightsoft fork (LIGH-191): Lightica must never take an upstream update. The
// Desktop app launches its daemon with MULTICA_LAUNCHED_BY=desktop
// (apps/desktop/src/main/daemon-spawn-env.ts); this pins the daemon half of
// that contract — an update sent from the Runtimes page is refused instead of
// replacing Lightica's bundled CLI with a multica-ai release.
func TestDesktopManagedDaemonRefusesRuntimeUpdates(t *testing.T) {
	var payload map[string]any
	d, reportCalls := updateReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode update report: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	})
	d.cfg.LaunchedBy = "desktop"
	d.runUpdateFn = func(string) (string, error) {
		t.Fatal("a Desktop-managed daemon must not download a CLI release")
		return "", nil
	}

	d.handleUpdate(context.Background(), "runtime-1", &PendingUpdate{ID: "update-1", TargetVersion: "v0.12.0"})

	if got := atomic.LoadInt32(reportCalls); got != 1 {
		t.Fatalf("update reports = %d, want 1", got)
	}
	if payload["status"] != "failed" || !strings.Contains(payload["error"].(string), "managed by Multica Desktop") {
		t.Fatalf("report = %v, want a failed report naming Desktop", payload)
	}
}
