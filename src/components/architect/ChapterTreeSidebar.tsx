import { useEffect, useState } from "react";
import {
  ArrowDown,
  ArrowRight,
  ArrowUp,
  Check,
  ChevronDown,
  ChevronRight,
  Copy,
  Download,
  Layers,
  ListChecks,
  PenLine,
  Pencil,
  Plus,
  Search,
  Sparkles,
  Trash2,
  X,
} from "lucide-react";
import type { Chapter, Volume } from "../../mygo";
import { useContextMenu } from "../ContextMenu";
import {
  formatSeq,
  getChapterStatusMeta,
  isIMEComposing,
} from "../../lib/constants";
import type { NotifyInput } from "../../stores/ToastContext";

interface ChapterTreeSidebarProps {
  volumes: Volume[];
  chapters: Chapter[];
  chaptersByVolume: Map<number, Chapter[]>;
  activeVolume: Volume | null;
  activeChapterId: number | null;
  movingChapterId: number | null;
  batchMode: boolean;
  selectedChapterIds: Set<number>;
  batchBusy: boolean;
  creatingChapter: boolean;
  insertingChapter: boolean;
  onToggleBatchMode: () => void;
  onToggleChapterSelection: (chapId: number) => void;
  onSelectAllChapters: () => void;
  onClearSelectedChapters: () => void;
  onBatchFinalize: () => void;
  onBatchExport: () => void;
  onBatchDelete: () => void;
  onOpenOutlineModal: () => void;
  onOpenCreateVolume: () => void;
  onOpenEditVolume: (vol: Volume) => void;
  onDeleteVolume: (volId: number) => void;
  onSelectChapter: (chapId: number) => void;
  onJumpToChapter: (chapId: number) => void;
  onCreateChapter: (volumeId: number, title: string) => Promise<boolean>;
  onInsertChapterAfter: (volId: number, afterChapId: number, title: string) => Promise<boolean>;
  onMoveChapter: (chapId: number, direction: "up" | "down") => void;
  onMoveChapterToVolume: (chapId: number, targetVolumeId: number, targetVolSeq?: number) => void;
  onDeleteChapter: (chapId: number) => void;
  notify: (input: NotifyInput) => void;
}

