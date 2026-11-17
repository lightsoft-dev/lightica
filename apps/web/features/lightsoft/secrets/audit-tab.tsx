"use client";

import { useMemo } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { cn } from "@multica/ui/lib/utils";
import {
  agentSummaryOptions,
  errorMessage,
  secretListOptions,
  secretsApi,
  secretsKeys,
  type AuditEvent,
  type AuditFilter,
} from "./api";
import { ACTION_LABELS, actionLabel, formatAbsolute, formatRelative, resultLabel } from "./format";
import { PrincipalLabel, type PrincipalDirectory } from "./principals";

const ALL = "all";

export interface AuditView {
  actorType: "agent" | "member" | typeof ALL;
  actorId: string;
  secretId: string;
  action: string;
}

export const DEFAULT_AUDIT_VIEW: AuditView = {
  actorType: "agent",
  actorId: ALL,
  secretId: ALL,
  action: ALL,
};

function auditFilterFor(view: AuditView): AuditFilter {
  const filter: AuditFilter = {};
  if (view.actorType !== ALL) filter.actor_type = view.actorType;
  if (view.actorId !== ALL) filter.actor_id = view.actorId;
  if (view.secretId !== ALL) filter.secret_id = view.secretId;
  if (view.action !== ALL) filter.action = view.action;
  return filter;
}

