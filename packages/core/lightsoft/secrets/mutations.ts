"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { secretsApi, secretsKeys, type Grant, type SecretMeta } from "./api";

// Every write is recorded in the access history, so it is refreshed together
// with the list the write changed.
function useInvalidateSecrets(wsId: string) {
  const qc = useQueryClient();
  return () => qc.invalidateQueries({ queryKey: secretsKeys.all(wsId) });
}

export function useCreateSecret(wsId: string) {
  const invalidate = useInvalidateSecrets(wsId);
  return useMutation({
    mutationFn: (input: { name: string; description: string; value: string; grants: Grant[] }) =>
      secretsApi.create(wsId, input),
    onSettled: invalidate,
  });
}

export interface SecretEdit {
  secret: SecretMeta;
  description: string;
  /** Empty keeps the stored value. */
  value: string;
  grants: Grant[];
  grantsChanged: boolean;
}

export function useEditSecret(wsId: string) {
  const invalidate = useInvalidateSecrets(wsId);
  return useMutation({
    mutationFn: async ({ secret, description, value, grants, grantsChanged }: SecretEdit) => {
      const patch: { description?: string; value?: string } = {};
      if (description !== secret.description) patch.description = description;
      if (value !== "") patch.value = value;
      if (Object.keys(patch).length > 0) await secretsApi.update(wsId, secret.id, patch);
      if (grantsChanged) await secretsApi.setGrants(wsId, secret.id, grants);
    },
    onSettled: invalidate,
  });
}

export function useDeleteSecret(wsId: string) {
  const invalidate = useInvalidateSecrets(wsId);
  return useMutation({
    mutationFn: (id: string) => secretsApi.remove(wsId, id),
    onSettled: invalidate,
  });
}

export function useEnrollAgent(wsId: string) {
  const invalidate = useInvalidateSecrets(wsId);
  return useMutation({
    mutationFn: (agentId: string) => secretsApi.enroll(wsId, agentId),
    onSettled: invalidate,
  });
}

export function useUnenrollAgent(wsId: string) {
  const invalidate = useInvalidateSecrets(wsId);
  return useMutation({
    mutationFn: (agentId: string) => secretsApi.unenroll(wsId, agentId),
    onSettled: invalidate,
  });
}
