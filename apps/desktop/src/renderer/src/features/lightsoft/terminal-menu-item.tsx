import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { DropdownMenuItem } from "@multica/ui/components/ui/dropdown-menu";
import { SquareTerminal } from "@multica/ui/icons";
import { useNavigation } from "@multica/views/navigation";

// Desktop twin of apps/web/features/lightsoft/terminal-menu-item.tsx. The
// gateway (lightsoft/terminal) lives on the team client origin and signs the
// user in with that origin's browser cookie, so Desktop opens it in the
// default browser: window.open is routed to openExternalSafely by main.
const TERMINAL_ROLES = new Set(["owner", "admin"]);

export function terminalPath(workspaceId: string, projectId: string): string {
  return `/lightsoft-term/p/${encodeURIComponent(projectId)}?ws=${encodeURIComponent(workspaceId)}`;
}

/** Project menu entry that opens a temporary shell in the project's folder. */
export function TerminalMenuItem({ projectId }: { projectId: string }) {
  const wsId = useWorkspaceId();
  const { getShareableUrl } = useNavigation();
  const userId = useAuthStore((s) => s.user?.id);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const role = members.find((m) => m.user_id === userId)?.role;
  if (!role || !TERMINAL_ROLES.has(role)) return null;

  return (
    <DropdownMenuItem
      onClick={() =>
        window.open(getShareableUrl(terminalPath(wsId, projectId)), "_blank", "noopener")
      }
    >
      <SquareTerminal className="h-3.5 w-3.5" />
      터미널 열기
    </DropdownMenuItem>
  );
}
