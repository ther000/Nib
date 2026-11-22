import { useEffect, useMemo, useState } from "react";
import {
  AlertTriangle,
  ArrowRight,
  Check,
  Loader2,
  Merge,
  Replace,
  Search,
} from "lucide-react";
import {
  AtlasService,
  type BookRenamePreview,
  type Entity,
  type EntityMergePreview,
  type TextImpactHit,
} from "../../mygo";
import { ModalShell } from "../ui/ModalShell";
import { formatErrorMessage, getEntityKindLabel } from "../../lib/constants";

type SafetyToolTab = "merge" | "rename";

interface EntitySafetyToolsModalProps {
  open: boolean;
  bookId: number;
  entities: Entity[];
  initialTab?: SafetyToolTab;
  onClose: () => void;
  onCompleted: () => Promise<void>;
  onNotify: (input: {
    tone: "info" | "ok" | "warn" | "danger";
    message: string;
    detail?: string;
  }) => void;
}

const SCOPE_LABELS: Record<string, string> = {
  chapter_content: "正文",
  chapter_meta: "章纲元信息",
  beat: "节拍卡",
  volume: "卷目标",
  arc_summary: "阶段摘要",
  entity: "设定档案",
  relation: "关系网",
  foreshadowing: "伏笔",
  state_event: "状态事件",
};

function HitReviewList({
  hits,
  excludedIds,
  onChangeExcluded,
}: {
  hits: TextImpactHit[];
  excludedIds: Set<string>;
  onChangeExcluded: (next: Set<string>) => void;
}) {
  const warningCount = useMemo(
    () => hits.filter((h) => Boolean(h.riskWarning)).length,
    [hits]
  );
  const includedCount = hits.length - excludedIds.size;

  const toggleHit = (id: string) => {
    const next = new Set(excludedIds);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    onChangeExcluded(next);
  };

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center justify-between gap-2 text-xs">
        <div className="flex items-center gap-2 text-ink-secondary">
          <span>
            命中 {hits.length} 处模块记录（已选{" "}
            <strong className="text-ink-primary">{includedCount}</strong> 处）
          </span>
          {warningCount > 0 && (
            <span className="inline-flex items-center gap-1 rounded bg-warn-soft px-1.5 py-0.5 text-[11px] font-medium text-warn">
              <AlertTriangle className="h-3 w-3" />
              {warningCount} 处可能误伤复合词
            </span>
          )}
        </div>

        <div className="flex items-center gap-1.5">
          <button
            type="button"
            onClick={() => onChangeExcluded(new Set())}
            className="rounded border border-border-subtle bg-surface px-2 py-0.5 text-[11px] text-ink-secondary hover:text-ink-primary"
          >
            全选
          </button>
          <button
            type="button"
            onClick={() => onChangeExcluded(new Set(hits.map((h) => h.id)))}
            className="rounded border border-border-subtle bg-surface px-2 py-0.5 text-[11px] text-ink-secondary hover:text-ink-primary"
          >
            全不选
          </button>
          {warningCount > 0 && (
            <button
              type="button"
              onClick={() => {
                const next = new Set(excludedIds);
                for (const h of hits) {
                  if (h.riskWarning) next.add(h.id);
                }
                onChangeExcluded(next);
              }}
              className="rounded border border-warn/40 bg-warn-soft/50 px-2 py-0.5 text-[11px] text-warn hover:border-warn"
            >
              跳过风险项
            </button>
          )}
        </div>
      </div>

      <div className="max-h-[260px] space-y-1.5 overflow-y-auto pr-1">
        {hits.map((hit) => {
          const checked = !excludedIds.has(hit.id);
          return (
            <label
              key={hit.id}
              className={`flex cursor-pointer items-start gap-2.5 rounded-lg border p-2.5 text-xs transition-opacity ${
                hit.riskWarning
                  ? "border-warn/45 bg-warn-soft/20"
                  : "border-border-subtle bg-canvas/70"
              } ${checked ? "" : "opacity-55"}`}
            >
              <input
                type="checkbox"
                checked={checked}
                onChange={() => toggleHit(hit.id)}
                className="mt-0.5 h-3.5 w-3.5 shrink-0 accent-[var(--accent)]"
              />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-1.5">
                  <span className="rounded bg-surface px-1.5 py-0.5 text-[10.5px] text-ink-secondary">
                    {SCOPE_LABELS[hit.scope] ?? hit.scope}
                  </span>
                  <span className="font-medium text-ink-primary">{hit.label}</span>
                  <span className="nib-mono text-[10.5px] text-ink-muted">
                    {hit.matchCount} 处
                  </span>
                  {hit.riskWarning && (
                    <span className="inline-flex items-center gap-1 rounded bg-warn-soft px-1.5 py-0.5 text-[10.5px] font-medium text-warn">
                      <AlertTriangle className="h-2.5 w-2.5" />
                      {hit.riskWarning}
                    </span>
                  )}
                </div>
                {hit.snippet && (
                  <p className="mt-1 line-clamp-2 text-[11px] leading-relaxed text-ink-secondary">
                    {hit.snippet}
                  </p>
                )}
              </div>
            </label>
          );
        })}
      </div>
    </div>
  );
}

