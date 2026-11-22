import { useMemo } from "react";
import { Clock, History } from "lucide-react";
import type { Chapter } from "../../mygo";
import { formatSeq } from "../../lib/constants";

interface AtlasTimelineBarProps {
  timelineSeq: number;
  latestChapterSeq: number;
  chapters: Chapter[];
  includePending: boolean;
  onChangeTimelineSeq: (seq: number, isManual: boolean) => void;
  onChangeIncludePending: (next: boolean) => void;
  compact?: boolean;
  labelPrefix?: string;
}

export function AtlasTimelineBar({
  timelineSeq,
  latestChapterSeq,
  chapters,
  includePending,
  onChangeTimelineSeq,
  onChangeIncludePending,
  compact = false,
  labelPrefix = "世界状态",
}: AtlasTimelineBarProps) {
  const storyTimeText = useMemo(() => {
    if (timelineSeq <= 0 || chapters.length === 0) return "";
    // 优先取当前滑块章的故事内时间标签，若当前章未填则取截至当前章最近一个已标注时间标签
    const candidates = chapters
      .filter((c) => c.globalSeq > 0 && c.globalSeq <= timelineSeq)
      .sort((a, b) => b.globalSeq - a.globalSeq);
    for (const ch of candidates) {
      const label = ch.storyTimeLabel?.trim();
      if (label) {
        return ch.globalSeq === timelineSeq
          ? label
          : `${label}（第 ${formatSeq(ch.globalSeq)} 章）`;
      }
      if (ch.storyDay !== undefined && ch.storyDay !== null) {
        return `故事第 ${ch.storyDay} 日`;
      }
    }
    return "";
  }, [chapters, timelineSeq]);

  return (
    <div
      className={`flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border-subtle bg-surface ${
        compact ? "px-3.5 py-2.5" : "px-4 py-3"
      }`}
    >
      <div className="flex min-w-[240px] flex-1 flex-wrap items-center gap-3">
        <div className="flex shrink-0 items-center gap-1.5 text-xs">
          <History className="h-3.5 w-3.5 text-accent" />
          <span className="font-medium text-ink-primary">
            {timelineSeq === 0
              ? `截至第 0 章（初始设定）`
              : `截至第 ${formatSeq(timelineSeq)} 章${labelPrefix}`}
          </span>
        </div>

        {storyTimeText && (
          <span className="inline-flex shrink-0 items-center gap-1 rounded-md border border-border-subtle bg-canvas/80 px-2 py-0.5 text-[11px] text-ink-secondary">
            <Clock className="h-3 w-3 text-accent" />
            <span>{storyTimeText}</span>
          </span>
        )}

        <input
          type="range"
          min={0}
          max={latestChapterSeq}
          value={timelineSeq}
          disabled={latestChapterSeq === 0}
          onChange={(e) => onChangeTimelineSeq(Number(e.target.value), true)}
          className="h-1.5 min-w-[120px] flex-1 cursor-pointer accent-[var(--accent)] disabled:opacity-35"
        />

        <span className="nib-mono shrink-0 text-[11px] text-ink-muted">
          {formatSeq(timelineSeq)} / {formatSeq(latestChapterSeq)}
        </span>

        {latestChapterSeq > 0 && (
          <div className="flex shrink-0 items-center gap-1">
            <button
              type="button"
              onClick={() => onChangeTimelineSeq(0, true)}
              className={`rounded px-1.5 py-0.5 text-[10.5px] transition-colors ${
                timelineSeq === 0
                  ? "bg-accent-soft text-accent"
                  : "text-ink-muted hover:bg-elevated hover:text-ink-primary"
              }`}
            >
              初始
            </button>
            <button
              type="button"
              onClick={() => onChangeTimelineSeq(latestChapterSeq, false)}
              className={`rounded px-1.5 py-0.5 text-[10.5px] transition-colors ${
                timelineSeq === latestChapterSeq
                  ? "bg-accent-soft text-accent"
                  : "text-ink-muted hover:bg-elevated hover:text-ink-primary"
              }`}
            >
              最新
            </button>
          </div>
        )}
      </div>

      <label className="flex shrink-0 cursor-pointer items-center gap-1.5 text-xs text-ink-secondary select-none">
        <input
          type="checkbox"
          checked={includePending}
          onChange={(e) => onChangeIncludePending(e.target.checked)}
          className="h-3.5 w-3.5 accent-[var(--accent)]"
        />
        <span>含待审状态</span>
      </label>
    </div>
  );
}
