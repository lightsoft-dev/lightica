"use client";
/* eslint-disable i18next/no-literal-string, no-restricted-syntax -- Lightsoft fork page (LIGH-282): the team UI is Korean-only, so copy is inline rather than in the upstream locale files that daily release merges touch. */

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useCurrentWorkspace } from "@multica/core/paths";
import {
  lightsoftGuestListOptions,
  useAddLightsoftGuest,
  useRemoveLightsoftGuest,
  useSetLightsoftGuestProject,
  type LightsoftGuest,
} from "@multica/core/lightsoft/guests";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Project } from "@multica/core/types";
import { Check, FolderKanban } from "@multica/ui/icons";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { Command, CommandEmpty, CommandInput, CommandItem, CommandList } from "@multica/ui/components/ui/command";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { SettingsCard, SettingsRow, SettingsSection, SettingsTab } from "../../settings/components/settings-layout";

// Lightsoft fork (LIGH-282): outside collaborators. An account listed here sees
// only the projects it is a project member of and cannot start agent runs; the
// server enforces it. Procedure: lightsoft/README.md ("외부 협업자").

function errorMessage(e: unknown, fallback: string): string {
  return e instanceof Error && e.message ? e.message : fallback;
}

function GuestStatus({ guest }: { guest: LightsoftGuest }) {
  if (guest.member_id) return <Badge variant="secondary">참여 중</Badge>;
  if (guest.user_id) return <Badge variant="outline">워크스페이스 밖</Badge>;
  return <Badge variant="outline">초대 대기</Badge>;
}

