// @vitest-environment node
import { beforeEach, describe, expect, it } from "vitest";
import {
  chatModelSelectionDraftKey,
  chatModelSelectionSupported,
  isDefaultChatModelSelection,
  useChatModelSelectionDraftStore,
} from "./model-selection";

describe("chat model selection (LIGH-104)", () => {
  beforeEach(() => {
    useChatModelSelectionDraftStore.setState({ drafts: {} });
  });

  it("keys a draft by session, or by agent before the session exists", () => {
    expect(chatModelSelectionDraftKey("session-1", "agent-1")).toBe("session-1");
    expect(chatModelSelectionDraftKey(null, "agent-1")).toBe("new:agent-1");
  });

  it("supports the verified runtimes only", () => {
    expect(["claude", "codex", "pi"].every(chatModelSelectionSupported)).toBe(true);
    expect(chatModelSelectionSupported("hermes")).toBe(false);
    expect(chatModelSelectionSupported("")).toBe(false);
  });

  it("treats an empty selection as the agent's defaults", () => {
    expect(isDefaultChatModelSelection(null)).toBe(true);
    expect(isDefaultChatModelSelection({ model: "", thinking_level: "" })).toBe(true);
    expect(isDefaultChatModelSelection({ model: "", thinking_level: "high" })).toBe(false);
  });

  it("sets and clears drafts per key", () => {
    const { setDraft, clearDraft } = useChatModelSelectionDraftStore.getState();
    setDraft("session-1", { model: "claude-opus-4-1", thinking_level: "" });
    setDraft("new:agent-1", { model: "", thinking_level: "low" });
    clearDraft("session-1");
    clearDraft("missing");
    expect(useChatModelSelectionDraftStore.getState().drafts).toEqual({
      "new:agent-1": { model: "", thinking_level: "low" },
    });
  });
});