export function EntitySafetyToolsModal({
  open,
  bookId,
  entities,
  initialTab = "merge",
  onClose,
  onCompleted,
  onNotify,
}: EntitySafetyToolsModalProps) {
  const [tab, setTab] = useState<SafetyToolTab>(initialTab);

  // Merge state
  const [sourceId, setSourceId] = useState<string>("");
  const [targetId, setTargetId] = useState<string>("");
  const [replaceInProse, setReplaceInProse] = useState(true);
  const [mergePreview, setMergePreview] = useState<EntityMergePreview | null>(null);
  const [mergeExcluded, setMergeExcluded] = useState<Set<string>>(new Set());

  // Rename state
  const [oldName, setOldName] = useState("");
  const [newName, setNewName] = useState("");
  const [includeOutlines, setIncludeOutlines] = useState(true);
  const [includeMemory, setIncludeMemory] = useState(true);
  const [renamePreview, setRenamePreview] = useState<BookRenamePreview | null>(null);
  const [renameExcluded, setRenameExcluded] = useState<Set<string>>(new Set());

  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!open) return;
    setTab(initialTab);
    setSourceId("");
    setTargetId("");
    setOldName("");
    setNewName("");
    setMergePreview(null);
    setMergeExcluded(new Set());
    setRenamePreview(null);
    setRenameExcluded(new Set());
  }, [open, initialTab]);

  const handlePreviewMerge = async () => {
    const src = Number.parseInt(sourceId, 10);
    const dst = Number.parseInt(targetId, 10);
    if (!src || !dst) {
      onNotify({ tone: "warn", message: "请选择源实体和目标实体" });
      return;
    }
    if (src === dst) {
      onNotify({ tone: "warn", message: "不能将实体合并到自身" });
      return;
    }
    setBusy(true);
    try {
      const prev = await AtlasService.previewEntityMerge({
        bookId,
        sourceEntityId: src,
        targetEntityId: dst,
        replaceInProse,
      });
      setMergePreview(prev);
      setMergeExcluded(new Set());
    } catch (err) {
      onNotify({ tone: "danger", message: "预览实体合并失败", detail: formatErrorMessage(err) });
    } finally {
      setBusy(false);
    }
  };

  const handleExecuteMerge = async () => {
    if (!mergePreview) return;
    const src = Number.parseInt(sourceId, 10);
    const dst = Number.parseInt(targetId, 10);
    setBusy(true);
    try {
      const res = await AtlasService.executeEntityMerge({
        bookId,
        sourceEntityId: src,
        targetEntityId: dst,
        replaceInProse,
        excludedHitIds: Array.from(mergeExcluded),
      });
      await onCompleted();
      onNotify({
        tone: "ok",
        message: `已合并「${mergePreview.sourceName}」→「${mergePreview.targetName}」`,
        detail: `迁移 ${res.mergedEvents} 条状态事件 · ${res.mergedRelations} 条关系 · 更新 ${res.updatedChapters} 章正文（已存安全撤销点）`,
      });
      onClose();
    } catch (err) {
      onNotify({ tone: "danger", message: "执行实体合并失败", detail: formatErrorMessage(err) });
    } finally {
      setBusy(false);
    }
  };

  const handlePreviewRename = async () => {
    if (!oldName.trim()) {
      onNotify({ tone: "warn", message: "请输入要替换的原名称" });
      return;
    }
    if (!newName.trim()) {
      onNotify({ tone: "warn", message: "请输入新名称" });
      return;
    }
    if (oldName.trim() === newName.trim()) {
      onNotify({ tone: "warn", message: "新名称与原名称相同" });
      return;
    }
    setBusy(true);
    try {
      const prev = await AtlasService.previewBookRename({
        bookId,
        oldName: oldName.trim(),
        newName: newName.trim(),
        includeOutlines,
        includeMemory,
      });
      setRenamePreview(prev);
      setRenameExcluded(new Set());
    } catch (err) {
      onNotify({ tone: "danger", message: "扫描全书命中失败", detail: formatErrorMessage(err) });
    } finally {
      setBusy(false);
    }
  };

  const handleExecuteRename = async () => {
    if (!renamePreview) return;
    const allExcluded = renamePreview.hits.every((h) => renameExcluded.has(h.id));
    if (renamePreview.hits.length === 0 || allExcluded) {
      onNotify({ tone: "warn", message: "请至少保留一条要替换的命中记录" });
      return;
    }
    setBusy(true);
    try {
      const res = await AtlasService.executeBookRename({
        bookId,
        oldName: renamePreview.oldName,
        newName: renamePreview.newName,
        includeOutlines,
        includeMemory,
        excludedHitIds: Array.from(renameExcluded),
      });
      await onCompleted();
      onNotify({
        tone: "ok",
        message: `已将「${renamePreview.oldName}」安全改名为「${renamePreview.newName}」`,
        detail: `替换 ${res.totalReplaced} 处 · 更新 ${res.updatedChapters} 章正文与 ${res.updatedRecords} 条档案记录（已生成章节快照与撤销点）`,
      });
      onClose();
    } catch (err) {
      onNotify({ tone: "danger", message: "执行全书改名失败", detail: formatErrorMessage(err) });
    } finally {
      setBusy(false);
    }
  };

  return (
    <ModalShell
      open={open}
      onClose={onClose}
      closeDisabled={busy}
      title="设定合并与全书改名"
      icon={tab === "merge" ? <Merge className="h-4 w-4 text-accent" /> : <Replace className="h-4 w-4 text-accent" />}
      maxWidthClass="max-w-[740px]"
    >
      <div className="flex flex-col gap-4 overflow-y-auto px-5 py-4">
        {/* Mode Switch */}
        <div className="flex items-center gap-1 rounded-lg border border-border-subtle bg-surface p-1 self-start">
          <button
            type="button"
            disabled={busy}
            onClick={() => setTab("merge")}
            className={`flex items-center gap-1.5 rounded-md px-3 py-1 text-xs font-medium transition-colors ${
              tab === "merge"
                ? "bg-accent-soft text-accent"
                : "text-ink-secondary hover:text-ink-primary"
            }`}
          >
            <Merge className="h-3.5 w-3.5" />
            <span>合并重复档案</span>
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={() => setTab("rename")}
            className={`flex items-center gap-1.5 rounded-md px-3 py-1 text-xs font-medium transition-colors ${
              tab === "rename"
                ? "bg-accent-soft text-accent"
                : "text-ink-secondary hover:text-ink-primary"
            }`}
          >
            <Replace className="h-3.5 w-3.5" />
            <span>全书安全改名</span>
          </button>
        </div>

        {tab === "merge" ? (
          <>
            <div className="rounded-lg border border-border-subtle bg-surface/60 px-3 py-2 text-xs leading-relaxed text-ink-secondary">
              将重复档案的所有历史记录、关系、伏笔与节拍合并到保留档案中，并自动把原名称存为别名；替换正文前自动备份历史版本。
            </div>

            <div className="grid grid-cols-1 items-center gap-2.5 sm:grid-cols-[1fr_auto_1fr]">
              <div>
                <label className="mb-1 block text-xs text-ink-secondary">
                  将被合并移除的档案
                </label>
                <select
                  value={sourceId}
                  onChange={(e) => {
                    setSourceId(e.target.value);
                    setMergePreview(null);
                  }}
                  className="w-full rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-xs text-ink-primary focus:border-accent focus:outline-none"
                >
                  <option value="">选择要移除的档案…</option>
                  {entities.map((e) => (
                    <option key={e.id} value={e.id}>
                      {e.name}（{getEntityKindLabel(e.kind)}）
                    </option>
                  ))}
                </select>
              </div>

              <ArrowRight className="mx-auto hidden h-4 w-4 text-ink-muted sm:mt-4 sm:block" />

              <div>
                <label className="mb-1 block text-xs text-ink-secondary">
                  保留并合入的目标档案
                </label>
                <select
                  value={targetId}
                  onChange={(e) => {
                    setTargetId(e.target.value);
                    setMergePreview(null);
                  }}
                  className="w-full rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-xs text-ink-primary focus:border-accent focus:outline-none"
                >
                  <option value="">选择要保留的档案…</option>
                  {entities
                    .filter((e) => String(e.id) !== sourceId)
                    .map((e) => (
                      <option key={e.id} value={e.id}>
                        {e.name}（{getEntityKindLabel(e.kind)}）
                      </option>
                    ))}
                </select>
              </div>
            </div>

            <div className="flex flex-wrap items-center justify-between gap-2">
              <label className="flex cursor-pointer items-center gap-2 text-xs text-ink-secondary select-none">
                <input
                  type="checkbox"
                  checked={replaceInProse}
                  onChange={(e) => {
                    setReplaceInProse(e.target.checked);
                    setMergePreview(null);
                  }}
                  className="h-3.5 w-3.5 accent-[var(--accent)]"
                />
                <span>同步替换正文中出现的源实体名称</span>
              </label>

              <button
                type="button"
                disabled={busy || !sourceId || !targetId || sourceId === targetId}
                onClick={() => void handlePreviewMerge()}
                className="flex items-center gap-1.5 rounded-lg border border-accent/40 bg-accent-soft px-3 py-1.5 text-xs font-medium text-accent transition-colors hover:border-accent disabled:opacity-40"
              >
                {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Search className="h-3.5 w-3.5" />}
                <span>预览影响范围</span>
              </button>
            </div>

            {mergePreview && (
              <div className="flex flex-col gap-3 rounded-xl border border-border-subtle bg-surface/50 p-3.5">
                <div className="flex flex-wrap items-center gap-2 text-xs">
                  <span className="font-semibold text-ink-primary">
                    {mergePreview.sourceName} → {mergePreview.targetName}
                  </span>
                  <span className="nib-mono rounded bg-canvas px-2 py-0.5 text-[11px] text-ink-secondary">
                    状态事件 {mergePreview.stateEventCount}
                  </span>
                  <span className="nib-mono rounded bg-canvas px-2 py-0.5 text-[11px] text-ink-secondary">
                    关系 {mergePreview.relationCount}
                  </span>
                  <span className="nib-mono rounded bg-canvas px-2 py-0.5 text-[11px] text-ink-secondary">
                    伏笔 {mergePreview.foreshadowCount}
                  </span>
                  <span className="nib-mono rounded bg-canvas px-2 py-0.5 text-[11px] text-ink-secondary">
                    节拍 {mergePreview.beatCount}
                  </span>
                </div>

                {mergePreview.textHits.length > 0 ? (
                  <HitReviewList
                    hits={mergePreview.textHits}
                    excludedIds={mergeExcluded}
                    onChangeExcluded={setMergeExcluded}
                  />
                ) : (
                  <div className="text-[11.5px] text-ink-muted">
                    {replaceInProse
                      ? "正文中未检测到需替换的源实体名称，仅执行账本引用合并。"
                      : "已跳过正文名称替换，仅迁移账本引用并保留别名。"}
                  </div>
                )}

                <div className="flex justify-end gap-2 border-t border-border-subtle pt-3">
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => void handleExecuteMerge()}
                    className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
                  >
                    {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Check className="h-3.5 w-3.5" />}
                    <span>确认合并实体</span>
                  </button>
                </div>
              </div>
            )}
          </>
        ) : (
          <>
            <div className="rounded-lg border border-border-subtle bg-surface/60 px-3 py-2 text-xs leading-relaxed text-ink-secondary">
              分模块扫描正文、大纲、节拍与设定库中的全部命中，自动标出可能误伤的复合词（如「林渊」可能误伤「林渊阁」），支持逐条跳过；执行前自动备份章节快照。
            </div>

            <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-[1fr_1fr]">
              <div>
                <div className="mb-1 flex items-center justify-between">
                  <label className="text-[11px] text-ink-secondary">原名称</label>
                  <select
                    value=""
                    onChange={(e) => {
                      if (!e.target.value) return;
                      setOldName(e.target.value);
                      setRenamePreview(null);
                    }}
                    className="rounded border border-border-subtle bg-canvas px-1.5 py-0.5 text-[10.5px] text-ink-secondary focus:outline-none"
                  >
                    <option value="">从实体快选…</option>
                    {entities.map((e) => (
                      <option key={e.id} value={e.name}>
                        {e.name}
                      </option>
                    ))}
                  </select>
                </div>
                <input
                  type="text"
                  value={oldName}
                  onChange={(e) => {
                    setOldName(e.target.value);
                    setRenamePreview(null);
                  }}
                  placeholder="例如：玄天宗"
                  className="w-full rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                />
              </div>

              <div>
                <label className="mb-1 block text-[11px] text-ink-secondary">
                  替换为新名称
                </label>
                <input
                  type="text"
                  value={newName}
                  onChange={(e) => {
                    setNewName(e.target.value);
                    setRenamePreview(null);
                  }}
                  placeholder="例如：青云剑宗"
                  className="w-full rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                />
              </div>
            </div>

            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="flex flex-wrap items-center gap-4 text-xs text-ink-secondary">
                <label className="flex cursor-pointer items-center gap-1.5 select-none">
                  <input
                    type="checkbox"
                    checked={includeOutlines}
                    onChange={(e) => {
                      setIncludeOutlines(e.target.checked);
                      setRenamePreview(null);
                    }}
                    className="h-3.5 w-3.5 accent-[var(--accent)]"
                  />
                  <span>含卷标/章纲/节拍/摘要</span>
                </label>
                <label className="flex cursor-pointer items-center gap-1.5 select-none">
                  <input
                    type="checkbox"
                    checked={includeMemory}
                    onChange={(e) => {
                      setIncludeMemory(e.target.checked);
                      setRenamePreview(null);
                    }}
                    className="h-3.5 w-3.5 accent-[var(--accent)]"
                  />
                  <span>含设定实体/关系/伏笔</span>
                </label>
              </div>

              <button
                type="button"
                disabled={busy || !oldName.trim() || !newName.trim()}
                onClick={() => void handlePreviewRename()}
                className="flex items-center gap-1.5 rounded-lg border border-accent/40 bg-accent-soft px-3 py-1.5 text-xs font-medium text-accent transition-colors hover:border-accent disabled:opacity-40"
              >
                {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Search className="h-3.5 w-3.5" />}
                <span>扫描全书命中</span>
              </button>
            </div>

            {renamePreview && (
              <div className="flex flex-col gap-3 rounded-xl border border-border-subtle bg-surface/50 p-3.5">
                {renamePreview.hits.length === 0 ? (
                  <div className="py-4 text-center text-xs text-ink-muted">
                    全书范围内未扫描到「{renamePreview.oldName}」的命中记录。
                  </div>
                ) : (
                  <>
                    <HitReviewList
                      hits={renamePreview.hits}
                      excludedIds={renameExcluded}
                      onChangeExcluded={setRenameExcluded}
                    />
                    <div className="flex items-center justify-between border-t border-border-subtle pt-3 text-xs">
                      <span className="nib-mono text-ink-muted">
                        总匹配 {renamePreview.totalMatches} 次
                      </span>
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => void handleExecuteRename()}
                        className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
                      >
                        {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Check className="h-3.5 w-3.5" />}
                        <span>确认执行改名</span>
                      </button>
                    </div>
                  </>
                )}
              </div>
            )}
          </>
        )}
      </div>
    </ModalShell>
  );
}
