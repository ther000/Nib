import { useEffect, useRef, useState } from "react";
import { Channel } from "mygo-runtime";
import { Loader2, Sparkles, Square, X } from "lucide-react";
import {
  LibraryService,
  type Chapter,
  type OutlineChunk,
  type Volume,
} from "../../mygo";
import { formatErrorMessage } from "../../lib/constants";

interface BatchOutlineModalProps {
  open: boolean;
  bookId: number;
  volume: Volume | null;
  defaultStartSeq: number;
  onClose: () => void;
  onGenerated: (chapters: Chapter[]) => Promise<void>;
  onNotify: (input: {
    tone: "info" | "ok" | "warn" | "danger";
    message: string;
    detail?: string;
  }) => void;
}

export function BatchOutlineModal({
  open,
  bookId,
  volume,
  defaultStartSeq,
  onClose,
  onGenerated,
  onNotify,
}: BatchOutlineModalProps) {
  const [startSeq, setStartSeq] = useState(String(defaultStartSeq));
  const [count, setCount] = useState("5");
  const [arcGoal, setArcGoal] = useState("");
  const [running, setRunning] = useState(false);
  const [streamPreview, setStreamPreview] = useState("");
  const [stageMsg, setStageMsg] = useState("");

  const abortRef = useRef<AbortController | null>(null);
  const channelRef = useRef<Channel<OutlineChunk> | null>(null);
  const fallbackTimerRef = useRef<number | null>(null);
  const userStoppedRef = useRef(false);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      if (fallbackTimerRef.current !== null) {
        window.clearTimeout(fallbackTimerRef.current);
      }
      try {
        channelRef.current?.close();
      } catch {
        // ignore
      }
      abortRef.current?.abort();
    };
  }, []);

  useEffect(() => {
    if (!open) return;
    setStartSeq(String(Math.max(1, defaultStartSeq)));
    setCount("5");
    setArcGoal(volume?.goal ?? "");
    setStreamPreview("");
    setStageMsg("");
  }, [open, defaultStartSeq, volume?.id, volume?.goal]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !running) onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, running, onClose]);

  if (!open || !volume) return null;

  const stopGeneration = () => {
    userStoppedRef.current = true;
    setStageMsg("正在取消生成…");
    try {
      channelRef.current?.close();
    } catch {
      // ignore
    }
    if (fallbackTimerRef.current !== null) {
      window.clearTimeout(fallbackTimerRef.current);
    }
    fallbackTimerRef.current = window.setTimeout(() => {
      abortRef.current?.abort();
    }, 3000);
  };

  const handleGenerate = async () => {
    if (running) return;
    const parsedStart = Math.max(1, Number.parseInt(startSeq, 10) || 1);
    const parsedCount = Math.min(10, Math.max(1, Number.parseInt(count, 10) || 5));

    const ac = new AbortController();
    abortRef.current = ac;
    userStoppedRef.current = false;
    setRunning(true);
    setStreamPreview("");
    setStageMsg(`正在调用 outline 路由生成 ${parsedCount} 章纲与节拍…`);

    let buf = "";
    const ch = new Channel<OutlineChunk>((chunk) => {
      if (!mountedRef.current) return;
      if (chunk.kind === "delta" && chunk.delta) {
        buf += chunk.delta;
        setStreamPreview(buf);
      } else if (chunk.kind === "completed") {
        setStageMsg("章纲解析完成，正在落库…");
      } else if (chunk.kind === "error" && chunk.error) {
        setStageMsg(formatErrorMessage(chunk.error));
      }
    });
    channelRef.current = ch;

    try {
      const created = await LibraryService.batchGenerateOutlines.with({
        signal: ac.signal,
      })(
        {
          bookId,
          volumeId: volume.id,
          startSeq: parsedStart,
          count: parsedCount,
          arcGoal: arcGoal.trim(),
        },
        ch
      );
      if (!mountedRef.current) return;
      await onGenerated(created);
      onNotify({
        tone: "ok",
        message: `已批量生成 ${created.length} 章大纲与节拍卡`,
        detail: "已落库为 outlined 状态，可直接在编排台微调节拍后推入微批",
      });
      onClose();
    } catch (err) {
      if (userStoppedRef.current || ac.signal.aborted) {
        if (mountedRef.current) {
          onNotify({ tone: "info", message: "已取消批量章纲生成" });
        }
      } else if (mountedRef.current) {
        onNotify({
          tone: "danger",
          message: "批量生成章纲失败",
          detail: formatErrorMessage(err),
        });
      }
    } finally {
      if (fallbackTimerRef.current !== null) {
        window.clearTimeout(fallbackTimerRef.current);
        fallbackTimerRef.current = null;
      }
      try {
        if (!ch.closed) ch.close();
      } catch {
        // ignore
      }
      if (channelRef.current === ch) channelRef.current = null;
      if (abortRef.current === ac) abortRef.current = null;
      if (mountedRef.current) {
        setRunning(false);
        setStageMsg("");
      }
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-[2px]"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget && !running) onClose();
      }}
    >
      <div className="flex max-h-[85vh] w-full max-w-[540px] flex-col overflow-hidden rounded-xl border border-border-strong bg-elevated shadow-[var(--shadow-elevated)]">
        <div className="flex items-center justify-between border-b border-border-subtle px-5 py-3.5">
          <div className="flex items-center gap-2">
            <Sparkles className="h-4 w-4 text-accent" />
            <h3 className="text-sm font-medium text-ink-primary">
              批量生成章纲与节拍 · {volume.title}
            </h3>
          </div>
          <button
            type="button"
            disabled={running}
            onClick={onClose}
            className="rounded p-1 text-ink-muted hover:bg-surface hover:text-ink-primary disabled:opacity-40"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="mb-1 block text-xs text-ink-secondary">
                起始章号
              </label>
              <input
                type="number"
                min={1}
                disabled={running}
                value={startSeq}
                onChange={(e) => setStartSeq(e.target.value)}
                className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none disabled:opacity-50"
              />
            </div>

            <div>
              <label className="mb-1 block text-xs text-ink-secondary">
                生成章数（3~10 章，建议 5 章）
              </label>
              <input
                type="number"
                min={1}
                max={10}
                disabled={running}
                value={count}
                onChange={(e) => setCount(e.target.value)}
                className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none disabled:opacity-50"
              />
            </div>
          </div>

          <div>
            <label className="mb-1 block text-xs text-ink-secondary">
              本阶段小高潮与剧情走向期望
            </label>
            <textarea
              rows={3}
              disabled={running}
              value={arcGoal}
              onChange={(e) => setArcGoal(e.target.value)}
              placeholder="例如：黑市试探受辱 -> 残阵诱杀王执事夺破障丹 -> 血玉令牌暴露引发百里追杀"
              className="w-full resize-y rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none disabled:opacity-50"
            />
          </div>

          {running && (
            <div className="flex flex-col gap-2 rounded-lg border border-accent/35 bg-canvas/80 p-3">
              <div className="flex items-center gap-2 text-xs text-accent">
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                <span>{stageMsg || "流式生成中…"}</span>
              </div>
              {streamPreview && (
                <pre className="nib-mono max-h-40 overflow-y-auto text-[11px] leading-relaxed whitespace-pre-wrap text-ink-muted">
                  {streamPreview.slice(-900)}
                </pre>
              )}
            </div>
          )}
        </div>

        <div className="flex items-center justify-end gap-2 border-t border-border-subtle bg-surface/50 px-5 py-3">
          {running ? (
            <button
              type="button"
              onClick={stopGeneration}
              className="flex items-center gap-1.5 rounded-md border border-danger/40 bg-danger-soft px-3.5 py-1.5 text-xs font-medium text-danger"
            >
              <Square className="h-3 w-3 fill-current" />
              <span>停止生成</span>
            </button>
          ) : (
            <>
              <button
                type="button"
                onClick={onClose}
                className="rounded-md px-3 py-1.5 text-xs text-ink-secondary hover:bg-surface hover:text-ink-primary"
              >
                取消
              </button>
              <button
                type="button"
                onClick={() => void handleGenerate()}
                className="flex items-center gap-1.5 rounded-md bg-accent px-4 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90"
              >
                <Sparkles className="h-3.5 w-3.5" />
                <span>生成章纲与节拍</span>
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
