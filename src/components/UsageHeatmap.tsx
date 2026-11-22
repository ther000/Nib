import { useMemo, useState } from "react";
import type { DailyUsageStat } from "../mygo";
import { formatCompactTokens, formatNumber } from "../lib/constants";

interface UsageHeatmapProps {
  daily: DailyUsageStat[];
}

interface HeatmapCell {
  date: string;
  calls: number;
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
  level: 0 | 1 | 2 | 3 | 4;
  future: boolean;
}

const TOTAL_WEEKS = 52;
const WEEKDAY_LABELS: Array<{ row: number; label: string }> = [
  { row: 0, label: "一" },
  { row: 2, label: "三" },
  { row: 4, label: "五" },
];

const LEVEL_CLASSES: Record<0 | 1 | 2 | 3 | 4, string> = {
  0: "bg-canvas border border-border-subtle/70",
  1: "bg-accent/22 border border-accent/20",
  2: "bg-accent/45 border border-accent/30",
  3: "bg-accent/72 border border-accent/45",
  4: "bg-accent border border-accent",
};

function toLocalDateKey(d: Date): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

function computeThresholds(values: number[]): [number, number, number] {
  if (values.length === 0) return [1, 2, 3];
  const sorted = [...values].sort((a, b) => a - b);
  const max = sorted[sorted.length - 1] ?? 1;
  if (sorted.length < 4) {
    return [max * 0.25, max * 0.5, max * 0.75];
  }
  const q1 = sorted[Math.floor((sorted.length - 1) * 0.25)] ?? max * 0.25;
  const q2 = sorted[Math.floor((sorted.length - 1) * 0.5)] ?? max * 0.5;
  const q3 = sorted[Math.floor((sorted.length - 1) * 0.75)] ?? max * 0.75;
  if (q1 === q3) {
    return [max * 0.25, max * 0.5, max * 0.75];
  }
  return [q1, q2, q3];
}

function getLevel(tokens: number, thresholds: [number, number, number]): 0 | 1 | 2 | 3 | 4 {
  if (tokens <= 0) return 0;
  const [t1, t2, t3] = thresholds;
  if (tokens <= t1) return 1;
  if (tokens <= t2) return 2;
  if (tokens <= t3) return 3;
  return 4;
}

