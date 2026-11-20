"use client";
/* eslint-disable i18next/no-literal-string -- Lightsoft fork page (LIGH-113): the team UI is Korean-only, so copy is inline rather than in the upstream locale files that daily release merges touch. */

import { useCallback, useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Gauge, RefreshCw } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useWorkspaceId } from "@multica/core/hooks";
import { runtimeDisplayName, runtimeListOptions } from "@multica/core/runtimes";
import { describeRateLimitWindow } from "@multica/core/runtimes/rate-limits";
import {
  LIMIT_HISTORY_DAYS,
  subscriptionLimitsOptions,
  type LimitHistoryDays,
  type LimitHistorySeries,
  type SubscriptionAccount,
} from "@multica/core/lightsoft/usage";
import type { AgentRuntime } from "@multica/core/types";
import { CollectionPageHeader } from "../../layout";
import { ObjectSectionCard, ObjectSectionEmpty } from "../../layout/object-page";
import { Segmented } from "../../dashboard/components/dashboard-shared";
import { WindowRow } from "../../runtimes/components/rate-limit-section";
import { useT, useTimeAgo } from "../../i18n";
import { LimitHistoryChart } from "./limit-history-chart";

const PROVIDER_LABEL: Record<string, string> = { claude: "Claude Code", codex: "Codex" };
const EMPTY_RUNTIMES: AgentRuntime[] = [];

const DAY_OPTIONS = LIMIT_HISTORY_DAYS.map((d) => ({ label: `${d}일`, value: d }));

// Re-evaluates countdowns and pace between refetches.
function useMinuteTick(): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 60_000);
    return () => clearInterval(id);
  }, []);
  return now;
}

// Same rule as the runtime pages: "max_20x" → "Max 20x".
function planLabel(plan: string): string {
  const words = plan.replace(/[_-]+/g, " ").trim();
  return words ? words.charAt(0).toUpperCase() + words.slice(1) : "";
}

function creditText(credits: NonNullable<SubscriptionAccount["rate_limits"]>["credits"]): string | null {
  if (!credits) return null;
  if (credits.unlimited) return "무제한";
  if (!credits.has_credits) return "없음";
  const n = Number(credits.balance);
  return credits.balance.trim() !== "" && Number.isFinite(n)
    ? `잔액 ${new Intl.NumberFormat("ko-KR", { maximumFractionDigits: 2 }).format(n)}`
    : `잔액 ${credits.balance}`;
}

function AccountSection({
  account,
  runtimeNames,
  now,
  from,
  seriesLabel,
}: {
  account: SubscriptionAccount;
  runtimeNames: Map<string, string>;
  now: number;
  from: number;
  seriesLabel: (s: LimitHistorySeries) => string;
}) {
  const timeAgo = useTimeAgo();
  const rateLimits = account.rate_limits;
  const views = useMemo(
    () =>
      rateLimits ? rateLimits.windows.map((w) => describeRateLimitWindow(w, rateLimits.observed_at, now)) : [],
    [rateLimits, now],
  );
  const plan = rateLimits ? planLabel(rateLimits.plan_type) : "";
  const credits = creditText(rateLimits?.credits);
  const runtimes = account.runtime_ids.map((id) => runtimeNames.get(id)).filter((n): n is string => !!n);
  const hasHistory = account.history.some((s) => s.points.length > 0);

  return (
    <section aria-label={account.name} className="min-w-0 space-y-2">
      <div className="flex min-w-0 items-center gap-2">
        <h2 className="min-w-0 truncate text-label font-medium">{account.name}</h2>
        {plan && (
          <span className="shrink-0 rounded-sm bg-muted px-1.5 py-0.5 text-caption font-medium">{plan}</span>
        )}
        <span className="shrink-0 text-caption text-muted-foreground">
          {PROVIDER_LABEL[account.provider] ?? account.provider}
        </span>
        {rateLimits && (
          <span className="ml-auto shrink-0 text-caption tabular-nums text-muted-foreground">
            {timeAgo(rateLimits.observed_at)} 기준
          </span>
        )}
      </div>
      <ObjectSectionCard>
        {account.error && (
          <p role="status" className="px-4 py-2.5 text-caption text-warning">
            {account.error}
          </p>
        )}
        {!rateLimits && !account.error && (
          <p className="px-4 py-3 text-caption text-muted-foreground">아직 읽은 값이 없습니다.</p>
        )}
        {rateLimits && views.length === 0 && (
          <p className="px-4 py-3 text-caption text-muted-foreground">진행 중인 한도 기간이 없습니다.</p>
        )}
        {views.map((view, index) => (
          <WindowRow key={`${view.window.kind}-${index}`} view={view} />
        ))}
        {credits && (
          <div className="flex items-center justify-between gap-4 px-4 py-3">
            <span className="text-label font-medium">크레딧</span>
            <span className="text-body font-medium tabular-nums">{credits}</span>
          </div>
        )}
        {hasHistory && (
          <div className="px-4 py-3">
            <LimitHistoryChart series={account.history} labelFor={seriesLabel} from={from} to={now} />
          </div>
        )}
        {runtimes.length > 0 && (
          <p className="truncate px-4 py-2.5 text-caption text-muted-foreground" title={runtimes.join(", ")}>
            런타임 · {runtimes.join(", ")}
          </p>
        )}
      </ObjectSectionCard>
    </section>
  );
}

