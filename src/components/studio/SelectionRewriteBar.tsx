import { useMemo, useState } from "react";
import { BookmarkPlus, ChevronUp, Sparkles, Trash2, X } from "lucide-react";
import type { RewritePreset } from "../../mygo";
import { countWords, isIMEComposing } from "../../lib/constants";
import { diffInlineTokens } from "../../lib/textDiff";

type RewriteAction = "expand" | "condense" | "dialogue" | "vivid" | "deai" | "custom";

interface SelectionRewriteBarProps {
  selectedText: string;
  selectionStage: string;
  selectionRewriting: boolean;
  selectionCustomPrompt: string;
  selectionPreview: string | null;
  presets: RewritePreset[];
  onChangeCustomPrompt: (value: string) => void;
  onRunAction: (action: RewriteAction, customOverride?: string) => void;
  onSavePreset: (label: string, instruction: string) => Promise<boolean>;
  onDeletePreset: (id: number) => Promise<void>;
  onDismissBar: () => void;
  onCancelPreview: () => void;
  onApplyPreview: () => void;
}

const PRESET_ACTIONS = [
  { key: "expand", label: "扩写细节" },
  { key: "condense", label: "精简紧凑" },
  { key: "dialogue", label: "优化对话" },
  { key: "vivid", label: "增强画面" },
  { key: "deai", label: "去 AI 味" },
] as const;

