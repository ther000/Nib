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
  FileDiff,
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
  ShieldCheck,
  Square,
  X,
} from "lucide-react";
import {
  LibraryService,
  MemoryService,
  StudioService,
  StyleService,
  type ApplyDeltaResult,
  type ApplySurgicalDiffResult,
  type Briefing,
  type ChapterAudit,
  type ChapterDelta,
  type ChapterVersion,
  type CriticFinding,
  type GenChunk,
  type LintIssue,
  type SurgicalDiff,
  type TokenUsage,
} from "../mygo";
import { useApp } from "../stores/AppContext";
import { AuditProseParagraph } from "../components/AuditProseParagraph";
import { DeltaReviewPanel } from "../components/DeltaReviewPanel";
import { QualityAuditPanel } from "../components/QualityAuditPanel";
import {
  countWords,
  formatErrorMessage,
  formatNumber,
  formatSeq,
  formatTimestamp,
  getChapterStatusMeta,
  getEntityKindLabel,
  getVersionSourceLabel,
  parseKeywordList,
  splitParagraphs,
} from "../lib/constants";

type DrawerTab = "briefing" | "audit" | "delta" | "versions";

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
    setStudioDrawerOpen,
    toggleStudioDrawer,
    selectChapter,
    refreshChapters,
    refreshActiveChapter,
    refreshEntities,
    refreshForeshadowings,
    bumpMemoryRevision,
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
  const [includePending, setIncludePending] = useState(false);
  const [searchKeywordsText, setSearchKeywordsText] = useState("");
  const [entityControlsOpen, setEntityControlsOpen] = useState(false);

  // Pending memory delta state
  const [pendingDelta, setPendingDelta] = useState<ChapterDelta | null>(null);
  const [deltaLoading, setDeltaLoading] = useState(false);
  const deltaReqIdRef = useRef(0);

  // Phase 3 Quality Audit & Surgical Diff state
  const [chapterAudit, setChapterAudit] = useState<ChapterAudit | null>(null);
  const [auditLoading, setAuditLoading] = useState(false);
  const [activeParagraphIndex, setActiveParagraphIndex] = useState<number | null>(null);
  const [polishingParagraphs, setPolishingParagraphs] = useState(false);
  const [applyingDiffId, setApplyingDiffId] = useState<string | null>(null);
  const auditReqIdRef = useRef(0);
  const polishAbortRef = useRef<AbortController | null>(null);
  const polishRunIdRef = useRef(0);

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
    if (polishAbortRef.current) {
      polishRunIdRef.current += 1;
      polishAbortRef.current.abort();
      polishAbortRef.current = null;
      setPolishingParagraphs(false);
    }

    if (!activeChapter) {
      setTitle("");
      setContent("");
      setDirty(false);
      setGenError(null);
      setLastUsage(null);
      setChapterAudit(null);
      setActiveParagraphIndex(null);
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
    setSearchKeywordsText("");
    setExpandedSlots(new Set());
    setPendingDelta(null);
    setChapterAudit(null);
    setActiveParagraphIndex(null);
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
      if (polishAbortRef.current) {
        polishRunIdRef.current += 1;
        polishAbortRef.current.abort();
        polishAbortRef.current = null;
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
      const keywords = parseKeywordList(searchKeywordsText);
      const res = await StudioService.previewBriefing(activeChapterId, {
        pinnedEntityIds: Array.from(pinnedEntityIds),
        excludedEntityIds: Array.from(excludedEntityIds),
        extraInstruction: extraInstruction.trim() || undefined,
        includePending,
        searchKeywords: keywords.length > 0 ? keywords : undefined,
      });
      setBriefing(res);
    } catch (err) {
      setBriefingError(formatErrorMessage(err));
    } finally {
      setBriefingLoading(false);
    }
  }, [
    activeChapterId,
    pinnedEntityIds,
    excludedEntityIds,
    extraInstruction,
    includePending,
    searchKeywordsText,
  ]);

  // Load pending chapter memory delta
  const loadPendingDelta = useCallback(async (targetChapterId?: number | null) => {
    const cid = targetChapterId ?? activeChapterId;
    if (!cid) {
      setPendingDelta(null);
      return;
    }
    const reqId = ++deltaReqIdRef.current;
    setDeltaLoading(true);
    try {
      const delta = await MemoryService.getPendingDelta(cid);
      if (deltaReqIdRef.current === reqId) {
        setPendingDelta(delta);
      }
    } catch {
      if (deltaReqIdRef.current === reqId) {
        setPendingDelta(null);
      }
    } finally {
      if (deltaReqIdRef.current === reqId) {
        setDeltaLoading(false);
      }
    }
  }, [activeChapterId]);

  // Automatically check pending delta when switching chapters
  useEffect(() => {
    void loadPendingDelta(activeChapterId);
  }, [activeChapterId, loadPendingDelta]);

  // Phase 3: Load persisted ChapterAudit or run 0-cost local lint
  const runLocalLint = useCallback(
    async (targetChapterId?: number | null, silent = false) => {
      const cid = targetChapterId ?? activeChapterId;
      if (!cid) return;
      const reqId = ++auditReqIdRef.current;
      setAuditLoading(true);
      try {
        const res = await StyleService.lintChapterLocal(cid);
        if (mountedRef.current && auditReqIdRef.current === reqId && activeChapterIdRef.current === cid) {
          setChapterAudit(res);
          if (!silent) {
            notify({
              tone: res.lintIssues.length === 0 ? "ok" : "info",
              message: `本地扫描完成 · 去 AI 味 ${res.deaiScore}/100`,
              detail:
                res.lintIssues.length > 0
                  ? `检出 ${res.lintIssues.length} 处套话或结构排版告警`
                  : "未检出高频 AI 套话与排版病灶",
            });
          }
        }
      } catch (err) {
        if (!silent && mountedRef.current) {
          notify({
            tone: "danger",
            message: "本地 AI 味扫描失败",
            detail: formatErrorMessage(err),
          });
        }
      } finally {
        if (mountedRef.current && auditReqIdRef.current === reqId) {
          setAuditLoading(false);
        }
      }
    },
    [activeChapterId, notify]
  );

  const loadChapterAudit = useCallback(
    async (targetChapterId?: number | null) => {
      const cid = targetChapterId ?? activeChapterId;
      if (!cid) {
        setChapterAudit(null);
        return;
      }
      const reqId = ++auditReqIdRef.current;
      setAuditLoading(true);
      try {
        const audit = await StyleService.getChapterAudit(cid);
        if (mountedRef.current && auditReqIdRef.current === reqId && activeChapterIdRef.current === cid) {
          setChapterAudit(audit);
          if (audit.id === 0 && activeChapter?.content?.trim()) {
            void runLocalLint(cid, true);
          }
        }
      } catch {
        if (mountedRef.current && auditReqIdRef.current === reqId && activeChapterIdRef.current === cid) {
          if (activeChapter?.content?.trim()) {
            void runLocalLint(cid, true);
          }
        }
      } finally {
        if (mountedRef.current && auditReqIdRef.current === reqId) {
          setAuditLoading(false);
        }
      }
    },
    [activeChapterId, activeChapter?.content, runLocalLint]
  );

  useEffect(() => {
    void loadChapterAudit(activeChapterId);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeChapterId]);

  const scrollToParagraph = useCallback((pIdx: number) => {
    setReadingMode(true);
    setActiveParagraphIndex(pIdx);
    requestAnimationFrame(() => {
      const el = canvasScrollRef.current?.querySelector(
        `[data-paragraph-index="${pIdx}"]`
      );
      if (el instanceof HTMLElement) {
        el.scrollIntoView({ behavior: "smooth", block: "center" });
      }
    });
  }, []);

  const stopSurgicalPolish = useCallback(() => {
    if (polishAbortRef.current) {
      polishAbortRef.current.abort();
      polishAbortRef.current = null;
    }
  }, []);

  // Flush unsaved manual edits before running backend lint / critic / surgical polish
  const ensureSavedBeforeAudit = useCallback(async (): Promise<boolean> => {
    if (!activeChapter || generating) return false;
    if (!dirty) return true;
    try {
      const saved = await LibraryService.saveChapterDraft({
        chapterId: activeChapter.id,
        title: title.trim() || activeChapter.title,
        content,
        backupSource: "manual_backup",
      });
      updateChapterInCache(saved);
      setDirty(false);
      return true;
    } catch (err) {
      notify({
        tone: "danger",
        message: "保存当前修改失败",
        detail: formatErrorMessage(err),
      });
      return false;
    }
  }, [activeChapter, generating, dirty, title, content, updateChapterInCache, notify]);

  const handleRunSurgicalPolish = useCallback(
    async (targetParagraphs: number[]) => {
      if (!activeChapter || polishingParagraphs) return;
      const savedOk = await ensureSavedBeforeAudit();
      if (!savedOk) return;
      const targetChapterId = activeChapter.id;
      const runId = ++polishRunIdRef.current;
      const ac = new AbortController();
      polishAbortRef.current = ac;
      setPolishingParagraphs(true);

      try {
        const updated = await StyleService.generateSurgicalDiffs.with({
          signal: ac.signal,
        })(targetChapterId, targetParagraphs);
        if (
          !mountedRef.current ||
          polishRunIdRef.current !== runId ||
          activeChapterIdRef.current !== targetChapterId
        ) {
          return;
        }
        setChapterAudit(updated);
        const pending = (updated.diffs ?? []).filter((d) => d.status === "pending");
        if (pending.length > 0) {
          const targetIdx =
            targetParagraphs[0] !== undefined
              ? targetParagraphs[0]
              : pending[0]!.paragraphIndex;
          scrollToParagraph(targetIdx);
          notify({
            tone: "ok",
            message: `已生成 ${pending.length} 条手术式局部润色建议`,
            detail: "按 Tab 采纳替换，按 Esc 忽略",
          });
        } else {
          notify({
            tone: "info",
            message: "未生成新的局部润色建议",
          });
        }
      } catch (err) {
        if (ac.signal.aborted) {
          if (mountedRef.current && polishRunIdRef.current === runId) {
            notify({ tone: "info", message: "已取消手术润色生成" });
          }
          return;
        }
        if (!mountedRef.current || polishRunIdRef.current !== runId) return;
        notify({
          tone: "danger",
          message: "生成手术润色失败",
          detail: formatErrorMessage(err),
        });
      } finally {
        if (polishAbortRef.current === ac) polishAbortRef.current = null;
        if (mountedRef.current && polishRunIdRef.current === runId) {
          setPolishingParagraphs(false);
        }
      }
    },
    [
      activeChapter,
      polishingParagraphs,
      ensureSavedBeforeAudit,
      scrollToParagraph,
      notify,
    ]
  );

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

  const handleDiffAppliedResult = useCallback(
    (res: ApplySurgicalDiffResult) => {
      updateChapterInCache(res.chapter);
      setTitle(res.chapter.title);
      setContent(res.chapter.content);
      setDirty(false);
      setChapterAudit(res.audit);
      if (studioDrawerOpen && drawerTab === "versions") {
        void loadVersions();
      }
    },
    [updateChapterInCache, studioDrawerOpen, drawerTab, loadVersions]
  );

  const handleApplyInlineDiff = useCallback(
    async (diffId: string, accept: boolean, customReplacement?: string) => {
      if (!activeChapter || applyingDiffId) return;
      setApplyingDiffId(diffId);
      try {
        const res = await StyleService.applySurgicalDiff({
          chapterId: activeChapter.id,
          diffId,
          accept,
          customReplacement,
        });
        handleDiffAppliedResult(res);
        notify({
          tone: accept ? "ok" : "info",
          message: accept ? "已采纳手术润色并归档快照" : "已忽略该条润色建议",
        });
      } catch (err) {
        notify({
          tone: "danger",
          message: "应用手术润色失败",
          detail: formatErrorMessage(err),
        });
      } finally {
        setApplyingDiffId(null);
      }
    },
    [activeChapter, applyingDiffId, handleDiffAppliedResult, notify]
  );

  const handleDeltaApplied = useCallback(
    async (result: ApplyDeltaResult) => {
      setPendingDelta(null);
      await Promise.all([
        refreshActiveChapter(activeChapterId),
        refreshChapters(activeBook?.id),
        refreshEntities(activeBook?.id),
        refreshForeshadowings(activeBook?.id),
      ]);
      bumpMemoryRevision();
      if (studioDrawerOpen && drawerTab === "briefing") {
        void loadBriefing();
      }

      const dirtyCount = result.markedDirtyChapterIds?.length ?? 0;
      const summaryParts = [
        `${result.committedEvents} 条状态事件入库`,
        `${result.updatedForeshadowings} 条伏笔更新`,
      ];
      if (dirtyCount > 0) {
        summaryParts.push(`下游 ${dirtyCount} 章标记为上游变更`);
      }
      notify({
        tone: dirtyCount > 0 ? "warn" : "ok",
        message: "记忆变更单已入库",
        detail: summaryParts.join(" · "),
      });
    },
    [
      activeChapterId,
      activeBook?.id,
      refreshActiveChapter,
      refreshChapters,
      refreshEntities,
      refreshForeshadowings,
      bumpMemoryRevision,
      studioDrawerOpen,
      drawerTab,
      loadBriefing,
      notify,
    ]
  );

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
      void runLocalLint(saved.id, true);
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
    runLocalLint,
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
    const keywords = parseKeywordList(searchKeywordsText);
    const opts = {
      briefing: {
        pinnedEntityIds: Array.from(pinnedEntityIds),
        excludedEntityIds: Array.from(excludedEntityIds),
        extraInstruction: trimmedExtra || undefined,
        includePending,
        searchKeywords: keywords.length > 0 ? keywords : undefined,
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
          void loadPendingDelta(genChapterId);
          void runLocalLint(genChapterId, true);
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
    includePending,
    searchKeywordsText,
    updateChapterInCache,
    refreshActiveChapter,
    notify,
    openSettings,
    studioDrawerOpen,
    drawerTab,
    loadPendingDelta,
    runLocalLint,
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
    () => splitParagraphs(content),
    [content]
  );

  // Per-paragraph maps for inline underlines, critic callouts, and pending surgical diffs
  const paragraphAuditMaps = useMemo(() => {
    const lintByPara = new Map<number, LintIssue[]>();
    const criticByPara = new Map<number, CriticFinding[]>();
    const pendingDiffByPara = new Map<number, SurgicalDiff>();
    const flaggedSet = new Set<number>();

    for (const iss of chapterAudit?.lintIssues ?? []) {
      if (iss.paragraphIndex < 0) continue;
      flaggedSet.add(iss.paragraphIndex);
      const list = lintByPara.get(iss.paragraphIndex);
      if (list) list.push(iss);
      else lintByPara.set(iss.paragraphIndex, [iss]);
    }
    for (const cf of chapterAudit?.criticFindings ?? []) {
      if (cf.paragraphIndex < 0) continue;
      flaggedSet.add(cf.paragraphIndex);
      const list = criticByPara.get(cf.paragraphIndex);
      if (list) list.push(cf);
      else criticByPara.set(cf.paragraphIndex, [cf]);
    }
    for (const df of chapterAudit?.diffs ?? []) {
      if (df.status === "pending" && df.paragraphIndex >= 0) {
        flaggedSet.add(df.paragraphIndex);
        pendingDiffByPara.set(df.paragraphIndex, df);
      }
    }

    const flaggedParagraphs = Array.from(flaggedSet).sort((a, b) => a - b);
    const totalFlags =
      (chapterAudit?.lintIssues?.length ?? 0) +
      (chapterAudit?.criticFindings?.length ?? 0) +
      pendingDiffByPara.size;

    return {
      lintByPara,
      criticByPara,
      pendingDiffByPara,
      flaggedParagraphs,
      totalFlags,
    };
  }, [chapterAudit]);

  const cycleFlaggedParagraph = useCallback(
    (dir: 1 | -1) => {
      const list = paragraphAuditMaps.flaggedParagraphs;
      if (list.length === 0) return;
      if (activeParagraphIndex === null) {
        const first = dir === 1 ? list[0] : list[list.length - 1];
        if (first !== undefined) scrollToParagraph(first);
        return;
      }
      const curPos = list.indexOf(activeParagraphIndex);
      let nextPos: number;
      if (curPos === -1) {
        nextPos = dir === 1 ? 0 : list.length - 1;
      } else {
        nextPos = (curPos + dir + list.length) % list.length;
      }
      const target = list[nextPos];
      if (target !== undefined) scrollToParagraph(target);
    },
    [paragraphAuditMaps.flaggedParagraphs, activeParagraphIndex, scrollToParagraph]
  );

  // Alt+] / Alt+[ issue navigation (Tab / Esc on focused diff is handled by AuditProseParagraph)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.altKey && !e.ctrlKey && !e.metaKey) {
        if (e.key === "]" || e.code === "BracketRight") {
          e.preventDefault();
          cycleFlaggedParagraph(1);
          return;
        }
        if (e.key === "[" || e.code === "BracketLeft") {
          e.preventDefault();
          cycleFlaggedParagraph(-1);
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [cycleFlaggedParagraph]);

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
                {paragraphAuditMaps.flaggedParagraphs.length > 0 && (
                  <div className="mr-1 flex items-center gap-0.5 rounded-md border border-border-subtle bg-surface px-1 py-0.5">
                    <button
                      type="button"
                      onClick={() => cycleFlaggedParagraph(-1)}
                      title="上一处 AI 味或审校告警 (Alt+[)"
                      className="rounded px-1.5 py-0.5 text-[10.5px] text-ink-secondary hover:bg-elevated hover:text-ink-primary"
                    >
                      [ 上一处
                    </button>
                    <span className="nib-mono px-1 text-[10.5px] text-warn">
                      {paragraphAuditMaps.flaggedParagraphs.length} 处病灶
                    </span>
                    <button
                      type="button"
                      onClick={() => cycleFlaggedParagraph(1)}
                      title="下一处 AI 味或审校告警 (Alt+])"
                      className="rounded px-1.5 py-0.5 text-[10.5px] text-ink-secondary hover:bg-elevated hover:text-ink-primary"
                    >
                      下一处 ]
                    </button>
                  </div>
                )}
                <button
                  type="button"
                  disabled={generating}
                  onClick={() => setReadingMode((v) => !v)}
                  title={readingMode ? "切回编辑模式" : "排版审校与局部润色视图"}
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
                      <span>排版审校</span>
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

            {!generating && !genError && activeChapter.status === "upstream_dirty" && (
              <div className="mb-5 flex items-center justify-between gap-3 rounded-lg border border-danger/35 bg-danger-soft/70 px-3.5 py-2 text-xs text-danger">
                <div className="flex items-center gap-2">
                  <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
                  <span>上游章节设定已变更，建议重新预览简报或复核本章正文</span>
                </div>
                <button
                  type="button"
                  onClick={() => {
                    setDrawerTab("briefing");
                    setStudioDrawerOpen(true);
                    void loadBriefing();
                  }}
                  className="shrink-0 font-medium underline underline-offset-2 hover:opacity-80"
                >
                  查看简报
                </button>
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

            {/* Prose Editor / Live Stream Surface / Interactive Paragraph Audit & Surgical Diff */}
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
                    <AuditProseParagraph
                      key={idx}
                      paragraphIndex={idx}
                      text={para}
                      isStreamingLast={
                        generating && idx === streamingParagraphs.length - 1
                      }
                      isFocused={!generating && activeParagraphIndex === idx}
                      lintIssues={
                        generating
                          ? []
                          : paragraphAuditMaps.lintByPara.get(idx) ?? []
                      }
                      criticFindings={
                        generating
                          ? []
                          : paragraphAuditMaps.criticByPara.get(idx) ?? []
                      }
                      pendingDiff={
                        generating
                          ? null
                          : paragraphAuditMaps.pendingDiffByPara.get(idx) ?? null
                      }
                      polishing={polishingParagraphs}
                      applyingDiffId={applyingDiffId}
                      onFocus={(pIdx) => setActiveParagraphIndex(pIdx)}
                      onRequestPolish={(pIdx) =>
                        void handleRunSurgicalPolish([pIdx])
                      }
                      onApplyDiff={(diffId, accept, customRep) =>
                        void handleApplyInlineDiff(diffId, accept, customRep)
                      }
                    />
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
          {/* Drawer Header Tabs: 简报 / 质检 / 变更单 / 快照 */}
          <div className="flex items-center justify-between border-b border-border-subtle px-2 py-2">
            <div className="flex items-center gap-0.5">
              <button
                type="button"
                onClick={() => setDrawerTab("briefing")}
                className={`flex items-center gap-1 whitespace-nowrap rounded-md px-2 py-1 text-xs font-medium transition-colors ${
                  drawerTab === "briefing"
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
                }`}
              >
                <FileText className="h-3.5 w-3.5" />
                <span>简报</span>
              </button>
              <button
                type="button"
                onClick={() => setDrawerTab("audit")}
                className={`flex items-center gap-1 whitespace-nowrap rounded-md px-2 py-1 text-xs font-medium transition-colors ${
                  drawerTab === "audit"
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
                }`}
              >
                <ShieldCheck className="h-3.5 w-3.5" />
                <span>质检</span>
                {paragraphAuditMaps.totalFlags > 0 && (
                  <span className="nib-mono rounded-full bg-warn-soft px-1 text-[10px] font-semibold text-warn">
                    {paragraphAuditMaps.totalFlags}
                  </span>
                )}
              </button>
              <button
                type="button"
                onClick={() => setDrawerTab("delta")}
                className={`flex items-center gap-1 whitespace-nowrap rounded-md px-2 py-1 text-xs font-medium transition-colors ${
                  drawerTab === "delta"
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
                }`}
              >
                <FileDiff className="h-3.5 w-3.5" />
                <span>变更单</span>
                {pendingDelta && (
                  <span
                    className={`h-1.5 w-1.5 rounded-full ${
                      pendingDelta.highRiskFlags > 0 ? "bg-danger" : "bg-warn"
                    }`}
                  />
                )}
              </button>
              <button
                type="button"
                onClick={() => setDrawerTab("versions")}
                className={`flex items-center gap-1 whitespace-nowrap rounded-md px-2 py-1 text-xs font-medium transition-colors ${
                  drawerTab === "versions"
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
                }`}
              >
                <History className="h-3.5 w-3.5" />
                <span>快照</span>
                {versions.length > 0 && (
                  <span className="nib-mono text-[10px] opacity-80">
                    {versions.length}
                  </span>
                )}
              </button>
            </div>

            <button
              type="button"
              onClick={() => {
                if (drawerTab === "briefing") void loadBriefing();
                else if (drawerTab === "audit") void runLocalLint(activeChapter.id, false);
                else if (drawerTab === "delta") void loadPendingDelta();
                else void loadVersions();
              }}
              title="刷新"
              className="rounded p-1 text-ink-muted transition-colors hover:bg-elevated hover:text-ink-primary"
            >
              <RefreshCw
                className={`h-3.5 w-3.5 ${
                  briefingLoading || versionsLoading || deltaLoading || auditLoading
                    ? "animate-spin text-accent"
                    : ""
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

              {/* Extra Instruction & Memory Retrieval Controls */}
              <div className="mb-3 flex flex-col gap-2.5">
                <div>
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

                <div>
                  <label className="mb-1 block text-[11px] font-medium text-ink-secondary">
                    记忆检索关键词（P6 槽位，逗号分隔）
                  </label>
                  <input
                    type="text"
                    value={searchKeywordsText}
                    onChange={(e) => setSearchKeywordsText(e.target.value)}
                    placeholder="例如：血玉令牌, 破障丹, 残阵"
                    className="w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                  />
                </div>

                <label className="flex cursor-pointer items-center justify-between rounded-md border border-border-subtle bg-canvas/60 px-2.5 py-1.5 text-xs text-ink-secondary select-none">
                  <span>含同批待审状态（pending）</span>
                  <input
                    type="checkbox"
                    checked={includePending}
                    onChange={(e) => setIncludePending(e.target.checked)}
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
                                  : slot.priority === "P2"
                                  ? "bg-ok-soft text-ok"
                                  : slot.priority === "P3"
                                  ? "bg-warn-soft text-warn"
                                  : slot.priority === "P7"
                                  ? "bg-accent-soft/80 text-accent"
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

          {/* Tab 2: Quality Audit & Surgical Polish */}
          {drawerTab === "audit" && (
            <QualityAuditPanel
              chapterId={activeChapter.id}
              chapterHasContent={content.trim().length > 0}
              audit={chapterAudit}
              loading={auditLoading}
              activeParagraphIndex={activeParagraphIndex}
              polishingParagraphs={polishingParagraphs}
              onAuditUpdated={setChapterAudit}
              onDiffApplied={handleDiffAppliedResult}
              onFocusParagraph={scrollToParagraph}
              onEnsureSavedBeforeAudit={ensureSavedBeforeAudit}
              onRunLocalLint={async () => {
                const ok = await ensureSavedBeforeAudit();
                if (ok) await runLocalLint(activeChapter.id, false);
              }}
              onRunSurgicalPolish={handleRunSurgicalPolish}
              onStopSurgicalPolish={stopSurgicalPolish}
              onNotify={notify}
            />
          )}

          {/* Tab 3: Memory Delta Review */}
          {drawerTab === "delta" && (
            <DeltaReviewPanel
              chapterId={activeChapter.id}
              chapterHasContent={content.trim().length > 0}
              delta={pendingDelta}
              loading={deltaLoading}
              onDeltaLoaded={setPendingDelta}
              onDeltaApplied={handleDeltaApplied}
              onNotify={notify}
            />
          )}

          {/* Tab 3: Version Snapshots */}
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
