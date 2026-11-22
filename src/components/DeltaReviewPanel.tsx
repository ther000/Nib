import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AlertTriangle,
  ArrowRight,
  CalendarClock,
  Check,
  Clock,
  GitFork,
  Loader2,
  ShieldAlert,
  Sparkles,
  Square,
  Wand2,
} from "lucide-react";
import {
  MemoryService,
  type ApplyDeltaResult,
  type ChapterDelta,
  type DeltaDecision,
} from "../mygo";
import {
  formatErrorMessage,
  getDeltaForeshadowActionMeta,
  getEntityKindLabel,
} from "../lib/constants";

function getDeadlineActionLabel(action: string): string {
  switch (action) {
    case "create":
      return "新建期限";
    case "update":
      return "调整期限";
    case "reach":
      return "如期兑现";
    case "expire":
      return "已超期";
    case "cancel":
      return "已取消";
    default:
      return action || "变更";
  }
}

interface DeltaReviewPanelProps {
  chapterId: number;
  chapterHasContent: boolean;
  delta: ChapterDelta | null;
  loading: boolean;
  onEnsureSavedBeforeExtract?: () => Promise<boolean>;
  onDeltaLoaded: (delta: ChapterDelta | null) => void;
  onDeltaApplied: (result: ApplyDeltaResult) => Promise<void>;
  onNotify: (input: {
    tone: "info" | "ok" | "warn" | "danger";
    message: string;
    detail?: string;
  }) => void;
}