export function ChapterTreeSidebar({
  volumes,
  chapters,
  chaptersByVolume,
  activeVolume,
  activeChapterId,
  movingChapterId,
  batchMode,
  selectedChapterIds,
  batchBusy,
  creatingChapter,
  insertingChapter,
  onToggleBatchMode,
  onToggleChapterSelection,
  onSelectAllChapters,
  onClearSelectedChapters,
  onBatchFinalize,
  onBatchExport,
  onBatchDelete,
  onOpenOutlineModal,
  onOpenCreateVolume,
  onOpenEditVolume,
  onDeleteVolume,
  onSelectChapter,
  onJumpToChapter,
  onCreateChapter,
  onInsertChapterAfter,
  onMoveChapter,
  onMoveChapterToVolume,
  onDeleteChapter,
  notify,
}: ChapterTreeSidebarProps) {
  const { openContextMenu } = useContextMenu();

  const [collapsedVolIds, setCollapsedVolIds] = useState<Set<number>>(new Set());
  const [searchQuery, setSearchQuery] = useState("");
  const [addingChapterVolId, setAddingChapterVolId] = useState<number | null>(null);
  const [newChapterTitle, setNewChapterTitle] = useState("");
  const [insertingAfterChap, setInsertingAfterChap] = useState<{
    volId: number;
    afterChapId: number;
  } | null>(null);
  const [insertChapterTitle, setInsertChapterTitle] = useState("");
  const [draggingChapterId, setDraggingChapterId] = useState<number | null>(null);
  const [dragOverVolumeId, setDragOverVolumeId] = useState<number | null>(null);
  const [dragOverChapterId, setDragOverChapterId] = useState<number | null>(null);

  const toggleVolumeCollapse = (volId: number) => {
    setCollapsedVolIds((prev) => {
      const next = new Set(prev);
      if (next.has(volId)) next.delete(volId);
      else next.add(volId);
      return next;
    });
  };

  const handleSubmitCreateChapter = async (volId: number) => {
    if (!newChapterTitle.trim()) {
      setAddingChapterVolId(null);
      return;
    }
    const ok = await onCreateChapter(volId, newChapterTitle);
    if (ok) {
      setNewChapterTitle("");
      setAddingChapterVolId(null);
    }
  };

  const handleSubmitInsertAfter = async (volId: number, afterChapId: number) => {
    const ok = await onInsertChapterAfter(volId, afterChapId, insertChapterTitle);
    if (ok) {
      setInsertingAfterChap(null);
      setInsertChapterTitle("");
    }
  };

  const handleDropOnTarget = (
    targetVolumeId: number,
    targetVolSeq: number,
    targetChapId?: number
  ) => {
    const chapId = draggingChapterId;
    setDraggingChapterId(null);
    setDragOverVolumeId(null);
    setDragOverChapterId(null);
    if (!chapId) return;
    if (targetChapId !== undefined && chapId === targetChapId) return;
    onMoveChapterToVolume(chapId, targetVolumeId, targetVolSeq);
  };

  return (
    <aside
      className="flex shrink-0 flex-col bg-transparent px-1 pb-2"
      style={{ width: "var(--sidebar-tree-width)" }}
    >
      <div className="flex items-center justify-between px-2.5 py-2">
        <div className="flex items-center gap-1.5 text-xs font-medium text-ink-secondary">
          <Layers className="h-3.5 w-3.5 text-ink-muted" />
          <span>目录</span>
          <span className="nib-mono text-xs text-ink-muted">{chapters.length}</span>
        </div>
        <div className="flex items-center gap-0.5">
          {volumes.length > 0 && (
            <button
              type="button"
              onClick={onOpenOutlineModal}
              title="AI 批量生成章纲与节拍"
              className="flex items-center gap-1 rounded-md px-1.5 py-1 text-xs font-medium text-accent transition-colors hover:bg-accent-soft"
            >
              <Sparkles className="h-3.5 w-3.5" />
              <span>AI 章纲</span>
            </button>
          )}
          <button
            type="button"
            onClick={onToggleBatchMode}
            title="批量管理章节"
            aria-label="批量管理章节"
            className={`rounded-md p-1.5 transition-colors ${
              batchMode
                ? "bg-accent-soft text-accent"
                : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
            }`}
          >
            <ListChecks className="h-3.5 w-3.5" />
          </button>
          <button
            type="button"
            onClick={onOpenCreateVolume}
            title="新建分卷"
            aria-label="新建分卷"
            className="rounded-md p-1.5 text-ink-secondary transition-colors hover:bg-elevated hover:text-accent"
          >
            <Plus className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      {chapters.length > 0 && (
        <div className="border-b border-border-subtle px-2.5 py-2">
          <div className="relative">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2 text-ink-muted" />
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="筛选章号或标题…"
              className="w-full rounded-lg border border-border-subtle bg-canvas py-1 pr-7 pl-8 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent"
            />
            {searchQuery && (
              <button
                type="button"
                onClick={() => setSearchQuery("")}
                aria-label="清空筛选"
                className="absolute top-1/2 right-2 -translate-y-1/2 rounded p-0.5 text-ink-muted hover:text-ink-primary"
              >
                <X className="h-3 w-3" />
              </button>
            )}
          </div>
        </div>
      )}

      {batchMode && (
        <div className="flex flex-col gap-1.5 border-b border-border-subtle bg-accent-soft/25 px-2.5 py-2">
          <div className="flex items-center justify-between text-xs text-ink-secondary">
            <span>已选 {selectedChapterIds.size} 章</span>
            <div className="flex items-center gap-1.5">
              <button
                type="button"
                onClick={onSelectAllChapters}
                className="text-xs text-ink-secondary hover:text-accent"
              >
                全选
              </button>
              <span className="text-border-strong">/</span>
              <button
                type="button"
                onClick={onClearSelectedChapters}
                className="text-xs text-ink-secondary hover:text-accent"
              >
                清空
              </button>
            </div>
          </div>
          <div className="flex items-center gap-1.5">
            <button
              type="button"
              disabled={batchBusy || selectedChapterIds.size === 0}
              onClick={onBatchFinalize}
              className="flex-1 rounded border border-border-subtle bg-surface px-2 py-1 text-xs text-ink-primary transition-colors hover:border-border-strong disabled:opacity-40"
            >
              定稿
            </button>
            <button
              type="button"
              disabled={batchBusy || selectedChapterIds.size === 0}
              onClick={onBatchExport}
              className="flex flex-1 items-center justify-center gap-1 rounded border border-border-subtle bg-surface px-2 py-1 text-xs text-ink-primary transition-colors hover:border-border-strong disabled:opacity-40"
            >
              <Download className="h-3 w-3" />
              导出
            </button>
            <button
              type="button"
              disabled={batchBusy || selectedChapterIds.size === 0}
              onClick={onBatchDelete}
              className="flex-1 rounded border border-border-subtle bg-surface px-2 py-1 text-xs text-danger transition-colors hover:border-danger/40 disabled:opacity-40"
            >
              删除
            </button>
          </div>
        </div>
      )}

      <div className="flex-1 overflow-y-auto px-2 py-2">
        {volumes.length === 0 ? (
          <div className="flex flex-col items-center justify-center px-3 py-12 text-center">
            <div className="text-xs text-ink-muted">暂无卷结构</div>
            <button
              type="button"
              onClick={onOpenCreateVolume}
              className="mt-3 flex items-center gap-1 rounded-md border border-accent/40 bg-accent-soft px-3 py-1.5 text-xs font-medium text-accent transition-colors hover:border-accent"
            >
              <Plus className="h-3.5 w-3.5" />
              <span>新建第一卷</span>
            </button>
          </div>
        ) : (
          <div className="flex flex-col gap-1.5">
            {volumes.map((vol) => {
              const normalizedSearch = searchQuery.trim().toLowerCase();
              const collapsed = normalizedSearch ? false : collapsedVolIds.has(vol.id);
              const allVolChapters = chaptersByVolume.get(vol.id) ?? [];
              const volChapters = normalizedSearch
                ? allVolChapters.filter(
                    (c) =>
                      c.title.toLowerCase().includes(normalizedSearch) ||
                      String(c.globalSeq).includes(normalizedSearch) ||
                      formatSeq(c.globalSeq).includes(normalizedSearch)
                  )
                : allVolChapters;
              if (normalizedSearch && volChapters.length === 0) return null;
              return (
                <div key={vol.id} className="select-none">
                  {/* Volume header */}
                  <div
                    onContextMenu={(e) => {
                      openContextMenu(e, [
                        {
                          label: "在本卷新建章",
                          icon: <Plus className="h-3.5 w-3.5" />,
                          onSelect: () => {
                            setCollapsedVolIds((prev) => {
                              const next = new Set(prev);
                              next.delete(vol.id);
                              return next;
                            });
                            setAddingChapterVolId(vol.id);
                            setNewChapterTitle("");
                          },
                        },
                        {
                          label: "编辑卷信息",
                          icon: <Pencil className="h-3.5 w-3.5" />,
                          onSelect: () => onOpenEditVolume(vol),
                        },
                        {
                          label: collapsed ? "展开本卷" : "收起本卷",
                          icon: collapsed ? (
                            <ChevronRight className="h-3.5 w-3.5" />
                          ) : (
                            <ChevronDown className="h-3.5 w-3.5" />
                          ),
                          onSelect: () => toggleVolumeCollapse(vol.id),
                        },
                        { type: "separator" },
                        {
                          label: "删除本卷…",
                          icon: <Trash2 className="h-3.5 w-3.5" />,
                          danger: true,
                          onSelect: () => onDeleteVolume(vol.id),
                        },
                      ]);
                    }}
                    onDragOver={(e) => {
                      if (draggingChapterId === null) return;
                      e.preventDefault();
                      setDragOverVolumeId(vol.id);
                      setDragOverChapterId(null);
                    }}
                    onDragLeave={() => {
                      if (dragOverVolumeId === vol.id) setDragOverVolumeId(null);
                    }}
                    onDrop={(e) => {
                      if (draggingChapterId === null) return;
                      e.preventDefault();
                      handleDropOnTarget(vol.id, 0);
                    }}
                    className={`group flex items-center gap-1 rounded-md px-1.5 py-1.5 transition-colors hover:bg-elevated/70 ${
                      dragOverVolumeId === vol.id
                        ? "bg-accent-soft/40 ring-1 ring-accent/30"
                        : ""
                    }`}
                  >
                    <button
                      type="button"
                      onClick={() => toggleVolumeCollapse(vol.id)}
                      className="flex min-w-0 flex-1 items-center gap-1 text-left"
                    >
                      {collapsed ? (
                        <ChevronRight className="h-3.5 w-3.5 shrink-0 text-ink-muted" />
                      ) : (
                        <ChevronDown className="h-3.5 w-3.5 shrink-0 text-ink-muted" />
                      )}
                      <span className="truncate text-xs font-medium text-ink-primary">
                        {vol.title}
                      </span>
                    </button>

                      <div className="flex items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
                        <button
                          type="button"
                          onClick={() => {
                            setCollapsedVolIds((prev) => {
                              const next = new Set(prev);
                              next.delete(vol.id);
                              return next;
                            });
                            setAddingChapterVolId(vol.id);
                            setNewChapterTitle("");
                          }}
                          title="在本卷新建章"
                          aria-label="在本卷新建章"
                          className="rounded p-1 text-ink-muted hover:bg-surface hover:text-accent"
                        >
                          <Plus className="h-3 w-3" />
                        </button>
                        <button
                          type="button"
                          onClick={() => onOpenEditVolume(vol)}
                          title="编辑卷信息"
                          aria-label="编辑卷信息"
                          className="rounded p-1 text-ink-muted hover:bg-surface hover:text-ink-primary"
                        >
                          <Pencil className="h-3 w-3" />
                        </button>
                        <button
                          type="button"
                          onClick={() => onDeleteVolume(vol.id)}
                          title="删除分卷"
                          aria-label="删除分卷"
                          className="rounded p-1 text-ink-muted hover:bg-surface hover:text-danger"
                        >
                          <Trash2 className="h-3 w-3" />
                        </button>
                      </div>
                  </div>

                  {/* Volume chapters */}
                  {!collapsed && (
                    <div className="mt-0.5 flex flex-col gap-0.5 pl-2">
                      {volChapters.map((chap, chapIdx) => {
                        const selected = chap.id === activeChapterId;
                        const st = getChapterStatusMeta(chap.status);
                        const isInsertingAfter =
                          insertingAfterChap?.volId === vol.id &&
                          insertingAfterChap.afterChapId === chap.id;
                        const isDragTarget =
                          dragOverChapterId === chap.id && draggingChapterId !== chap.id;

                        return (
                          <div key={chap.id} className="flex flex-col">
                            <div
                              onContextMenu={(e) => {
                                openContextMenu(e, [
                                  {
                                    label: "进入正文写作",
                                    icon: <PenLine className="h-3.5 w-3.5" />,
                                    onSelect: () => onJumpToChapter(chap.id),
                                  },
                                  {
                                    label: "选中编排本章",
                                    icon: <Layers className="h-3.5 w-3.5" />,
                                    onSelect: () => onSelectChapter(chap.id),
                                  },
                                  { type: "separator" },
                                  {
                                    label: "在此章后插入新章",
                                    icon: <Plus className="h-3.5 w-3.5" />,
                                    onSelect: () => {
                                      setInsertingAfterChap({
                                        volId: vol.id,
                                        afterChapId: chap.id,
                                      });
                                      setInsertChapterTitle("");
                                    },
                                  },
                                  {
                                    label: "上移一章",
                                    icon: <ArrowUp className="h-3.5 w-3.5" />,
                                    disabled: chapIdx === 0 || movingChapterId !== null,
                                    onSelect: () => onMoveChapter(chap.id, "up"),
                                  },
                                  {
                                    label: "下移一章",
                                    icon: <ArrowDown className="h-3.5 w-3.5" />,
                                    disabled:
                                      chapIdx === volChapters.length - 1 ||
                                      movingChapterId !== null,
                                    onSelect: () => onMoveChapter(chap.id, "down"),
                                  },
                                  ...volumes
                                    .filter((target) => target.id !== vol.id)
                                    .map((target) => ({
                                      label: `移动到「${target.title}」`,
                                      icon: <ArrowRight className="h-3.5 w-3.5" />,
                                      disabled: movingChapterId !== null,
                                      onSelect: () =>
                                        onMoveChapterToVolume(chap.id, target.id),
                                    })),
                                  {
                                    label: "复制章节标题",
                                    icon: <Copy className="h-3.5 w-3.5" />,
                                    onSelect: async () => {
                                      try {
                                        await navigator.clipboard.writeText(chap.title);
                                        notify({ tone: "ok", message: "已复制章节标题" });
                                      } catch {
                                        // ignore
                                      }
                                    },
                                  },
                                  { type: "separator" },
                                  {
                                    label: "删除本章…",
                                    icon: <Trash2 className="h-3.5 w-3.5" />,
                                    danger: true,
                                    onSelect: () => onDeleteChapter(chap.id),
                                  },
                                ]);
                              }}
                              draggable={!batchMode && movingChapterId === null}
                              onDragStart={(e) => {
                                e.dataTransfer.effectAllowed = "move";
                                e.dataTransfer.setData("text/plain", String(chap.id));
                                setDraggingChapterId(chap.id);
                              }}
                              onDragEnd={() => {
                                setDraggingChapterId(null);
                                setDragOverVolumeId(null);
                                setDragOverChapterId(null);
                              }}
                              onDragOver={(e) => {
                                if (draggingChapterId === null || draggingChapterId === chap.id) {
                                  return;
                                }
                                e.preventDefault();
                                e.stopPropagation();
                                setDragOverVolumeId(null);
                                setDragOverChapterId(chap.id);
                              }}
                              onDragLeave={() => {
                                if (dragOverChapterId === chap.id) {
                                  setDragOverChapterId(null);
                                }
                              }}
                              onDrop={(e) => {
                                if (draggingChapterId === null) return;
                                e.preventDefault();
                                e.stopPropagation();
                                handleDropOnTarget(vol.id, chapIdx + 1, chap.id);
                              }}
                              className={`group flex w-full items-center gap-1 rounded-xl px-2.5 py-1.5 transition-colors ${
                                selected
                                  ? "bg-elevated font-medium text-ink-primary shadow-xs"
                                  : "text-ink-secondary hover:bg-[var(--bg-hover)] hover:text-ink-primary"
                              } ${isDragTarget ? "ring-1 ring-accent/40 bg-accent-soft/20" : ""}`}
                            >
                              {batchMode && (
                                <button
                                  type="button"
                                  onClick={() => onToggleChapterSelection(chap.id)}
                                  aria-label={
                                    selectedChapterIds.has(chap.id) ? "取消选择" : "选择本章"
                                  }
                                  className={`flex h-4 w-4 shrink-0 items-center justify-center rounded border ${
                                    selectedChapterIds.has(chap.id)
                                      ? "border-accent bg-accent text-canvas"
                                      : "border-border-strong bg-canvas"
                                  }`}
                                >
                                  {selectedChapterIds.has(chap.id) && (
                                    <Check className="h-3 w-3" />
                                  )}
                                </button>
                              )}
                              <button
                                type="button"
                                onClick={() =>
                                  batchMode
                                    ? onToggleChapterSelection(chap.id)
                                    : onSelectChapter(chap.id)
                                }
                                onDoubleClick={() => {
                                  if (!batchMode) onJumpToChapter(chap.id);
                                }}
                                title={`${chap.title}（双击进入写作，右键更多操作）`}
                                className="flex min-w-0 flex-1 items-center gap-2 text-left"
                              >
                                <span
                                  className={`nib-mono shrink-0 text-xs ${
                                    selected ? "text-accent" : "text-ink-muted"
                                  }`}
                                >
                                  {formatSeq(chap.globalSeq)}
                                </span>
                                <span className="min-w-0 flex-1 truncate text-[12.5px]">
                                  {chap.title}
                                </span>
                              </button>

                              <div className="hidden shrink-0 items-center gap-0.5 group-hover:flex">
                                <button
                                  type="button"
                                  onClick={() => {
                                    setInsertingAfterChap({
                                      volId: vol.id,
                                      afterChapId: chap.id,
                                    });
                                    setInsertChapterTitle("");
                                  }}
                                  title="在此章后插入新章"
                                  aria-label="在此章后插入新章"
                                  className="rounded p-0.5 text-ink-muted hover:bg-surface hover:text-accent"
                                >
                                  <Plus className="h-3 w-3" />
                                </button>
                                <button
                                  type="button"
                                  onClick={() => onDeleteChapter(chap.id)}
                                  title="删除本章"
                                  aria-label="删除本章"
                                  className="rounded p-0.5 text-ink-muted hover:bg-surface hover:text-danger"
                                >
                                  <Trash2 className="h-3 w-3" />
                                </button>
                              </div>
                              <div className="flex shrink-0 items-center group-hover:hidden">
                                {chap.status === "upstream_dirty" ? (
                                  <span
                                    title="前文设定有变动，建议核对"
                                    className="shrink-0 rounded border border-warn/40 bg-warn-soft px-1 text-[11px] font-medium text-warn"
                                  >
                                    待核
                                  </span>
                                ) : (
                                  <span
                                    title={st.label}
                                    className={`h-1.5 w-1.5 shrink-0 rounded-full ${st.dotClass}`}
                                  />
                                )}
                              </div>
                            </div>

                            {isInsertingAfter && (
                              <div className="my-1 flex items-center gap-1 rounded-md border border-accent/50 bg-canvas px-1.5 py-1">
                                <input
                                  type="text"
                                  autoFocus
                                  value={insertChapterTitle}
                                  disabled={insertingChapter}
                                  onChange={(e) => setInsertChapterTitle(e.target.value)}
                                  onKeyDown={(e) => {
                                    if (e.key === "Enter" && !isIMEComposing(e)) {
                                      e.preventDefault();
                                      void handleSubmitInsertAfter(vol.id, chap.id);
                                    } else if (e.key === "Escape") {
                                      setInsertingAfterChap(null);
                                      setInsertChapterTitle("");
                                    }
                                  }}
                                  placeholder="插入章节标题…"
                                  className="min-w-0 flex-1 bg-transparent text-xs text-ink-primary placeholder:text-ink-muted focus:outline-none"
                                />
                                <button
                                  type="button"
                                  disabled={insertingChapter}
                                  onClick={() => void handleSubmitInsertAfter(vol.id, chap.id)}
                                  className="rounded p-0.5 text-accent hover:bg-surface"
                                >
                                  <Check className="h-3.5 w-3.5" />
                                </button>
                                <button
                                  type="button"
                                  onClick={() => {
                                    setInsertingAfterChap(null);
                                    setInsertChapterTitle("");
                                  }}
                                  className="rounded p-0.5 text-ink-muted hover:text-ink-primary"
                                >
                                  <X className="h-3.5 w-3.5" />
                                </button>
                              </div>
                            )}
                          </div>
                        );
                      })}

                      {addingChapterVolId === vol.id ? (
                        <div className="mt-1 flex items-center gap-1 rounded-md border border-accent/50 bg-canvas px-1.5 py-1">
                          <input
                            type="text"
                            autoFocus
                            value={newChapterTitle}
                            disabled={creatingChapter}
                            onChange={(e) => setNewChapterTitle(e.target.value)}
                            onKeyDown={(e) => {
                              if (e.key === "Enter" && !isIMEComposing(e)) {
                                e.preventDefault();
                                void handleSubmitCreateChapter(vol.id);
                              } else if (e.key === "Escape") {
                                setAddingChapterVolId(null);
                                setNewChapterTitle("");
                              }
                            }}
                            placeholder="章节标题…"
                            className="min-w-0 flex-1 bg-transparent text-xs text-ink-primary placeholder:text-ink-muted focus:outline-none"
                          />
                          <button
                            type="button"
                            disabled={creatingChapter}
                            onClick={() => void handleSubmitCreateChapter(vol.id)}
                            className="rounded p-0.5 text-accent hover:bg-surface"
                          >
                            <Check className="h-3.5 w-3.5" />
                          </button>
                          <button
                            type="button"
                            onClick={() => {
                              setAddingChapterVolId(null);
                              setNewChapterTitle("");
                            }}
                            className="rounded p-0.5 text-ink-muted hover:text-ink-primary"
                          >
                            <X className="h-3.5 w-3.5" />
                          </button>
                        </div>
                      ) : (
                        <button
                          type="button"
                          onClick={() => {
                            setAddingChapterVolId(vol.id);
                            setNewChapterTitle("");
                          }}
                          className="mt-0.5 flex items-center gap-1.5 rounded-md px-2 py-1 text-[11.5px] text-ink-muted transition-colors hover:bg-elevated/40 hover:text-ink-secondary"
                        >
                          <Plus className="h-3 w-3" />
                          <span>新建章</span>
                        </button>
                      )}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </aside>
  );
}
