import { useCallback, useEffect, useMemo, useState } from "react";
import { AlertTriangle, Clock, Plus, Trash2, X } from "lucide-react";
import {
  MemoryService,
  type Foreshadowing,
  type ForeshadowingStatus,
} from "../../mygo";
import { useApp } from "../../stores/AppContext";
import {
  FORESHADOWING_STATUSES,
  formatErrorMessage,
  formatSeq,
  getEntityKindLabel,
  getForeshadowingStatusMeta,
  isForeshadowingApproaching,
  isForeshadowingOverdue,
} from "../../lib/constants";

const DISPLAY_GROUP_ORDER: ForeshadowingStatus[] = [
  "planted",
  "hinted",
  "pending_plant",
  "resolved",
  "abandoned",
];

interface ForeshadowEditorState {
  id: number;
  title: string;
  description: string;
  status: ForeshadowingStatus;
  plantedChapterSeq: string;
  lastHintSeq: string;
  targetResolveSeq: string;
  resolvedChapterSeq: string;
  involvedEntityIds: number[];
}

export function ForeshadowingBoard({ latestChapterSeq }: { latestChapterSeq: number }) {
  const {
    activeBook,
    entities,
    memoryRevision,
    refreshForeshadowings,
    notify,
  } = useApp();

  const [statusFilter, setStatusFilter] = useState<string>("all");
  const [items, setItems] = useState<Foreshadowing[]>([]);
  const [loading, setLoading] = useState(false);

  const [editor, setEditor] = useState<ForeshadowEditorState | null>(null);
  const [saving, setSaving] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const entityMap = useMemo(() => {
    const map = new Map<number, (typeof entities)[number]>();
    for (const e of entities) map.set(e.id, e);
    return map;
  }, [entities]);

  const fetchItems = useCallback(async () => {
    if (!activeBook) {
      setItems([]);
      return;
    }
    setLoading(true);
    try {
      const list = await MemoryService.listForeshadowings(
        activeBook.id,
        statusFilter === "all" ? "" : statusFilter
      );
      setItems(list);
    } catch (err) {
      notify({ tone: "danger", message: "加载伏笔列表失败", detail: formatErrorMessage(err) });
    } finally {
      setLoading(false);
    }
  }, [activeBook, statusFilter, notify]);

  useEffect(() => {
    void fetchItems();
  }, [fetchItems, memoryRevision]);

  useEffect(() => {
    if (!editor) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setEditor(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [editor]);

  const grouped = useMemo(() => {
    const byStatus = new Map<string, Foreshadowing[]>();
    for (const item of items) {
      const list = byStatus.get(item.status);
      if (list) list.push(item);
      else byStatus.set(item.status, [item]);
    }
    if (statusFilter !== "all") {
      return [
        {
          status: statusFilter,
          items: byStatus.get(statusFilter) ?? [],
        },
      ];
    }
    return DISPLAY_GROUP_ORDER.map((st) => ({
      status: st,
      items: byStatus.get(st) ?? [],
    })).filter((g) => g.items.length > 0);
  }, [items, statusFilter]);

  const openCreate = () => {
    setEditor({
      id: 0,
      title: "",
      description: "",
      status: statusFilter === "all" ? "planted" : statusFilter,
      plantedChapterSeq: latestChapterSeq > 0 ? String(latestChapterSeq) : "1",
      lastHintSeq: "0",
      targetResolveSeq:
        latestChapterSeq > 0 ? String(latestChapterSeq + 20) : "20",
      resolvedChapterSeq: "0",
      involvedEntityIds: [],
    });
    setConfirmDelete(false);
  };

  const openEdit = (fs: Foreshadowing) => {
    setEditor({
      id: fs.id,
      title: fs.title,
      description: fs.description,
      status: fs.status,
      plantedChapterSeq: String(fs.plantedChapterSeq || 0),
      lastHintSeq: String(fs.lastHintSeq || 0),
      targetResolveSeq: String(fs.targetResolveSeq || 0),
      resolvedChapterSeq: String(fs.resolvedChapterSeq || 0),
      involvedEntityIds: Array.isArray(fs.involvedEntityIds)
        ? [...fs.involvedEntityIds]
        : [],
    });
    setConfirmDelete(false);
  };

  const toggleEntity = (eid: number) => {
    if (!editor) return;
    const exists = editor.involvedEntityIds.includes(eid);
    setEditor({
      ...editor,
      involvedEntityIds: exists
        ? editor.involvedEntityIds.filter((id) => id !== eid)
        : [...editor.involvedEntityIds, eid],
    });
  };

  const parseNonNeg = (raw: string): number => {
    const n = Number.parseInt(raw, 10);
    return Number.isFinite(n) && n > 0 ? n : 0;
  };

  const handleSave = async () => {
    if (!activeBook || !editor) return;
    const trimmedTitle = editor.title.trim();
    if (!trimmedTitle) {
      notify({ tone: "warn", message: "伏笔标题不能为空" });
      return;
    }
    setSaving(true);
    try {
      await MemoryService.upsertForeshadowing({
        id: editor.id,
        bookId: activeBook.id,
        title: trimmedTitle,
        description: editor.description.trim(),
        status: editor.status,
        plantedChapterSeq: parseNonNeg(editor.plantedChapterSeq),
        lastHintSeq: parseNonNeg(editor.lastHintSeq),
        targetResolveSeq: parseNonNeg(editor.targetResolveSeq),
        resolvedChapterSeq: parseNonNeg(editor.resolvedChapterSeq),
        involvedEntityIds: editor.involvedEntityIds,
      });
      await Promise.all([fetchItems(), refreshForeshadowings(activeBook.id)]);
      setEditor(null);
      notify({
        tone: "ok",
        message: editor.id > 0 ? "伏笔已更新" : "已新增伏笔",
      });
    } catch (err) {
      notify({ tone: "danger", message: "保存伏笔失败", detail: formatErrorMessage(err) });
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!activeBook || !editor || editor.id <= 0) return;
    setSaving(true);
    try {
      await MemoryService.deleteForeshadowing(editor.id);
      await Promise.all([fetchItems(), refreshForeshadowings(activeBook.id)]);
      setEditor(null);
      notify({ tone: "ok", message: "已删除伏笔" });
    } catch (err) {
      notify({ tone: "danger", message: "删除伏笔失败", detail: formatErrorMessage(err) });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex-1 overflow-y-auto px-6 py-5">
      <div className="mx-auto flex w-full max-w-[1160px] flex-col gap-5">
        {/* Toolbar */}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-1 rounded-lg border border-border-subtle bg-surface p-1">
            <button
              type="button"
              onClick={() => setStatusFilter("all")}
              className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                statusFilter === "all"
                  ? "bg-accent-soft text-accent"
                  : "text-ink-secondary hover:text-ink-primary"
              }`}
            >
              全部
            </button>
            {FORESHADOWING_STATUSES.map((st) => (
              <button
                key={st.value}
                type="button"
                onClick={() => setStatusFilter(st.value)}
                className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                  statusFilter === st.value
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:text-ink-primary"
                }`}
              >
                {st.label}
              </button>
            ))}
          </div>

          <button
            type="button"
            onClick={openCreate}
            className="flex items-center gap-1.5 rounded-md bg-accent px-3 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90"
          >
            <Plus className="h-3.5 w-3.5" />
            <span>新建伏笔</span>
          </button>
        </div>

        {/* Grouped Board */}
        {loading && items.length === 0 ? (
          <div className="py-16 text-center text-xs text-ink-muted">
            正在加载伏笔看板…
          </div>
        ) : grouped.length === 0 || grouped.every((g) => g.items.length === 0) ? (
          <div className="flex flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-border-strong bg-surface/35 py-16 text-center">
            <div className="text-xs text-ink-secondary">
              当前状态下暂无伏笔记录
            </div>
            <button
              type="button"
              onClick={openCreate}
              className="rounded-md border border-accent/40 bg-accent-soft px-3.5 py-1.5 text-xs font-medium text-accent hover:border-accent"
            >
              埋设第一条伏笔
            </button>
          </div>
        ) : (
          <div className="flex flex-col gap-6 pb-8">
            {grouped.map((group) => {
              const stMeta = getForeshadowingStatusMeta(group.status);
              return (
                <section key={group.status} className="flex flex-col gap-3">
                  <div className="flex items-center gap-2">
                    <span className={`h-2 w-2 rounded-full ${stMeta.dotClass}`} />
                    <h3 className="text-xs font-semibold tracking-wide text-ink-primary">
                      {stMeta.label}
                    </h3>
                    <span className="nib-mono text-[11px] text-ink-muted">
                      {group.items.length}
                    </span>
                  </div>

                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
                    {group.items.map((fs) => {
                      const overdue = isForeshadowingOverdue(fs, latestChapterSeq);
                      const approaching =
                        !overdue && isForeshadowingApproaching(fs, latestChapterSeq);
                      return (
                        <button
                          key={fs.id}
                          type="button"
                          onClick={() => openEdit(fs)}
                          className={`flex flex-col rounded-xl border p-4 text-left transition-colors ${
                            overdue
                              ? "border-danger/45 bg-danger-soft/25 hover:border-danger/70"
                              : approaching
                              ? "border-warn/40 bg-warn-soft/20 hover:border-warn/65"
                              : "border-border-subtle bg-surface hover:border-border-strong hover:bg-elevated/60"
                          }`}
                        >
                          <div className="flex items-start justify-between gap-2">
                            <span className="text-[14px] font-medium text-ink-primary">
                              {fs.title}
                            </span>
                            {overdue ? (
                              <span className="inline-flex shrink-0 items-center gap-1 rounded border border-danger/40 bg-danger-soft px-1.5 py-0.5 text-[10px] font-semibold text-danger">
                                <AlertTriangle className="h-2.5 w-2.5" />
                                超期 {latestChapterSeq - fs.targetResolveSeq} 章
                              </span>
                            ) : approaching ? (
                              <span className="inline-flex shrink-0 items-center gap-1 rounded border border-warn/40 bg-warn-soft px-1.5 py-0.5 text-[10px] font-medium text-warn">
                                <Clock className="h-2.5 w-2.5" />
                                临期
                              </span>
                            ) : (
                              <span
                                className={`shrink-0 rounded border px-1.5 py-0.5 text-[10px] ${stMeta.badgeClass}`}
                              >
                                {stMeta.label}
                              </span>
                            )}
                          </div>

                          {fs.description && (
                            <p className="mt-2 line-clamp-2 text-xs leading-relaxed text-ink-secondary">
                              {fs.description}
                            </p>
                          )}

                          {/* Milestones */}
                          <div className="nib-mono mt-3 flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-border-subtle pt-2.5 text-[11px] text-ink-muted">
                            {fs.plantedChapterSeq > 0 && (
                              <span>埋设 {formatSeq(fs.plantedChapterSeq)}</span>
                            )}
                            {fs.lastHintSeq > 0 && (
                              <span>暗示 {formatSeq(fs.lastHintSeq)}</span>
                            )}
                            {fs.targetResolveSeq > 0 && (
                              <span className={overdue ? "text-danger font-medium" : ""}>
                                计划回收 {formatSeq(fs.targetResolveSeq)}
                              </span>
                            )}
                            {fs.resolvedChapterSeq > 0 && (
                              <span className="text-ok">
                                已收 {formatSeq(fs.resolvedChapterSeq)}
                              </span>
                            )}
                          </div>

                          {fs.involvedEntityIds && fs.involvedEntityIds.length > 0 && (
                            <div className="mt-2 flex flex-wrap gap-1">
                              {fs.involvedEntityIds.map((eid) => {
                                const ent = entityMap.get(eid);
                                if (!ent) return null;
                                return (
                                  <span
                                    key={eid}
                                    className="rounded bg-canvas/80 px-1.5 py-0.5 text-[10.5px] text-ink-secondary"
                                  >
                                    {ent.name}
                                  </span>
                                );
                              })}
                            </div>
                          )}
                        </button>
                      );
                    })}
                  </div>
                </section>
              );
            })}
          </div>
        )}
      </div>

      {/* Slide-over Editor */}
      {editor && (
        <div
          className="fixed inset-0 z-40 flex justify-end bg-black/50 backdrop-blur-[1px]"
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) setEditor(null);
          }}
        >
          <div className="flex h-full w-full max-w-[440px] flex-col border-l border-border-strong bg-elevated shadow-[var(--shadow-elevated)]">
            <div className="flex items-center justify-between border-b border-border-subtle px-5 py-3.5">
              <h2 className="text-sm font-medium text-ink-primary">
                {editor.id > 0 ? "编辑伏笔" : "新建伏笔"}
              </h2>
              <button
                type="button"
                onClick={() => setEditor(null)}
                aria-label="关闭"
                className="rounded p-1 text-ink-muted hover:bg-surface hover:text-ink-primary"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
              <div>
                <label className="mb-1 block text-xs text-ink-secondary">伏笔标题</label>
                <input
                  type="text"
                  autoFocus
                  value={editor.title}
                  onChange={(e) => setEditor({ ...editor, title: e.target.value })}
                  placeholder="例如：血玉令牌背后的宗门密库"
                  className="w-full rounded-md border border-border-subtle bg-canvas px-3 py-2 text-sm text-ink-primary focus:border-accent focus:outline-none"
                />
              </div>

              <div>
                <label className="mb-1 block text-xs text-ink-secondary">状态</label>
                <select
                  value={editor.status}
                  onChange={(e) => setEditor({ ...editor, status: e.target.value })}
                  className="w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-2 text-xs text-ink-primary focus:border-accent focus:outline-none"
                >
                  {FORESHADOWING_STATUSES.map((st) => (
                    <option key={st.value} value={st.value}>
                      {st.label}
                    </option>
                  ))}
                </select>
              </div>

              <div>
                <label className="mb-1 block text-xs text-ink-secondary">伏笔详情</label>
                <textarea
                  rows={3}
                  value={editor.description}
                  onChange={(e) =>
                    setEditor({ ...editor, description: e.target.value })
                  }
                  placeholder="线索内容、关联真相及预期回收爆点"
                  className="w-full resize-y rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                />
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="mb-1 block text-[11px] text-ink-secondary">
                    埋设章号
                  </label>
                  <input
                    type="number"
                    min={0}
                    value={editor.plantedChapterSeq}
                    onChange={(e) =>
                      setEditor({ ...editor, plantedChapterSeq: e.target.value })
                    }
                    className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                  />
                </div>
                <div>
                  <label className="mb-1 block text-[11px] text-ink-secondary">
                    最近暗示章号
                  </label>
                  <input
                    type="number"
                    min={0}
                    value={editor.lastHintSeq}
                    onChange={(e) =>
                      setEditor({ ...editor, lastHintSeq: e.target.value })
                    }
                    className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                  />
                </div>
                <div>
                  <label className="mb-1 block text-[11px] text-ink-secondary">
                    计划回收章号
                  </label>
                  <input
                    type="number"
                    min={0}
                    value={editor.targetResolveSeq}
                    onChange={(e) =>
                      setEditor({ ...editor, targetResolveSeq: e.target.value })
                    }
                    className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                  />
                </div>
                <div>
                  <label className="mb-1 block text-[11px] text-ink-secondary">
                    实际回收章号
                  </label>
                  <input
                    type="number"
                    min={0}
                    value={editor.resolvedChapterSeq}
                    onChange={(e) =>
                      setEditor({ ...editor, resolvedChapterSeq: e.target.value })
                    }
                    className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                  />
                </div>
              </div>

              {/* Involved Entities */}
              <div>
                <label className="mb-1.5 block text-xs text-ink-secondary">
                  关联设定实体
                </label>
                {entities.length === 0 ? (
                  <div className="text-[11px] text-ink-muted">账本暂无实体</div>
                ) : (
                  <div className="flex max-h-40 flex-wrap gap-1.5 overflow-y-auto rounded-md border border-border-subtle bg-canvas p-2">
                    {entities.map((ent) => {
                      const active = editor.involvedEntityIds.includes(ent.id);
                      return (
                        <button
                          key={ent.id}
                          type="button"
                          onClick={() => toggleEntity(ent.id)}
                          className={`inline-flex items-center gap-1 rounded-full border px-2.5 py-0.5 text-[11px] transition-colors ${
                            active
                              ? "border-accent bg-accent-soft text-accent"
                              : "border-border-subtle bg-surface text-ink-secondary hover:text-ink-primary"
                          }`}
                        >
                          <span>{ent.name}</span>
                          <span className="text-[10px] opacity-65">
                            {getEntityKindLabel(ent.kind)}
                          </span>
                        </button>
                      );
                    })}
                  </div>
                )}
              </div>
            </div>

            <div className="flex items-center justify-between border-t border-border-subtle bg-surface/60 px-5 py-3">
              {editor.id > 0 ? (
                confirmDelete ? (
                  <div className="flex items-center gap-1.5">
                    <button
                      type="button"
                      disabled={saving}
                      onClick={() => void handleDelete()}
                      className="rounded-md bg-danger px-2.5 py-1 text-xs font-medium text-white"
                    >
                      确认删除
                    </button>
                    <button
                      type="button"
                      onClick={() => setConfirmDelete(false)}
                      className="text-xs text-ink-secondary hover:text-ink-primary"
                    >
                      取消
                    </button>
                  </div>
                ) : (
                  <button
                    type="button"
                    onClick={() => setConfirmDelete(true)}
                    className="flex items-center gap-1 rounded-md px-2 py-1 text-xs text-ink-muted transition-colors hover:bg-danger-soft hover:text-danger"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                    <span>删除</span>
                  </button>
                )
              ) : (
                <div />
              )}

              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => setEditor(null)}
                  className="rounded-md px-3 py-1.5 text-xs text-ink-secondary hover:bg-surface hover:text-ink-primary"
                >
                  取消
                </button>
                <button
                  type="button"
                  disabled={saving}
                  onClick={() => void handleSave()}
                  className="rounded-md bg-accent px-4 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
                >
                  {saving ? "保存中…" : "保存"}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
