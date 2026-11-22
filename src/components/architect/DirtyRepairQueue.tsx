import { useCallback, useEffect, useState } from "react";
import {
  AlertTriangle,
  CheckCheck,
  ChevronDown,
  ChevronUp,
  ExternalLink,
  RefreshCw,
  ShieldCheck,
  Sparkles,
} from "lucide-react";
import {
  MemoryService,
  StyleService,
  type DirtyChapterItem,
} from "../../mygo";
import { useApp } from "../../stores/AppContext";
import { formatErrorMessage, formatSeq } from "../../lib/constants";

interface DirtyRepairQueueProps {
  bookId: number;
  compact?: boolean;
  onOpenFullAudit?: () => void;
  onChanged?: () => void;
}

export function DirtyRepairQueue({
  bookId,
  compact = false,
  onOpenFullAudit,
  onChanged,
}: DirtyRepairQueueProps) {
  const {
    activeChapterId,
    memoryRevision,
    jumpToChapter,
    refreshChapters,
    refreshActiveChapter,
    bumpMemoryRevision,
    notify,
  } = useApp();

  const [items, setItems] = useState<DirtyChapterItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [expanded, setExpanded] = useState(!compact);
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
  const [busyAction, setBusyAction] = useState<string | null>(null);

  const loadDirty = useCallback(async () => {
    if (!bookId || bookId <= 0) {
      setItems([]);
      return;
    }
    setLoading(true);
    try {
      const list = await MemoryService.listDirtyChapters(bookId);
      setItems(list);
      setSelectedIds((prev) => {
        const validIds = new Set(list.map((i) => i.chapterId));
        const next = new Set<number>();
        for (const id of prev) {
          if (validIds.has(id)) next.add(id);
        }
        return next;
      });
    } catch (err) {
      notify({
        tone: "danger",
        message: "读取脏标修复队列失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setLoading(false);
    }
  }, [bookId, notify]);

  useEffect(() => {
    void loadDirty();
  }, [loadDirty, memoryRevision]);

  const syncAfterMutation = async (chapterId?: number) => {
    await Promise.all([refreshChapters(bookId), loadDirty()]);
    if (chapterId && activeChapterId === chapterId) {
      await refreshActiveChapter(chapterId);
    } else if (activeChapterId) {
      await refreshActiveChapter(activeChapterId);
    }
    bumpMemoryRevision();
    onChanged?.();
  };

  const handleReExtract = async (item: DirtyChapterItem) => {
    if (!item.hasContent) {
      notify({ tone: "warn", message: `第 ${formatSeq(item.globalSeq)} 章暂无正文，无法重抽记忆` });
      return;
    }
    setBusyAction(`extract:${item.chapterId}`);
    try {
      await MemoryService.extractChapterDelta(item.chapterId);
      await syncAfterMutation(item.chapterId);
      notify({
        tone: "ok",
        message: `已重抽第 ${formatSeq(item.globalSeq)} 章记忆变更单，请到写作台右侧审阅`,
      });
    } catch (err) {
      notify({
        tone: "danger",
        message: "重抽记忆变更单失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setBusyAction(null);
    }
  };

  const handleRunCritic = async (item: DirtyChapterItem) => {
    if (!item.hasContent) {
      notify({ tone: "warn", message: `第 ${formatSeq(item.globalSeq)} 章暂无正文，无法执行 Critic 复核` });
      return;
    }
    setBusyAction(`critic:${item.chapterId}`);
    try {
      const audit = await StyleService.runCriticAudit(item.chapterId);
      await syncAfterMutation(item.chapterId);
      notify({
        tone: audit.criticFindings.length > 0 ? "warn" : "ok",
        message: `第 ${formatSeq(item.globalSeq)} 章 Critic 复核完成：检出 ${audit.criticFindings.length} 项建议`,
      });
    } catch (err) {
      notify({
        tone: "danger",
        message: "Critic 复核失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setBusyAction(null);
    }
  };

  const handleClear = async (ids: number[]) => {
    setBusyAction(ids.length === 1 ? `clear:${ids[0]}` : "clear:batch");
    try {
      const res = await MemoryService.clearDirtyChapters(bookId, ids);
      await syncAfterMutation(ids.length === 1 ? ids[0] : undefined);
      notify({
        tone: "ok",
        message: `已清除 ${res.clearedCount} 个章节的「前文有变动」标记`,
      });
    } catch (err) {
      notify({
        tone: "danger",
        message: "清除脏标失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setBusyAction(null);
    }
  };

  const toggleSelect = (cid: number) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(cid)) next.delete(cid);
      else next.add(cid);
      return next;
    });
  };

  if (compact && !loading && items.length === 0) {
    return null;
  }

  return (
    <section className="rounded-xl border border-warn/40 bg-warn-soft/20">
      {/* Header Strip */}
      <div className="flex flex-wrap items-center justify-between gap-2 px-4 py-2.5">
        <button
          type="button"
          onClick={() => compact && setExpanded((v) => !v)}
          className={`flex items-center gap-2 text-left text-xs ${
            compact ? "cursor-pointer" : "cursor-default"
          }`}
        >
          <AlertTriangle className="h-4 w-4 shrink-0 text-warn" />
          <span className="font-semibold text-ink-primary">
            下游脏标（upstream_dirty）修复队列
          </span>
          <span className="nib-mono rounded-full bg-warn-soft px-2 py-0.5 text-[10.5px] font-semibold text-warn">
            {items.length} 章待核对
          </span>
          {compact &&
            (expanded ? (
              <ChevronUp className="h-3.5 w-3.5 text-ink-muted" />
            ) : (
              <ChevronDown className="h-3.5 w-3.5 text-ink-muted" />
            ))}
        </button>

        <div className="flex flex-wrap items-center gap-1.5">
          {items.length > 0 && selectedIds.size > 0 && (
            <button
              type="button"
              disabled={busyAction !== null}
              onClick={() => void handleClear(Array.from(selectedIds))}
              className="flex items-center gap-1 rounded-md border border-accent/40 bg-accent-soft px-2.5 py-1 text-[11px] font-medium text-accent hover:border-accent disabled:opacity-45"
            >
              <CheckCheck className="h-3 w-3" />
              <span>清除已选 ({selectedIds.size})</span>
            </button>
          )}
          {items.length > 0 && (
            <button
              type="button"
              disabled={busyAction !== null}
              onClick={() => void handleClear([])}
              className="flex items-center gap-1 rounded-md border border-border-subtle bg-surface px-2.5 py-1 text-[11px] text-ink-secondary hover:border-border-strong hover:text-ink-primary disabled:opacity-45"
            >
              <ShieldCheck className="h-3 w-3 text-ok" />
              <span>全部消标</span>
            </button>
          )}
          {compact && onOpenFullAudit && (
            <button
              type="button"
              onClick={onOpenFullAudit}
              className="rounded-md px-2 py-1 text-[11px] text-accent hover:bg-surface"
            >
              全书体检
            </button>
          )}
        </div>
      </div>

      {/* Queue Items */}
      {(!compact || expanded) && (
        <div className="flex flex-col gap-2 border-t border-warn/25 px-4 py-3">
          {loading && items.length === 0 ? (
            <div className="py-6 text-center text-xs text-ink-muted">
              正在检查受上游变动影响的章节…
            </div>
          ) : items.length === 0 ? (
            <div className="py-8 text-center text-xs text-ink-secondary">
              全书当前没有标记为「前文有变动」的脏标章节
            </div>
          ) : (
            items.map((item) => {
              const extracting = busyAction === `extract:${item.chapterId}`;
              const critiquing = busyAction === `critic:${item.chapterId}`;
              const clearing = busyAction === `clear:${item.chapterId}`;

              return (
                <div
                  key={item.chapterId}
                  className="flex flex-col gap-2 rounded-lg border border-border-subtle bg-surface px-3.5 py-2.5"
                >
                  <div className="flex flex-wrap items-start justify-between gap-2">
                    <div className="flex min-w-0 flex-1 items-start gap-2">
                      <input
                        type="checkbox"
                        checked={selectedIds.has(item.chapterId)}
                        onChange={() => toggleSelect(item.chapterId)}
                        className="mt-0.5 h-3.5 w-3.5 shrink-0 accent-[var(--accent)]"
                      />
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-1.5 text-xs font-medium text-ink-primary">
                          <span className="nib-mono rounded bg-canvas px-1.5 py-0.5 text-[11px] text-accent">
                            第 {formatSeq(item.globalSeq)} 章
                          </span>
                          <span className="truncate">{item.title}</span>
                          <span className="nib-mono text-[10.5px] text-ink-muted">
                            {item.hasContent ? `${item.wordCount} 字` : "尚无正文"}
                          </span>
                          {item.hasPendingDelta && (
                            <span className="rounded bg-warn-soft px-1.5 py-0.5 text-[10px] text-warn">
                              待审变更单
                            </span>
                          )}
                          {item.lastAuditFindings > 0 && (
                            <span className="rounded bg-danger-soft px-1.5 py-0.5 text-[10px] text-danger">
                              上次审校 {item.lastAuditFindings} 项
                            </span>
                          )}
                        </div>
                        <p className="mt-1 text-[11px] text-ink-secondary">
                          {item.upstreamSummary}
                        </p>
                      </div>
                    </div>

                    <div className="flex flex-wrap items-center gap-1.5">
                      <button
                        type="button"
                        onClick={() => void jumpToChapter(item.chapterId)}
                        className="flex items-center gap-1 rounded border border-border-subtle bg-canvas px-2 py-1 text-[11px] text-ink-primary hover:border-accent hover:text-accent"
                      >
                        <ExternalLink className="h-3 w-3" />
                        <span>去修改</span>
                      </button>
                      <button
                        type="button"
                        disabled={!item.hasContent || busyAction !== null}
                        onClick={() => void handleReExtract(item)}
                        className="flex items-center gap-1 rounded border border-border-subtle bg-canvas px-2 py-1 text-[11px] text-ink-secondary hover:border-border-strong hover:text-ink-primary disabled:opacity-40"
                      >
                        <RefreshCw
                          className={`h-3 w-3 ${extracting ? "animate-spin" : ""}`}
                        />
                        <span>{extracting ? "抽取中…" : "重抽记忆"}</span>
                      </button>
                      <button
                        type="button"
                        disabled={!item.hasContent || busyAction !== null}
                        onClick={() => void handleRunCritic(item)}
                        className="flex items-center gap-1 rounded border border-border-subtle bg-canvas px-2 py-1 text-[11px] text-ink-secondary hover:border-border-strong hover:text-ink-primary disabled:opacity-40"
                      >
                        <Sparkles
                          className={`h-3 w-3 text-accent ${
                            critiquing ? "animate-pulse" : ""
                          }`}
                        />
                        <span>{critiquing ? "复核中…" : "Critic 复核"}</span>
                      </button>
                      <button
                        type="button"
                        disabled={busyAction !== null}
                        onClick={() => void handleClear([item.chapterId])}
                        className="flex items-center gap-1 rounded bg-ok-soft px-2 py-1 text-[11px] font-medium text-ok hover:opacity-85 disabled:opacity-40"
                      >
                        <ShieldCheck className="h-3 w-3" />
                        <span>{clearing ? "消标中…" : "清除脏标"}</span>
                      </button>
                    </div>
                  </div>
                </div>
              );
            })
          )}
        </div>
      )}
    </section>
  );
}
