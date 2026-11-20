"use client";

import { Suspense } from "react";
import { configureUsageService } from "@multica/core/lightsoft/usage";
import { SubscriptionLimitsPage } from "@multica/views/lightsoft/usage";

// Lightsoft team client only: served by lightsoft/usage (see its README).
// The service shares the team client origin; local web dev points this at the
// service's own port.
configureUsageService({
  baseUrl: process.env.NEXT_PUBLIC_LIGHTSOFT_USAGE_URL ?? "/lightsoft-usage",
});

export default function Page() {
  return (
    <Suspense fallback={null}>
      <SubscriptionLimitsPage />
    </Suspense>
  );
}
