"use client";

import { useQuery } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import {
  isRuntimeUsableForUser,
  runtimeListOptions,
  runtimeModelsOptions,
} from "@multica/core/runtimes";
import {
  chatModelSelectionDraftKey,
  chatModelSelectionOptions,
  chatModelSelectionSupported,
  lightsoftCapabilitiesOptions,
  useChatModelSelectionDraftStore,
  type ChatModelSelection,
} from "@multica/core/chat/model-selection";
import { ModelPicker } from "../../agents/components/inspector/model-picker";
import { ThinkingPicker } from "../../agents/components/inspector/thinking-picker";
import { findModelCapabilityEntry } from "../../agents/components/inspector/model-capability";

const AGENT_DEFAULT: ChatModelSelection = { model: "", thinking_level: "" };

/**
 * Lightsoft fork (LIGH-104): the composer's model / effort pickers for this
 * chat. Shows the agent's saved values until the chat overrides them; picking
 * the agent's own value stores "" so the chat keeps following the agent. A
 * change is a draft until the next send records it on the server.
 */
export function ChatModelSelectionPicker({
  agent,
  sessionId,
}: {
  agent: Agent | null | undefined;
  sessionId: string | null;
}) {
  const wsId = useWorkspaceId();
  const currentUserId = useAuthStore((s) => s.user?.id ?? null);
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const runtime = agent?.runtime_id
    ? runtimes.find((r) => r.id === agent.runtime_id) ?? null
    : null;
  const provider = runtime?.provider ?? "";
  const { data: capabilities } = useQuery(lightsoftCapabilitiesOptions());
  const supported =
    capabilities?.chat_model_selection === true && !!agent && chatModelSelectionSupported(provider);
  const runtimeOnline =
    !!runtime && runtime.status === "online" && isRuntimeUsableForUser(runtime, currentUserId);

  const { data: recorded } = useQuery({
    ...chatModelSelectionOptions(wsId, sessionId),
    enabled: supported && !!sessionId,
  });
  const draftKey = agent ? chatModelSelectionDraftKey(sessionId, agent.id) : "";
  const draft = useChatModelSelectionDraftStore((s) => (draftKey ? s.drafts[draftKey] : undefined));
  const setDraft = useChatModelSelectionDraftStore((s) => s.setDraft);

  const modelsQuery = useQuery(
    runtimeModelsOptions(supported && runtimeOnline ? agent?.runtime_id ?? null : null),
  );

  if (!agent || !supported) return null;

  const selection = draft ?? recorded ?? AGENT_DEFAULT;
  const model = selection.model || agent.model || "";
  const thinkingLevel = selection.thinking_level || agent.thinking_level || "";

  const models = modelsQuery.data?.models ?? [];
  const entry = model
    ? findModelCapabilityEntry(models, model, provider)
    : provider === "codex"
      ? undefined
      : models.find((m) => m.default) ?? models[0];
  const levels = entry?.thinking?.supported_levels ?? [];

  const update = (next: Partial<ChatModelSelection>) => {
    const merged = { ...selection, ...next };
    setDraft(draftKey, {
      model: merged.model === (agent.model ?? "") ? "" : merged.model,
      thinking_level: merged.thinking_level === (agent.thinking_level ?? "") ? "" : merged.thinking_level,
    });
  };

  return (
    <div className="flex min-w-0 max-w-[16rem] items-center gap-0.5">
      <ModelPicker
        runtimeId={agent.runtime_id ?? null}
        runtimeOnline={runtimeOnline}
        value={model}
        onChange={(next) => update({ model: next })}
      />
      {(levels.length > 0 || thinkingLevel) && (
        <ThinkingPicker
          value={thinkingLevel}
          levels={levels}
          onChange={(next) => update({ thinking_level: next })}
        />
      )}
    </div>
  );
}
