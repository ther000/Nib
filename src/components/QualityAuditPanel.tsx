import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AlertTriangle,
  Check,
  CheckCircle2,
  Eye,
  Loader2,
  ScanSearch,
  ShieldAlert,
  Sparkles,
  Square,
  Wand2,
  X,
} from "lucide-react";
import {
  StyleService,
  type ApplySurgicalDiffResult,
  type ChapterAudit,
  type LintIssue,
} from "../mygo";
import {
  formatErrorMessage,
  getCriticDimensionMeta,
  getDeaiCategoryLabel,
  getLintSeverityMeta,
} from "../lib/constants";

type AuditFilter = "all" | "lint" | "critic" | "diffs";

interface QualityAuditPanelProps {
  chapterId: number;
  chapterHasContent: boolean;
  audit: ChapterAudit | null;
  loading: boolean;
  activeParagraphIndex: number | null;
  polishingParagraphs: boolean;
  onAuditUpdated: (audit: ChapterAudit) => void;
  onDiffApplied: (res: ApplySurgicalDiffResult) => void;
  onFocusParagraph: (paragraphIndex: number) => void;
  onEnsureSavedBeforeAudit: () => Promise<boolean>;
  onRunLocalLint: () => Promise<void>;
  onRunSurgicalPolish: (targetParagraphs: number[]) => Promise<void>;
  onStopSurgicalPolish: () => void;
  onQuickReplaceIssue?: (issue: LintIssue, replacement: string) => void;
  onNotify: (input: {
    tone: "info" | "ok" | "warn" | "danger";
    message: string;
    detail?: string;
  }) => void;
}

