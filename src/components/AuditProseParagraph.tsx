import { memo, useEffect, useMemo, useState, type ReactNode } from "react";
import {
  AlertTriangle,
  BookMarked,
  Check,
  Pencil,
  ShieldAlert,
  Sparkles,
  Wand2,
  X,
} from "lucide-react";
import type {
  CriticFinding,
  EntitySnapshot,
  LintIssue,
  SurgicalDiff,
} from "../mygo";
import {
  getCriticDimensionMeta,
  getDeaiCategoryLabel,
  getEntityKindLabel,
  getLintSeverityMeta,
} from "../lib/constants";
import { resolveEntitySpans } from "../lib/studioHelpers";

interface AuditProseParagraphProps {
  paragraphIndex: number;
  text: string;
  isStreamingLast: boolean;
  isFocused: boolean;
  lintIssues: LintIssue[];
  criticFindings: CriticFinding[];
  pendingDiff: SurgicalDiff | null;
  polishing: boolean;
  applyingDiffId: string | null;
  entitySnapshots?: EntitySnapshot[];
  onFocus: (paragraphIndex: number) => void;
  onRequestPolish: (paragraphIndex: number) => void;
  onApplyDiff: (
    diffId: string,
    accept: boolean,
    customReplacement?: string
  ) => void;
  onQuickReplaceIssue?: (issue: LintIssue, replacement: string) => void;
  onInspectEntity?: (snapshot: EntitySnapshot) => void;
}

interface ResolvedSpan {
  start: number;
  end: number;
  issue: LintIssue;
}

/**
 * Resolves non-overlapping rune spans for inline underline rendering inside a paragraph.
 */
function resolveLintSpans(runes: string[], issues: LintIssue[]): {
  inlineSpans: ResolvedSpan[];
  paragraphLevelIssues: LintIssue[];
} {
  const rawSpans: ResolvedSpan[] = [];
  const paragraphLevelIssues: LintIssue[] = [];
  const fullText = runes.join("");

  for (const issue of issues) {
    let start = issue.startRune;
    let end = issue.endRune;

    if (
      start < 0 ||
      end <= start ||
      start >= runes.length ||
      end > runes.length
    ) {
      // Try fallback match by matchedText if available
      if (issue.matchedText) {
        const charIdx = fullText.indexOf(issue.matchedText);
        if (charIdx >= 0) {
          const prefixRunes = Array.from(fullText.slice(0, charIdx)).length;
          const matchRunes = Array.from(issue.matchedText).length;
          start = prefixRunes;
          end = Math.min(runes.length, prefixRunes + matchRunes);
        }
      }
    }

    if (start >= 0 && end > start && start < runes.length) {
      rawSpans.push({
        start,
        end: Math.min(runes.length, end),
        issue,
      });
    } else {
      paragraphLevelIssues.push(issue);
    }
  }

  // Sort by start ascending, then prefer error severity or longer span
  rawSpans.sort((a, b) => {
    if (a.start !== b.start) return a.start - b.start;
    if (a.issue.severity !== b.issue.severity) {
      return a.issue.severity === "error" ? -1 : 1;
    }
    return b.end - a.end;
  });

  const inlineSpans: ResolvedSpan[] = [];
  let cursor = 0;
  for (const sp of rawSpans) {
    if (sp.end <= cursor) continue;
    const clampedStart = Math.max(sp.start, cursor);
    if (clampedStart < sp.end) {
      inlineSpans.push({
        start: clampedStart,
        end: sp.end,
        issue: sp.issue,
      });
      cursor = sp.end;
    }
  }

  return { inlineSpans, paragraphLevelIssues };
}

