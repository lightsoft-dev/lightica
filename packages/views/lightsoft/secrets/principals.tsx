"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { cn } from "@multica/ui/lib/utils";
import { agentListOptions, memberListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import type { Agent } from "@multica/core/types";
import type { PrincipalType } from "@multica/core/lightsoft/secrets";

export interface Principal {
  type: PrincipalType;
  id: string;
  name: string;
  avatarUrl: string | null;
  /** Archived agents and squads stay resolvable for history but are not offered for new grants. */
  archived: boolean;
}

export interface PrincipalDirectory {
  members: Principal[];
  agents: Principal[];
  squads: Principal[];
  agentRecords: Agent[];
  lookup: (type: string, id: string) => Principal;
}

const EMPTY: never[] = [];

/** Names and avatars for the members, agents and squads a secret can be granted to. */
export function usePrincipalDirectory(wsId: string): PrincipalDirectory {
  const { data: members = EMPTY } = useQuery({ ...memberListOptions(wsId), enabled: !!wsId });
  const { data: agents = EMPTY } = useQuery({ ...agentListOptions(wsId), enabled: !!wsId });
  const { data: squads = EMPTY } = useQuery(squadListOptions(wsId));

  return useMemo(() => {
    const memberPrincipals: Principal[] = members.map((m) => ({
      type: "member",
      id: m.user_id,
      name: m.name || m.email,
      avatarUrl: m.avatar_url,
      archived: false,
    }));
    const agentPrincipals: Principal[] = agents.map((a) => ({
      type: "agent",
      id: a.id,
      name: a.name,
      avatarUrl: a.avatar_url,
      archived: !!a.archived_at,
    }));
    const squadPrincipals: Principal[] = squads.map((s) => ({
      type: "squad",
      id: s.id,
      name: s.name,
      avatarUrl: s.avatar_url,
      archived: !!s.archived_at,
    }));
    const index = new Map<string, Principal>();
    for (const p of [...memberPrincipals, ...agentPrincipals, ...squadPrincipals]) {
      index.set(`${p.type}:${p.id}`, p);
    }
    return {
      members: memberPrincipals,
      agents: agentPrincipals,
      squads: squadPrincipals,
      agentRecords: agents,
      lookup: (type, id) =>
        index.get(`${type}:${id}`) ?? {
          type: type === "agent" || type === "squad" ? type : "member",
          id,
          name: `알 수 없음 (${id.slice(0, 8)})`,
          avatarUrl: null,
          archived: true,
        },
    };
  }, [members, agents, squads]);
}

export function PrincipalLabel({
  principal,
  className,
}: {
  principal: Principal;
  className?: string;
}) {
  return (
    <span className={cn("inline-flex min-w-0 items-center gap-1.5", className)}>
      <ActorAvatar
        name={principal.name}
        initials={principal.name.charAt(0).toUpperCase()}
        avatarUrl={resolvePublicFileUrl(principal.avatarUrl)}
        isAgent={principal.type === "agent"}
        isSquad={principal.type === "squad"}
        size="xs"
      />
      <span className="min-w-0 truncate">{principal.name}</span>
    </span>
  );
}
