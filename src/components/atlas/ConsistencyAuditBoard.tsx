import { useCallback, useEffect, useMemo, useState } from "react";
import {
  AlertCircle,
  AlertTriangle,
  CheckCircle2,
  ExternalLink,
  EyeOff,
  Loader2,
  RefreshCw,
  RotateCcw,
  ShieldCheck,
  Sparkles,
  Trash2,
} from "lucide-react";
import {
  MemoryService,
  type BookConsistencyReport,
  type ConsistencyAuditScopeType,
  type ConsistencyFindingStatus,
  type LocalAuditFinding,
  type LocalAuditReport,
} from "../../mygo";
import { useApp } from "../../stores/AppContext";
import {
  LOCAL_AUDIT_CATEGORIES,
  formatErrorMessage,
  formatLatency,
  formatSeq,
  formatTimestamp,
  getAIAuditCategoryMeta,
  getLocalAuditCategoryLabel,
} from "../../lib/constants";
import { DirtyRepairQueue } from "../architect/DirtyRepairQueue";
import { ConfirmDialog, type ConfirmDialogState } from "../ui/ConfirmDialog";

type AuditInnerTab = "local" | "ai" | "dirty";

const FINDING_STATUS_LABEL: Record<string, string> = {
  open: "待处理",
  resolved: "已处理",
  ignored: "已忽略",
};

function readIgnoredLocalIds(bookId: number): Set<string> {
  try {
    const raw = window.localStorage.getItem(`nib:ignored-local-audit:${bookId}`);
    if (!raw) return new Set();
    const arr = JSON.parse(raw);
    return Array.isArray(arr) ? new Set(arr.filter((x): x is string => typeof x === "string")) : new Set();
  } catch {
    return new Set();
  }
}

function writeIgnoredLocalIds(bookId: number, ids: Set<string>) {
  try {
    if (ids.size === 0) {
      window.localStorage.removeItem(`nib:ignored-local-audit:${bookId}`);
    } else {
      window.localStorage.setItem(
        `nib:ignored-local-audit:${bookId}`,
        JSON.stringify(Array.from(ids))
      );
    }
  } catch {
    // ignore storage quota errors
  }
}

