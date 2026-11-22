import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Plus, Sparkles } from "lucide-react";
import {
  LibraryService,
  type Beat,
  type Chapter,
  type Volume,
} from "../mygo";
import { useApp } from "../stores/AppContext";
import { VolumePacingChart } from "../components/architect/VolumePacingChart";
import { BatchOutlineModal } from "../components/architect/BatchOutlineModal";
import { ChapterTreeSidebar } from "../components/architect/ChapterTreeSidebar";
import { ChapterMetaForm } from "../components/architect/ChapterMetaForm";
import {
  BeatEditorList,
  type EditableBeat,
} from "../components/architect/BeatEditorList";
import {
  VolumeFormModal,
  type VolumeDialogState,
} from "../components/architect/VolumeFormModal";
import { ConfirmDialog, type ConfirmDialogState } from "../components/ui/ConfirmDialog";
import { formatErrorMessage, formatSeq } from "../lib/constants";

let beatKeyCounter = 1;
function nextBeatKey(): string {
  return `beat-${beatKeyCounter++}`;
}

function parseOptionalStoryDay(raw: string): { valid: boolean; value?: number } {
  const trimmed = raw.trim();
  if (!trimmed) return { valid: true, value: undefined };
  if (!/^-?\d+$/.test(trimmed)) return { valid: false };
  const n = Number.parseInt(trimmed, 10);
  if (!Number.isSafeInteger(n)) return { valid: false };
  return { valid: true, value: n };
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
    updateChapterInCache,
    setActiveTab,
    openAtlasSubTab,
    notify,
    registerSaveHandler,
  } = useApp();

  // Phase 4 outline modal & pacing refresh counter
  const [outlineModalOpen, setOutlineModalOpen] = useState(false);
  const [pacingRefreshKey, setPacingRefreshKey] = useState(0);

  // Tree & modal shared operation state
  const [emptyVolChapterTitle, setEmptyVolChapterTitle] = useState("");
  const [creatingChapter, setCreatingChapter] = useState(false);
  const [insertingChapter, setInsertingChapter] = useState(false);
  const [movingChapterId, setMovingChapterId] = useState<number | null>(null);
  const [batchMode, setBatchMode] = useState(false);
  const [selectedChapterIds, setSelectedChapterIds] = useState<Set<number>>(new Set());
  const [batchBusy, setBatchBusy] = useState(false);
  const [volumeDialog, setVolumeDialog] = useState<VolumeDialogState | null>(null);
  const [savingVolume, setSavingVolume] = useState(false);
  const [confirmDialog, setConfirmDialog] = useState<ConfirmDialogState | null>(null);

  // Chapter metadata draft
  const [metaTitle, setMetaTitle] = useState("");
  const [metaEmotion, setMetaEmotion] = useState("setup");
  const [metaHookType, setMetaHookType] = useState("mystery");
  const [metaHookNote, setMetaHookNote] = useState("");
  const [metaStoryTimeLabel, setMetaStoryTimeLabel] = useState("");
  const [metaStoryDay, setMetaStoryDay] = useState("");
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
    for (const e of entities) map.set(e.id, e);
    return map;
  }, [entities]);

  const chaptersByVolume = useMemo(() => {
    const map = new Map<number, Chapter[]>();
    for (const c of chapters) {
      const list = map.get(c.volumeId);
      if (list) list.push(c);
      else map.set(c.volumeId, [c]);
    }
    return map;
  }, [chapters]);

  const activeVolume = useMemo(() => {
    if (!activeChapter) return volumes[0] ?? null;
    return volumes.find((v) => v.id === activeChapter.volumeId) ?? null;
  }, [volumes, activeChapter]);

  const nextGlobalSeq = useMemo(
    () => chapters.reduce((m, c) => (c.globalSeq > m ? c.globalSeq : m), 0) + 1,
    [chapters]
  );

  // Sync metadata form when active chapter changes, flushing dirty meta/beats on switch or unmount
  const metaDraftRef = useRef({
    title: metaTitle,
    emotion: metaEmotion,
    hookType: metaHookType,
    hookNote: metaHookNote,
    storyTimeLabel: metaStoryTimeLabel,
    storyDay: metaStoryDay,
    outline: metaOutline,
    causal: metaCausal,
  });
  metaDraftRef.current = {
    title: metaTitle,
    emotion: metaEmotion,
    hookType: metaHookType,
    hookNote: metaHookNote,
    storyTimeLabel: metaStoryTimeLabel,
    storyDay: metaStoryDay,
    outline: metaOutline,
    causal: metaCausal,
  };
  const metaDirtyRef = useRef(metaDirty);
  metaDirtyRef.current = metaDirty;
  const beatsDraftRef = useRef(beats);
  beatsDraftRef.current = beats;
  const beatsDirtyRef = useRef(beatsDirty);
  beatsDirtyRef.current = beatsDirty;

  useEffect(() => {
    if (!activeChapter) {
      setMetaTitle("");
      setMetaEmotion("setup");
      setMetaHookType("mystery");
      setMetaHookNote("");
      setMetaStoryTimeLabel("");
      setMetaStoryDay("");
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
    setMetaStoryTimeLabel(activeChapter.storyTimeLabel || "");
    setMetaStoryDay(
      activeChapter.storyDay !== undefined && activeChapter.storyDay !== null
        ? String(activeChapter.storyDay)
        : ""
    );
    setMetaOutline(activeChapter.outlineSummary || "");
    setMetaCausal(activeChapter.causalSummary || "");
    setMetaDirty(false);
    setConfirmFillBeats(false);
    setOpenEntityPickerKey(null);
  }, [activeChapter?.id, activeChapter?.updatedAt]);

  useEffect(() => {
    const chap = activeChapter;
    return () => {
      if (!chap) return;
      if (metaDirtyRef.current) {
        const d = metaDraftRef.current;
        const trimmedTitle = d.title.trim() || chap.title;
        const parsedDay = parseOptionalStoryDay(d.storyDay);
        void LibraryService.updateChapterMeta({
          id: chap.id,
          title: trimmedTitle,
          status: chap.status,
          emotionTag: d.emotion,
          hookType: d.hookType,
          hookNote: d.hookNote.trim(),
          outlineSummary: d.outline.trim(),
          causalSummary: d.causal.trim(),
          storyTimeLabel: d.storyTimeLabel.trim(),
          storyDay: parsedDay.valid ? parsedDay.value : chap.storyDay,
          updateStoryTimeline: true,
        })
          .then((updated) => updateChapterInCache(updated))
          .catch(() => {
            // ignore background flush failure
          });
      }
      if (beatsDirtyRef.current) {
        const curBeats = beatsDraftRef.current;
        if (curBeats.every((b) => b.summary.trim().length > 0)) {
          const payload: Beat[] = curBeats.map((b, idx) => ({
            id: b.id,
            chapterId: chap.id,
            seq: idx + 1,
            stage: b.stage,
            summary: b.summary.trim(),
            conflictNote: b.conflictNote.trim(),
            povEntityId: b.povEntityId && b.povEntityId > 0 ? b.povEntityId : undefined,
            entityIds: b.entityIds,
            targetWords: b.targetWords > 0 ? b.targetWords : 500,
          }));
          void LibraryService.replaceBeats(chap.id, payload).catch(() => {
            // ignore background flush failure
          });
        }
      }
    };
  }, [activeChapter?.id, updateChapterInCache]);

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

  const handleSaveMeta = useCallback(async (): Promise<boolean> => {
    if (!activeChapter) return true;
    const trimmedTitle = metaTitle.trim();
    if (!trimmedTitle) {
      notify({ tone: "warn", message: "章节标题不能为空" });
      return false;
    }
    const parsedDay = parseOptionalStoryDay(metaStoryDay);
    if (!parsedDay.valid) {
      notify({ tone: "warn", message: "故事日序号必须为整数（如 10）或留空" });
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
        storyTimeLabel: metaStoryTimeLabel.trim(),
        storyDay: parsedDay.value,
        updateStoryTimeline: true,
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
    metaStoryTimeLabel,
    metaStoryDay,
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
      const nextPreferred = activeChapterId === chapId ? null : activeChapterId;
      await refreshBookData(activeBook.id, nextPreferred);
      setPacingRefreshKey((k) => k + 1);
      notify({ tone: "ok", message: "已删除章节并重排章序" });
    } catch (err) {
      notify({ tone: "danger", message: "删除章节失败", detail: formatErrorMessage(err) });
    }
  };

  const handleMoveChapterToVolume = async (
    chapId: number,
    targetVolumeId: number,
    targetVolSeq = 0
  ) => {
    if (!activeBook || movingChapterId !== null) return;
    setMovingChapterId(chapId);
    try {
      await LibraryService.moveChapter({ chapterId: chapId, targetVolumeId, targetVolSeq });
      await refreshBookData(activeBook.id, activeChapterId);
      setPacingRefreshKey((k) => k + 1);
    } catch (err) {
      notify({ tone: "danger", message: "移动章节失败", detail: formatErrorMessage(err) });
    } finally {
      setMovingChapterId(null);
    }
  };

  const toggleChapterSelection = (chapId: number) => {
    setSelectedChapterIds((prev) => {
      const next = new Set(prev);
      if (next.has(chapId)) next.delete(chapId);
      else next.add(chapId);
      return next;
    });
  };

  const handleBatchFinalize = async () => {
    if (!activeBook || selectedChapterIds.size === 0) return;
    setBatchBusy(true);
    try {
      const count = await LibraryService.batchFinalizeChapters(Array.from(selectedChapterIds));
      await refreshBookData(activeBook.id, activeChapterId);
      setSelectedChapterIds(new Set());
      notify({ tone: "ok", message: `已定稿 ${count} 章` });
    } catch (err) {
      notify({ tone: "danger", message: "批量定稿失败", detail: formatErrorMessage(err) });
    } finally {
      setBatchBusy(false);
    }
  };

  const handleBatchDelete = () => {
    if (!activeBook || selectedChapterIds.size === 0) return;
    const ids = Array.from(selectedChapterIds);
    setConfirmDialog({
      title: "批量删除章节",
      message: `确认删除选中的 ${ids.length} 章？删除后可在设置里的回收站恢复。`,
      confirmLabel: "确认删除",
      danger: true,
      onConfirm: async () => {
        setBatchBusy(true);
        try {
          const count = await LibraryService.batchDeleteChapters(ids);
          setConfirmDialog(null);
          setSelectedChapterIds(new Set());
          const nextPreferred = ids.includes(activeChapterId ?? -1) ? null : activeChapterId;
          await refreshBookData(activeBook.id, nextPreferred);
          setPacingRefreshKey((k) => k + 1);
          notify({ tone: "ok", message: `已删除 ${count} 章，可在回收站恢复` });
        } catch (err) {
          notify({ tone: "danger", message: "批量删除失败", detail: formatErrorMessage(err) });
        } finally {
          setBatchBusy(false);
        }
      },
    });
  };

  const handleBatchExport = async () => {
    if (!activeBook || selectedChapterIds.size === 0) return;
    setBatchBusy(true);
    try {
      const outPath = await LibraryService.exportChaptersTXT(
        activeBook.id,
        Array.from(selectedChapterIds)
      );
      if (outPath) {
        notify({ tone: "ok", message: `已导出 ${selectedChapterIds.size} 章`, detail: outPath });
      }
    } catch (err) {
      notify({ tone: "danger", message: "导出选中章节失败", detail: formatErrorMessage(err) });
    } finally {
      setBatchBusy(false);
    }
  };

  const handleInsertChapterAfter = async (
    volId: number,
    afterChapId: number,
    title: string
  ): Promise<boolean> => {
    if (!activeBook || insertingChapter) return false;
    const trimmed = title.trim();
    if (!trimmed) {
      notify({ tone: "warn", message: "请输入章节标题" });
      return false;
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
      await refreshBookData(activeBook.id, created.id);
      setPacingRefreshKey((k) => k + 1);
      notify({ tone: "ok", message: `已插入第 ${formatSeq(created.globalSeq)} 章` });
      return true;
    } catch (err) {
      notify({ tone: "danger", message: "插入章节失败", detail: formatErrorMessage(err) });
      return false;
    } finally {
      setInsertingChapter(false);
    }
  };

  const handleCreateChapter = async (
    volumeId: number,
    title: string
  ): Promise<boolean> => {
    if (!activeBook) return false;
    const trimmed = title.trim();
    if (!trimmed) return false;
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
      await refreshBookData(activeBook.id, created.id);
      return true;
    } catch (err) {
      notify({ tone: "danger", message: "新建章节失败", detail: formatErrorMessage(err) });
      return false;
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
    setBeats((prev) => prev.map((b) => (b.localKey === localKey ? { ...b, ...patch } : b)));
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

  return (
    <div className="flex min-h-0 flex-1 overflow-hidden">
      <ChapterTreeSidebar
        volumes={volumes}
        chapters={chapters}
        chaptersByVolume={chaptersByVolume}
        activeVolume={activeVolume}
        activeChapterId={activeChapterId}
        movingChapterId={movingChapterId}
        batchMode={batchMode}
        selectedChapterIds={selectedChapterIds}
        batchBusy={batchBusy}
        creatingChapter={creatingChapter}
        insertingChapter={insertingChapter}
        onToggleBatchMode={() => {
          setBatchMode((value) => !value);
          setSelectedChapterIds(new Set());
        }}
        onToggleChapterSelection={toggleChapterSelection}
        onSelectAllChapters={() => setSelectedChapterIds(new Set(chapters.map((c) => c.id)))}
        onClearSelectedChapters={() => setSelectedChapterIds(new Set())}
        onBatchFinalize={() => void handleBatchFinalize()}
        onBatchExport={() => void handleBatchExport()}
        onBatchDelete={handleBatchDelete}
        onOpenOutlineModal={() => setOutlineModalOpen(true)}
        onOpenCreateVolume={openCreateVolume}
        onOpenEditVolume={openEditVolume}
        onDeleteVolume={(volId) => void handleDeleteVolume(volId)}
        onSelectChapter={(chapId) => void selectChapter(chapId)}
        onJumpToChapter={(chapId) => void jumpToChapter(chapId)}
        onCreateChapter={handleCreateChapter}
        onInsertChapterAfter={handleInsertChapterAfter}
        onMoveChapter={(chapId, dir) => void handleMoveChapter(chapId, dir)}
        onMoveChapterToVolume={(chapId, targetVolId, targetSeq) =>
          void handleMoveChapterToVolume(chapId, targetVolId, targetSeq)
        }
        onDeleteChapter={(chapId) => void handleDeleteChapter(chapId)}
        notify={notify}
      />

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
                  {activeVolume
                    ? `「${activeVolume.title}」还没有章节`
                    : "选择或新建一章开始写大纲"}
                </div>
                {activeVolume && (
                  <form
                    onSubmit={async (e) => {
                      e.preventDefault();
                      const ok = await handleCreateChapter(
                        activeVolume.id,
                        emptyVolChapterTitle
                      );
                      if (ok) setEmptyVolChapterTitle("");
                    }}
                    className="flex w-full items-center gap-2"
                  >
                    <input
                      type="text"
                      value={emptyVolChapterTitle}
                      disabled={creatingChapter}
                      onChange={(e) => setEmptyVolChapterTitle(e.target.value)}
                      placeholder="输入第一章标题，回车新建"
                      className="min-w-0 flex-1 rounded-lg border border-border-subtle bg-surface px-3 py-2 text-[13px] text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                    />
                    <button
                      type="submit"
                      disabled={creatingChapter || !emptyVolChapterTitle.trim()}
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
              {activeVolume && (
                <VolumePacingChart
                  volume={activeVolume}
                  activeChapterId={activeChapter.id}
                  refreshTrigger={pacingRefreshKey + chapters.length}
                  onSelectChapter={(cid) => void selectChapter(cid)}
                  onOpenForeshadowings={() => openAtlasSubTab("foreshadowings")}
                />
              )}

              <ChapterMetaForm
                chapter={activeChapter}
                title={metaTitle}
                emotion={metaEmotion}
                hookType={metaHookType}
                hookNote={metaHookNote}
                storyTimeLabel={metaStoryTimeLabel}
                storyDay={metaStoryDay}
                outline={metaOutline}
                dirty={metaDirty || beatsDirty}
                saving={savingMeta || savingBeats}
                onChangeTitle={(v) => {
                  setMetaTitle(v);
                  setMetaDirty(true);
                }}
                onChangeEmotion={(v) => {
                  setMetaEmotion(v);
                  setMetaDirty(true);
                }}
                onChangeHookType={(v) => {
                  setMetaHookType(v);
                  setMetaDirty(true);
                }}
                onChangeHookNote={(v) => {
                  setMetaHookNote(v);
                  setMetaDirty(true);
                }}
                onChangeStoryTimeLabel={(v) => {
                  setMetaStoryTimeLabel(v);
                  setMetaDirty(true);
                }}
                onChangeStoryDay={(v) => {
                  setMetaStoryDay(v);
                  setMetaDirty(true);
                }}
                onChangeOutline={(v) => {
                  setMetaOutline(v);
                  setMetaDirty(true);
                }}
                onSaveAll={() => void handleSaveAll()}
                onStartWriting={async () => {
                  if (metaDirty || beatsDirty) {
                    const ok = await handleSaveAll();
                    if (!ok) return;
                  }
                  setActiveTab("studio");
                }}
              />

              <BeatEditorList
                beatListRef={beatListRef}
                beats={beats}
                beatsLoading={beatsLoading}
                beatsDirty={beatsDirty}
                savingBeats={savingBeats}
                totalBeatWords={totalBeatWords}
                confirmFillBeats={confirmFillBeats}
                activeGenreName={activeGenrePreset?.name ?? "当前题材"}
                entities={entities}
                entityMap={entityMap}
                openEntityPickerKey={openEntityPickerKey}
                onSetOpenEntityPickerKey={setOpenEntityPickerKey}
                onSetConfirmFillBeats={setConfirmFillBeats}
                onApplyGenreBeatTemplates={applyGenreBeatTemplates}
                onAddBeat={addBeat}
                onSaveBeats={() =>
                  void handleSaveBeats().then(
                    (ok) => ok && notify({ tone: "ok", message: "节拍卡已保存" })
                  )
                }
                onUpdateBeat={updateBeat}
                onMoveBeat={moveBeat}
                onRemoveBeat={removeBeat}
                onToggleBeatEntity={toggleBeatEntity}
              />
            </div>
          </div>
        )}
      </main>

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

      <VolumeFormModal
        dialog={volumeDialog}
        saving={savingVolume}
        onChange={setVolumeDialog}
        onClose={() => setVolumeDialog(null)}
        onSubmit={() => void handleSubmitVolume()}
      />

      <ConfirmDialog
        dialog={confirmDialog}
        busy={batchBusy}
        onClose={() => setConfirmDialog(null)}
      />
    </div>
  );
}
