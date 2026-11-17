"use client";
/* eslint-disable i18next/no-literal-string -- Lightsoft fork page (LIGH-112): the team UI is Korean-only, so copy is inline rather than in the upstream locale files that daily release merges touch. */

import { useId, useState } from "react";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import type { Grant, PrincipalType } from "@multica/core/lightsoft/secrets";
import { PrincipalLabel, type Principal, type PrincipalDirectory } from "./principals";

const GROUPS: Array<{ type: PrincipalType; label: string; key: "members" | "agents" | "squads" }> = [
  { type: "member", label: "구성원", key: "members" },
  { type: "agent", label: "에이전트", key: "agents" },
  { type: "squad", label: "스쿼드", key: "squads" },
];

export function grantKey(g: Grant): string {
  return `${g.principal_type}:${g.principal_id}`;
}

/** Searchable checkbox list of the members, agents and squads that may read a secret. */
export function GrantsPicker({
  directory,
  value,
  onChange,
  ownerId,
}: {
  directory: PrincipalDirectory;
  value: Grant[];
  onChange: (next: Grant[]) => void;
  /** The owner always reads; shown checked and locked. */
  ownerId?: string;
}) {
  const [query, setQuery] = useState("");
  const searchId = useId();
  const selected = new Set(value.map(grantKey));
  const needle = query.trim().toLowerCase();

  const toggle = (p: Principal, checked: boolean) => {
    const key = `${p.type}:${p.id}`;
    if (checked) {
      if (!selected.has(key)) onChange([...value, { principal_type: p.type, principal_id: p.id }]);
    } else {
      onChange(value.filter((g) => grantKey(g) !== key));
    }
  };

  return (
    <div className="flex flex-col gap-2">
      <label htmlFor={searchId} className="sr-only">
        읽기 권한 검색
      </label>
      <Input
        id={searchId}
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="이름으로 찾기"
      />
      <div className="max-h-64 overflow-y-auto rounded-md border">
        {GROUPS.map((group) => {
          const rows = directory[group.key].filter((p) => {
            const key = `${p.type}:${p.id}`;
            if (p.archived && !selected.has(key)) return false;
            return !needle || p.name.toLowerCase().includes(needle);
          });
          if (rows.length === 0) return null;
          return (
            <div key={group.type} role="group" aria-label={group.label}>
              <div className="sticky top-0 bg-muted px-3 py-1 text-caption font-medium text-muted-foreground">
                {group.label}
              </div>
              {rows.map((p) => {
                const key = `${p.type}:${p.id}`;
                const isOwner = p.type === "member" && p.id === ownerId;
                return (
                  <label
                    key={key}
                    className="flex cursor-pointer items-center gap-2 px-3 py-1.5 text-body hover:bg-accent"
                  >
                    <Checkbox
                      checked={isOwner || selected.has(key)}
                      disabled={isOwner}
                      onCheckedChange={(checked) => toggle(p, checked === true)}
                      aria-label={p.name}
                    />
                    <PrincipalLabel principal={p} />
                    {isOwner && <span className="ml-auto text-caption text-muted-foreground">소유자</span>}
                  </label>
                );
              })}
            </div>
          );
        })}
      </div>
    </div>
  );
}
