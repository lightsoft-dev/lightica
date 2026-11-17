"use client";

import { Suspense } from "react";
import { configureSecretsService } from "@multica/core/lightsoft/secrets";
import { SecretsPage } from "@multica/views/lightsoft/secrets";

// Lightsoft team client only: served by lightsoft/secrets (see its README).
// The service shares the team client origin; local web dev points this at the
// service's own port.
configureSecretsService({
  baseUrl: process.env.NEXT_PUBLIC_LIGHTSOFT_SECRETS_URL ?? "/lightsoft-secrets",
});

export default function Page() {
  return (
    <Suspense fallback={null}>
      <SecretsPage />
    </Suspense>
  );
}
