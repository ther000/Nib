import { useMemo, useState, type RefObject } from "react";
import { ArrowDown, ArrowUp, LayoutList, Plus, Search, Trash2, X } from "lucide-react";
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
  onUpdateBeat: (localKey: string, patch: Partial<EditableBeat>) => void;
  onMoveBeat: (index: number, delta: -1 | 1) => void;
  onRemoveBeat: (localKey: string) => void;
  onToggleBeatEntity: (localKey: string, entityId: number) => void;
}

export function BeatEditorList({
  beatListRef,
  beats,
  beatsLoading,
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
  onUpdateBeat,
  onMoveBeat,
  onRemoveBeat,
  onToggleBeatEntity,
}: BeatEditorListProps) {
  const [pickerQuery, setPickerQuery] = useState("");

  // 视角人物仅筛选角色类设定
  const characterEntities = useMemo(
    () => entities.filter((e) => e.kind === "character"),
    [entities]
  );

  const filteredPickerEntities = useMemo(() => {
    const q = pickerQuery.trim().toLowerCase();
    if (!q) return entities;
    return entities.filter(
      (e) =>
        e.name.toLowerCase().includes(q) ||
        getEntityKindLabel(e.kind).includes(q) ||
        (e.aliases ?? []).some((a) => a.toLowerCase().includes(q))
    );
  }, [entities, pickerQuery]);

  return (
    <section ref={beatListRef} className="flex flex-col gap-3 pb-10">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2.5">
          <h2 className="text-sm font-medium text-ink-primary">剧情节拍</h2>
          <span className="nib-mono text-xs text-ink-muted">
            {beats.length} 段 · 约 {formatNumber(totalBeatWords)} 字
          </span>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {confirmFillBeats ? (
            <div className="flex items-center gap-1.5 rounded-lg border border-warn/40 bg-warn-soft px-2.5 py-1 text-xs">
              <span className="text-warn">替换当前 {beats.length} 段节拍？</span>
              <button
                type="button"
                onClick={onApplyGenreBeatTemplates}
                className="rounded bg-warn px-2 py-0.5 text-xs font-medium text-canvas"
              >
                确定
              </button>
              <button
                type="button"
                onClick={() => onSetConfirmFillBeats(false)}
                aria-label="取消替换"
                className="text-ink-secondary hover:text-ink-primary"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>
          ) : (
            <button
              type="button"
              onClick={onApplyGenreBeatTemplates}
              className="flex items-center gap-1.5 rounded-lg border border-border-subtle bg-surface px-2.5 py-1.5 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary"
            >
              <LayoutList className="h-3.5 w-3.5 text-accent" />
              <span>套用题材模板</span>
            </button>
          )}

          <button
            type="button"
            onClick={onAddBeat}
            className="flex items-center gap-1 rounded-lg border border-accent/35 bg-accent-soft px-3 py-1.5 text-xs font-medium text-accent transition-colors hover:border-accent"
          >
            <Plus className="h-3.5 w-3.5" />
            <span>添加节拍</span>
          </button>
        </div>
      </div>

      {beatsLoading ? (
        <div className="rounded-lg border border-border-subtle bg-surface/50 px-4 py-10 text-center text-xs text-ink-muted">
          正在读取剧情节拍…
        </div>
      ) : beats.length === 0 ? (
        <div className="flex flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-border-strong bg-surface/35 px-6 py-10 text-center">
          <div className="text-xs text-ink-secondary">
            还没有剧情节拍（建议每章安排 3~6 段）
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={onApplyGenreBeatTemplates}
              className="flex items-center gap-1.5 rounded-lg border border-accent/35 bg-accent-soft px-3 py-1.5 text-xs font-medium text-accent hover:border-accent"
            >
              <LayoutList className="h-3.5 w-3.5" />
              <span>按「{activeGenreName}」生成默认节拍</span>
            </button>
            <button
              type="button"
              onClick={onAddBeat}
              className="rounded-lg border border-border-subtle bg-surface px-3 py-1.5 text-xs text-ink-secondary hover:text-ink-primary"
            >
              手动新增
            </button>
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          {beats.map((beat, idx) => {
            const pickerOpen = openEntityPickerKey === beat.localKey;
            const currentPov = beat.povEntityId ? entityMap.get(beat.povEntityId) : undefined;
            const povOptions =
              currentPov && currentPov.kind !== "character"
                ? [currentPov, ...characterEntities]
                : characterEntities;

            return (
              <div
                key={beat.localKey}
                className={`rounded-xl border border-border-subtle border-l-[3px] ${getBeatAccentClass(
                  beat.stage
                )} bg-surface p-3.5 transition-colors`}
              >
                {/* Card Top Row */}
                <div className="flex flex-wrap items-center gap-2">
                  <span className="nib-mono text-xs font-medium text-ink-muted">
                    #{idx + 1}
                  </span>

                  <select
                    aria-label="节拍阶段"
                    value={beat.stage}
                    onChange={(e) =>
                      onUpdateBeat(beat.localKey, { stage: e.target.value })
                    }
                    className="rounded-lg border border-border-subtle bg-canvas px-2 py-1 text-xs font-medium text-ink-primary focus:border-accent"
                  >
                    {BEAT_STAGES.map((st) => (
                      <option key={st.value} value={st.value}>
                        {st.label}
                      </option>
                    ))}
                  </select>

                  {povOptions.length > 0 && (
                    <div className="flex items-center gap-1.5 text-xs text-ink-secondary">
                      <span className="text-xs text-ink-muted">视角</span>
                      <select
                        aria-label="视角人物"
                        value={beat.povEntityId ?? ""}
                        onChange={(e) => {
                          const raw = e.target.value;
                          onUpdateBeat(beat.localKey, {
                            povEntityId: raw ? Number(raw) : undefined,
                          });
                        }}
                        className="max-w-[130px] rounded-lg border border-border-subtle bg-canvas px-2 py-1 text-xs text-ink-primary focus:border-accent"
                      >
                        <option value="">默认</option>
                        {povOptions.map((ent) => (
                          <option key={ent.id} value={ent.id}>
                            {ent.name}
                          </option>
                        ))}
                      </select>
                    </div>
                  )}

                  <div className="flex items-center gap-1">
                    <input
                      type="number"
                      aria-label="目标字数"
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
                      className="nib-mono w-[72px] rounded-lg border border-border-subtle bg-canvas px-2 py-1 text-right text-xs text-ink-primary focus:border-accent"
                    />
                    <span className="text-xs text-ink-muted">字</span>
                  </div>

                  <div className="ml-auto flex items-center gap-0.5">
                    <button
                      type="button"
                      disabled={idx === 0}
                      onClick={() => onMoveBeat(idx, -1)}
                      title="上移"
                      aria-label="上移节拍"
                      className="rounded p-1 text-ink-muted transition-colors hover:bg-elevated hover:text-ink-primary disabled:opacity-30"
                    >
                      <ArrowUp className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      disabled={idx === beats.length - 1}
                      onClick={() => onMoveBeat(idx, 1)}
                      title="下移"
                      aria-label="下移节拍"
                      className="rounded p-1 text-ink-muted transition-colors hover:bg-elevated hover:text-ink-primary disabled:opacity-30"
                    >
                      <ArrowDown className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      onClick={() => onRemoveBeat(beat.localKey)}
                      title="移除节拍"
                      aria-label="移除节拍"
                      className="rounded p-1 text-ink-muted transition-colors hover:bg-danger-soft hover:text-danger"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                </div>

                {/* Summary & Conflict Note */}
                <div className="mt-2.5 flex flex-col gap-2">
                  <textarea
                    rows={2}
                    value={beat.summary}
                    onChange={(e) =>
                      onUpdateBeat(beat.localKey, { summary: e.target.value })
                    }
                    placeholder="这一段发生什么（具体行动、关键反转或结果落袋）"
                    className="w-full resize-y rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent"
                  />
                  <input
                    type="text"
                    value={beat.conflictNote}
                    onChange={(e) =>
                      onUpdateBeat(beat.localKey, { conflictNote: e.target.value })
                    }
                    placeholder="冲突 / 限制要点（可选，如：不可提前暴露底牌）"
                    className="w-full rounded-lg border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent"
                  />
                </div>

                {/* Participating Entities Chips */}
                <div
                  data-entity-picker
                  className="relative mt-2.5 flex flex-wrap items-center gap-1.5"
                >
                  <span className="text-xs text-ink-muted">出场角色与道具</span>
                  {beat.entityIds.map((eid) => {
                    const ent = entityMap.get(eid);
                    if (!ent) return null;
                    return (
                      <span
                        key={eid}
                        className="inline-flex items-center gap-1 rounded-full border border-border-subtle bg-elevated px-2 py-0.5 text-xs text-ink-primary"
                      >
                        <span>{ent.name}</span>
                        <button
                          type="button"
                          aria-label={`移除 ${ent.name}`}
                          onClick={() => onToggleBeatEntity(beat.localKey, eid)}
                          className="text-ink-muted hover:text-danger"
                        >
                          <X className="h-3 w-3" />
                        </button>
                      </span>
                    );
                  })}
                  {entities.length > 0 && (
                    <button
                      type="button"
                      onClick={() => {
                        setPickerQuery("");
                        onSetOpenEntityPickerKey(pickerOpen ? null : beat.localKey);
                      }}
                      className="inline-flex items-center gap-1 rounded-full border border-dashed border-border-strong px-2 py-0.5 text-xs text-ink-secondary transition-colors hover:border-accent hover:text-accent"
                    >
                      <Plus className="h-3 w-3" />
                      <span>关联</span>
                    </button>
                  )}

                  {pickerOpen && (
                    <div className="absolute top-[calc(100%+6px)] left-0 z-20 flex w-64 flex-col overflow-hidden rounded-lg border border-border-strong bg-elevated shadow-[var(--shadow-elevated)]">
                      {entities.length >= 6 && (
                        <div className="relative border-b border-border-subtle p-1.5">
                          <Search className="pointer-events-none absolute top-1/2 left-3.5 h-3 w-3 -translate-y-1/2 text-ink-muted" />
                          <input
                            type="text"
                            autoFocus
                            value={pickerQuery}
                            onChange={(e) => setPickerQuery(e.target.value)}
                            placeholder="搜索设定…"
                            className="w-full rounded border border-border-subtle bg-canvas py-1 pr-2 pl-6 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent"
                          />
                        </div>
                      )}
                      <div className="max-h-52 overflow-y-auto p-1">
                        {filteredPickerEntities.length === 0 ? (
                          <div className="py-4 text-center text-xs text-ink-muted">
                            无匹配设定
                          </div>
                        ) : (
                          filteredPickerEntities.map((ent) => {
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
                                <span className="ml-2 shrink-0 text-xs text-ink-muted">
                                  {getEntityKindLabel(ent.kind)}
                                </span>
                              </button>
                            );
                          })
                        )}
                      </div>
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
