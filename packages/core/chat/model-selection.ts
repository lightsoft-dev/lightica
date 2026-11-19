"use client";

// Lightsoft fork (LIGH-104): per-chat-session model / reasoning effort.
//
// The server records a selection on the task each send creates; the session's
// current selection is the latest one recorded (GET .../model-selection).
// Until the next send, a change in the composer is a local draft keyed by the
// session, or by the agent for a chat that has no session yet. A send without
// a draft omits the field, so the run follows the session's recorded one.

import { create } from "zustand";
import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { ChatModelSelection } from "../api/lightsoft-chat-model-selection";

export type { ChatModelSelection } from "../api/lightsoft-chat-model-selection";

/** Runtimes the per-session selection is verified against (server mirror). */
const SUPPORTED_PROVIDERS = new Set(["claude", "codex", "pi"]);

export function chatModelSelectionSupported(provider: string | null | undefined): boolean {
  return !!provider && SUPPORTED_PROVIDERS.has(provider);
}

export const chatModelSelectionKeys = {
  session: (wsId: string, sessionId: string) =>
    ["chat", wsId, "lightsoft-model-selection", sessionId] as const,
};

export function chatModelSelectionOptions(wsId: string, sessionId: string | null) {
  return queryOptions({
    queryKey: chatModelSelectionKeys.session(wsId, sessionId ?? ""),
    queryFn: async () => (await api.getChatSessionModelSelection(sessionId ?? "")).model_selection,
    enabled: !!sessionId,
  });
}

/** Whether the connected backend records per-chat selections. Until the
 *  production backend runs the fork, the team client keeps the pickers
 *  hidden instead of offering a choice the server would drop. */
export function lightsoftCapabilitiesOptions() {
  return queryOptions({
    queryKey: ["lightsoft", "capabilities"] as const,
    queryFn: () => api.getLightsoftCapabilities(),
    staleTime: 5 * 60 * 1000,
  });
}

/** Draft key: the session, or the agent for a chat not yet created. */
export function chatModelSelectionDraftKey(sessionId: string | null, agentId: string): string {
  return sessionId ?? `new:${agentId}`;
}

export function isDefaultChatModelSelection(selection: ChatModelSelection | null | undefined): boolean {
  return !selection || (!selection.model && !selection.thinking_level);
}

interface ChatModelSelectionDraftState {
  drafts: Record<string, ChatModelSelection>;
  setDraft: (key: string, selection: ChatModelSelection) => void;
  clearDraft: (key: string) => void;
}

export const useChatModelSelectionDraftStore = create<ChatModelSelectionDraftState>()((set) => ({
  drafts: {},
  setDraft: (key, selection) =>
    set((state) => ({ drafts: { ...state.drafts, [key]: selection } })),
  clearDraft: (key) =>
    set((state) => {
      if (!(key in state.drafts)) return state;
      const { [key]: _removed, ...rest } = state.drafts;
      return { drafts: rest };
    }),
}));
