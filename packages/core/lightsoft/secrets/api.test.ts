// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { CSRF_HEADER, SecretsApiError, secretsApi } from "./api";

const WS = "11111111-1111-1111-1111-111111111111";

function mockFetch(status: number, body: unknown) {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(body === undefined ? null : JSON.stringify(body), { status }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("secretsApi", () => {
  it("sends the CSRF header and cookie on writes", async () => {
    const fetchMock = mockFetch(200, { value: "s3cret" });

    await expect(secretsApi.reveal(WS, "sec-1")).resolves.toBe("s3cret");

    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe(`/lightsoft-secrets/api/w/${WS}/secrets/sec-1/reveal`);
    expect(init.method).toBe("POST");
    expect(init.credentials).toBe("include");
    expect(init.cache).toBe("no-store");
    expect(init.headers[CSRF_HEADER]).toBe("1");
  });

  it("reads without the CSRF header", async () => {
    const fetchMock = mockFetch(200, { viewer: { user_id: "u-1", role: "member" }, secrets: [] });

    await secretsApi.list(WS);

    const [, init] = fetchMock.mock.calls[0]!;
    expect(init.method).toBe("GET");
    expect(init.credentials).toBe("include");
    expect(init.headers[CSRF_HEADER]).toBeUndefined();
  });

  it("surfaces the service's error message", async () => {
    mockFetch(403, { error: "이 시크릿을 읽을 권한이 없습니다." });

    const err = await secretsApi.reveal(WS, "sec-1").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(SecretsApiError);
    expect((err as SecretsApiError).status).toBe(403);
    expect((err as SecretsApiError).message).toBe("이 시크릿을 읽을 권한이 없습니다.");
  });

  it("builds audit filters into the query string", async () => {
    const fetchMock = mockFetch(200, { events: [], next_before: null });

    await secretsApi.audit(WS, { actor_type: "agent", actor_id: "a-1" }, "ev-9");

    const [url] = fetchMock.mock.calls[0]!;
    const query = new URL(url as string, "http://x").searchParams;
    expect(query.get("actor_type")).toBe("agent");
    expect(query.get("actor_id")).toBe("a-1");
    expect(query.get("before")).toBe("ev-9");
    expect(query.has("secret_id")).toBe(false);
  });

  it("keeps a malformed list renderable", async () => {
    mockFetch(200, { secrets: "nope" });
    await expect(secretsApi.list(WS)).resolves.toEqual({ viewer: null, secrets: [] });
  });
});
