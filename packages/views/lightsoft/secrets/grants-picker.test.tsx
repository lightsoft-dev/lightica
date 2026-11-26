import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { MemberWithUser } from "@multica/core/types";

vi.mock("@multica/core/api", () => ({ api: { getBaseUrl: () => "" } }));

import { GrantsPicker } from "./grants-picker";
import { memberPrincipal, type PrincipalDirectory } from "./principals";

function member(userId: string, name: string, role: MemberWithUser["role"]): MemberWithUser {
  return {
    id: `m-${userId}`,
    workspace_id: "ws-1",
    user_id: userId,
    role,
    created_at: "2026-11-26T00:00:00Z",
    name,
    email: `${userId}@example.com`,
    avatar_url: null,
  };
}

function directoryOf(members: MemberWithUser[]): PrincipalDirectory {
  const principals = members.map(memberPrincipal);
  return {
    members: principals,
    agents: [],
    squads: [],
    agentRecords: [],
    lookup: (_type, id) => principals.find((p) => p.id === id)!,
  };
}

describe("GrantsPicker", () => {
  // The secrets service refuses guests (LIGH-282), so they are not offered.
  it("does not offer a guest for a new grant", () => {
    const directory = directoryOf([member("u-1", "Teammate", "member"), member("u-2", "Outside", "guest")]);
    render(<GrantsPicker directory={directory} value={[]} onChange={() => {}} />);
    expect(screen.getByText("Teammate")).toBeTruthy();
    expect(screen.queryByText("Outside")).toBeNull();
  });

  it("still shows a guest an existing grant names, so it can be removed", () => {
    const directory = directoryOf([member("u-2", "Outside", "guest")]);
    render(
      <GrantsPicker
        directory={directory}
        value={[{ principal_type: "member", principal_id: "u-2" }]}
        onChange={() => {}}
      />,
    );
    expect(screen.getByText("Outside")).toBeTruthy();
  });
});
