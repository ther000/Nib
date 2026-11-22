import { useEffect, useState } from "react";
import { Loader2, Sparkles, Square } from "lucide-react";
import {
  LibraryService,
  type Chapter,
  type OutlineChunk,
  type Volume,
} from "../../mygo";
import { formatErrorMessage } from "../../lib/constants";
import { useMygoChannel } from "../../hooks/useMygoStream";
import { ModalShell } from "../ui/ModalShell";

interface BatchOutlineModalProps {
  open: boolean;
  bookId: number;
  volumes: Volume[];
  defaultVolumeId: number | null;
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
  volumes,
  defaultVolumeId,
  defaultStartSeq,
  onClose,
  onGenerated,
  onNotify,
}: BatchOutlineModalProps) {
  const [targetVolumeId, setTargetVolumeId] = useState<number>(0);
  const [startSeq, setStartSeq] = useState(String(defaultStartSeq));
  const [count, setCount] = useState("5");
  const [arcGoal, setArcGoal] = useState("");
  const [running, setRunning] = useState(false);
  const [streamChars, setStreamChars] = useState(0);
  const [stageMsg, setStageMsg] = useState("");

  const { open: openStream, stop: stopStream, release: releaseStream, isCurrent, wasStopped } =
    useMygoChannel<OutlineChunk>();

  const selectedVolume =
    volumes.find((v) => v.id === targetVolumeId) ??
    volumes.find((v) => v.id === defaultVolumeId) ??
    volumes[0] ??
    null;

  useEffect(() => {
    if (!open) return;
    const initVol =
      volumes.find((v) => v.id === defaultVolumeId) ?? volumes[0] ?? null;
    setTargetVolumeId(initVol?.id ?? 0);
    setStartSeq(String(Math.max(1, defaultStartSeq)));
    setCount("5");
    setArcGoal(initVol?.goal ?? "");
    setStreamChars(0);
    setStageMsg("");
  }, [open, defaultStartSeq, defaultVolumeId, volumes]);

  if (!open || !selectedVolume) return null;

  const stopGeneration = () => {
    setStageMsg("正在取消生成…");
    stopStream();
  };

  const handleGenerate = async () => {
    if (running || !selectedVolume) return;
    const parsedStart = Math.max(1, Number.parseInt(startSeq, 10) || 1);
    const parsedCount = Math.min(10, Math.max(1, Number.parseInt(count, 10) || 5));

    setRunning(true);
    setStreamChars(0);
    setStageMsg(`正在构思 ${parsedCount} 章大纲与剧情节拍…`);

    const { channel: ch, runId } = openStream((chunk) => {
      if (!isCurrent(runId)) return;
      if (chunk.kind === "delta" && chunk.delta) {
        setStreamChars((prev) => prev + chunk.delta!.length);
      } else if (chunk.kind === "completed") {
        setStageMsg("章纲已生成，正在保存…");
      } else if (chunk.kind === "error" && chunk.error) {
        setStageMsg(formatErrorMessage(chunk.error));
      }
    });

    try {
      const created = await LibraryService.batchGenerateOutlines(
        {
          bookId,
          volumeId: selectedVolume.id,
          startSeq: parsedStart,
          count: parsedCount,
          arcGoal: arcGoal.trim(),
        },
        ch
      );
      if (!isCurrent(runId)) return;
      await onGenerated(created);
      onNotify({
        tone: "ok",
        message: `已生成 ${created.length} 章大纲与节拍`,
        detail: "可直接微调节拍或切到「写作」开始出稿",
      });
      onClose();
    } catch (err) {
      if (!isCurrent(runId)) return;
      if (wasStopped()) {
        onNotify({ tone: "info", message: "已取消批量章纲生成" });
      } else {
        onNotify({
          tone: "danger",
          message: "批量生成章纲失败",
          detail: formatErrorMessage(err),
        });
      }
    } finally {
      releaseStream(ch);
      if (isCurrent(runId)) {
        setRunning(false);
        setStageMsg("");
      }
    }
  };

  return (
    <ModalShell
      open={open}
      onClose={onClose}
      closeDisabled={running}
      maxWidthClass="max-w-[540px]"
      maxHeightClass="max-h-[85vh]"
      icon={<Sparkles className="h-4 w-4 text-accent" />}
      title="AI 批量生成章纲"
      footer={
        running ? (
          <button
            type="button"
            onClick={stopGeneration}
            className="flex items-center gap-1.5 rounded-lg border border-danger/40 bg-danger-soft px-3.5 py-1.5 text-xs font-medium text-danger"
          >
            <Square className="h-3 w-3 fill-current" />
            <span>停止生成</span>
          </button>
        ) : (
          <>
            <button
              type="button"
              onClick={onClose}
              className="rounded-lg px-3 py-1.5 text-xs text-ink-secondary hover:bg-surface hover:text-ink-primary"
            >
              取消
            </button>
            <button
              type="button"
              onClick={() => void handleGenerate()}
              className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90"
            >
              <Sparkles className="h-3.5 w-3.5" />
              <span>生成章纲与节拍</span>
            </button>
          </>
        )
      }
    >
      <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
        <div>
          <label className="mb-1 block text-xs font-medium text-ink-secondary">
            归属分卷
          </label>
          <select
            disabled={running}
            value={selectedVolume.id}
            onChange={(e) => {
              const nextId = Number(e.target.value);
              setTargetVolumeId(nextId);
              const nextVol = volumes.find((v) => v.id === nextId);
              if (nextVol && !arcGoal.trim()) {
                setArcGoal(nextVol.goal ?? "");
              }
            }}
            className="w-full rounded-lg border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary focus:border-accent disabled:opacity-50"
          >
            {volumes.map((vol) => (
              <option key={vol.id} value={vol.id}>
                {vol.title}
              </option>
            ))}
          </select>
        </div>

        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="mb-1 block text-xs font-medium text-ink-secondary">
              起始章号
            </label>
            <input
              type="number"
              min={1}
              disabled={running}
              value={startSeq}
              onChange={(e) => setStartSeq(e.target.value)}
              className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary focus:border-accent disabled:opacity-50"
            />
          </div>

          <div>
            <label className="mb-1 block text-xs font-medium text-ink-secondary">
              生成章数（1~10 章，建议 5 章）
            </label>
            <input
              type="number"
              min={1}
              max={10}
              disabled={running}
              value={count}
              onChange={(e) => setCount(e.target.value)}
              className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary focus:border-accent disabled:opacity-50"
            />
          </div>
        </div>

        <div>
          <label className="mb-1 block text-xs font-medium text-ink-secondary">
            本阶段小高潮与剧情走向期望
          </label>
          <textarea
            rows={3}
            disabled={running}
            value={arcGoal}
            onChange={(e) => setArcGoal(e.target.value)}
            placeholder="例如：黑市试探受辱 -> 残阵诱杀王执事夺破障丹 -> 血玉令牌暴露引发百里追杀"
            className="w-full resize-y rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent disabled:opacity-50"
          />
        </div>

        {running && (
          <div className="flex items-center justify-between rounded-lg border border-accent/35 bg-canvas/80 px-3.5 py-2.5 text-xs">
            <div className="flex items-center gap-2 text-accent">
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
              <span>{stageMsg || "正在生成中…"}</span>
            </div>
            {streamChars > 0 && (
              <span className="nib-mono text-ink-muted">已构思 {streamChars} 字</span>
            )}
          </div>
        )}
      </div>
    </ModalShell>
  );
}
