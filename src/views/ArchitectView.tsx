import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowDown,
  ArrowRight,
  ArrowUp,
  Check,
  ChevronDown,
  ChevronRight,
  Layers,
  Pencil,
  Plus,
  Save,
  Sparkles,
  Trash2,
  X,
} from "lucide-react";
import {
  LibraryService,
  type Beat,
  type Chapter,
  type Volume,
} from "../mygo";
import { useApp } from "../stores/AppContext";
import { VolumePacingChart } from "../components/architect/VolumePacingChart";
import { BatchSchedulerPanel } from "../components/architect/BatchSchedulerPanel";
import { BatchOutlineModal } from "../components/architect/BatchOutlineModal";
import {
  BEAT_STAGES,
  EMOTION_TAGS,
  HOOK_TYPES,
  formatErrorMessage,
  formatNumber,
  formatSeq,
  getBeatAccentClass,
  getChapterStatusMeta,
  getEntityKindLabel,
} from "../lib/constants";

interface VolumeDialogState {
  mode: "create" | "edit";
  id: number;
  seq: number;
  title: string;
  goal: string;
  climaxPlan: string;
}

interface EditableBeat {
  localKey: string;
  id: number;
  stage: string;
  summary: string;
  conflictNote: string;
  povEntityId?: number;
  entityIds: number[];
  targetWords: number;
}

let beatKeyCounter = 1;
function nextBeatKey(): string {
  return `beat-${beatKeyCounter++}`;
}

function toEditableBeats(beats: Beat[]): EditableBeat[] {
  return beats.map((b) => ({
    localKey: b.id > 0 ? `id-${b.id}` : nextBeatKey(),
    id: b.id,
    stage: b.stage,
    summary: b.summary,
    conflictNote: b.conflictNote,
    povEntityId: b.povEntityId,
    entityIds: Array.isArray(b.entityIds) ? [...b.entityIds] : [],
    targetWords: b.targetWords || 500,
  }));
}