/** Subscription limits of the agent CLI accounts on the Mac Studio, with their recent trend. */
export function SubscriptionLimitsPage() {
  const wsId = useWorkspaceId();
  const { t } = useT("runtimes");
  const now = useMinuteTick();
  const [days, setDays] = useState<LimitHistoryDays>(7);
  const { data, error, isLoading, isFetching, refetch } = useQuery(subscriptionLimitsOptions(wsId, days));
  const { data: runtimes = EMPTY_RUNTIMES } = useQuery({ ...runtimeListOptions(wsId), enabled: !!wsId });

  const runtimeNames = useMemo(
    () => new Map(runtimes.map((r) => [r.id, runtimeDisplayName(r)])),
    [runtimes],
  );
  const seriesLabel = useCallback(
    (s: LimitHistorySeries) => {
      switch (s.kind) {
        case "session":
          return t(($) => $.rate_limits.short_session);
        case "weekly":
          return t(($) => $.rate_limits.short_weekly);
        case "weekly_opus":
          return t(($) => $.rate_limits.short_weekly_opus);
        case "weekly_sonnet":
          return t(($) => $.rate_limits.short_weekly_sonnet);
        default:
          return t(($) => $.rate_limits.short_generic);
      }
    },
    [t],
  );

  const accounts = data?.accounts ?? [];
  const intervalMinutes = Math.max(1, Math.round((data?.interval_seconds ?? 300) / 60));
  const from = now - days * 24 * 60 * 60 * 1000;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <CollectionPageHeader
        icon={Gauge}
        title="구독 한도"
        tools={<Segmented value={days} onChange={setDays} options={DAY_OPTIONS} label="추이 기간" />}
        actions={
          <IconButton
            label="새로고침"
            tooltipSide="bottom"
            className="text-muted-foreground"
            onClick={() => void refetch()}
            disabled={isFetching}
          >
            <RefreshCw className={isFetching ? "animate-spin" : undefined} />
          </IconButton>
        }
      />
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-[1440px] space-y-4 px-[var(--page-padding-x,var(--space-md,1rem))] py-4">
          {isLoading ? (
            <div className="grid gap-6 xl:grid-cols-2">
              <Skeleton className="h-48 w-full rounded-lg" />
              <Skeleton className="h-48 w-full rounded-lg" />
            </div>
          ) : error ? (
            <ObjectSectionEmpty icon={Gauge} title="구독 한도를 불러오지 못했습니다" description={error.message} />
          ) : accounts.length === 0 ? (
            <ObjectSectionEmpty
              icon={Gauge}
              title="연결된 구독 계정이 없습니다"
              description="Mac Studio의 ~/.multica/usage/accounts.json에 이 워크스페이스와 계정을 적으면 표시됩니다."
            />
          ) : (
            <>
              <p className="text-caption text-muted-foreground">
                Mac Studio에서 에이전트가 쓰는 구독 계정을 {intervalMinutes}분마다 읽습니다. Codex 값은 그 계정으로
                Codex가 마지막으로 실행된 시점 기준입니다.
              </p>
              <div className="grid gap-6 xl:grid-cols-2">
                {accounts.map((account) => (
                  <AccountSection
                    key={account.id}
                    account={account}
                    runtimeNames={runtimeNames}
                    now={now}
                    from={from}
                    seriesLabel={seriesLabel}
                  />
                ))}
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
