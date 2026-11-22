import { Sparkles, X } from "lucide-react";
import { countWords, isIMEComposing } from "../../lib/constants";

type RewriteAction = "expand" | "condense" | "dialogue" | "vivid" | "deai" | "custom";

interface SelectionRewriteBarProps {
  selectedText: string;
  selectionStage: string;
  selectionRewriting: boolean;
  selectionCustomPrompt: string;
  selectionPreview: string | null;
  onChangeCustomPrompt: (value: string) => void;
  onRunAction: (action: RewriteAction) => void;
  onDismissBar: () => void;
  onCancelPreview: () => void;
  onApplyPreview: () => void;
}

const PRESET_ACTIONS = [
  { key: "expand", label: "扩写细节" },
  { key: "condense", label: "精简紧凑" },
  { key: "dialogue", label: "优化对话" },
  { key: "vivid", label: "增强动作感" },
  { key: "deai", label: "去 AI 味" },
] as const;

export function SelectionRewriteBar({
  selectedText,
  selectionStage,
  selectionRewriting,
  selectionCustomPrompt,
  selectionPreview,
  onChangeCustomPrompt,
  onRunAction,
  onDismissBar,
  onCancelPreview,
  onApplyPreview,
}: SelectionRewriteBarProps) {
  return (
    <div className="mb-4 flex flex-col gap-2.5 rounded-xl border border-accent/35 bg-surface p-3 text-xs">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-1.5 text-accent">
          <Sparkles className="h-3.5 w-3.5" />
          <span className="font-medium">已选中 {countWords(selectedText)} 字</span>
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
              className="rounded-md border border-border-subtle bg-elevated px-2 py-1 text-xs text-ink-primary transition-colors hover:border-accent/50 hover:text-accent disabled:opacity-40"
            >
              {item.label}
            </button>
          ))}
          <button
            type="button"
            onClick={onDismissBar}
            className="rounded p-1 text-ink-muted hover:text-ink-primary"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      <div className="flex items-center gap-2">
        <input
          type="text"
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
          placeholder="或输入自定义修改要求（例如：改成冷嘲热讽语气、加入拔剑动作）"
          className="min-w-0 flex-1 rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
        />
        <button
          type="button"
          disabled={selectionRewriting || !selectionCustomPrompt.trim()}
          onClick={() => onRunAction("custom")}
          className="rounded-lg bg-accent px-3 py-1.5 font-medium text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
        >
          {selectionRewriting ? "改写中…" : "按要求改写"}
        </button>
      </div>

      {selectionPreview !== null && (
        <div className="flex flex-col gap-2 rounded-lg border border-border-subtle bg-canvas p-3">
          <div className="text-xs font-medium text-ink-secondary">改写预览</div>
          <div className="max-h-48 overflow-y-auto whitespace-pre-wrap text-[13px] leading-relaxed text-ink-primary">
            {selectionPreview || "正在生成…"}
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
    </div>
  );
}
