import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const loginWithCloudflareAccess = vi.fn();
const authState = { loginWithCloudflareAccess };

vi.mock("@multica/core/auth", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/auth")>("@multica/core/auth");
  const useAuthStore = Object.assign(
    (selector: (s: typeof authState) => unknown) => selector(authState),
    { getState: () => authState },
  );
  return { ...actual, useAuthStore };
});

import {
  CloudflareAccessLoginNotice,
  noteCloudflareAccessLogout,
  useCloudflareAccessLogin,
} from "./cloudflare-access-login";

function Harness({ ready, onSignedIn }: { ready: boolean; onSignedIn: () => void }) {
  const login = useCloudflareAccessLogin({ ready, onSignedIn });
  return (
    <>
      <span data-testid="phase">{login.phase}</span>
      <CloudflareAccessLoginNotice login={login} />
    </>
  );
}

function denied(code: string, extra: Record<string, string> = {}) {
  return Object.assign(new Error(code), { status: 403, body: { code, ...extra } });
}

describe("useCloudflareAccessLogin", () => {
  beforeEach(() => {
    loginWithCloudflareAccess.mockReset();
    window.sessionStorage.clear();
  });

  it("waits until auth has settled logged out", () => {
    render(<Harness ready={false} onSignedIn={vi.fn()} />);
    expect(loginWithCloudflareAccess).not.toHaveBeenCalled();
  });

  it("signs in once through Access and hands over navigation", async () => {
    loginWithCloudflareAccess.mockResolvedValue({ id: "u-1" });
    const onSignedIn = vi.fn();
    const { rerender } = render(<Harness ready onSignedIn={onSignedIn} />);
    await waitFor(() => expect(onSignedIn).toHaveBeenCalledTimes(1));
    rerender(<Harness ready onSignedIn={onSignedIn} />);
    expect(loginWithCloudflareAccess).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("phase")).toHaveTextContent("signed_in");
  });

  it("falls back to the form silently without an Access identity", async () => {
    loginWithCloudflareAccess.mockRejectedValue(denied("cf_access_no_assertion"));
    const onSignedIn = vi.fn();
    render(<Harness ready onSignedIn={onSignedIn} />);
    await waitFor(() => expect(screen.getByTestId("phase")).toHaveTextContent("unavailable"));
    expect(onSignedIn).not.toHaveBeenCalled();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.queryByText(/Cloudflare Access/)).not.toBeInTheDocument();
  });

  it("names the Access email that has no invitation", async () => {
    loginWithCloudflareAccess.mockRejectedValue(
      denied("cf_access_not_invited", { email: "guest@example.com" }),
    );
    render(<Harness ready onSignedIn={vi.fn()} />);
    expect(await screen.findByText(/guest@example\.com/)).toBeInTheDocument();
  });

  it("does not sign back in after an explicit logout until asked", async () => {
    loginWithCloudflareAccess.mockResolvedValue({ id: "u-1" });
    const first = render(<Harness ready onSignedIn={vi.fn()} />);
    await waitFor(() => expect(loginWithCloudflareAccess).toHaveBeenCalledTimes(1));
    first.unmount();

    noteCloudflareAccessLogout();
    const onSignedIn = vi.fn();
    render(<Harness ready onSignedIn={onSignedIn} />);
    const button = await screen.findByRole("button", { name: "Cloudflare Access 계정으로 로그인" });
    expect(loginWithCloudflareAccess).toHaveBeenCalledTimes(1);

    await userEvent.click(button);
    await waitFor(() => expect(onSignedIn).toHaveBeenCalledTimes(1));
    expect(loginWithCloudflareAccess).toHaveBeenCalledTimes(2);
  });

  it("keeps auto sign-in when the session was not an Access one", async () => {
    noteCloudflareAccessLogout();
    loginWithCloudflareAccess.mockResolvedValue({ id: "u-1" });
    render(<Harness ready onSignedIn={vi.fn()} />);
    await waitFor(() => expect(loginWithCloudflareAccess).toHaveBeenCalledTimes(1));
  });
});