export function ArchitectView() {
  const {
    activeBook,
    activeGenrePreset,
    volumes,
    chapters,
    activeChapter,
    activeChapterId,
    entities,
    selectChapter,
    jumpToChapter,
    refreshBookData,
    refreshChapters,
    refreshForeshadowings,
    updateChapterInCache,
    setActiveTab,
    openAtlasSubTab,
    notify,
    registerSaveHandler,
  } = useApp();

  // Phase 4 outline modal & pacing refresh counter
  const [outlineModalOpen, setOutlineModalOpen] = useState(false);
  const [pacingRefreshKey, setPacingRefreshKey] = useState(0);

  // Tree state
  const [collapsedVolIds, setCollapsedVolIds] = useState<Set<number>>(new Set());
  const [addingChapterVolId, setAddingChapterVolId] = useState<number | null>(null);
  const [newChapterTitle, setNewChapterTitle] = useState("");
  const [creatingChapter, setCreatingChapter] = useState(false);
  const [confirmDeleteVolId, setConfirmDeleteVolId] = useState<number | null>(null);
  const [confirmDeleteChapId, setConfirmDeleteChapId] = useState<number | null>(null);
  const [insertingAfterChap, setInsertingAfterChap] = useState<{ volId: number; afterChapId: number } | null>(null);
  const [insertChapterTitle, setInsertChapterTitle] = useState("");
  const [insertingChapter, setInsertingChapter] = useState(false);
  const [movingChapterId, setMovingChapterId] = useState<number | null>(null);
  const [volumeDialog, setVolumeDialog] = useState<VolumeDialogState | null>(null);
  const [savingVolume, setSavingVolume] = useState(false);

  // Chapter metadata draft
  const [metaTitle, setMetaTitle] = useState("");
  const [metaEmotion, setMetaEmotion] = useState("setup");
  const [metaHookType, setMetaHookType] = useState("mystery");
  const [metaHookNote, setMetaHookNote] = useState("");
  const [metaOutline, setMetaOutline] = useState("");
  const [metaCausal, setMetaCausal] = useState("");
  const [metaDirty, setMetaDirty] = useState(false);
  const [savingMeta, setSavingMeta] = useState(false);

  // Beats state
  const [beats, setBeats] = useState<EditableBeat[]>([]);
  const [beatsLoading, setBeatsLoading] = useState(false);
  const [beatsDirty, setBeatsDirty] = useState(false);
  const [savingBeats, setSavingBeats] = useState(false);
  const [confirmFillBeats, setConfirmFillBeats] = useState(false);
  const [openEntityPickerKey, setOpenEntityPickerKey] = useState<string | null>(null);

  const entityMap = useMemo(() => {
    const map = new Map<number, (typeof entities)[number]>();
    for (const e of entities) {
      map.set(e.id, e);
    }
    return map;
  }, [entities]);

  const chaptersByVolume = useMemo(() => {
    const map = new Map<number, Chapter[]>();
    for (const c of chapters) {
      const list = map.get(c.volumeId);
      if (list) {
        list.push(c);
      } else {
        map.set(c.volumeId, [c]);
      }
    }
    return map;
  }, [chapters]);

  const activeVolume = useMemo(() => {
    if (!activeChapter) return volumes[0] ?? null;
    return volumes.find((v) => v.id === activeChapter.volumeId) ?? null;
  }, [volumes, activeChapter]);

  const activeVolumeChapters = useMemo(() => {
    if (!activeVolume) return [];
    return chaptersByVolume.get(activeVolume.id) ?? [];
  }, [activeVolume, chaptersByVolume]);

  const nextGlobalSeq = useMemo(
    () => chapters.reduce((m, c) => (c.globalSeq > m ? c.globalSeq : m), 0) + 1,
    [chapters]
  );

  // Sync metadata form when active chapter changes
  useEffect(() => {
    if (!activeChapter) {
      setMetaTitle("");
      setMetaEmotion("setup");
      setMetaHookType("mystery");
      setMetaHookNote("");
      setMetaOutline("");
      setMetaCausal("");
      setMetaDirty(false);
      setBeats([]);
      setBeatsDirty(false);
      return;
    }
    setMetaTitle(activeChapter.title);
    setMetaEmotion(activeChapter.emotionTag || "setup");
    setMetaHookType(activeChapter.hookType || "mystery");
    setMetaHookNote(activeChapter.hookNote || "");
    setMetaOutline(activeChapter.outlineSummary || "");
    setMetaCausal(activeChapter.causalSummary || "");
    setMetaDirty(false);
    setConfirmFillBeats(false);
    setOpenEntityPickerKey(null);
  }, [activeChapter?.id, activeChapter?.updatedAt]);

  // Fetch beats when active chapter ID changes
  useEffect(() => {
    if (!activeChapterId) {
      setBeats([]);
      setBeatsDirty(false);
      return;
    }
    let cancelled = false;
    setBeatsLoading(true);
    LibraryService.listBeats(activeChapterId)
      .then((list) => {
        if (!cancelled) {
          setBeats(toEditableBeats(list));
          setBeatsDirty(false);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          notify({ tone: "danger", message: "读取节拍卡失败", detail: formatErrorMessage(err) });
        }
      })
      .finally(() => {
        if (!cancelled) setBeatsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [activeChapterId, notify]);

  // Close entity picker when clicking outside or pressing Escape
  const beatListRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!openEntityPickerKey) return;
    const onDown = (e: MouseEvent) => {
      const target = e.target as HTMLElement | null;
      if (target && !target.closest("[data-entity-picker]")) {
        setOpenEntityPickerKey(null);
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpenEntityPickerKey(null);
    };
    document.addEventListener("mousedown", onDown);
    window.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      window.removeEventListener("keydown", onKey);
    };
  }, [openEntityPickerKey]);

  // Close volume dialog on Escape
  useEffect(() => {
    if (!volumeDialog) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setVolumeDialog(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [volumeDialog]);

  const handleSaveMeta = useCallback(async (): Promise<boolean> => {
    if (!activeChapter) return true;
    const trimmedTitle = metaTitle.trim();
    if (!trimmedTitle) {
      notify({ tone: "warn", message: "章节标题不能为空" });
      return false;
    }
    setSavingMeta(true);
    try {
      const updated = await LibraryService.updateChapterMeta({
        id: activeChapter.id,
        title: trimmedTitle,
        status: activeChapter.status,
        emotionTag: metaEmotion,
        hookType: metaHookType,
        hookNote: metaHookNote.trim(),
        outlineSummary: metaOutline.trim(),
        causalSummary: metaCausal.trim(),
      });
      updateChapterInCache(updated);
      setMetaDirty(false);
      setPacingRefreshKey((k) => k + 1);
      return true;
    } catch (err) {
      notify({ tone: "danger", message: "保存章节元信息失败", detail: formatErrorMessage(err) });
      return false;
    } finally {
      setSavingMeta(false);
    }
  }, [
    activeChapter,
    metaTitle,
    metaEmotion,
    metaHookType,
    metaHookNote,
    metaOutline,
    metaCausal,
    notify,
    updateChapterInCache,
  ]);

  const handleSaveBeats = useCallback(async (): Promise<boolean> => {
    if (!activeChapter) return true;
    for (let i = 0; i < beats.length; i++) {
      const beat = beats[i];
      if (!beat || !beat.summary.trim()) {
        notify({ tone: "warn", message: `第 ${i + 1} 张节拍卡摘要不能为空` });
        return false;
      }
    }
    setSavingBeats(true);
    try {
      const payload: Beat[] = beats.map((b, idx) => ({
        id: b.id,
        chapterId: activeChapter.id,
        seq: idx + 1,
        stage: b.stage,
        summary: b.summary.trim(),
        conflictNote: b.conflictNote.trim(),
        povEntityId: b.povEntityId && b.povEntityId > 0 ? b.povEntityId : undefined,
        entityIds: b.entityIds,
        targetWords: b.targetWords > 0 ? b.targetWords : 500,
      }));
      const saved = await LibraryService.replaceBeats(activeChapter.id, payload);
      setBeats(toEditableBeats(saved));
      setBeatsDirty(false);
      // Reload chapter so status transitions (e.g. planned -> outlined) reflect cleanly
      const fresh = await LibraryService.getChapter(activeChapter.id);
      updateChapterInCache(fresh);
      return true;
    } catch (err) {
      notify({ tone: "danger", message: "保存节拍卡失败", detail: formatErrorMessage(err) });
      return false;
    } finally {
      setSavingBeats(false);
    }
  }, [activeChapter, beats, notify, updateChapterInCache]);

  const handleSaveAll = useCallback(async (): Promise<boolean> => {
    if (!activeChapter) return true;
    let savedSomething = false;
    if (metaDirty) {
      const ok = await handleSaveMeta();
      if (!ok) return false;
      savedSomething = true;
    }
    if (beatsDirty) {
      const ok = await handleSaveBeats();
      if (!ok) return false;
      savedSomething = true;
    }
    if (savedSomething) {
      notify({ tone: "ok", message: "编排已保存" });
      return true;
    }
    // Explicit save when nothing dirty still saves cleanly
    const okMeta = await handleSaveMeta();
    const okBeats = okMeta ? await handleSaveBeats() : false;
    if (okMeta && okBeats) {
      notify({ tone: "ok", message: "编排已保存" });
      return true;
    }
    return false;
  }, [activeChapter, metaDirty, beatsDirty, handleSaveMeta, handleSaveBeats, notify]);

  useEffect(() => {
    registerSaveHandler(async () => {
      await handleSaveAll();
    });
    return () => registerSaveHandler(null);
  }, [registerSaveHandler, handleSaveAll]);

  // Volume CRUD
  const openCreateVolume = () => {
    const nextNum = Math.max(0, ...volumes.map((v) => v.seq)) + 1;
    setVolumeDialog({
      mode: "create",
      id: 0,
      seq: 0,
      title: `第${nextNum}卷 `,
      goal: "",
      climaxPlan: "",
    });
  };

  const openEditVolume = (vol: Volume) => {
    setVolumeDialog({
      mode: "edit",
      id: vol.id,
      seq: vol.seq,
      title: vol.title,
      goal: vol.goal,
      climaxPlan: vol.climaxPlan,
    });
  };

  const handleSubmitVolume = async () => {
    if (!activeBook || !volumeDialog) return;
    const trimmedTitle = volumeDialog.title.trim();
    if (!trimmedTitle) {
      notify({ tone: "warn", message: "卷名不能为空" });
      return;
    }
    setSavingVolume(true);
    try {
      await LibraryService.upsertVolume({
        id: volumeDialog.mode === "edit" ? volumeDialog.id : 0,
        bookId: activeBook.id,
        seq: volumeDialog.mode === "edit" ? volumeDialog.seq : 0,
        title: trimmedTitle,
        goal: volumeDialog.goal.trim(),
        climaxPlan: volumeDialog.climaxPlan.trim(),
      });
      await refreshBookData(activeBook.id, activeChapterId);
      setVolumeDialog(null);
      notify({
        tone: "ok",
        message: volumeDialog.mode === "create" ? "已新建卷" : "已更新卷信息",
      });
    } catch (err) {
      notify({ tone: "danger", message: "保存卷失败", detail: formatErrorMessage(err) });
    } finally {
      setSavingVolume(false);
    }
  };

  const handleDeleteVolume = async (volId: number) => {
    if (!activeBook) return;
    try {
      await LibraryService.deleteVolume(volId);
      setConfirmDeleteVolId(null);
      await refreshBookData(activeBook.id, null);
      notify({ tone: "ok", message: "已删除该卷" });
    } catch (err) {
      notify({ tone: "danger", message: "删除卷失败", detail: formatErrorMessage(err) });
    }
  };

  const handleMoveChapter = async (chapId: number, direction: "up" | "down") => {
    if (!activeBook || movingChapterId !== null) return;
    setMovingChapterId(chapId);
    try {
      await LibraryService.moveChapter({ chapterId: chapId, direction });
      await refreshBookData(activeBook.id, activeChapterId);
      setPacingRefreshKey((k) => k + 1);
    } catch (err) {
      notify({ tone: "danger", message: "调整章节顺序失败", detail: formatErrorMessage(err) });
    } finally {
      setMovingChapterId(null);
    }
  };

  const handleDeleteChapter = async (chapId: number) => {
    if (!activeBook) return;
    try {
      await LibraryService.deleteChapter(chapId);
      setConfirmDeleteChapId(null);
      const nextPreferred = activeChapterId === chapId ? null : activeChapterId;
      await refreshBookData(activeBook.id, nextPreferred);
      setPacingRefreshKey((k) => k + 1);
      notify({ tone: "ok", message: "已删除章节并重排章序" });
    } catch (err) {
      notify({ tone: "danger", message: "删除章节失败", detail: formatErrorMessage(err) });
    }
  };

  const handleInsertChapterAfter = async (volId: number, afterChapId: number) => {
    if (!activeBook || insertingChapter) return;
    const trimmed = insertChapterTitle.trim();
    if (!trimmed) {
      notify({ tone: "warn", message: "请输入章节标题" });
      return;
    }
    setInsertingChapter(true);
    try {
      const created = await LibraryService.insertChapter({
        bookId: activeBook.id,
        volumeId: volId,
        afterChapterId: afterChapId,
        title: trimmed,
        emotionTag: "setup",
        hookType: "mystery",
        hookNote: "",
        outlineSummary: "",
      });
      setInsertingAfterChap(null);
      setInsertChapterTitle("");
      await refreshBookData(activeBook.id, created.id);
      setPacingRefreshKey((k) => k + 1);
      notify({ tone: "ok", message: `已插入第 ${formatSeq(created.globalSeq)} 章` });
    } catch (err) {
      notify({ tone: "danger", message: "插入章节失败", detail: formatErrorMessage(err) });
    } finally {
      setInsertingChapter(false);
    }
  };

  const handleCreateChapter = async (volumeId: number) => {
    if (!activeBook) return;
    const trimmed = newChapterTitle.trim();
    if (!trimmed) {
      setAddingChapterVolId(null);
      return;
    }
    setCreatingChapter(true);
    try {
      const created = await LibraryService.createChapter({
        bookId: activeBook.id,
        volumeId,
        title: trimmed,
        emotionTag: "setup",
        hookType: "mystery",
        hookNote: "",
        outlineSummary: "",
      });
      setNewChapterTitle("");
      setAddingChapterVolId(null);
      await refreshBookData(activeBook.id, created.id);
    } catch (err) {
      notify({ tone: "danger", message: "新建章节失败", detail: formatErrorMessage(err) });
    } finally {
      setCreatingChapter(false);
    }
  };

  // Beat mutations
  const addBeat = () => {
    setBeats((prev) => [
      ...prev,
      {
        localKey: nextBeatKey(),
        id: 0,
        stage: prev.length === 0 ? "setup" : "turn",
        summary: "",
        conflictNote: "",
        entityIds: [],
        targetWords: 500,
      },
    ]);
    setBeatsDirty(true);
  };

  const updateBeat = (localKey: string, patch: Partial<EditableBeat>) => {
    setBeats((prev) =>
      prev.map((b) => (b.localKey === localKey ? { ...b, ...patch } : b))
    );
    setBeatsDirty(true);
  };

  const moveBeat = (index: number, delta: -1 | 1) => {
    const nextIdx = index + delta;
    setBeats((prev) => {
      if (nextIdx < 0 || nextIdx >= prev.length) return prev;
      const copy = [...prev];
      const current = copy[index];
      const target = copy[nextIdx];
      if (!current || !target) return prev;
      copy[index] = target;
      copy[nextIdx] = current;
      return copy;
    });
    setBeatsDirty(true);
  };

  const removeBeat = (localKey: string) => {
    setBeats((prev) => prev.filter((b) => b.localKey !== localKey));
    setBeatsDirty(true);
  };

  const toggleBeatEntity = (localKey: string, entityId: number) => {
    setBeats((prev) =>
      prev.map((b) => {
        if (b.localKey !== localKey) return b;
        const exists = b.entityIds.includes(entityId);
        return {
          ...b,
          entityIds: exists
            ? b.entityIds.filter((id) => id !== entityId)
            : [...b.entityIds, entityId],
        };
      })
    );
    setBeatsDirty(true);
  };

  const applyGenreBeatTemplates = () => {
    const templates = activeGenrePreset?.defaultBeats ?? [];
    if (templates.length === 0) {
      notify({ tone: "warn", message: "当前题材暂无默认节拍模板" });
      return;
    }
    if (beats.length > 0 && !confirmFillBeats) {
      setConfirmFillBeats(true);
      return;
    }
    const generated: EditableBeat[] = templates.map((tpl) => ({
      localKey: nextBeatKey(),
      id: 0,
      stage: tpl.stage,
      summary: tpl.title ? `${tpl.title}：${tpl.promptHint}` : tpl.promptHint,
      conflictNote: "",
      entityIds: [],
      targetWords: tpl.targetWords || 500,
    }));
    setBeats(generated);
    setBeatsDirty(true);
    setConfirmFillBeats(false);
    notify({
      tone: "info",
      message: `已按「${activeGenrePreset?.name ?? "当前题材"}」填充 ${generated.length} 张节拍卡`,
    });
  };

  const totalBeatWords = useMemo(
    () => beats.reduce((sum, b) => sum + (b.targetWords || 0), 0),
    [beats]
  );

  const toggleVolumeCollapse = (volId: number) => {
    setCollapsedVolIds((prev) => {
      const next = new Set(prev);
      if (next.has(volId)) next.delete(volId);
      else next.add(volId);
      return next;
    });
  };

  return (
    <div className="flex min-h-0 flex-1 overflow-hidden">
      {/* Left Panel: Volume & Chapter Tree (240px) */}
      <aside
        className="flex shrink-0 flex-col border-r border-border-subtle bg-surface"
        style={{ width: "var(--sidebar-tree-width)" }}
      >
        <div className="flex items-center justify-between border-b border-border-subtle px-3 py-2.5">
          <div className="flex items-center gap-1.5 text-xs font-medium text-ink-secondary">
            <Layers className="h-3.5 w-3.5 text-ink-muted" />
            <span>目录</span>
            <span className="nib-mono text-xs text-ink-muted">
              {chapters.length}
            </span>
          </div>
          <button
            type="button"
            onClick={openCreateVolume}
            title="新建卷"
            className="flex items-center gap-1 rounded-md px-1.5 py-1 text-xs font-medium text-ink-secondary transition-colors hover:bg-elevated hover:text-ink-primary"
          >
            <Plus className="h-3.5 w-3.5 text-accent" />
            <span>新建卷</span>
          </button>
        </div>

        <div className="flex-1 overflow-y-auto px-2 py-2">
          {volumes.length === 0 ? (
            <div className="flex flex-col items-center justify-center px-3 py-12 text-center">
              <div className="text-xs text-ink-muted">暂无卷结构</div>
              <button
                type="button"
                onClick={openCreateVolume}
                className="mt-3 flex items-center gap-1 rounded-md border border-accent/40 bg-accent-soft px-3 py-1.5 text-xs font-medium text-accent transition-colors hover:border-accent"
              >
                <Plus className="h-3.5 w-3.5" />
                <span>新建第一卷</span>
              </button>
            </div>
          ) : (
            <div className="flex flex-col gap-1.5">
              {volumes.map((vol) => {
                const collapsed = collapsedVolIds.has(vol.id);
                const volChapters = chaptersByVolume.get(vol.id) ?? [];
                const confirmingDel = confirmDeleteVolId === vol.id;
                return (
                  <div key={vol.id} className="select-none">
                    {/* Volume header */}
                    <div className="group flex items-center gap-1 rounded-md px-1.5 py-1.5 transition-colors hover:bg-elevated/70">
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

                      {confirmingDel ? (
                        <div className="flex items-center gap-0.5">
                          <button
                            type="button"
                            onClick={() => void handleDeleteVolume(vol.id)}
                            title="确认删除卷及下属章节"
                            className="rounded bg-danger-soft px-1.5 py-0.5 text-[10px] font-medium text-danger"
                          >
                            删除
                          </button>
                          <button
                            type="button"
                            onClick={() => setConfirmDeleteVolId(null)}
                            className="rounded p-0.5 text-ink-muted hover:text-ink-primary"
                          >
                            <X className="h-3 w-3" />
                          </button>
                        </div>
                      ) : (
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
                            title="新建章"
                            className="rounded p-1 text-ink-muted hover:bg-surface hover:text-accent"
                          >
                            <Plus className="h-3 w-3" />
                          </button>
                          <button
                            type="button"
                            onClick={() => openEditVolume(vol)}
                            title="编辑卷"
                            className="rounded p-1 text-ink-muted hover:bg-surface hover:text-ink-primary"
                          >
                            <Pencil className="h-3 w-3" />
                          </button>
                          <button
                            type="button"
                            onClick={() => setConfirmDeleteVolId(vol.id)}
                            title="删除卷"
                            className="rounded p-1 text-ink-muted hover:bg-surface hover:text-danger"
                          >
                            <Trash2 className="h-3 w-3" />
                          </button>
                        </div>
                      )}
                    </div>

                    {/* Volume chapters */}
                    {!collapsed && (
                      <div className="mt-0.5 flex flex-col gap-0.5 pl-3">
                        {volChapters.map((chap, chapIdx) => {
                          const selected = chap.id === activeChapterId;
                          const st = getChapterStatusMeta(chap.status);
                          const confirmingChapDel = confirmDeleteChapId === chap.id;
                          const isInsertingAfter =
                            insertingAfterChap?.volId === vol.id &&
                            insertingAfterChap.afterChapId === chap.id;
                          return (
                            <div key={chap.id} className="flex flex-col">
                              <div
                                className={`group flex w-full items-center gap-1 rounded-md px-2 py-1.5 transition-colors ${
                                  selected
                                    ? "bg-accent-soft text-ink-primary"
                                    : "text-ink-secondary hover:bg-elevated/60 hover:text-ink-primary"
                                }`}
                              >
                                <button
                                  type="button"
                                  onClick={() => void selectChapter(chap.id)}
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

                                {confirmingChapDel ? (
                                  <div className="flex shrink-0 items-center gap-0.5">
                                    <button
                                      type="button"
                                      onClick={() => void handleDeleteChapter(chap.id)}
                                      title="确认删除本章"
                                      className="rounded bg-danger-soft px-1.5 py-0.5 text-xs font-medium text-danger"
                                    >
                                      删除
                                    </button>
                                    <button
                                      type="button"
                                      onClick={() => setConfirmDeleteChapId(null)}
                                      className="rounded p-0.5 text-ink-muted hover:text-ink-primary"
                                    >
                                      <X className="h-3 w-3" />
                                    </button>
                                  </div>
                                ) : (
                                  <>
                                    <div className="hidden shrink-0 items-center gap-0.5 group-hover:flex">
                                      <button
                                        type="button"
                                        disabled={chapIdx === 0 || movingChapterId !== null}
                                        onClick={() => void handleMoveChapter(chap.id, "up")}
                                        title="上移一章"
                                        className="rounded p-0.5 text-ink-muted hover:bg-surface hover:text-ink-primary disabled:opacity-30"
                                      >
                                        <ArrowUp className="h-3 w-3" />
                                      </button>
                                      <button
                                        type="button"
                                        disabled={chapIdx === volChapters.length - 1 || movingChapterId !== null}
                                        onClick={() => void handleMoveChapter(chap.id, "down")}
                                        title="下移一章"
                                        className="rounded p-0.5 text-ink-muted hover:bg-surface hover:text-ink-primary disabled:opacity-30"
                                      >
                                        <ArrowDown className="h-3 w-3" />
                                      </button>
                                      <button
                                        type="button"
                                        onClick={() => {
                                          setInsertingAfterChap({ volId: vol.id, afterChapId: chap.id });
                                          setInsertChapterTitle("");
                                        }}
                                        title="在此章后插入新章"
                                        className="rounded p-0.5 text-ink-muted hover:bg-surface hover:text-accent"
                                      >
                                        <Plus className="h-3 w-3" />
                                      </button>
                                      <button
                                        type="button"
                                        onClick={() => setConfirmDeleteChapId(chap.id)}
                                        title="删除本章"
                                        className="rounded p-0.5 text-ink-muted hover:bg-surface hover:text-danger"
                                      >
                                        <Trash2 className="h-3 w-3" />
                                      </button>
                                    </div>
                                    <div className="flex shrink-0 items-center group-hover:hidden">
                                      {chap.status === "upstream_dirty" ? (
                                        <span
                                          title="上游章节设定变更，建议复核"
                                          className="shrink-0 rounded border border-danger/40 bg-danger-soft px-1 text-xs font-medium text-danger"
                                        >
                                          变更
                                        </span>
                                      ) : (
                                        <span
                                          title={st.label}
                                          className={`h-1.5 w-1.5 shrink-0 rounded-full ${st.dotClass}`}
                                        />
                                      )}
                                    </div>
                                  </>
                                )}
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
                                      if (e.key === "Enter") {
                                        e.preventDefault();
                                        void handleInsertChapterAfter(vol.id, chap.id);
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
                                    onClick={() => void handleInsertChapterAfter(vol.id, chap.id)}
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
                                if (e.key === "Enter") {
                                  e.preventDefault();
                                  void handleCreateChapter(vol.id);
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
                              onClick={() => void handleCreateChapter(vol.id)}
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

      {/* Center Workspace */}
      <main className="flex min-w-0 flex-1 flex-col overflow-hidden bg-canvas">
        {!activeChapter ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-4 px-6 text-center">
            {volumes.length === 0 ? (
              <>
                <div className="text-[13px] text-ink-secondary">
                  先新建第一卷，再开始安排章节与大纲
                </div>
                <button
                  type="button"
                  onClick={openCreateVolume}
                  className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-[13px] font-semibold text-canvas transition-opacity hover:opacity-90"
                >
                  <Plus className="h-3.5 w-3.5" />
                  <span>新建第一卷</span>
                </button>
              </>
            ) : (
              <div className="flex w-full max-w-[380px] flex-col items-center gap-3">
                <div className="text-[13px] text-ink-secondary">
                  {activeVolume ? `「${activeVolume.title}」还没有章节` : "选择或新建一章开始写大纲"}
                </div>
                {activeVolume && (
                  <form
                    onSubmit={(e) => {
                      e.preventDefault();
                      void handleCreateChapter(activeVolume.id);
                    }}
                    className="flex w-full items-center gap-2"
                  >
                    <input
                      type="text"
                      value={newChapterTitle}
                      disabled={creatingChapter}
                      onChange={(e) => setNewChapterTitle(e.target.value)}
                      placeholder="输入第一章标题，回车新建"
                      className="min-w-0 flex-1 rounded-lg border border-border-subtle bg-surface px-3 py-2 text-[13px] text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                    />
                    <button
                      type="submit"
                      disabled={creatingChapter || !newChapterTitle.trim()}
                      className="shrink-0 rounded-lg bg-accent px-4 py-2 text-[13px] font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-45"
                    >
                      新建
                    </button>
                  </form>
                )}
                {activeVolume && (
                  <button
                    type="button"
                    onClick={() => setOutlineModalOpen(true)}
                    className="flex items-center gap-1.5 rounded-lg border border-border-subtle bg-surface px-3.5 py-1.5 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary"
                  >
                    <Sparkles className="h-3.5 w-3.5 text-accent" />
                    <span>用 AI 批量生成章纲</span>
                  </button>
                )}
              </div>
            )}
          </div>
        ) : (
          <div className="flex-1 overflow-y-auto px-8 py-6">
            <div className="mx-auto flex w-full max-w-[880px] flex-col gap-6">
              {/* Volume Pacing SVG Chart & Diagnostics */}
              {activeVolume && (
                <VolumePacingChart
                  volume={activeVolume}
                  activeChapterId={activeChapter.id}
                  refreshTrigger={pacingRefreshKey + chapters.length}
                  onSelectChapter={(cid) => void selectChapter(cid)}
                  onOpenForeshadowings={() => openAtlasSubTab("foreshadowings")}
                />
              )}

              {/* Chapter Metadata Card */}
              <section className="rounded-xl border border-border-subtle bg-surface p-5">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div className="flex min-w-0 flex-1 items-center gap-2.5">
                    <span className="nib-mono shrink-0 rounded bg-elevated px-2 py-0.5 text-xs text-ink-secondary">
                      第 {formatSeq(activeChapter.globalSeq)} 章
                    </span>
                    <input
                      type="text"
                      value={metaTitle}
                      onChange={(e) => {
                        setMetaTitle(e.target.value);
                        setMetaDirty(true);
                      }}
                      placeholder="输入章节标题"
                      className="min-w-0 flex-1 bg-transparent text-base font-medium text-ink-primary placeholder:text-ink-muted focus:outline-none"
                    />
                  </div>

                  <div className="flex shrink-0 items-center gap-2">
                    <span
                      className={`rounded-full border px-2 py-0.5 text-xs ${
                        getChapterStatusMeta(activeChapter.status).badgeClass
                      }`}
                    >
                      {getChapterStatusMeta(activeChapter.status).label}
                    </span>
                    <button
                      type="button"
                      disabled={savingMeta || savingBeats || (!metaDirty && !beatsDirty)}
                      onClick={() => void handleSaveAll()}
                      title="保存大纲 (Ctrl+S)"
                      className="flex items-center gap-1 rounded-lg border border-border-subtle bg-elevated px-2.5 py-1 text-xs font-medium text-ink-primary transition-colors hover:border-border-strong disabled:opacity-45"
                    >
                      <Save className="h-3.5 w-3.5 text-accent" />
                      <span>{savingMeta || savingBeats ? "保存中…" : "保存"}</span>
                    </button>
                    <button
                      type="button"
                      onClick={async () => {
                        if (metaDirty || beatsDirty) {
                          const ok = await handleSaveAll();
                          if (!ok) return;
                        }
                        setActiveTab("studio");
                      }}
                      className="flex items-center gap-1 rounded-lg bg-accent px-3 py-1 text-xs font-semibold text-canvas transition-opacity hover:opacity-90"
                    >
                      <span>开始写作</span>
                      <ArrowRight className="h-3.5 w-3.5" />
                    </button>
                  </div>
                </div>

                {/* Emotion, Hook Type, Hook Note */}
                <div className="mt-4 grid grid-cols-1 gap-3 md:grid-cols-[170px_170px_1fr]">
                  <div>
                    <label className="mb-1 block text-xs font-medium text-ink-secondary">
                      本章节奏
                    </label>
                    <select
                      value={metaEmotion}
                      onChange={(e) => {
                        setMetaEmotion(e.target.value);
                        setMetaDirty(true);
                      }}
                      className="w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                    >
                      {EMOTION_TAGS.map((opt) => (
                        <option key={opt.value} value={opt.value}>
                          {opt.label}
                        </option>
                      ))}
                    </select>
                  </div>

                  <div>
                    <label className="mb-1 block text-xs font-medium text-ink-secondary">
                      章末悬念
                    </label>
                    <select
                      value={metaHookType}
                      onChange={(e) => {
                        setMetaHookType(e.target.value);
                        setMetaDirty(true);
                      }}
                      className="w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                    >
                      {HOOK_TYPES.map((opt) => (
                        <option key={opt.value} value={opt.value}>
                          {opt.label}
                        </option>
                      ))}
                    </select>
                  </div>

                  <div>
                    <label className="mb-1 block text-xs font-medium text-ink-secondary">
                      结尾留白说明
                    </label>
                    <input
                      type="text"
                      value={metaHookNote}
                      onChange={(e) => {
                        setMetaHookNote(e.target.value);
                        setMetaDirty(true);
                      }}
                      placeholder="例如：刚收完战利品，宗门追兵的血玉令牌亮起"
                      className="w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                    />
                  </div>
                </div>

                {/* Outline Summary */}
                <div className="mt-3">
                  <label className="mb-1 block text-xs font-medium text-ink-secondary">
                    本章梗概
                  </label>
                  <textarea
                    rows={2}
                    value={metaOutline}
                    onChange={(e) => {
                      setMetaOutline(e.target.value);
                      setMetaDirty(true);
                    }}
                    placeholder="本章主要剧情推进与结尾结果"
                    className="w-full resize-y rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                  />
                </div>
              </section>

              {/* Beat Cards Section */}
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
                          onClick={applyGenreBeatTemplates}
                          className="rounded bg-warn px-2 py-0.5 text-[11px] font-medium text-canvas"
                        >
                          确定
                        </button>
                        <button
                          type="button"
                          onClick={() => setConfirmFillBeats(false)}
                          className="text-ink-secondary hover:text-ink-primary"
                        >
                          <X className="h-3.5 w-3.5" />
                        </button>
                      </div>
                    ) : (
                      <button
                        type="button"
                        onClick={applyGenreBeatTemplates}
                        className="flex items-center gap-1.5 rounded-md border border-border-subtle bg-surface px-2.5 py-1.5 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary"
                      >
                        <Sparkles className="h-3.5 w-3.5 text-accent" />
                        <span>按题材填充默认节拍</span>
                      </button>
                    )}

                    <button
                      type="button"
                      onClick={addBeat}
                      className="flex items-center gap-1 rounded-md border border-border-subtle bg-surface px-2.5 py-1.5 text-xs text-ink-primary transition-colors hover:border-border-strong hover:bg-elevated"
                    >
                      <Plus className="h-3.5 w-3.5 text-accent" />
                      <span>添加节拍</span>
                    </button>

                    <button
                      type="button"
                      disabled={savingBeats || !beatsDirty}
                      onClick={() => void handleSaveBeats().then((ok) => ok && notify({ tone: "ok", message: "节拍卡已保存" }))}
                      className="flex items-center gap-1 rounded-md bg-accent-soft border border-accent/35 px-3 py-1.5 text-xs font-medium text-accent transition-colors hover:border-accent disabled:opacity-40"
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
                        onClick={applyGenreBeatTemplates}
                        className="flex items-center gap-1.5 rounded-md bg-accent-soft border border-accent/35 px-3 py-1.5 text-xs font-medium text-accent hover:border-accent"
                      >
                        <Sparkles className="h-3.5 w-3.5" />
                        <span>
                          按「{activeGenrePreset?.name ?? "当前题材"}」填充默认节拍
                        </span>
                      </button>
                      <button
                        type="button"
                        onClick={addBeat}
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
                                updateBeat(beat.localKey, { stage: e.target.value })
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
                                  updateBeat(beat.localKey, {
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
                                  updateBeat(beat.localKey, {
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
                                onClick={() => moveBeat(idx, -1)}
                                title="上移"
                                className="rounded p-1 text-ink-muted transition-colors hover:bg-elevated hover:text-ink-primary disabled:opacity-30"
                              >
                                <ArrowUp className="h-3.5 w-3.5" />
                              </button>
                              <button
                                type="button"
                                disabled={idx === beats.length - 1}
                                onClick={() => moveBeat(idx, 1)}
                                title="下移"
                                className="rounded p-1 text-ink-muted transition-colors hover:bg-elevated hover:text-ink-primary disabled:opacity-30"
                              >
                                <ArrowDown className="h-3.5 w-3.5" />
                              </button>
                              <button
                                type="button"
                                onClick={() => removeBeat(beat.localKey)}
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
                                updateBeat(beat.localKey, { summary: e.target.value })
                              }
                              placeholder="节拍推进摘要（具体行动、关键反转或结果落袋）"
                              className="w-full resize-none rounded-md border border-border-subtle bg-canvas px-3 py-2 text-[13px] leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                            />
                            <input
                              type="text"
                              value={beat.conflictNote}
                              onChange={(e) =>
                                updateBeat(beat.localKey, { conflictNote: e.target.value })
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
                                    onClick={() => toggleBeatEntity(beat.localKey, eid)}
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
                                  setOpenEntityPickerKey(pickerOpen ? null : beat.localKey)
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
                                      onClick={() =>
                                        toggleBeatEntity(beat.localKey, ent.id)
                                      }
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
            </div>
          </div>
        )}
      </main>

      {/* Right Panel: Serial Micro-Batch Scheduler (280px) */}
      {activeBook && (
        <BatchSchedulerPanel
          bookId={activeBook.id}
          volumeChapters={activeVolumeChapters}
          hasVolume={activeVolume !== null}
          onOpenOutlineModal={() => setOutlineModalOpen(true)}
          onJumpToChapterStudio={jumpToChapter}
          onBatchUpdated={async () => {
            await Promise.all([
              refreshChapters(activeBook.id),
              refreshForeshadowings(activeBook.id),
            ]);
            setPacingRefreshKey((k) => k + 1);
          }}
          onNotify={notify}
        />
      )}

      {/* Batch Outline Generation Modal */}
      {activeBook && (
        <BatchOutlineModal
          open={outlineModalOpen}
          bookId={activeBook.id}
          volume={activeVolume}
          defaultStartSeq={nextGlobalSeq}
          onClose={() => setOutlineModalOpen(false)}
          onGenerated={async (created) => {
            const firstId = created[0]?.id ?? activeChapterId;
            await refreshBookData(activeBook.id, firstId);
            setPacingRefreshKey((k) => k + 1);
          }}
          onNotify={notify}
        />
      )}

      {/* Create / Edit Volume Modal */}
      {volumeDialog && (
        <div
          className="fixed inset-0 z-40 flex items-center justify-center bg-black/55 p-4"
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) setVolumeDialog(null);
          }}
        >
          <div className="w-full max-w-[440px] overflow-hidden rounded-xl border border-border-strong bg-elevated shadow-[var(--shadow-elevated)]">
            <div className="flex items-center justify-between border-b border-border-subtle px-4 py-3">
              <h3 className="text-sm font-medium text-ink-primary">
                {volumeDialog.mode === "create" ? "新建卷" : "编辑卷"}
              </h3>
              <button
                type="button"
                onClick={() => setVolumeDialog(null)}
                className="rounded p-1 text-ink-muted hover:bg-surface hover:text-ink-primary"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="flex flex-col gap-3 p-4">
              <div>
                <label className="mb-1 block text-xs text-ink-secondary">卷名</label>
                <input
                  type="text"
                  autoFocus
                  value={volumeDialog.title}
                  onChange={(e) =>
                    setVolumeDialog({ ...volumeDialog, title: e.target.value })
                  }
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      e.preventDefault();
                      void handleSubmitVolume();
                    }
                  }}
                  placeholder="例如：第一卷 青云起"
                  className="w-full rounded-md border border-border-subtle bg-canvas px-3 py-2 text-sm text-ink-primary focus:border-accent focus:outline-none"
                />
              </div>
              <div>
                <label className="mb-1 block text-xs text-ink-secondary">
                  卷核心目标
                </label>
                <textarea
                  rows={2}
                  value={volumeDialog.goal}
                  onChange={(e) =>
                    setVolumeDialog({ ...volumeDialog, goal: e.target.value })
                  }
                  placeholder="本卷主角要达成的实力/地位/资源阶段目标"
                  className="w-full resize-none rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary focus:border-accent focus:outline-none"
                />
              </div>
              <div>
                <label className="mb-1 block text-xs text-ink-secondary">
                  卷末高潮规划
                </label>
                <textarea
                  rows={2}
                  value={volumeDialog.climaxPlan}
                  onChange={(e) =>
                    setVolumeDialog({ ...volumeDialog, climaxPlan: e.target.value })
                  }
                  placeholder="本卷收束大战、揭秘或换地图节点"
                  className="w-full resize-none rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary focus:border-accent focus:outline-none"
                />
              </div>
            </div>
            <div className="flex items-center justify-end gap-2 border-t border-border-subtle bg-surface/50 px-4 py-2.5">
              <button
                type="button"
                onClick={() => setVolumeDialog(null)}
                className="rounded-md px-3 py-1.5 text-xs text-ink-secondary hover:bg-surface hover:text-ink-primary"
              >
                取消
              </button>
              <button
                type="button"
                disabled={savingVolume}
                onClick={() => void handleSubmitVolume()}
                className="rounded-md bg-accent px-3.5 py-1.5 text-xs font-semibold text-canvas hover:opacity-90 disabled:opacity-50"
              >
                {savingVolume ? "保存中…" : "保存"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
