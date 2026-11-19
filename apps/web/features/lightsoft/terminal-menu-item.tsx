"use client";

import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { DropdownMenuItem } from "@multica/ui/components/ui/dropdown-menu";
import { SquareTerminal } from "@multica/ui/icons";

// Served by the Lightsoft terminal gateway (lightsoft/terminal), which
// Tailscale serve mounts on the team client origin. Local web dev points this
// at the gateway's own port instead.
const TERMINAL_BASE =
  process.env.NEXT_PUBLIC_LIGHTSOFT_TERMINAL_URL ?? "/lightsoft-term";

const TERMINAL_ROLES = new Set(["owner", "admin"]);

export function terminalUrl(workspaceId: string, projectId: string): string {
  return `${TERMINAL_BASE}/p/${encodeURIComponent(projectId)}?ws=${encodeURIComponent(workspaceId)}`;
}

/** Project menu entry that opens a temporary shell in the project's folder. */
export function TerminalMenuItem({ projectId }: { projectId: string }) {
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const role = members.find((m) => m.user_id === userId)?.role;
  if (!role || !TERMINAL_ROLES.has(role)) return null;

  return (
    <DropdownMenuItem
      onClick={() => window.open(terminalUrl(wsId, projectId), "_blank", "noopener")}
    >
      <SquareTerminal className="h-3.5 w-3.5" />
      터미널 열기
    </DropdownMenuItem>
  );
}
