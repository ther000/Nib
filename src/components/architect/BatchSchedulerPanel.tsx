import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { isMyGo } from "mygo-runtime";
import {
  AlertCircle,
  CheckCircle2,
  Circle,
  Loader2,
  Play,
  RotateCcw,
  Sparkles,
  Square,
  XCircle,
} from "lucide-react";
import {
  BatchService,
  events,
  type BatchMode,
  type BatchRun,
  type BatchRunItem,
  type Chapter,
} from "../../mygo";
import { formatErrorMessage, formatSeq } from "../../lib/constants";

interface BatchSchedulerPanelProps {
  bookId: number;
  volumeChapters: Chapter[];
  hasVolume: boolean;
  onOpenOutlineModal: () => void;
  onJumpToChapterStudio: (chapterId: number) => Promise<void>;
  onBatchUpdated: () => Promise<void>;
  onNotify: (input: {
    tone: "info" | "ok" | "warn" | "danger";
    message: string;
    detail?: string;
  }) => void;
}

const BATCH_MODES: Array<{ value: BatchMode; label: string; hint: string }> = [
  { value: "draft_only", label: "仅出草稿", hint: "出稿 + 0 成本本地扫描" },
  { value: "draft_and_polish", label: "草稿 + 审校润色", hint: "加跑 Critic 与手术润色" },
  { value: "full_pipeline", label: "全流水线接力", hint: "含完章状态抽取喂给下章" },
];

const ITEM_STATE_LABELS: Record<string, string> = {
  queued: "排队中",
  compiling: "编译简报…",
  drafting: "正文生成中…",
  linting: "本地去AI味扫描…",
  critiquing: "Critic 盲审中…",
  polishing: "手术润色中…",
  extracting: "抽取状态变更…",
  awaiting_review: "待审改",
  completed: "已完成",
  failed: "失败",
  canceled: "已中止",
};

function isActiveState(state: string): boolean {
  return (
    state === "compiling" ||
    state === "drafting" ||
    state === "linting" ||
    state === "critiquing" ||
    state === "polishing" ||
    state === "extracting"
  );
}

