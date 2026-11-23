import { Check } from "lucide-react";
import type { GenrePreset } from "../../mygo";

export type AIStep = 1 | 2 | 3;

export const POV_OPTIONS: { value: string; label: string }[] = [
  { value: "third_limited", label: "第三人称限知" },
  { value: "first_person", label: "第一人称" },
  { value: "third_omniscient", label: "第三人称全知" },
  { value: "multi_pov", label: "多视角群像" },
];

export const IDEA_EXAMPLES: { genre: string; label: string; text: string }[] = [
  {
    genre: "xuanhuan",
    label: "三息残剑",
    text: "外门杂役被构陷废去修为，坠崖前捡到一把每十息可回溯三息时间的残剑，借时间差一路越阶反杀宗门强敌。",
  },
  {
    genre: "xianxia",
    label: "寿元典当行",
    text: "资质平庸的散修意外继承一座能以他人因果换取寿元与功法的黑市典当行，步步为营在各大仙门之间藏拙成道。",
  },
  {
    genre: "urban",
    label: "未来三天快讯",
    text: "刚被裁员的投行分析师手机每天零点收到一条来自三天后的商业快讯，凭信息差精准抄底、手撕前老板并建立资本帝国。",
  },
];

export function getPovLabel(povMode: string): string {
  return POV_OPTIONS.find((item) => item.value === povMode)?.label ?? "第三人称限知";
}

export function GenrePresetGrid({
  presets,
  selectedId,
  disabled,
  columns = 2,
  onSelect,
}: {
  presets: GenrePreset[];
  selectedId: string;
  disabled?: boolean;
  columns?: 2 | 4;
  onSelect: (id: string) => void;
}) {
  return (
    <div
      className={`grid gap-2.5 ${
        columns === 4 ? "grid-cols-2 sm:grid-cols-4" : "grid-cols-2"
      }`}
    >
      {presets.map((preset) => {
        const selected = preset.id === selectedId;
        return (
          <button
            key={preset.id}
            type="button"
            disabled={disabled}
            onClick={() => onSelect(preset.id)}
            className={`rounded-xl border px-3.5 py-3 text-left transition-colors disabled:opacity-60 ${
              selected
                ? "border-accent bg-accent-soft"
                : "border-border-subtle bg-surface hover:border-border-strong"
            }`}
          >
            <div className="flex items-center justify-between gap-2">
              <span
                className={`text-[13px] font-medium ${
                  selected ? "text-accent" : "text-ink-primary"
                }`}
              >
                {preset.name}
              </span>
              {preset.loopCycleChapters && (
                <span className="text-[11px] text-ink-muted">
                  {preset.loopCycleChapters}
                </span>
              )}
            </div>
            <div className="mt-1 line-clamp-2 text-xs leading-relaxed text-ink-secondary">
              {preset.tagline}
            </div>
          </button>
        );
      })}
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
                      : "bg-surface text-ink-muted border border-border-subtle"
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
