"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useAuthStore } from "@multica/core/auth";
import { Button } from "@multica/ui/components/ui/button";

// Lightsoft fork (LIGH-253). Outside the tailnet the team client sits behind
// Cloudflare Access. The login page exchanges the Access identity for a
// Multica session once on arrival; through the tailnet the backend finds no
// Access identity and the page simply shows the normal form.

// Per-tab marker: "active" after an Access sign-in, "signed_out" after the
// user logs out of that session. An explicit logout must not bounce straight
// back in, but a session the server ended may.
const SESSION_KEY = "lightsoft_cf_access";

function readMarker(): string | null {
  try {
    return window.sessionStorage.getItem(SESSION_KEY);
  } catch {
    return null;
  }
}

function writeMarker(value: string) {
  try {
    window.sessionStorage.setItem(SESSION_KEY, value);
  } catch {
    // Storage unavailable (private mode quota): auto sign-in just stays on.
  }
}

/** Call on explicit logout so this tab stops signing back in through Access. */
export function noteCloudflareAccessLogout() {
  if (readMarker() === "active") writeMarker("signed_out");
}

export type CloudflareAccessPhase =
  | "idle"
  | "trying"
  | "unavailable"
  | "signed_out"
  | "not_invited"
  | "rejected"
  | "signed_in";

export interface CloudflareAccessLogin {
  phase: CloudflareAccessPhase;
  email?: string;
  signIn: () => void;
}

// Duck-typed so this does not depend on the ApiError class identity.
function failure(err: unknown): { code?: string; email?: string } {
  const body =
    err && typeof err === "object" && "body" in err
      ? (err as { body?: unknown }).body
      : undefined;
  if (!body || typeof body !== "object") return {};
  const { code, email } = body as Record<string, unknown>;
  return {
    code: typeof code === "string" ? code : undefined,
    email: typeof email === "string" ? email : undefined,
  };
}

export function useCloudflareAccessLogin({
  ready,
  onSignedIn,
}: {
  /** Auth has settled logged out and the page is a plain browser login. */
  ready: boolean;
  onSignedIn: () => void | Promise<void>;
}): CloudflareAccessLogin {
  const [phase, setPhase] = useState<CloudflareAccessPhase>("idle");
  const [email, setEmail] = useState<string>();
  const attemptedRef = useRef(false);
  const onSignedInRef = useRef(onSignedIn);
  useEffect(() => {
    onSignedInRef.current = onSignedIn;
  }, [onSignedIn]);

  const attempt = useCallback(async () => {
    setPhase("trying");
    try {
      await useAuthStore.getState().loginWithCloudflareAccess();
    } catch (err) {
      const { code, email: deniedEmail } = failure(err);
      if (code === "cf_access_not_invited") {
        setEmail(deniedEmail);
        setPhase("not_invited");
      } else if (
        code === "cf_access_invalid_assertion" ||
        code === "cf_access_user_disabled"
      ) {
        setPhase("rejected");
      } else {
        // 404 (no Access login on this backend), no Access identity on this
        // request (tailnet), or a network error: fall back to the form.
        setPhase("unavailable");
      }
      return;
    }
    writeMarker("active");
    setPhase("signed_in");
    await onSignedInRef.current();
  }, []);

  useEffect(() => {
    if (!ready || attemptedRef.current) return;
    attemptedRef.current = true;
    if (readMarker() === "signed_out") {
      setPhase("signed_out");
      return;
    }
    void attempt();
  }, [ready, attempt]);

  const signIn = useCallback(() => {
    void attempt();
  }, [attempt]);

  return { phase, email, signIn };
}

export function CloudflareAccessLoginNotice({
  login,
}: {
  login: CloudflareAccessLogin;
}) {
  switch (login.phase) {
    case "signed_out":
      return (
        <Button
          type="button"
          variant="outline"
          className="mb-3 w-full"
          onClick={login.signIn}
        >
          Cloudflare Access 계정으로 로그인
        </Button>
      );
    case "not_invited":
      return (
        <p className="mb-3 text-caption text-destructive">
          Cloudflare Access로 확인한 {login.email ?? "이 계정"}은 초대되지 않은
          계정입니다. 초대받은 이메일로 로그인하세요.
        </p>
      );
    case "rejected":
      return (
        <p className="mb-3 text-caption text-destructive">
          Cloudflare Access 로그인을 확인하지 못했습니다. 페이지를 새로고침해
          보세요.
        </p>
      );
    default:
      return null;
  }
}