export function BatchSchedulerPanel({
  bookId,
  volumeChapters,
  hasVolume,
  onOpenOutlineModal,
  onJumpToChapterStudio,
  onBatchUpdated,
  onNotify,
}: BatchSchedulerPanelProps) {
  const [selectedIds, setSelectedIds] = useState<number[]>([]);
  const [mode, setMode] = useState<BatchMode>("full_pipeline");
  const [batchRun, setBatchRun] = useState<BatchRun | null>(null);
  const [liveMessage, setLiveMessage] = useState("");
  const [starting, setStarting] = useState(false);
  const [canceling, setCanceling] = useState(false);
  const [retryingItemId, setRetryingItemId] = useState<number | null>(null);

  const mountedRef = useRef(true);
  const bookIdRef = useRef(bookId);
  const terminalEventRef = useRef<{ batchId: number; at: number } | null>(null);
  bookIdRef.current = bookId;

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  // Keep selectedIds pruned to current volume chapters
  useEffect(() => {
    const valid = new Set(volumeChapters.map((c) => c.id));
    setSelectedIds((prev) => prev.filter((id) => valid.has(id)));
  }, [volumeChapters]);

  const loadLatestBatch = useCallback(async () => {
    try {
      const latest = await BatchService.getLatestBatchRun(bookId);
      if (mountedRef.current && bookIdRef.current === bookId) {
        setBatchRun(latest);
      }
    } catch {
      if (mountedRef.current && bookIdRef.current === bookId) {
        setBatchRun(null);
      }
    }
  }, [bookId]);

  useEffect(() => {
    setLiveMessage("");
    void loadLatestBatch();
  }, [loadLatestBatch]);

  // Subscribe to real-time Go events.batchProgress
  useEffect(() => {
    if (!isMyGo()) return;
    let unsub: (() => void) | null = null;
    try {
      unsub = events.batchProgress.on((prog) => {
        if (!mountedRef.current || prog.bookId !== bookIdRef.current) return;
        setLiveMessage(prog.message || "");

        setBatchRun((prev) => {
          if (!prev || prev.id !== prog.batchRunId) return prev;
          return {
            ...prev,
            status: prog.status,
            doneItems: prog.doneItems,
            totalItems: prog.totalItems,
            items: prev.items.map((it) =>
              it.id === prog.currentItemId
                ? { ...it, state: prog.currentState }
                : it
            ),
          };
        });

        // Refresh full batch & chapters on item finish or terminal batch status
        const itemFinished =
          prog.currentState === "awaiting_review" ||
          prog.currentState === "completed" ||
          prog.currentState === "failed" ||
          prog.currentState === "canceled";
        const runFinished =
          prog.status === "completed" ||
          prog.status === "failed" ||
          prog.status === "canceled";

        if (runFinished) {
          terminalEventRef.current = {
            batchId: prog.batchRunId,
            at: Date.now(),
          };
        }

        if (itemFinished || runFinished) {
          void BatchService.getBatchRun(prog.batchRunId)
            .then((fresh) => {
              if (mountedRef.current && bookIdRef.current === prog.bookId) {
                setBatchRun(fresh);
              }
            })
            .catch(() => {
              // ignore transient errors
            });
          void onBatchUpdated();
        }
      });
    } catch {
      // ignore outside MyGo
    }
    return () => {
      if (unsub) unsub();
    };
  }, [onBatchUpdated]);

  const isRunning = batchRun?.status === "running";

  const toggleChapterSelection = (id: number) => {
    setSelectedIds((prev) => {
      if (prev.includes(id)) return prev.filter((x) => x !== id);
      if (prev.length >= 5) {
        onNotify({ tone: "warn", message: "单次串行微批最多选择 5 章" });
        return prev;
      }
      return [...prev, id];
    });
  };

  const selectPendingChapters = () => {
    const candidates = volumeChapters
      .filter(
        (c) =>
          c.status === "planned" ||
          c.status === "outlined" ||
          c.status === "partial" ||
          c.status === "upstream_dirty"
      )
      .slice(0, 5)
      .map((c) => c.id);
    if (candidates.length === 0) {
      onNotify({ tone: "info", message: "本卷暂无待写章节" });
      return;
    }
    setSelectedIds(candidates);
  };

  const orderedSelectedIds = useMemo(() => {
    const set = new Set(selectedIds);
    return volumeChapters.filter((c) => set.has(c.id)).map((c) => c.id);
  }, [volumeChapters, selectedIds]);

  const handleStartBatch = async () => {
    if (starting || isRunning) return;
    if (orderedSelectedIds.length === 0) {
      onNotify({ tone: "warn", message: "请先勾选 1~5 章进入微批队列" });
      return;
    }
    setStarting(true);
    setLiveMessage("");
    const startedAt = Date.now();
    terminalEventRef.current = null;
    try {
      const created = await BatchService.startBatchRun(
        bookId,
        orderedSelectedIds,
        mode
      );
      const termEv = terminalEventRef.current as {
        batchId: number;
        at: number;
      } | null;
      const fastTerminated =
        termEv !== null &&
        termEv.batchId === created.id &&
        termEv.at >= startedAt;
      setBatchRun((prev) =>
        prev &&
        prev.id === created.id &&
        prev.status !== "running" &&
        prev.status !== "paused"
          ? prev
          : created
      );
      if (fastTerminated) {
        void BatchService.getBatchRun(created.id)
          .then((fresh) => {
            if (mountedRef.current && bookIdRef.current === bookId) {
              setBatchRun(fresh);
            }
          })
          .catch(() => {
            // ignore transient errors
          });
      }
      onNotify({
        tone: "ok",
        message: `已启动 ${created.totalItems} 章串行微批`,
        detail: "逐章接力执行，支持随时停止并保底留稿",
      });
    } catch (err) {
      onNotify({
        tone: "danger",
        message: "启动串行微批失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setStarting(false);
    }
  };

  const handleCancelBatch = async () => {
    if (!batchRun || canceling) return;
    setCanceling(true);
    try {
      const updated = await BatchService.cancelBatchRun(batchRun.id);
      setBatchRun(updated);
      onNotify({
        tone: "info",
        message: "已请求停止微批队列",
        detail: "当前出稿章节将保底保存为 partial",
      });
    } catch (err) {
      onNotify({
        tone: "danger",
        message: "停止微批失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setCanceling(false);
    }
  };

  const handleRetryItem = async (item: BatchRunItem) => {
    if (retryingItemId !== null || isRunning) return;
    setRetryingItemId(item.id);
    const startedAt = Date.now();
    terminalEventRef.current = null;
    try {
      const updated = await BatchService.retryBatchItem(item.id);
      const termEv = terminalEventRef.current as {
        batchId: number;
        at: number;
      } | null;
      const fastTerminated =
        termEv !== null &&
        termEv.batchId === updated.id &&
        termEv.at >= startedAt;
      if (fastTerminated) {
        setBatchRun((prev) =>
          prev &&
          prev.id === updated.id &&
          prev.status !== "running" &&
          prev.status !== "paused"
            ? prev
            : updated
        );
        void BatchService.getBatchRun(updated.id)
          .then((fresh) => {
            if (mountedRef.current && bookIdRef.current === bookId) {
              setBatchRun(fresh);
            }
          })
          .catch(() => {
            // ignore transient errors
          });
      } else {
        setBatchRun(updated);
      }
      onNotify({
        tone: "ok",
        message:
          item.state === "queued"
            ? `已恢复执行第 ${formatSeq(item.chapterGlobalSeq)} 章`
            : `已重新排队第 ${formatSeq(item.chapterGlobalSeq)} 章`,
      });
    } catch (err) {
      onNotify({
        tone: "danger",
        message: "重跑失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setRetryingItemId(null);
    }
  };

  return (
    <aside
      className="flex shrink-0 flex-col border-l border-border-subtle bg-surface"
      style={{ width: "var(--sidebar-batch-width)" }}
    >
      {/* Top Action: Batch Outline */}
      <div className="border-b border-border-subtle p-3">
        <button
          type="button"
          disabled={!hasVolume}
          onClick={onOpenOutlineModal}
          className="flex w-full items-center justify-center gap-1.5 rounded-md border border-accent/40 bg-accent-soft px-3 py-2 text-xs font-medium text-accent transition-colors hover:border-accent disabled:opacity-40"
        >
          <Sparkles className="h-3.5 w-3.5" />
          <span>批量生成章纲与节拍</span>
        </button>
      </div>

      {/* Serial Micro-Batch Selector */}
      <div className="flex flex-col gap-2.5 border-b border-border-subtle p-3">
        <div className="flex items-center justify-between">
          <span className="text-xs font-medium text-ink-primary">
            串行微批调度（{orderedSelectedIds.length}/5）
          </span>
          <div className="flex items-center gap-1.5 text-[11px]">
            <button
              type="button"
              disabled={isRunning || volumeChapters.length === 0}
              onClick={selectPendingChapters}
              className="text-accent hover:opacity-80 disabled:opacity-35"
            >
              选待写章
            </button>
            {orderedSelectedIds.length > 0 && (
              <button
                type="button"
                disabled={isRunning}
                onClick={() => setSelectedIds([])}
                className="text-ink-muted hover:text-ink-primary disabled:opacity-35"
              >
                清空
              </button>
            )}
          </div>
        </div>

        {/* Volume chapter checklist */}
        <div className="max-h-36 overflow-y-auto rounded-lg border border-border-subtle bg-canvas/70 p-1.5">
          {volumeChapters.length === 0 ? (
            <div className="px-2 py-3 text-center text-[11px] text-ink-muted">
              当前卷暂无可选章节
            </div>
          ) : (
            <div className="flex flex-col gap-0.5">
              {volumeChapters.map((chap) => {
                const checked = selectedIds.includes(chap.id);
                const disabled =
                  isRunning || (!checked && selectedIds.length >= 5);
                return (
                  <label
                    key={chap.id}
                    className={`flex items-center gap-2 rounded px-1.5 py-1 text-xs select-none ${
                      disabled
                        ? "opacity-45"
                        : "cursor-pointer hover:bg-surface"
                    }`}
                  >
                    <input
                      type="checkbox"
                      disabled={disabled}
                      checked={checked}
                      onChange={() => toggleChapterSelection(chap.id)}
                      className="h-3.5 w-3.5 shrink-0 accent-[var(--accent)]"
                    />
                    <span className="nib-mono shrink-0 text-[10.5px] text-ink-muted">
                      {formatSeq(chap.globalSeq)}
                    </span>
                    <span className="min-w-0 flex-1 truncate text-[11.5px] text-ink-primary">
                      {chap.title}
                    </span>
                  </label>
                );
              })}
            </div>
          )}
        </div>

        {/* Mode Selector */}
        <div>
          <select
            disabled={isRunning}
            value={mode}
            onChange={(e) => setMode(e.target.value)}
            className="w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none disabled:opacity-50"
          >
            {BATCH_MODES.map((m) => (
              <option key={m.value} value={m.value}>
                {m.label} · {m.hint}
              </option>
            ))}
          </select>
        </div>

        {/* Start / Stop Button */}
        {isRunning ? (
          <button
            type="button"
            disabled={canceling}
            onClick={() => void handleCancelBatch()}
            className="flex w-full items-center justify-center gap-1.5 rounded-md border border-danger/45 bg-danger-soft px-3 py-2 text-xs font-semibold text-danger transition-colors hover:border-danger disabled:opacity-50"
          >
            <Square className="h-3 w-3 fill-current" />
            <span>{canceling ? "正在停稳队列…" : "停止队列"}</span>
          </button>
        ) : (
          <button
            type="button"
            disabled={starting || orderedSelectedIds.length === 0}
            onClick={() => void handleStartBatch()}
            className="flex w-full items-center justify-center gap-1.5 rounded-md bg-accent px-3 py-2 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
          >
            {starting ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Play className="h-3 w-3 fill-current" />
            )}
            <span>启动串行微批</span>
          </button>
        )}
      </div>

      {/* Live Queue Status */}
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden p-3">
        <div className="mb-2 flex items-center justify-between text-xs">
          <span className="font-medium text-ink-secondary">队列状态（串行）</span>
          {batchRun && (
            <span className="nib-mono text-[11px] text-ink-muted">
              {batchRun.doneItems} / {batchRun.totalItems}
            </span>
          )}
        </div>

        {liveMessage && isRunning && (
          <div className="mb-2 rounded-md border border-accent/30 bg-accent-soft/60 px-2.5 py-1.5 text-[11px] text-accent">
            {liveMessage}
          </div>
        )}

        {!batchRun || batchRun.items.length === 0 ? (
          <div className="flex flex-1 items-center justify-center px-3 text-center text-[11.5px] leading-relaxed text-ink-muted">
            勾选 1~5 章后启动串行接力，完章待审状态自动喂给下一章防吃书。
          </div>
        ) : (
          <div className="flex flex-1 flex-col gap-1.5 overflow-y-auto">
            {batchRun.items.map((item) => {
              const active = isActiveState(item.state);
              const done =
                item.state === "awaiting_review" || item.state === "completed";
              const failed = item.state === "failed";
              const canceled = item.state === "canceled";
              const queued = item.state === "queued";
              const canRetry = !isRunning && (failed || canceled || queued);
              const label = ITEM_STATE_LABELS[item.state] ?? item.state;

              return (
                <div
                  key={item.id}
                  onClick={() => void onJumpToChapterStudio(item.chapterId)}
                  title="点击前往审改台查看本章"
                  className={`cursor-pointer rounded-lg border px-2.5 py-2 transition-colors ${
                    active
                      ? "border-accent/45 bg-accent-soft/45"
                      : failed
                      ? "border-danger/35 bg-danger-soft/25"
                      : "border-border-subtle bg-canvas/70 hover:border-border-strong"
                  }`}
                >
                  <div className="flex items-center gap-2">
                    {active ? (
                      <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-accent" />
                    ) : done ? (
                      <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-ok" />
                    ) : failed ? (
                      <XCircle className="h-3.5 w-3.5 shrink-0 text-danger" />
                    ) : canceled ? (
                      <AlertCircle className="h-3.5 w-3.5 shrink-0 text-warn" />
                    ) : (
                      <Circle className="h-3 w-3 shrink-0 text-ink-muted" />
                    )}

                    <span className="nib-mono shrink-0 text-[11px] text-ink-muted">
                      {formatSeq(item.chapterGlobalSeq)}
                    </span>
                    <span className="min-w-0 flex-1 truncate text-xs text-ink-primary">
                      {item.chapterTitle || `第${item.chapterGlobalSeq}章`}
                    </span>
                  </div>

                  <div className="mt-1 flex items-center justify-between gap-2 pl-5 text-[10.5px]">
                    <span
                      className={
                        active
                          ? "font-medium text-accent"
                          : done
                          ? "text-ok"
                          : failed
                          ? "text-danger"
                          : canceled
                          ? "text-warn"
                          : "text-ink-muted"
                      }
                    >
                      {label}
                    </span>

                    {canRetry && (
                      <button
                        type="button"
                        disabled={retryingItemId === item.id}
                        onClick={(e) => {
                          e.stopPropagation();
                          void handleRetryItem(item);
                        }}
                        className="flex items-center gap-1 rounded bg-elevated px-1.5 py-0.5 text-[10.5px] font-medium text-accent hover:bg-accent-soft disabled:opacity-45"
                      >
                        {queued ? (
                          <Play className="h-2.5 w-2.5 fill-current" />
                        ) : (
                          <RotateCcw className="h-2.5 w-2.5" />
                        )}
                        <span>{queued ? "继续" : "重试"}</span>
                      </button>
                    )}
                  </div>

                  {item.errorMsg && (
                    <div className="mt-1 pl-5 text-[10.5px] leading-snug text-danger break-words">
                      {formatErrorMessage(item.errorMsg)}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </aside>
  );
}