export const AuditProseParagraph = memo(function AuditProseParagraph({
  paragraphIndex,
  text,
  isStreamingLast,
  isFocused,
  lintIssues,
  criticFindings,
  pendingDiff,
  polishing,
  applyingDiffId,
  entitySnapshots,
  onFocus,
  onRequestPolish,
  onApplyDiff,
  onQuickReplaceIssue,
  onInspectEntity,
}: AuditProseParagraphProps) {
  const [selectedIssue, setSelectedIssue] = useState<LintIssue | null>(null);
  const [selectedEntity, setSelectedEntity] = useState<EntitySnapshot | null>(null);
  const [inlineSensitiveRep, setInlineSensitiveRep] = useState("");
  const [editingDiff, setEditingDiff] = useState(false);
  const [customReplacement, setCustomReplacement] = useState("");

  useEffect(() => {
    setCustomReplacement(pendingDiff?.replacement ?? "");
    setEditingDiff(false);
  }, [pendingDiff?.id, pendingDiff?.replacement]);

  useEffect(() => {
    setInlineSensitiveRep(selectedIssue?.replacement ?? "");
  }, [selectedIssue]);

  useEffect(() => {
    if (!isFocused) {
      setSelectedIssue(null);
      setSelectedEntity(null);
    }
  }, [isFocused]);

  // Tab (Accept) / Esc (Ignore) keyboard shortcuts for focused paragraph's pending diff,
  // preserving any edited customReplacement text.
  useEffect(() => {
    if (!isFocused || !pendingDiff) return;
    const onKey = (e: KeyboardEvent) => {
      const targetEl = e.target as HTMLElement | null;
      const isTypingTarget =
        targetEl &&
        (targetEl.tagName === "INPUT" ||
          targetEl.tagName === "TEXTAREA" ||
          targetEl.tagName === "SELECT" ||
          targetEl.isContentEditable);
      if (isTypingTarget) return;

      if (e.key === "Tab" && !e.shiftKey && !e.ctrlKey && !e.altKey && !e.metaKey) {
        e.preventDefault();
        const trimmed = customReplacement.trim();
        const override =
          trimmed && trimmed !== pendingDiff.replacement ? trimmed : undefined;
        onApplyDiff(pendingDiff.id, true, override);
      } else if (e.key === "Escape") {
        if (document.querySelector('[data-nib-modal="true"]')) return;
        e.preventDefault();
        onApplyDiff(pendingDiff.id, false);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [isFocused, pendingDiff, customReplacement, onApplyDiff]);

  const runes = useMemo(() => Array.from(text), [text]);
  const { inlineSpans, paragraphLevelIssues } = useMemo(
    () => resolveLintSpans(runes, lintIssues),
    [runes, lintIssues]
  );
  const entitySpans = useMemo(() => {
    if (!entitySnapshots || entitySnapshots.length === 0 || isStreamingLast) {
      return [];
    }
    return resolveEntitySpans(
      runes,
      entitySnapshots,
      inlineSpans.map((sp) => ({ start: sp.start, end: sp.end }))
    );
  }, [runes, entitySnapshots, inlineSpans, isStreamingLast]);

  const hasAnyFlag =
    lintIssues.length > 0 || criticFindings.length > 0 || pendingDiff !== null;

  const renderedText = useMemo<ReactNode>(() => {
    if (inlineSpans.length === 0 && entitySpans.length === 0) return text;
    type UnifiedSpan =
      | { kind: "lint"; start: number; end: number; issue: LintIssue }
      | { kind: "entity"; start: number; end: number; snapshot: EntitySnapshot };

    const merged: UnifiedSpan[] = [
      ...inlineSpans.map((sp) => ({
        kind: "lint" as const,
        start: sp.start,
        end: sp.end,
        issue: sp.issue,
      })),
      ...entitySpans.map((ep) => ({
        kind: "entity" as const,
        start: ep.start,
        end: ep.end,
        snapshot: ep.snapshot,
      })),
    ].sort((a, b) => a.start - b.start);

    const nodes: ReactNode[] = [];
    let cursor = 0;
    merged.forEach((sp, i) => {
      if (sp.start > cursor) {
        nodes.push(
          <span key={`plain-${i}`}>
            {runes.slice(cursor, sp.start).join("")}
          </span>
        );
      }
      const sliceText = runes.slice(sp.start, sp.end).join("");
      if (sp.kind === "lint") {
        const isErr = sp.issue.severity === "error";
        nodes.push(
          <span
            key={`lint-${i}`}
            role="button"
            tabIndex={0}
            title={`${getDeaiCategoryLabel(sp.issue.category)}：${sp.issue.hint}`}
            onClick={(e) => {
              e.stopPropagation();
              onFocus(paragraphIndex);
              setSelectedEntity(null);
              setSelectedIssue((prev) =>
                prev === sp.issue ? null : sp.issue
              );
            }}
            className={`cursor-pointer rounded-sm px-0.5 underline decoration-dashed decoration-2 underline-offset-4 transition-colors ${
              isErr
                ? "bg-danger-soft/45 text-ink-primary decoration-danger hover:bg-danger-soft"
                : "bg-warn-soft/40 text-ink-primary decoration-warn hover:bg-warn-soft"
            }`}
          >
            {sliceText}
          </span>
        );
      } else {
        nodes.push(
          <span
            key={`ent-${i}`}
            role="button"
            tabIndex={0}
            title={`点击查看「${sp.snapshot.entity.name}」截至本章档案`}
            onClick={(e) => {
              e.stopPropagation();
              onFocus(paragraphIndex);
              setSelectedIssue(null);
              setSelectedEntity((prev) =>
                prev?.entity.id === sp.snapshot.entity.id ? null : sp.snapshot
              );
            }}
            className="cursor-pointer rounded-sm border-b border-dotted border-accent/55 px-0.5 text-ink-primary transition-colors hover:bg-accent-soft/45"
          >
            {sliceText}
          </span>
        );
      }
      cursor = sp.end;
    });
    if (cursor < runes.length) {
      nodes.push(
        <span key="plain-tail">{runes.slice(cursor).join("")}</span>
      );
    }
    return nodes;
  }, [inlineSpans, entitySpans, runes, text, onFocus, paragraphIndex]);

  const activeIssue = selectedIssue ?? paragraphLevelIssues[0] ?? null;

  return (
    <div
      data-paragraph-index={paragraphIndex}
      onClick={() => {
        if (hasAnyFlag) onFocus(paragraphIndex);
      }}
      className={`group relative mb-[var(--prose-paragraph-gap)] rounded-lg transition-colors ${
        isFocused
          ? "bg-surface/80 ring-1 ring-accent/40 px-3 py-2 -mx-3"
          : hasAnyFlag
          ? "hover:bg-surface/45 px-2 py-1 -mx-2"
          : ""
      }`}
    >
      {/* Left Gutter Paragraph Number & Issue Dot */}
      <div
        className={`mb-1 flex items-center justify-between text-[11px] select-none ${
          hasAnyFlag || isFocused
            ? "opacity-100"
            : "opacity-0 transition-opacity group-hover:opacity-65"
        }`}
      >
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="nib-mono text-[10.5px] text-ink-muted">
            P{paragraphIndex + 1}
          </span>

          {paragraphLevelIssues.map((iss, idx) => (
            <button
              key={idx}
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                onFocus(paragraphIndex);
                setSelectedIssue((prev) => (prev === iss ? null : iss));
              }}
              className={`inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-[10px] ${
                getLintSeverityMeta(iss.severity).badgeClass
              }`}
            >
              <AlertTriangle className="h-2.5 w-2.5" />
              <span>{getDeaiCategoryLabel(iss.category)}</span>
            </button>
          ))}

          {criticFindings.map((cf) => {
            const dimMeta = getCriticDimensionMeta(cf.dimension);
            return (
              <span
                key={cf.id}
                className={`inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-[10px] font-medium ${dimMeta.badgeClass}`}
              >
                <span>{dimMeta.shortLabel ?? dimMeta.label}</span>
              </span>
            );
          })}
        </div>

        {hasAnyFlag && !pendingDiff && (
          <button
            type="button"
            disabled={polishing}
            onClick={(e) => {
              e.stopPropagation();
              onFocus(paragraphIndex);
              onRequestPolish(paragraphIndex);
            }}
            className="flex items-center gap-1 rounded px-1.5 py-0.5 text-[10.5px] font-medium text-accent transition-colors hover:bg-accent-soft disabled:opacity-40"
          >
            <Wand2 className="h-2.5 w-2.5" />
            <span>{polishing ? "润色中…" : "手术润色"}</span>
          </button>
        )}
      </div>

      {/* Paragraph Text */}
      <p className="!mb-0">
        {renderedText}
        {isStreamingLast && (
          <span className="ml-0.5 inline-block h-[1.1em] w-[2px] translate-y-[2px] animate-pulse bg-accent align-baseline" />
        )}
      </p>

      {/* Inline Lint Issue Tooltip Card (when user clicks an underline or paragraph badge) */}
      {isFocused && activeIssue && !pendingDiff && (
        <div
          onClick={(e) => e.stopPropagation()}
          className="mt-2.5 flex items-start justify-between gap-3 rounded-lg border border-border-strong bg-elevated px-3.5 py-2.5 font-ui text-xs"
        >
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-1.5">
              <span
                className={`inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-[10px] font-medium ${
                  getLintSeverityMeta(activeIssue.severity).badgeClass
                }`}
              >
                <ShieldAlert className="h-2.5 w-2.5" />
                {getDeaiCategoryLabel(activeIssue.category)}
              </span>
              {activeIssue.matchedText && (
                <span className="rounded bg-danger-soft px-1.5 py-0.5 text-[11px] font-medium text-danger">
                  「{activeIssue.matchedText}」
                </span>
              )}
            </div>
            {activeIssue.hint && (
              <div className="mt-1 text-xs leading-relaxed text-ink-secondary">
                {activeIssue.hint}
              </div>
            )}
          </div>

          <div className="flex shrink-0 flex-wrap items-center gap-1.5">
            {onQuickReplaceIssue && activeIssue.replacement && (
              <button
                type="button"
                onClick={() => {
                  onQuickReplaceIssue(activeIssue, activeIssue.replacement!);
                  setSelectedIssue(null);
                }}
                className="flex items-center gap-1 rounded-md bg-ok px-2.5 py-1 text-[11px] font-semibold text-white hover:opacity-90"
              >
                <Check className="h-3 w-3" />
                <span>替换为「{activeIssue.replacement}」</span>
              </button>
            )}
            <button
              type="button"
              disabled={polishing}
              onClick={() => onRequestPolish(paragraphIndex)}
              className="flex items-center gap-1 rounded-md bg-accent px-2.5 py-1 text-[11px] font-semibold text-canvas hover:opacity-90 disabled:opacity-45"
            >
              <Wand2 className="h-3 w-3" />
              <span>生成替换</span>
            </button>
            <button
              type="button"
              onClick={() => setSelectedIssue(null)}
              className="rounded p-1 text-ink-muted hover:text-ink-primary"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
        </div>
      )}

      {isFocused &&
        activeIssue &&
        !pendingDiff &&
        onQuickReplaceIssue &&
        activeIssue.category === "sensitive" &&
        !activeIssue.replacement && (
          <div
            onClick={(e) => e.stopPropagation()}
            className="mt-1.5 flex items-center gap-2 rounded-lg border border-border-subtle bg-surface px-3 py-2 font-ui text-xs"
          >
            <span className="shrink-0 text-[11px] text-ink-secondary">
              敏感词替换：
            </span>
            <input
              type="text"
              value={inlineSensitiveRep}
              onChange={(e) => setInlineSensitiveRep(e.target.value)}
              placeholder="输入安全替换词（留空则直接删去）"
              className="min-w-0 flex-1 rounded border border-border-subtle bg-canvas px-2 py-1 text-xs text-ink-primary focus:border-accent focus:outline-none"
            />
            <button
              type="button"
              onClick={() => {
                onQuickReplaceIssue(activeIssue, inlineSensitiveRep);
                setSelectedIssue(null);
              }}
              className="rounded bg-ok px-2.5 py-1 text-[11px] font-semibold text-white hover:opacity-90"
            >
              {inlineSensitiveRep ? "一键替换" : "直接删去"}
            </button>
          </div>
        )}

      {/* Inline Entity Snapshot Detail Card */}
      {isFocused && selectedEntity && !pendingDiff && (
        <div
          onClick={(e) => e.stopPropagation()}
          className="mt-2.5 rounded-lg border border-accent/40 bg-elevated px-3.5 py-2.5 font-ui text-xs"
        >
          <div className="flex items-center justify-between gap-2">
            <div className="flex flex-wrap items-center gap-1.5">
              <BookMarked className="h-3.5 w-3.5 text-accent" />
              <span className="font-medium text-ink-primary">
                {selectedEntity.entity.name}
              </span>
              <span className="rounded bg-surface px-1.5 py-0.5 text-[10px] text-ink-secondary">
                {getEntityKindLabel(selectedEntity.entity.kind)}
              </span>
              {selectedEntity.pendingEventCount > 0 && (
                <span className="rounded bg-warn-soft px-1.5 py-0.5 text-[10px] text-warn">
                  含 {selectedEntity.pendingEventCount} 条未审状态
                </span>
              )}
            </div>
            <div className="flex items-center gap-1.5">
              {onInspectEntity && (
                <button
                  type="button"
                  onClick={() => onInspectEntity(selectedEntity)}
                  className="rounded border border-border-subtle bg-surface px-2 py-0.5 text-[11px] text-accent hover:border-accent/50"
                >
                  在参照栏查看
                </button>
              )}
              <button
                type="button"
                onClick={() => setSelectedEntity(null)}
                className="rounded p-1 text-ink-muted hover:text-ink-primary"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>
          </div>
          {selectedEntity.entity.summary && (
            <div className="mt-1.5 text-[11.5px] leading-relaxed text-ink-secondary">
              {selectedEntity.entity.summary}
            </div>
          )}
          {Object.keys(selectedEntity.effectiveAttributes ?? {}).length > 0 && (
            <div className="mt-2 flex flex-wrap gap-1.5">
              {Object.entries(selectedEntity.effectiveAttributes).map(([k, v]) => (
                <span
                  key={k}
                  className="rounded border border-border-subtle bg-canvas px-1.5 py-0.5 text-[10.5px] text-ink-primary"
                >
                  <span className="text-ink-muted">{k}：</span>
                  {String(v)}
                </span>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Critic Finding Inline Callout */}
      {isFocused && criticFindings.length > 0 && !pendingDiff && (
        <div
          onClick={(e) => e.stopPropagation()}
          className="mt-2 flex flex-col gap-1.5 rounded-lg border border-warn/35 bg-warn-soft/25 px-3.5 py-2.5 font-ui text-xs"
        >
          {criticFindings.map((cf) => {
            const dimMeta = getCriticDimensionMeta(cf.dimension);
            return (
              <div key={cf.id} className="flex flex-col gap-0.5">
                <div className="flex items-center gap-1.5 font-medium text-ink-primary">
                  <span
                    className={`rounded border px-1.5 py-0.5 text-[10px] ${dimMeta.badgeClass}`}
                  >
                    {dimMeta.label}
                  </span>
                  <span>{cf.title}</span>
                </div>
                {cf.detail && (
                  <div className="text-[11.5px] text-ink-secondary">{cf.detail}</div>
                )}
                {cf.suggestion && (
                  <div className="text-[11.5px] text-accent">
                    建议：{cf.suggestion}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {/* In-Place Surgical Diff Floating Card (docs/04-ui-ux-design.md §3.2) */}
      {pendingDiff && (
        <div
          onClick={(e) => e.stopPropagation()}
          className="mt-3 rounded-xl border border-accent/45 bg-elevated p-3.5 font-ui text-xs"
        >
          <div className="flex items-center justify-between gap-2 border-b border-border-subtle pb-2">
            <div className="flex min-w-0 items-center gap-1.5">
              <Sparkles className="h-3.5 w-3.5 shrink-0 text-accent" />
              <span className="truncate font-medium text-ink-primary">
                手术润色建议：{pendingDiff.reason || "消除 AI 套话与灌水"}
              </span>
            </div>
            <button
              type="button"
              onClick={() => setEditingDiff((v) => !v)}
              className="flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-ink-secondary hover:bg-surface hover:text-ink-primary"
            >
              <Pencil className="h-2.5 w-2.5" />
              <span>{editingDiff ? "完成微调" : "微调译文"}</span>
            </button>
          </div>

          <div className="mt-2.5 rounded-md border border-danger/25 bg-danger-soft/35 px-3 py-2 font-prose text-[14px] leading-relaxed text-ink-muted line-through">
            {pendingDiff.original}
          </div>

          <div className="mt-2">
            {editingDiff ? (
              <textarea
                rows={3}
                autoFocus
                value={customReplacement}
                onChange={(e) => setCustomReplacement(e.target.value)}
                className="w-full resize-y rounded-md border border-ok/50 bg-canvas px-3 py-2 font-prose text-[14.5px] leading-relaxed text-ink-primary focus:border-accent focus:outline-none"
              />
            ) : (
              <div className="rounded-md border border-ok/35 bg-ok-soft/45 px-3 py-2 font-prose text-[14.5px] leading-relaxed text-ink-primary">
                {customReplacement || pendingDiff.replacement}
              </div>
            )}
          </div>

          <div className="mt-3 flex items-center justify-between gap-2">
            <span className="text-[11px] text-ink-muted">
              采纳前自动备份当前版本至历史快照
            </span>
            <div className="flex items-center gap-2">
              <button
                type="button"
                disabled={applyingDiffId === pendingDiff.id}
                onClick={() => onApplyDiff(pendingDiff.id, false)}
                className="flex items-center gap-1 rounded-md border border-border-subtle bg-surface px-3 py-1 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary disabled:opacity-45"
              >
                <X className="h-3.5 w-3.5" />
                <span>忽略 (Esc)</span>
              </button>
              <button
                type="button"
                disabled={applyingDiffId === pendingDiff.id}
                onClick={() => {
                  const trimmed = customReplacement.trim();
                  const override =
                    trimmed && trimmed !== pendingDiff.replacement
                      ? trimmed
                      : undefined;
                  onApplyDiff(pendingDiff.id, true, override);
                }}
                className="flex items-center gap-1 rounded-md bg-ok px-3.5 py-1 text-xs font-semibold text-white transition-opacity hover:opacity-90 disabled:opacity-45"
              >
                <Check className="h-3.5 w-3.5" />
                <span>采纳替换 (Tab)</span>
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
});
