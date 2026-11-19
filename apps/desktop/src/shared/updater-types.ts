export interface UpdaterPreferences {
  automaticUpdates: boolean;
}

export type UpdaterFailureReason = "unavailable" | "network" | "unknown";

export type UpdaterDownloadState =
  | { status: "idle" }
  | { status: "downloading"; version: string }
  | { status: "downloaded"; version: string }
  // Lightica (LIGH-191): this app cannot install the published build itself.
  | { status: "manual"; version: string; downloadUrl: string }
  | { status: "error"; reason: UpdaterFailureReason };

export type ManualUpdateCheckResult =
  | {
      ok: true;
      currentVersion: string;
      latestVersion: string;
      available: boolean;
      downloadState: UpdaterDownloadState;
    }
  | { ok: false; error: string; reason: UpdaterFailureReason };
