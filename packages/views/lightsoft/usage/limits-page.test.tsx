// @vitest-environment jsdom

import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentRuntime } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };

const listRuntimes = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", () => ({
  api: { getBaseUrl: () => "", getToken: () => null, listRuntimes },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("../../common/use-viewing-timezone", () => ({
  useViewingTimezone: () => "UTC",
}));

import { SubscriptionLimitsPage } from "./limits-page";

const NOW = Date.parse("2026-11-20T06:00:00Z");

function runtime(id: string, name: string): AgentRuntime {
  return {
    id,
    workspace_id: "ws-1",
    daemon_id: null,
    name,
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: null,
    visibility: "private",
    last_seen_at: null,
    created_at: "2026-11-01T00:00:00Z",
    updated_at: "2026-11-01T00:00:00Z",
  };
}

const LIMITS = {
  interval_seconds: 300,
  accounts: [
    {
      id: "claude-max",
      name: "Claude Max",
      provider: "claude",
      runtime_ids: ["rt-1", "rt-2", "rt-gone"],
      rate_limits: {
        plan_type: "max_20x",
        status: "",
        windows: [
          { kind: "weekly", used_percent: 27, window_minutes: 10080, resets_at: "2026-11-21T22:00:00Z", status: "" },
        ],
        observed_at: "2026-11-20T05:55:00Z",
      },
      checked_at: "2026-11-20T06:00:00Z",
      error: "Claude 로그인 토큰이 만료됐습니다.",
      history: [
        {
          kind: "weekly",
          window_minutes: 10080,
          points: [
            { t: "2026-11-19T06:00:00Z", used_percent: 20 },
            { t: "2026-11-20T05:00:00Z", used_percent: 27 },
          ],
        },
      ],
    },
    {
      id: "codex-main",
      name: "Codex",
      provider: "codex",
      runtime_ids: [],
      rate_limits: {
        plan_type: "pro",
        status: "",
        windows: [
          // Reset after the last report: the share belongs to the old window.
          { kind: "weekly", used_percent: 17, window_minutes: 10080, resets_at: "2026-11-18T07:00:00Z", status: "" },
        ],
        credits: { has_credits: true, unlimited: false, balance: "62500.0000000000" },
        observed_at: "2026-11-17T02:25:00Z",
      },
      checked_at: "2026-11-20T06:00:00Z",
      error: null,
      history: [],
    },
  ],
};

let fetchMock: ReturnType<typeof vi.fn>;

function Wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        {children}
      </I18nProvider>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(NOW);
  listRuntimes.mockResolvedValue([runtime("rt-1", "Claude (Mac-Studio)"), runtime("rt-2", "Claude (second daemon)")]);
  fetchMock = vi.fn(async () => new Response(JSON.stringify(LIMITS), { status: 200 }));
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
  listRuntimes.mockReset();
});

describe("SubscriptionLimitsPage", () => {
  it("shows each account's windows with the runtime-page rows", async () => {
    render(<SubscriptionLimitsPage />, { wrapper: Wrapper });

    const claude = await screen.findByRole("region", { name: "Claude Max" });
    expect(within(claude).getByText("Max 20x")).toBeTruthy();
    expect(within(claude).getByRole("meter", { name: "Weekly" }).getAttribute("aria-valuenow")).toBe("27");
    expect(within(claude).getByText("27% used")).toBeTruthy();
    // A failed read keeps the last snapshot and says why it is old.
    expect(within(claude).getByRole("status").textContent).toBe("Claude 로그인 토큰이 만료됐습니다.");
    // Runtimes the list no longer knows are left out.
    expect(within(claude).getByText("런타임 · Claude (Mac-Studio), Claude (second daemon)")).toBeTruthy();

    const codex = screen.getByRole("region", { name: "Codex" });
    expect(within(codex).getByText("Reset since last report")).toBeTruthy();
    expect(within(codex).getByText("잔액 62,500")).toBeTruthy();

    const [url] = fetchMock.mock.calls[0]!;
    expect(url).toBe("/lightsoft-usage/api/w/ws-1/limits?days=7");
  });

  it("explains an empty account list", async () => {
    fetchMock.mockImplementation(async () => new Response(JSON.stringify({ interval_seconds: 300, accounts: [] }), { status: 200 }));

    render(<SubscriptionLimitsPage />, { wrapper: Wrapper });

    expect(await screen.findByText("연결된 구독 계정이 없습니다")).toBeTruthy();
  });

  it("shows the service's refusal", async () => {
    fetchMock.mockImplementation(async () =>
      new Response(JSON.stringify({ error: "이 워크스페이스의 구성원이 아닙니다." }), { status: 403 }),
    );

    render(<SubscriptionLimitsPage />, { wrapper: Wrapper });

    expect(await screen.findByText("이 워크스페이스의 구성원이 아닙니다.")).toBeTruthy();
  });
});
