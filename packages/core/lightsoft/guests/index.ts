"use client";

// Lightsoft outside collaborators (LIGH-282): whether the caller is a guest
// of the current workspace, and the guest list owners and admins manage.
// Rules and procedure: lightsoft/README.md ("외부 협업자").
import { useContext } from "react";
import {
  QueryClient,
  QueryClientContext,
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { api } from "../../api";
import { NO_LIGHTSOFT_CAPABILITIES } from "../../api/lightsoft-chat-model-selection";
import type { LightsoftGuest, LightsoftGuestList } from "../../api/lightsoft-guests";
import { workspaceKeys } from "../../workspace/queries";
import { projectKeys } from "../../projects/queries";

export type { LightsoftGuest, LightsoftGuestList, LightsoftGuestProject } from "../../api/lightsoft-guests";

export const lightsoftGuestKeys = {
  capabilities: (wsId: string) => ["lightsoft", "capabilities", wsId] as const,
  list: (wsId: string) => ["lightsoft", "guests", wsId] as const,
};

/** Capabilities as the current workspace answers them; guest status is per
 *  workspace, so the key carries wsId. An upstream backend reads as all-false. */
export function lightsoftWorkspaceCapabilitiesOptions(wsId: string) {
  return queryOptions({
    queryKey: lightsoftGuestKeys.capabilities(wsId),
    queryFn: () => api.getLightsoftCapabilities(),
    enabled: !!wsId,
    staleTime: 60 * 1000,
  });
}

/**
 * Whether the caller is an outside collaborator of wsId. The server enforces
 * the limits; this only keeps the client from offering what it would refuse.
 * While unknown it reads as false, the same as an upstream backend.
 */
export function useLightsoftGuest(wsId: string | null | undefined): {
  isGuest: boolean;
  canManageGuests: boolean;
} {
  // Shared components also render outside a QueryClientProvider (isolated
  // trees, tests); there the answer is "not a guest" instead of a crash.
  const client = useContext(QueryClientContext);
  const options = lightsoftWorkspaceCapabilitiesOptions(wsId ?? "");
  const { data = NO_LIGHTSOFT_CAPABILITIES } = useQuery(
    { ...options, enabled: !!client && !!wsId },
    client ?? inertQueryClient(),
  );
  return { isGuest: data.guest, canManageGuests: data.guest_management };
}

let inert: QueryClient | null = null;
function inertQueryClient(): QueryClient {
  inert ??= new QueryClient();
  return inert;
}

export function lightsoftGuestListOptions(wsId: string) {
  return queryOptions({
    queryKey: lightsoftGuestKeys.list(wsId),
    queryFn: () => api.listLightsoftGuests(),
    enabled: !!wsId,
  });
}

function useStoreGuestList(wsId: string) {
  const qc = useQueryClient();
  return (list: LightsoftGuestList) => {
    qc.setQueryData(lightsoftGuestKeys.list(wsId), list);
    qc.invalidateQueries({ queryKey: workspaceKeys.members(wsId) });
    qc.invalidateQueries({ queryKey: workspaceKeys.invitations(wsId) });
  };
}

/** Lists the email as a guest, then invites it unless it is already a member:
 *  listing first makes the account a guest from its first request. */
export function useAddLightsoftGuest(wsId: string) {
  const store = useStoreGuestList(wsId);
  return useMutation({
    mutationFn: async ({ email, invite }: { email: string; invite: boolean }) => {
      const list = await api.addLightsoftGuest(email);
      const entry = list.guests.find((g) => g.email === email.trim().toLowerCase());
      if (invite && !entry?.member_id) {
        await api.createMember(wsId, { email: email.trim(), role: "member" });
      }
      return list;
    },
    onSuccess: store,
  });
}

/** Removes a list entry. A current member needs promote (they become a full
 *  member); to revoke access, remove the member from the workspace instead. */
export function useRemoveLightsoftGuest(wsId: string) {
  const store = useStoreGuestList(wsId);
  return useMutation({
    mutationFn: ({ guest, promote }: { guest: LightsoftGuest; promote: boolean }) =>
      api.removeLightsoftGuest(guest.email, { promote }),
    onSuccess: store,
  });
}

/** Opens or closes one project to a guest: project membership, which works on
 *  workspace-visible projects too, is all a guest can see. */
export function useSetLightsoftGuestProject(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ userId, projectId, open }: { userId: string; projectId: string; open: boolean }) => {
      if (open) await api.addProjectMembers(projectId, [userId]);
      else await api.removeProjectMember(projectId, userId);
    },
    onSettled: async () => {
      await qc.invalidateQueries({ queryKey: lightsoftGuestKeys.list(wsId) });
      await qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
    },
  });
}
