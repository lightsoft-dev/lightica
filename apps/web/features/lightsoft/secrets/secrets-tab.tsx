"use client";

import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
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
import { Check, Copy, Eye, EyeOff, KeyRound, Pencil, Plus, Trash2 } from "@multica/ui/icons";
import {
  errorMessage,
  secretListOptions,
  secretsApi,
  secretsKeys,
  type SecretMeta,
} from "./api";
import { formatAbsolute, formatRelative } from "./format";
import { useDeleteSecret } from "./mutations";
import { PrincipalLabel, type PrincipalDirectory } from "./principals";
import { CreateSecretDialog, EditSecretDialog } from "./secret-form-dialog";

/** How long a revealed value stays on screen before it is hidden again. */
const REVEAL_TTL_MS = 60_000;

/**
 * Reveals one secret's value. The value lives only in this component's state —
 * never in the query cache — and is dropped on hide, timeout or unmount.
 */
export function RevealValue({ wsId, secret }: { wsId: string; secret: SecretMeta }) {
  const qc = useQueryClient();
  const [value, setValue] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const reveal = useMutation({
    mutationFn: () => secretsApi.reveal(wsId, secret.id),
    gcTime: 0,
    onSettled: () => qc.invalidateQueries({ queryKey: secretsKeys.all(wsId) }),
  });

  useEffect(() => {
    if (value === null) return;
    const timer = setTimeout(() => setValue(null), REVEAL_TTL_MS);
    return () => clearTimeout(timer);
  }, [value]);

  const show = () => {
    reveal.mutateAsync().then(
      (v) => {
        setValue(v);
        // Drop the value from the mutation state; only local state keeps it.
        reveal.reset();
      },
      () => {
        // reveal.error renders below.
      },
    );
  };

  const copy = () => {
    if (value === null) return;
    void navigator.clipboard?.writeText(value).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  };

  if (value === null) {
    return (
      <div className="flex flex-col items-start gap-1">
        <Button
          variant="outline"
          size="xs"
          onClick={show}
          disabled={reveal.isPending}
          aria-busy={reveal.isPending}
        >
          <Eye />
          값 보기
        </Button>
        {reveal.error && (
          <span role="alert" className="text-caption text-destructive">
            {errorMessage(reveal.error)}
          </span>
        )}
      </div>
    );
  }

  return (
    <div className="flex min-w-0 items-center gap-1">
      <code
        data-testid="revealed-value"
        className="min-w-0 max-w-72 truncate rounded-sm bg-muted px-1.5 py-0.5 font-mono text-caption"
        title={value}
      >
        {value}
      </code>
      <Button variant="ghost" size="icon-xs" aria-label="값 복사" onClick={copy}>
        {copied ? <Check /> : <Copy />}
      </Button>
      <Button variant="ghost" size="icon-xs" aria-label="값 가리기" onClick={() => setValue(null)}>
        <EyeOff />
      </Button>
    </div>
  );
}

