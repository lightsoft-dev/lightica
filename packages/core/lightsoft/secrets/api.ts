import { queryOptions } from "@tanstack/react-query";
import { z } from "zod";
import { api } from "../../api";
import { parseWithFallback } from "../../api/schema";

// The Lightsoft secrets service (lightsoft/secrets) is mounted by Tailscale
// serve under this path on the team client origin, which is also the API
// origin Lightica (desktop) talks to.
const SERVICE_PATH = "/lightsoft-secrets";

let serviceUrlOverride: string | null = null;

/**
 * Platform override for the service URL. Web calls it at boot because its API
 * base is same-origin (and local web dev runs the service on its own port);
 * Desktop leaves it unset and derives the URL from its API base.
 */
export function configureSecretsService(opts: { baseUrl: string }) {
  serviceUrlOverride = opts.baseUrl.replace(/\/+$/, "");
}

export function secretsServiceUrl(): string {
  if (serviceUrlOverride !== null) return serviceUrlOverride;
  const base = typeof api.getBaseUrl === "function" ? api.getBaseUrl() : "";
  return `${(base ?? "").replace(/\/+$/, "")}${SERVICE_PATH}`;
}

/** Desktop holds its session as a bearer token; web (cookie mode) has none. */
function sessionToken(): string | null {
  return typeof api.getToken === "function" ? api.getToken() : null;
}

/** Sent on every state-changing request; the service rejects cookie writes without it. */
export const CSRF_HEADER = "X-Lightsoft-Secrets";

export const SECRET_NAME_PATTERN = /^[A-Z][A-Z0-9_]{0,127}$/;

export type PrincipalType = "member" | "agent" | "squad";
export interface Grant {
  principal_type: PrincipalType;
  principal_id: string;
}

export interface SecretMeta {
  id: string;
  name: string;
  description: string;
  owner_id: string;
  created_at: string;
  updated_at: string;
  updated_by: string;
  value_updated_at: string;
  version: number;
  grants: Grant[];
  last_read_at: string | null;
  last_read_actor_type: "member" | "agent" | null;
  last_read_actor_id: string | null;
  can_read: boolean;
  can_manage: boolean;
}

export interface SecretsViewer {
  user_id: string;
  role: string;
}

export interface SecretList {
  viewer: SecretsViewer | null;
  secrets: SecretMeta[];
}

export interface Enrollment {
  agent_id: string;
  enrolled_at: string;
  enrolled_by: string;
  last_used_at: string | null;
}

export interface AuditEvent {
  id: string;
  created_at: string;
  secret_id: string | null;
  secret_name: string | null;
  actor_type: string;
  actor_id: string;
  task_id: string | null;
  action: string;
  result: string;
  detail: Record<string, unknown>;
}

export interface AuditPage {
  events: AuditEvent[];
  next_before: string | null;
}

export interface AgentAccessSummary {
  agent_id: string;
  read_count: number;
  denied_count: number;
  last_read_at: string | null;
  secret_names: string[];
}

export interface AuditFilter {
  secret_id?: string;
  actor_type?: "agent" | "member";
  actor_id?: string;
  action?: string;
}

// Lenient on enums (kept as strings) so a newer service still parses.
const grantSchema = z.object({
  principal_type: z.string(),
  principal_id: z.string(),
});

const secretSchema = z.object({
  id: z.string(),
  name: z.string(),
  description: z.string().catch(""),
  owner_id: z.string().catch(""),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
  updated_by: z.string().catch(""),
  value_updated_at: z.string().catch(""),
  version: z.number().catch(0),
  grants: z.array(grantSchema).catch([]),
  last_read_at: z.string().nullable().catch(null),
  last_read_actor_type: z.string().nullable().catch(null),
  last_read_actor_id: z.string().nullable().catch(null),
  can_read: z.boolean().catch(false),
  can_manage: z.boolean().catch(false),
});

