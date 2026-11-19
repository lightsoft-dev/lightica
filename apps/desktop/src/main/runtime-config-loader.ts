import { app } from "electron";
import { readFile } from "fs/promises";
import { join } from "path";
import { LIGHTICA_DEFAULT_API_URL, LIGHTICA_HOME_DIR } from "../shared/lightica";
import {
  deriveAppUrl,
  deriveWsUrl,
  parseRuntimeConfig,
  runtimeConfigFromDevEnv,
  type RuntimeConfig,
  type RuntimeConfigEnv,
  type RuntimeConfigResult,
} from "../shared/runtime-config";

export async function loadRuntimeConfig(options: {
  isDev: boolean;
  env: RuntimeConfigEnv;
  configPath?: string;
}): Promise<RuntimeConfigResult> {
  if (options.isDev) {
    try {
      return { ok: true, config: runtimeConfigFromDevEnv(options.env) };
    } catch (err) {
      return { ok: false, error: { message: errorMessage(err) } };
    }
  }

  const configPath = options.configPath ?? desktopConfigPath();
  try {
    const raw = await readFile(configPath, "utf-8");
    return { ok: true, config: parseRuntimeConfig(raw) };
  } catch (err) {
    if (isMissingFileError(err)) {
      return { ok: true, config: lighticaDefaultRuntimeConfig() };
    }
    return {
      ok: false,
      error: {
        message: `Invalid ${configPath}: ${errorMessage(err)}`,
      },
    };
  }
}

export function desktopConfigPath(): string {
  return join(app.getPath("home"), LIGHTICA_HOME_DIR, "desktop.json");
}

// Lightica talks to the team server unless desktop.json says otherwise.
function lighticaDefaultRuntimeConfig(): RuntimeConfig {
  return {
    schemaVersion: 1,
    apiUrl: LIGHTICA_DEFAULT_API_URL,
    wsUrl: deriveWsUrl(LIGHTICA_DEFAULT_API_URL),
    appUrl: deriveAppUrl(LIGHTICA_DEFAULT_API_URL),
  };
}

function isMissingFileError(err: unknown): boolean {
  return Boolean(
    err &&
      typeof err === "object" &&
      "code" in err &&
      (err as NodeJS.ErrnoException).code === "ENOENT",
  );
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export type { RuntimeConfig, RuntimeConfigResult };
