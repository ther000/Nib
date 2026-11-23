import { useState, type KeyboardEvent } from "react";
import { Check, Plus, X } from "lucide-react";
import type { GenrePreset } from "../../mygo";

export type AIStep = 1 | 2 | 3;

export const POV_OPTIONS: { value: string; label: string }[] = [
  { value: "third_limited", label: "第三人称限知" },
  { value: "first_person", label: "第一人称" },
  { value: "third_omniscient", label: "第三人称全知" },
  { value: "multi_pov", label: "多视角群像" },
];

export const BUILTIN_STYLE_TAGS: string[] = [
  "快节奏",
  "慢热布局",
  "苟道藏拙",
  "群像博弈",
  "轻松日常",
  "黑暗冷峻",
  "克苏鲁诡异",
  "权谋智斗",
  "热血战斗",
  "种田基建",
];

export const IDEA_EXAMPLES: {
  genre: string;
  tags: string[];
  label: string;
  text: string;
}[] = [
  {
    genre: "xuanhuan",
    tags: ["快节奏", "热血战斗"],
    label: "三息残剑",
    text: "外门杂役被构陷废去修为，坠崖前捡到一把每十息可回溯三息时间的残剑，借时间差一路越阶反杀宗门强敌。",
  },
  {
    genre: "xianxia",
    tags: ["苟道藏拙", "慢热布局"],
    label: "寿元典当行",
    text: "资质平庸的散修意外继承一座能以他人因果换取寿元与功法的黑市典当行，步步为营在各大仙门之间藏拙成道。",
  },
  {
    genre: "mystery",
    tags: ["克苏鲁诡异", "慢热布局"],
    label: "午夜民俗局",
    text: "民俗档案员接手一桩被封存三十年的村落失踪案，发现每翻开一页旧卷宗，现实中就会多出一条必须遵守的诡异规则。",
  },
];

export function getPovLabel(povMode: string): string {
  return POV_OPTIONS.find((item) => item.value === povMode)?.label ?? "第三人称限知";
}

export function parseStyleTags(raw: string): string[] {
  if (!raw.trim()) return [];
  const seen = new Set<string>();
  const result: string[] = [];
  for (const part of raw.split(/[、,，/]+/)) {
    const t = part.trim();
    if (!t || seen.has(t)) continue;
    seen.add(t);
    result.push(t);
  }
  return result;
}

export function formatStyleTags(tags: string[]): string {
  return tags
    .map((t) => t.trim())
    .filter(Boolean)
    .join("、");
}

export function GenrePillPicker({
  presets,
  selectedId,
  disabled,
  showFieldHint = true,
  onSelect,
}: {
  presets: GenrePreset[];
  selectedId: string;
  disabled?: boolean;
  showFieldHint?: boolean;
  onSelect: (id: string) => void;
}) {
  const [customInputOpen, setCustomInputOpen] = useState(false);
  const [customDraft, setCustomDraft] = useState("");

  const matchedBuiltin = presets.find((p) => p.id === selectedId);
  const isCustomSelected = Boolean(selectedId.trim() && !matchedBuiltin);

  const commitCustom = () => {
    const trimmed = customDraft.trim();
    if (trimmed) {
      onSelect(trimmed.slice(0, 32));
    }
    setCustomDraft("");
    setCustomInputOpen(false);
  };

  const handleCustomKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault();
      commitCustom();
    } else if (e.key === "Escape") {
      setCustomDraft("");
      setCustomInputOpen(false);
    }
  };

  const effectivePreset =
    matchedBuiltin ?? presets.find((p) => p.id === "general") ?? presets[0];

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-1.5">
        {presets.map((preset) => {
          const selected = preset.id === selectedId;
          return (
            <button
              key={preset.id}
              type="button"
              disabled={disabled}
              onClick={() => {
                setCustomInputOpen(false);
                onSelect(preset.id);
              }}
              className={`rounded-lg border px-3 py-1.5 text-[13px] transition-colors disabled:opacity-50 ${
                selected
                  ? "border-accent bg-accent-soft font-medium text-accent"
                  : "border-border-subtle bg-surface text-ink-secondary hover:border-border-strong hover:text-ink-primary"
              }`}
            >
              {preset.name}
            </button>
          );
        })}

        {isCustomSelected && !customInputOpen && (
          <button
            type="button"
            disabled={disabled}
            onClick={() => {
              setCustomDraft(selectedId);
              setCustomInputOpen(true);
            }}
            className="flex items-center gap-1 rounded-lg border border-accent bg-accent-soft px-3 py-1.5 text-[13px] font-medium text-accent"
          >
            <span>{selectedId}</span>
          </button>
        )}

        {customInputOpen ? (
          <input
            type="text"
            autoFocus
            maxLength={32}
            disabled={disabled}
            value={customDraft}
            onChange={(e) => setCustomDraft(e.target.value)}
            onBlur={commitCustom}
            onKeyDown={handleCustomKeyDown}
            placeholder="输入题材名回车"
            className="w-32 rounded-lg border border-accent bg-surface px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:outline-none"
          />
        ) : (
          <button
            type="button"
            disabled={disabled}
            onClick={() => {
              setCustomDraft(isCustomSelected ? selectedId : "");
              setCustomInputOpen(true);
            }}
            className="flex items-center gap-1 rounded-lg border border-dashed border-border-strong px-2.5 py-1.5 text-xs text-ink-muted transition-colors hover:border-ink-secondary hover:text-ink-primary disabled:opacity-40"
          >
            <Plus className="h-3 w-3" />
            <span>自定义</span>
          </button>
        )}
      </div>

      {showFieldHint && effectivePreset && effectivePreset.entityFields.length > 0 && (
        <div className="text-xs text-ink-muted">
          角色字段：
          {effectivePreset.entityFields
            .slice(0, 5)
            .map((f) => f.label)
            .join(" · ")}
        </div>
      )}
    </div>
  );
}

