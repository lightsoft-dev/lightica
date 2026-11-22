// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

afterEach(() => {
  vi.unstubAllGlobals();
});

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("ApiClient chat model selection (LIGH-104)", () => {
  it("sendChatMessage carries model_selection only when given", async () => {
    const fetchMock = vi.fn().mockImplementation(() =>
      Promise.resolve(jsonResponse({ message_id: "m1", task_id: "t1", created_at: "2026-11-17T00:00:00Z" }, 201)),
    );
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");

    await client.sendChatMessage("session-1", "hello", undefined, undefined, { model: "claude-opus-4-1", thinking_level: "high" });
    await client.sendChatMessage("session-1", "again");

    expect(JSON.parse(fetchMock.mock.calls[0]![1]?.body as string)).toEqual({
      content: "hello",
      steer: true,
      model_selection: { model: "claude-opus-4-1", thinking_level: "high" },
    });
    expect(JSON.parse(fetchMock.mock.calls[1]![1]?.body as string)).toEqual({ content: "again", steer: true });
  });

  it("getChatSessionModelSelection returns the recorded selection", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(jsonResponse({ model_selection: { model: "gpt-5.1-codex", thinking_level: "xhigh" } })),
    );
    await expect(
      new ApiClient("https://api.example.test").getChatSessionModelSelection("session-1"),
    ).resolves.toEqual({ model_selection: { model: "gpt-5.1-codex", thinking_level: "xhigh" } });
  });

  it.each([
    ["null selection", { model_selection: null }],
    ["missing field", {}],
    ["malformed selection", { model_selection: "opus" }],
    ["malformed body", ["unexpected"]],
  ])("getChatSessionModelSelection reads %s as no selection", async (_name, body) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(body)));
    await expect(
      new ApiClient("https://api.example.test").getChatSessionModelSelection("session-1"),
    ).resolves.toEqual({ model_selection: null });
  });

  it("getChatSessionModelSelection defaults missing fields to the agent's values", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse({ model_selection: { model: "claude-opus-4-1" } })));
    await expect(
      new ApiClient("https://api.example.test").getChatSessionModelSelection("session-1"),
    ).resolves.toEqual({ model_selection: { model: "claude-opus-4-1", thinking_level: "" } });
  });

  it("getLightsoftCapabilities reads an upstream backend's 404 as no fork features", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse({ error: "not found" }, 404)));
    await expect(
      new ApiClient("https://api.example.test").getLightsoftCapabilities(),
    ).resolves.toEqual({ chat_model_selection: false, guest: false, guest_management: false });
  });

  it.each([
    ["fork backend", { chat_model_selection: true }, { chat_model_selection: true, guest: false, guest_management: false }],
    ["malformed flag", { chat_model_selection: "yes" }, { chat_model_selection: false, guest: false, guest_management: false }],
    ["guest caller", { chat_model_selection: false, guest: true, guest_management: false }, { chat_model_selection: false, guest: true, guest_management: false }],
    ["malformed guest flag", { chat_model_selection: true, guest: "yes", guest_management: 1 }, { chat_model_selection: true, guest: false, guest_management: false }],
  ])("getLightsoftCapabilities parses a %s", async (_name, body, expected) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(body)));
    await expect(
      new ApiClient("https://api.example.test").getLightsoftCapabilities(),
    ).resolves.toEqual(expected);
  });
});
