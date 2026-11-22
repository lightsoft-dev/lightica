import type { ReactNode } from "react";
import type { IssueSidebarSection } from "@multica/core/issues/stores";

// Lightsoft (LIGH-282): issue sidebar sections about runs, wakeups, PRs and
// apps. A guest cannot act on any of them (the server refuses), so the team
// client leaves them out instead of offering a "+" that fails.
const GUEST_HIDDEN: readonly IssueSidebarSection[] = [
  "quick_actions",
  "wakeups",
  "plugins",
  "pull_requests",
  "environments",
  "apps",
  "execution_log",
];

export function withoutLightsoftGuestSections(
  sections: Record<IssueSidebarSection, ReactNode>,
): Record<IssueSidebarSection, ReactNode> {
  const out = { ...sections };
  for (const id of GUEST_HIDDEN) out[id] = null;
  return out;
}
