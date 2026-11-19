// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { UsageApiError, configureUsageService, getSubscriptionLimits, usageServiceUrl } from "./api";

// Web runs the API client in cookie mode with a same-origin base; Lightica
// (desktop) runs it with the team client origin and a bearer session token.
const apiState = vi.hoisted(() => ({ baseUrl: "", token: null as string | null }));
vi.mock("../../api", () => ({
  api: { getBaseUrl: () => apiState.baseUrl, getToken: () => apiState.token },
}));

const WS = "11111111-1111-1111-1111-111111111111";

function mockFetch(status: number, body: unknown) {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(body === undefined ? null : JSON.stringify(body), { status }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
  apiState.baseUrl = "";
  apiState.token = null;
});

describe("getSubscriptionLimits", () => {
  it("reads with the cookie on web and parses the runtime snapshot shape", async () => {
    const fetchMock = mockFetch(200, {
      interval_seconds: 300,
      accounts: [
        {
          id: "claude-max",
          name: "Claude Max",
          provider: "claude",
          runtime_ids: ["rt-1"],
          rate_limits: {
            plan_type: "max_20x",
            status: "",
            windows: [
              { kind: "weekly", used_percent: 27, window_minutes: 10080, resets_at: "2026-11-21T22:00:00Z", status: "" },
            ],
            observed_at: "2026-11-20T06:00:00Z",
          },
          checked_at: "2026-11-20T06:00:00Z",
          error: null,
          history: [{ kind: "weekly", window_minutes: 10080, points: [{ t: "2026-11-20T06:00:00Z", used_percent: 27 }] }],
        },
      ],
    });

    const limits = await getSubscriptionLimits(WS, 7);

    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe(`/lightsoft-usage/api/w/${WS}/limits?days=7`);
    expect(init.method).toBe("GET");
    expect(init.credentials).toBe("include");
    expect(init.headers.Authorization).toBeUndefined();
    expect(limits.accounts[0]!.rate_limits!.windows[0]).toMatchObject({ kind: "weekly", used_percent: 27 });
    expect(limits.accounts[0]!.history[0]!.points).toHaveLength(1);
  });

  it("sends Lightica's bearer session to the service on its API origin", async () => {
    apiState.baseUrl = "https://team.example:3444/";
    apiState.token = "eyJ.session.sig";
    const fetchMock = mockFetch(200, { interval_seconds: 300, accounts: [] });

    await getSubscriptionLimits(WS, 30);

    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe(`https://team.example:3444/lightsoft-usage/api/w/${WS}/limits?days=30`);
    expect(init.headers.Authorization).toBe("Bearer eyJ.session.sig");
  });

  it("degrades unknown window kinds and malformed fields instead of failing", async () => {
    mockFetch(200, {
      accounts: [
        {
          id: "codex-main",
          rate_limits: {
            plan_type: 3,
            status: "throttled",
            windows: [{ kind: "monthly", used_percent: "high", window_minutes: 43200, status: "nope" }],
            observed_at: "2026-11-20T06:00:00Z",
          },
          history: "bad",
        },
      ],
    });

    const limits = await getSubscriptionLimits(WS, 7);
    const account = limits.accounts[0]!;

    expect(limits.interval_seconds).toBe(300);
    expect(account.name).toBe("");
    expect(account.runtime_ids).toEqual([]);
    expect(account.history).toEqual([]);
    expect(account.rate_limits).toMatchObject({ plan_type: "", status: "" });
    expect(account.rate_limits!.windows[0]).toEqual({
      kind: "other",
      used_percent: undefined,
      window_minutes: 43200,
      resets_at: undefined,
      status: "",
    });
  });

  it("surfaces the service's error message", async () => {
    mockFetch(403, { error: "이 워크스페이스의 구성원이 아닙니다." });

    const err = await getSubscriptionLimits(WS, 7).catch((e: unknown) => e);

    expect(err).toBeInstanceOf(UsageApiError);
    expect((err as UsageApiError).status).toBe(403);
    expect((err as UsageApiError).message).toBe("이 워크스페이스의 구성원이 아닙니다.");
  });

  it("reports an unreachable service", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));

    const err = await getSubscriptionLimits(WS, 7).catch((e: unknown) => e);

    expect(err).toBeInstanceOf(UsageApiError);
    expect((err as UsageApiError).status).toBe(0);
  });

  // Last: the override is module state and would leak into later tests.
  it("uses the web override (local dev runs the service on its own port)", () => {
    apiState.baseUrl = "https://team.example:3444";
    configureUsageService({ baseUrl: "http://localhost:13501/lightsoft-usage/" });

    expect(usageServiceUrl()).toBe("http://localhost:13501/lightsoft-usage");
  });
});
