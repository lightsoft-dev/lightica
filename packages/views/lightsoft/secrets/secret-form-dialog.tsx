"use client";

import { useId, useState } from "react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Eye, EyeOff } from "@multica/ui/icons";
import { SECRET_NAME_PATTERN, errorMessage, type Grant, type SecretMeta } from "./api";
import { GrantsPicker, grantKey } from "./grants-picker";
import { useCreateSecret, useEditSecret } from "./mutations";
import type { PrincipalDirectory } from "./principals";

function sameGrants(a: Grant[], b: Grant[]): boolean {
  if (a.length !== b.length) return false;
  const keys = new Set(a.map(grantKey));
  return b.every((g) => keys.has(grantKey(g)));
}

function ValueField({
  id,
  value,
  onChange,
  placeholder,
}: {
  id: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
}) {
  const [shown, setShown] = useState(false);
  return (
    <div className="flex items-start gap-1">
      {shown ? (
        <Textarea
          id={id}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          spellCheck={false}
          autoComplete="off"
          className="min-h-9 font-mono"
        />
      ) : (
        <Input
          id={id}
          type="password"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          autoComplete="new-password"
          className="font-mono"
        />
      )}
      <Button
        type="button"
        variant="ghost"
        size="icon"
        aria-label={shown ? "값 가리기" : "값 표시"}
        onClick={() => setShown(!shown)}
      >
        {shown ? <EyeOff /> : <Eye />}
      </Button>
    </div>
  );
}

/** Create a secret. Any workspace member may create one and becomes its owner. */
export function CreateSecretDialog({
  wsId,
  viewerId,
  directory,
  trigger,
}: {
  wsId: string;
  viewerId: string;
  directory: PrincipalDirectory;
  trigger: React.ReactElement;
}) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [description, setDescription] = useState("");
  const [grants, setGrants] = useState<Grant[]>([]);
  const create = useCreateSecret(wsId);
  const ids = { name: useId(), value: useId(), description: useId() };

  const nameValid = SECRET_NAME_PATTERN.test(name);
  const canSubmit = nameValid && value !== "" && !create.isPending;

  const reset = () => {
    setName("");
    setValue("");
    setDescription("");
    setGrants([]);
    create.reset();
  };

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    create
      .mutateAsync({ name, value, description: description.trim(), grants })
      .then(() => {
        setOpen(false);
        reset();
      }, () => {
        // The error stays on screen through create.error; input is kept.
      });
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) reset();
      }}
    >
      <DialogTrigger render={trigger} />
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>새 시크릿</DialogTitle>
            <DialogDescription>값은 암호화해 저장하고, 읽기 권한을 준 사람과 에이전트만 꺼낼 수 있습니다.</DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={ids.name}>이름</Label>
            <Input
              id={ids.name}
              value={name}
              onChange={(e) => setName(e.target.value.toUpperCase())}
              placeholder="GITHUB_TOKEN"
              autoComplete="off"
              spellCheck={false}
              className="font-mono"
              aria-invalid={name !== "" && !nameValid}
            />
            {name !== "" && !nameValid && (
              <p className="text-caption text-destructive">영문 대문자로 시작하고 대문자·숫자·밑줄만 쓸 수 있습니다.</p>
            )}
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={ids.value}>값</Label>
            <ValueField id={ids.value} value={value} onChange={setValue} />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={ids.description}>설명</Label>
            <Input
              id={ids.description}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <span className="text-body font-medium">읽기 권한</span>
            <GrantsPicker directory={directory} value={grants} onChange={setGrants} ownerId={viewerId} />
          </div>
          {create.error && (
            <p role="alert" className="text-body text-destructive">
              {errorMessage(create.error)}
            </p>
          )}
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="outline" />}>취소</DialogClose>
            <Button type="submit" disabled={!canSubmit} aria-busy={create.isPending}>
              만들기
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** Change a secret's description, value or readers. Owners and workspace owner/admin only. */
export function EditSecretDialog({
  wsId,
  secret,
  directory,
  trigger,
}: {
  wsId: string;
  secret: SecretMeta;
  directory: PrincipalDirectory;
  trigger: React.ReactElement;
}) {
  const [open, setOpen] = useState(false);
  const [description, setDescription] = useState(secret.description);
  const [value, setValue] = useState("");
  const [grants, setGrants] = useState<Grant[]>(secret.grants);
  const edit = useEditSecret(wsId);
  const ids = { value: useId(), description: useId() };

  const grantsChanged = !sameGrants(grants, secret.grants);
  const dirty = grantsChanged || value !== "" || description !== secret.description;

  const resetTo = (s: SecretMeta) => {
    setDescription(s.description);
    setValue("");
    setGrants(s.grants);
    edit.reset();
  };

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!dirty || edit.isPending) return;
    edit
      .mutateAsync({ secret, description: description.trim(), value, grants, grantsChanged })
      .then(() => setOpen(false), () => {
        // Kept open with the error and the input.
      });
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next) resetTo(secret);
        setOpen(next);
      }}
    >
      <DialogTrigger render={trigger} />
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle className="font-mono">{secret.name}</DialogTitle>
            <DialogDescription>값을 비워 두면 저장된 값을 그대로 둡니다.</DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={ids.value}>새 값</Label>
            <ValueField id={ids.value} value={value} onChange={setValue} />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={ids.description}>설명</Label>
            <Input
              id={ids.description}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <span className="text-body font-medium">읽기 권한</span>
            <GrantsPicker directory={directory} value={grants} onChange={setGrants} ownerId={secret.owner_id} />
          </div>
          {edit.error && (
            <p role="alert" className="text-body text-destructive">
              {errorMessage(edit.error)}
            </p>
          )}
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="outline" />}>취소</DialogClose>
            <Button type="submit" disabled={!dirty || edit.isPending} aria-busy={edit.isPending}>
              저장
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