export function QualityAuditPanel({
  chapterId,
  chapterHasContent,
  audit,
  loading,
  activeParagraphIndex,
  polishingParagraphs,
  onAuditUpdated,
  onDiffApplied,
  onFocusParagraph,
  onEnsureSavedBeforeAudit,
  onRunLocalLint,
  onRunSurgicalPolish,
  onStopSurgicalPolish,
  onQuickReplaceIssue,
  onNotify,
}: QualityAuditPanelProps) {
  const [filter, setFilter] = useState<AuditFilter>("all");
  const [criticFilter, setCriticFilter] = useState<string>("all");
  const [criticRunning, setCriticRunning] = useState(false);
  const [applyingDiffId, setApplyingDiffId] = useState<string | null>(null);

  const criticAbortRef = useRef<AbortController | null>(null);
  const criticRunIdRef = useRef(0);
  const mountedRef = useRef(true);
  const chapterIdRef = useRef(chapterId);
  chapterIdRef.current = chapterId;

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      criticRunIdRef.current += 1;
      if (criticAbortRef.current) {
        criticAbortRef.current.abort();
        criticAbortRef.current = null;
      }
    };
  }, []);

  useEffect(() => {
    if (criticAbortRef.current) {
      criticRunIdRef.current += 1;
      criticAbortRef.current.abort();
      criticAbortRef.current = null;
      setCriticRunning(false);
    }
  }, [chapterId]);

  const stopCritic = useCallback(() => {
    if (criticAbortRef.current) {
      criticAbortRef.current.abort();
      criticAbortRef.current = null;
    }
  }, []);

  const handleRunCritic = useCallback(async () => {
    if (criticRunning) return;
    const savedOk = await onEnsureSavedBeforeAudit();
    if (!savedOk) return;
    const targetChapterId = chapterId;
    const runId = ++criticRunIdRef.current;
    const ac = new AbortController();
    criticAbortRef.current = ac;
    setCriticRunning(true);

    try {
      const updated = await StyleService.runCriticAudit.with({
        signal: ac.signal,
      })(targetChapterId);
      if (
        !mountedRef.current ||
        criticRunIdRef.current !== runId ||
        chapterIdRef.current !== targetChapterId
      ) {
        return;
      }
      onAuditUpdated(updated);
      onNotify({
        tone: updated.hookPassed && updated.criticFindings.length === 0 ? "ok" : "info",
        message: `Critic 盲审完成（${updated.criticFindings.length} 条审校意见）`,
        detail: updated.hookVerdict || (updated.hookPassed ? "章末钩子达标" : "章末钩子待加强"),
      });
    } catch (err) {
      if (ac.signal.aborted) {
        if (mountedRef.current && criticRunIdRef.current === runId) {
          onNotify({ tone: "info", message: "已取消 Critic 盲审" });
        }
        return;
      }
      if (!mountedRef.current || criticRunIdRef.current !== runId) return;
      onNotify({
        tone: "danger",
        message: "Critic 盲审失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      if (criticAbortRef.current === ac) criticAbortRef.current = null;
      if (mountedRef.current && criticRunIdRef.current === runId) {
        setCriticRunning(false);
      }
    }
  }, [chapterId, criticRunning, onEnsureSavedBeforeAudit, onAuditUpdated, onNotify]);

  const handleApplyDiff = async (diffId: string, accept: boolean) => {
    setApplyingDiffId(diffId);
    try {
      const res = await StyleService.applySurgicalDiff({
        chapterId,
        diffId,
        accept,
      });
      onDiffApplied(res);
      onNotify({
        tone: accept ? "ok" : "info",
        message: accept ? "已采纳手术润色并归档快照" : "已忽略该条润色建议",
      });
    } catch (err) {
      onNotify({
        tone: "danger",
        message: "应用手术润色失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setApplyingDiffId(null);
    }
  };

  const lintIssues = audit?.lintIssues ?? [];
  const criticFindings = audit?.criticFindings ?? [];
  const pendingDiffs = useMemo(
    () => (audit?.diffs ?? []).filter((d) => d.status === "pending"),
    [audit?.diffs]
  );

  const filteredCriticFindings = useMemo(
    () =>
      criticFilter === "all"
        ? criticFindings
        : criticFindings.filter((f) => f.dimension === criticFilter),
    [criticFindings, criticFilter]
  );

  const flaggedParagraphIndices = useMemo(() => {
    const set = new Set<number>();
    for (const issue of lintIssues) {
      if (issue.paragraphIndex >= 0) set.add(issue.paragraphIndex);
    }
    for (const finding of criticFindings) {
      if (finding.paragraphIndex >= 0) set.add(finding.paragraphIndex);
    }
    return Array.from(set).sort((a, b) => a - b);
  }, [lintIssues, criticFindings]);

  const score = audit ? audit.deaiScore : 100;
  const scoreColorClass =
    score >= 85
      ? "text-ok bg-ok-soft border-ok/35"
      : score >= 65
      ? "text-warn bg-warn-soft border-warn/35"
      : "text-danger bg-danger-soft border-danger/35";

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      {/* Top Score & Hook Summary Strip */}
      <div className="shrink-0 border-b border-border-subtle bg-surface/65 px-3.5 py-2.5">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <span
              className={`nib-mono rounded-md border px-2 py-0.5 text-xs font-semibold ${scoreColorClass}`}
            >
              去 AI 味 {score} / 100
            </span>

            {audit && (audit.hookVerdict || criticFindings.length > 0 || audit.hookPassed) && (
              <span
                title={audit.hookVerdict || undefined}
                className={`inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[10.5px] font-medium ${
                  audit.hookPassed
                    ? "border-ok/35 bg-ok-soft text-ok"
                    : "border-warn/35 bg-warn-soft text-warn"
                }`}
              >
                {audit.hookPassed ? (
                  <CheckCircle2 className="h-3 w-3" />
                ) : (
                  <AlertTriangle className="h-3 w-3" />
                )}
                <span>{audit.hookPassed ? "钩子达标" : "钩子待加强"}</span>
              </span>
            )}
          </div>

          <span className="nib-mono text-[11px] text-ink-muted">
            {lintIssues.length} 告警 · {criticFindings.length} 盲审
          </span>
        </div>

        {audit?.hookVerdict && (
          <div className="mt-1.5 text-[11px] leading-relaxed text-ink-secondary">
            钩子评语：{audit.hookVerdict}
          </div>
        )}

        {/* 3 Action Buttons */}
        <div className="mt-2.5 grid grid-cols-3 gap-1.5">
          <button
            type="button"
            disabled={!chapterHasContent || loading}
            onClick={() => void onRunLocalLint()}
            title="0 Token 确定性本地扫描 (<20ms)"
            className="flex items-center justify-center gap-1 rounded-md border border-border-subtle bg-elevated px-2 py-1.5 text-[11px] font-medium text-ink-primary transition-colors hover:border-border-strong disabled:opacity-40"
          >
            <ScanSearch className="h-3 w-3 text-accent" />
            <span>本地扫描</span>
          </button>

          {criticRunning ? (
            <button
              type="button"
              onClick={stopCritic}
              className="flex items-center justify-center gap-1 rounded-md border border-danger/40 bg-danger-soft px-2 py-1.5 text-[11px] font-medium text-danger"
            >
              <Square className="h-2.5 w-2.5 fill-current" />
              <span>停止盲审</span>
            </button>
          ) : (
            <button
              type="button"
              disabled={!chapterHasContent || polishingParagraphs}
              onClick={() => void handleRunCritic()}
              title="调用 critic 模型执行 4 维防吃书与钩子盲审"
              className="flex items-center justify-center gap-1 rounded-md border border-border-subtle bg-elevated px-2 py-1.5 text-[11px] font-medium text-ink-primary transition-colors hover:border-border-strong disabled:opacity-40"
            >
              <Eye className="h-3 w-3 text-accent" />
              <span>Critic 盲审</span>
            </button>
          )}

          {polishingParagraphs ? (
            <button
              type="button"
              onClick={onStopSurgicalPolish}
              className="flex items-center justify-center gap-1 rounded-md border border-danger/40 bg-danger-soft px-2 py-1.5 text-[11px] font-medium text-danger"
            >
              <Square className="h-2.5 w-2.5 fill-current" />
              <span>停止润色</span>
            </button>
          ) : (
            <button
              type="button"
              disabled={!chapterHasContent || criticRunning}
              onClick={() =>
                void onRunSurgicalPolish(
                  activeParagraphIndex !== null
                    ? [activeParagraphIndex]
                    : flaggedParagraphIndices
                )
              }
              title={
                activeParagraphIndex !== null
                  ? `针对当前选中段 [P${activeParagraphIndex + 1}] 生成手术式润色`
                  : "针对病灶段落生成最小局部替换建议"
              }
              className="flex items-center justify-center gap-1 rounded-md border border-accent/35 bg-accent-soft px-2 py-1.5 text-[11px] font-medium text-accent transition-colors hover:border-accent disabled:opacity-40"
            >
              <Wand2 className="h-3 w-3" />
              <span>手术润色</span>
            </button>
          )}
        </div>
      </div>

      {/* Progress Banners */}
      {(criticRunning || polishingParagraphs) && (
        <div className="flex shrink-0 items-center gap-2 border-b border-border-subtle bg-accent-soft/60 px-3.5 py-2 text-xs text-accent">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          <span>
            {criticRunning
              ? "正在执行 4 维独立盲审（防吃书 / 防毒 / 节拍 / 钩子）…"
              : "正在针对病灶段落生成手术式局部替换建议…"}
          </span>
        </div>
      )}

      {/* Filter Bar */}
      <div className="flex shrink-0 items-center gap-1 border-b border-border-subtle bg-canvas/40 px-3 py-1.5 text-[11px]">
        {(
          [
            { id: "all", label: "全部" },
            { id: "diffs", label: `润色 (${pendingDiffs.length})` },
            { id: "critic", label: `盲审 (${criticFindings.length})` },
            { id: "lint", label: `AI 味 (${lintIssues.length})` },
          ] as Array<{ id: AuditFilter; label: string }>
        ).map((tab) => (
          <button
            key={tab.id}
            type="button"
            onClick={() => setFilter(tab.id)}
            className={`rounded px-2 py-0.5 font-medium transition-colors ${
              filter === tab.id
                ? "bg-accent-soft text-accent"
                : "text-ink-secondary hover:text-ink-primary"
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* Findings & Diffs List */}
      <div className="flex-1 overflow-y-auto px-3.5 py-3">
        {!chapterHasContent ? (
          <div className="py-12 text-center text-xs text-ink-muted">
            本章尚无正文，生成或输入正文后即可自动执行去 AI 味扫描与审校。
          </div>
        ) : pendingDiffs.length === 0 &&
          criticFindings.length === 0 &&
          lintIssues.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-12 text-center">
            <CheckCircle2 className="h-5 w-5 text-ok" />
            <div className="text-xs font-medium text-ink-primary">
              当前未检出 AI 套话或结构病灶
            </div>
            <div className="text-[11px] text-ink-muted">
              可点击上方「Critic 盲审」进一步核验吃书冲突与章末钩子
            </div>
          </div>
        ) : (
          <div className="flex flex-col gap-3.5">
            {/* 1. Surgical Diffs */}
            {(filter === "all" || filter === "diffs") && pendingDiffs.length > 0 && (
              <div className="flex flex-col gap-2">
                <div className="text-[11px] font-medium text-ink-secondary">
                  待处理手术润色建议（{pendingDiffs.length}）
                </div>
                {pendingDiffs.map((diff) => {
                  const isFocused = activeParagraphIndex === diff.paragraphIndex;
                  const isApplying = applyingDiffId === diff.id;
                  return (
                    <div
                      key={diff.id}
                      onClick={() => onFocusParagraph(diff.paragraphIndex)}
                      className={`cursor-pointer rounded-lg border p-3 transition-colors ${
                        isFocused
                          ? "border-accent bg-accent-soft/35"
                          : "border-border-subtle bg-canvas/75 hover:border-border-strong"
                      }`}
                    >
                      <div className="flex items-center justify-between gap-2">
                        <div className="flex items-center gap-1.5">
                          <span className="nib-mono rounded bg-accent-soft px-1.5 py-0.5 text-[10px] font-semibold text-accent">
                            P{diff.paragraphIndex + 1}
                          </span>
                          <span className="truncate text-xs font-medium text-ink-primary">
                            {diff.reason || "病灶段落润色"}
                          </span>
                        </div>
                        <Sparkles className="h-3.5 w-3.5 shrink-0 text-accent" />
                      </div>

                      <div className="mt-2 rounded bg-danger-soft/50 px-2 py-1.5 text-[11.5px] leading-relaxed text-ink-muted line-through">
                        {diff.original}
                      </div>
                      <div className="mt-1.5 rounded bg-ok-soft/60 px-2 py-1.5 text-[11.5px] leading-relaxed text-ink-primary">
                        {diff.replacement}
                      </div>

                      <div
                        className="mt-2.5 flex items-center justify-end gap-1.5"
                        onClick={(e) => e.stopPropagation()}
                      >
                        <button
                          type="button"
                          disabled={isApplying}
                          onClick={() => void handleApplyDiff(diff.id, false)}
                          className="flex items-center gap-1 rounded border border-border-subtle bg-surface px-2.5 py-1 text-[11px] text-ink-secondary hover:text-ink-primary"
                        >
                          <X className="h-3 w-3" />
                          <span>忽略</span>
                        </button>
                        <button
                          type="button"
                          disabled={isApplying}
                          onClick={() => void handleApplyDiff(diff.id, true)}
                          className="flex items-center gap-1 rounded bg-ok px-2.5 py-1 text-[11px] font-semibold text-white hover:opacity-90"
                        >
                          <Check className="h-3 w-3" />
                          <span>采纳替换</span>
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}

            {/* 2. Critic Findings */}
            {(filter === "all" || filter === "critic") &&
              criticFindings.length > 0 && (
                <div className="flex flex-col gap-2">
                  <div className="flex flex-wrap items-center justify-between gap-1">
                    <span className="text-[11px] font-medium text-ink-secondary">
                      Critic 盲审意见（{filteredCriticFindings.length}）
                    </span>
                    <select
                      value={criticFilter}
                      onChange={(e) => setCriticFilter(e.target.value)}
                      className="rounded border border-border-subtle bg-canvas px-1.5 py-0.5 text-[10.5px] text-ink-secondary focus:border-accent focus:outline-none"
                    >
                      <option value="all">全部维度</option>
                      <option value="consistency">防吃书一致性</option>
                      <option value="poison_point">男频毒点排查</option>
                      <option value="beat_coverage">节拍落点核验</option>
                      <option value="hook">章末钩子强检</option>
                    </select>
                  </div>

                  {filteredCriticFindings.map((finding) => {
                    const dimMeta = getCriticDimensionMeta(finding.dimension);
                    const sevMeta = getLintSeverityMeta(finding.severity);
                    const isFocused =
                      activeParagraphIndex === finding.paragraphIndex;
                    return (
                      <div
                        key={finding.id}
                        onClick={() => {
                          if (finding.paragraphIndex >= 0) {
                            onFocusParagraph(finding.paragraphIndex);
                          }
                        }}
                        className={`cursor-pointer rounded-lg border p-3 transition-colors ${
                          isFocused
                            ? "border-accent bg-accent-soft/30"
                            : "border-border-subtle bg-canvas/70 hover:border-border-strong"
                        }`}
                      >
                        <div className="flex flex-wrap items-center justify-between gap-1.5">
                          <div className="flex flex-wrap items-center gap-1.5">
                            {finding.paragraphIndex >= 0 && (
                              <span className="nib-mono rounded bg-surface px-1.5 py-0.5 text-[10px] font-medium text-ink-secondary">
                                P{finding.paragraphIndex + 1}
                              </span>
                            )}
                            <span
                              className={`rounded border px-1.5 py-0.5 text-[10px] font-medium ${dimMeta.badgeClass}`}
                            >
                              {dimMeta.shortLabel ?? dimMeta.label}
                            </span>
                            <span
                              className={`rounded border px-1.5 py-0.5 text-[10px] ${sevMeta.badgeClass}`}
                            >
                              {sevMeta.label}
                            </span>
                          </div>

                          {finding.paragraphIndex >= 0 && (
                            <button
                              type="button"
                              disabled={polishingParagraphs}
                              onClick={(e) => {
                                e.stopPropagation();
                                void onRunSurgicalPolish([finding.paragraphIndex]);
                              }}
                              className="flex items-center gap-1 text-[10.5px] font-medium text-accent hover:opacity-80"
                            >
                              <Wand2 className="h-2.5 w-2.5" />
                              <span>润色此段</span>
                            </button>
                          )}
                        </div>

                        <div className="mt-1.5 text-xs font-medium text-ink-primary">
                          {finding.title}
                        </div>
                        {finding.detail && (
                          <div className="mt-1 text-[11.5px] leading-relaxed text-ink-secondary">
                            {finding.detail}
                          </div>
                        )}
                        {finding.suggestion && (
                          <div className="mt-1.5 rounded bg-surface/80 px-2 py-1 text-[11px] text-ink-primary">
                            建议：{finding.suggestion}
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              )}

            {/* 3. Local De-AI Lint Issues */}
            {(filter === "all" || filter === "lint") && lintIssues.length > 0 && (
              <div className="flex flex-col gap-2">
                <div className="text-[11px] font-medium text-ink-secondary">
                  本地 AI 味与排版告警（{lintIssues.length}）
                </div>
                {lintIssues.map((issue, idx) => {
                  const sevMeta = getLintSeverityMeta(issue.severity);
                  const isFocused = activeParagraphIndex === issue.paragraphIndex;
                  return (
                    <div
                      key={`${issue.ruleId}-${issue.paragraphIndex}-${issue.startRune}-${idx}`}
                      onClick={() => onFocusParagraph(issue.paragraphIndex)}
                      className={`cursor-pointer rounded-lg border p-2.5 transition-colors ${
                        isFocused
                          ? "border-accent bg-accent-soft/30"
                          : "border-border-subtle bg-canvas/70 hover:border-border-strong"
                      }`}
                    >
                      <div className="flex items-center justify-between gap-2">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <span className="nib-mono rounded bg-surface px-1.5 py-0.5 text-[10px] font-medium text-ink-secondary">
                            P{issue.paragraphIndex + 1}
                          </span>
                          <span
                            className={`rounded border px-1.5 py-0.5 text-[10px] font-medium ${sevMeta.badgeClass}`}
                          >
                            {issue.severity === "error" ? (
                              <ShieldAlert className="mr-0.5 inline h-2.5 w-2.5" />
                            ) : null}
                            {getDeaiCategoryLabel(issue.category)}
                          </span>
                          {issue.matchedText && (
                            <span className="truncate rounded bg-danger-soft/60 px-1.5 py-0.5 text-[11px] font-medium text-danger">
                              「{issue.matchedText}」
                            </span>
                          )}
                        </div>

                        <div className="flex shrink-0 items-center gap-1.5">
                          {onQuickReplaceIssue &&
                            (issue.replacement || issue.category === "sensitive") && (
                              <button
                                type="button"
                                onClick={(e) => {
                                  e.stopPropagation();
                                  onQuickReplaceIssue(
                                    issue,
                                    issue.replacement ?? ""
                                  );
                                }}
                                className="rounded bg-ok-soft px-1.5 py-0.5 text-[10.5px] font-medium text-ok hover:bg-ok hover:text-white"
                              >
                                {issue.replacement
                                  ? `替换为「${issue.replacement}」`
                                  : "一键删去"}
                              </button>
                            )}
                          <button
                            type="button"
                            disabled={polishingParagraphs}
                            onClick={(e) => {
                              e.stopPropagation();
                              void onRunSurgicalPolish([issue.paragraphIndex]);
                            }}
                            className="shrink-0 text-[10.5px] font-medium text-accent hover:opacity-80"
                          >
                            润色
                          </button>
                        </div>
                      </div>

                      {issue.hint && (
                        <div className="mt-1.5 text-[11px] leading-relaxed text-ink-secondary">
                          {issue.hint}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
