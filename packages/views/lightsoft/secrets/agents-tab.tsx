"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@multica/ui/components/ui/alert-dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import type { Agent } from "@multica/core/types";
import { enrollmentListOptions, errorMessage, type Enrollment } from "./api";
import { formatAbsolute, formatRelative } from "./format";
import { useEnrollAgent, useUnenrollAgent } from "./mutations";
import { PrincipalLabel, type PrincipalDirectory } from "./principals";

const MANAGER_ROLES = new Set(["owner", "admin"]);

/** Who may connect an agent: its owner, or a workspace owner/admin (the same rule as agent env). */
export function canEnrollAgent(agent: Agent, viewerId: string, viewerRole: string): boolean {
  if (MANAGER_ROLES.has(viewerRole)) return true;
  return !!agent.owner_id && agent.owner_id === viewerId;
}

function EnrollmentActions({
  wsId,
  agent,
  enrollment,
}: {
  wsId: string;
  agent: Agent;
  enrollment: Enrollment | undefined;
}) {
  const enroll = useEnrollAgent(wsId);
  const unenroll = useUnenrollAgent(wsId);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const pending = enroll.isPending || unenroll.isPending;
  const error = enroll.error ?? unenroll.error;

  return (
    <div className="flex flex-col items-end gap-1">
      <div className="flex items-center gap-1">
        <Button
          variant={enrollment ? "outline" : "default"}
          size="xs"
          disabled={pending}
          aria-busy={enroll.isPending}
          onClick={() => {
            unenroll.reset();
            enroll.mutateAsync(agent.id).catch(() => {});
          }}
        >
          {enrollment ? "토큰 재발급" : "연결"}
        </Button>
        {enrollment && (
          <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
            <AlertDialogTrigger render={<Button variant="ghost" size="xs" disabled={pending} />}>
              연결 해제
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>{agent.name} 연결을 해제할까요?</AlertDialogTitle>
                <AlertDialogDescription>
                  이 에이전트의 토큰이 바로 무효가 되어 시크릿을 더 꺼낼 수 없습니다.
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>취소</AlertDialogCancel>
                <AlertDialogAction
                  variant="destructive"
                  disabled={unenroll.isPending}
                  aria-busy={unenroll.isPending}
                  onClick={() => {
                    enroll.reset();
                    unenroll.mutateAsync(agent.id).then(
                      () => setConfirmOpen(false),
                      () => setConfirmOpen(false),
                    );
                  }}
                >
                  연결 해제
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        )}
      </div>
      {error && (
        <span role="alert" className="text-caption text-destructive">
          {errorMessage(error)}
        </span>
      )}
    </div>
  );
}

export function AgentsTab({
  wsId,
  directory,
  viewerId,
  viewerRole,
}: {
  wsId: string;
  directory: PrincipalDirectory;
  viewerId: string;
  viewerRole: string;
}) {
  const { data: enrollments = [], isLoading, error } = useQuery(enrollmentListOptions(wsId));
  const byAgent = new Map(enrollments.map((e) => [e.agent_id, e]));
  const agents = directory.agentRecords.filter((a) => !a.archived_at);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1 rounded-lg border bg-muted/40 px-4 py-3 text-body">
        <span>
          연결한 에이전트는 다음 실행부터 읽기 권한을 받은 시크릿을 꺼낼 수 있습니다.
        </span>
        <code className="font-mono text-caption text-muted-foreground">lightsoft-secrets get NAME</code>
        <code className="font-mono text-caption text-muted-foreground">
          lightsoft-secrets exec -- &lt;명령&gt;
        </code>
      </div>

      {error ? (
        <p role="alert" className="text-body text-destructive">
          {errorMessage(error)}
        </p>
      ) : isLoading ? (
        <p className="text-body text-muted-foreground">불러오는 중…</p>
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>에이전트</TableHead>
                <TableHead>상태</TableHead>
                <TableHead>마지막 사용</TableHead>
                <TableHead className="w-0">
                  <span className="sr-only">관리</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {agents.map((agent) => {
                const enrollment = byAgent.get(agent.id);
                const allowed = canEnrollAgent(agent, viewerId, viewerRole);
                return (
                  <TableRow key={agent.id} data-testid={`agent-row-${agent.id}`}>
                    <TableCell>
                      <PrincipalLabel principal={directory.lookup("agent", agent.id)} className="max-w-64" />
                    </TableCell>
                    <TableCell>
                      {enrollment ? (
                        <span title={formatAbsolute(enrollment.enrolled_at)}>
                          연결됨 · {formatRelative(enrollment.enrolled_at)}
                        </span>
                      ) : (
                        <span className="text-muted-foreground">미연결</span>
                      )}
                    </TableCell>
                    <TableCell title={formatAbsolute(enrollment?.last_used_at)}>
                      {enrollment?.last_used_at ? formatRelative(enrollment.last_used_at) : "—"}
                    </TableCell>
                    <TableCell>
                      {allowed ? (
                        <EnrollmentActions wsId={wsId} agent={agent} enrollment={enrollment} />
                      ) : null}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}
