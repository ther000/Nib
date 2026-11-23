import { useState } from "react";
import {
  ArrowLeft,
  Check,
  ChevronDown,
  ChevronRight,
  FolderOpen,
  Loader2,
  Pencil,
  Plus,
  RefreshCw,
  Square,
  Trash2,
} from "lucide-react";
import type {
  Beat,
  BeatStage,
  BookBlueprint,
  BookConcept,
  EmotionTag,
  EntityKind,
  GeneratedChapterOutline,
  GenesisEntity,
  HookType,
} from "../../mygo";
import {
  BEAT_STAGES,
  EMOTION_TAGS,
  ENTITY_KINDS,
  HOOK_TYPES,
} from "../../lib/constants";
import { getPovLabel } from "./GenesisShared";

interface BlueprintStepProps {
  concept: BookConcept;
  blueprint: BookBlueprint;
  genreName: string;
  targetWords: number;
  exportDir: string;
  selectedEntityIdx: Set<number>;
  selectedOutlineIdx: Set<number>;
  regeneratingSection: "entities" | "outlines" | null;
  streamChars: number;
  submitting: boolean;
  onChangeBlueprint: (next: BookBlueprint) => void;
  onChangeTargetWords: (words: number) => void;
  onChangeExportDir: (dir: string) => void;
  onPickExportDir: () => void;
  onToggleEntity: (idx: number) => void;
  onToggleOutline: (idx: number) => void;
  onAddEntity: () => number;
  onDeleteEntity: (idx: number) => void;
  onRegenerateSection: (section: "entities" | "outlines") => void;
  onStopRegenerate: () => void;
  onBackToConcept: () => void;
  onApply: (targetTab: "studio" | "architect") => void;
}

