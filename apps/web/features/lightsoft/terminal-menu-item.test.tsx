import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { DropdownMenu, DropdownMenuContent } from "@multica/ui/components/ui/dropdown-menu";

const WS_ID = "11111111-1111-1111-1111-111111111111";
const PROJECT_ID = "22222222-2222-2222-2222-222222222222";

const authState = { user: { id: "u-1" } };
const listMembers = vi.fn();

vi.mock("@multica/core/auth", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/auth")>("@multica/core/auth");
  const useAuthStore = Object.assign(
    (selector: (s: typeof authState) => unknown) => selector(authState),
    { getState: () => authState },
  );
  return { ...actual, useAuthStore };
});

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => WS_ID }));

vi.mock("@multica/core/api", () => ({ api: { listMembers: (...a: unknown[]) => listMembers(...a) } }));

import { TerminalMenuItem, terminalUrl } from "./terminal-menu-item";

function renderItem() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <DropdownMenu open>
        <DropdownMenuContent>
          <TerminalMenuItem projectId={PROJECT_ID} />
          <span>menu-ready</span>
        </DropdownMenuContent>
      </DropdownMenu>
    </QueryClientProvider>,
  );
}

describe("TerminalMenuItem", () => {
  beforeEach(() => {
    listMembers.mockReset();
  });

  it("opens the project's terminal in a new tab for workspace admins", async () => {
    listMembers.mockResolvedValue([{ user_id: "u-1", role: "admin" }]);
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    renderItem();

    await userEvent.click(await screen.findByText("터미널 열기"));

    expect(open).toHaveBeenCalledWith(terminalUrl(WS_ID, PROJECT_ID), "_blank", "noopener");
    expect(terminalUrl(WS_ID, PROJECT_ID)).toBe(`/lightsoft-term/p/${PROJECT_ID}?ws=${WS_ID}`);
  });

  it("is hidden from plain members", async () => {
    listMembers.mockResolvedValue([{ user_id: "u-1", role: "member" }]);
    renderItem();

    await screen.findByText("menu-ready");
    await waitFor(() => expect(listMembers).toHaveBeenCalled());
    expect(screen.queryByText("터미널 열기")).toBeNull();
  });
});
