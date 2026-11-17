"use client";

import { useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { KeyRound } from "@multica/ui/icons";
import { CollectionPageHeader } from "@multica/views/layout";
import { AgentsTab } from "./agents-tab";
import { AuditTab, DEFAULT_AUDIT_VIEW, type AuditView } from "./audit-tab";
import { usePrincipalDirectory } from "./principals";
import { SecretsTab } from "./secrets-tab";

const TABS = ["secrets", "audit", "agents"] as const;
type SecretsPageTab = (typeof TABS)[number];

function parseTab(value: string | null): SecretsPageTab {
  return (TABS as readonly string[]).includes(value ?? "") ? (value as SecretsPageTab) : "secrets";
}

const TAB_TRIGGER_CLASS = "h-full rounded-none px-2.5 group-data-horizontal/tabs:after:bottom-0";

/** Team secrets: stored values, who may read them, and every agent's access history. */
export function SecretsPage() {
  const wsId = useWorkspaceId();
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const tab = parseTab(searchParams.get("tab"));
  const [auditView, setAuditView] = useState<AuditView>(DEFAULT_AUDIT_VIEW);

  const userId = useAuthStore((s) => s.user?.id) ?? "";
  const { data: members = [] } = useQuery({ ...memberListOptions(wsId), enabled: !!wsId });
  const role = members.find((m) => m.user_id === userId)?.role ?? "";
  const directory = usePrincipalDirectory(wsId);

  const setTab = (next: SecretsPageTab) => {
    const params = new URLSearchParams(searchParams.toString());
    if (next === "secrets") params.delete("tab");
    else params.set("tab", next);
    const query = params.toString();
    router.replace(query ? `${pathname}?${query}` : pathname, { scroll: false });
  };

  return (
    <Tabs
      value={tab}
      onValueChange={(next) => setTab(parseTab(String(next)))}
      className="flex h-full min-h-0 flex-col gap-0"
    >
      <CollectionPageHeader
        icon={KeyRound}
        title="시크릿"
        tools={
          <TabsList variant="line" className="shrink-0 gap-0 p-0 group-data-horizontal/tabs:h-full">
            <TabsTrigger value="secrets" className={TAB_TRIGGER_CLASS}>
              시크릿
            </TabsTrigger>
            <TabsTrigger value="audit" className={TAB_TRIGGER_CLASS}>
              에이전트 접근 기록
            </TabsTrigger>
            <TabsTrigger value="agents" className={TAB_TRIGGER_CLASS}>
              에이전트 연결
            </TabsTrigger>
          </TabsList>
        }
      />
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-[1440px] px-[var(--page-padding-x,var(--space-md,1rem))] py-4">
          <TabsContent value="secrets">
            <SecretsTab wsId={wsId} directory={directory} />
          </TabsContent>
          <TabsContent value="audit">
            <AuditTab wsId={wsId} directory={directory} view={auditView} onViewChange={setAuditView} />
          </TabsContent>
          <TabsContent value="agents">
            <AgentsTab wsId={wsId} directory={directory} viewerId={userId} viewerRole={role} />
          </TabsContent>
        </div>
      </div>
    </Tabs>
  );
}