export function SelectionRewriteBar({
  selectedText,
  selectionStage,
  selectionRewriting,
  selectionCustomPrompt,
  selectionPreview,
  presets,
  onChangeCustomPrompt,
  onRunAction,
  onSavePreset,
  onDeletePreset,
  onDismissBar,
  onCancelPreview,
  onApplyPreview,
}: SelectionRewriteBarProps) {
  const [customOpen, setCustomOpen] = useState(false);
  const [savingFormOpen, setSavingFormOpen] = useState(false);
  const [presetLabel, setPresetLabel] = useState("");
  const [savingPreset, setSavingPreset] = useState(false);
  const [previewMode, setPreviewMode] = useState<"diff" | "clean">("diff");

  const inlineDiff = useMemo(() => {
    if (selectionPreview === null || selectionRewriting) return [];
    return diffInlineTokens(selectedText, selectionPreview);
  }, [selectedText, selectionPreview, selectionRewriting]);

  const openSaveForm = () => {
    const trimmed = selectionCustomPrompt.trim();
    if (!trimmed) return;
    setPresetLabel(trimmed.slice(0, 8));
    setSavingFormOpen(true);
  };

  const handleConfirmSavePreset = async () => {
    const label = presetLabel.trim();
    const instruction = selectionCustomPrompt.trim();
    if (!label || !instruction || savingPreset) return;
    setSavingPreset(true);
    try {
      const ok = await onSavePreset(label, instruction);
      if (ok) {
        setSavingFormOpen(false);
        setPresetLabel("");
      }
    } finally {
      setSavingPreset(false);
    }
  };

  const expanded = customOpen || selectionRewriting || selectionPreview !== null;

  return (
    <div className="pointer-events-auto nib-floating-dock flex w-full max-w-[700px] flex-col gap-2.5 rounded-2xl px-3.5 py-2.5 text-xs">
      {/* Preview Panel (renders above action bar when active) */}
      {selectionPreview !== null && (
        <div className="flex flex-col gap-2 rounded-lg border border-border-subtle bg-canvas p-3">
          <div className="flex items-center justify-between gap-2">
            <div className="text-xs font-medium text-ink-secondary">
              {selectionRewriting ? "正在生成改写预览…" : "改写预览"}
            </div>
            {!selectionRewriting && selectionPreview && (
              <div className="flex items-center gap-1 rounded border border-border-subtle bg-surface p-0.5 text-xs">
                <button
                  type="button"
                  onClick={() => setPreviewMode("diff")}
                  className={`rounded px-2 py-0.5 transition-colors ${
                    previewMode === "diff"
                      ? "bg-accent-soft font-medium text-accent"
                      : "text-ink-secondary hover:text-ink-primary"
                  }`}
                >
                  词级对比
                </button>
                <button
                  type="button"
                  onClick={() => setPreviewMode("clean")}
                  className={`rounded px-2 py-0.5 transition-colors ${
                    previewMode === "clean"
                      ? "bg-accent-soft font-medium text-accent"
                      : "text-ink-secondary hover:text-ink-primary"
                  }`}
                >
                  改写结果
                </button>
              </div>
            )}
          </div>

          <div className="max-h-48 overflow-y-auto whitespace-pre-wrap text-[13px] leading-relaxed text-ink-primary">
            {selectionRewriting || previewMode === "clean" ? (
              selectionPreview || "正在生成…"
            ) : (
              inlineDiff.map((seg, i) => {
                if (seg.kind === "equal") {
                  return <span key={i}>{seg.text}</span>;
                }
                if (seg.kind === "removed") {
                  return (
                    <del
                      key={i}
                      className="rounded-sm bg-danger-soft/75 px-0.5 text-danger line-through"
                    >
                      {seg.text}
                    </del>
                  );
                }
                return (
                  <ins
                    key={i}
                    className="rounded-sm bg-ok-soft/75 px-0.5 text-ok no-underline"
                  >
                    {seg.text}
                  </ins>
                );
              })
            )}
          </div>

          <div className="flex items-center justify-end gap-2">
            <button
              type="button"
              onClick={onCancelPreview}
              className="rounded-md border border-border-subtle px-2.5 py-1 text-xs text-ink-secondary hover:text-ink-primary"
            >
              放弃
            </button>
            <button
              type="button"
              disabled={selectionRewriting || !selectionPreview.trim()}
              onClick={onApplyPreview}
              className="rounded-md bg-accent px-3 py-1 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
            >
              替换选区
            </button>
          </div>
        </div>
      )}

      {/* Expanded Custom Prompt + Saved Presets */}
      {expanded && selectionPreview === null && (
        <div className="flex flex-col gap-2 border-b border-border-subtle pb-2.5">
          {presets.length > 0 && (
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-xs text-ink-muted">常用：</span>
              {presets.map((preset) => (
                <div
                  key={preset.id}
                  className="group inline-flex items-center overflow-hidden rounded-md border border-border-subtle bg-canvas text-xs"
                >
                  <button
                    type="button"
                    disabled={selectionRewriting}
                    title={preset.instruction}
                    onClick={() => onRunAction("custom", preset.instruction)}
                    className="px-2 py-0.5 text-ink-primary transition-colors hover:bg-accent-soft hover:text-accent disabled:opacity-40"
                  >
                    {preset.label}
                  </button>
                  <button
                    type="button"
                    disabled={selectionRewriting}
                    onClick={() => void onDeletePreset(preset.id)}
                    title="删除此常用指令"
                    aria-label={`删除常用指令 ${preset.label}`}
                    className="border-l border-border-subtle px-1 py-0.5 text-ink-muted transition-colors hover:bg-danger-soft hover:text-danger disabled:opacity-40"
                  >
                    <Trash2 className="h-3 w-3" />
                  </button>
                </div>
              ))}
            </div>
          )}

          <div className="flex items-center gap-2">
            <input
              type="text"
              autoFocus={customOpen}
              value={selectionCustomPrompt}
              disabled={selectionRewriting}
              onChange={(e) => onChangeCustomPrompt(e.target.value)}
              onKeyDown={(e) => {
                if (
                  e.key === "Enter" &&
                  !isIMEComposing(e) &&
                  selectionCustomPrompt.trim()
                ) {
                  e.preventDefault();
                  onRunAction("custom");
                }
              }}
              placeholder="输入自定义改写要求（如：改成冷嘲热讽语气、加入拔剑动作）"
              className="min-w-0 flex-1 rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent"
            />
            {selectionCustomPrompt.trim() && (
              <button
                type="button"
                disabled={selectionRewriting}
                onClick={openSaveForm}
                title="保存为常用按钮"
                className="flex items-center gap-1 rounded-lg border border-border-subtle bg-surface px-2.5 py-1.5 text-xs text-ink-secondary transition-colors hover:border-accent/50 hover:text-accent disabled:opacity-40"
              >
                <BookmarkPlus className="h-3.5 w-3.5" />
                <span>存为常用</span>
              </button>
            )}
            <button
              type="button"
              disabled={selectionRewriting || !selectionCustomPrompt.trim()}
              onClick={() => onRunAction("custom")}
              className="rounded-lg bg-accent px-3 py-1.5 font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
            >
              {selectionRewriting ? "改写中…" : "改写"}
            </button>
          </div>

          {savingFormOpen && (
            <div className="flex items-center gap-2 rounded-lg border border-border-subtle bg-canvas px-2.5 py-2">
              <span className="shrink-0 text-xs text-ink-secondary">按钮名</span>
              <input
                type="text"
                autoFocus
                maxLength={30}
                value={presetLabel}
                onChange={(e) => setPresetLabel(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !isIMEComposing(e)) {
                    e.preventDefault();
                    void handleConfirmSavePreset();
                  } else if (e.key === "Escape") {
                    setSavingFormOpen(false);
                  }
                }}
                placeholder="例如：冷峻压迫感"
                className="min-w-0 flex-1 rounded border border-border-subtle bg-surface px-2 py-1 text-xs text-ink-primary focus:border-accent"
              />
              <button
                type="button"
                disabled={savingPreset || !presetLabel.trim()}
                onClick={() => void handleConfirmSavePreset()}
                className="rounded bg-accent px-2.5 py-1 text-xs font-semibold text-canvas disabled:opacity-40"
              >
                {savingPreset ? "保存中…" : "保存"}
              </button>
              <button
                type="button"
                onClick={() => setSavingFormOpen(false)}
                className="rounded px-2 py-1 text-xs text-ink-muted hover:text-ink-primary"
              >
                取消
              </button>
            </div>
          )}
        </div>
      )}

      {/* Primary Single-Row Compact Floating Pill */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-1.5 text-accent">
          <Sparkles className="h-3.5 w-3.5 shrink-0" />
          <span className="nib-mono font-medium">已选 {countWords(selectedText)} 字</span>
          {selectionStage && (
            <span className="text-ink-muted">· {selectionStage}</span>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-1.5">
          {PRESET_ACTIONS.map((item) => (
            <button
              key={item.key}
              type="button"
              disabled={selectionRewriting}
              onClick={() => onRunAction(item.key)}
              className="rounded-md border border-border-subtle bg-surface px-2 py-1 text-xs text-ink-primary transition-colors hover:border-accent/50 hover:text-accent disabled:opacity-40"
            >
              {item.label}
            </button>
          ))}
          <button
            type="button"
            disabled={selectionRewriting}
            onClick={() => setCustomOpen((v) => !v)}
            className={`flex items-center gap-1 rounded-md border px-2 py-1 text-xs transition-colors ${
              customOpen
                ? "border-accent/40 bg-accent-soft text-accent"
                : "border-border-subtle bg-surface text-ink-secondary hover:text-ink-primary"
            }`}
          >
            <span>自定义</span>
            <ChevronUp className={`h-3 w-3 transition-transform ${customOpen ? "rotate-180" : ""}`} />
          </button>
          <button
            type="button"
            onClick={onDismissBar}
            title="关闭改写浮条"
            aria-label="关闭改写浮条"
            className="rounded p-1 text-ink-muted transition-colors hover:bg-surface hover:text-ink-primary"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>
    </div>
  );
}
