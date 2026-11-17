import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent } from "@multica/core/types";

vi.mock("@multica/core/api", () => ({ api: { getBaseUrl: () => "" } }));

import { canEnrollAgent } from "./agents-tab";
import type { SecretMeta } from "@multica/core/lightsoft/secrets";
import type { Principal, PrincipalDirectory } from "./principals";
import { SecretsTab } from "./secrets-tab";

const WS = "11111111-1111-1111-1111-111111111111";
const SECRET_VALUE = "ghp_super_secret_value";

function secret(overrides: Partial<SecretMeta>): SecretMeta {
  return {
    id: "sec-1",
    name: "GITHUB_TOKEN",
    description: "",
    owner_id: "u-1",
    created_at: "2026-11-17T00:00:00Z",
    updated_at: "2026-11-17T00:00:00Z",
    updated_by: "u-1",
    value_updated_at: "2026-11-17T00:00:00Z",
    version: 1,
    grants: [],
    last_read_at: null,
    last_read_actor_type: null,
    last_read_actor_id: null,
    can_read: false,
    can_manage: false,
    ...overrides,
  };
}

const directory: PrincipalDirectory = {
  members: [],
  agents: [],
  squads: [],
  agentRecords: [],
  lookup: (type, id): Principal => ({
    type: type === "agent" || type === "squad" ? type : "member",
    id,
    name: `name-${id}`,
    avatarUrl: null,
    archived: false,
  }),
};

let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn(async (url: string, init: RequestInit) => {
    if (url.endsWith("/secrets") && init.method === "GET") {
      return new Response(
        JSON.stringify({
          viewer: { user_id: "u-1", role: "member" },
          secrets: [
            secret({ id: "sec-1", name: "MINE", can_read: true, can_manage: true }),
            secret({ id: "sec-2", name: "THEIRS", owner_id: "u-2" }),
          ],
        }),
        { status: 200 },
      );
    }
    if (url.endsWith("/secrets/sec-1/reveal")) {
      return new Response(JSON.stringify({ value: SECRET_VALUE }), { status: 200 });
    }
    return new Response(JSON.stringify({ error: "unexpected" }), { status: 500 });
  });
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function renderTab() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <SecretsTab wsId={WS} directory={directory} />
    </QueryClientProvider>,
  );
  return client;
}

describe("SecretsTab", () => {
  it("offers reveal and management only where the service allows it", async () => {
    renderTab();

    const mine = await screen.findByTestId("secret-row-MINE");
    const theirs = screen.getByTestId("secret-row-THEIRS");

    expect(within(mine).getByRole("button", { name: /값 보기/ })).toBeInTheDocument();
    expect(within(mine).getByRole("button", { name: "MINE 수정" })).toBeInTheDocument();
    expect(within(mine).getByRole("button", { name: "MINE 삭제" })).toBeInTheDocument();

    expect(within(theirs).queryByRole("button", { name: /값 보기/ })).toBeNull();
    expect(within(theirs).queryByRole("button", { name: "THEIRS 수정" })).toBeNull();
    expect(within(theirs).getByText("권한 없음")).toBeInTheDocument();
  });

  it("reveals into local state only and forgets the value on hide", async () => {
    const client = renderTab();
    const mine = await screen.findByTestId("secret-row-MINE");

    await userEvent.click(within(mine).getByRole("button", { name: /값 보기/ }));

    expect(await within(mine).findByTestId("revealed-value")).toHaveTextContent(SECRET_VALUE);
    const revealCall = fetchMock.mock.calls.find(([url]) => String(url).endsWith("/reveal"))!;
    expect((revealCall[1] as RequestInit).method).toBe("POST");
    expect(((revealCall[1] as RequestInit).headers as Record<string, string>)["X-Lightsoft-Secrets"]).toBe("1");

    const cached = JSON.stringify([
      client.getQueryCache().getAll().map((q) => q.state.data),
      client.getMutationCache().getAll().map((m) => m.state.data),
    ]);
    expect(cached).not.toContain(SECRET_VALUE);

    await userEvent.click(within(mine).getByRole("button", { name: "값 가리기" }));
    expect(within(mine).queryByTestId("revealed-value")).toBeNull();
    expect(within(mine).getByRole("button", { name: /값 보기/ })).toBeInTheDocument();
  });
});

describe("canEnrollAgent", () => {
  const agent = { id: "a-1", owner_id: "u-1" } as Agent;

  it("allows the agent owner and workspace owner/admin", () => {
    expect(canEnrollAgent(agent, "u-1", "member")).toBe(true);
    expect(canEnrollAgent(agent, "u-2", "admin")).toBe(true);
    expect(canEnrollAgent(agent, "u-2", "owner")).toBe(true);
  });

  it("refuses other members and ownerless agents", () => {
    expect(canEnrollAgent(agent, "u-2", "member")).toBe(false);
    expect(canEnrollAgent({ ...agent, owner_id: null } as Agent, "", "member")).toBe(false);
  });
});
