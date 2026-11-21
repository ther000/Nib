import { useEffect, useMemo, useState } from "react";
import { Check, Loader2, Sparkles, X } from "lucide-react";
import {
  MemoryService,
  type ReverseExtractChunk,
  type ReverseExtractPreview,
} from "../mygo";
import { useApp } from "../stores/AppContext";
import { useMygoChannel } from "../hooks/useMygoStream";
import { formatErrorMessage } from "../lib/constants";

interface SelectedState {
  entities: boolean[];
  relations: boolean[];
  foreshadowings: boolean[];
  arcSummaries: boolean[];
}

const EMPTY_SELECTION: SelectedState = { entities: [], relations: [], foreshadowings: [], arcSummaries: [] };

export function ReverseExtractModal() {
  const { activeBook, refreshBookData, refreshEntities, refreshForeshadowings, notify } = useApp();
  const stream = useMygoChannel<ReverseExtractChunk>();

  const [open, setOpen] = useState(false);
  const [bookId, setBookId] = useState<number | null>(null);
  const [startSeq, setStartSeq] = useState("1");
  const [endSeq, setEndSeq] = useState("");
  const [maxChapters, setMaxChapters] = useState("20");
  const [running, setRunning] = useState(false);
  const [progressText, setProgressText] = useState("");
  const [preview, setPreview] = useState<ReverseExtractPreview | null>(null);
  const [selection, setSelection] = useState<SelectedState>(EMPTY_SELECTION);
  const [applying, setApplying] = useState(false);

  useEffect(() => {
    const handler = (event: Event) => {
      const detail = (event as CustomEvent<{ bookId?: number }>).detail;
      const targetId = detail?.bookId ?? activeBook?.id;
      if (!targetId) return;
      setBookId(targetId);
      setOpen(true);
      setPreview(null);
      setSelection(EMPTY_SELECTION);
      setProgressText("");
      setStartSeq("1");
      setEndSeq("");
      setMaxChapters("20");
    };
    window.addEventListener("nib:reverse-extract", handler);
    return () => window.removeEventListener("nib:reverse-extract", handler);
  }, [activeBook?.id]);

  const selectedCount = useMemo(() => {
    if (!preview) return 0;
    return (
      selection.entities.filter(Boolean).length +
      selection.relations.filter(Boolean).length +
      selection.foreshadowings.filter(Boolean).length +
      selection.arcSummaries.filter(Boolean).length
    );
  }, [preview, selection]);

  const handleStart = async () => {
    if (!bookId || running) return;
    const parsedStart = Number.parseInt(startSeq, 10) || 0;
    const parsedEnd = endSeq.trim() ? Number.parseInt(endSeq, 10) : 0;
    const parsedMax = Number.parseInt(maxChapters, 10) || 20;
    if (parsedStart < 0 || (parsedEnd > 0 && parsedEnd < parsedStart)) {
      notify({ tone: "warn", message: "章节范围不正确" });
      return;
    }
    if (parsedMax < 1 || parsedMax > 100) {
      notify({ tone: "warn", message: "采样章数需在 1 到 100 之间" });
      return;
    }
    setRunning(true);
    setPreview(null);
    setProgressText("正在读取样章…");
    const { channel, runId } = stream.open((chunk) => {
      if (!stream.isCurrent(runId)) return;
      if (chunk.kind === "delta" && chunk.delta) {
        setProgressText((prev) => {
          const next = prev + chunk.delta;
          return next.length > 800 ? next.slice(-800) : next;
        });
      } else if (chunk.kind === "started") {
        setProgressText("正在分析人物、势力、道具与伏笔…");
      } else if (chunk.kind === "error" && chunk.error) {
        setProgressText(chunk.error);
      }
    });
    try {
      const result = await MemoryService.reverseExtractBook(
        { bookId, startSeq: parsedStart, endSeq: parsedEnd, maxChapters: parsedMax },
        channel
      );
      setPreview(result);
      setSelection({
        entities: result.entities.map(() => true),
        relations: result.relations.map(() => true),
        foreshadowings: result.foreshadowings.map(() => true),
        arcSummaries: result.arcSummaries.map(() => true),
      });
      setProgressText("");
    } catch (err) {
      if (!stream.wasStopped()) {
        notify({ tone: "danger", message: "AI 反推设定失败", detail: formatErrorMessage(err) });
      }
    } finally {
      stream.release(channel);
      setRunning(false);
    }
  };

  const handleApply = async () => {
    if (!bookId || !preview || selectedCount === 0) return;
    setApplying(true);
    try {
      const filtered: ReverseExtractPreview = {
        entities: preview.entities.filter((_, index) => selection.entities[index]),
        relations: preview.relations.filter((_, index) => selection.relations[index]),
        foreshadowings: preview.foreshadowings.filter((_, index) => selection.foreshadowings[index]),
        arcSummaries: preview.arcSummaries.filter((_, index) => selection.arcSummaries[index]),
        warnings: preview.warnings,
      };
      const result = await MemoryService.applyReverseExtract({ bookId, preview: filtered });
      await refreshBookData(bookId, null);
      await refreshEntities(bookId);
      await refreshForeshadowings(bookId);
      notify({
        tone: "ok",
        message: `已写入 ${result.entitiesCreated + result.entitiesUpdated} 个设定、${result.relationsCreated} 条关系、${result.foreshadowings} 条伏笔`,
      });
      setOpen(false);
    } catch (err) {
      notify({ tone: "danger", message: "写入反推结果失败", detail: formatErrorMessage(err) });
    } finally {
      setApplying(false);
    }
  };

  const close = () => {
    if (running) stream.stop();
    setOpen(false);
    setPreview(null);
    setProgressText("");
  };

  if (!open) return null;

  const toggle = (group: keyof SelectedState, index: number) => {
    setSelection((prev) => {
      const next = { ...prev, [group]: [...prev[group]] };
      next[group][index] = !next[group][index];
      return next;
    });
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/55 p-4">
      <div className="flex max-h-[86vh] w-full max-w-[760px] flex-col overflow-hidden rounded-xl border border-border-strong bg-elevated shadow-[var(--shadow-elevated)]">
        <div className="flex items-center justify-between border-b border-border-subtle px-5 py-3">
          <div className="flex items-center gap-2">
            <Sparkles className="h-4 w-4 text-accent" />
            <h2 className="text-sm font-medium text-ink-primary">AI 反推旧稿设定</h2>
          </div>
          <button
            type="button"
            onClick={close}
            aria-label="关闭"
            className="rounded-lg p-1 text-ink-muted transition-colors hover:bg-surface hover:text-ink-primary"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto px-5 py-4">
          <div className="grid grid-cols-3 gap-3">
            <label className="text-xs text-ink-secondary">
              起始章
              <input
                type="number"
                min={0}
                value={startSeq}
                onChange={(e) => setStartSeq(e.target.value)}
                className="nib-mono mt-1 w-full rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
              />
            </label>
            <label className="text-xs text-ink-secondary">
              结束章
              <input
                type="number"
                min={0}
                placeholder="留空为起始章之后"
                value={endSeq}
                onChange={(e) => setEndSeq(e.target.value)}
                className="nib-mono mt-1 w-full rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
              />
            </label>
            <label className="text-xs text-ink-secondary">
              最多采样章数
              <input
                type="number"
                min={1}
                max={100}
                value={maxChapters}
                onChange={(e) => setMaxChapters(e.target.value)}
                className="nib-mono mt-1 w-full rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
              />
            </label>
          </div>

          <div className="mt-3 flex items-center gap-2">
            <button
              type="button"
              disabled={running}
              onClick={() => void handleStart()}
              className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
            >
              {running ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Sparkles className="h-3.5 w-3.5" />}
              <span>{running ? "反推中…" : "开始反推"}</span>
            </button>
            {running && (
              <button
                type="button"
                onClick={() => {
                  stream.stop();
                  setRunning(false);
                }}
                className="rounded-lg border border-border-subtle bg-surface px-3 py-1.5 text-xs text-ink-secondary hover:border-border-strong"
              >
                停止
              </button>
            )}
          </div>

          {running && (
            <div className="mt-3 max-h-40 overflow-y-auto rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed whitespace-pre-wrap text-ink-secondary">
              {progressText || "正在等待模型返回…"}
            </div>
          )}

          {preview && (
            <div className="mt-4 flex flex-col gap-4">
              {preview.warnings.length > 0 && (
                <div className="rounded-lg border border-warn/35 bg-warn-soft/40 px-3 py-2 text-xs text-warn">
                  {preview.warnings.join(" ")}
                </div>
              )}

              {[
                {
                  key: "entities" as const,
                  title: `设定实体 ${preview.entities.length}`,
                  rows: preview.entities.map((item) => ({
                    label: item.name,
                    meta: `${item.kind}${item.existingMatch ? " · 已有档案将更新" : ""}`,
                    detail: item.summary,
                  })),
                },
                {
                  key: "relations" as const,
                  title: `关系 ${preview.relations.length}`,
                  rows: preview.relations.map((item) => ({
                    label: `${item.sourceName} → ${item.targetName}`,
                    meta: item.relationType,
                    detail: item.description,
                  })),
                },
                {
                  key: "foreshadowings" as const,
                  title: `伏笔 ${preview.foreshadowings.length}`,
                  rows: preview.foreshadowings.map((item) => ({
                    label: item.title,
                    meta: `第 ${item.plantedChapterSeq} 章埋设`,
                    detail: item.description,
                  })),
                },
                {
                  key: "arcSummaries" as const,
                  title: `摘要 ${preview.arcSummaries.length}`,
                  rows: preview.arcSummaries.map((item) => ({
                    label: `第 ${item.startSeq}–${item.endSeq} 章`,
                    meta: item.level,
                    detail: item.summary,
                  })),
                },
              ].map((group) =>
                group.rows.length === 0 ? null : (
                  <section key={group.key}>
                    <div className="mb-1.5 text-xs font-medium text-ink-secondary">{group.title}</div>
                    <div className="divide-y divide-border-subtle overflow-hidden rounded-lg border border-border-subtle bg-surface/50">
                      {group.rows.map((row, index) => (
                        <label key={`${group.key}-${index}`} className="flex cursor-pointer items-start gap-2.5 px-3 py-2">
                          <input
                            type="checkbox"
                            checked={selection[group.key][index] ?? false}
                            onChange={() => toggle(group.key, index)}
                            className="mt-0.5 h-3.5 w-3.5 accent-[var(--accent)]"
                          />
                          <span className="min-w-0">
                            <span className="text-xs font-medium text-ink-primary">{row.label}</span>
                            <span className="ml-2 text-[11px] text-ink-muted">{row.meta}</span>
                            {row.detail && (
                              <span className="mt-0.5 block text-[11px] leading-relaxed text-ink-secondary">{row.detail}</span>
                            )}
                          </span>
                        </label>
                      ))}
                    </div>
                  </section>
                )
              )}
            </div>
          )}
        </div>

        <div className="flex items-center justify-between border-t border-border-subtle px-5 py-3">
          <span className="text-xs text-ink-muted">
            {preview ? `已选择 ${selectedCount} 项` : "先采样旧稿章节，确认后再写入设定账本"}
          </span>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={close}
              className="rounded-lg border border-border-subtle bg-surface px-3 py-1.5 text-xs text-ink-secondary transition-colors hover:border-border-strong"
            >
              取消
            </button>
            <button
              type="button"
              disabled={!preview || selectedCount === 0 || applying}
              onClick={() => void handleApply()}
              className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
            >
              <Check className="h-3.5 w-3.5" />
              <span>{applying ? "写入中…" : "确认写入"}</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
