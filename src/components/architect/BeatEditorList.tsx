import type { RefObject } from "react";
import { ArrowDown, ArrowUp, Plus, Save, Sparkles, Trash2, X } from "lucide-react";
import type { Entity } from "../../mygo";
import {
  BEAT_STAGES,
  formatNumber,
  getBeatAccentClass,
  getEntityKindLabel,
} from "../../lib/constants";

export interface EditableBeat {
  localKey: string;
  id: number;
  stage: string;
  summary: string;
  conflictNote: string;
  povEntityId?: number;
  entityIds: number[];
  targetWords: number;
}

interface BeatEditorListProps {
  beatListRef: RefObject<HTMLDivElement | null>;
  beats: EditableBeat[];
  beatsLoading: boolean;
  beatsDirty: boolean;
  savingBeats: boolean;
  totalBeatWords: number;
  confirmFillBeats: boolean;
  activeGenreName: string;
  entities: Entity[];
  entityMap: Map<number, Entity>;
  openEntityPickerKey: string | null;
  onSetOpenEntityPickerKey: (key: string | null) => void;
  onSetConfirmFillBeats: (value: boolean) => void;
  onApplyGenreBeatTemplates: () => void;
  onAddBeat: () => void;
  onSaveBeats: () => void;
  onUpdateBeat: (localKey: string, patch: Partial<EditableBeat>) => void;
  onMoveBeat: (index: number, delta: -1 | 1) => void;
  onRemoveBeat: (localKey: string) => void;
  onToggleBeatEntity: (localKey: string, entityId: number) => void;
}