export function ConsistencyAuditBoard() {
  const {
    activeBook,
    volumes,
    chapters,
    memoryRevision,
    jumpToChapter,
    jumpToEntity,
    selectChapter,
    setActiveTab,
    setAtlasSubTab,
    notify,
  } = useApp();

  const [innerTab, setInnerTab] = useState<AuditInnerTab>("local");

  // 第一层：快速体检（本地）
  const [absentThreshold, setAbsentThreshold] = useState(20);
  const [maxDayJump, setMaxDayJump] = useState(365);
  const [categoryFilter, setCategoryFilter] = useState<string>("all");
  const [localReport, setLocalReport] = useState<LocalAuditReport | null>(null);
  const [localLoading, setLocalLoading] = useState(false);
  const [ignoredLocalIds, setIgnoredLocalIds] = useState<Set<string>>(() =>
    activeBook ? readIgnoredLocalIds(activeBook.id) : new Set()
  );

  useEffect(() => {
    setIgnoredLocalIds(activeBook ? readIgnoredLocalIds(activeBook.id) : new Set());
  }, [activeBook]);

  const ignoreLocalFinding = (id: string) => {
    if (!activeBook) return;
    setIgnoredLocalIds((prev) => {
      const next = new Set(prev);
      next.add(id);
      writeIgnoredLocalIds(activeBook.id, next);
      return next;
    });
  };

  const restoreIgnoredLocal = () => {
    if (!activeBook) return;
    const empty = new Set<string>();
    setIgnoredLocalIds(empty);
    writeIgnoredLocalIds(activeBook.id, empty);
  };

  // 第二层：深度剧情审计（AI）
  const [scopeType, setScopeType] = useState<ConsistencyAuditScopeType>("volume");
  const [selectedVolumeId, setSelectedVolumeId] = useState<string>("");
  const [startSeqDraft, setStartSeqDraft] = useState("1");
  const [endSeqDraft, setEndSeqDraft] = useState("30");
  const [aiReports, setAiReports] = useState<BookConsistencyReport[]>([]);
  const [activeReportId, setActiveReportId] = useState<number | null>(null);
  const [aiRunning, setAiRunning] = useState(false);
  const [aiListLoading, setAiListLoading] = useState(false);
  const [updatingFindingKey, setUpdatingFindingKey] = useState<string | null>(null);
  const [confirmDialog, setConfirmDialog] = useState<ConfirmDialogState | null>(null);
  const [confirmBusy, setConfirmBusy] = useState(false);

  const latestSeq = useMemo(
    () => chapters.reduce((max, c) => (c.globalSeq > max ? c.globalSeq : max), 0),
    [chapters]
  );

  const dirtyCount = useMemo(
    () => chapters.filter((c) => c.status === "upstream_dirty").length,
    [chapters]
  );

  useEffect(() => {
    if (volumes.length > 0 && !selectedVolumeId) {
      setSelectedVolumeId(String(volumes[0]!.id));
    }
  }, [volumes, selectedVolumeId]);

  useEffect(() => {
    if (latestSeq > 0) {
      setEndSeqDraft(String(latestSeq));
    }
  }, [latestSeq]);

  const runLocalScan = useCallback(async () => {
    if (!activeBook) {
      setLocalReport(null);
      return;
    }
    setLocalLoading(true);
    try {
      const rep = await MemoryService.runLocalConsistencyAudit(activeBook.id, {
        absentThresholdChapters: absentThreshold,
        maxStoryDayJump: maxDayJump,
      });
      setLocalReport(rep);
    } catch (err) {
      notify({
        tone: "danger",
        message: "本地全书体检失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setLocalLoading(false);
    }
  }, [activeBook, absentThreshold, maxDayJump, notify]);

  useEffect(() => {
    void runLocalScan();
  }, [runLocalScan, memoryRevision]);

  const loadAIReports = useCallback(async () => {
    if (!activeBook) {
      setAiReports([]);
      setActiveReportId(null);
      return;
    }
    setAiListLoading(true);
    try {
      const list = await MemoryService.listConsistencyReports(activeBook.id);
      setAiReports(list);
      setActiveReportId((prev) => {
        if (prev && list.some((r) => r.id === prev)) return prev;
        return list[0]?.id ?? null;
      });
    } catch (err) {
      notify({
        tone: "danger",
        message: "加载 AI 深度审计报告失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setAiListLoading(false);
    }
  }, [activeBook, notify]);

  useEffect(() => {
    void loadAIReports();
  }, [loadAIReports]);

  const handleRunAIAudit = async () => {
    if (!activeBook) return;
    let volumeId = 0;
    let startSeq = 0;
    let endSeq = 0;

    if (scopeType === "volume") {
      volumeId = Number.parseInt(selectedVolumeId, 10) || 0;
      if (!volumeId) {
        notify({ tone: "warn", message: "请先选择要审计的分卷" });
        return;
      }
    } else if (scopeType === "range") {
      startSeq = Number.parseInt(startSeqDraft, 10) || 0;
      endSeq = Number.parseInt(endSeqDraft, 10) || 0;
      if (startSeq <= 0 || endSeq < startSeq) {
        notify({ tone: "warn", message: "请填写有效的起止章号范围" });
        return;
      }
    }

    setAiRunning(true);
    try {
      const rep = await MemoryService.runAIConsistencyAudit({
        bookId: activeBook.id,
        scopeType,
        volumeId,
        startSeq,
        endSeq,
      });
      await loadAIReports();
      setActiveReportId(rep.id);
      notify({
        tone: rep.findings.length > 0 ? "warn" : "ok",
        message: `AI 深度语义审计完成：第 ${rep.startSeq}~${rep.endSeq} 章检出 ${rep.findings.length} 项问题`,
      });
    } catch (err) {
      notify({
        tone: "danger",
        message: "AI 深度语义审计失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setAiRunning(false);
    }
  };

  const handleUpdateFindingStatus = async (
    reportId: number,
    findingId: string,
    status: ConsistencyFindingStatus
  ) => {
    const key = `${reportId}:${findingId}`;
    setUpdatingFindingKey(key);
    try {
      const updated = await MemoryService.updateConsistencyFindingStatus(
        reportId,
        findingId,
        status
      );
      setAiReports((prev) => prev.map((r) => (r.id === updated.id ? updated : r)));
    } catch (err) {
      notify({
        tone: "danger",
        message: "更新问题状态失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setUpdatingFindingKey(null);
    }
  };

  const requestDeleteReport = (rep: BookConsistencyReport) => {
    setConfirmDialog({
      title: "删除审计报告",
      message: `确定删除「第 ${formatSeq(rep.startSeq)}–${formatSeq(rep.endSeq)} 章」的 AI 审计报告？删除后无法恢复。`,
      confirmLabel: "删除",
      danger: true,
      onConfirm: async () => {
        setConfirmBusy(true);
        try {
          await MemoryService.deleteConsistencyReport(rep.id);
          setConfirmDialog(null);
          await loadAIReports();
          notify({ tone: "ok", message: "已删除审计报告" });
        } catch (err) {
          notify({
            tone: "danger",
            message: "删除审计报告失败",
            detail: formatErrorMessage(err),
          });
        } finally {
          setConfirmBusy(false);
        }
      },
    });
  };

  const handleJumpFromLocalFinding = async (f: LocalAuditFinding) => {
    switch (f.jumpTarget) {
      case "studio":
        if (f.chapterId > 0) await jumpToChapter(f.chapterId);
        break;
      case "architect":
        if (f.chapterId > 0) {
          await selectChapter(f.chapterId);
        }
        setActiveTab("architect");
        break;
      case "atlas_entity":
        if (f.entityId > 0) {
          jumpToEntity(f.entityId);
        } else {
          setAtlasSubTab("entities");
        }
        break;
      case "atlas_foreshadow":
        setAtlasSubTab("foreshadowings");
        break;
      case "dirty_queue":
        setInnerTab("dirty");
        break;
      default:
        if (f.chapterId > 0) await jumpToChapter(f.chapterId);
    }
  };

  const handleJumpToChapterSeq = async (seq: number) => {
    const ch = chapters.find((c) => c.globalSeq === seq);
    if (!ch) {
      notify({ tone: "warn", message: `未找到第 ${seq} 章` });
      return;
    }
    await jumpToChapter(ch.id);
  };

  const activeLocalFindings = useMemo(() => {
    const all = localReport?.findings ?? [];
    return all.filter((f) => !ignoredLocalIds.has(f.id));
  }, [localReport, ignoredLocalIds]);

  const ignoredLocalCount = useMemo(() => {
    const all = localReport?.findings ?? [];
    return all.length - activeLocalFindings.length;
  }, [localReport, activeLocalFindings]);

  const localCategoryCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const f of activeLocalFindings) {
      counts[f.category] = (counts[f.category] ?? 0) + 1;
    }
    return counts;
  }, [activeLocalFindings]);

  const filteredLocalFindings = useMemo(() => {
    if (categoryFilter === "all") return activeLocalFindings;
    return activeLocalFindings.filter((f) => f.category === categoryFilter);
  }, [activeLocalFindings, categoryFilter]);

  const activeAIReport = useMemo(
    () => aiReports.find((r) => r.id === activeReportId) ?? aiReports[0] ?? null,
    [aiReports, activeReportId]
  );

  if (!activeBook) {
    return null;
  }

  return (
    <div className="flex-1 overflow-y-auto px-6 py-5">
      <div className="mx-auto flex w-full max-w-[1160px] flex-col gap-4">
        {/* Top Mode Switcher */}
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border-subtle bg-surface px-4 py-2.5">
          <div className="flex flex-wrap items-center gap-1">
            <button
              type="button"
              onClick={() => setInnerTab("local")}
              className={`flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
                innerTab === "local"
                  ? "bg-accent-soft text-accent"
                  : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
              }`}
            >
              <ShieldCheck className="h-3.5 w-3.5" />
              <span>快速体检（本地）</span>
              {activeLocalFindings.length > 0 && (
                <span className="nib-mono rounded-full bg-danger-soft px-1.5 py-0.5 text-[11px] text-danger">
                  {activeLocalFindings.length}
                </span>
              )}
            </button>

            <button
              type="button"
              onClick={() => setInnerTab("ai")}
              className={`flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
                innerTab === "ai"
                  ? "bg-accent-soft text-accent"
                  : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
              }`}
            >
              <Sparkles className="h-3.5 w-3.5" />
              <span>深度剧情审计（AI）</span>
              {aiReports.length > 0 && (
                <span className="nib-mono rounded-full bg-canvas px-1.5 py-0.5 text-[11px] text-ink-muted">
                  {aiReports.length}
                </span>
              )}
            </button>

            <button
              type="button"
              onClick={() => setInnerTab("dirty")}
              className={`flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
                innerTab === "dirty"
                  ? "bg-accent-soft text-accent"
                  : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
              }`}
            >
              <AlertTriangle className="h-3.5 w-3.5" />
              <span>前文改动待核对</span>
              {dirtyCount > 0 && (
                <span className="nib-mono rounded-full bg-warn-soft px-1.5 py-0.5 text-[11px] text-warn">
                  {dirtyCount}
                </span>
              )}
            </button>
          </div>

          {localReport && innerTab === "local" && (
            <span className="nib-mono text-[11px] text-ink-muted">
              已扫描 {localReport.scannedChapters} 章 · {localReport.scannedEntities}{" "}
              设定 · 耗时 {formatLatency(Math.max(1, localReport.elapsedMs))}
            </span>
          )}
        </div>

        {/* Layer 1: Zero-cost Deterministic Local Audit */}
        {innerTab === "local" && (
          <div className="flex flex-col gap-3">
            {/* Threshold & Refresh Bar */}
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border-subtle bg-surface px-4 py-2.5 text-xs">
              <div className="flex flex-wrap items-center gap-4">
                <label className="flex items-center gap-1.5 text-ink-secondary">
                  <span>掉线告警阈值</span>
                  <select
                    value={absentThreshold}
                    onChange={(e) => setAbsentThreshold(Number(e.target.value))}
                    className="nib-mono rounded border border-border-subtle bg-canvas px-2 py-1 text-xs text-ink-primary focus:border-accent focus:outline-none"
                  >
                    {[15, 20, 30, 50].map((n) => (
                      <option key={n} value={n}>
                        {n} 章
                      </option>
                    ))}
                  </select>
                </label>

                <label className="flex items-center gap-1.5 text-ink-secondary">
                  <span>时间跨度警戒</span>
                  <select
                    value={maxDayJump}
                    onChange={(e) => setMaxDayJump(Number(e.target.value))}
                    className="nib-mono rounded border border-border-subtle bg-canvas px-2 py-1 text-xs text-ink-primary focus:border-accent focus:outline-none"
                  >
                    {[180, 365, 730, 3650].map((d) => (
                      <option key={d} value={d}>
                        {d} 天
                      </option>
                    ))}
                  </select>
                </label>
              </div>

              <button
                type="button"
                disabled={localLoading}
                onClick={() => void runLocalScan()}
                className="flex items-center gap-1.5 rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs font-medium text-ink-primary hover:border-border-strong disabled:opacity-50"
              >
                <RefreshCw
                  className={`h-3.5 w-3.5 text-accent ${
                    localLoading ? "animate-spin" : ""
                  }`}
                />
                <span>重新体检</span>
              </button>
            </div>

            {/* Category Filter Chips */}
            {localReport && (
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex flex-wrap items-center gap-1.5">
                  <button
                    type="button"
                    onClick={() => setCategoryFilter("all")}
                    className={`rounded-full border px-3 py-1 text-xs transition-colors ${
                      categoryFilter === "all"
                        ? "border-accent bg-accent-soft font-medium text-accent"
                        : "border-border-subtle bg-surface text-ink-secondary hover:text-ink-primary"
                    }`}
                  >
                    全部 ({activeLocalFindings.length})
                  </button>
                  {LOCAL_AUDIT_CATEGORIES.map((cat) => {
                    const count = localCategoryCounts[cat.value] ?? 0;
                    return (
                      <button
                        key={cat.value}
                        type="button"
                        onClick={() => setCategoryFilter(cat.value)}
                        className={`rounded-full border px-3 py-1 text-xs transition-colors ${
                          categoryFilter === cat.value
                            ? "border-accent bg-accent-soft font-medium text-accent"
                            : "border-border-subtle bg-surface text-ink-secondary hover:text-ink-primary"
                        }`}
                      >
                        {cat.shortLabel} ({count})
                      </button>
                    );
                  })}
                </div>

                {ignoredLocalCount > 0 && (
                  <button
                    type="button"
                    onClick={restoreIgnoredLocal}
                    className="flex items-center gap-1 text-xs text-ink-muted hover:text-accent"
                  >
                    <RotateCcw className="h-3 w-3" />
                    <span>恢复已忽略的 {ignoredLocalCount} 项</span>
                  </button>
                )}
              </div>
            )}

            {/* Findings List */}
            {localLoading && !localReport ? (
              <div className="py-16 text-center text-xs text-ink-muted">
                正在扫描全书一致性…
              </div>
            ) : filteredLocalFindings.length === 0 ? (
              <div className="flex flex-col items-center gap-2 rounded-xl border border-dashed border-border-strong bg-surface/40 py-14 text-center">
                <CheckCircle2 className="h-7 w-7 text-ok" />
                <div className="text-xs font-medium text-ink-primary">
                  {categoryFilter === "all"
                    ? "全书快速体检全部通过"
                    : "当前分类下暂无提醒"}
                </div>
                <div className="text-xs text-ink-muted">
                  可切至「深度剧情审计（AI）」排查战力崩坏与人物动机冲突
                </div>
              </div>
            ) : (
              <div className="flex flex-col gap-2.5 pb-6">
                {filteredLocalFindings.map((f) => {
                  const isErr = f.severity === "error";
                  return (
                    <div
                      key={f.id}
                      className={`flex flex-col gap-2 rounded-xl border bg-surface p-4 ${
                        isErr ? "border-danger/45" : "border-warn/35"
                      }`}
                    >
                      <div className="flex flex-wrap items-start justify-between gap-2">
                        <div className="flex flex-wrap items-center gap-2">
                          <span
                            className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-medium ${
                              isErr
                                ? "bg-danger-soft text-danger"
                                : "bg-warn-soft text-warn"
                            }`}
                          >
                            {isErr ? (
                              <AlertCircle className="h-3 w-3" />
                            ) : (
                              <AlertTriangle className="h-3 w-3" />
                            )}
                            {getLocalAuditCategoryLabel(f.category)}
                          </span>
                          {f.chapterSeq > 0 && (
                            <span className="nib-mono rounded bg-canvas px-1.5 py-0.5 text-[11px] text-accent">
                              第 {formatSeq(f.chapterSeq)} 章
                            </span>
                          )}
                          <span className="text-sm font-medium text-ink-primary">
                            {f.title}
                          </span>
                        </div>

                        <div className="flex shrink-0 items-center gap-1.5">
                          <button
                            type="button"
                            onClick={() => ignoreLocalFinding(f.id)}
                            title="忽略此提醒（随时可恢复）"
                            className="flex items-center gap-1 rounded-md border border-border-subtle bg-canvas px-2 py-1 text-xs text-ink-muted hover:border-border-strong hover:text-ink-primary"
                          >
                            <EyeOff className="h-3 w-3" />
                            <span>忽略</span>
                          </button>
                          <button
                            type="button"
                            onClick={() => void handleJumpFromLocalFinding(f)}
                            className="flex items-center gap-1 rounded-md border border-border-subtle bg-canvas px-2.5 py-1 text-xs text-accent hover:border-accent"
                          >
                            <ExternalLink className="h-3 w-3" />
                            <span>
                              {f.jumpTarget === "studio"
                                ? "去写正文"
                                : f.jumpTarget === "architect"
                                ? "去大纲"
                                : f.jumpTarget === "atlas_entity"
                                ? "查看档案"
                                : f.jumpTarget === "dirty_queue"
                                ? "查看待核列表"
                                : "查看伏笔期限"}
                            </span>
                          </button>
                        </div>
                      </div>

                      <p className="text-xs leading-relaxed text-ink-secondary">
                        {f.detail}
                      </p>
                      {f.suggestion && (
                        <div className="rounded-lg bg-canvas/75 px-3 py-1.5 text-[11px] text-ink-muted">
                          建议：{f.suggestion}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        )}

        {/* Layer 2: On-Demand AI Deep Semantic Consistency Audit */}
        {innerTab === "ai" && (
          <div className="flex flex-col gap-4 pb-8">
            {/* Run Config Card */}
            <section className="flex flex-col gap-3 rounded-xl border border-border-subtle bg-surface p-4">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <h3 className="text-sm font-medium text-ink-primary">
                    AI 跨章剧情审计
                  </h3>
                  <p className="mt-0.5 text-xs text-ink-muted">
                    汇总前情摘要、状态演变与世界铁律，由 AI 检查跨章战力崩坏与人物动机冲突
                  </p>
                </div>

                <button
                  type="button"
                  disabled={aiRunning || chapters.length === 0}
                  onClick={() => void handleRunAIAudit()}
                  className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-45"
                >
                  {aiRunning ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Sparkles className="h-3.5 w-3.5" />
                  )}
                  <span>{aiRunning ? "深度审计中…" : "启动深度审计"}</span>
                </button>
              </div>

              <div className="flex flex-wrap items-center gap-3 text-xs">
                <div className="inline-flex rounded-lg border border-border-subtle bg-canvas p-0.5">
                  {(
                    [
                      ["volume", "按分卷"],
                      ["range", "按章号区间"],
                      ["book", "全书汇总"],
                    ] as const
                  ).map(([val, label]) => (
                    <button
                      key={val}
                      type="button"
                      onClick={() => setScopeType(val)}
                      className={`rounded-md px-2.5 py-1 text-xs transition-colors ${
                        scopeType === val
                          ? "bg-accent-soft font-medium text-accent"
                          : "text-ink-secondary hover:text-ink-primary"
                      }`}
                    >
                      {label}
                    </button>
                  ))}
                </div>

                {scopeType === "volume" && (
                  <select
                    value={selectedVolumeId}
                    onChange={(e) => setSelectedVolumeId(e.target.value)}
                    className="rounded-lg border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                  >
                    <option value="">选择分卷…</option>
                    {volumes.map((v) => (
                      <option key={v.id} value={v.id}>
                        第 {v.seq} 卷 · {v.title}
                      </option>
                    ))}
                  </select>
                )}

                {scopeType === "range" && (
                  <div className="flex items-center gap-1.5">
                    <span>第</span>
                    <input
                      type="number"
                      min={1}
                      value={startSeqDraft}
                      onChange={(e) => setStartSeqDraft(e.target.value)}
                      className="nib-mono w-20 rounded-md border border-border-subtle bg-canvas px-2 py-1 text-xs text-ink-primary focus:border-accent focus:outline-none"
                    />
                    <span>至第</span>
                    <input
                      type="number"
                      min={1}
                      value={endSeqDraft}
                      onChange={(e) => setEndSeqDraft(e.target.value)}
                      className="nib-mono w-20 rounded-md border border-border-subtle bg-canvas px-2 py-1 text-xs text-ink-primary focus:border-accent focus:outline-none"
                    />
                    <span>章</span>
                  </div>
                )}
              </div>
            </section>

            {/* Historical Reports Selector + Detail */}
            {aiListLoading && aiReports.length === 0 ? (
              <div className="py-12 text-center text-xs text-ink-muted">
                正在读取历史深度审计报告…
              </div>
            ) : aiReports.length === 0 ? (
              <div className="rounded-xl border border-dashed border-border-strong bg-surface/40 py-14 text-center text-xs text-ink-secondary">
                尚无 AI 深度语义审计记录，可在完成一卷后点击上方「启动深度审计」存档排查
              </div>
            ) : (
              <div className="grid grid-cols-1 gap-4 lg:grid-cols-[260px_1fr]">
                {/* Left Report History List */}
                <div className="flex flex-col gap-2">
                  {aiReports.map((rep) => {
                    const active = activeAIReport?.id === rep.id;
                    const openCount = rep.findings.filter(
                      (x) => x.status === "open"
                    ).length;
                    return (
                      <button
                        key={rep.id}
                        type="button"
                        onClick={() => setActiveReportId(rep.id)}
                        className={`flex flex-col gap-1 rounded-xl border p-3 text-left transition-colors ${
                          active
                            ? "border-accent bg-accent-soft/45"
                            : "border-border-subtle bg-surface hover:border-border-strong"
                        }`}
                      >
                        <div className="flex items-center justify-between gap-2 text-xs font-medium text-ink-primary">
                          <span>
                            第 {formatSeq(rep.startSeq)}–{formatSeq(rep.endSeq)} 章
                          </span>
                          <span
                            className={`nib-mono rounded px-1.5 py-0.5 text-[10px] ${
                              openCount > 0
                                ? "bg-warn-soft text-warn"
                                : "bg-ok-soft text-ok"
                            }`}
                          >
                            {openCount > 0 ? `${openCount} 待处理` : "已清零"}
                          </span>
                        </div>
                        <div className="text-[10.5px] text-ink-muted">
                          {formatTimestamp(rep.createdAt)} · {rep.modelName || "critic"}
                        </div>
                      </button>
                    );
                  })}
                </div>

                {/* Right Report Detail */}
                {activeAIReport && (
                  <div className="flex flex-col gap-3 rounded-xl border border-border-subtle bg-surface p-4">
                    <div className="flex flex-wrap items-start justify-between gap-2 border-b border-border-subtle pb-3">
                      <div>
                        <div className="text-sm font-medium text-ink-primary">
                          第 {formatSeq(activeAIReport.startSeq)}–
                          {formatSeq(activeAIReport.endSeq)} 章深度语义审计报告
                        </div>
                        <p className="mt-1 text-xs leading-relaxed text-ink-secondary">
                          {activeAIReport.summary}
                        </p>
                      </div>
                      <button
                        type="button"
                        onClick={() => requestDeleteReport(activeAIReport)}
                        title="删除此审计报告"
                        className="rounded p-1.5 text-ink-muted hover:bg-elevated hover:text-danger"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </div>

                    {activeAIReport.findings.length === 0 ? (
                      <div className="py-10 text-center text-xs text-ink-secondary">
                        本次深度审计未发现跨章战力、动机或规则冲突
                      </div>
                    ) : (
                      <div className="flex flex-col gap-3">
                        {activeAIReport.findings.map((f) => {
                          const catMeta = getAIAuditCategoryMeta(f.category);
                          const isBusy =
                            updatingFindingKey === `${activeAIReport.id}:${f.id}`;
                          const dimmed = f.status !== "open";

                          return (
                            <div
                              key={f.id}
                              className={`flex flex-col gap-2 rounded-xl border border-border-subtle bg-canvas/75 p-3.5 ${
                                dimmed ? "opacity-65" : ""
                              }`}
                            >
                              <div className="flex flex-wrap items-start justify-between gap-2">
                                <div className="flex flex-wrap items-center gap-1.5">
                                  <span
                                    className={`rounded border px-1.5 py-0.5 text-[10.5px] font-medium ${catMeta.badgeClass}`}
                                  >
                                    {catMeta.label}
                                  </span>
                                  <span className="text-sm font-medium text-ink-primary">
                                    {f.title}
                                  </span>
                                  <span className="rounded bg-surface px-1.5 py-0.5 text-[10px] text-ink-muted">
                                    {FINDING_STATUS_LABEL[f.status] ?? f.status}
                                  </span>
                                </div>

                                <div className="flex items-center gap-1">
                                  {f.status !== "resolved" && (
                                    <button
                                      type="button"
                                      disabled={isBusy}
                                      onClick={() =>
                                        void handleUpdateFindingStatus(
                                          activeAIReport.id,
                                          f.id,
                                          "resolved"
                                        )
                                      }
                                      className="flex items-center gap-1 rounded bg-ok-soft px-2 py-1 text-[11px] text-ok hover:opacity-85"
                                    >
                                      <CheckCircle2 className="h-3 w-3" />
                                      <span>已处理</span>
                                    </button>
                                  )}
                                  {f.status !== "ignored" && (
                                    <button
                                      type="button"
                                      disabled={isBusy}
                                      onClick={() =>
                                        void handleUpdateFindingStatus(
                                          activeAIReport.id,
                                          f.id,
                                          "ignored"
                                        )
                                      }
                                      className="flex items-center gap-1 rounded border border-border-subtle bg-surface px-2 py-1 text-[11px] text-ink-secondary hover:text-ink-primary"
                                    >
                                      <EyeOff className="h-3 w-3" />
                                      <span>忽略</span>
                                    </button>
                                  )}
                                  {f.status !== "open" && (
                                    <button
                                      type="button"
                                      disabled={isBusy}
                                      onClick={() =>
                                        void handleUpdateFindingStatus(
                                          activeAIReport.id,
                                          f.id,
                                          "open"
                                        )
                                      }
                                      className="flex items-center gap-1 rounded border border-border-subtle bg-surface px-2 py-1 text-[11px] text-ink-secondary hover:text-ink-primary"
                                    >
                                      <RotateCcw className="h-3 w-3" />
                                      <span>重开</span>
                                    </button>
                                  )}
                                </div>
                              </div>

                              <p className="text-xs leading-relaxed text-ink-secondary">
                                {f.description}
                              </p>
                              {f.evidence && (
                                <div className="text-[11px] text-ink-muted">
                                  依据：{f.evidence}
                                </div>
                              )}
                              {f.suggestion && (
                                <div className="rounded-lg bg-surface px-2.5 py-1.5 text-[11px] text-ink-secondary">
                                  建议：{f.suggestion}
                                </div>
                              )}

                              {(f.chapterSeqs.length > 0 || f.entityNames.length > 0) && (
                                <div className="flex flex-wrap items-center gap-1.5 pt-1">
                                  {f.chapterSeqs.map((seq) => (
                                    <button
                                      key={seq}
                                      type="button"
                                      onClick={() => void handleJumpToChapterSeq(seq)}
                                      className="nib-mono rounded border border-border-subtle bg-surface px-1.5 py-0.5 text-[10.5px] text-accent hover:border-accent"
                                    >
                                      跳至第 {formatSeq(seq)} 章
                                    </button>
                                  ))}
                                  {f.entityNames.map((name) => (
                                    <span
                                      key={name}
                                      className="rounded bg-surface px-1.5 py-0.5 text-[10.5px] text-ink-muted"
                                    >
                                      {name}
                                    </span>
                                  ))}
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
            )}
          </div>
        )}

        {/* Layer 3: Downstream Dirty Repair Queue */}
        {innerTab === "dirty" && (
          <DirtyRepairQueue bookId={activeBook.id} />
        )}
      </div>

      <ConfirmDialog
        dialog={confirmDialog}
        busy={confirmBusy}
        onClose={() => setConfirmDialog(null)}
      />
    </div>
  );
}