/** Which projects a joined guest can see; workspace-visible ones included. */
function GuestProjectsPicker({ wsId, guest, projects }: { wsId: string; guest: LightsoftGuest; projects: Project[] }) {
  const setProject = useSetLightsoftGuestProject(wsId);
  const open = new Set(guest.projects.map((p) => p.id));
  const toggle = (project: Project) => {
    if (!guest.user_id) return;
    const grant = !open.has(project.id);
    setProject.mutateAsync({ userId: guest.user_id, projectId: project.id, open: grant }).then(
      () => toast.success(grant ? `${project.title}을(를) 열었습니다.` : `${project.title} 접근을 회수했습니다.`),
      (e: unknown) => toast.error(errorMessage(e, "프로젝트 접근을 바꾸지 못했습니다.")),
    );
  };
  return (
    <Popover>
      <PopoverTrigger render={<Button variant="outline" size="sm"><FolderKanban />프로젝트</Button>} />
      <PopoverContent align="end" className="w-80 p-0">
        <Command>
          <CommandInput placeholder="프로젝트 검색" aria-label="프로젝트 검색" />
          <CommandList>
            <CommandEmpty>프로젝트가 없습니다.</CommandEmpty>
            {projects.map((project) => (
              <CommandItem key={project.id} value={project.title} disabled={setProject.isPending} onSelect={() => toggle(project)}>
                <Check className={open.has(project.id) ? "opacity-100" : "opacity-0"} />
                <span className="min-w-0 flex-1 truncate">{project.title}</span>
                {project.visibility === "private" ? <span className="text-caption text-muted-foreground">비공개</span> : null}
              </CommandItem>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

function GuestRow({ wsId, guest, projects: allProjects, onRemove }: { wsId: string; guest: LightsoftGuest; projects: Project[]; onRemove: (g: LightsoftGuest) => void }) {
  const projects = guest.projects.map((p) => p.title).join(", ");
  return (
    <SettingsRow
      label={
        <span className="flex min-w-0 items-center gap-2">
          <span className="truncate">{guest.name ? `${guest.name} · ${guest.email}` : guest.email}</span>
          <GuestStatus guest={guest} />
        </span>
      }
      description={
        guest.member_id
          ? projects
            ? `볼 수 있는 프로젝트: ${projects}`
            : "아직 볼 수 있는 프로젝트가 없습니다. 프로젝트 버튼으로 여세요."
          : undefined
      }
    >
      <div className="flex shrink-0 items-center gap-2">
        {guest.member_id ? <GuestProjectsPicker wsId={wsId} guest={guest} projects={allProjects} /> : null}
        <Button variant="outline" size="sm" onClick={() => onRemove(guest)}>
          {guest.member_id ? "정식 멤버로 전환" : "목록에서 삭제"}
        </Button>
      </div>
    </SettingsRow>
  );
}

export function LightsoftGuestsTab() {
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const { data, isLoading } = useQuery(lightsoftGuestListOptions(wsId));
  const add = useAddLightsoftGuest(wsId);
  const remove = useRemoveLightsoftGuest(wsId);
  const [email, setEmail] = useState("");
  const [pending, setPending] = useState<LightsoftGuest | null>(null);
  const guests = data?.guests ?? [];
  const { data: allProjects = [] } = useQuery(projectListOptions(wsId));
  const projects = allProjects.filter((p) => !p.archived_at);

  const submit = () => {
    const value = email.trim();
    if (!value) return;
    add.mutateAsync({ email: value, invite: true }).then(
      () => {
        setEmail("");
        toast.success("외부 협업자로 등록하고 초대를 보냈습니다.");
      },
      (e: unknown) => toast.error(errorMessage(e, "등록하지 못했습니다.")),
    );
  };

  const confirmRemove = () => {
    if (!pending) return;
    const guest = pending;
    remove.mutateAsync({ guest, promote: !!guest.member_id }).then(
      () => {
        setPending(null);
        toast.success(guest.member_id ? "정식 멤버로 전환했습니다." : "목록에서 삭제했습니다.");
      },
      (e: unknown) => toast.error(errorMessage(e, "변경하지 못했습니다.")),
    );
  };

  return (
    <SettingsTab title="외부 협업자" scope="workspace">
      <SettingsSection
        title="외부 협업자 추가"
        description="등록한 계정은 열어 준 프로젝트와 그 이슈만 보고, 에이전트를 실행할 수 없습니다. 초대를 수락하면 목록의 프로젝트 버튼으로 프로젝트를 여세요."
      >
        <SettingsCard>
          <form
            className="flex flex-col gap-2 px-4 py-3.5 sm:flex-row"
            onSubmit={(e) => {
              e.preventDefault();
              submit();
            }}
          >
            <Input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="partner@example.com"
              aria-label="외부 협업자 이메일"
            />
            <Button type="submit" disabled={!email.trim() || add.isPending}>
              등록하고 초대
            </Button>
          </form>
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title="목록"
        description="접근을 회수하려면 구성원 탭에서 워크스페이스에서 내보내세요. 목록에는 남아 있어 다시 초대해도 외부 협업자입니다."
      >
        <SettingsCard>
          {isLoading ? (
            <div className="px-4 py-3.5 text-muted-foreground">불러오는 중…</div>
          ) : guests.length === 0 ? (
            <div className="px-4 py-3.5 text-muted-foreground">등록된 외부 협업자가 없습니다.</div>
          ) : (
            guests.map((g) => <GuestRow key={g.email} wsId={wsId} guest={g} projects={projects} onRemove={setPending} />)
          )}
        </SettingsCard>
      </SettingsSection>

      <AlertDialog open={pending !== null} onOpenChange={(open) => !open && setPending(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{pending?.member_id ? "정식 멤버로 전환할까요?" : "목록에서 삭제할까요?"}</AlertDialogTitle>
            <AlertDialogDescription>
              {pending?.member_id
                ? `${pending.email}은(는) 워크스페이스의 모든 공개 프로젝트와 에이전트에 접근하게 됩니다. 접근을 끊으려면 대신 구성원 탭에서 내보내세요.`
                : `${pending?.email ?? ""}을(를) 나중에 초대하면 일반 멤버가 됩니다.`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>취소</AlertDialogCancel>
            <AlertDialogAction
              variant={pending?.member_id ? "destructive" : "default"}
              disabled={remove.isPending}
              onClick={confirmRemove}
            >
              {pending?.member_id ? "전환" : "삭제"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsTab>
  );
}
