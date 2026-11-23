import { useState, type KeyboardEvent } from "react";
import { ArrowLeft, ArrowRight, Loader2, RefreshCw, Sparkles, Square } from "lucide-react";
import type { BookConcept } from "../../mygo";
import { getPovLabel } from "./GenesisShared";

interface ConceptStepProps {
  concept: BookConcept;
  genreName: string;
  chapterCount: number;
  targetWords: string;
  busy: boolean;
  busyKind: "refine" | "blueprint" | null;
  streamChars: number;
  onChange: (next: BookConcept) => void;
  onBack: () => void;
  onRefine: (feedback: string) => void;
  onProceed: () => void;
  onStop: () => void;
}

export function ConceptStep({
  concept,
  genreName,
  chapterCount,
  targetWords,
  busy,
  busyKind,
  streamChars,
  onChange,
  onBack,
  onRefine,
  onProceed,
  onStop,
}: ConceptStepProps) {
  const [feedback, setFeedback] = useState("");

  const handleRefineSubmit = () => {
    const trimmed = feedback.trim();
    if (!trimmed || busy) return;
    onRefine(trimmed);
    setFeedback("");
  };

  const handleFeedbackKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleRefineSubmit();
    }
  };

  return (
    <div className="flex flex-col gap-6">
      {/* Meta strip */}
      <div className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-border-subtle bg-surface px-4 py-2.5 text-xs text-ink-secondary">
        <div className="flex flex-wrap items-center gap-3">
          <span>题材：{genreName}</span>
          {concept.pacingStyle && (
            <>
              <span className="text-border-strong">·</span>
              <span>风格：{concept.pacingStyle}</span>
            </>
          )}
          <span className="text-border-strong">·</span>
          <span>视角：{getPovLabel(concept.povMode ?? "")}</span>
          <span className="text-border-strong">·</span>
          <span className="nib-mono">
            {targetWords} 字/章 · {chapterCount > 0 ? `生成前 ${chapterCount} 章` : "暂不生成章纲"}
          </span>
        </div>
        <button
          type="button"
          disabled={busy}
          onClick={onBack}
          className="text-ink-muted transition-colors hover:text-ink-primary disabled:opacity-40"
        >
          修改规格
        </button>
      </div>

      {/* Title + Candidates */}
      <section className="flex flex-col gap-2.5">
        <div className="flex items-center justify-between gap-2">
          <label className="text-xs font-medium text-ink-secondary">书名</label>
          {concept.titleCandidates.length > 1 && (
            <div className="flex flex-wrap items-center gap-1.5">
              {concept.titleCandidates.map((cand) => {
                const active = concept.title.trim() === cand;
                return (
                  <button
                    key={cand}
                    type="button"
                    disabled={busy}
                    onClick={() => onChange({ ...concept, title: cand })}
                    className={`rounded-md border px-2 py-0.5 text-xs transition-colors disabled:opacity-50 ${
                      active
                        ? "border-accent bg-accent-soft font-medium text-accent"
                        : "border-border-subtle bg-surface text-ink-secondary hover:border-border-strong hover:text-ink-primary"
                    }`}
                  >
                    {cand}
                  </button>
                );
              })}
            </div>
          )}
        </div>
        <input
          type="text"
          disabled={busy}
          value={concept.title}
          onChange={(e) => onChange({ ...concept, title: e.target.value })}
          placeholder="输入书名"
          className="w-full rounded-xl border border-border-subtle bg-surface px-4 py-3 font-prose text-lg font-medium text-ink-primary placeholder:text-ink-muted focus:border-accent disabled:opacity-60"
        />
      </section>

      {/* Premise */}
      <section className="flex flex-col gap-2">
        <label className="text-xs font-medium text-ink-secondary">故事简介</label>
        <textarea
          rows={3}
          disabled={busy}
          value={concept.premise}
          onChange={(e) => onChange({ ...concept, premise: e.target.value })}
          className="w-full resize-none rounded-xl border border-border-subtle bg-surface px-4 py-3 text-[13px] leading-relaxed text-ink-primary focus:border-accent disabled:opacity-60"
        />
      </section>

      {/* Golden Finger & Core Conflict */}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <section className="flex flex-col gap-2">
          <label className="text-xs font-medium text-ink-secondary">
            核心设定 / 金手指
          </label>
          <textarea
            rows={3}
            disabled={busy}
            value={concept.goldenFinger}
            onChange={(e) => onChange({ ...concept, goldenFinger: e.target.value })}
            className="w-full resize-none rounded-xl border border-border-subtle bg-surface px-4 py-3 text-[13px] leading-relaxed text-ink-primary focus:border-accent disabled:opacity-60"
          />
        </section>

        <section className="flex flex-col gap-2">
          <label className="text-xs font-medium text-ink-secondary">
            初期核心冲突
          </label>
          <textarea
            rows={3}
            disabled={busy}
            value={concept.coreConflict}
            onChange={(e) => onChange({ ...concept, coreConflict: e.target.value })}
            placeholder="主角开局面对的压迫与破局方向"
            className="w-full resize-none rounded-xl border border-border-subtle bg-surface px-4 py-3 text-[13px] leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent disabled:opacity-60"
          />
        </section>
      </div>

      {/* Inline AI Refine Bar */}
      <section className="rounded-xl border border-border-subtle bg-surface/70 p-3">
        <div className="flex items-center gap-2">
          <Sparkles className="h-4 w-4 shrink-0 text-accent" />
          <input
            type="text"
            disabled={busy}
            value={feedback}
            onChange={(e) => setFeedback(e.target.value)}
            onKeyDown={handleFeedbackKeyDown}
            placeholder="不满意？一句话让 AI 微调方向，如：主角更狠一点 / 换成苟道流 / 换个更有记忆点的书名"
            className="min-w-0 flex-1 bg-transparent text-[13px] text-ink-primary placeholder:text-ink-muted focus:outline-none disabled:opacity-60"
          />
          <button
            type="button"
            disabled={busy || !feedback.trim()}
            onClick={handleRefineSubmit}
            className="flex shrink-0 items-center gap-1.5 rounded-lg border border-border-strong bg-elevated px-3 py-1.5 text-xs font-medium text-ink-primary transition-colors hover:border-accent/50 disabled:opacity-40"
          >
            <RefreshCw className="h-3.5 w-3.5" />
            <span>微调立意</span>
          </button>
        </div>
      </section>

      {/* Streaming progress banner */}
      {busy && (
        <div className="flex items-center justify-between rounded-xl border border-border-subtle bg-surface px-4 py-3">
          <div className="flex items-center gap-2.5 text-xs text-ink-primary">
            <Loader2 className="h-4 w-4 animate-spin text-accent" />
            <span>
              {busyKind === "refine"
                ? "正在按你的意见微调核心立意…"
                : chapterCount > 0
                  ? `正在基于已拍板立意构建角色设定、第一卷与前 ${chapterCount} 章大纲…`
                  : "正在基于已拍板立意构建角色设定与第一卷规划…"}
            </span>
          </div>
          {streamChars > 0 && (
            <span className="nib-mono text-xs text-ink-muted">
              已生成 {streamChars} 字
            </span>
          )}
        </div>
      )}

      {/* Footer Actions */}
      <div className="flex items-center justify-between border-t border-border-subtle pt-4">
        <button
          type="button"
          disabled={busy}
          onClick={onBack}
          className="flex items-center gap-1.5 rounded-lg px-3 py-2 text-[13px] font-medium text-ink-secondary transition-colors hover:bg-surface hover:text-ink-primary disabled:opacity-40"
        >
          <ArrowLeft className="h-3.5 w-3.5" />
          <span>上一步</span>
        </button>

        {busy ? (
          <button
            type="button"
            onClick={onStop}
            className="flex items-center gap-1.5 rounded-lg border border-border-strong bg-surface px-4 py-2 text-[13px] font-medium text-ink-primary transition-colors hover:bg-elevated"
          >
            <Square className="h-3.5 w-3.5" />
            <span>停止生成</span>
          </button>
        ) : (
          <button
            type="button"
            onClick={onProceed}
            className="flex items-center gap-1.5 rounded-lg bg-accent px-5 py-2 text-[13px] font-semibold text-canvas transition-opacity hover:opacity-90"
          >
            <span>下一步：生成设定与开篇蓝图</span>
            <ArrowRight className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
    </div>
  );
}
