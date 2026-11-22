import { useState } from "react";
import { AlertTriangle, Ban, ChevronDown, Pin } from "lucide-react";
import type { Briefing, Entity } from "../../mygo";
import {
  formatCompactTokens,
  formatNumber,
  getEntityKindLabel,
} from "../../lib/constants";

interface BriefingDrawerTabProps {
  briefing: Briefing | null;
  briefingLoading: boolean;
  briefingError: string | null;
  extraInstruction: string;
  searchKeywordsText: string;
  includePending: boolean;
  entities: Entity[];
  pinnedEntityIds: Set<number>;
  excludedEntityIds: Set<number>;
  expandedSlots: Set<string>;
  onChangeExtraInstruction: (value: string) => void;
  onChangeSearchKeywordsText: (value: string) => void;
  onChangeIncludePending: (value: boolean) => void;
  onTogglePinEntity: (entityId: number) => void;
  onToggleExcludeEntity: (entityId: number) => void;
  onToggleSlotExpand: (key: string) => void;
}

export function BriefingDrawerTab({
  briefing,
  briefingLoading,
  briefingError,
  extraInstruction,
  searchKeywordsText,
  includePending,
  entities,
  pinnedEntityIds,
  excludedEntityIds,
  expandedSlots,
  onChangeExtraInstruction,
  onChangeSearchKeywordsText,
  onChangeIncludePending,
  onTogglePinEntity,
  onToggleExcludeEntity,
  onToggleSlotExpand,
}: BriefingDrawerTabProps) {
  const [entityControlsOpen, setEntityControlsOpen] = useState(false);
  const [slotsDetailOpen, setSlotsDetailOpen] = useState(false);

  const tokenRatio =
    briefing && briefing.totalTokenBudget > 0
      ? Math.min(
          100,
          Math.round((briefing.totalTokenEst / briefing.totalTokenBudget) * 100)
        )
      : 0;

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto px-3.5 py-3">
      {briefingError && (
        <div className="mb-3 rounded-lg border border-danger/30 bg-danger-soft px-3 py-2 text-xs text-danger">
          {briefingError}
        </div>
      )}

      {/* Writer-Facing Generation Controls */}
      <div className="mb-3 flex flex-col gap-2.5">
        <div>
          <label className="mb-1 block text-xs font-medium text-ink-secondary">
            本章写作补充要求
          </label>
          <textarea
            rows={2}
            value={extraInstruction}
            onChange={(e) => onChangeExtraInstruction(e.target.value)}
            placeholder="例如：压低配角台词字数，突出招式细节"
            className="w-full resize-none rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent"
          />
        </div>

        <div>
          <label className="mb-1 block text-xs font-medium text-ink-secondary">
            额外提醒关键词（逗号分隔）
          </label>
          <input
            type="text"
            value={searchKeywordsText}
            onChange={(e) => onChangeSearchKeywordsText(e.target.value)}
            placeholder="例如：血玉令牌, 破障丹, 残阵"
            className="w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent"
          />
        </div>

        <label className="flex cursor-pointer items-center justify-between rounded-lg border border-border-subtle bg-canvas/60 px-2.5 py-1.5 text-xs text-ink-secondary select-none">
          <span>包含尚未确认的连载变动</span>
          <input
            type="checkbox"
            checked={includePending}
            onChange={(e) => onChangeIncludePending(e.target.checked)}
            className="h-3.5 w-3.5 accent-[var(--accent)]"
          />
        </label>
      </div>

      {/* Pin / Exclude Entities */}
      <div className="mb-3 rounded-lg border border-border-subtle bg-canvas/50">
        <button
          type="button"
          onClick={() => setEntityControlsOpen((v) => !v)}
          className="flex w-full items-center justify-between px-3 py-2 text-left text-xs"
        >
          <div className="flex items-center gap-2">
            <span className="font-medium text-ink-primary">指定出场 / 排除设定</span>
            {(pinnedEntityIds.size > 0 || excludedEntityIds.size > 0) && (
              <span className="nib-mono text-xs text-accent">
                +{pinnedEntityIds.size} / -{excludedEntityIds.size}
              </span>
            )}
          </div>
          <ChevronDown
            className={`h-3.5 w-3.5 text-ink-muted transition-transform ${
              entityControlsOpen ? "rotate-180" : ""
            }`}
          />
        </button>

        {entityControlsOpen && (
          <div className="max-h-48 overflow-y-auto border-t border-border-subtle px-2 py-1.5">
            {entities.length === 0 ? (
              <div className="px-2 py-2 text-xs text-ink-muted">
                本书暂无设定档案
              </div>
            ) : (
              <div className="flex flex-col gap-1">
                {entities.map((ent) => {
                  const pinned = pinnedEntityIds.has(ent.id);
                  const excluded = excludedEntityIds.has(ent.id);
                  const included =
                    ent.isInvariant ||
                    (briefing?.includedEntities?.includes(ent.id) ?? false);
                  return (
                    <div
                      key={ent.id}
                      className="flex items-center justify-between gap-2 rounded px-2 py-1 text-xs hover:bg-surface"
                    >
                      <div className="flex min-w-0 items-center gap-1.5">
                        <span
                          className={`h-1.5 w-1.5 shrink-0 rounded-full ${
                            included ? "bg-ok" : "bg-ink-muted/40"
                          }`}
                          title={
                            ent.isInvariant
                              ? "世界铁律（每章必带）"
                              : included
                              ? "已带入本章参考"
                              : "未带入当前参考"
                          }
                        />
                        <span
                          className={`truncate ${
                            excluded
                              ? "text-ink-muted line-through"
                              : "text-ink-primary"
                          }`}
                        >
                          {ent.name}
                        </span>
                        <span className="shrink-0 text-xs text-ink-muted">
                          {getEntityKindLabel(ent.kind)}
                        </span>
                      </div>
                      <div className="flex shrink-0 items-center gap-1">
                        <button
                          type="button"
                          onClick={() => onTogglePinEntity(ent.id)}
                          title="固定带入本章"
                          aria-label={`固定带入 ${ent.name}`}
                          className={`rounded p-1 transition-colors ${
                            pinned
                              ? "bg-accent-soft text-accent"
                              : "text-ink-muted hover:bg-elevated hover:text-ink-primary"
                          }`}
                        >
                          <Pin className="h-3 w-3" />
                        </button>
                        <button
                          type="button"
                          disabled={ent.isInvariant}
                          onClick={() => onToggleExcludeEntity(ent.id)}
                          title={
                            ent.isInvariant ? "世界铁律不可排除" : "本章排除"
                          }
                          aria-label={`本章排除 ${ent.name}`}
                          className={`rounded p-1 transition-colors disabled:opacity-25 ${
                            excluded
                              ? "bg-danger-soft text-danger"
                              : "text-ink-muted hover:bg-elevated hover:text-danger"
                          }`}
                        >
                          <Ban className="h-3 w-3" />
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        )}
      </div>

      {/* Warnings (always visible when present) */}
      {briefing?.warnings && briefing.warnings.length > 0 && (
        <div className="mb-3 flex flex-col gap-1 rounded-lg border border-warn/35 bg-warn-soft/50 p-2.5">
          {briefing.warnings.map((w, idx) => (
            <div key={idx} className="flex items-start gap-1.5 text-xs text-warn">
              <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              <span>{w}</span>
            </div>
          ))}
        </div>
      )}

      {/* Collapsible Context Details (replaces wall of P0-P7 engineering cards) */}
      <div className="rounded-lg border border-border-subtle bg-canvas/50">
        <button
          type="button"
          onClick={() => setSlotsDetailOpen((v) => !v)}
          className="flex w-full items-center justify-between gap-2 px-3 py-2 text-left text-xs"
        >
          <span className="font-medium text-ink-secondary">
            查看发给 AI 的参考资料明细
          </span>
          <div className="flex shrink-0 items-center gap-2">
            {briefing && (
              <span
                title={`约 ${formatNumber(briefing.totalTokenEst)} / ${formatNumber(briefing.totalTokenBudget)} Token`}
                className={`nib-mono text-xs ${
                  tokenRatio >= 90 ? "text-danger" : "text-ink-muted"
                }`}
              >
                容量 {tokenRatio}%
              </span>
            )}
            <ChevronDown
              className={`h-3.5 w-3.5 text-ink-muted transition-transform ${
                slotsDetailOpen ? "rotate-180" : ""
              }`}
            />
          </div>
        </button>

        {slotsDetailOpen && (
          <div className="border-t border-border-subtle p-2.5">
            {briefingLoading && !briefing ? (
              <div className="py-6 text-center text-xs text-ink-muted">
                正在整理参考资料…
              </div>
            ) : (
              <div className="flex flex-col gap-2">
                {briefing && (
                  <div className="mb-1">
                    <div className="flex items-center justify-between text-xs text-ink-muted">
                      <span>参考资料长度</span>
                      <span className="nib-mono">
                        {formatCompactTokens(briefing.totalTokenEst)} / {formatCompactTokens(briefing.totalTokenBudget)}
                      </span>
                    </div>
                    <div className="mt-1.5 h-1.5 w-full overflow-hidden rounded-full bg-elevated">
                      <div
                        className={`h-full rounded-full transition-all ${
                          tokenRatio >= 90 ? "bg-danger" : "bg-accent"
                        }`}
                        style={{ width: `${tokenRatio}%` }}
                      />
                    </div>
                  </div>
                )}

                {(briefing?.slots ?? []).map((slot) => {
                  const expanded = expandedSlots.has(slot.key);
                  return (
                    <div
                      key={slot.key}
                      className="overflow-hidden rounded-lg border border-border-subtle bg-canvas"
                    >
                      <button
                        type="button"
                        onClick={() => onToggleSlotExpand(slot.key)}
                        className="flex w-full items-center justify-between gap-2 px-2.5 py-2 text-left transition-colors hover:bg-elevated/40"
                      >
                        <div className="flex min-w-0 items-center gap-1.5">
                          <span className="truncate text-xs font-medium text-ink-primary">
                            {slot.title}
                          </span>
                          {slot.truncated && (
                            <span className="shrink-0 rounded bg-warn-soft px-1 text-xs text-warn">
                              已精简
                            </span>
                          )}
                        </div>
                        <ChevronDown
                          className={`h-3.5 w-3.5 shrink-0 text-ink-muted transition-transform ${
                            expanded ? "rotate-180" : ""
                          }`}
                        />
                      </button>

                      {expanded && (
                        <div className="border-t border-border-subtle bg-surface/40 px-2.5 py-2">
                          <pre className="max-h-56 overflow-y-auto font-ui text-xs leading-relaxed whitespace-pre-wrap text-ink-secondary">
                            {slot.content || "（暂无内容）"}
                          </pre>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
