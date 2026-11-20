import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Channel } from "mygo-runtime";
import {
  AlertCircle,
  AlertTriangle,
  Ban,
  BookOpen,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Download,
  FileText,
  History,
  Loader2,
  PanelRight,
  Pencil,
  Pin,
  Play,
  RefreshCw,
  RotateCcw,
  Save,
  Square,
  X,
} from "lucide-react";
import {
  LibraryService,
  StudioService,
  type Briefing,
  type ChapterVersion,
  type GenChunk,
  type TokenUsage,
} from "../mygo";
import { useApp } from "../stores/AppContext";
import {
  countWords,
  formatErrorMessage,
  formatNumber,
  formatSeq,
  formatTimestamp,
  getChapterStatusMeta,
  getEntityKindLabel,
  getVersionSourceLabel,
} from "../lib/constants";

type DrawerTab = "briefing" | "versions";

export function StudioView() {
  const {
    activeBook,
    volumes,
    chapters,
    activeChapter,
    activeChapterId,
    entities,
    providers,
    modelRoutes,
    proseFont,
    proseIndent,
    studioDrawerOpen,
    toggleStudioDrawer,
    selectChapter,
    refreshActiveChapter,
    updateChapterInCache,
    setActiveTab,
    openSettings,
    notify,
    registerSaveHandler,
    registerGenerateHandler,
  } = useApp();

  // Editor state
  const [title, setTitle] = useState("");
  const [content, setContent] = useState("");
  const [dirty, setDirty] = useState(false);
  const [readingMode, setReadingMode] = useState(false);
  const [saving, setSaving] = useState(false);
  const [finalizing, setFinalizing] = useState(false);
  const [exporting, setExporting] = useState(false);

  // Generation streaming state
  const [generating, setGenerating] = useState(false);
  const [genStageText, setGenStageText] = useState("");
  const [genModelName, setGenModelName] = useState("");
  const [genError, setGenError] = useState<string | null>(null);
  const [lastUsage, setLastUsage] = useState<TokenUsage | null>(null);

  const abortRef = useRef<AbortController | null>(null);
  const channelRef = useRef<Channel<GenChunk> | null>(null);
  const stopFallbackTimerRef = useRef<number | null>(null);
  const userStoppedRef = useRef(false);
  const mountedRef = useRef(true);
  const activeChapterIdRef = useRef<number | null>(activeChapterId);
  const streamRunIdRef = useRef(0);
  const streamTextRef = useRef("");
  const receivedDeltaRef = useRef(false);
  const canvasScrollRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  activeChapterIdRef.current = activeChapterId;

  // Drawer state
  const [drawerTab, setDrawerTab] = useState<DrawerTab>("briefing");
  const [briefing, setBriefing] = useState<Briefing | null>(null);
  const [briefingLoading, setBriefingLoading] = useState(false);
  const [briefingError, setBriefingError] = useState<string | null>(null);
  const [expandedSlots, setExpandedSlots] = useState<Set<string>>(new Set());
  const [pinnedEntityIds, setPinnedEntityIds] = useState<Set<number>>(new Set());
  const [excludedEntityIds, setExcludedEntityIds] = useState<Set<number>>(new Set());
  const [extraInstruction, setExtraInstruction] = useState("");
  const [entityControlsOpen, setEntityControlsOpen] = useState(false);

  // Versions state
  const [versions, setVersions] = useState<ChapterVersion[]>([]);
  const [versionsLoading, setVersionsLoading] = useState(false);
  const [selectedVersionId, setSelectedVersionId] = useState<number | null>(null);
  const [confirmRestoreId, setConfirmRestoreId] = useState<number | null>(null);
  const [restoringId, setRestoringId] = useState<number | null>(null);

  // Sync editor content and per-chapter briefing overrides when active chapter switches
  useEffect(() => {
    // Cancel any in-flight stream for a previous chapter so stale chunks cannot overwrite this chapter
    if (channelRef.current || abortRef.current) {
      streamRunIdRef.current += 1;
      userStoppedRef.current = true;
      if (stopFallbackTimerRef.current !== null) {
        window.clearTimeout(stopFallbackTimerRef.current);
        stopFallbackTimerRef.current = null;
      }
      if (channelRef.current) {
        try {
          channelRef.current.close();
        } catch {
          // ignore
        }
        channelRef.current = null;
      }
      if (abortRef.current) {
        abortRef.current.abort();
        abortRef.current = null;
      }
      setGenerating(false);
      setGenStageText("");
    }

    if (!activeChapter) {
      setTitle("");
      setContent("");
      setDirty(false);
      setGenError(null);
      setLastUsage(null);
      return;
    }
    setTitle(activeChapter.title);
    setContent(activeChapter.content);
    setDirty(false);
    setReadingMode(false);
    setGenError(null);
    setConfirmRestoreId(null);
    setPinnedEntityIds(new Set());
    setExcludedEntityIds(new Set());
    setExtraInstruction("");
    setExpandedSlots(new Set());
  }, [activeChapter?.id]);

  // Keep synced if chapter updated externally while not editing or streaming
  useEffect(() => {
    if (!activeChapter || dirty || generating) return;
    setTitle(activeChapter.title);
    setContent(activeChapter.content);
  }, [activeChapter?.updatedAt, activeChapter?.content, activeChapter?.title, dirty, generating]);

  // Auto-resize textarea height cleanly after content/mode/layout/typography changes
  useLayoutEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    const resize = () => {
      el.style.height = "auto";
      el.style.height = `${Math.max(el.scrollHeight, 480)}px`;
    };
    resize();
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => resize());
    ro.observe(el);
    return () => ro.disconnect();
  }, [content, activeChapter?.id, generating, readingMode, studioDrawerOpen, proseFont, proseIndent]);

  // Abort active generation if unmounting StudioView
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      streamRunIdRef.current += 1;
      if (stopFallbackTimerRef.current !== null) {
        window.clearTimeout(stopFallbackTimerRef.current);
        stopFallbackTimerRef.current = null;
      }
      if (channelRef.current) {
        try {
          channelRef.current.close();
        } catch {
          // ignore
        }
        channelRef.current = null;
      }
      if (abortRef.current) {
        userStoppedRef.current = true;
        abortRef.current.abort();
        abortRef.current = null;
      }
    };
  }, []);

  // Draft route display info
  const draftRouteInfo = useMemo(() => {
    const route = modelRoutes.find((r) => r.stage === "draft");
    if (!route || !route.providerId || !route.modelName) {
      return { configured: false, label: "未配置正文模型" };
    }
    const prov = providers.find((p) => p.id === route.providerId);
    const label = prov ? `${route.modelName}` : route.modelName;
    return { configured: true, label, providerName: prov?.name };
  }, [modelRoutes, providers]);

  const currentChapterIndex = useMemo(
    () => chapters.findIndex((c) => c.id === activeChapterId),
    [chapters, activeChapterId]
  );

  const activeVolumeTitle = useMemo(() => {
    if (!activeChapter) return "";
    return volumes.find((v) => v.id === activeChapter.volumeId)?.title ?? "";
  }, [volumes, activeChapter]);

  // Load briefing
  const loadBriefing = useCallback(async () => {
    if (!activeChapterId) {
      setBriefing(null);
      return;
    }
    setBriefingLoading(true);
    setBriefingError(null);
    try {
      const res = await StudioService.previewBriefing(activeChapterId, {
        pinnedEntityIds: Array.from(pinnedEntityIds),
        excludedEntityIds: Array.from(excludedEntityIds),
        extraInstruction: extraInstruction.trim() || undefined,
      });
      setBriefing(res);
    } catch (err) {
      setBriefingError(formatErrorMessage(err));
    } finally {
      setBriefingLoading(false);
    }
  }, [activeChapterId, pinnedEntityIds, excludedEntityIds, extraInstruction]);

  // Load versions
  const loadVersions = useCallback(async () => {
    if (!activeChapterId) {
      setVersions([]);
      return;
    }
    setVersionsLoading(true);
    try {
      const list = await StudioService.listChapterVersions(activeChapterId);
      setVersions(list);
      setSelectedVersionId((prev) =>
        prev && list.some((v) => v.id === prev) ? prev : list[0]?.id ?? null
      );
    } catch (err) {
      notify({ tone: "danger", message: "读取历史快照失败", detail: formatErrorMessage(err) });
    } finally {
      setVersionsLoading(false);
    }
  }, [activeChapterId, notify]);

  // Debounced auto-load briefing when drawer is open on briefing tab
  useEffect(() => {
    if (!studioDrawerOpen || drawerTab !== "briefing" || !activeChapterId) return;
    const timer = window.setTimeout(() => {
      void loadBriefing();
    }, 220);
    return () => window.clearTimeout(timer);
  }, [studioDrawerOpen, drawerTab, activeChapterId, loadBriefing]);

  // Load versions when drawer opens on versions tab or chapter changes
  useEffect(() => {
    if (!studioDrawerOpen || drawerTab !== "versions" || !activeChapterId) return;
    void loadVersions();
  }, [studioDrawerOpen, drawerTab, activeChapterId, loadVersions]);

  // Save draft
  const handleSave = useCallback(async (): Promise<boolean> => {
    if (!activeChapter || generating) return false;
    setSaving(true);
    try {
      const saved = await LibraryService.saveChapterDraft({
        chapterId: activeChapter.id,
        title: title.trim() || activeChapter.title,
        content,
        backupSource: "manual_backup",
      });
      updateChapterInCache(saved);
      setDirty(false);
      if (studioDrawerOpen && drawerTab === "versions") {
        void loadVersions();
      }
      notify({ tone: "ok", message: "正文已保存" });
      return true;
    } catch (err) {
      notify({ tone: "danger", message: "保存正文失败", detail: formatErrorMessage(err) });
      return false;
    } finally {
      setSaving(false);
    }
  }, [
    activeChapter,
    generating,
    title,
    content,
    updateChapterInCache,
    studioDrawerOpen,
    drawerTab,
    loadVersions,
    notify,
  ]);

  // Finalize chapter
  const handleFinalize = useCallback(async () => {
    if (!activeChapter || generating) return;
    setFinalizing(true);
    try {
      if (dirty) {
        const saved = await LibraryService.saveChapterDraft({
          chapterId: activeChapter.id,
          title: title.trim() || activeChapter.title,
          content,
          backupSource: "manual_backup",
        });
        updateChapterInCache(saved);
        setDirty(false);
      }
      const finalized = await LibraryService.finalizeChapter(activeChapter.id);
      updateChapterInCache(finalized);
      notify({
        tone: "ok",
        message: `第 ${formatSeq(finalized.globalSeq)} 章已定稿`,
        detail: activeBook?.exportDir ? "已同步更新本地 Markdown 副本" : undefined,
      });
    } catch (err) {
      notify({ tone: "danger", message: "定稿失败", detail: formatErrorMessage(err) });
    } finally {
      setFinalizing(false);
    }
  }, [
    activeChapter,
    generating,
    dirty,
    title,
    content,
    updateChapterInCache,
    activeBook?.exportDir,
    notify,
  ]);

  // Export whole book Markdown
  const handleExportBook = useCallback(async () => {
    if (!activeBook) return;
    if (!activeBook.exportDir?.trim()) {
      notify({
        tone: "warn",
        message: "尚未设置本地 Markdown 导出目录",
        actionLabel: "前往设置",
        onAction: () => openSettings("workspace"),
      });
      return;
    }
    setExporting(true);
    try {
      const result = await LibraryService.exportBookMarkdown(activeBook.id);
      notify({
        tone: "ok",
        message: `已导出全书（${result.chapterCount} 章）`,
        detail: result.bookDir || result.exportDir,
      });
    } catch (err) {
      notify({
        tone: "danger",
        message: "导出全书失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setExporting(false);
    }
  }, [activeBook, notify, openSettings]);

  // Stop generation: close channel first so Go cancels ctx AND commits savePartialDetached
  // before the RPC promise settles; reserve AbortController.abort() as an 8s safety fallback.
  const stopGeneration = useCallback(() => {
    userStoppedRef.current = true;
    setGenStageText("正在停止并保存已生成内容…");
    if (channelRef.current) {
      try {
        channelRef.current.close();
      } catch {
        // ignore
      }
    }
    if (stopFallbackTimerRef.current !== null) {
      window.clearTimeout(stopFallbackTimerRef.current);
    }
    stopFallbackTimerRef.current = window.setTimeout(() => {
      stopFallbackTimerRef.current = null;
      if (abortRef.current) {
        abortRef.current.abort();
      }
    }, 8000);
  }, []);

  // Start generation
  const startGeneration = useCallback(async () => {
    if (!activeChapter || generating) return;

    const genChapterId = activeChapter.id;
    const runId = ++streamRunIdRef.current;
    const ac = new AbortController();
    abortRef.current = ac;
    userStoppedRef.current = false;
    streamTextRef.current = "";
    receivedDeltaRef.current = false;

    setGenerating(true);
    setGenError(null);
    setGenStageText("正在编译作战简报…");
    setGenModelName(draftRouteInfo.configured ? draftRouteInfo.label : "");
    setLastUsage(null);

    const ch = new Channel<GenChunk>((chunk) => {
      if (
        !mountedRef.current ||
        streamRunIdRef.current !== runId ||
        activeChapterIdRef.current !== genChapterId
      ) {
        return;
      }
      switch (chunk.kind) {
        case "started": {
          const model = chunk.modelName || draftRouteInfo.label;
          setGenModelName(model);
          setGenStageText(`流式出稿中 · ${model}`);
          break;
        }
        case "delta": {
          if (chunk.delta) {
            if (!receivedDeltaRef.current) {
              receivedDeltaRef.current = true;
              streamTextRef.current = chunk.delta;
            } else {
              streamTextRef.current += chunk.delta;
            }
            setContent(streamTextRef.current);
            setDirty(false);
            // Smooth follow bottom if user is near bottom
            const scroller = canvasScrollRef.current;
            if (scroller) {
              const dist =
                scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight;
              if (dist < 180) {
                requestAnimationFrame(() => {
                  scroller.scrollTop = scroller.scrollHeight;
                });
              }
            }
          }
          break;
        }
        case "usage": {
          if (chunk.usage) setLastUsage(chunk.usage);
          break;
        }
        case "completed": {
          if (chunk.usage) setLastUsage(chunk.usage);
          if (chunk.modelName) setGenModelName(chunk.modelName);
          if (chunk.error) {
            notify({ tone: "warn", message: chunk.error });
          }
          setGenStageText("");
          break;
        }
        case "partial": {
          setGenStageText("");
          break;
        }
        case "error": {
          const msg = chunk.error ? formatErrorMessage(chunk.error) : "生成过程发生错误";
          if (chunk.retryable && chunk.retryAttempt) {
            setGenStageText(`${msg}（重试第 ${chunk.retryAttempt} 次…）`);
          } else {
            setGenError(msg);
          }
          break;
        }
        default:
          break;
      }
    });
    channelRef.current = ch;

    const trimmedExtra = extraInstruction.trim();
    const opts = {
      briefing: {
        pinnedEntityIds: Array.from(pinnedEntityIds),
        excludedEntityIds: Array.from(excludedEntityIds),
        extraInstruction: trimmedExtra || undefined,
      },
      extraInstruction: trimmedExtra || undefined,
    };

    try {
      const updated = await StudioService.generateChapter.with({
        signal: ac.signal,
      })(genChapterId, opts, ch);

      if (!mountedRef.current || streamRunIdRef.current !== runId) {
        return;
      }

      updateChapterInCache(updated);
      if (activeChapterIdRef.current === genChapterId) {
        setTitle(updated.title);
        setContent(updated.content);
        setDirty(false);
      }

      if (updated.status === "partial") {
        notify({
          tone: "warn",
          message: `已停在 ${formatNumber(updated.wordCount)} 字`,
          detail: "已保留已生成文本并归档快照，可继续编辑或重新生成",
        });
      } else {
        notify({
          tone: "ok",
          message: `本章生成完成（${formatNumber(updated.wordCount)} 字）`,
        });
      }
    } catch (err) {
      if (!mountedRef.current || streamRunIdRef.current !== runId) {
        return;
      }

      const isSameChapter = activeChapterIdRef.current === genChapterId;

      if (userStoppedRef.current || ac.signal.aborted) {
        // Go finished savePartialDetached before returning ErrGenerationCanceled; reload safely.
        if (isSameChapter) {
          const fresh = await refreshActiveChapter(genChapterId);
          if (
            fresh &&
            mountedRef.current &&
            streamRunIdRef.current === runId &&
            activeChapterIdRef.current === genChapterId
          ) {
            setTitle(fresh.title);
            setContent(fresh.content);
            setDirty(false);
          }
        }
      } else {
        if (receivedDeltaRef.current && isSameChapter) {
          const fresh = await refreshActiveChapter(genChapterId);
          if (
            fresh &&
            mountedRef.current &&
            streamRunIdRef.current === runId &&
            activeChapterIdRef.current === genChapterId
          ) {
            setTitle(fresh.title);
            setContent(fresh.content);
            setDirty(false);
          }
        }
        const msg = formatErrorMessage(err);
        if (isSameChapter) {
          setGenError(msg);
        }
        notify({
          tone: "danger",
          message: "生成失败",
          detail: msg,
          actionLabel: msg.includes("设置") ? "打开设置" : undefined,
          onAction: msg.includes("设置")
            ? () => openSettings(msg.includes("路由") ? "routes" : "providers")
            : undefined,
        });
      }
    } finally {
      if (stopFallbackTimerRef.current !== null) {
        window.clearTimeout(stopFallbackTimerRef.current);
        stopFallbackTimerRef.current = null;
      }
      try {
        if (!ch.closed) ch.close();
      } catch {
        // ignore
      }
      if (abortRef.current === ac) abortRef.current = null;
      if (channelRef.current === ch) channelRef.current = null;
      if (mountedRef.current && streamRunIdRef.current === runId) {
        setGenerating(false);
        setGenStageText("");
        if (activeChapterIdRef.current === genChapterId) {
          if (studioDrawerOpen && drawerTab === "versions") {
            void loadVersions();
          }
          if (studioDrawerOpen && drawerTab === "briefing") {
            void loadBriefing();
          }
        }
      }
    }
  }, [
    activeChapter,
    generating,
    draftRouteInfo,
    extraInstruction,
    pinnedEntityIds,
    excludedEntityIds,
    updateChapterInCache,
    refreshActiveChapter,
    notify,
    openSettings,
    studioDrawerOpen,
    drawerTab,
    loadVersions,
    loadBriefing,
  ]);

  const toggleGenerate = useCallback(() => {
    if (generating) {
      stopGeneration();
    } else {
      void startGeneration();
    }
  }, [generating, stopGeneration, startGeneration]);

  // Register global Ctrl+S & Ctrl+Enter shortcuts
  useEffect(() => {
    registerSaveHandler(async () => {
      await handleSave();
    });
    registerGenerateHandler(toggleGenerate);
    return () => {
      registerSaveHandler(null);
      registerGenerateHandler(null);
    };
  }, [registerSaveHandler, registerGenerateHandler, handleSave, toggleGenerate]);

  // Restore snapshot version
  const handleRestoreVersion = async (versionId: number) => {
    if (generating) return;
    setRestoringId(versionId);
    try {
      const restored = await StudioService.restoreVersion(versionId);
      updateChapterInCache(restored);
      setTitle(restored.title);
      setContent(restored.content);
      setDirty(false);
      setConfirmRestoreId(null);
      await loadVersions();
      notify({
        tone: "ok",
        message: "已回滚到此版本快照",
        detail: "原正文已自动归档至历史快照",
      });
    } catch (err) {
      notify({ tone: "danger", message: "回滚快照失败", detail: formatErrorMessage(err) });
    } finally {
      setRestoringId(null);
    }
  };

  // Pin / Exclude cycle helpers
  const togglePinEntity = (entityId: number) => {
    setPinnedEntityIds((prev) => {
      const next = new Set(prev);
      if (next.has(entityId)) next.delete(entityId);
      else next.add(entityId);
      return next;
    });
    setExcludedEntityIds((prev) => {
      if (!prev.has(entityId)) return prev;
      const next = new Set(prev);
      next.delete(entityId);
      return next;
    });
  };

  const toggleExcludeEntity = (entityId: number) => {
    setExcludedEntityIds((prev) => {
      const next = new Set(prev);
      if (next.has(entityId)) next.delete(entityId);
      else next.add(entityId);
      return next;
    });
    setPinnedEntityIds((prev) => {
      if (!prev.has(entityId)) return prev;
      const next = new Set(prev);
      next.delete(entityId);
      return next;
    });
  };

  const toggleSlotExpand = (key: string) => {
    setExpandedSlots((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  const liveWordCount = useMemo(() => countWords(content), [content]);
  const streamingParagraphs = useMemo(
    () =>
      content
        .split(/\n+/)
        .map((line) => line.trim())
        .filter((line) => line.length > 0),
    [content]
  );

  const selectedVersion = useMemo(
    () => versions.find((v) => v.id === selectedVersionId) ?? null,
    [versions, selectedVersionId]
  );

  if (!activeChapter) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-3 bg-canvas px-6 text-center">
        <div className="text-sm text-ink-secondary">当前书籍尚无章节</div>
        <button
          type="button"
          onClick={() => setActiveTab("architect")}
          className="rounded-md bg-accent px-4 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90"
        >
          前往编排台新建章节
        </button>
      </div>
    );
  }

  const statusMeta = getChapterStatusMeta(activeChapter.status);
  const tokenRatio =
    briefing && briefing.totalTokenBudget > 0
      ? Math.min(100, Math.round((briefing.totalTokenEst / briefing.totalTokenBudget) * 100))
      : 0;

  return (
    <div className="flex min-h-0 flex-1 overflow-hidden bg-canvas">
      {/* Main Quiet Prose Canvas + Minimal Bottom Bar */}
      <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
        {/* Scrollable Prose Canvas */}
        <div
          ref={canvasScrollRef}
          className="flex-1 overflow-y-auto px-8 pt-8 pb-24"
        >
          <div className="mx-auto w-full max-w-[var(--prose-max-width)]">
            {/* Subtle chapter breadcrumb & quick switcher */}
            <div className="mb-5 flex items-center justify-between gap-2 text-xs text-ink-muted select-none">
              <div className="flex min-w-0 items-center gap-2">
                {activeVolumeTitle && (
                  <>
                    <span className="truncate">{activeVolumeTitle}</span>
                    <span>·</span>
                  </>
                )}
                <span className="nib-mono text-ink-secondary">
                  第 {formatSeq(activeChapter.globalSeq)} 章
                </span>
                <span
                  className={`rounded-full border px-2 py-0.5 text-[10.5px] ${statusMeta.badgeClass}`}
                >
                  {statusMeta.label}
                </span>
              </div>

              <div className="flex items-center gap-1">
                <button
                  type="button"
                  disabled={generating}
                  onClick={() => setReadingMode((v) => !v)}
                  title={readingMode ? "切回编辑模式" : "排版阅读预览"}
                  className={`flex items-center gap-1 rounded px-2 py-1 text-[11px] transition-colors disabled:opacity-30 ${
                    readingMode
                      ? "bg-accent-soft text-accent"
                      : "text-ink-muted hover:bg-surface hover:text-ink-primary"
                  }`}
                >
                  {readingMode ? (
                    <>
                      <Pencil className="h-3 w-3" />
                      <span>编辑</span>
                    </>
                  ) : (
                    <>
                      <BookOpen className="h-3 w-3" />
                      <span>排版阅读</span>
                    </>
                  )}
                </button>
                {chapters.length > 1 && (
                  <>
                    <button
                      type="button"
                      disabled={generating || currentChapterIndex <= 0}
                      onClick={() => {
                        const prevChap = chapters[currentChapterIndex - 1];
                        if (prevChap) void selectChapter(prevChap.id);
                      }}
                      title="上一章"
                      className="rounded p-1 text-ink-muted transition-colors hover:bg-surface hover:text-ink-primary disabled:opacity-30"
                    >
                      <ChevronLeft className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      disabled={
                        generating ||
                        currentChapterIndex < 0 ||
                        currentChapterIndex >= chapters.length - 1
                      }
                      onClick={() => {
                        const nextChap = chapters[currentChapterIndex + 1];
                        if (nextChap) void selectChapter(nextChap.id);
                      }}
                      title="下一章"
                      className="rounded p-1 text-ink-muted transition-colors hover:bg-surface hover:text-ink-primary disabled:opacity-30"
                    >
                      <ChevronRight className="h-3.5 w-3.5" />
                    </button>
                  </>
                )}
              </div>
            </div>

            {/* Chapter Title */}
            <input
              type="text"
              value={title}
              readOnly={generating}
              onChange={(e) => {
                setTitle(e.target.value);
                setDirty(true);
              }}
              placeholder="章节标题"
              className="mb-6 w-full bg-transparent font-prose text-[26px] leading-snug font-medium tracking-tight text-ink-primary placeholder:text-ink-muted focus:outline-none"
            />

            {/* Contextual inline banners (only when relevant: streaming / partial / API error) */}
            {generating && (
              <div className="mb-5 flex items-center justify-between gap-3 rounded-lg border border-accent/30 bg-accent-soft px-3.5 py-2 text-xs text-accent">
                <div className="flex items-center gap-2">
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  <span>{genStageText || "正在流式出稿…"}</span>
                </div>
                <button
                  type="button"
                  onClick={stopGeneration}
                  className="rounded px-2 py-0.5 font-medium text-danger transition-colors hover:bg-danger-soft"
                >
                  停止
                </button>
              </div>
            )}

            {!generating && genError && (
              <div className="mb-5 flex items-start justify-between gap-3 rounded-lg border border-danger/35 bg-danger-soft px-3.5 py-2.5 text-xs text-danger">
                <div className="flex items-start gap-2">
                  <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                  <span className="leading-relaxed">{genError}</span>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <button
                    type="button"
                    onClick={() => void startGeneration()}
                    className="rounded bg-danger/20 px-2 py-0.5 font-medium text-danger hover:bg-danger/30"
                  >
                    重试
                  </button>
                  <button
                    type="button"
                    onClick={() => setGenError(null)}
                    className="text-danger/70 hover:text-danger"
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                </div>
              </div>
            )}

            {!generating && !genError && activeChapter.status === "partial" && (
              <div className="mb-5 flex items-center justify-between gap-3 rounded-lg border border-warn/30 bg-warn-soft px-3.5 py-2 text-xs text-warn">
                <div className="flex items-center gap-2">
                  <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
                  <span>
                    已停在 <span className="nib-mono">{formatNumber(liveWordCount)}</span> 字，可继续手改或重新生成
                  </span>
                </div>
                <button
                  type="button"
                  onClick={() => void startGeneration()}
                  className="shrink-0 font-medium underline underline-offset-2 hover:opacity-80"
                >
                  重新生成
                </button>
              </div>
            )}

            {/* Prose Editor / Live Stream Surface / Reading Preview */}
            {generating || readingMode ? (
              <article
                className="nib-prose-column min-h-[480px]"
                onDoubleClick={() => {
                  if (!generating && readingMode) setReadingMode(false);
                }}
              >
                {streamingParagraphs.length === 0 ? (
                  <p className="text-ink-muted">
                    {generating ? "等待首字输出…" : "本章暂无正文，双击进入编辑模式"}
                  </p>
                ) : (
                  streamingParagraphs.map((para, idx) => (
                    <p key={idx}>
                      {para}
                      {generating && idx === streamingParagraphs.length - 1 && (
                        <span className="ml-0.5 inline-block h-[1.1em] w-[2px] translate-y-[2px] animate-pulse bg-accent align-baseline" />
                      )}
                    </p>
                  ))
                )}
              </article>
            ) : (
              <textarea
                ref={textareaRef}
                value={content}
                onChange={(e) => {
                  setContent(e.target.value);
                  setDirty(true);
                }}
                placeholder="在此直接落笔，或按 Ctrl+Enter 按编排节拍流式生成本章…"
                spellCheck={false}
                className="nib-prose-column block min-h-[480px] resize-none overflow-hidden bg-transparent placeholder:text-ink-muted/70 focus:outline-none"
              />
            )}
          </div>
        </div>

        {/* Minimal Studio Bottom Bar */}
        <footer className="flex h-10 shrink-0 items-center justify-between border-t border-border-subtle bg-surface/90 px-4 text-xs select-none">
          {/* Left metrics & status */}
          <div className="flex min-w-0 items-center gap-3 text-ink-secondary">
            <span className="nib-mono font-medium text-ink-primary">
              {formatNumber(liveWordCount)} 字
            </span>

            <span className="text-border-strong">·</span>

            <button
              type="button"
              onClick={() => openSettings("routes")}
              title="配置正文模型路由"
              className={`truncate transition-colors hover:text-ink-primary ${
                draftRouteInfo.configured ? "nib-mono text-ink-secondary" : "text-warn"
              }`}
            >
              {genModelName || draftRouteInfo.label}
            </button>

            <span className="text-border-strong">·</span>

            {generating ? (
              <span className="text-accent">流式生成中</span>
            ) : saving ? (
              <span className="text-ink-muted">保存中…</span>
            ) : dirty ? (
              <span className="text-warn">未保存</span>
            ) : activeChapter.status === "partial" ? (
              <span className="text-warn">partial</span>
            ) : (
              <span className="text-ink-muted">已保存</span>
            )}

            {lastUsage && !generating && (
              <span
                title={`输入 ${lastUsage.promptTokens} / 输出 ${lastUsage.completionTokens} tokens`}
                className="nib-mono hidden text-[11px] text-ink-muted lg:inline"
              >
                ↑{formatNumber(lastUsage.promptTokens)} ↓
                {formatNumber(lastUsage.completionTokens)}
              </span>
            )}
          </div>

          {/* Right actions */}
          <div className="flex shrink-0 items-center gap-1.5">
            {generating ? (
              <button
                type="button"
                onClick={stopGeneration}
                title="停止生成 (Ctrl+Enter)"
                className="flex items-center gap-1.5 rounded-md border border-danger/40 bg-danger-soft px-3 py-1 font-medium text-danger transition-colors hover:border-danger"
              >
                <Square className="h-3 w-3 fill-current" />
                <span>停止</span>
              </button>
            ) : (
              <button
                type="button"
                onClick={() => void startGeneration()}
                title="生成本章 (Ctrl+Enter)"
                className="flex items-center gap-1.5 rounded-md bg-accent px-3 py-1 font-semibold text-canvas transition-opacity hover:opacity-90"
              >
                <Play className="h-3 w-3 fill-current" />
                <span>生成本章</span>
              </button>
            )}

            <button
              type="button"
              disabled={generating || saving}
              onClick={() => void handleSave()}
              title="保存正文并归档快照 (Ctrl+S)"
              className="flex items-center gap-1 rounded-md border border-border-subtle bg-elevated px-2.5 py-1 text-ink-primary transition-colors hover:border-border-strong disabled:opacity-40"
            >
              <Save className="h-3.5 w-3.5 text-ink-secondary" />
              <span>保存</span>
            </button>

            <button
              type="button"
              disabled={generating || finalizing}
              onClick={() => void handleFinalize()}
              title="标记本章定稿并同步 Markdown"
              className={`flex items-center gap-1 rounded-md border px-2.5 py-1 transition-colors disabled:opacity-40 ${
                activeChapter.status === "finalized"
                  ? "border-ok/35 bg-ok-soft text-ok"
                  : "border-border-subtle bg-elevated text-ink-primary hover:border-ok/50 hover:text-ok"
              }`}
            >
              <CheckCircle2 className="h-3.5 w-3.5" />
              <span>{finalizing ? "定稿中…" : "定稿"}</span>
            </button>

            <button
              type="button"
              disabled={exporting}
              onClick={() => void handleExportBook()}
              title="单向导出全书 Markdown"
              className="flex items-center gap-1 rounded-md border border-border-subtle bg-elevated px-2.5 py-1 text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary disabled:opacity-40"
            >
              <Download className="h-3.5 w-3.5" />
              <span className="hidden sm:inline">
                {exporting ? "导出中…" : "导出全书"}
              </span>
            </button>

            <button
              type="button"
              onClick={toggleStudioDrawer}
              title="折叠 / 展开右侧抽屉 (Ctrl+\)"
              aria-label="切换右侧抽屉"
              className={`ml-0.5 flex h-7 w-7 items-center justify-center rounded-md border transition-colors ${
                studioDrawerOpen
                  ? "border-accent/35 bg-accent-soft text-accent"
                  : "border-border-subtle bg-elevated text-ink-secondary hover:text-ink-primary"
              }`}
            >
              <PanelRight className="h-3.5 w-3.5" />
            </button>
          </div>
        </footer>
      </div>

      {/* Collapsible Right Drawer (360px) */}
      {studioDrawerOpen && (
        <aside
          className="flex shrink-0 flex-col border-l border-border-subtle bg-surface"
          style={{ width: "var(--studio-drawer-width)" }}
        >
          {/* Drawer Header Tabs */}
          <div className="flex items-center justify-between border-b border-border-subtle px-3 py-2">
            <div className="flex items-center gap-1">
              <button
                type="button"
                onClick={() => setDrawerTab("briefing")}
                className={`flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                  drawerTab === "briefing"
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
                }`}
              >
                <FileText className="h-3.5 w-3.5" />
                <span>作战简报</span>
              </button>
              <button
                type="button"
                onClick={() => setDrawerTab("versions")}
                className={`flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                  drawerTab === "versions"
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
                }`}
              >
                <History className="h-3.5 w-3.5" />
                <span>历史快照</span>
                {versions.length > 0 && (
                  <span className="nib-mono text-[10px] opacity-80">
                    {versions.length}
                  </span>
                )}
              </button>
            </div>

            <button
              type="button"
              onClick={() =>
                drawerTab === "briefing" ? void loadBriefing() : void loadVersions()
              }
              title="刷新"
              className="rounded p-1 text-ink-muted transition-colors hover:bg-elevated hover:text-ink-primary"
            >
              <RefreshCw
                className={`h-3.5 w-3.5 ${
                  briefingLoading || versionsLoading ? "animate-spin text-accent" : ""
                }`}
              />
            </button>
          </div>

          {/* Tab 1: Briefing */}
          {drawerTab === "briefing" && (
            <div className="flex min-h-0 flex-1 flex-col overflow-y-auto px-3.5 py-3">
              {/* Token Budget Bar */}
              {briefing && (
                <div className="mb-3 rounded-lg border border-border-subtle bg-canvas/70 p-3">
                  <div className="flex items-center justify-between text-[11px]">
                    <span className="text-ink-secondary">估算上下文占用</span>
                    <span className="nib-mono font-medium text-ink-primary">
                      {formatNumber(briefing.totalTokenEst)} /{" "}
                      {formatNumber(briefing.totalTokenBudget)}
                    </span>
                  </div>
                  <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-elevated">
                    <div
                      className={`h-full rounded-full transition-all ${
                        tokenRatio >= 90 ? "bg-danger" : "bg-accent"
                      }`}
                      style={{ width: `${tokenRatio}%` }}
                    />
                  </div>
                  {briefing.warnings && briefing.warnings.length > 0 && (
                    <div className="mt-2 flex flex-col gap-1">
                      {briefing.warnings.map((w, idx) => (
                        <div
                          key={idx}
                          className="flex items-start gap-1.5 text-[11px] text-warn"
                        >
                          <AlertTriangle className="mt-0.5 h-3 w-3 shrink-0" />
                          <span>{w}</span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}

              {briefingError && (
                <div className="mb-3 rounded-lg border border-danger/30 bg-danger-soft px-3 py-2 text-xs text-danger">
                  {briefingError}
                </div>
              )}

              {/* Extra Instruction */}
              <div className="mb-3">
                <label className="mb-1 block text-[11px] font-medium text-ink-secondary">
                  本次出稿补充指令
                </label>
                <textarea
                  rows={2}
                  value={extraInstruction}
                  onChange={(e) => setExtraInstruction(e.target.value)}
                  placeholder="例如：本段重点压低配角台词字数，突出实战招式细节"
                  className="w-full resize-none rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                />
              </div>

              {/* Pin / Exclude Entities */}
              <div className="mb-3 rounded-lg border border-border-subtle bg-canvas/50">
                <button
                  type="button"
                  onClick={() => setEntityControlsOpen((v) => !v)}
                  className="flex w-full items-center justify-between px-3 py-2 text-left text-xs"
                >
                  <div className="flex items-center gap-2">
                    <span className="font-medium text-ink-primary">实体注入微调</span>
                    {(pinnedEntityIds.size > 0 || excludedEntityIds.size > 0) && (
                      <span className="nib-mono text-[10px] text-accent">
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
                  <div className="max-h-44 overflow-y-auto border-t border-border-subtle px-2 py-1.5">
                    {entities.length === 0 ? (
                      <div className="px-2 py-2 text-[11px] text-ink-muted">
                        账本暂无设定实体
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
                                      ? "世界铁律（P0 强制注入）"
                                      : included
                                      ? "已在简报中"
                                      : "未进入当前简报"
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
                                <span className="shrink-0 text-[10px] text-ink-muted">
                                  {getEntityKindLabel(ent.kind)}
                                </span>
                              </div>
                              <div className="flex shrink-0 items-center gap-1">
                                <button
                                  type="button"
                                  onClick={() => togglePinEntity(ent.id)}
                                  title="强制固定注入"
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
                                  onClick={() => toggleExcludeEntity(ent.id)}
                                  title={
                                    ent.isInvariant
                                      ? "世界铁律不可排除"
                                      : "本章排除"
                                  }
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

              {/* Slots List */}
              <div className="flex flex-col gap-2">
                {briefingLoading && !briefing ? (
                  <div className="py-8 text-center text-xs text-ink-muted">
                    正在编译简报…
                  </div>
                ) : (
                  (briefing?.slots ?? []).map((slot) => {
                    const expanded = expandedSlots.has(slot.key);
                    return (
                      <div
                        key={slot.key}
                        className="overflow-hidden rounded-lg border border-border-subtle bg-canvas/70"
                      >
                        <button
                          type="button"
                          onClick={() => toggleSlotExpand(slot.key)}
                          className="flex w-full items-center justify-between gap-2 px-3 py-2 text-left transition-colors hover:bg-elevated/40"
                        >
                          <div className="flex min-w-0 items-center gap-2">
                            <span
                              className={`nib-mono shrink-0 rounded px-1.5 py-0.5 text-[10px] font-semibold ${
                                slot.priority === "P0"
                                  ? "bg-danger-soft text-danger"
                                  : slot.priority === "P1"
                                  ? "bg-accent-soft text-accent"
                                  : "bg-elevated text-ink-secondary"
                              }`}
                            >
                              {slot.priority}
                            </span>
                            <span className="truncate text-xs font-medium text-ink-primary">
                              {slot.title}
                            </span>
                            {slot.truncated && (
                              <span className="shrink-0 rounded bg-warn-soft px-1 text-[10px] text-warn">
                                截断
                              </span>
                            )}
                          </div>
                          <div className="flex shrink-0 items-center gap-1.5">
                            <span className="nib-mono text-[11px] text-ink-muted">
                              {formatNumber(slot.tokenEst)}t
                            </span>
                            <ChevronDown
                              className={`h-3.5 w-3.5 text-ink-muted transition-transform ${
                                expanded ? "rotate-180" : ""
                              }`}
                            />
                          </div>
                        </button>

                        {expanded && (
                          <div className="border-t border-border-subtle bg-canvas px-3 py-2.5">
                            <pre className="max-h-64 overflow-y-auto font-ui text-[11.5px] leading-relaxed whitespace-pre-wrap text-ink-secondary">
                              {slot.content || "（空槽位）"}
                            </pre>
                          </div>
                        )}
                      </div>
                    );
                  })
                )}
              </div>
            </div>
          )}

          {/* Tab 2: Version Snapshots */}
          {drawerTab === "versions" && (
            <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
              {versionsLoading && versions.length === 0 ? (
                <div className="flex flex-1 items-center justify-center text-xs text-ink-muted">
                  正在读取历史快照…
                </div>
              ) : versions.length === 0 ? (
                <div className="flex flex-1 items-center justify-center px-6 text-center text-xs text-ink-muted">
                  暂无历史快照。保存正文、生成或回滚时会自动归档。
                </div>
              ) : (
                <>
                  <div className="max-h-[45%] shrink-0 overflow-y-auto border-b border-border-subtle p-2.5">
                    <div className="flex flex-col gap-1.5">
                      {versions.map((ver) => {
                        const selected = ver.id === selectedVersionId;
                        return (
                          <button
                            key={ver.id}
                            type="button"
                            onClick={() => {
                              setSelectedVersionId(ver.id);
                              setConfirmRestoreId(null);
                            }}
                            className={`flex flex-col gap-1 rounded-lg border px-3 py-2 text-left transition-colors ${
                              selected
                                ? "border-accent/50 bg-accent-soft"
                                : "border-border-subtle bg-canvas/60 hover:border-border-strong"
                            }`}
                          >
                            <div className="flex items-center justify-between gap-2">
                              <span
                                className={`text-xs font-medium ${
                                  selected ? "text-accent" : "text-ink-primary"
                                }`}
                              >
                                {getVersionSourceLabel(ver.source)}
                              </span>
                              <span className="nib-mono text-[11px] text-ink-secondary">
                                {formatNumber(ver.wordCount)} 字
                              </span>
                            </div>
                            <div className="flex items-center justify-between gap-2 text-[11px] text-ink-muted">
                              <span className="nib-mono truncate">
                                {ver.modelName || "人工编辑"}
                              </span>
                              <span className="nib-mono shrink-0">
                                {formatTimestamp(ver.createdAt)}
                              </span>
                            </div>
                          </button>
                        );
                      })}
                    </div>
                  </div>

                  {selectedVersion && (
                    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
                      <div className="flex items-center justify-between border-b border-border-subtle bg-surface px-3.5 py-2">
                        <span className="nib-mono text-[11px] text-ink-secondary">
                          快照 #{selectedVersion.id} ·{" "}
                          {formatNumber(selectedVersion.wordCount)} 字
                        </span>
                        {confirmRestoreId === selectedVersion.id ? (
                          <div className="flex items-center gap-1.5">
                            <button
                              type="button"
                              disabled={restoringId === selectedVersion.id}
                              onClick={() =>
                                void handleRestoreVersion(selectedVersion.id)
                              }
                              className="flex items-center gap-1 rounded bg-accent px-2 py-0.5 text-[11px] font-semibold text-canvas"
                            >
                              <Check className="h-3 w-3" />
                              <span>
                                {restoringId === selectedVersion.id
                                  ? "回滚中…"
                                  : "确认回滚"}
                              </span>
                            </button>
                            <button
                              type="button"
                              onClick={() => setConfirmRestoreId(null)}
                              className="rounded p-0.5 text-ink-muted hover:text-ink-primary"
                            >
                              <X className="h-3.5 w-3.5" />
                            </button>
                          </div>
                        ) : (
                          <button
                            type="button"
                            disabled={generating}
                            onClick={() => setConfirmRestoreId(selectedVersion.id)}
                            className="flex items-center gap-1 rounded-md border border-border-subtle bg-elevated px-2.5 py-1 text-[11px] font-medium text-ink-primary transition-colors hover:border-accent hover:text-accent disabled:opacity-40"
                          >
                            <RotateCcw className="h-3 w-3" />
                            <span>回滚到此版本</span>
                          </button>
                        )}
                      </div>

                      <div className="flex-1 overflow-y-auto bg-canvas/50 px-4 py-3">
                        <div className="font-prose text-[13.5px] leading-relaxed whitespace-pre-wrap text-ink-secondary">
                          {selectedVersion.content || "（空正文）"}
                        </div>
                      </div>
                    </div>
                  )}
                </>
              )}
            </div>
          )}
        </aside>
      )}
    </div>
  );
}
