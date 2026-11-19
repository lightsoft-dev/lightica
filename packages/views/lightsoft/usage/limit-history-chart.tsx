"use client";

import { useMemo } from "react";
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from "recharts";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@multica/ui/components/ui/chart";
import type { LimitHistorySeries } from "@multica/core/lightsoft/usage";
import { useLocale } from "../../i18n";

const SERIES_COLORS = ["var(--chart-1)", "var(--chart-2)", "var(--chart-4)", "var(--chart-3)", "var(--chart-5)"];

interface Row {
  t: number;
  [series: string]: number;
}

/**
 * Share used of each window over the chosen period, one point per hour.
 * Codex only reports while it runs, so its line has gaps; points are drawn
 * when they are sparse so a lone observation still shows.
 */
export function LimitHistoryChart({
  series,
  labelFor,
  from,
  to,
}: {
  series: LimitHistorySeries[];
  labelFor: (s: LimitHistorySeries) => string;
  from: number;
  to: number;
}) {
  const locale = useLocale();
  const { rows, config, keys, sparse } = useMemo(() => {
    const byTime = new Map<number, Row>();
    const config: ChartConfig = {};
    const keys: string[] = [];
    let points = 0;
    series.forEach((s, index) => {
      const key = `s${index}`;
      keys.push(key);
      config[key] = { label: labelFor(s), color: SERIES_COLORS[index % SERIES_COLORS.length] };
      for (const p of s.points) {
        const t = Date.parse(p.t);
        if (!Number.isFinite(t)) continue;
        const row = byTime.get(t) ?? { t };
        row[key] = Math.round(p.used_percent * 10) / 10;
        byTime.set(t, row);
        points++;
      }
    });
    const rows = [...byTime.values()].sort((a, b) => a.t - b.t);
    return { rows, config, keys, sparse: points / Math.max(1, series.length) < 48 };
  }, [series, labelFor]);

  // Day ticks across the whole period: with sparse data recharts would only
  // label the days that happen to have a point.
  const ticks = useMemo(() => {
    const day = 24 * 60 * 60 * 1000;
    const step = to - from > 10 * day ? 5 : 1;
    const first = new Date(from);
    first.setHours(0, 0, 0, 0);
    const out: number[] = [];
    for (const d = new Date(first.getTime() + day); d.getTime() <= to; d.setDate(d.getDate() + step)) {
      out.push(d.getTime());
    }
    return out;
  }, [from, to]);

  const dayFormat = useMemo(
    () => new Intl.DateTimeFormat(locale, { month: "numeric", day: "numeric" }),
    [locale],
  );
  const pointFormat = useMemo(
    () =>
      new Intl.DateTimeFormat(locale, {
        month: "numeric",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        hourCycle: "h23",
      }),
    [locale],
  );

  return (
    <ChartContainer config={config} className="aspect-[5/1] min-h-28 w-full">
      <LineChart data={rows} margin={{ left: 0, right: 8, top: 4, bottom: 0 }}>
        <CartesianGrid vertical={false} />
        <XAxis
          dataKey="t"
          type="number"
          scale="time"
          domain={[from, to]}
          ticks={ticks}
          tickLine={false}
          axisLine={false}
          tickMargin={8}
          tickFormatter={(v: number) => dayFormat.format(new Date(v))}
        />
        <YAxis
          domain={[0, 100]}
          ticks={[0, 50, 100]}
          tickLine={false}
          axisLine={false}
          tickMargin={8}
          width={44}
          tickFormatter={(v: number) => `${v}%`}
        />
        <ChartTooltip
          content={
            <ChartTooltipContent
              labelFormatter={(_, payload) => {
                const t = payload?.[0]?.payload?.t;
                return typeof t === "number" ? pointFormat.format(new Date(t)) : "";
              }}
              formatter={(value, name) => `${config[String(name)]?.label ?? name} ${value}%`}
            />
          }
        />
        {keys.map((key) => (
          <Line
            key={key}
            dataKey={key}
            type="monotone"
            stroke={`var(--color-${key})`}
            strokeWidth={2}
            dot={sparse ? { r: 2.5 } : false}
            connectNulls
            isAnimationActive={false}
          />
        ))}
      </LineChart>
    </ChartContainer>
  );
}
