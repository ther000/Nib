import { ArrowRight, Save } from "lucide-react";
import type { Chapter } from "../../mygo";
import {
  EMOTION_TAGS,
  HOOK_TYPES,
  formatSeq,
  getChapterStatusMeta,
} from "../../lib/constants";

interface ChapterMetaFormProps {
  chapter: Chapter;
  title: string;
  emotion: string;
  hookType: string;
  hookNote: string;
  storyTimeLabel: string;
  storyDay: string;
  outline: string;
  dirty: boolean;
  saving: boolean;
  onChangeTitle: (value: string) => void;
  onChangeEmotion: (value: string) => void;
  onChangeHookType: (value: string) => void;
  onChangeHookNote: (value: string) => void;
  onChangeStoryTimeLabel: (value: string) => void;
  onChangeStoryDay: (value: string) => void;
  onChangeOutline: (value: string) => void;
  onSaveAll: () => void;
  onStartWriting: () => void;
}

export function ChapterMetaForm({
  chapter,
  title,
  emotion,
  hookType,
  hookNote,
  storyTimeLabel,
  storyDay,
  outline,
  dirty,
  saving,
  onChangeTitle,
  onChangeEmotion,
  onChangeHookType,
  onChangeHookNote,
  onChangeStoryTimeLabel,
  onChangeStoryDay,
  onChangeOutline,
  onSaveAll,
  onStartWriting,
}: ChapterMetaFormProps) {
  const statusMeta = getChapterStatusMeta(chapter.status);

  return (
    <section className="rounded-xl border border-border-subtle bg-surface p-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 flex-1 items-center gap-2.5">
          <span className="nib-mono shrink-0 rounded bg-elevated px-2 py-0.5 text-xs text-ink-secondary">
            第 {formatSeq(chapter.globalSeq)} 章
          </span>
          <input
            type="text"
            value={title}
            onChange={(e) => onChangeTitle(e.target.value)}
            placeholder="输入章节标题"
            className="min-w-0 flex-1 bg-transparent text-base font-medium text-ink-primary placeholder:text-ink-muted focus:outline-none"
          />
        </div>

        <div className="flex shrink-0 items-center gap-2">
          <span
            className={`rounded-full border px-2 py-0.5 text-xs ${statusMeta.badgeClass}`}
          >
            {statusMeta.label}
          </span>
          <button
            type="button"
            disabled={saving || !dirty}
            onClick={onSaveAll}
            title="保存大纲 (Ctrl+S)"
            className="flex items-center gap-1 rounded-lg border border-border-subtle bg-elevated px-2.5 py-1 text-xs font-medium text-ink-primary transition-colors hover:border-border-strong disabled:opacity-45"
          >
            <Save className="h-3.5 w-3.5 text-accent" />
            <span>{saving ? "保存中…" : "保存"}</span>
          </button>
          <button
            type="button"
            onClick={onStartWriting}
            className="flex items-center gap-1 rounded-lg bg-accent px-3 py-1 text-xs font-semibold text-canvas transition-opacity hover:opacity-90"
          >
            <span>开始写作</span>
            <ArrowRight className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      {/* Emotion, Hook Type, Hook Note */}
      <div className="mt-4 grid grid-cols-1 gap-3 md:grid-cols-[170px_170px_1fr]">
        <div>
          <label className="mb-1 block text-xs font-medium text-ink-secondary">
            本章节奏
          </label>
          <select
            value={emotion}
            onChange={(e) => onChangeEmotion(e.target.value)}
            className="w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
          >
            {EMOTION_TAGS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
        </div>

        <div>
          <label className="mb-1 block text-xs font-medium text-ink-secondary">
            章末悬念
          </label>
          <select
            value={hookType}
            onChange={(e) => onChangeHookType(e.target.value)}
            className="w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
          >
            {HOOK_TYPES.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
        </div>

        <div>
          <label className="mb-1 block text-xs font-medium text-ink-secondary">
            结尾留白说明
          </label>
          <input
            type="text"
            value={hookNote}
            onChange={(e) => onChangeHookNote(e.target.value)}
            placeholder="例如：刚收完战利品，宗门追兵的血玉令牌亮起"
            className="w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
          />
        </div>
      </div>

      {/* Story Timeline */}
      <div className="mt-3 grid grid-cols-1 gap-3 md:grid-cols-[1fr_140px]">
        <div>
          <label className="mb-1 block text-xs font-medium text-ink-secondary">
            故事内时间
          </label>
          <input
            type="text"
            value={storyTimeLabel}
            onChange={(e) => onChangeStoryTimeLabel(e.target.value)}
            placeholder="例如：天元三年秋 · 入宗第十日"
            className="w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
          />
        </div>

        <div>
          <label className="mb-1 block text-xs font-medium text-ink-secondary">
            故事日序号
          </label>
          <input
            type="number"
            value={storyDay}
            onChange={(e) => onChangeStoryDay(e.target.value)}
            placeholder="如 10，用于倒计时"
            className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
          />
        </div>
      </div>

      {/* Outline Summary */}
      <div className="mt-3">
        <label className="mb-1 block text-xs font-medium text-ink-secondary">
          本章梗概
        </label>
        <textarea
          rows={2}
          value={outline}
          onChange={(e) => onChangeOutline(e.target.value)}
          placeholder="本章主要剧情推进与结尾结果"
          className="w-full resize-y rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
        />
      </div>
    </section>
  );
}