export function DeltaReviewPanel({
  chapterId,
  chapterHasContent,
  delta,
  loading,
  onEnsureSavedBeforeExtract,
  onDeltaLoaded,
  onDeltaApplied,
  onNotify,
}: DeltaReviewPanelProps) {
  const [extracting, setExtracting] = useState(false);
  const [applying, setApplying] = useState(false);

  // Interactive decision state
  const [applyCausal, setApplyCausal] = useState(true);
  const [causalText, setCausalText] = useState("");
  const [applyTimeline, setApplyTimeline] = useState(false);
  const [timelineLabel, setTimelineLabel] = useState("");
  const [timelineDay, setTimelineDay] = useState("");
  const [acceptedNewEntities, setAcceptedNewEntities] = useState<Record<string, boolean>>({});
  const [acceptedPatches, setAcceptedPatches] = useState<Record<string, boolean>>({});
  const [patchOverrides, setPatchOverrides] = useState<Record<string, string>>({});
  const [acceptedForeshadows, setAcceptedForeshadows] = useState<Record<string, boolean>>({});
  const [acceptedRelations, setAcceptedRelations] = useState<Record<string, boolean>>({});
  const [acceptedDeadlines, setAcceptedDeadlines] = useState<Record<string, boolean>>({});

  const extractAbortRef = useRef<AbortController | null>(null);
  const extractRunIdRef = useRef(0);
  const mountedRef = useRef(true);
  const chapterIdRef = useRef(chapterId);
  chapterIdRef.current = chapterId;

  // Cancel any in-flight extraction on unmount
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      extractRunIdRef.current += 1;
      if (extractAbortRef.current) {
        extractAbortRef.current.abort();
        extractAbortRef.current = null;
      }
    };
  }, []);

  // Cancel any in-flight extraction if active chapter switches
  useEffect(() => {
    if (extractAbortRef.current) {
      extractRunIdRef.current += 1;
      extractAbortRef.current.abort();
      extractAbortRef.current = null;
      setExtracting(false);
    }
  }, [chapterId]);

  // Initialize review selections whenever delta record changes
  useEffect(() => {
    if (!delta) {
      setApplyCausal(false);
      setCausalText("");
      setApplyTimeline(false);
      setTimelineLabel("");
      setTimelineDay("");
      setAcceptedNewEntities({});
      setAcceptedPatches({});
      setPatchOverrides({});
      setAcceptedForeshadows({});
      setAcceptedRelations({});
      setAcceptedDeadlines({});
      return;
    }
    setApplyCausal(Boolean(delta.causalSummary?.trim()));
    setCausalText(delta.causalSummary || "");
    const hasTimeline =
      Boolean(delta.storyTimeLabel?.trim()) ||
      (delta.storyDay !== undefined && delta.storyDay !== null);
    setApplyTimeline(hasTimeline);
    setTimelineLabel(delta.storyTimeLabel || "");
    setTimelineDay(
      delta.storyDay !== undefined && delta.storyDay !== null ? String(delta.storyDay) : ""
    );

    const nextNewEnts: Record<string, boolean> = {};
    for (const item of delta.newEntities ?? []) {
      nextNewEnts[item.id] = true;
    }
    setAcceptedNewEntities(nextNewEnts);

    const nextPatches: Record<string, boolean> = {};
    for (const patch of delta.statePatches ?? []) {
      nextPatches[patch.id] = true;
    }
    setAcceptedPatches(nextPatches);
    setPatchOverrides({});

    const nextFs: Record<string, boolean> = {};
    for (const fs of delta.foreshadows ?? []) {
      nextFs[fs.id] = true;
    }
    setAcceptedForeshadows(nextFs);

    const nextRels: Record<string, boolean> = {};
    for (const rel of delta.relationChanges ?? []) {
      nextRels[rel.id] = true;
    }
    setAcceptedRelations(nextRels);

    const nextDls: Record<string, boolean> = {};
    for (const dl of delta.deadlineChanges ?? []) {
      nextDls[dl.id] = true;
    }
    setAcceptedDeadlines(nextDls);
  }, [delta?.id, delta?.createdAt]);

  const stopExtract = useCallback(() => {
    if (extractAbortRef.current) {
      extractAbortRef.current.abort();
      extractAbortRef.current = null;
    }
  }, []);

  const handleExtract = useCallback(async () => {
    if (extracting) return;
    if (onEnsureSavedBeforeExtract) {
      const saved = await onEnsureSavedBeforeExtract();
      if (!saved) return;
    }
    const targetChapterId = chapterId;
    const runId = ++extractRunIdRef.current;
    const ac = new AbortController();
    extractAbortRef.current = ac;
    setExtracting(true);

    try {
      const extracted = await MemoryService.extractChapterDelta.with({
        signal: ac.signal,
      })(targetChapterId);
      if (
        !mountedRef.current ||
        extractRunIdRef.current !== runId ||
        chapterIdRef.current !== targetChapterId
      ) {
        return;
      }
      onDeltaLoaded(extracted);
      const totalItems =
        (extracted.newEntities?.length ?? 0) +
        (extracted.statePatches?.length ?? 0) +
        (extracted.foreshadows?.length ?? 0) +
        (extracted.relationChanges?.length ?? 0) +
        (extracted.deadlineChanges?.length ?? 0);
      onNotify({
        tone: extracted.highRiskFlags > 0 ? "warn" : "ok",
        message: `已抽取本章变更单（${totalItems} 条提案）`,
        detail:
          extracted.highRiskFlags > 0
            ? `包含 ${extracted.highRiskFlags} 条高危变更，请核对后入库`
            : "请勾选确认后入库，绝不静默写入账本",
      });
    } catch (err) {
      if (ac.signal.aborted) {
        if (mountedRef.current && extractRunIdRef.current === runId) {
          onNotify({ tone: "info", message: "已取消变更抽取" });
        }
        return;
      }
      if (!mountedRef.current || extractRunIdRef.current !== runId) return;
      onNotify({
        tone: "danger",
        message: "抽取本章变更失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      if (extractAbortRef.current === ac) extractAbortRef.current = null;
      if (mountedRef.current && extractRunIdRef.current === runId) {
        setExtracting(false);
      }
    }
  }, [chapterId, extracting, onEnsureSavedBeforeExtract, onDeltaLoaded, onNotify]);

  const counts = useMemo(() => {
    if (!delta) return { selected: 0, total: 0, highRiskSelected: 0 };
    const newEnts = delta.newEntities ?? [];
    const patches = delta.statePatches ?? [];
    const fss = delta.foreshadows ?? [];
    const rels = delta.relationChanges ?? [];
    const dls = delta.deadlineChanges ?? [];
    let selected = 0;
    let highRiskSelected = 0;
    for (const item of newEnts) {
      if (acceptedNewEntities[item.id] ?? true) selected += 1;
    }
    for (const p of patches) {
      if (acceptedPatches[p.id] ?? true) {
        selected += 1;
        if (p.severity === "high_risk") highRiskSelected += 1;
      }
    }
    for (const f of fss) {
      if (acceptedForeshadows[f.id] ?? true) selected += 1;
    }
    for (const r of rels) {
      if (acceptedRelations[r.id] ?? true) selected += 1;
    }
    for (const d of dls) {
      if (acceptedDeadlines[d.id] ?? true) selected += 1;
    }
    return {
      selected,
      total: newEnts.length + patches.length + fss.length + rels.length + dls.length,
      highRiskSelected,
    };
  }, [
    delta,
    acceptedNewEntities,
    acceptedPatches,
    acceptedForeshadows,
    acceptedRelations,
    acceptedDeadlines,
  ]);

  const handleApply = async () => {
    if (!delta || applying) return;
    let parsedStoryDay: number | undefined;
    if (applyTimeline && timelineDay.trim()) {
      if (!/^-?\d+$/.test(timelineDay.trim())) {
        onNotify({ tone: "warn", message: "故事日序号必须为整数（或留空）" });
        return;
      }
      parsedStoryDay = Number.parseInt(timelineDay.trim(), 10);
    }

    setApplying(true);
    try {
      const decisions: DeltaDecision[] = [
        ...(delta.newEntities ?? []).map((item) => ({
          kind: "new_entity",
          itemId: item.id,
          accept: acceptedNewEntities[item.id] ?? true,
        })),
        ...(delta.statePatches ?? []).map((patch) => {
          const override = patchOverrides[patch.id]?.trim() ?? "";
          return {
            kind: "state_patch",
            itemId: patch.id,
            accept: acceptedPatches[patch.id] ?? true,
            overrideNewValue:
              override && override !== patch.newValue ? override : undefined,
          };
        }),
        ...(delta.foreshadows ?? []).map((fs) => ({
          kind: "foreshadow",
          itemId: fs.id,
          accept: acceptedForeshadows[fs.id] ?? true,
        })),
        ...(delta.relationChanges ?? []).map((rel) => ({
          kind: "relation",
          itemId: rel.id,
          accept: acceptedRelations[rel.id] ?? true,
        })),
        ...(delta.deadlineChanges ?? []).map((dl) => ({
          kind: "deadline",
          itemId: dl.id,
          accept: acceptedDeadlines[dl.id] ?? true,
        })),
      ];

      const trimmedCausal = causalText.trim();
      const trimmedLabel = timelineLabel.trim();
      const shouldApplyTimeline =
        applyTimeline && (trimmedLabel.length > 0 || parsedStoryDay !== undefined);

      const result = await MemoryService.applyDeltaDecisions({
        deltaId: delta.id,
        applyCausalSummary: applyCausal && trimmedCausal.length > 0,
        causalSummaryOverride:
          applyCausal && trimmedCausal !== (delta.causalSummary || "").trim()
            ? trimmedCausal
            : undefined,
        applyStoryTimeline: shouldApplyTimeline,
        storyTimeLabelOverride:
          shouldApplyTimeline && trimmedLabel !== (delta.storyTimeLabel || "").trim()
            ? trimmedLabel
            : undefined,
        storyDayOverride: shouldApplyTimeline ? parsedStoryDay : undefined,
        decisions,
      });

      await onDeltaApplied(result);
    } catch (err) {
      onNotify({
        tone: "danger",
        message: "提交变更单失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setApplying(false);
    }
  };

  if (loading && !delta && !extracting) {
    return (
      <div className="flex flex-1 items-center justify-center text-xs text-ink-muted">
        正在读取待审变更单…
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      {/* Top Action Bar */}
      <div className="flex shrink-0 items-center justify-between gap-2 border-b border-border-subtle bg-surface/60 px-3.5 py-2.5">
        <div className="min-w-0">
          <div className="text-xs font-medium text-ink-primary">
            {delta ? "待审记忆变更单" : "暂无待审变更单"}
          </div>
          {delta && (
            <div className="mt-0.5 flex items-center gap-1.5 text-[11px] text-ink-muted">
              <span className="nib-mono">
                {counts.selected}/{counts.total} 项拟入库
              </span>
              {delta.highRiskFlags > 0 && (
                <span className="rounded bg-danger-soft px-1.5 py-0.5 text-[10px] font-medium text-danger">
                  {delta.highRiskFlags} 高危
                </span>
              )}
            </div>
          )}
        </div>

        {extracting ? (
          <button
            type="button"
            onClick={stopExtract}
            className="flex shrink-0 items-center gap-1 rounded-md border border-danger/40 bg-danger-soft px-2.5 py-1 text-[11px] font-medium text-danger transition-colors hover:border-danger"
          >
            <Square className="h-3 w-3 fill-current" />
            <span>取消抽取</span>
          </button>
        ) : (
          <button
            type="button"
            disabled={!chapterHasContent || applying}
            onClick={() => void handleExtract()}
            title={
              chapterHasContent
                ? "使用 extract 路由分析本章正文并生成待审变更提案"
                : "请先生成或撰写正文后再抽取变更"
            }
            className="flex shrink-0 items-center gap-1 rounded-md border border-accent/35 bg-accent-soft px-2.5 py-1 text-[11px] font-medium text-accent transition-colors hover:border-accent disabled:opacity-40"
          >
            <Wand2 className="h-3 w-3" />
            <span>{delta ? "重新抽取" : "抽取本章变更"}</span>
          </button>
        )}
      </div>

      {/* Extracting Progress Indicator */}
      {extracting && (
        <div className="flex shrink-0 items-center gap-2 border-b border-border-subtle bg-accent-soft/60 px-3.5 py-2 text-xs text-accent">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          <span>正在分析本章正文、人物状态与伏笔变化…</span>
        </div>
      )}

      {/* Body */}
      {!delta ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
          <div className="text-xs leading-relaxed text-ink-muted">
            {chapterHasContent
              ? "点击「抽取本章变更」解析人物伤势、境界、道具易主与伏笔推进，经你勾选后才入库。"
              : "本章尚无正文内容。出稿或手动保存正文后可抽取记忆变更单。"}
          </div>
          {chapterHasContent && !extracting && (
            <button
              type="button"
              onClick={() => void handleExtract()}
              className="flex items-center gap-1.5 rounded-md bg-accent px-3.5 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90"
            >
              <Wand2 className="h-3.5 w-3.5" />
              <span>抽取本章变更</span>
            </button>
          )}
        </div>
      ) : (
        <>
          <div className="flex-1 overflow-y-auto px-3.5 py-3">
            <div className="flex flex-col gap-3.5">
              {/* Causal Summary */}
              <div className="rounded-lg border border-border-subtle bg-canvas/70 p-3">
                <label className="mb-1.5 flex cursor-pointer items-center justify-between gap-2">
                  <span className="text-xs font-medium text-ink-primary">
                    本章因果摘要
                  </span>
                  <input
                    type="checkbox"
                    checked={applyCausal}
                    onChange={(e) => setApplyCausal(e.target.checked)}
                    className="h-3.5 w-3.5 accent-[var(--accent)]"
                  />
                </label>
                <textarea
                  rows={3}
                  disabled={!applyCausal}
                  value={causalText}
                  onChange={(e) => setCausalText(e.target.value)}
                  placeholder="150~250 字，记录核心因果转折与战利品落袋"
                  className="w-full resize-y rounded-md border border-border-subtle bg-surface/70 px-2.5 py-1.5 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none disabled:opacity-45"
                />
              </div>

              {/* Story Timeline */}
              {(delta.storyTimeLabel ||
                (delta.storyDay !== undefined && delta.storyDay !== null)) && (
                <div className="rounded-lg border border-border-subtle bg-canvas/70 p-3">
                  <label className="mb-1.5 flex cursor-pointer items-center justify-between gap-2">
                    <span className="inline-flex items-center gap-1.5 text-xs font-medium text-ink-primary">
                      <Clock className="h-3.5 w-3.5 text-accent" />
                      <span>本章故事内时间</span>
                    </span>
                    <input
                      type="checkbox"
                      checked={applyTimeline}
                      onChange={(e) => setApplyTimeline(e.target.checked)}
                      className="h-3.5 w-3.5 accent-[var(--accent)]"
                    />
                  </label>
                  <div className="grid grid-cols-[1fr_92px] gap-2">
                    <input
                      type="text"
                      disabled={!applyTimeline}
                      value={timelineLabel}
                      onChange={(e) => setTimelineLabel(e.target.value)}
                      placeholder="时间标签"
                      className="rounded-md border border-border-subtle bg-surface/70 px-2.5 py-1 text-xs text-ink-primary focus:border-accent focus:outline-none disabled:opacity-45"
                    />
                    <input
                      type="number"
                      disabled={!applyTimeline}
                      value={timelineDay}
                      onChange={(e) => setTimelineDay(e.target.value)}
                      placeholder="Day"
                      className="nib-mono rounded-md border border-border-subtle bg-surface/70 px-2 py-1 text-xs text-ink-primary focus:border-accent focus:outline-none disabled:opacity-45"
                    />
                  </div>
                </div>
              )}

              {/* State Patches */}
              {(delta.statePatches ?? []).length > 0 && (
                <div className="flex flex-col gap-2">
                  <div className="text-[11px] font-medium text-ink-secondary">
                    状态变更（{delta.statePatches.length}）
                  </div>
                  {delta.statePatches.map((patch) => {
                    const checked = acceptedPatches[patch.id] ?? true;
                    const isHighRisk = patch.severity === "high_risk";
                    return (
                      <div
                        key={patch.id}
                        className={`rounded-lg border p-3 transition-colors ${
                          isHighRisk
                            ? "border-danger/45 bg-danger-soft/35"
                            : "border-border-subtle bg-canvas/70"
                        } ${checked ? "" : "opacity-55"}`}
                      >
                        <div className="flex items-start gap-2">
                          <input
                            type="checkbox"
                            checked={checked}
                            onChange={(e) =>
                              setAcceptedPatches((prev) => ({
                                ...prev,
                                [patch.id]: e.target.checked,
                              }))
                            }
                            className="mt-0.5 h-3.5 w-3.5 shrink-0 accent-[var(--accent)]"
                          />
                          <div className="min-w-0 flex-1">
                            <div className="flex flex-wrap items-center gap-1.5">
                              {isHighRisk && (
                                <span className="inline-flex items-center gap-0.5 rounded border border-danger/45 bg-danger-soft px-1.5 py-0.5 text-[10px] font-semibold text-danger">
                                  <ShieldAlert className="h-2.5 w-2.5" />
                                  高危
                                </span>
                              )}
                              <span className="text-xs font-medium text-ink-primary">
                                {patch.entityName || `实体 #${patch.entityId}`}
                              </span>
                              <span className="nib-mono text-[11px] text-ink-muted">
                                · {patch.attrKey}
                              </span>
                            </div>

                            <div className="mt-1.5 flex flex-wrap items-center gap-1.5 text-xs">
                              <span className="rounded bg-surface px-1.5 py-0.5 text-ink-muted line-through">
                                {patch.prevValue || "无"}
                              </span>
                              <ArrowRight className="h-3 w-3 shrink-0 text-ink-muted" />
                              <span
                                className={`rounded px-1.5 py-0.5 font-medium ${
                                  isHighRisk
                                    ? "bg-danger-soft text-danger"
                                    : "bg-ok-soft text-ok"
                                }`}
                              >
                                {patch.newValue || "清空"}
                              </span>
                            </div>

                            {patch.reason && (
                              <div className="mt-1 text-[11px] leading-relaxed text-ink-secondary">
                                {patch.reason}
                              </div>
                            )}

                            {checked && (
                              <input
                                type="text"
                                value={patchOverrides[patch.id] ?? ""}
                                onChange={(e) =>
                                  setPatchOverrides((prev) => ({
                                    ...prev,
                                    [patch.id]: e.target.value,
                                  }))
                                }
                                placeholder="微调覆写新值（留空则采用建议值）"
                                className="mt-2 w-full rounded border border-border-subtle bg-surface px-2 py-1 text-[11.5px] text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                              />
                            )}
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}

              {/* New Entities */}
              {(delta.newEntities ?? []).length > 0 && (
                <div className="flex flex-col gap-2">
                  <div className="text-[11px] font-medium text-ink-secondary">
                    新登场实体（{delta.newEntities.length}）
                  </div>
                  {delta.newEntities.map((ent) => {
                    const checked = acceptedNewEntities[ent.id] ?? true;
                    return (
                      <div
                        key={ent.id}
                        className={`rounded-lg border border-border-subtle bg-canvas/70 p-3 transition-opacity ${
                          checked ? "" : "opacity-55"
                        }`}
                      >
                        <div className="flex items-start gap-2">
                          <input
                            type="checkbox"
                            checked={checked}
                            onChange={(e) =>
                              setAcceptedNewEntities((prev) => ({
                                ...prev,
                                [ent.id]: e.target.checked,
                              }))
                            }
                            className="mt-0.5 h-3.5 w-3.5 shrink-0 accent-[var(--accent)]"
                          />
                          <div className="min-w-0 flex-1">
                            <div className="flex flex-wrap items-center gap-1.5">
                              <span className="inline-flex items-center gap-0.5 rounded bg-accent-soft px-1.5 py-0.5 text-[10px] font-medium text-accent">
                                <Sparkles className="h-2.5 w-2.5" />
                                新增 · {getEntityKindLabel(ent.kind)}
                              </span>
                              <span className="text-xs font-medium text-ink-primary">
                                {ent.name}
                              </span>
                              {ent.isInvariant && (
                                <span className="rounded bg-danger-soft px-1 text-[10px] text-danger">
                                  铁律
                                </span>
                              )}
                            </div>
                            {ent.summary && (
                              <p className="mt-1 text-[11.5px] leading-relaxed text-ink-secondary">
                                {ent.summary}
                              </p>
                            )}
                            {ent.attributes &&
                              Object.keys(ent.attributes).length > 0 && (
                                <div className="mt-1.5 flex flex-wrap gap-1">
                                  {Object.entries(ent.attributes).map(([k, v]) => (
                                    <span
                                      key={k}
                                      className="rounded bg-surface px-1.5 py-0.5 text-[10.5px] text-ink-secondary"
                                    >
                                      {k}: {v}
                                    </span>
                                  ))}
                                </div>
                              )}
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}

              {/* Foreshadowing Updates */}
              {(delta.foreshadows ?? []).length > 0 && (
                <div className="flex flex-col gap-2">
                  <div className="text-[11px] font-medium text-ink-secondary">
                    伏笔变动（{delta.foreshadows.length}）
                  </div>
                  {delta.foreshadows.map((fs) => {
                    const checked = acceptedForeshadows[fs.id] ?? true;
                    const actionMeta = getDeltaForeshadowActionMeta(fs.action);
                    return (
                      <div
                        key={fs.id}
                        className={`rounded-lg border border-border-subtle bg-canvas/70 p-3 transition-opacity ${
                          checked ? "" : "opacity-55"
                        }`}
                      >
                        <div className="flex items-start gap-2">
                          <input
                            type="checkbox"
                            checked={checked}
                            onChange={(e) =>
                              setAcceptedForeshadows((prev) => ({
                                ...prev,
                                [fs.id]: e.target.checked,
                              }))
                            }
                            className="mt-0.5 h-3.5 w-3.5 shrink-0 accent-[var(--accent)]"
                          />
                          <div className="min-w-0 flex-1">
                            <div className="flex flex-wrap items-center gap-1.5">
                              <span
                                className={`rounded border px-1.5 py-0.5 text-[10px] font-medium ${actionMeta.badgeClass}`}
                              >
                                {actionMeta.label}
                              </span>
                              <span className="text-xs font-medium text-ink-primary">
                                {fs.title || `伏笔 #${fs.foreshadowingId}`}
                              </span>
                            </div>
                            {fs.description && (
                              <p className="mt-1 text-[11.5px] leading-relaxed text-ink-secondary">
                                {fs.description}
                              </p>
                            )}
                            {fs.targetResolveSeq > 0 && (
                              <div className="nib-mono mt-1 text-[10.5px] text-ink-muted">
                                计划回收：第 {fs.targetResolveSeq} 章
                              </div>
                            )}
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}

              {/* Relation Changes */}
              {(delta.relationChanges ?? []).length > 0 && (
                <div className="flex flex-col gap-2">
                  <div className="text-[11px] font-medium text-ink-secondary">
                    关系变动（{delta.relationChanges.length}）
                  </div>
                  {delta.relationChanges.map((rel) => {
                    const checked = acceptedRelations[rel.id] ?? true;
                    const isEnd = rel.action === "end";
                    return (
                      <div
                        key={rel.id}
                        className={`rounded-lg border border-border-subtle bg-canvas/70 p-3 transition-opacity ${
                          checked ? "" : "opacity-55"
                        }`}
                      >
                        <div className="flex items-start gap-2">
                          <input
                            type="checkbox"
                            checked={checked}
                            onChange={(e) =>
                              setAcceptedRelations((prev) => ({
                                ...prev,
                                [rel.id]: e.target.checked,
                              }))
                            }
                            className="mt-0.5 h-3.5 w-3.5 shrink-0 accent-[var(--accent)]"
                          />
                          <div className="min-w-0 flex-1">
                            <div className="flex flex-wrap items-center gap-1.5 text-xs">
                              <span
                                className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-medium ${
                                  isEnd
                                    ? "bg-danger-soft text-danger"
                                    : "bg-accent-soft text-accent"
                                }`}
                              >
                                <GitFork className="h-2.5 w-2.5" />
                                {isEnd ? "终止关系" : "新增/变更"}
                              </span>
                              <span className="font-medium text-ink-primary">
                                {rel.sourceName}
                              </span>
                              <span className="rounded border border-border-subtle bg-surface px-1.5 py-0.5 text-[10.5px] text-ink-secondary">
                                {rel.relationType}
                              </span>
                              <ArrowRight className="h-3 w-3 text-ink-muted" />
                              <span className="font-medium text-ink-primary">
                                {rel.targetName}
                              </span>
                            </div>
                            {(rel.description || rel.reason) && (
                              <p className="mt-1 text-[11.5px] leading-relaxed text-ink-secondary">
                                {rel.description || rel.reason}
                              </p>
                            )}
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}

              {/* Deadline Changes */}
              {(delta.deadlineChanges ?? []).length > 0 && (
                <div className="flex flex-col gap-2">
                  <div className="text-[11px] font-medium text-ink-secondary">
                    期限倒计时变动（{delta.deadlineChanges.length}）
                  </div>
                  {delta.deadlineChanges.map((dl) => {
                    const checked = acceptedDeadlines[dl.id] ?? true;
                    return (
                      <div
                        key={dl.id}
                        className={`rounded-lg border border-border-subtle bg-canvas/70 p-3 transition-opacity ${
                          checked ? "" : "opacity-55"
                        }`}
                      >
                        <div className="flex items-start gap-2">
                          <input
                            type="checkbox"
                            checked={checked}
                            onChange={(e) =>
                              setAcceptedDeadlines((prev) => ({
                                ...prev,
                                [dl.id]: e.target.checked,
                              }))
                            }
                            className="mt-0.5 h-3.5 w-3.5 shrink-0 accent-[var(--accent)]"
                          />
                          <div className="min-w-0 flex-1">
                            <div className="flex flex-wrap items-center gap-1.5">
                              <span className="inline-flex items-center gap-1 rounded bg-accent-soft px-1.5 py-0.5 text-[10px] font-medium text-accent">
                                <CalendarClock className="h-2.5 w-2.5" />
                                {getDeadlineActionLabel(dl.action)}
                              </span>
                              <span className="text-xs font-medium text-ink-primary">
                                {dl.title || `期限 #${dl.deadlineId}`}
                              </span>
                              <span className="nib-mono rounded bg-surface px-1.5 py-0.5 text-[10.5px] text-ink-secondary">
                                Day {dl.targetDay}
                                {dl.targetLabel ? ` · ${dl.targetLabel}` : ""}
                              </span>
                            </div>
                            {dl.note && (
                              <p className="mt-1 text-[11.5px] leading-relaxed text-ink-secondary">
                                {dl.note}
                              </p>
                            )}
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}

              {counts.total === 0 && (
                <div className="rounded-lg border border-dashed border-border-subtle px-3 py-4 text-center text-xs text-ink-muted">
                  本章未检测到实体状态或伏笔变更，仅可确认更新因果摘要。
                </div>
              )}
            </div>
          </div>

          {/* Sticky Confirm Footer */}
          <div className="shrink-0 border-t border-border-subtle bg-surface px-3.5 py-2.5">
            {counts.highRiskSelected > 0 && (
              <div className="mb-2 flex items-center gap-1.5 text-[11px] text-danger">
                <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
                <span>已勾选 {counts.highRiskSelected} 条高危变更，入库前请复核</span>
              </div>
            )}
            <button
              type="button"
              disabled={applying || extracting}
              onClick={() => void handleApply()}
              className="flex w-full items-center justify-center gap-1.5 rounded-md bg-accent px-3 py-2 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
            >
              {applying ? (
                <>
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  <span>正在入库…</span>
                </>
              ) : (
                <>
                  <Check className="h-3.5 w-3.5" />
                  <span>确认选定项入库</span>
                </>
              )}
            </button>
          </div>
        </>
      )}
    </div>
  );
}
