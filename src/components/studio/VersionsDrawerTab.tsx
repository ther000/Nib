import { useMemo, useState } from "react";
import { RotateCcw } from "lucide-react";
import type { ChapterVersion } from "../../mygo";
import { diffParagraphs } from "../../lib/textDiff";
import {
  formatNumber,
  formatTimestamp,
  getVersionSourceLabel,
} from "../../lib/constants";
import type { NotifyInput } from "../../stores/ToastContext";

interface VersionsDrawerTabProps {
  versions: ChapterVersion[];
  versionsLoading: boolean;
  selectedVersionId: number | null;
  restoringId: number | null;
  generating: boolean;
  currentContent: string;
  onSelectVersionId: (id: number) => void;
  onRestoreVersion: (versionId: number) => void;
  onInsertParagraph: (para: string) => void;
  notify: (input: NotifyInput) => void;
}

export function VersionsDrawerTab({
  versions,
  versionsLoading,
  selectedVersionId,
  restoringId,
  generating,
  currentContent,
  onSelectVersionId,
  onRestoreVersion,
  onInsertParagraph,
  notify,
}: VersionsDrawerTabProps) {
  const [versionViewMode, setVersionViewMode] = useState<"diff" | "raw">("diff");

  const selectedVersion = useMemo(
    () => versions.find((v) => v.id === selectedVersionId) ?? null,
    [versions, selectedVersionId]
  );

  const selectedVersionDiff = useMemo(() => {
    if (!selectedVersion || versionViewMode !== "diff") return null;
    return diffParagraphs(selectedVersion.content, currentContent);
  }, [selectedVersion, versionViewMode, currentContent]);

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      {versionsLoading && versions.length === 0 ? (
        <div className="flex flex-1 items-center justify-center text-xs text-ink-muted">
          正在读取历史快照…
        </div>
      ) : versions.length === 0 ? (
        <div className="flex flex-1 items-center justify-center px-6 text-center text-xs text-ink-muted">
          暂无历史快照。保存正文、生成或回滚时会自动归档。
        </div>
      ) : (
        <>
          <div className="max-h-[45%] shrink-0 overflow-y-auto border-b border-border-subtle p-2.5">
            <div className="flex flex-col gap-1.5">
              {versions.map((ver) => {
                const selected = ver.id === selectedVersionId;
                return (
                  <button
                    key={ver.id}
                    type="button"
                    onClick={() => onSelectVersionId(ver.id)}
                    className={`flex flex-col gap-1 rounded-lg border px-3 py-2 text-left transition-colors ${
                      selected
                        ? "border-accent/50 bg-accent-soft"
                        : "border-border-subtle bg-canvas/60 hover:border-border-strong"
                    }`}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span
                        className={`text-xs font-medium ${
                          selected ? "text-accent" : "text-ink-primary"
                        }`}
                      >
                        {getVersionSourceLabel(ver.source)}
                      </span>
                      <span className="nib-mono text-[11px] text-ink-secondary">
                        {formatNumber(ver.wordCount)} 字
                      </span>
                    </div>
                    <div className="flex items-center justify-between gap-2 text-[11px] text-ink-muted">
                      <span className="nib-mono truncate">
                        {ver.modelName || "人工编辑"}
                      </span>
                      <span className="nib-mono shrink-0">
                        {formatTimestamp(ver.createdAt)}
                      </span>
                    </div>
                  </button>
                );
              })}
            </div>
          </div>

          {selectedVersion && (
            <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
              <div className="flex items-center justify-between gap-2 border-b border-border-subtle bg-surface px-3 py-2">
                <div className="flex items-center gap-1 rounded-md border border-border-subtle bg-canvas p-0.5 text-[11px]">
                  <button
                    type="button"
                    onClick={() => setVersionViewMode("diff")}
                    className={`rounded px-2 py-0.5 transition-colors ${
                      versionViewMode === "diff"
                        ? "bg-accent-soft font-medium text-accent"
                        : "text-ink-secondary hover:text-ink-primary"
                    }`}
                  >
                    对比当前
                  </button>
                  <button
                    type="button"
                    onClick={() => setVersionViewMode("raw")}
                    className={`rounded px-2 py-0.5 transition-colors ${
                      versionViewMode === "raw"
                        ? "bg-accent-soft font-medium text-accent"
                        : "text-ink-secondary hover:text-ink-primary"
                    }`}
                  >
                    快照全文
                  </button>
                </div>
                <button
                  type="button"
                  disabled={generating || restoringId === selectedVersion.id}
                  onClick={() => onRestoreVersion(selectedVersion.id)}
                  className="flex shrink-0 items-center gap-1 rounded-lg border border-border-subtle bg-elevated px-2.5 py-1 text-xs font-medium text-ink-primary transition-colors hover:border-accent hover:text-accent disabled:opacity-40"
                >
                  <RotateCcw className="h-3 w-3" />
                  <span>
                    {restoringId === selectedVersion.id ? "恢复中…" : "整章恢复"}
                  </span>
                </button>
              </div>

              {versionViewMode === "diff" && selectedVersionDiff ? (
                <div className="flex flex-1 flex-col overflow-hidden bg-canvas/50">
                  <div className="flex items-center justify-between border-b border-border-subtle px-3.5 py-1.5 text-[11px] text-ink-muted">
                    <span>历史快照 → 当前正文</span>
                    <span className="nib-mono flex items-center gap-2">
                      <span className="text-ok">+{selectedVersionDiff.added} 段</span>
                      <span className="text-danger">
                        -{selectedVersionDiff.removed} 段
                      </span>
                    </span>
                  </div>
                  <div className="flex-1 overflow-y-auto px-3 py-2.5">
                    {selectedVersionDiff.added === 0 &&
                    selectedVersionDiff.removed === 0 ? (
                      <div className="py-8 text-center text-xs text-ink-muted">
                        此历史版本与当前正文内容完全一致
                      </div>
                    ) : (
                      <div className="flex flex-col gap-2">
                        {selectedVersionDiff.blocks.map((blk, idx) => (
                          <div
                            key={idx}
                            className={`group rounded-lg border px-2.5 py-2 text-[12.5px] leading-relaxed ${
                              blk.kind === "added"
                                ? "border-ok/30 bg-ok-soft/60 text-ink-primary"
                                : blk.kind === "removed"
                                ? "border-danger/30 bg-danger-soft/55 text-ink-secondary"
                                : "border-transparent bg-transparent text-ink-muted"
                            }`}
                          >
                            <div className="flex items-start justify-between gap-2">
                              <span className="nib-mono shrink-0 text-[10.5px] font-semibold opacity-75">
                                {blk.kind === "added"
                                  ? "+"
                                  : blk.kind === "removed"
                                  ? "-"
                                  : "·"}
                              </span>
                              <p className="min-w-0 flex-1 font-prose whitespace-pre-wrap">
                                {blk.text}
                              </p>
                            </div>
                            {blk.kind === "removed" && (
                              <div className="mt-1.5 flex items-center justify-end gap-2 opacity-0 transition-opacity group-hover:opacity-100">
                                <button
                                  type="button"
                                  onClick={async () => {
                                    try {
                                      await navigator.clipboard.writeText(blk.text);
                                      notify({ tone: "ok", message: "已复制该历史段落" });
                                    } catch {
                                      // ignore
                                    }
                                  }}
                                  className="rounded px-1.5 py-0.5 text-[11px] text-ink-secondary hover:bg-elevated hover:text-ink-primary"
                                >
                                  复制
                                </button>
                                <button
                                  type="button"
                                  disabled={generating}
                                  onClick={() => onInsertParagraph(blk.text)}
                                  className="rounded bg-elevated px-1.5 py-0.5 text-[11px] font-medium text-accent hover:bg-accent-soft disabled:opacity-40"
                                >
                                  插回当前稿
                                </button>
                              </div>
                            )}
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                </div>
              ) : (
                <div className="flex-1 overflow-y-auto bg-canvas/50 px-4 py-3">
                  <div className="font-prose text-[13.5px] leading-relaxed whitespace-pre-wrap text-ink-secondary">
                    {selectedVersion.content || "（空正文）"}
                  </div>
                </div>
              )}
            </div>
          )}
        </>
      )}
    </div>
  );
}
