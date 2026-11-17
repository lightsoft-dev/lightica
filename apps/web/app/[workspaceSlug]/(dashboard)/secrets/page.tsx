"use client";

import { Suspense } from "react";
import { SecretsPage } from "@/features/lightsoft/secrets/secrets-page";

// Lightsoft team client only: served by lightsoft/secrets (see its README).
export default function Page() {
  return (
    <Suspense fallback={null}>
      <SecretsPage />
    </Suspense>
  );
}
