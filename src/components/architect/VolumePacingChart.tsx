import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AlertTriangle, Flag, RefreshCw, TrendingUp } from "lucide-react";
import {
  LibraryService,
  type Volume,
  type VolumePacingReport,
} from "../../mygo";
import {
  formatSeq,
  getEmotionMeta,
  getHookMeta,
} from "../../lib/constants";

interface VolumePacingChartProps {
  volume: Volume;
  activeChapterId: number | null;
  refreshTrigger: number;
  onSelectChapter: (chapterId: number) => void;
  onOpenForeshadowings: () => void;
}

const Y_LABELS = [
  { weight: 4, label: "爆发" },
  { weight: 3, label: "收割" },
  { weight: 2, label: "反转" },
  { weight: 1, label: "铺垫" },
  { weight: 0, label: "蓄压" },
];

function nodeFill(tag: string): string {
  switch (tag) {
    case "burst":
      return "var(--accent)";
    case "turn":
      return "var(--status-ok)";
    case "suppress":
      return "var(--status-warn)";
    case "harvest":
      return "var(--text-primary)";
    default:
      return "var(--text-secondary)";
  }
}

export function VolumePacingChart({
  volume,
  activeChapterId,
  refreshTrigger,
  onSelectChapter,
  onOpenForeshadowings,
}: VolumePacingChartProps) {
  const [report, setReport] = useState<VolumePacingReport | null>(null);
  const [loading, setLoading] = useState(false);
  const reqIdRef = useRef(0);

  const loadPacing = useCallback(async () => {
    const reqId = ++reqIdRef.current;
    setLoading(true);
    try {
      const res = await LibraryService.getVolumePacing(volume.id);
      if (reqIdRef.current === reqId) {
        setReport(res);
      }
    } catch {
      if (reqIdRef.current === reqId) {
        setReport(null);
      }
    } finally {
      if (reqIdRef.current === reqId) {
        setLoading(false);
      }
    }
  }, [volume.id]);

  useEffect(() => {
    void loadPacing();
  }, [loadPacing, refreshTrigger]);

  const points = report?.points ?? [];
  const alerts = report?.alerts ?? [];

  const chart = useMemo(() => {
    const padLeft = 38;
    const padRight = 22;
    const padTop = 14;
    const padBottom = 24;
    const height = 112;
    const stepX = 56;
    const width = Math.max(
      360,
      padLeft + padRight + Math.max(points.length - 1, 1) * stepX
    );
    const plotHeight = height - padTop - padBottom;
    const yForWeight = (w: number) => {
      const clamped = Math.max(0, Math.min(4, w));
      return padTop + ((4 - clamped) / 4) * plotHeight;
    };

    const coords = points.map((pt, idx) => {
      const x =
        points.length === 1
          ? padLeft + (width - padLeft - padRight) / 2
          : padLeft + idx * stepX;
      const y = yForWeight(pt.weight);
      return { pt, x, y };
    });

    const polylinePoints = coords.map((c) => `${c.x},${c.y}`).join(" ");
    return { width, height, padLeft, padRight, yForWeight, coords, polylinePoints };
  }, [points]);

  return (
    <section className="flex flex-col gap-2.5 rounded-xl border border-border-subtle bg-surface/75 px-4 py-3">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2 text-xs">
          <TrendingUp className="h-3.5 w-3.5 shrink-0 text-accent" />
          <span className="truncate font-medium text-ink-primary">
            {volume.title} · 情绪节奏曲线
          </span>
          {volume.goal && (
            <span className="hidden truncate text-[11px] text-ink-muted md:inline">
              · {volume.goal}
            </span>
          )}
        </div>

        <button
          type="button"
          onClick={() => void loadPacing()}
          title="刷新节奏诊断"
          className="rounded p-1 text-ink-muted transition-colors hover:bg-elevated hover:text-ink-primary"
        >
          <RefreshCw
            className={`h-3 w-3 ${loading ? "animate-spin text-accent" : ""}`}
          />
        </button>
      </div>

      {/* Diagnostic Alert Pills */}
      {alerts.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          {alerts.map((al, idx) => {
            const isForeshadow = al.kind === "overdue_foreshadowing";
            const isDanger = al.severity === "error" || al.kind === "suppress_overload";
            return (
              <button
                key={`${al.kind}-${idx}`}
                type="button"
                onClick={() => {
                  if (isForeshadow) {
                    onOpenForeshadowings();
                  } else if (al.chapterIds?.[0]) {
                    onSelectChapter(al.chapterIds[0]);
                  }
                }}
                className={`inline-flex items-center gap-1 rounded-full border px-2.5 py-0.5 text-[11px] font-medium transition-opacity hover:opacity-85 ${
                  isDanger
                    ? "border-danger/35 bg-danger-soft text-danger"
                    : "border-warn/35 bg-warn-soft text-warn"
                }`}
              >
                {isForeshadow ? (
                  <Flag className="h-3 w-3 shrink-0" />
                ) : (
                  <AlertTriangle className="h-3 w-3 shrink-0" />
                )}
                <span>{al.message}</span>
              </button>
            );
          })}
        </div>
      )}

      {/* Interactive SVG Line Chart */}
      {points.length > 0 ? (
        <div className="overflow-x-auto pb-0.5">
          <svg
            width={chart.width}
            height={chart.height}
            viewBox={`0 0 ${chart.width} ${chart.height}`}
            className="block select-none"
          >
            {/* Horizontal grid lines & Y axis labels */}
            {Y_LABELS.map((item) => {
              const y = chart.yForWeight(item.weight);
              return (
                <g key={item.weight}>
                  <line
                    x1={chart.padLeft}
                    y1={y}
                    x2={chart.width - chart.padRight}
                    y2={y}
                    stroke="var(--border-subtle)"
                    strokeDasharray={item.weight === 4 ? undefined : "2 3"}
                  />
                  <text
                    x={chart.padLeft - 6}
                    y={y + 3}
                    textAnchor="end"
                    fill="var(--text-muted)"
                    fontSize={9.5}
                  >
                    {item.label}
                  </text>
                </g>
              );
            })}

            {/* Connecting curve polyline */}
            {chart.coords.length > 1 && (
              <polyline
                fill="none"
                stroke="var(--accent)"
                strokeOpacity={0.55}
                strokeWidth={1.75}
                strokeLinejoin="round"
                strokeLinecap="round"
                points={chart.polylinePoints}
              />
            )}

            {/* Clickable chapter nodes */}
            {chart.coords.map(({ pt, x, y }) => {
              const selected = pt.chapterId === activeChapterId;
              const em = getEmotionMeta(pt.emotionTag);
              const hk = getHookMeta(pt.hookType);
              return (
                <g
                  key={pt.chapterId}
                  role="button"
                  tabIndex={0}
                  onClick={() => onSelectChapter(pt.chapterId)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      onSelectChapter(pt.chapterId);
                    }
                  }}
                  className="cursor-pointer focus:outline-none"
                >
                  <title>
                    {`第${pt.globalSeq}章 ${pt.title} · ${em.label} · ${hk.label}`}
                  </title>
                  {selected && (
                    <circle
                      cx={x}
                      cy={y}
                      r={8.5}
                      fill="var(--accent-soft)"
                      stroke="var(--accent)"
                      strokeWidth={1.2}
                    />
                  )}
                  <circle
                    cx={x}
                    cy={y}
                    r={selected ? 4.5 : 3.8}
                    fill={nodeFill(pt.emotionTag)}
                    stroke="var(--bg-surface)"
                    strokeWidth={1.5}
                  />
                  <text
                    x={x}
                    y={chart.height - 6}
                    textAnchor="middle"
                    fill={selected ? "var(--accent)" : "var(--text-muted)"}
                    fontSize={10}
                    fontWeight={selected ? 600 : 400}
                    className="nib-mono"
                  >
                    {formatSeq(pt.globalSeq)}
                  </text>
                </g>
              );
            })}
          </svg>
        </div>
      ) : (
        <div className="py-2 text-xs text-ink-muted">
          本卷尚无章节，可手动新建或使用右侧「批量生成章纲」。
        </div>
      )}
    </section>
  );
}