export function BeatEditorList({
  beatListRef,
  beats,
  beatsLoading,
  beatsDirty,
  savingBeats,
  totalBeatWords,
  confirmFillBeats,
  activeGenreName,
  entities,
  entityMap,
  openEntityPickerKey,
  onSetOpenEntityPickerKey,
  onSetConfirmFillBeats,
  onApplyGenreBeatTemplates,
  onAddBeat,
  onSaveBeats,
  onUpdateBeat,
  onMoveBeat,
  onRemoveBeat,
  onToggleBeatEntity,
}: BeatEditorListProps) {
  return (
    <section ref={beatListRef} className="flex flex-col gap-3 pb-10">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2.5">
          <h2 className="text-sm font-medium text-ink-primary">剧情节拍</h2>
          <span className="nib-mono text-xs text-ink-muted">
            {beats.length} 段 目标 {formatNumber(totalBeatWords)} 字
          </span>
          {beatsDirty && (
            <span className="rounded bg-warn-soft px-1.5 py-0.5 text-xs text-warn">
              未保存
            </span>
          )}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {confirmFillBeats ? (
            <div className="flex items-center gap-1.5 rounded-md border border-warn/40 bg-warn-soft px-2 py-1 text-xs">
              <span className="text-warn">替换当前 {beats.length} 张节拍？</span>
              <button
                type="button"
                onClick={onApplyGenreBeatTemplates}
                className="rounded bg-warn px-2 py-0.5 text-[11px] font-medium text-canvas"
              >
                确定
              </button>
              <button
                type="button"
                onClick={() => onSetConfirmFillBeats(false)}
                className="text-ink-secondary hover:text-ink-primary"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>
          ) : (
            <button
              type="button"
              onClick={onApplyGenreBeatTemplates}
              className="flex items-center gap-1.5 rounded-md border border-border-subtle bg-surface px-2.5 py-1.5 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary"
            >
              <Sparkles className="h-3.5 w-3.5 text-accent" />
              <span>按题材填充默认节拍</span>
            </button>
          )}

          <button
            type="button"
            onClick={onAddBeat}
            className="flex items-center gap-1 rounded-md border border-border-subtle bg-surface px-2.5 py-1.5 text-xs text-ink-primary transition-colors hover:border-border-strong hover:bg-elevated"
          >
            <Plus className="h-3.5 w-3.5 text-accent" />
            <span>添加节拍</span>
          </button>

          <button
            type="button"
            disabled={savingBeats || !beatsDirty}
            onClick={onSaveBeats}
            className="flex items-center gap-1 rounded-md border border-accent/35 bg-accent-soft px-3 py-1.5 text-xs font-medium text-accent transition-colors hover:border-accent disabled:opacity-40"
          >
            <Save className="h-3.5 w-3.5" />
            <span>{savingBeats ? "保存中…" : "保存节拍"}</span>
          </button>
        </div>
      </div>

      {beatsLoading ? (
        <div className="rounded-lg border border-border-subtle bg-surface/50 px-4 py-10 text-center text-xs text-ink-muted">
          正在读取节拍卡…
        </div>
      ) : beats.length === 0 ? (
        <div className="flex flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-border-strong bg-surface/35 px-6 py-10 text-center">
          <div className="text-xs text-ink-secondary">
            尚未编排节拍卡（建议单章 3~6 张）
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={onApplyGenreBeatTemplates}
              className="flex items-center gap-1.5 rounded-md border border-accent/35 bg-accent-soft px-3 py-1.5 text-xs font-medium text-accent hover:border-accent"
            >
              <Sparkles className="h-3.5 w-3.5" />
              <span>按「{activeGenreName}」填充默认节拍</span>
            </button>
            <button
              type="button"
              onClick={onAddBeat}
              className="rounded-md border border-border-subtle bg-surface px-3 py-1.5 text-xs text-ink-secondary hover:text-ink-primary"
            >
              手动新增
            </button>
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          {beats.map((beat, idx) => {
            const pickerOpen = openEntityPickerKey === beat.localKey;
            return (
              <div
                key={beat.localKey}
                className={`rounded-xl border border-border-subtle border-l-[3px] ${getBeatAccentClass(
                  beat.stage
                )} bg-surface p-4 transition-colors`}
              >
                {/* Card Top Row */}
                <div className="flex flex-wrap items-center gap-2">
                  <span className="nib-mono text-xs font-medium text-ink-muted">
                    #{idx + 1}
                  </span>

                  <select
                    value={beat.stage}
                    onChange={(e) =>
                      onUpdateBeat(beat.localKey, { stage: e.target.value })
                    }
                    className="rounded-md border border-border-subtle bg-canvas px-2 py-1 text-xs font-medium text-ink-primary focus:border-accent focus:outline-none"
                  >
                    {BEAT_STAGES.map((st) => (
                      <option key={st.value} value={st.value}>
                        {st.label}
                      </option>
                    ))}
                  </select>

                  <div className="flex items-center gap-1.5 text-xs text-ink-secondary">
                    <span className="text-xs text-ink-muted">视角人物</span>
                    <select
                      value={beat.povEntityId ?? ""}
                      onChange={(e) => {
                        const raw = e.target.value;
                        onUpdateBeat(beat.localKey, {
                          povEntityId: raw ? Number(raw) : undefined,
                        });
                      }}
                      className="max-w-[140px] rounded-md border border-border-subtle bg-canvas px-2 py-1 text-xs text-ink-primary focus:border-accent focus:outline-none"
                    >
                      <option value="">不指定</option>
                      {entities.map((ent) => (
                        <option key={ent.id} value={ent.id}>
                          {ent.name}
                        </option>
                      ))}
                    </select>
                  </div>

                  <div className="flex items-center gap-1">
                    <input
                      type="number"
                      min={100}
                      max={10000}
                      step={50}
                      value={beat.targetWords}
                      onChange={(e) =>
                        onUpdateBeat(beat.localKey, {
                          targetWords: Math.max(
                            0,
                            Number.parseInt(e.target.value, 10) || 0
                          ),
                        })
                      }
                      className="nib-mono w-[74px] rounded-md border border-border-subtle bg-canvas px-2 py-1 text-right text-xs text-ink-primary focus:border-accent focus:outline-none"
                    />
                    <span className="text-[11px] text-ink-muted">字</span>
                  </div>

                  <div className="ml-auto flex items-center gap-0.5">
                    <button
                      type="button"
                      disabled={idx === 0}
                      onClick={() => onMoveBeat(idx, -1)}
                      title="上移"
                      className="rounded p-1 text-ink-muted transition-colors hover:bg-elevated hover:text-ink-primary disabled:opacity-30"
                    >
                      <ArrowUp className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      disabled={idx === beats.length - 1}
                      onClick={() => onMoveBeat(idx, 1)}
                      title="下移"
                      className="rounded p-1 text-ink-muted transition-colors hover:bg-elevated hover:text-ink-primary disabled:opacity-30"
                    >
                      <ArrowDown className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      onClick={() => onRemoveBeat(beat.localKey)}
                      title="移除节拍"
                      className="rounded p-1 text-ink-muted transition-colors hover:bg-danger-soft hover:text-danger"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                </div>

                {/* Summary & Conflict Note */}
                <div className="mt-3 flex flex-col gap-2">
                  <textarea
                    rows={2}
                    value={beat.summary}
                    onChange={(e) =>
                      onUpdateBeat(beat.localKey, { summary: e.target.value })
                    }
                    placeholder="节拍推进摘要（具体行动、关键反转或结果落袋）"
                    className="w-full resize-none rounded-md border border-border-subtle bg-canvas px-3 py-2 text-[13px] leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                  />
                  <input
                    type="text"
                    value={beat.conflictNote}
                    onChange={(e) =>
                      onUpdateBeat(beat.localKey, { conflictNote: e.target.value })
                    }
                    placeholder="冲突 / 博弈要点（可选，例如：不可提前暴露底牌）"
                    className="w-full rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                  />
                </div>

                {/* Participating Entities Chips */}
                <div
                  data-entity-picker
                  className="relative mt-2.5 flex flex-wrap items-center gap-1.5"
                >
                  <span className="text-xs text-ink-muted">登场角色与道具</span>
                  {beat.entityIds.map((eid) => {
                    const ent = entityMap.get(eid);
                    if (!ent) return null;
                    return (
                      <span
                        key={eid}
                        className="inline-flex items-center gap-1 rounded-full border border-border-subtle bg-elevated px-2 py-0.5 text-[11px] text-ink-primary"
                      >
                        <span>{ent.name}</span>
                        <button
                          type="button"
                          onClick={() => onToggleBeatEntity(beat.localKey, eid)}
                          className="text-ink-muted hover:text-danger"
                        >
                          <X className="h-2.5 w-2.5" />
                        </button>
                      </span>
                    );
                  })}
                  {entities.length > 0 && (
                    <button
                      type="button"
                      onClick={() =>
                        onSetOpenEntityPickerKey(pickerOpen ? null : beat.localKey)
                      }
                      className="inline-flex items-center gap-1 rounded-full border border-dashed border-border-strong px-2 py-0.5 text-[11px] text-ink-secondary transition-colors hover:border-accent hover:text-accent"
                    >
                      <Plus className="h-2.5 w-2.5" />
                      <span>关联</span>
                    </button>
                  )}

                  {pickerOpen && (
                    <div className="absolute top-[calc(100%+6px)] left-0 z-20 max-h-52 w-64 overflow-y-auto rounded-lg border border-border-strong bg-elevated p-1.5 shadow-[var(--shadow-elevated)]">
                      {entities.map((ent) => {
                        const checked = beat.entityIds.includes(ent.id);
                        return (
                          <button
                            key={ent.id}
                            type="button"
                            onClick={() => onToggleBeatEntity(beat.localKey, ent.id)}
                            className={`flex w-full items-center justify-between rounded px-2 py-1.5 text-left text-xs transition-colors ${
                              checked
                                ? "bg-accent-soft text-accent"
                                : "text-ink-primary hover:bg-surface"
                            }`}
                          >
                            <span className="truncate">{ent.name}</span>
                            <span className="ml-2 shrink-0 text-[10px] text-ink-muted">
                              {getEntityKindLabel(ent.kind)}
                            </span>
                          </button>
                        );
                      })}
                    </div>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}