export function BlueprintStep({
  concept,
  blueprint,
  genreName,
  targetWords,
  exportDir,
  selectedEntityIdx,
  selectedOutlineIdx,
  regeneratingSection,
  streamChars,
  submitting,
  onChangeBlueprint,
  onChangeTargetWords,
  onChangeExportDir,
  onPickExportDir,
  onToggleEntity,
  onToggleOutline,
  onAddEntity,
  onDeleteEntity,
  onRegenerateSection,
  onStopRegenerate,
  onBackToConcept,
  onApply,
}: BlueprintStepProps) {
  const [editingEntityIdx, setEditingEntityIdx] = useState<number | null>(null);
  const [expandedOutlineIdx, setExpandedOutlineIdx] = useState<number | null>(0);
  const [newAttrKey, setNewAttrKey] = useState("");
  const [newAttrVal, setNewAttrVal] = useState("");

  const busy = regeneratingSection !== null || submitting;

  const entityKindLabel = (kind: string) =>
    ENTITY_KINDS.find((k) => k.value === kind)?.label ?? kind;

  const emotionLabel = (tag: string) =>
    EMOTION_TAGS.find((t) => t.value === tag)?.shortLabel ?? tag;

  const hookLabel = (hook: string) =>
    HOOK_TYPES.find((h) => h.value === hook)?.label ?? hook;

  const updateEntity = (idx: number, patch: Partial<GenesisEntity>) => {
    const next = blueprint.entities.map((ent, i) =>
      i === idx ? { ...ent, ...patch } : ent
    );
    onChangeBlueprint({ ...blueprint, entities: next });
  };

  const updateEntityAttr = (idx: number, key: string, val: string) => {
    const current = blueprint.entities[idx];
    if (!current) return;
    updateEntity(idx, {
      attributes: { ...(current.attributes ?? {}), [key]: val },
    });
  };

  const removeEntityAttr = (idx: number, key: string) => {
    const current = blueprint.entities[idx];
    if (!current) return;
    const nextAttrs = { ...(current.attributes ?? {}) };
    delete nextAttrs[key];
    updateEntity(idx, { attributes: nextAttrs });
  };

  const addEntityAttr = (idx: number) => {
    const k = newAttrKey.trim();
    if (!k) return;
    updateEntityAttr(idx, k, newAttrVal.trim());
    setNewAttrKey("");
    setNewAttrVal("");
  };

  const updateOutline = (idx: number, patch: Partial<GeneratedChapterOutline>) => {
    const next = blueprint.initialOutlines.map((ch, i) =>
      i === idx ? { ...ch, ...patch } : ch
    );
    onChangeBlueprint({ ...blueprint, initialOutlines: next });
  };

  const updateBeat = (
    outlineIdx: number,
    beatIdx: number,
    patch: Partial<Beat>
  ) => {
    const outline = blueprint.initialOutlines[outlineIdx];
    if (!outline) return;
    const nextBeats = outline.beats.map((b, i) =>
      i === beatIdx ? { ...b, ...patch } : b
    );
    updateOutline(outlineIdx, { beats: nextBeats });
  };

  const addBeat = (outlineIdx: number) => {
    const outline = blueprint.initialOutlines[outlineIdx];
    if (!outline) return;
    const nextBeats: Beat[] = [
      ...outline.beats,
      {
        id: 0,
        chapterId: 0,
        seq: outline.beats.length + 1,
        stage: "turn",
        summary: "",
        conflictNote: "",
        entityIds: [],
        targetWords: 500,
      },
    ];
    updateOutline(outlineIdx, { beats: nextBeats });
  };

  const removeBeat = (outlineIdx: number, beatIdx: number) => {
    const outline = blueprint.initialOutlines[outlineIdx];
    if (!outline) return;
    updateOutline(outlineIdx, {
      beats: outline.beats.filter((_, i) => i !== beatIdx),
    });
  };

  return (
    <div className="flex flex-col gap-6">
      {/* Compact Concept Summary Bar */}
      <section className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border-subtle bg-surface px-4 py-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2.5">
            <span className="font-prose text-base font-medium text-ink-primary">
              《{concept.title || "未命名作品"}》
            </span>
            <span className="rounded-md bg-accent-soft px-2 py-0.5 text-xs font-medium text-accent">
              {genreName}
            </span>
            {concept.pacingStyle && (
              <span className="rounded-md border border-border-subtle bg-canvas px-2 py-0.5 text-xs text-ink-secondary">
                {concept.pacingStyle}
              </span>
            )}
            <span className="text-xs text-ink-secondary">
              {getPovLabel(concept.povMode ?? "")}
            </span>
          </div>
          {concept.premise && (
            <p className="mt-1 line-clamp-1 text-xs text-ink-secondary">
              {concept.premise}
            </p>
          )}
        </div>
        <div className="flex shrink-0 items-center gap-2.5">
          <div className="relative w-28">
            <input
              type="number"
              min={300}
              max={50000}
              step={100}
              disabled={busy}
              aria-label="每章目标字数"
              value={targetWords}
              onChange={(e) =>
                onChangeTargetWords(Number.parseInt(e.target.value, 10) || 2500)
              }
              className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas py-1.5 pr-9 pl-2.5 text-xs text-ink-primary focus:border-accent disabled:opacity-60"
            />
            <span className="pointer-events-none absolute top-1/2 right-2.5 -translate-y-1/2 text-[11px] text-ink-muted">
              字/章
            </span>
          </div>
          <button
            type="button"
            disabled={busy}
            onClick={onBackToConcept}
            className="rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary disabled:opacity-40"
          >
            改立意
          </button>
        </div>
      </section>

      {/* First Volume (fully editable) */}
      <section className="rounded-xl border border-border-subtle bg-surface p-4">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-xs font-medium text-ink-secondary">第一卷规划</h2>
        </div>
        <div className="flex flex-col gap-3">
          <input
            type="text"
            disabled={busy}
            value={blueprint.firstVolume.title}
            onChange={(e) =>
              onChangeBlueprint({
                ...blueprint,
                firstVolume: { ...blueprint.firstVolume, title: e.target.value },
              })
            }
            placeholder="第一卷卷名"
            className="w-full rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] font-medium text-ink-primary focus:border-accent disabled:opacity-60"
          />
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            <div>
              <label className="mb-1 block text-[11px] text-ink-muted">卷目标</label>
              <textarea
                rows={2}
                disabled={busy}
                value={blueprint.firstVolume.goal}
                onChange={(e) =>
                  onChangeBlueprint({
                    ...blueprint,
                    firstVolume: { ...blueprint.firstVolume, goal: e.target.value },
                  })
                }
                placeholder="主角本卷要达成的阶段目标"
                className="w-full resize-none rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary focus:border-accent disabled:opacity-60"
              />
            </div>
            <div>
              <label className="mb-1 block text-[11px] text-ink-muted">卷末高潮落点</label>
              <textarea
                rows={2}
                disabled={busy}
                value={blueprint.firstVolume.climaxPlan}
                onChange={(e) =>
                  onChangeBlueprint({
                    ...blueprint,
                    firstVolume: { ...blueprint.firstVolume, climaxPlan: e.target.value },
                  })
                }
                placeholder="本卷大高潮事件与反转"
                className="w-full resize-none rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary focus:border-accent disabled:opacity-60"
              />
            </div>
          </div>
        </div>
      </section>

      {/* Entities Section (editable & regeneratable) */}
      <section className="flex flex-col gap-2.5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-xs font-medium text-ink-secondary">
            初始设定（已选 {selectedEntityIdx.size} / {blueprint.entities.length}）
          </h2>
          <div className="flex items-center gap-2">
            {regeneratingSection === "entities" ? (
              <button
                type="button"
                onClick={onStopRegenerate}
                className="flex items-center gap-1.5 rounded-lg border border-border-strong bg-surface px-2.5 py-1 text-xs text-ink-primary"
              >
                <Square className="h-3 w-3" />
                <span>停止{streamChars > 0 ? ` (${streamChars} 字)` : ""}</span>
              </button>
            ) : (
              <>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => {
                    const idx = onAddEntity();
                    setEditingEntityIdx(idx);
                  }}
                  className="flex items-center gap-1 rounded-lg border border-border-subtle bg-surface px-2.5 py-1 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary disabled:opacity-40"
                >
                  <Plus className="h-3 w-3" />
                  <span>新增设定</span>
                </button>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => {
                    setEditingEntityIdx(null);
                    onRegenerateSection("entities");
                  }}
                  className="flex items-center gap-1 rounded-lg border border-border-subtle bg-surface px-2.5 py-1 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary disabled:opacity-40"
                >
                  <RefreshCw className="h-3 w-3" />
                  <span>重生成设定</span>
                </button>
              </>
            )}
          </div>
        </div>

        <div className="divide-y divide-border-subtle rounded-xl border border-border-subtle bg-surface">
          {blueprint.entities.length === 0 ? (
            <div className="px-4 py-6 text-center text-xs text-ink-muted">
              暂无初始设定，可点击上方「新增设定」或「重生成设定」
            </div>
          ) : (
            blueprint.entities.map((ent, idx) => {
              const checked = selectedEntityIdx.has(idx);
              const isEditing = editingEntityIdx === idx;
              const attrEntries = Object.entries(ent.attributes ?? {});

              return (
                <div key={`ent-${idx}`} className="px-3.5 py-3">
                  <div className="flex items-start gap-2.5">
                    <button
                      type="button"
                      disabled={busy}
                      aria-label={`选择设定 ${ent.name}`}
                      onClick={() => onToggleEntity(idx)}
                      className={`mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-[4px] border transition-colors ${
                        checked
                          ? "border-accent bg-accent text-canvas"
                          : "border-border-strong bg-canvas"
                      }`}
                    >
                      {checked && <Check className="h-3 w-3" />}
                    </button>

                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="text-[13px] font-medium text-ink-primary">
                          {ent.name || "未命名设定"}
                        </span>
                        <span className="rounded bg-canvas px-1.5 py-0.5 text-[11px] text-ink-muted">
                          {entityKindLabel(ent.kind)}
                        </span>
                        {ent.isInvariant && (
                          <span className="rounded bg-accent-soft px-1.5 py-0.5 text-[11px] text-accent">
                            铁律
                          </span>
                        )}
                      </div>
                      {!isEditing && ent.summary && (
                        <p className="mt-1 text-xs leading-relaxed text-ink-secondary">
                          {ent.summary}
                        </p>
                      )}
                      {!isEditing && attrEntries.length > 0 && (
                        <div className="mt-1.5 flex flex-wrap gap-1.5">
                          {attrEntries.map(([k, v]) => (
                            <span
                              key={k}
                              className="rounded-md border border-border-subtle bg-canvas px-2 py-0.5 text-[11px] text-ink-secondary"
                            >
                              {k}：{v}
                            </span>
                          ))}
                        </div>
                      )}
                    </div>

                    <div className="flex shrink-0 items-center gap-1">
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => setEditingEntityIdx(isEditing ? null : idx)}
                        className={`rounded-md p-1.5 text-xs transition-colors ${
                          isEditing
                            ? "bg-accent-soft text-accent"
                            : "text-ink-muted hover:bg-canvas hover:text-ink-primary"
                        }`}
                        title={isEditing ? "收起编辑" : "编辑设定"}
                      >
                        <Pencil className="h-3.5 w-3.5" />
                      </button>
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => {
                          if (editingEntityIdx === idx) setEditingEntityIdx(null);
                          else if (editingEntityIdx !== null && editingEntityIdx > idx) {
                            setEditingEntityIdx(editingEntityIdx - 1);
                          }
                          onDeleteEntity(idx);
                        }}
                        className="rounded-md p-1.5 text-ink-muted transition-colors hover:bg-danger-soft hover:text-danger"
                        title="删除设定"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  </div>

                  {isEditing && (
                    <div className="mt-3 ml-6.5 flex flex-col gap-3 rounded-lg border border-border-subtle bg-canvas p-3">
                      <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-[1fr_110px_auto]">
                        <input
                          type="text"
                          value={ent.name}
                          onChange={(e) => updateEntity(idx, { name: e.target.value })}
                          placeholder="设定名称"
                          className="rounded-lg border border-border-subtle bg-surface px-2.5 py-1.5 text-xs font-medium text-ink-primary focus:border-accent"
                        />
                        <select
                          value={ent.kind}
                          onChange={(e) =>
                            updateEntity(idx, {
                              kind: e.target.value as EntityKind,
                              isInvariant:
                                e.target.value === "rule" ? true : ent.isInvariant,
                            })
                          }
                          className="rounded-lg border border-border-subtle bg-surface px-2 py-1.5 text-xs text-ink-primary focus:border-accent"
                        >
                          {ENTITY_KINDS.map((k) => (
                            <option key={k.value} value={k.value}>
                              {k.label}
                            </option>
                          ))}
                        </select>
                        <label className="flex cursor-pointer items-center gap-1.5 rounded-lg border border-border-subtle bg-surface px-2.5 py-1.5 text-xs text-ink-secondary">
                          <input
                            type="checkbox"
                            checked={ent.isInvariant}
                            onChange={(e) =>
                              updateEntity(idx, { isInvariant: e.target.checked })
                            }
                            className="accent-[var(--accent)]"
                          />
                          <span>世界铁律</span>
                        </label>
                      </div>

                      <textarea
                        rows={2}
                        value={ent.summary}
                        onChange={(e) => updateEntity(idx, { summary: e.target.value })}
                        placeholder="核心设定说明"
                        className="w-full resize-none rounded-lg border border-border-subtle bg-surface px-2.5 py-1.5 text-xs leading-relaxed text-ink-primary focus:border-accent"
                      />

                      <div className="flex flex-col gap-1.5">
                        <span className="text-[11px] text-ink-muted">关键属性</span>
                        {attrEntries.map(([k, v]) => (
                          <div key={k} className="flex items-center gap-2">
                            <span className="w-24 shrink-0 truncate text-xs text-ink-secondary">
                              {k}
                            </span>
                            <input
                              type="text"
                              value={v}
                              onChange={(e) => updateEntityAttr(idx, k, e.target.value)}
                              className="min-w-0 flex-1 rounded-md border border-border-subtle bg-surface px-2 py-1 text-xs text-ink-primary focus:border-accent"
                            />
                            <button
                              type="button"
                              onClick={() => removeEntityAttr(idx, k)}
                              className="rounded p-1 text-ink-muted hover:text-danger"
                              title="移除属性"
                            >
                              <Trash2 className="h-3 w-3" />
                            </button>
                          </div>
                        ))}
                        <div className="mt-1 flex items-center gap-2">
                          <input
                            type="text"
                            value={newAttrKey}
                            onChange={(e) => setNewAttrKey(e.target.value)}
                            placeholder="属性名（如：境界）"
                            className="w-28 rounded-md border border-border-subtle bg-surface px-2 py-1 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent"
                          />
                          <input
                            type="text"
                            value={newAttrVal}
                            onChange={(e) => setNewAttrVal(e.target.value)}
                            onKeyDown={(e) => {
                              if (e.key === "Enter") {
                                e.preventDefault();
                                addEntityAttr(idx);
                              }
                            }}
                            placeholder="属性值"
                            className="min-w-0 flex-1 rounded-md border border-border-subtle bg-surface px-2 py-1 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent"
                          />
                          <button
                            type="button"
                            disabled={!newAttrKey.trim()}
                            onClick={() => addEntityAttr(idx)}
                            className="rounded-md border border-border-subtle bg-surface px-2.5 py-1 text-xs text-ink-secondary hover:border-border-strong hover:text-ink-primary disabled:opacity-40"
                          >
                            添加
                          </button>
                        </div>
                      </div>
                    </div>
                  )}
                </div>
              );
            })
          )}
        </div>
      </section>

      {/* Initial Outlines & Beats Section */}
      <section className="flex flex-col gap-2.5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-xs font-medium text-ink-secondary">
            开篇章纲（已选 {selectedOutlineIdx.size} / {blueprint.initialOutlines.length}）
          </h2>
          {regeneratingSection === "outlines" ? (
            <button
              type="button"
              onClick={onStopRegenerate}
              className="flex items-center gap-1.5 rounded-lg border border-border-strong bg-surface px-2.5 py-1 text-xs text-ink-primary"
            >
              <Square className="h-3 w-3" />
              <span>停止{streamChars > 0 ? ` (${streamChars} 字)` : ""}</span>
            </button>
          ) : (
            <button
              type="button"
              disabled={busy}
              onClick={() => onRegenerateSection("outlines")}
              className="flex items-center gap-1 rounded-lg border border-border-subtle bg-surface px-2.5 py-1 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary disabled:opacity-40"
            >
              <RefreshCw className="h-3 w-3" />
              <span>
                {blueprint.initialOutlines.length > 0 ? "重生成章纲" : "生成前 3 章大纲"}
              </span>
            </button>
          )}
        </div>

        {blueprint.initialOutlines.length === 0 ? (
          <div className="rounded-xl border border-dashed border-border-subtle bg-surface/50 px-4 py-6 text-center text-xs text-ink-secondary">
            当前未生成初始章纲，建书时会自动预建空白第一章方便直接开写。
          </div>
        ) : (
          <div className="divide-y divide-border-subtle rounded-xl border border-border-subtle bg-surface">
            {blueprint.initialOutlines.map((ch, idx) => {
              const checked = selectedOutlineIdx.has(idx);
              const expanded = expandedOutlineIdx === idx;
              return (
                <div key={`outline-${idx}`} className="px-3.5 py-3">
                  <div className="flex items-start gap-2.5">
                    <button
                      type="button"
                      disabled={busy}
                      aria-label={`选择第 ${idx + 1} 章`}
                      onClick={() => onToggleOutline(idx)}
                      className={`mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-[4px] border transition-colors ${
                        checked
                          ? "border-accent bg-accent text-canvas"
                          : "border-border-strong bg-canvas"
                      }`}
                    >
                      {checked && <Check className="h-3 w-3" />}
                    </button>

                    <button
                      type="button"
                      onClick={() => setExpandedOutlineIdx(expanded ? null : idx)}
                      className="min-w-0 flex-1 text-left"
                    >
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="text-[13px] font-medium text-ink-primary">
                          第 {idx + 1} 章 {ch.title}
                        </span>
                        <span className="rounded bg-canvas px-1.5 py-0.5 text-[11px] text-ink-muted">
                          {emotionLabel(ch.emotionTag)}
                        </span>
                        <span className="rounded bg-canvas px-1.5 py-0.5 text-[11px] text-ink-muted">
                          {hookLabel(ch.hookType)}
                        </span>
                        <span className="nib-mono text-[11px] text-ink-muted">
                          {ch.beats.length} 节拍
                        </span>
                      </div>
                      {!expanded && ch.outlineSummary && (
                        <p className="mt-1 line-clamp-2 text-xs leading-relaxed text-ink-secondary">
                          {ch.outlineSummary}
                        </p>
                      )}
                    </button>

                    <button
                      type="button"
                      onClick={() => setExpandedOutlineIdx(expanded ? null : idx)}
                      className="rounded-md p-1 text-ink-muted transition-colors hover:bg-canvas hover:text-ink-primary"
                      aria-label={expanded ? "收起章节详情" : "展开章节详情"}
                    >
                      {expanded ? (
                        <ChevronDown className="h-4 w-4" />
                      ) : (
                        <ChevronRight className="h-4 w-4" />
                      )}
                    </button>
                  </div>

                  {expanded && (
                    <div className="mt-3 ml-6.5 flex flex-col gap-3 rounded-lg border border-border-subtle bg-canvas p-3">
                      <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-[1fr_110px_115px]">
                        <input
                          type="text"
                          disabled={busy}
                          value={ch.title}
                          onChange={(e) => updateOutline(idx, { title: e.target.value })}
                          placeholder="章节标题"
                          className="rounded-lg border border-border-subtle bg-surface px-2.5 py-1.5 text-xs font-medium text-ink-primary focus:border-accent"
                        />
                        <select
                          disabled={busy}
                          value={ch.emotionTag}
                          onChange={(e) =>
                            updateOutline(idx, {
                              emotionTag: e.target.value as EmotionTag,
                            })
                          }
                          className="rounded-lg border border-border-subtle bg-surface px-2 py-1.5 text-xs text-ink-primary focus:border-accent"
                        >
                          {EMOTION_TAGS.map((t) => (
                            <option key={t.value} value={t.value}>
                              {t.label}
                            </option>
                          ))}
                        </select>
                        <select
                          disabled={busy}
                          value={ch.hookType}
                          onChange={(e) =>
                            updateOutline(idx, {
                              hookType: e.target.value as HookType,
                            })
                          }
                          className="rounded-lg border border-border-subtle bg-surface px-2 py-1.5 text-xs text-ink-primary focus:border-accent"
                        >
                          {HOOK_TYPES.map((h) => (
                            <option key={h.value} value={h.value}>
                              {h.label}
                            </option>
                          ))}
                        </select>
                      </div>

                      <textarea
                        rows={2}
                        disabled={busy}
                        value={ch.outlineSummary}
                        onChange={(e) =>
                          updateOutline(idx, { outlineSummary: e.target.value })
                        }
                        placeholder="本章剧情推进摘要"
                        className="w-full resize-none rounded-lg border border-border-subtle bg-surface px-2.5 py-1.5 text-xs leading-relaxed text-ink-primary focus:border-accent"
                      />

                      <input
                        type="text"
                        disabled={busy}
                        value={ch.hookNote}
                        onChange={(e) => updateOutline(idx, { hookNote: e.target.value })}
                        placeholder="章末悬念钩子落点"
                        className="w-full rounded-lg border border-border-subtle bg-surface px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent"
                      />

                      {/* Beats */}
                      <div className="flex flex-col gap-2">
                        <div className="flex items-center justify-between">
                          <span className="text-[11px] font-medium text-ink-muted">
                            剧情节拍（{ch.beats.length}）
                          </span>
                          <button
                            type="button"
                            disabled={busy}
                            onClick={() => addBeat(idx)}
                            className="flex items-center gap-1 text-[11px] text-ink-secondary transition-colors hover:text-accent"
                          >
                            <Plus className="h-3 w-3" />
                            <span>添加节拍</span>
                          </button>
                        </div>

                        {ch.beats.map((beat, bIdx) => (
                          <div
                            key={`beat-${idx}-${bIdx}`}
                            className="flex items-start gap-2 rounded-lg border border-border-subtle bg-surface p-2"
                          >
                            <select
                              disabled={busy}
                              value={beat.stage}
                              onChange={(e) =>
                                updateBeat(idx, bIdx, {
                                  stage: e.target.value as BeatStage,
                                })
                              }
                              className="w-20 shrink-0 rounded-md border border-border-subtle bg-canvas px-1.5 py-1 text-[11px] text-ink-primary focus:border-accent"
                            >
                              {BEAT_STAGES.map((st) => (
                                <option key={st.value} value={st.value}>
                                  {st.shortLabel ?? st.label}
                                </option>
                              ))}
                            </select>
                            <div className="flex min-w-0 flex-1 flex-col gap-1.5">
                              <input
                                type="text"
                                disabled={busy}
                                value={beat.summary}
                                onChange={(e) =>
                                  updateBeat(idx, bIdx, { summary: e.target.value })
                                }
                                placeholder="节拍情节"
                                className="w-full rounded-md border border-border-subtle bg-canvas px-2 py-1 text-xs text-ink-primary focus:border-accent"
                              />
                              <input
                                type="text"
                                disabled={busy}
                                value={beat.conflictNote}
                                onChange={(e) =>
                                  updateBeat(idx, bIdx, {
                                    conflictNote: e.target.value,
                                  })
                                }
                                placeholder="冲突筹码 / 反转点（选填）"
                                className="w-full rounded-md border border-border-subtle bg-canvas px-2 py-1 text-[11px] text-ink-secondary placeholder:text-ink-muted focus:border-accent"
                              />
                            </div>
                            <button
                              type="button"
                              disabled={busy}
                              onClick={() => removeBeat(idx, bIdx)}
                              className="rounded p-1 text-ink-muted hover:text-danger"
                              title="删除节拍"
                            >
                              <Trash2 className="h-3 w-3" />
                            </button>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </section>

      {/* Export Dir (subtle optional row) */}
      <section className="flex flex-wrap items-center gap-2 rounded-xl border border-border-subtle bg-surface/60 px-3.5 py-2.5">
        <span className="shrink-0 text-xs text-ink-secondary">本地备份目录（选填）</span>
        <input
          type="text"
          disabled={busy}
          value={exportDir}
          onChange={(e) => onChangeExportDir(e.target.value)}
          placeholder="配置后，定稿章节会自动镜像导出为 Markdown"
          className="min-w-0 flex-1 bg-transparent text-xs text-ink-primary placeholder:text-ink-muted focus:outline-none"
        />
        <button
          type="button"
          disabled={busy}
          onClick={onPickExportDir}
          className="flex shrink-0 items-center gap-1.5 rounded-lg border border-border-subtle bg-canvas px-2.5 py-1 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary"
        >
          <FolderOpen className="h-3.5 w-3.5" />
          <span>选择</span>
        </button>
      </section>

      {/* Footer Actions */}
      <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border-subtle pt-4">
        <button
          type="button"
          disabled={busy}
          onClick={onBackToConcept}
          className="flex items-center gap-1.5 rounded-lg px-3 py-2 text-[13px] font-medium text-ink-secondary transition-colors hover:bg-surface hover:text-ink-primary disabled:opacity-40"
        >
          <ArrowLeft className="h-3.5 w-3.5" />
          <span>返回改立意</span>
        </button>

        <div className="flex items-center gap-2.5">
          <button
            type="button"
            disabled={busy}
            onClick={() => onApply("architect")}
            className="rounded-lg border border-border-strong bg-surface px-4 py-2 text-[13px] font-medium text-ink-primary transition-colors hover:border-accent/50 disabled:opacity-50"
          >
            建书并看大纲
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={() => onApply("studio")}
            className="flex items-center gap-1.5 rounded-lg bg-accent px-5 py-2 text-[13px] font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
          >
            {submitting && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            <span>{submitting ? "创建中…" : "建书并开始写作"}</span>
          </button>
        </div>
      </div>
    </div>
  );
}
