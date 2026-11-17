// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent, RuntimeModelsResult } from "@multica/core/types";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import enAgents from "../../locales/en/agents.json";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import { useChatModelSelectionDraftStore } from "@multica/core/chat/model-selection";
import { ChatModelSelectionPicker } from "./chat-model-selection-picker";

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, issues: enIssues },
};

const levels = [
  { value: "low", label: "Low" },
  { value: "high", label: "High" },
];
const CATALOG: RuntimeModelsResult = {
  models: [
    { id: "claude-sonnet-4-6", label: "Sonnet 4.6", thinking: { supported_levels: levels } },
    { id: "claude-opus-4-1", label: "Opus 4.1", thinking: { supported_levels: levels } },
  ],
  supported: true,
};

const h = vi.hoisted(() => ({
  provider: "claude",
  forkBackend: true,
  recorded: null as { model: string; thinking_level: string } | null,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (sel: (s: { user: { id: string } }) => unknown) => sel({ user: { id: "user-1" } }),
}));
vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: () => ({
    queryKey: ["runtimes", "ws-1", h.provider],
    queryFn: async () => [{ id: "rt-1", provider: h.provider, status: "online" }],
  }),
  runtimeModelsOptions: (runtimeId: string | null) => ({
    enabled: Boolean(runtimeId),
    queryKey: ["runtime-models", runtimeId],
    queryFn: async () => CATALOG,
  }),
  isRuntimeUsableForUser: () => true,
  refreshRuntimeModels: vi.fn(),
}));
vi.mock("@multica/core/api", () => ({
  api: {
    getChatSessionModelSelection: vi.fn(async () => ({ model_selection: h.recorded })),
    getLightsoftCapabilities: vi.fn(async () => ({ chat_model_selection: h.forkBackend })),
  },
}));

const agent = {
  id: "agent-1",
  runtime_id: "rt-1",
  model: "claude-sonnet-4-6",
  thinking_level: "low",
} as Agent;

function renderPicker(sessionId: string | null = "session-1") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <ChatModelSelectionPicker agent={agent} sessionId={sessionId} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("ChatModelSelectionPicker (LIGH-104)", () => {
  beforeEach(() => {
    h.provider = "claude";
    h.forkBackend = true;
    h.recorded = null;
    useChatModelSelectionDraftStore.setState({ drafts: {} });
  });
  afterEach(() => cleanup());

  it("renders nothing for a runtime without per-chat selection", async () => {
    h.provider = "hermes";
    const { container } = renderPicker();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(container).toBeEmptyDOMElement();
  });

  it("stays hidden on a backend without the fork's capability", async () => {
    h.forkBackend = false;
    const { container } = renderPicker();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(container).toBeEmptyDOMElement();
  });

  it("shows the agent's values until the chat records its own", async () => {
    renderPicker();
    expect(await screen.findByText("claude-sonnet-4-6")).toBeTruthy();
    expect(await screen.findByText("Low")).toBeTruthy();
  });

  it("shows the session's recorded selection", async () => {
    h.recorded = { model: "claude-opus-4-1", thinking_level: "high" };
    renderPicker();
    expect(await screen.findByText("claude-opus-4-1")).toBeTruthy();
    expect(await screen.findByText("High")).toBeTruthy();
  });

  it("keeps a picked model as this chat's draft", async () => {
    renderPicker(null);
    fireEvent.click(await screen.findByText("claude-sonnet-4-6"));
    fireEvent.click(await screen.findByText("Opus 4.1"));

    expect(useChatModelSelectionDraftStore.getState().drafts).toEqual({
      "new:agent-1": { model: "claude-opus-4-1", thinking_level: "" },
    });
    expect(await screen.findByText("claude-opus-4-1")).toBeTruthy();
  });

  it("stores the agent's own model as no override", async () => {
    h.recorded = { model: "claude-opus-4-1", thinking_level: "" };
    renderPicker();
    fireEvent.click(await screen.findByText("claude-opus-4-1"));
    fireEvent.click(await screen.findByText("Sonnet 4.6"));

    expect(useChatModelSelectionDraftStore.getState().drafts).toEqual({
      "session-1": { model: "", thinking_level: "" },
    });
  });
});
