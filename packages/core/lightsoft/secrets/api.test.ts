// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { CSRF_HEADER, SecretsApiError, secretsApi } from "./api";

// Web runs the API client in cookie mode with a same-origin base; Lightica
// (desktop) runs it with the team client origin and a bearer session token.
const apiState = vi.hoisted(() => ({ baseUrl: "", token: null as string | null }));
vi.mock("../../api", () => ({
  api: { getBaseUrl: () => apiState.baseUrl, getToken: () => apiState.token },
}));

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
  apiState.baseUrl = "";
  apiState.token = null;
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

  it("sends no bearer token in cookie mode (web)", async () => {
    const fetchMock = mockFetch(200, { viewer: null, secrets: [] });

    await secretsApi.list(WS);

    const [, init] = fetchMock.mock.calls[0]!;
    expect(init.headers.Authorization).toBeUndefined();
  });

  it("calls the team client origin with the session token in Lightica", async () => {
    apiState.baseUrl = "https://mac-studio.taile83671.ts.net:3444/";
    apiState.token = "eyJhbGciOiJIUzI1NiJ9.e30.sig";
    const fetchMock = mockFetch(200, { value: "s3cret" });

    await secretsApi.reveal(WS, "sec-1");

    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe(
      `https://mac-studio.taile83671.ts.net:3444/lightsoft-secrets/api/w/${WS}/secrets/sec-1/reveal`,
    );
    expect(init.headers.Authorization).toBe("Bearer eyJhbGciOiJIUzI1NiJ9.e30.sig");
  });

  it("prefers the platform override for the service URL", async () => {
    vi.resetModules();
    const mod = await import("./api");
    apiState.baseUrl = "https://ignored.example";
    mod.configureSecretsService({ baseUrl: "http://localhost:13301/lightsoft-secrets/" });
    const fetchMock = mockFetch(200, { viewer: null, secrets: [] });

    await mod.secretsApi.list(WS);

    expect(fetchMock.mock.calls[0]![0]).toBe(
      `http://localhost:13301/lightsoft-secrets/api/w/${WS}/secrets`,
    );
  });
});
