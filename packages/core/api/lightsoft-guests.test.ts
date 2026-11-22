// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("Lightsoft guest list (LIGH-282)", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("parses the list and fills missing fields", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse({
      guests: [
        { email: "a@x.io", user_id: "u1", name: "A", member_id: "m1", joined_at: "2026-11-22T00:00:00Z", projects: [{ id: "p1", title: "P" }] },
        { email: "b@x.io" },
      ],
    })));
    const list = await new ApiClient("https://api.example.test").listLightsoftGuests();
    expect(list.guests[0]).toEqual({ email: "a@x.io", user_id: "u1", name: "A", member_id: "m1", joined_at: "2026-11-22T00:00:00Z", projects: [{ id: "p1", title: "P" }] });
    expect(list.guests[1]).toEqual({ email: "b@x.io", user_id: null, name: null, member_id: null, joined_at: null, projects: [] });
  });

  it("reads a malformed payload as an empty list", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse({ guests: "nope" })));
    await expect(new ApiClient("https://api.example.test").listLightsoftGuests()).resolves.toEqual({ guests: [] });
  });

  it("removes with promote only when asked", async () => {
    const fetchMock = vi.fn().mockImplementation(async () => jsonResponse({ guests: [] }));
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");
    await client.removeLightsoftGuest("a@x.io");
    await client.removeLightsoftGuest("a@x.io", { promote: true });
    expect(String(fetchMock.mock.calls[0]![0])).toContain("/api/lightsoft/guests?email=a%40x.io");
    expect(String(fetchMock.mock.calls[0]![0])).not.toContain("promote");
    expect(String(fetchMock.mock.calls[1]![0])).toContain("promote=1");
  });
});