function DeleteSecretButton({ wsId, secret }: { wsId: string; secret: SecretMeta }) {
  const [open, setOpen] = useState(false);
  const del = useDeleteSecret(wsId);
  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) del.reset();
      }}
    >
      <AlertDialogTrigger
        render={<Button variant="ghost" size="icon-xs" aria-label={`${secret.name} 삭제`} />}
      >
        <Trash2 />
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{secret.name}을(를) 삭제할까요?</AlertDialogTitle>
          <AlertDialogDescription>
            값은 복구할 수 없습니다. 이 시크릿을 쓰는 에이전트는 다음 조회부터 실패합니다.
          </AlertDialogDescription>
        </AlertDialogHeader>
        {del.error && (
          <p role="alert" className="text-body text-destructive">
            {errorMessage(del.error)}
          </p>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel>취소</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={del.isPending}
            aria-busy={del.isPending}
            onClick={() => {
              del.mutateAsync(secret.id).then(() => setOpen(false), () => {});
            }}
          >
            삭제
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function Readers({ secret, directory }: { secret: SecretMeta; directory: PrincipalDirectory }) {
  const owner = directory.lookup("member", secret.owner_id);
  const others = secret.grants.filter(
    (g) => !(g.principal_type === "member" && g.principal_id === secret.owner_id),
  );
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
      <PrincipalLabel principal={owner} className="max-w-40" />
      {others.map((g) => (
        <PrincipalLabel
          key={`${g.principal_type}:${g.principal_id}`}
          principal={directory.lookup(g.principal_type, g.principal_id)}
          className="max-w-40"
        />
      ))}
    </div>
  );
}

function LastRead({ secret, directory }: { secret: SecretMeta; directory: PrincipalDirectory }) {
  if (!secret.last_read_at) return <span className="text-muted-foreground">—</span>;
  const actor =
    secret.last_read_actor_type && secret.last_read_actor_id
      ? directory.lookup(secret.last_read_actor_type, secret.last_read_actor_id)
      : null;
  return (
    <div className="flex flex-col gap-0.5">
      <span title={formatAbsolute(secret.last_read_at)}>{formatRelative(secret.last_read_at)}</span>
      {actor && <PrincipalLabel principal={actor} className="max-w-40 text-caption text-muted-foreground" />}
    </div>
  );
}

export function SecretsTab({
  wsId,
  directory,
}: {
  wsId: string;
  directory: PrincipalDirectory;
}) {
  const { data, isLoading, error } = useQuery(secretListOptions(wsId));
  const viewerId = data?.viewer?.user_id ?? "";
  const secrets = data?.secrets ?? [];

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-2">
        <span className="text-body text-muted-foreground">
          {secrets.length > 0 ? `시크릿 ${secrets.length}개` : ""}
        </span>
        <CreateSecretDialog
          wsId={wsId}
          viewerId={viewerId}
          directory={directory}
          trigger={
            <Button size="sm" disabled={!data}>
              <Plus />
              새 시크릿
            </Button>
          }
        />
      </div>

      {error ? (
        <p role="alert" className="text-body text-destructive">
          {errorMessage(error)}
        </p>
      ) : isLoading ? (
        <p className="text-body text-muted-foreground">불러오는 중…</p>
      ) : secrets.length === 0 ? (
        <div className="flex flex-col items-center gap-2 rounded-lg border border-dashed py-12 text-muted-foreground">
          <KeyRound className="size-6" />
          <span className="text-body">아직 시크릿이 없습니다.</span>
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>이름</TableHead>
                <TableHead>읽기 권한</TableHead>
                <TableHead>마지막 조회</TableHead>
                <TableHead>값</TableHead>
                <TableHead className="w-0">
                  <span className="sr-only">관리</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {secrets.map((secret) => (
                <TableRow key={secret.id} data-testid={`secret-row-${secret.name}`}>
                  <TableCell className="max-w-72 align-top">
                    <div className="flex min-w-0 flex-col gap-0.5">
                      <span className="truncate font-mono font-medium">{secret.name}</span>
                      {secret.description && (
                        <span className="truncate text-caption text-muted-foreground" title={secret.description}>
                          {secret.description}
                        </span>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className="align-top">
                    <Readers secret={secret} directory={directory} />
                  </TableCell>
                  <TableCell className="align-top text-body">
                    <LastRead secret={secret} directory={directory} />
                  </TableCell>
                  <TableCell className="align-top">
                    {secret.can_read ? (
                      <RevealValue wsId={wsId} secret={secret} />
                    ) : (
                      <span className="text-caption text-muted-foreground">권한 없음</span>
                    )}
                  </TableCell>
                  <TableCell className="align-top">
                    {secret.can_manage && (
                      <div className="flex items-center gap-0.5">
                        <EditSecretDialog
                          wsId={wsId}
                          secret={secret}
                          directory={directory}
                          trigger={
                            <Button variant="ghost" size="icon-xs" aria-label={`${secret.name} 수정`}>
                              <Pencil />
                            </Button>
                          }
                        />
                        <DeleteSecretButton wsId={wsId} secret={secret} />
                      </div>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}