function FilterSelect({
  label,
  value,
  items,
  onChange,
  className,
}: {
  label: string;
  value: string;
  items: Array<{ value: string; label: string }>;
  onChange: (value: string) => void;
  className?: string;
}) {
  return (
    <Select items={items} value={value} onValueChange={(next) => next && onChange(String(next))}>
      <SelectTrigger size="sm" className={cn("w-40", className)} aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent align="start">
        {items.map((item) => (
          <SelectItem key={item.value} value={item.value}>
            {item.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function grantCount(detail: Record<string, unknown>, key: string): number {
  const v = detail[key];
  return Array.isArray(v) ? v.length : 0;
}

function eventDetail(event: AuditEvent): string {
  const d = event.detail;
  if (event.result !== "ok" && typeof d.reason === "string") return d.reason;
  switch (event.action) {
    case "secret.update": {
      const parts: string[] = [];
      if (d.value_changed === true) parts.push("값 변경");
      if (d.description_changed === true) parts.push("설명 변경");
      return parts.join(", ");
    }
    case "secret.grants": {
      const added = grantCount(d, "added");
      const removed = grantCount(d, "removed");
      return [added && `${added}명 추가`, removed && `${removed}명 제거`].filter(Boolean).join(", ");
    }
    case "agent.enroll":
    case "agent.unenroll":
      return typeof d.agent_name === "string" ? d.agent_name : "";
    default:
      return "";
  }
}

function AgentSummaryTable({
  wsId,
  directory,
  selectedAgentId,
  onSelect,
}: {
  wsId: string;
  directory: PrincipalDirectory;
  selectedAgentId: string;
  onSelect: (agentId: string) => void;
}) {
  const { data: summaries = [], isLoading, error } = useQuery(agentSummaryOptions(wsId));

  if (error) {
    return (
      <p role="alert" className="text-body text-destructive">
        {errorMessage(error)}
      </p>
    );
  }
  if (isLoading) return <p className="text-body text-muted-foreground">불러오는 중…</p>;
  if (summaries.length === 0) {
    return <p className="text-body text-muted-foreground">아직 에이전트가 시크릿을 조회한 기록이 없습니다.</p>;
  }

  return (
    <div className="overflow-x-auto rounded-lg border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>에이전트</TableHead>
            <TableHead className="text-right">조회</TableHead>
            <TableHead className="text-right">거부</TableHead>
            <TableHead>마지막 조회</TableHead>
            <TableHead>조회한 시크릿</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {summaries.map((s) => {
            const selected = s.agent_id === selectedAgentId;
            return (
              <TableRow key={s.agent_id} data-state={selected ? "selected" : undefined}>
                <TableCell>
                  <button
                    type="button"
                    className="flex min-w-0 items-center text-left hover:underline"
                    aria-pressed={selected}
                    title={selected ? "필터 해제" : "이 에이전트의 기록만 보기"}
                    onClick={() => onSelect(selected ? ALL : s.agent_id)}
                  >
                    <PrincipalLabel principal={directory.lookup("agent", s.agent_id)} className="max-w-56" />
                  </button>
                </TableCell>
                <TableCell className="text-right tabular-nums">{s.read_count}</TableCell>
                <TableCell className={cn("text-right tabular-nums", s.denied_count > 0 && "text-destructive")}>
                  {s.denied_count}
                </TableCell>
                <TableCell title={formatAbsolute(s.last_read_at)}>
                  {s.last_read_at ? formatRelative(s.last_read_at) : "—"}
                </TableCell>
                <TableCell>
                  <span className="flex flex-wrap gap-1">
                    {s.secret_names.map((name) => (
                      <code key={name} className="rounded-sm bg-muted px-1 font-mono text-caption">
                        {name}
                      </code>
                    ))}
                  </span>
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}

export function AuditTab({
  wsId,
  directory,
  view,
  onViewChange,
}: {
  wsId: string;
  directory: PrincipalDirectory;
  view: AuditView;
  onViewChange: (view: AuditView) => void;
}) {
  const filter = useMemo(() => auditFilterFor(view), [view]);
  const { data: secretList } = useQuery(secretListOptions(wsId));
  const events = useInfiniteQuery({
    queryKey: secretsKeys.audit(wsId, filter),
    queryFn: ({ pageParam }) => secretsApi.audit(wsId, filter, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_before ?? undefined,
    enabled: !!wsId,
  });
  const rows = events.data?.pages.flatMap((p) => p.events) ?? [];

  const actorItems =
    view.actorType === "member"
      ? directory.members.map((m) => ({ value: m.id, label: m.name }))
      : directory.agents.map((a) => ({ value: a.id, label: a.name }));
  const secretItems = [
    { value: ALL, label: "모든 시크릿" },
    ...(secretList?.secrets ?? []).map((s) => ({ value: s.id, label: s.name })),
  ];
  const actionItems = [
    { value: ALL, label: "모든 동작" },
    ...Object.entries(ACTION_LABELS).map(([value, label]) => ({ value, label })),
  ];

  return (
    <div className="flex flex-col gap-6">
      <section className="flex flex-col gap-2" aria-label="에이전트별 조회">
        <h2 className="text-body font-medium">에이전트별 조회</h2>
        <AgentSummaryTable
          wsId={wsId}
          directory={directory}
          selectedAgentId={view.actorType === "agent" ? view.actorId : ALL}
          onSelect={(agentId) => onViewChange({ ...view, actorType: "agent", actorId: agentId })}
        />
      </section>

      <section className="flex flex-col gap-2" aria-label="접근 기록">
        <div className="flex flex-wrap items-center gap-2">
          <h2 className="mr-auto text-body font-medium">접근 기록</h2>
          <FilterSelect
            label="행위자 종류"
            value={view.actorType}
            className="w-28"
            items={[
              { value: "agent", label: "에이전트" },
              { value: "member", label: "사람" },
              { value: ALL, label: "전체" },
            ]}
            onChange={(next) =>
              onViewChange({ ...view, actorType: next as AuditView["actorType"], actorId: ALL })
            }
          />
          {view.actorType !== ALL && (
            <FilterSelect
              label={view.actorType === "member" ? "구성원" : "에이전트"}
              value={view.actorId}
              items={[
                { value: ALL, label: view.actorType === "member" ? "모든 구성원" : "모든 에이전트" },
                ...actorItems,
              ]}
              onChange={(next) => onViewChange({ ...view, actorId: next })}
            />
          )}
          <FilterSelect
            label="시크릿"
            value={view.secretId}
            items={secretItems}
            onChange={(next) => onViewChange({ ...view, secretId: next })}
          />
          <FilterSelect
            label="동작"
            value={view.action}
            className="w-32"
            items={actionItems}
            onChange={(next) => onViewChange({ ...view, action: next })}
          />
        </div>

        {events.error ? (
          <p role="alert" className="text-body text-destructive">
            {errorMessage(events.error)}
          </p>
        ) : events.isLoading ? (
          <p className="text-body text-muted-foreground">불러오는 중…</p>
        ) : rows.length === 0 ? (
          <p className="text-body text-muted-foreground">조건에 맞는 기록이 없습니다.</p>
        ) : (
          <div className="overflow-x-auto rounded-lg border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>시각</TableHead>
                  <TableHead>행위자</TableHead>
                  <TableHead>시크릿</TableHead>
                  <TableHead>동작</TableHead>
                  <TableHead>결과</TableHead>
                  <TableHead>내용</TableHead>
                  <TableHead>태스크</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((event) => (
                  <TableRow key={event.id} data-testid="audit-row">
                    <TableCell className="whitespace-nowrap" title={formatAbsolute(event.created_at)}>
                      {formatRelative(event.created_at)}
                    </TableCell>
                    <TableCell>
                      <PrincipalLabel
                        principal={directory.lookup(event.actor_type, event.actor_id)}
                        className="max-w-48"
                      />
                    </TableCell>
                    <TableCell>
                      {event.secret_name ? (
                        <code className="font-mono text-caption">{event.secret_name}</code>
                      ) : (
                        "—"
                      )}
                    </TableCell>
                    <TableCell className="whitespace-nowrap">{actionLabel(event.action)}</TableCell>
                    <TableCell
                      className={cn("whitespace-nowrap", event.result !== "ok" && "text-destructive")}
                    >
                      {resultLabel(event.result)}
                    </TableCell>
                    <TableCell className="max-w-64 truncate text-muted-foreground">
                      {eventDetail(event)}
                    </TableCell>
                    <TableCell>
                      {event.task_id ? (
                        <code className="font-mono text-caption text-muted-foreground" title={event.task_id}>
                          {event.task_id.slice(0, 8)}
                        </code>
                      ) : (
                        "—"
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
        {events.hasNextPage && (
          <Button
            variant="outline"
            size="sm"
            className="self-center"
            disabled={events.isFetchingNextPage}
            aria-busy={events.isFetchingNextPage}
            onClick={() => void events.fetchNextPage()}
          >
            더 보기
          </Button>
        )}
      </section>
    </div>
  );
}