const secretListSchema = z.object({
  viewer: z.object({ user_id: z.string(), role: z.string() }).nullable().catch(null),
  secrets: z.array(secretSchema),
});

const enrollmentSchema = z.object({
  agent_id: z.string(),
  enrolled_at: z.string().catch(""),
  enrolled_by: z.string().catch(""),
  last_used_at: z.string().nullable().catch(null),
});

const auditEventSchema = z.object({
  id: z.string(),
  created_at: z.string(),
  secret_id: z.string().nullable().catch(null),
  secret_name: z.string().nullable().catch(null),
  actor_type: z.string(),
  actor_id: z.string(),
  task_id: z.string().nullable().catch(null),
  action: z.string(),
  result: z.string(),
  detail: z.record(z.string(), z.unknown()).catch({}),
});

const auditPageSchema = z.object({
  events: z.array(auditEventSchema),
  next_before: z.string().nullable().catch(null),
});

const agentSummarySchema = z.object({
  agent_id: z.string(),
  read_count: z.number().catch(0),
  denied_count: z.number().catch(0),
  last_read_at: z.string().nullable().catch(null),
  secret_names: z.array(z.string()).catch([]),
});

export class SecretsApiError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = "SecretsApiError";
    this.status = status;
  }
}

function wsPath(wsId: string, path: string): string {
  return `${secretsServiceUrl()}/api/w/${encodeURIComponent(wsId)}${path}`;
}

async function request(
  method: "GET" | "POST" | "PATCH" | "PUT" | "DELETE",
  url: string,
  body?: unknown,
): Promise<unknown> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (method !== "GET") headers[CSRF_HEADER] = "1";
  if (body !== undefined) headers["Content-Type"] = "application/json";
  const token = sessionToken();
  if (token) headers["Authorization"] = `Bearer ${token}`;

  let res: Response;
  try {
    res = await fetch(url, {
      method,
      headers,
      credentials: "include",
      cache: "no-store",
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new SecretsApiError(0, "시크릿 서비스에 연결하지 못했습니다.");
  }
  if (res.status === 204) return null;
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
        : `시크릿 서비스 오류 (${res.status})`;
    throw new SecretsApiError(res.status, message);
  }
  return data;
}

const EMPTY_SECRET: SecretMeta = {
  id: "",
  name: "",
  description: "",
  owner_id: "",
  created_at: "",
  updated_at: "",
  updated_by: "",
  value_updated_at: "",
  version: 0,
  grants: [],
  last_read_at: null,
  last_read_actor_type: null,
  last_read_actor_id: null,
  can_read: false,
  can_manage: false,
};

function parseSecret(data: unknown, endpoint: string): SecretMeta {
  return parseWithFallback<SecretMeta>(data, secretSchema, EMPTY_SECRET, { endpoint });
}

