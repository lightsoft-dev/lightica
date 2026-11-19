import { queryOptions } from "@tanstack/react-query";
import { z } from "zod";
import { api } from "../../api";
import { parseWithFallback } from "../../api/schema";
import type { RuntimeRateLimits, RuntimeRateLimitWindow } from "../../types";

// The Lightsoft subscription-limit service (lightsoft/usage) is mounted by
// Tailscale serve under this path on the team client origin, which is also
// the API origin Lightica (desktop) talks to.
const SERVICE_PATH = "/lightsoft-usage";

let serviceUrlOverride: string | null = null;

/**
 * Platform override for the service URL. Web calls it at boot because its API
 * base is same-origin (and local web dev runs the service on its own port);
 * Desktop leaves it unset and derives the URL from its API base.
 */
export function configureUsageService(opts: { baseUrl: string }) {
  serviceUrlOverride = opts.baseUrl.replace(/\/+$/, "");
}

export function usageServiceUrl(): string {
  if (serviceUrlOverride !== null) return serviceUrlOverride;
  const base = typeof api.getBaseUrl === "function" ? api.getBaseUrl() : "";
  return `${(base ?? "").replace(/\/+$/, "")}${SERVICE_PATH}`;
}

/** Desktop holds its session as a bearer token; web (cookie mode) has none. */
function sessionToken(): string | null {
  return typeof api.getToken === "function" ? api.getToken() : null;
}

export interface LimitHistoryPoint {
  t: string;
  used_percent: number;
}

export interface LimitHistorySeries {
  kind: RuntimeRateLimitWindow["kind"];
  window_minutes: number;
  points: LimitHistoryPoint[];
}

export interface SubscriptionAccount {
  id: string;
  name: string;
  provider: string;
  runtime_ids: string[];
  /** Same shape as a runtime's snapshot, so the runtime-page helpers apply. */
  rate_limits: RuntimeRateLimits | null;
  checked_at: string | null;
  /** Why the last read failed; the snapshot above is then the last good one. */
  error: string | null;
  history: LimitHistorySeries[];
}

export interface SubscriptionLimits {
  interval_seconds: number;
  accounts: SubscriptionAccount[];
}

// Unknown kinds/statuses from a newer service degrade to the generic window
// and "no verdict" instead of failing the whole response.
const windowKind = z.enum(["session", "weekly", "weekly_opus", "weekly_sonnet", "other"]).catch("other");
const windowStatus = z.enum(["allowed", "warning", "rejected", ""]).catch("");

const windowSchema = z.object({
  kind: windowKind,
  used_percent: z.number().optional().catch(undefined),
  window_minutes: z.number().catch(0),
  resets_at: z.string().optional().catch(undefined),
  status: windowStatus,
});

const rateLimitsSchema = z.object({
  plan_type: z.string().catch(""),
  status: windowStatus,
  windows: z.array(windowSchema).catch([]),
  credits: z
    .object({ has_credits: z.boolean(), unlimited: z.boolean(), balance: z.string().catch("") })
    .optional()
    .catch(undefined),
  observed_at: z.string(),
});

const historySeriesSchema = z.object({
  kind: windowKind,
  window_minutes: z.number().catch(0),
  points: z.array(z.object({ t: z.string(), used_percent: z.number() })).catch([]),
});

const accountSchema = z.object({
  id: z.string(),
  name: z.string().catch(""),
  provider: z.string().catch(""),
  runtime_ids: z.array(z.string()).catch([]),
  rate_limits: rateLimitsSchema.nullable().catch(null),
  checked_at: z.string().nullable().catch(null),
  error: z.string().nullable().catch(null),
  history: z.array(historySeriesSchema).catch([]),
});

const limitsSchema = z.object({
  interval_seconds: z.number().catch(300),
  accounts: z.array(accountSchema),
});

const EMPTY_LIMITS: SubscriptionLimits = { interval_seconds: 300, accounts: [] };

export class UsageApiError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = "UsageApiError";
    this.status = status;
  }
}

export const LIMIT_HISTORY_DAYS = [7, 30] as const;
export type LimitHistoryDays = (typeof LIMIT_HISTORY_DAYS)[number];

export async function getSubscriptionLimits(
  wsId: string,
  days: LimitHistoryDays,
): Promise<SubscriptionLimits> {
  const url = `${usageServiceUrl()}/api/w/${encodeURIComponent(wsId)}/limits?days=${days}`;
  const headers: Record<string, string> = { Accept: "application/json" };
  const token = sessionToken();
  if (token) headers["Authorization"] = `Bearer ${token}`;

  let res: Response;
  try {
    res = await fetch(url, { method: "GET", headers, credentials: "include", cache: "no-store" });
  } catch {
    throw new UsageApiError(0, "구독 한도 서비스에 연결하지 못했습니다.");
  }
  const text = await res.text();
  let data: unknown = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = null;
    }
  }
  if (!res.ok) {
    const message =
      data && typeof data === "object" && typeof (data as { error?: unknown }).error === "string"
        ? (data as { error: string }).error
        : `구독 한도 서비스 오류 (${res.status})`;
    throw new UsageApiError(res.status, message);
  }
  return parseWithFallback<SubscriptionLimits>(data, limitsSchema, EMPTY_LIMITS, {
    endpoint: "lightsoft-usage/limits",
  });
}

export const usageKeys = {
  limits: (wsId: string, days: number) => ["lightsoft", "usage", wsId, "limits", days] as const,
};

// The service polls every few minutes; a one-minute refetch keeps an open
// page in step without a realtime event.
export function subscriptionLimitsOptions(wsId: string, days: LimitHistoryDays) {
  return queryOptions({
    queryKey: usageKeys.limits(wsId, days),
    queryFn: () => getSubscriptionLimits(wsId, days),
    enabled: !!wsId,
    staleTime: 30 * 1000,
    refetchInterval: 60 * 1000,
  });
}