export function StyleTagPicker({
  tags,
  disabled,
  maxTags = 5,
  onChange,
}: {
  tags: string[];
  disabled?: boolean;
  maxTags?: number;
  onChange: (next: string[]) => void;
}) {
  const [draft, setDraft] = useState("");

  const customTags = tags.filter((t) => !BUILTIN_STYLE_TAGS.includes(t));

  const toggleTag = (tag: string) => {
    if (tags.includes(tag)) {
      onChange(tags.filter((t) => t !== tag));
    } else if (tags.length < maxTags) {
      onChange([...tags, tag]);
    }
  };

  const addCustomTag = () => {
    const trimmed = draft.trim().replace(/[、,，/]/g, "");
    if (!trimmed) return;
    if (!tags.includes(trimmed) && tags.length < maxTags) {
      onChange([...tags, trimmed]);
    }
    setDraft("");
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault();
      addCustomTag();
    }
  };

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {BUILTIN_STYLE_TAGS.map((tag) => {
        const active = tags.includes(tag);
        const reachedLimit = !active && tags.length >= maxTags;
        return (
          <button
            key={tag}
            type="button"
            disabled={disabled || reachedLimit}
            onClick={() => toggleTag(tag)}
            className={`rounded-md border px-2.5 py-1 text-xs transition-colors disabled:opacity-40 ${
              active
                ? "border-accent bg-accent-soft font-medium text-accent"
                : "border-border-subtle bg-surface text-ink-secondary hover:border-border-strong hover:text-ink-primary"
            }`}
          >
            {tag}
          </button>
        );
      })}

      {customTags.map((tag) => (
        <span
          key={tag}
          className="flex items-center gap-1 rounded-md border border-accent bg-accent-soft px-2.5 py-1 text-xs font-medium text-accent"
        >
          <span>{tag}</span>
          <button
            type="button"
            disabled={disabled}
            onClick={() => toggleTag(tag)}
            aria-label={`移除风格标签 ${tag}`}
            className="rounded hover:bg-accent/20"
          >
            <X className="h-3 w-3" />
          </button>
        </span>
      ))}

      {tags.length < maxTags && (
        <input
          type="text"
          maxLength={16}
          disabled={disabled}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={handleKeyDown}
          onBlur={addCustomTag}
          placeholder="+ 自定义标签"
          className="w-28 rounded-md border border-dashed border-border-strong bg-transparent px-2.5 py-1 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none disabled:opacity-40"
        />
      )}
    </div>
  );
}

const STEP_LABELS: { step: AIStep; label: string }[] = [
  { step: 1, label: "灵感定位" },
  { step: 2, label: "立意拍板" },
  { step: 3, label: "蓝图精修" },
];

export function StepIndicator({
  current,
  maxReached,
  disabled,
  onNavigate,
}: {
  current: AIStep;
  maxReached: AIStep;
  disabled?: boolean;
  onNavigate: (step: AIStep) => void;
}) {
  return (
    <nav aria-label="开书进度" className="flex items-center gap-2">
      {STEP_LABELS.map((item, idx) => {
        const isCurrent = current === item.step;
        const isDone = item.step < current;
        const canClick = !disabled && item.step <= maxReached && !isCurrent;
        return (
          <div key={item.step} className="flex items-center gap-2">
            <button
              type="button"
              disabled={!canClick}
              onClick={() => canClick && onNavigate(item.step)}
              className={`flex items-center gap-2 rounded-lg px-2 py-1 text-xs transition-colors ${
                isCurrent
                  ? "font-semibold text-ink-primary"
                  : isDone
                    ? "text-ink-secondary hover:text-ink-primary"
                    : "text-ink-muted"
              } ${canClick ? "cursor-pointer" : "cursor-default"}`}
            >
              <span
                className={`nib-mono flex h-5 w-5 items-center justify-center rounded-full text-[11px] font-semibold ${
                  isCurrent
                    ? "bg-accent text-canvas"
                    : isDone
                      ? "bg-ok-soft text-ok"
                      : "border border-border-subtle bg-surface text-ink-muted"
                }`}
              >
                {isDone ? <Check className="h-3 w-3" /> : item.step}
              </span>
              <span>{item.label}</span>
            </button>
            {idx < STEP_LABELS.length - 1 && (
              <span className="h-px w-6 bg-border-subtle" />
            )}
          </div>
        );
      })}
    </nav>
  );
}