export const secretsApi = {
  async list(wsId: string): Promise<SecretList> {
    const data = await request("GET", wsPath(wsId, "/secrets"));
    return parseWithFallback<SecretList>(
      data,
      secretListSchema,
      { viewer: null, secrets: [] },
      { endpoint: "lightsoft-secrets GET /secrets" },
    );
  },
  async create(
    wsId: string,
    input: { name: string; description: string; value: string; grants: Grant[] },
  ): Promise<SecretMeta> {
    const data = await request("POST", wsPath(wsId, "/secrets"), input);
    return parseSecret(data, "lightsoft-secrets POST /secrets");
  },
  async update(
    wsId: string,
    id: string,
    input: { description?: string; value?: string },
  ): Promise<SecretMeta> {
    const data = await request("PATCH", wsPath(wsId, `/secrets/${encodeURIComponent(id)}`), input);
    return parseSecret(data, "lightsoft-secrets PATCH /secrets/{id}");
  },
  async setGrants(wsId: string, id: string, grants: Grant[]): Promise<SecretMeta> {
    const data = await request(
      "PUT",
      wsPath(wsId, `/secrets/${encodeURIComponent(id)}/grants`),
      { grants },
    );
    return parseSecret(data, "lightsoft-secrets PUT /secrets/{id}/grants");
  },
  async remove(wsId: string, id: string): Promise<void> {
    await request("DELETE", wsPath(wsId, `/secrets/${encodeURIComponent(id)}`));
  },
  async reveal(wsId: string, id: string): Promise<string> {
    const data = await request("POST", wsPath(wsId, `/secrets/${encodeURIComponent(id)}/reveal`));
    const parsed = z.object({ value: z.string() }).safeParse(data);
    if (!parsed.success) throw new SecretsApiError(500, "값을 읽지 못했습니다.");
    return parsed.data.value;
  },
  async enrollments(wsId: string): Promise<Enrollment[]> {
    const data = await request("GET", wsPath(wsId, "/agents"));
    return parseWithFallback<{ enrollments: Enrollment[] }>(
      data,
      z.object({ enrollments: z.array(enrollmentSchema) }),
      { enrollments: [] },
      { endpoint: "lightsoft-secrets GET /agents" },
    ).enrollments;
  },
  async enroll(wsId: string, agentId: string): Promise<void> {
    await request("POST", wsPath(wsId, `/agents/${encodeURIComponent(agentId)}/enroll`));
  },
  async unenroll(wsId: string, agentId: string): Promise<void> {
    await request("DELETE", wsPath(wsId, `/agents/${encodeURIComponent(agentId)}/enroll`));
  },
  async audit(wsId: string, filter: AuditFilter, before?: string): Promise<AuditPage> {
    const params = new URLSearchParams();
    for (const [key, value] of Object.entries(filter)) {
      if (value) params.set(key, value);
    }
    if (before) params.set("before", before);
    params.set("limit", "50");
    const data = await request("GET", wsPath(wsId, `/audit?${params.toString()}`));
    return parseWithFallback<AuditPage>(
      data,
      auditPageSchema,
      { events: [], next_before: null },
      { endpoint: "lightsoft-secrets GET /audit" },
    );
  },
  async agentSummaries(wsId: string): Promise<AgentAccessSummary[]> {
    const data = await request("GET", wsPath(wsId, "/audit/agents"));
    return parseWithFallback<{ agents: AgentAccessSummary[] }>(
      data,
      z.object({ agents: z.array(agentSummarySchema) }),
      { agents: [] },
      { endpoint: "lightsoft-secrets GET /audit/agents" },
    ).agents;
  },
};

export const secretsKeys = {
  all: (wsId: string) => ["lightsoft-secrets", wsId] as const,
  list: (wsId: string) => [...secretsKeys.all(wsId), "secrets"] as const,
  enrollments: (wsId: string) => [...secretsKeys.all(wsId), "enrollments"] as const,
  audit: (wsId: string, filter: AuditFilter) => [...secretsKeys.all(wsId), "audit", filter] as const,
  agentSummaries: (wsId: string) => [...secretsKeys.all(wsId), "audit-agents"] as const,
};

export function secretListOptions(wsId: string) {
  return queryOptions({
    queryKey: secretsKeys.list(wsId),
    queryFn: () => secretsApi.list(wsId),
    enabled: !!wsId,
  });
}

export function enrollmentListOptions(wsId: string) {
  return queryOptions({
    queryKey: secretsKeys.enrollments(wsId),
    queryFn: () => secretsApi.enrollments(wsId),
    enabled: !!wsId,
  });
}

export function agentSummaryOptions(wsId: string) {
  return queryOptions({
    queryKey: secretsKeys.agentSummaries(wsId),
    queryFn: () => secretsApi.agentSummaries(wsId),
    enabled: !!wsId,
  });
}

export function errorMessage(error: unknown): string {
  if (error instanceof SecretsApiError) return error.message;
  if (error instanceof Error && error.message) return error.message;
  return "요청을 처리하지 못했습니다.";
}