export function UsageHeatmap({ daily }: UsageHeatmapProps) {
  const [hovered, setHovered] = useState<HeatmapCell | null>(null);

  const { weeks, monthLabels, activeDays, peakTokens } = useMemo(() => {
    const map = new Map<string, DailyUsageStat>();
    for (const item of daily) {
      map.set(item.date, item);
    }

    const now = new Date();
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const todayKey = toLocalDateKey(today);

    // 将今天对齐到所在周的周一（周一=0 ... 周日=6），再回退 51 周构成 52 列
    const todayDow = (today.getDay() + 6) % 7;
    const thisWeekMonday = new Date(today);
    thisWeekMonday.setDate(today.getDate() - todayDow);

    const startMonday = new Date(thisWeekMonday);
    startMonday.setDate(thisWeekMonday.getDate() - (TOTAL_WEEKS - 1) * 7);

    // 仅统计落在 52 周视口内的活跃数据，保证色阶分档与汇总一致
    const inRangeTokens: number[] = [];
    let peak = 0;
    const cursor = new Date(startMonday);
    for (let i = 0; i < TOTAL_WEEKS * 7; i++) {
      const key = toLocalDateKey(cursor);
      if (key <= todayKey) {
        const stat = map.get(key);
        if (stat && stat.totalTokens > 0) {
          inRangeTokens.push(stat.totalTokens);
          if (stat.totalTokens > peak) peak = stat.totalTokens;
        }
      }
      cursor.setDate(cursor.getDate() + 1);
    }

    const thresholds = computeThresholds(inRangeTokens);

    const builtWeeks: HeatmapCell[][] = [];
    const labels: Array<{ weekIndex: number; label: string }> = [];
    let lastLabeledMonth = -1;
    let lastLabelCol = -10;

    const iter = new Date(startMonday);
    for (let w = 0; w < TOTAL_WEEKS; w++) {
      const col: HeatmapCell[] = [];
      let monthForCol: number | null = null;

      for (let d = 0; d < 7; d++) {
        const key = toLocalDateKey(iter);
        const isFuture = key > todayKey;
        const stat = isFuture ? undefined : map.get(key);
        const totalTokens = stat?.totalTokens ?? 0;

        if (!isFuture && (iter.getDate() === 1 || (w === 0 && d === 0))) {
          monthForCol = iter.getMonth() + 1;
        }

        col.push({
          date: key,
          calls: stat?.calls ?? 0,
          promptTokens: stat?.promptTokens ?? 0,
          completionTokens: stat?.completionTokens ?? 0,
          totalTokens,
          level: isFuture ? 0 : getLevel(totalTokens, thresholds),
          future: isFuture,
        });

        iter.setDate(iter.getDate() + 1);
      }

      if (monthForCol !== null && monthForCol !== lastLabeledMonth && w - lastLabelCol >= 3) {
        labels.push({ weekIndex: w, label: `${monthForCol}月` });
        lastLabeledMonth = monthForCol;
        lastLabelCol = w;
      }

      builtWeeks.push(col);
    }

    return {
      weeks: builtWeeks,
      monthLabels: labels,
      activeDays: inRangeTokens.length,
      peakTokens: peak,
    };
  }, [daily]);

  return (
    <div className="rounded-xl border border-border-subtle bg-surface p-4">
      {/* Top Status & Legend Row */}
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2 text-xs">
        <div className="min-h-4 text-ink-secondary">
          {hovered ? (
            <span className="nib-mono text-ink-primary">
              {hovered.date}
              {hovered.calls > 0 ? (
                <>
                  {" · "}
                  <span className="text-accent">{formatCompactTokens(hovered.totalTokens)}</span>
                  {" Token"}
                  <span className="text-ink-muted">
                    {" "}
                    (输入 {formatCompactTokens(hovered.promptTokens)} / 输出{" "}
                    {formatCompactTokens(hovered.completionTokens)} · {formatNumber(hovered.calls)} 次)
                  </span>
                </>
              ) : (
                <span className="text-ink-muted"> · 无调用</span>
              )}
            </span>
          ) : activeDays > 0 ? (
            <span>
              近一年活跃 <span className="nib-mono font-medium text-ink-primary">{activeDays}</span> 天
              {" · "}单日峰值{" "}
              <span className="nib-mono font-medium text-ink-primary">
                {formatCompactTokens(peakTokens)}
              </span>{" "}
              Token
            </span>
          ) : (
            <span className="text-ink-muted">近一年暂无调用记录</span>
          )}
        </div>

        <div className="flex items-center gap-1 text-[11px] text-ink-muted select-none">
          <span className="mr-0.5">少</span>
          {([0, 1, 2, 3, 4] as const).map((lvl) => (
            <span
              key={lvl}
              className={`h-2.5 w-2.5 rounded-[2px] ${LEVEL_CLASSES[lvl]}`}
            />
          ))}
          <span className="ml-0.5">多</span>
        </div>
      </div>

      {/* Month Labels + 7x52 Grid */}
      <div className="flex gap-2">
        {/* Weekday Labels */}
        <div className="flex shrink-0 flex-col pt-4 text-[10px] leading-none text-ink-muted select-none">
          <div className="grid flex-1 grid-rows-7 gap-[3px]">
            {Array.from({ length: 7 }).map((_, rowIdx) => {
              const found = WEEKDAY_LABELS.find((item) => item.row === rowIdx);
              return (
                <span key={rowIdx} className="flex items-center justify-end pr-0.5">
                  {found?.label ?? ""}
                </span>
              );
            })}
          </div>
        </div>

        {/* Columns */}
        <div className="min-w-0 flex-1">
          {/* Month Header */}
          <div className="relative mb-1 h-3 text-[10px] leading-none text-ink-muted select-none">
            {monthLabels.map((m) => (
              <span
                key={`${m.weekIndex}-${m.label}`}
                className="absolute top-0 whitespace-nowrap"
                style={{ left: `${(m.weekIndex / TOTAL_WEEKS) * 100}%` }}
              >
                {m.label}
              </span>
            ))}
          </div>

          {/* 52 Weeks x 7 Days */}
          <div
            className="grid gap-[3px]"
            style={{ gridTemplateColumns: `repeat(${TOTAL_WEEKS}, minmax(0, 1fr))` }}
            onMouseLeave={() => setHovered(null)}
          >
            {weeks.map((week, wIdx) => (
              <div key={wIdx} className="grid grid-rows-7 gap-[3px]">
                {week.map((cell) =>
                  cell.future ? (
                    <div key={cell.date} className="aspect-square w-full" aria-hidden="true" />
                  ) : (
                    <div
                      key={cell.date}
                      onMouseEnter={() => setHovered(cell)}
                      title={
                        cell.calls > 0
                          ? `${cell.date}：${formatNumber(cell.totalTokens)} Token（${formatNumber(cell.calls)} 次调用）`
                          : `${cell.date}：无调用`
                      }
                      className={`aspect-square w-full rounded-[2px] transition-all hover:ring-1 hover:ring-ink-primary/40 ${LEVEL_CLASSES[cell.level]}`}
                    />
                  )
                )}
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
