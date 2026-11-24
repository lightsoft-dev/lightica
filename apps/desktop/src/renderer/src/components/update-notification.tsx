import { useEffect, useRef, useState } from "react";
import { AnimatePresence, motion } from "motion/react";
import { useReducedMotion } from "@multica/core/appearance";
import { ArrowDownToLine, RefreshCw, X } from "@multica/ui/icons";
import { UI_EASE_OUT, UI_MOTION_DURATION } from "@multica/ui/lib/motion";
import { useT } from "@multica/views/i18n";
import type { UpdaterDownloadState } from "../../../shared/updater-types";
import { LIGHTICA_RELEASES_URL, LIGHTICA_UPDATE_COPY } from "../../../shared/lightica";

// Downloads run silently in the background. The renderer only renders UI once
// the package is fully downloaded and waiting for a restart.
// Lightica (LIGH-191): when this app cannot install the published build
// itself, the banner links to its dmg instead ("manual"). Otherwise "지금
// 업데이트" quits, installs and reopens the app (LIGH-268). A window opened
// after the download (say, from the update's system notification) reads the
// state main already holds (LIGH-257).
type UpdateState =
  | { status: "idle" }
  | { status: "ready"; version: string }
  | { status: "manual"; version: string; downloadUrl: string };

function bannerState(download: UpdaterDownloadState): UpdateState | null {
  if (download.status === "downloaded") return { status: "ready", version: download.version };
  if (download.status === "manual") {
    return { status: "manual", version: download.version, downloadUrl: download.downloadUrl };
  }
  return null;
}

export function UpdateNotification() {
  const { t } = useT("settings");
  const prefersReducedMotion = useReducedMotion();
  const [state, setState] = useState<UpdateState>({ status: "idle" });
  const [dismissed, setDismissed] = useState(false);
  const shownState = useRef<string | null>(null);

  useEffect(() => {
    const cleanup = window.updater.onUpdateDownloaded((info) => {
      setState({ status: "ready", version: info.version });
      setDismissed(false);
    });
    return cleanup;
  }, []);

  useEffect(() => {
    let mounted = true;
    // Checks repeat hourly; only a newer version brings a dismissed banner back.
    const show = (next: UpdateState | null) => {
      if (!next || next.status === "idle" || !mounted) return;
      const key = `${next.status}:${next.version}`;
      if (shownState.current === key) return;
      shownState.current = key;
      setState(next);
      setDismissed(false);
    };
    const unsubscribe = window.updater.onDownloadState((download) => show(bannerState(download)));
    void window.updater.getDownloadState().then((download) => show(bannerState(download))).catch(() => {});
    return () => {
      mounted = false;
      unsubscribe();
    };
  }, []);

  return (
    <AnimatePresence>
      {state.status !== "idle" && !dismissed && (
        <motion.div
          key="update-notification"
          initial={{ opacity: 0, y: prefersReducedMotion ? 0 : 8 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: prefersReducedMotion ? 0 : 8 }}
          transition={{
            duration: prefersReducedMotion ? 0 : UI_MOTION_DURATION.standard,
            ease: UI_EASE_OUT,
          }}
          className="fixed bottom-4 right-4 z-50 w-80 rounded-lg border border-border bg-background p-4 shadow-lg"
        >
          <button
            type="button"
            onClick={() => setDismissed(true)}
            className="absolute top-2 right-2 rounded-md p-1 text-muted-foreground hover:text-foreground transition-colors"
            aria-label={t(($) => $.desktop.updates.banner_dismiss)}
          >
            <X className="size-3.5" />
          </button>

          {state.status === "manual" ? (
            <div className="flex items-start gap-3">
              <div className="mt-0.5 rounded-md bg-primary/10 p-1.5">
                <ArrowDownToLine className="size-4 text-primary" />
              </div>
              <div className="flex-1 min-w-0">
                <p className="text-body font-medium break-all pr-4">
                  {LIGHTICA_UPDATE_COPY.manualTitle(state.version)}
                </p>
                <p className="text-caption text-muted-foreground mt-0.5">
                  {LIGHTICA_UPDATE_COPY.manualBody}
                </p>
                <div className="mt-2 flex items-center gap-1.5">
                  <button
                    type="button"
                    onClick={() => window.desktopAPI.openExternal(LIGHTICA_RELEASES_URL)}
                    className="inline-flex items-center rounded-md border border-border bg-background px-3 py-1.5 text-caption font-medium text-foreground hover:bg-accent transition-colors"
                  >
                    {LIGHTICA_UPDATE_COPY.releases}
                  </button>
                  <button
                    type="button"
                    onClick={() => window.desktopAPI.openExternal(state.downloadUrl)}
                    className="inline-flex items-center rounded-md bg-primary px-3 py-1.5 text-caption font-medium text-primary-foreground hover:bg-primary/90 transition-colors"
                  >
                    {LIGHTICA_UPDATE_COPY.download}
                  </button>
                </div>
              </div>
            </div>
          ) : (
            <div className="flex items-start gap-3">
              <div className="mt-0.5 rounded-md bg-success/10 p-1.5">
                <RefreshCw className="size-4 text-success" />
              </div>
              <div className="flex-1 min-w-0">
                <p className="text-body font-medium break-all pr-4">
                  {LIGHTICA_UPDATE_COPY.readyTitle(state.version)}
                </p>
                <p className="text-caption text-muted-foreground mt-0.5">
                  {LIGHTICA_UPDATE_COPY.readyBody}
                </p>
                <div className="mt-2 flex items-center gap-1.5">
                  <button
                    type="button"
                    // Lightica versions (0.11.0-YYYYMMDD) are not on multica.ai's changelog.
                    onClick={() => window.desktopAPI.openExternal(LIGHTICA_RELEASES_URL)}
                    className="inline-flex items-center rounded-md border border-border bg-background px-3 py-1.5 text-caption font-medium text-foreground hover:bg-accent transition-colors"
                  >
                    {LIGHTICA_UPDATE_COPY.releases}
                  </button>
                  <button
                    type="button"
                    onClick={() => window.updater.installUpdate()}
                    className="inline-flex items-center rounded-md bg-primary px-3 py-1.5 text-caption font-medium text-primary-foreground hover:bg-primary/90 transition-colors"
                  >
                    {LIGHTICA_UPDATE_COPY.installNow}
                  </button>
                </div>
              </div>
            </div>
          )}
        </motion.div>
      )}
    </AnimatePresence>
  );
}
