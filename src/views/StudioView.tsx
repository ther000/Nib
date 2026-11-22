import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
} from "react";
import {
  AlertCircle,
  AlertTriangle,
  BookOpen,
  Check,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Clipboard,
  Copy,
  Download,
  FastForward,
  FileDiff,
  FileText,
  History,
  Loader2,
  Minimize2,
  MoreHorizontal,
  PanelLeft,
  PanelRight,
  Pencil,
  Play,
  RefreshCw,
  Save,
  Scissors,
  Search,
  ShieldCheck,
  Sparkles,
  Square,
  TextSelect,
  Wand2,
  X,
} from "lucide-react";
import {
  LibraryService,
  MemoryService,
  StudioService,
  StyleService,
  type ApplyDeltaResult,
  type ApplySurgicalDiffResult,
  type Beat,
  type Briefing,
  type ChapterAudit,
  type ChapterDelta,
  type ChapterVersion,
  type CriticFinding,
  type EntitySnapshot,
  type GenChunk,
  type LintIssue,
  type RewritePreset,
  type SurgicalDiff,
} from "../mygo";
import { useApp } from "../stores/AppContext";
import { useContextMenu, type ContextMenuItem } from "../components/ContextMenu";
import { useMygoChannel } from "../hooks/useMygoStream";
import { useChapterGeneration } from "../hooks/useChapterGeneration";
import { AuditProseParagraph } from "../components/AuditProseParagraph";
import { DeltaReviewPanel } from "../components/DeltaReviewPanel";
import { QualityAuditPanel } from "../components/QualityAuditPanel";
import { FindReplaceBar } from "../components/studio/FindReplaceBar";
import { SelectionRewriteBar } from "../components/studio/SelectionRewriteBar";
import { BriefingDrawerTab } from "../components/studio/BriefingDrawerTab";
import { VersionsDrawerTab } from "../components/studio/VersionsDrawerTab";
import {
  StudioReferenceSidebar,
  type ReferenceTab,
} from "../components/studio/StudioReferenceSidebar";
import { QuickModelPicker } from "../components/studio/QuickModelPicker";
import { applyLintReplacementInContent } from "../lib/studioHelpers";
import { formatWebNovelProse } from "../lib/textDiff";
import {
  countWords,
  formatErrorMessage,
  formatNumber,
  formatSeq,
  getChapterStatusMeta,
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
    foreshadowings,
    memoryRevision,
    providers,
    modelRoutes,
    proseFont,
    proseIndent,
    proseFontSize,
    proseLineHeight,
    proseMaxWidth,
    studioDrawerOpen,
    setStudioDrawerOpen,
    toggleStudioDrawer,
    focusMode,
    setFocusMode,
    selectChapter,
    refreshChapters,
    refreshActiveChapter,
    refreshEntities,
    refreshForeshadowings,
    bumpMemoryRevision,
    updateChapterInCache,
    setActiveTab,
    openAtlasSubTab,
    openSettings,
    notify,
    registerSaveHandler,
    registerGenerateHandler,
  } = useApp();
  const { openContextMenu } = useContextMenu();

  // Editor state
  const [title, setTitle] = useState("");
  const [content, setContent] = useState("");
  const [dirty, setDirty] = useState(false);
  const [readingMode, setReadingMode] = useState(false);
  const [saving, setSaving] = useState(false);
  const [lastAutoSavedAt, setLastAutoSavedAt] = useState<string | null>(null);
  const [copiedClean, setCopiedClean] = useState(false);
  const [finalizing, setFinalizing] = useState(false);
  const [exporting, setExporting] = useState(false);

  // Find & Replace bar (Ctrl+H)
  const [findOpen, setFindOpen] = useState(false);
  const [findScope, setFindScope] = useState<"chapter" | "book">("chapter");
  const [findQuery, setFindQuery] = useState("");
  const [replaceText, setReplaceText] = useState("");
  const [currentMatchIndex, setCurrentMatchIndex] = useState(0);
  const [bookReplaceStats, setBookReplaceStats] = useState<{
    affectedChapters: number;
    totalMatches: number;
  } | null>(null);
  const [replacingBook, setReplacingBook] = useState(false);

  // Canvas overflow menu (···)
  const [moreMenuOpen, setMoreMenuOpen] = useState(false);
  const moreMenuRef = useRef<HTMLDivElement>(null);

  // Selection AI micro-edit state
  const [selectionRange, setSelectionRange] = useState<{
    start: number;
    end: number;
    text: string;
  } | null>(null);
  const [selectionCustomPrompt, setSelectionCustomPrompt] = useState("");
  const [selectionRewriting, setSelectionRewriting] = useState(false);
  const [selectionStage, setSelectionStage] = useState("");
  const [selectionPreview, setSelectionPreview] = useState<string | null>(null);
  const selectionChannel = useMygoChannel<GenChunk>();

  const mountedRef = useRef(true);
  const activeChapterIdRef = useRef<number | null>(activeChapterId);
  const canvasScrollRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const activeChapterRef = useRef(activeChapter);
  const titleRef = useRef(title);
  const contentRef = useRef(content);
  const dirtyRef = useRef(dirty);
  const generatingRef = useRef(false);

  activeChapterIdRef.current = activeChapterId;
  activeChapterRef.current = activeChapter;
  titleRef.current = title;
  contentRef.current = content;
  dirtyRef.current = dirty;

  // Drawer state
  const [drawerTab, setDrawerTab] = useState<DrawerTab>("audit");

  // Left reference sidebar + rewrite presets + snapshots state (default open in Clear Ink)
  const [refSidebarOpen, setRefSidebarOpen] = useState<boolean>(() => {
    try {
      const saved = window.localStorage.getItem("nib.studio.refSidebarOpen");
      return saved === null ? true : saved === "1";
    } catch {
      return true;
    }
  });
  const [refSidebarTab, setRefSidebarTab] = useState<ReferenceTab>("toc");
  const [focusedEntityId, setFocusedEntityId] = useState<number | null>(null);
  const [chapterBeats, setChapterBeats] = useState<Beat[]>([]);
  const [beatsLoading, setBeatsLoading] = useState(false);
  const [chapterSnapshots, setChapterSnapshots] = useState<EntitySnapshot[]>([]);
  const [snapshotsLoading, setSnapshotsLoading] = useState(false);
  const [rewritePresets, setRewritePresets] = useState<RewritePreset[]>([]);
  const beatsReqIdRef = useRef(0);
  const snapshotsReqIdRef = useRef(0);
  const [briefing, setBriefing] = useState<Briefing | null>(null);
  const [briefingLoading, setBriefingLoading] = useState(false);
  const [briefingError, setBriefingError] = useState<string | null>(null);
  const [expandedSlots, setExpandedSlots] = useState<Set<string>>(new Set());
  const [pinnedEntityIds, setPinnedEntityIds] = useState<Set<number>>(new Set());
  const [excludedEntityIds, setExcludedEntityIds] = useState<Set<number>>(new Set());
  const [extraInstruction, setExtraInstruction] = useState("");
  const [includePending, setIncludePending] = useState(true);
  const [searchKeywordsText, setSearchKeywordsText] = useState("");

  // Pending memory delta state
  const [pendingDelta, setPendingDelta] = useState<ChapterDelta | null>(null);
  const [deltaLoading, setDeltaLoading] = useState(false);
  const deltaReqIdRef = useRef(0);

  // Quality Audit & Surgical Diff state
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
  const [restoringId, setRestoringId] = useState<number | null>(null);

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
  const loadPendingDelta = useCallback(
    async (targetChapterId?: number | null) => {
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
    },
    [activeChapterId]
  );

  useEffect(() => {
    void loadPendingDelta(activeChapterId);
  }, [activeChapterId, loadPendingDelta]);

  // Persist left reference sidebar toggle
  useEffect(() => {
    try {
      window.localStorage.setItem(
        "nib.studio.refSidebarOpen",
        refSidebarOpen ? "1" : "0"
      );
    } catch {
      // ignore storage errors
    }
  }, [refSidebarOpen]);

  // Load book rewrite presets
  useEffect(() => {
    if (!activeBook?.id) {
      setRewritePresets([]);
      return;
    }
    let cancelled = false;
    StyleService.listRewritePresets(activeBook.id)
      .then((list) => {
        if (!cancelled) setRewritePresets(list);
      })
      .catch(() => {
        if (!cancelled) setRewritePresets([]);
      });
    return () => {
      cancelled = true;
    };
  }, [activeBook?.id]);

  // Load current chapter beats for reference beat alignment & entity matching
  useEffect(() => {
    if (!activeChapterId) {
      setChapterBeats([]);
      return;
    }
    const reqId = ++beatsReqIdRef.current;
    setBeatsLoading(true);
    LibraryService.listBeats(activeChapterId)
      .then((list) => {
        if (mountedRef.current && beatsReqIdRef.current === reqId) {
          setChapterBeats(list);
        }
      })
      .catch(() => {
        if (mountedRef.current && beatsReqIdRef.current === reqId) {
          setChapterBeats([]);
        }
      })
      .finally(() => {
        if (mountedRef.current && beatsReqIdRef.current === reqId) {
          setBeatsLoading(false);
        }
      });
  }, [activeChapterId]);

  const loadChapterSnapshots = useCallback(async () => {
    if (!activeBook?.id || !activeChapter) {
      setChapterSnapshots([]);
      return;
    }
    const reqId = ++snapshotsReqIdRef.current;
    setSnapshotsLoading(true);
    try {
      const list = await MemoryService.snapshotAt(
        activeBook.id,
        activeChapter.globalSeq,
        includePending
      );
      if (mountedRef.current && snapshotsReqIdRef.current === reqId) {
        setChapterSnapshots(list);
      }
    } catch {
      if (mountedRef.current && snapshotsReqIdRef.current === reqId) {
        setChapterSnapshots([]);
      }
    } finally {
      if (mountedRef.current && snapshotsReqIdRef.current === reqId) {
        setSnapshotsLoading(false);
      }
    }
  }, [activeBook?.id, activeChapter, includePending]);

  useEffect(() => {
    if (!refSidebarOpen && !readingMode) return;
    void loadChapterSnapshots();
  }, [refSidebarOpen, readingMode, memoryRevision, loadChapterSnapshots]);

  // Load persisted ChapterAudit or run 0-cost local lint
  const runLocalLint = useCallback(
    async (targetChapterId?: number | null, silent = false) => {
      const cid = targetChapterId ?? activeChapterId;
      if (!cid) return;
      const reqId = ++auditReqIdRef.current;
      setAuditLoading(true);
      try {
        const res = await StyleService.lintChapterLocal(cid);
        if (
          mountedRef.current &&
          auditReqIdRef.current === reqId &&
          activeChapterIdRef.current === cid
        ) {
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
        if (
          mountedRef.current &&
          auditReqIdRef.current === reqId &&
          activeChapterIdRef.current === cid
        ) {
          setChapterAudit(audit);
          if (audit.id === 0 && activeChapter?.content?.trim()) {
            void runLocalLint(cid, true);
          }
        }
      } catch {
        if (
          mountedRef.current &&
          auditReqIdRef.current === reqId &&
          activeChapterIdRef.current === cid
        ) {
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
      notify({
        tone: "danger",
        message: "读取历史快照失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setVersionsLoading(false);
    }
  }, [activeChapterId, notify]);

  const studioDrawerOpenRef = useRef(studioDrawerOpen);
  const drawerTabRef = useRef(drawerTab);
  studioDrawerOpenRef.current = studioDrawerOpen;
  drawerTabRef.current = drawerTab;

  const handleGenerationSettled = useCallback(
    (genChapterId: number) => {
      void loadPendingDelta(genChapterId);
      void runLocalLint(genChapterId, true);
      if (studioDrawerOpenRef.current && drawerTabRef.current === "versions") {
        void loadVersions();
      }
      if (studioDrawerOpenRef.current && drawerTabRef.current === "briefing") {
        void loadBriefing();
      }
    },
    [loadPendingDelta, runLocalLint, loadVersions, loadBriefing]
  );

  // Save draft (silent=true for background AutoSave)
  const handleSave = useCallback(
    async (silent = false): Promise<boolean> => {
      if (!activeChapter || generatingRef.current) return false;
      const snapChapterId = activeChapter.id;
      const snapTitle = title.trim() || activeChapter.title;
      const snapContent = content;
      setSaving(true);
      try {
        const saved = await LibraryService.saveChapterDraft({
          chapterId: snapChapterId,
          title: snapTitle,
          content: snapContent,
          backupSource: "manual_backup",
        });
        updateChapterInCache(saved);
        // 仅当保存期间未发生新编辑或切章时才清除 dirty 标记，避免丢失并发输入的修改
        if (
          activeChapterRef.current?.id === snapChapterId &&
          (titleRef.current.trim() || activeChapterRef.current.title) === snapTitle &&
          contentRef.current === snapContent
        ) {
          setDirty(false);
        }
        if (silent) {
          const now = new Date();
          setLastAutoSavedAt(
            `${String(now.getHours()).padStart(2, "0")}:${String(
              now.getMinutes()
            ).padStart(2, "0")}`
          );
        } else {
          setLastAutoSavedAt(null);
        }
        void runLocalLint(saved.id, true);
        if (studioDrawerOpen && drawerTab === "versions") {
          void loadVersions();
        }
        if (!silent) {
          notify({ tone: "ok", message: "正文已保存" });
        }
        return true;
      } catch (err) {
        if (!silent) {
          notify({
            tone: "danger",
            message: "保存正文失败",
            detail: formatErrorMessage(err),
          });
        }
        return false;
      } finally {
        setSaving(false);
      }
    },
    [
      activeChapter,
      title,
      content,
      updateChapterInCache,
      runLocalLint,
      studioDrawerOpen,
      drawerTab,
      loadVersions,
      notify,
    ]
  );

  const saveDraftSilent = useCallback(() => handleSave(true), [handleSave]);

  const {
    generating,
    genStageText,
    genModelName,
    genError,
    setGenError,
    lastUsage,
    startGeneration,
    stopGeneration,
  } = useChapterGeneration({
    activeChapter,
    activeChapterId,
    providers,
    draftRouteInfo,
    content,
    dirty,
    extraInstruction,
    searchKeywordsText,
    includePending,
    pinnedEntityIds,
    excludedEntityIds,
    canvasScrollRef,
    saveDraftSilent,
    updateChapterInCache,
    refreshActiveChapter,
    setTitle,
    setContent,
    setDirty,
    openSettings,
    notify,
    onGenerationSettled: handleGenerationSettled,
  });
  generatingRef.current = generating;

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
            notify({ tone: "info", message: "已取消局部润色" });
          }
          return;
        }
        if (!mountedRef.current || polishRunIdRef.current !== runId) return;
        notify({
          tone: "danger",
          message: "生成局部润色失败",
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
          message: accept ? "已采纳局部润色并保存快照" : "已忽略该条润色建议",
        });
      } catch (err) {
        notify({
          tone: "danger",
          message: "应用局部润色失败",
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
        `${result.committedEvents} 条状态更新`,
        `${result.updatedForeshadowings} 条伏笔更新`,
      ];
      if (result.updatedRelations > 0) {
        summaryParts.push(`${result.updatedRelations} 条关系更新`);
      }
      if (result.updatedDeadlines > 0) {
        summaryParts.push(`${result.updatedDeadlines} 条期限更新`);
      }
      if (dirtyCount > 0) {
        summaryParts.push(`后续 ${dirtyCount} 章已标记待核对`);
      }
      notify({
        tone: dirtyCount > 0 ? "warn" : "ok",
        message: "设定更新已写入",
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

  // Sync editor content and per-chapter briefing overrides when active chapter switches
  useEffect(() => {
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
      setChapterAudit(null);
      setActiveParagraphIndex(null);
      return;
    }
    setTitle(activeChapter.title);
    setContent(activeChapter.content);
    setDirty(false);
    setLastAutoSavedAt(null);
    setSelectionRange(null);
    setSelectionPreview(null);
    setSelectionRewriting(false);
    setSelectionStage("");
    selectionChannel.stop();
    setReadingMode(false);
    setPinnedEntityIds(new Set());
    setExcludedEntityIds(new Set());
    setExtraInstruction("");
    setSearchKeywordsText("");
    setExpandedSlots(new Set());
    setPendingDelta(null);
    setChapterAudit(null);
    setActiveParagraphIndex(null);
    setVersions([]);
    setSelectedVersionId(null);
    setBriefing(null);
    setFocusedEntityId(null);
    setBookReplaceStats(null);
    setCurrentMatchIndex(0);
    setMoreMenuOpen(false);
    return () => {
      if (activeChapter && dirtyRef.current && !generatingRef.current) {
        const snapId = activeChapter.id;
        const snapTitle = titleRef.current.trim() || activeChapter.title;
        const snapContent = contentRef.current;
        void LibraryService.saveChapterDraft({
          chapterId: snapId,
          title: snapTitle,
          content: snapContent,
          backupSource: "manual_backup",
        })
          .then((saved) => updateChapterInCache(saved))
          .catch((err) => {
            notify({
              tone: "danger",
              message: "切章自动保存草稿失败",
              detail: formatErrorMessage(err),
            });
          });
      }
    };
  }, [activeChapter?.id]);

  // Keep synced if chapter updated externally while not editing or streaming
  useEffect(() => {
    if (!activeChapter || dirty || generating) return;
    setTitle(activeChapter.title);
    setContent(activeChapter.content);
  }, [activeChapter?.updatedAt, activeChapter?.content, activeChapter?.title, dirty, generating]);

  // Receive insert-text events from ScratchNotesDrawer
  useEffect(() => {
    const handleInsertText = (event: Event) => {
      const text = (event as CustomEvent<{ text?: string }>).detail?.text;
      if (!text) return;
      const current = contentRef.current;
      const el = textareaRef.current;
      const start = el?.selectionStart ?? current.length;
      const end = el?.selectionEnd ?? current.length;
      const next = current.slice(0, start) + text + current.slice(end);
      setContent(next);
      setDirty(true);
      requestAnimationFrame(() => {
        const target = textareaRef.current;
        if (!target) return;
        target.focus();
        const cursor = start + text.length;
        target.setSelectionRange(cursor, cursor);
      });
    };
    window.addEventListener("nib:insert-text", handleInsertText);
    return () => window.removeEventListener("nib:insert-text", handleInsertText);
  }, []);

  const syncTextareaHeight = useCallback(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.max(el.scrollHeight, 480)}px`;
  }, []);

  useEffect(() => {
    const el = textareaRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => syncTextareaHeight());
    ro.observe(el);
    return () => ro.disconnect();
  }, [activeChapter?.id, generating, readingMode, syncTextareaHeight]);

  useLayoutEffect(() => {
    syncTextareaHeight();
  }, [
    content,
    activeChapter?.id,
    generating,
    readingMode,
    studioDrawerOpen,
    focusMode,
    proseFont,
    proseIndent,
    proseFontSize,
    proseLineHeight,
    proseMaxWidth,
    syncTextareaHeight,
  ]);

  // Flush dirty draft & abort polish on unmount
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      if (activeChapterRef.current && dirtyRef.current && !generatingRef.current) {
        const snapChap = activeChapterRef.current;
        const snapTitle = titleRef.current.trim() || snapChap.title;
        const snapContent = contentRef.current;
        void LibraryService.saveChapterDraft({
          chapterId: snapChap.id,
          title: snapTitle,
          content: snapContent,
          backupSource: "manual_backup",
        })
          .then((saved) => updateChapterInCache(saved))
          .catch(() => {
            // ignore unmount background flush error
          });
      }
      if (polishAbortRef.current) {
        polishRunIdRef.current += 1;
        polishAbortRef.current.abort();
        polishAbortRef.current = null;
      }
    };
  }, []);

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

  // Debounced AutoSave: 3s after user stops typing
  useEffect(() => {
    if (!dirty || generating || saving || !activeChapter) return;
    const timer = window.setTimeout(() => {
      void handleSave(true);
    }, 3000);
    return () => window.clearTimeout(timer);
  }, [dirty, generating, saving, activeChapter, content, title, handleSave]);

  // Close overflow menu on outside click or Escape
  useEffect(() => {
    if (!moreMenuOpen) return;
    const onDown = (e: MouseEvent) => {
      if (moreMenuRef.current && !moreMenuRef.current.contains(e.target as Node)) {
        setMoreMenuOpen(false);
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setMoreMenuOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [moreMenuOpen]);

  // Ctrl+H toggles floating Find & Replace bar
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "h") {
        e.preventDefault();
        setFindOpen((prev) => !prev);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const handleCopyCleanProse = useCallback(async () => {
    const clean = content
      .replace(/\r\n/g, "\n")
      .replace(/^#{1,6}\s+.*$/gm, "")
      .split(/\n+/)
      .map((p) => p.trim())
      .filter(Boolean)
      .join("\n\n");
    if (!clean) {
      notify({ tone: "warn", message: "当前章节暂无可复制的正文" });
      return;
    }
    try {
      await navigator.clipboard.writeText(clean);
      setCopiedClean(true);
      window.setTimeout(() => setCopiedClean(false), 1800);
      notify({ tone: "ok", message: "已复制纯净正文，可直接粘贴发文" });
    } catch (err) {
      notify({ tone: "danger", message: "复制失败", detail: formatErrorMessage(err) });
    }
  }, [content, notify]);

  const handleFormatProse = useCallback(() => {
    if (generating || !content.trim()) return;
    const { text, changed } = formatWebNovelProse(content);
    if (!changed) {
      notify({ tone: "info", message: "当前正文排版已经很规范，无需调整" });
      return;
    }
    // 覆写前先归档原正文快照
    if (activeChapter && content.trim()) {
      void LibraryService.saveChapterDraft({
        chapterId: activeChapter.id,
        title: title.trim() || activeChapter.title,
        content,
        backupSource: "manual_backup",
      }).catch(() => {});
    }
    setContent(text);
    setDirty(true);
    notify({ tone: "ok", message: "已整理标点、引号与段落间距" });
  }, [activeChapter, title, content, generating, notify]);

  // Selection AI micro-edit handlers
  const handleEditorSelect = useCallback(() => {
    const el = textareaRef.current;
    if (!el || generating) return;
    const start = el.selectionStart ?? 0;
    const end = el.selectionEnd ?? 0;
    if (end <= start) {
      if (!selectionRewriting && selectionPreview === null) {
        setSelectionRange(null);
      }
      return;
    }
    const text = el.value.slice(start, end);
    if (text.trim().length < 2) {
      if (!selectionRewriting && selectionPreview === null) {
        setSelectionRange(null);
      }
      return;
    }
    setSelectionRange({ start, end, text });
    setSelectionPreview(null);
  }, [generating, selectionRewriting, selectionPreview]);

  const runSelectionRewrite = useCallback(
    async (
      action: "expand" | "condense" | "dialogue" | "vivid" | "deai" | "custom",
      overrideRange?: { start: number; end: number; text: string },
      customOverride?: string
    ) => {
      const activeRange = overrideRange ?? selectionRange;
      if (!activeChapter || !activeRange || selectionRewriting) return;
      if (overrideRange) {
        setSelectionRange(overrideRange);
      }
      const effectiveCustom = (customOverride ?? selectionCustomPrompt).trim();
      if (action === "custom" && !effectiveCustom) {
        notify({ tone: "warn", message: "请先输入自定义改写要求" });
        return;
      }
      setSelectionRewriting(true);
      setSelectionStage("正在微调选中片段…");
      setSelectionPreview("");

      let buf = "";
      const { channel, runId } = selectionChannel.open((chunk) => {
        if (chunk.kind === "thinking") {
          setSelectionStage("模型思考中…");
        } else if (chunk.kind === "delta" && chunk.delta) {
          buf += chunk.delta;
          setSelectionPreview(buf);
          setSelectionStage("正在输出改写建议…");
        }
      });

      try {
        const res = await StudioService.rewriteSelection(
          {
            chapterId: activeChapter.id,
            selectedText: activeRange.text,
            beforeContext: content.slice(
              Math.max(0, activeRange.start - 360),
              activeRange.start
            ),
            afterContext: content.slice(
              activeRange.end,
              Math.min(content.length, activeRange.end + 360)
            ),
            action,
            customInstruction: action === "custom" ? effectiveCustom : "",
          },
          channel
        );
        if (selectionChannel.isCurrent(runId)) {
          setSelectionPreview(res.rewrittenText);
        }
      } catch (err) {
        if (!selectionChannel.wasStopped()) {
          notify({
            tone: "danger",
            message: "选区微调失败",
            detail: formatErrorMessage(err),
          });
          setSelectionPreview(null);
        }
      } finally {
        selectionChannel.release(channel);
        if (selectionChannel.isCurrent(runId)) {
          setSelectionRewriting(false);
          setSelectionStage("");
        }
      }
    },
    [
      activeChapter,
      selectionRange,
      selectionRewriting,
      selectionCustomPrompt,
      content,
      selectionChannel,
      notify,
    ]
  );

  const applySelectionRewrite = useCallback(() => {
    if (!selectionRange || selectionPreview === null) return;
    let start = selectionRange.start;
    let end = selectionRange.end;
    if (content.slice(start, end) !== selectionRange.text) {
      const firstIdx = content.indexOf(selectionRange.text);
      const secondIdx =
        firstIdx >= 0 ? content.indexOf(selectionRange.text, firstIdx + 1) : -1;
      if (firstIdx >= 0 && secondIdx < 0) {
        start = firstIdx;
        end = firstIdx + selectionRange.text.length;
      } else {
        notify({
          tone: "warn",
          message: "正文内容已发生变动，请重新划选片段后再替换",
        });
        return;
      }
    }
    // 覆写前先归档原正文快照，确保历史版本可完整回退
    if (activeChapter && content.trim()) {
      void LibraryService.saveChapterDraft({
        chapterId: activeChapter.id,
        title: title.trim() || activeChapter.title,
        content,
        backupSource: "surgical_polish",
      }).catch(() => {});
    }
    const nextContent =
      content.slice(0, start) + selectionPreview + content.slice(end);
    setContent(nextContent);
    setDirty(true);
    setSelectionRange(null);
    setSelectionPreview(null);
    setSelectionCustomPrompt("");
    notify({ tone: "ok", message: "已替换选中片段，原文已自动留存快照" });
  }, [activeChapter, title, selectionRange, selectionPreview, content, notify]);

  const handleSaveRewritePreset = useCallback(
    async (label: string, instruction: string): Promise<boolean> => {
      if (!activeBook) return false;
      try {
        const saved = await StyleService.upsertRewritePreset({
          id: 0,
          bookId: activeBook.id,
          label,
          instruction,
          sortOrder: rewritePresets.length + 1,
        });
        setRewritePresets((prev) => [...prev, saved]);
        notify({ tone: "ok", message: `已保存常用指令「${saved.label}」` });
        return true;
      } catch (err) {
        notify({
          tone: "danger",
          message: "保存常用指令失败",
          detail: formatErrorMessage(err),
        });
        return false;
      }
    },
    [activeBook, rewritePresets.length, notify]
  );

  const handleDeleteRewritePreset = useCallback(
    async (id: number) => {
      try {
        await StyleService.deleteRewritePreset(id);
        setRewritePresets((prev) => prev.filter((p) => p.id !== id));
        notify({ tone: "info", message: "已删除常用指令" });
      } catch (err) {
        notify({
          tone: "danger",
          message: "删除常用指令失败",
          detail: formatErrorMessage(err),
        });
      }
    },
    [notify]
  );

  const handleInsertEntityName = useCallback(
    (name: string) => {
      if (generating || readingMode) return;
      const el = textareaRef.current;
      if (!el) {
        setContent((prev) => prev + name);
        setDirty(true);
        return;
      }
      const start = el.selectionStart ?? content.length;
      const end = el.selectionEnd ?? content.length;
      const next = content.slice(0, start) + name + content.slice(end);
      setContent(next);
      setDirty(true);
      const caret = start + name.length;
      requestAnimationFrame(() => {
        el.focus();
        el.setSelectionRange(caret, caret);
      });
    },
    [generating, readingMode, content]
  );

  const handleQuickReplaceIssue = useCallback(
    async (issue: LintIssue, replacement: string) => {
      if (!activeChapter || generating) return;
      const res = applyLintReplacementInContent(content, issue, replacement);
      if (!res.replaced) {
        notify({
          tone: "warn",
          message: "正文已变化，未能定位到原词，请先重新执行本地扫描",
        });
        return;
      }
      setContent(res.content);
      setDirty(false);
      try {
        const saved = await LibraryService.saveChapterDraft({
          chapterId: activeChapter.id,
          title: title.trim() || activeChapter.title,
          content: res.content,
          backupSource: "manual_backup",
        });
        updateChapterInCache(saved);
        await runLocalLint(activeChapter.id, true);
        notify({
          tone: "ok",
          message: replacement
            ? `已替换为「${replacement}」并更新本地扫描`
            : "已删去目标词条并更新本地扫描",
        });
      } catch (err) {
        setDirty(true);
        notify({
          tone: "danger",
          message: "替换后保存失败",
          detail: formatErrorMessage(err),
        });
      }
    },
    [
      activeChapter,
      generating,
      content,
      title,
      updateChapterInCache,
      runLocalLint,
      notify,
    ]
  );

  const handleInspectEntityFromProse = useCallback((snapshot: EntitySnapshot) => {
    setFocusedEntityId(snapshot.entity.id);
    setRefSidebarTab("entities");
    setRefSidebarOpen(true);
  }, []);

  // Chapter & whole-book find/replace helpers
  const chapterMatchPositions = useMemo(() => {
    if (!findQuery) return [];
    const positions: number[] = [];
    let start = 0;
    while (start <= content.length) {
      const idx = content.indexOf(findQuery, start);
      if (idx < 0) break;
      positions.push(idx);
      start = idx + findQuery.length;
    }
    return positions;
  }, [content, findQuery]);

  const chapterFindMatches = chapterMatchPositions.length;

  const handleJumpMatch = useCallback(
    (delta: -1 | 1) => {
      if (chapterMatchPositions.length === 0 || !findQuery) return;
      if (readingMode) setReadingMode(false);
      const nextIndex =
        (currentMatchIndex + delta + chapterMatchPositions.length) %
        chapterMatchPositions.length;
      setCurrentMatchIndex(nextIndex);
      const pos = chapterMatchPositions[nextIndex]!;
      requestAnimationFrame(() => {
        const el = textareaRef.current;
        if (!el) return;
        el.focus();
        el.setSelectionRange(pos, pos + findQuery.length);
        // 近似滚动到匹配行附近
        const beforeLines = content.slice(0, pos).split("\n").length;
        const totalLines = Math.max(1, content.split("\n").length);
        if (canvasScrollRef.current) {
          const targetScroll =
            (beforeLines / totalLines) * canvasScrollRef.current.scrollHeight - 180;
          canvasScrollRef.current.scrollTo({
            top: Math.max(0, targetScroll),
            behavior: "smooth",
          });
        }
      });
    },
    [chapterMatchPositions, currentMatchIndex, findQuery, readingMode, content]
  );

  const handleReplaceInChapter = useCallback(() => {
    if (!findQuery) return;
    const count = content.split(findQuery).length - 1;
    if (count === 0) {
      notify({ tone: "warn", message: "当前章节未找到匹配内容" });
      return;
    }
    if (activeChapter && content.trim()) {
      void LibraryService.saveChapterDraft({
        chapterId: activeChapter.id,
        title: title.trim() || activeChapter.title,
        content,
        backupSource: "manual_backup",
      }).catch(() => {});
    }
    setContent(content.split(findQuery).join(replaceText));
    setDirty(true);
    setCurrentMatchIndex(0);
    notify({ tone: "ok", message: `已在本章替换 ${count} 处` });
  }, [activeChapter, title, content, findQuery, replaceText, notify]);

  const handlePreviewBookReplace = useCallback(async () => {
    if (!activeBook || !findQuery) return;
    setReplacingBook(true);
    try {
      const res = await LibraryService.replaceBookText({
        bookId: activeBook.id,
        find: findQuery,
        replace: replaceText,
        includeTitles: true,
        includeOutlines: true,
        dryRun: true,
      });
      setBookReplaceStats(res);
    } catch (err) {
      notify({
        tone: "danger",
        message: "统计全书匹配失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setReplacingBook(false);
    }
  }, [activeBook, findQuery, replaceText, notify]);

  const handleExecuteBookReplace = useCallback(async () => {
    if (!activeBook || !findQuery) return;
    setReplacingBook(true);
    try {
      // 全书替换前先将当前章未保存修改落盘，防止丢稿
      if (activeChapter && dirty) {
        const saved = await LibraryService.saveChapterDraft({
          chapterId: activeChapter.id,
          title: title.trim() || activeChapter.title,
          content,
          backupSource: "manual_backup",
        });
        updateChapterInCache(saved);
        setDirty(false);
      }
      const res = await LibraryService.replaceBookText({
        bookId: activeBook.id,
        find: findQuery,
        replace: replaceText,
        includeTitles: true,
        includeOutlines: true,
        dryRun: false,
      });
      setBookReplaceStats(null);
      await refreshChapters(activeBook.id);
      if (activeChapter) {
        const fresh = await refreshActiveChapter(activeChapter.id);
        if (fresh) {
          setTitle(fresh.title);
          setContent(fresh.content);
          setDirty(false);
        }
      }
      notify({
        tone: "ok",
        message: `全书替换完成：共修改 ${res.affectedChapters} 章、${res.totalMatches} 处`,
        detail: "修改前已自动归档每章历史快照",
      });
    } catch (err) {
      notify({
        tone: "danger",
        message: "全书替换失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setReplacingBook(false);
    }
  }, [
    activeBook,
    activeChapter,
    dirty,
    title,
    content,
    updateChapterInCache,
    findQuery,
    replaceText,
    refreshChapters,
    refreshActiveChapter,
    notify,
  ]);

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

  const handleExportBook = useCallback(async () => {
    if (!activeBook) return;
    if (!activeBook.exportDir?.trim()) {
      notify({
        tone: "warn",
        message: "尚未设置本地 Markdown 导出目录",
        actionLabel: "前往本书设置",
        onAction: () => setActiveTab("book_settings"),
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
  }, [activeBook, notify, setActiveTab]);

  const toggleGenerate = useCallback(() => {
    if (generating) stopGeneration();
    else void startGeneration();
  }, [generating, stopGeneration, startGeneration]);

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

  const handleRestoreVersion = async (versionId: number) => {
    if (generating) return;
    setRestoringId(versionId);
    try {
      const restored = await StudioService.restoreVersion(versionId);
      updateChapterInCache(restored);
      setTitle(restored.title);
      setContent(restored.content);
      setDirty(false);
      await loadVersions();
      notify({
        tone: "ok",
        message: "已恢复到此历史版本",
        detail: "原正文已自动存入历史版本",
      });
    } catch (err) {
      notify({
        tone: "danger",
        message: "回滚快照失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setRestoringId(null);
    }
  };

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
    () => (generating || readingMode ? splitParagraphs(content) : []),
    [content, generating, readingMode]
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
      const idx = list.indexOf(activeParagraphIndex);
      if (idx === -1) {
        const first = dir === 1 ? list[0] : list[list.length - 1];
        if (first !== undefined) scrollToParagraph(first);
        return;
      }
      const nextPos = (idx + dir + list.length) % list.length;
      const target = list[nextPos];
      if (target !== undefined) scrollToParagraph(target);
    },
    [paragraphAuditMaps.flaggedParagraphs, activeParagraphIndex, scrollToParagraph]
  );

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

  const handleInsertHistoricalParagraph = useCallback(
    (para: string) => {
      const trimmed = para.trim();
      if (!trimmed || generating) return;
      const base = content.trimEnd();
      const next = base ? `${base}\n\n${trimmed}` : trimmed;
      setContent(next);
      setDirty(true);
      notify({ tone: "ok", message: "已将该历史段落插回当前正文末尾" });
    },
    [content, generating, notify]
  );

  const handleTextareaContextMenu = useCallback(
    (e: ReactMouseEvent<HTMLTextAreaElement>) => {
      const el = textareaRef.current;
      if (!el || !activeChapter) return;
      const start = el.selectionStart ?? 0;
      const end = el.selectionEnd ?? 0;
      const hasSelection = end > start;
      const selectedText = hasSelection ? el.value.slice(start, end) : "";
      const aiEligible = !generating && selectedText.trim().length >= 2;
      const range = aiEligible ? { start, end, text: selectedText } : null;
      if (range) setSelectionRange(range);

      const items: ContextMenuItem[] = [];
      if (range) {
        items.push({
          type: "label",
          label: `AI 选区微调（${countWords(range.text)} 字）`,
        });
        const actions: Array<{
          key: "expand" | "condense" | "dialogue" | "vivid" | "deai";
          label: string;
        }> = [
          { key: "expand", label: "扩写细节" },
          { key: "condense", label: "精简紧凑" },
          { key: "dialogue", label: "优化对话" },
          { key: "vivid", label: "增强动作感" },
          { key: "deai", label: "去 AI 味" },
        ];
        for (const act of actions) {
          items.push({
            label: act.label,
            icon: <Sparkles className="h-3.5 w-3.5" />,
            disabled: selectionRewriting,
            onSelect: () => void runSelectionRewrite(act.key, range),
          });
        }
        items.push({ type: "separator" });
      }

      items.push(
        {
          label: "剪切",
          icon: <Scissors className="h-3.5 w-3.5" />,
          shortcut: "Ctrl+X",
          disabled: !hasSelection || generating,
          onSelect: async () => {
            if (!hasSelection) return;
            try {
              await navigator.clipboard.writeText(selectedText);
            } catch {
              // ignore
            }
            const next = content.slice(0, start) + content.slice(end);
            setContent(next);
            setDirty(true);
            setSelectionRange(null);
            requestAnimationFrame(() => {
              el.focus();
              el.setSelectionRange(start, start);
            });
          },
        },
        {
          label: "复制",
          icon: <Copy className="h-3.5 w-3.5" />,
          shortcut: "Ctrl+C",
          disabled: !hasSelection,
          onSelect: async () => {
            if (!hasSelection) return;
            try {
              await navigator.clipboard.writeText(selectedText);
            } catch {
              // ignore
            }
          },
        },
        {
          label: "粘贴",
          icon: <Clipboard className="h-3.5 w-3.5" />,
          shortcut: "Ctrl+V",
          disabled: generating,
          onSelect: async () => {
            let pasted = "";
            try {
              pasted = await navigator.clipboard.readText();
            } catch {
              return;
            }
            if (!pasted) return;
            const next = content.slice(0, start) + pasted + content.slice(end);
            setContent(next);
            setDirty(true);
            const caret = start + pasted.length;
            requestAnimationFrame(() => {
              el.focus();
              el.setSelectionRange(caret, caret);
            });
          },
        },
        {
          label: "全选",
          icon: <TextSelect className="h-3.5 w-3.5" />,
          shortcut: "Ctrl+A",
          disabled: content.length === 0,
          onSelect: () => {
            el.focus();
            el.select();
          },
        },
        { type: "separator" }
      );

      if (hasSelection && selectedText.trim().length > 0 && selectedText.length <= 80) {
        items.push({
          label: "在文中查找 / 替换选中词",
          icon: <Search className="h-3.5 w-3.5" />,
          shortcut: "Ctrl+H",
          onSelect: () => {
            setFindQuery(selectedText.trim());
            setFindOpen(true);
          },
        });
      } else {
        if (generating) {
          items.push({
            label: "停止生成",
            icon: <Square className="h-3.5 w-3.5" />,
            shortcut: "Ctrl+Enter",
            danger: true,
            onSelect: () => stopGeneration(),
          });
        } else if (content.trim().length > 0) {
          items.push(
            {
              label: "紧接结尾续写",
              icon: <FastForward className="h-3.5 w-3.5" />,
              onSelect: () => void startGeneration("continue"),
            },
            {
              label: "按节拍重新生成",
              icon: <RefreshCw className="h-3.5 w-3.5" />,
              onSelect: () => void startGeneration("rewrite"),
            }
          );
        } else {
          items.push({
            label: "生成本章",
            icon: <Play className="h-3.5 w-3.5" />,
            shortcut: "Ctrl+Enter",
            onSelect: () => void startGeneration("rewrite"),
          });
        }
        items.push(
          { type: "separator" },
          {
            label: "保存正文",
            icon: <Save className="h-3.5 w-3.5" />,
            shortcut: "Ctrl+S",
            disabled: generating || saving,
            onSelect: () => void handleSave(),
          },
          {
            label: "复制纯净正文",
            icon: <Copy className="h-3.5 w-3.5" />,
            disabled: !content.trim(),
            onSelect: () => void handleCopyCleanProse(),
          },
          {
            label: "一键整理排版",
            icon: <Wand2 className="h-3.5 w-3.5" />,
            disabled: generating || !content.trim(),
            onSelect: () => handleFormatProse(),
          },
          {
            label: "查找与替换",
            icon: <Search className="h-3.5 w-3.5" />,
            shortcut: "Ctrl+H",
            onSelect: () => setFindOpen(true),
          },
          {
            label: "切换校对视图",
            icon: <BookOpen className="h-3.5 w-3.5" />,
            disabled: generating,
            onSelect: () => setReadingMode(true),
          }
        );
      }

      openContextMenu(e, items);
    },
    [
      activeChapter,
      generating,
      selectionRewriting,
      content,
      saving,
      runSelectionRewrite,
      stopGeneration,
      startGeneration,
      handleSave,
      handleCopyCleanProse,
      handleFormatProse,
      openContextMenu,
    ]
  );

  const handleReadingContextMenu = useCallback(
    (e: ReactMouseEvent<HTMLElement>) => {
      if (!activeChapter) return;
      const selectedText = window.getSelection()?.toString() ?? "";
      const items: ContextMenuItem[] = [];
      if (selectedText.trim().length > 0) {
        items.push({
          label: "复制选中文本",
          icon: <Copy className="h-3.5 w-3.5" />,
          shortcut: "Ctrl+C",
          onSelect: async () => {
            try {
              await navigator.clipboard.writeText(selectedText);
            } catch {
              // ignore
            }
          },
        });
        if (selectedText.length <= 80) {
          items.push({
            label: "在文中查找 / 替换选中词",
            icon: <Search className="h-3.5 w-3.5" />,
            shortcut: "Ctrl+H",
            onSelect: () => {
              setFindQuery(selectedText.trim());
              setFindOpen(true);
            },
          });
        }
        items.push({ type: "separator" });
      }
      if (!generating) {
        items.push({
          label: "切回编辑模式",
          icon: <Pencil className="h-3.5 w-3.5" />,
          onSelect: () => setReadingMode(false),
        });
      }
      items.push(
        {
          label: "复制纯净正文",
          icon: <Copy className="h-3.5 w-3.5" />,
          disabled: !content.trim(),
          onSelect: () => void handleCopyCleanProse(),
        },
        {
          label: "本地 AI 味扫描",
          icon: <ShieldCheck className="h-3.5 w-3.5" />,
          disabled: generating || !content.trim(),
          onSelect: () => {
            setDrawerTab("audit");
            setStudioDrawerOpen(true);
            void runLocalLint(activeChapter.id, false);
          },
        },
        {
          label: "查找与替换",
          icon: <Search className="h-3.5 w-3.5" />,
          shortcut: "Ctrl+H",
          onSelect: () => setFindOpen(true),
        }
      );
      openContextMenu(e, items);
    },
    [
      activeChapter,
      generating,
      content,
      handleCopyCleanProse,
      runLocalLint,
      setStudioDrawerOpen,
      openContextMenu,
    ]
  );

  if (!activeChapter) {
    return (
      <div className="nib-paper-sheet m-2 mt-0 flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
        <div className="text-sm text-ink-secondary">当前书籍尚无章节</div>
        <button
          type="button"
          onClick={() => setActiveTab("architect")}
          className="rounded-lg bg-accent px-4 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90"
        >
          前往大纲台新建章节
        </button>
      </div>
    );
  }

  const statusMeta = getChapterStatusMeta(activeChapter.status);

  return (
    <div className="relative flex min-h-0 flex-1 overflow-hidden">
      {/* 专注写作模式顶部隐形拖拽栏 + 右上角悬浮微弱状态条（避开原生窗口控制按钮） */}
      {focusMode && (
        <div
          className="nib-drag-region fixed top-0 right-0 left-0 z-30 flex items-center justify-end"
          style={{
            height: "var(--mygo-titlebar-height, var(--topbar-height))",
            paddingLeft: "calc(12px + var(--mygo-titlebar-inset-left, 0px))",
            paddingRight: "calc(12px + var(--mygo-titlebar-inset-right, 0px))",
          }}
        >
          <div className="nib-no-drag nib-floating-dock flex items-center gap-2 rounded-full px-3 py-1 text-xs text-ink-secondary opacity-75 transition-opacity hover:opacity-100">
            <span className="nib-mono text-ink-primary">{formatNumber(liveWordCount)} 字</span>
            <span className="h-3 w-px bg-border-subtle" />
            <button
              type="button"
              onClick={() => setFocusMode(false)}
              title="退出专注模式 (Esc / F11)"
              className="flex items-center gap-1 text-ink-secondary transition-colors hover:text-accent"
            >
              <Minimize2 className="h-3 w-3" />
              <span>退出专注</span>
            </button>
          </div>
        </div>
      )}

      {/* Left 3-in-1 Collapsible Reference Sidebar (History / Beats / Entity Snapshot) */}
      {!focusMode && refSidebarOpen && (
        <StudioReferenceSidebar
          activeChapter={activeChapter}
          chapters={chapters}
          currentContent={content}
          readingMode={readingMode}
          generating={generating}
          beats={chapterBeats}
          beatsLoading={beatsLoading}
          entities={entities}
          snapshots={chapterSnapshots}
          snapshotsLoading={snapshotsLoading}
          includePending={includePending}
          foreshadowings={foreshadowings}
          criticFindings={chapterAudit?.criticFindings ?? []}
          activeTab={refSidebarTab}
          focusedEntityId={focusedEntityId}
          onChangeTab={setRefSidebarTab}
          onChangeIncludePending={setIncludePending}
          onRefreshSnapshots={() => void loadChapterSnapshots()}
          onClose={() => setRefSidebarOpen(false)}
          onSwitchChapter={(cid) => void selectChapter(cid)}
          onFocusParagraph={scrollToParagraph}
          onInsertEntityName={handleInsertEntityName}
          onNotify={notify}
        />
      )}

      {/* Main Floating Paper Sheet + Translucent Action Dock */}
      <div
        className={`relative flex min-w-0 flex-1 flex-col overflow-hidden ${
          focusMode
            ? "bg-canvas"
            : `nib-paper-sheet m-2 mt-0 ${refSidebarOpen ? "ml-0" : ""}`
        }`}
      >
        {/* Floating Find & Replace Bar (zero layout shift) */}
        {findOpen && (
          <div
            className={`pointer-events-none absolute right-5 left-5 z-20 flex justify-end ${
              focusMode ? "top-3" : "top-13"
            }`}
          >
            <FindReplaceBar
              findScope={findScope}
              findQuery={findQuery}
              replaceText={replaceText}
              chapterFindMatches={chapterFindMatches}
              currentMatchIndex={currentMatchIndex}
              bookReplaceStats={bookReplaceStats}
              replacingBook={replacingBook}
              onChangeScope={(scope) => {
                setFindScope(scope);
                setBookReplaceStats(null);
              }}
              onChangeQuery={(val) => {
                setFindQuery(val);
                setCurrentMatchIndex(0);
                setBookReplaceStats(null);
              }}
              onChangeReplace={setReplaceText}
              onJumpMatch={handleJumpMatch}
              onReplaceInChapter={handleReplaceInChapter}
              onPreviewBookReplace={() => void handlePreviewBookReplace()}
              onExecuteBookReplace={() => void handleExecuteBookReplace()}
              onClose={() => {
                setFindOpen(false);
                setBookReplaceStats(null);
              }}
            />
          </div>
        )}

        {/* Fixed Top Chapter Meta, Status & Panel Controls Bar */}
        {!focusMode && (
          <div className="relative z-10 flex shrink-0 items-center justify-between gap-2 border-b border-border-subtle/70 px-4 py-2 text-xs text-ink-muted select-none">
            <div className="flex min-w-0 items-center gap-2">
              <button
                type="button"
                onClick={() => setRefSidebarOpen((v) => !v)}
                title={refSidebarOpen ? "收起左侧目录与对照栏" : "展开左侧目录与对照栏"}
                aria-label="切换左侧目录栏"
                className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-lg transition-colors ${
                  refSidebarOpen
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:bg-[var(--bg-hover)] hover:text-ink-primary"
                }`}
              >
                <PanelLeft className="h-3.5 w-3.5" />
              </button>
              {activeVolumeTitle && (
                <span className="truncate text-ink-muted">{activeVolumeTitle}</span>
              )}
              {activeVolumeTitle && <span aria-hidden="true">·</span>}
              <span className="nib-mono shrink-0 text-ink-secondary">
                第 {formatSeq(activeChapter.globalSeq)} 章
              </span>
              <span
                className={`shrink-0 rounded-md px-1.5 py-0.5 text-[11px] font-medium ${statusMeta.badgeClass}`}
              >
                {statusMeta.label}
              </span>
              {chapters.length > 1 && (
                <div className="ml-0.5 flex shrink-0 items-center gap-0.5">
                  <button
                    type="button"
                    disabled={generating || currentChapterIndex <= 0}
                    onClick={() => {
                      const prevChap = chapters[currentChapterIndex - 1];
                      if (prevChap) void selectChapter(prevChap.id);
                    }}
                    title="上一章"
                    aria-label="上一章"
                    className="rounded-md p-1 text-ink-muted transition-colors hover:bg-[var(--bg-hover)] hover:text-ink-primary disabled:opacity-30"
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
                    aria-label="下一章"
                    className="rounded-md p-1 text-ink-muted transition-colors hover:bg-[var(--bg-hover)] hover:text-ink-primary disabled:opacity-30"
                  >
                    <ChevronRight className="h-3.5 w-3.5" />
                  </button>
                </div>
              )}
            </div>

            <div className="flex shrink-0 items-center gap-1.5">
              {paragraphAuditMaps.flaggedParagraphs.length > 0 && (
                <div className="flex items-center gap-0.5 rounded-lg bg-warn-soft/75 px-1.5 py-0.5">
                  <button
                    type="button"
                    onClick={() => cycleFlaggedParagraph(-1)}
                    title="上一处建议 (Alt+[)"
                    aria-label="上一处建议"
                    className="rounded px-1 py-0.5 text-xs text-warn hover:bg-warn-soft"
                  >
                    ‹
                  </button>
                  <button
                    type="button"
                    onClick={() => cycleFlaggedParagraph(1)}
                    title="查看问题段落 (Alt+])"
                    className="nib-mono px-1 text-xs font-medium text-warn"
                  >
                    {paragraphAuditMaps.flaggedParagraphs.length} 段待改
                  </button>
                  <button
                    type="button"
                    onClick={() => cycleFlaggedParagraph(1)}
                    title="下一处建议 (Alt+])"
                    aria-label="下一处建议"
                    className="rounded px-1 py-0.5 text-xs text-warn hover:bg-warn-soft"
                  >
                    ›
                  </button>
                </div>
              )}

              <div className="flex items-center gap-1.5 px-1 text-[11px]">
                <span className="nib-mono font-medium text-ink-secondary">
                  {formatNumber(liveWordCount)} 字
                </span>
                <span className="text-border-strong" aria-hidden="true">
                  ·
                </span>
                <span>
                  {generating ? (
                    <span className="text-accent">写作中</span>
                  ) : saving ? (
                    <span className="text-ink-muted">保存中…</span>
                  ) : dirty ? (
                    <span className="text-warn">未保存</span>
                  ) : activeChapter.status === "partial" ? (
                    <span className="text-warn">未写完</span>
                  ) : lastAutoSavedAt ? (
                    <span className="text-ink-muted">已存 {lastAutoSavedAt}</span>
                  ) : (
                    <span className="text-ink-muted">已保存</span>
                  )}
                </span>
              </div>

              <span className="h-3.5 w-px bg-border-subtle" aria-hidden="true" />

              {/* Overflow Utility Menu (···) */}
              <div ref={moreMenuRef} className="relative">
                <button
                  type="button"
                  onClick={() => setMoreMenuOpen((v) => !v)}
                  title="更多工具（重写 / 查找替换 / 排版 / 复制 / 导出）"
                  aria-label="更多写作工具"
                  className={`flex h-7 w-7 items-center justify-center rounded-lg transition-colors ${
                    moreMenuOpen || findOpen
                      ? "bg-accent-soft text-accent"
                      : "text-ink-secondary hover:bg-[var(--bg-hover)] hover:text-ink-primary"
                  }`}
                >
                  <MoreHorizontal className="h-3.5 w-3.5" />
                </button>

                {moreMenuOpen && (
                  <div className="nib-floating-dock absolute top-[calc(100%+6px)] right-0 z-30 w-52 rounded-xl py-1.5">
                    {!generating && content.trim().length > 0 && (
                      <>
                        <button
                          type="button"
                          onClick={() => {
                            setMoreMenuOpen(false);
                            void startGeneration("rewrite");
                          }}
                          className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs text-ink-primary transition-colors hover:bg-[var(--bg-hover)]"
                        >
                          <RefreshCw className="h-3.5 w-3.5 text-ink-secondary" />
                          <span>存档并按节拍重写本章</span>
                        </button>
                        <div className="my-1 border-t border-border-subtle" />
                      </>
                    )}
                    <button
                      type="button"
                      onClick={() => {
                        setMoreMenuOpen(false);
                        setFindOpen((v) => !v);
                      }}
                      className="flex w-full items-center justify-between px-3 py-1.5 text-left text-xs text-ink-primary transition-colors hover:bg-[var(--bg-hover)]"
                    >
                      <span className="flex items-center gap-2">
                        <Search className="h-3.5 w-3.5 text-ink-secondary" />
                        <span>查找与替换</span>
                      </span>
                      <span className="nib-mono text-[11px] text-ink-muted">Ctrl+H</span>
                    </button>
                    <button
                      type="button"
                      disabled={generating || !content.trim()}
                      onClick={() => {
                        setMoreMenuOpen(false);
                        handleFormatProse();
                      }}
                      className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs text-ink-primary transition-colors hover:bg-[var(--bg-hover)] disabled:opacity-40"
                    >
                      <Wand2 className="h-3.5 w-3.5 text-ink-secondary" />
                      <span>规范标点与段落排版</span>
                    </button>
                    <button
                      type="button"
                      disabled={generating || !content.trim()}
                      onClick={() => {
                        setMoreMenuOpen(false);
                        void handleCopyCleanProse();
                      }}
                      className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs text-ink-primary transition-colors hover:bg-[var(--bg-hover)] disabled:opacity-40"
                    >
                      {copiedClean ? (
                        <Check className="h-3.5 w-3.5 text-ok" />
                      ) : (
                        <Copy className="h-3.5 w-3.5 text-ink-secondary" />
                      )}
                      <span>{copiedClean ? "已复制纯净正文" : "复制纯净正文"}</span>
                    </button>
                    <div className="my-1 border-t border-border-subtle" />
                    <button
                      type="button"
                      disabled={exporting}
                      onClick={() => {
                        setMoreMenuOpen(false);
                        void handleExportBook();
                      }}
                      className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs text-ink-primary transition-colors hover:bg-[var(--bg-hover)] disabled:opacity-40"
                    >
                      <Download className="h-3.5 w-3.5 text-ink-secondary" />
                      <span>{exporting ? "导出中…" : "导出全书 Markdown"}</span>
                    </button>
                  </div>
                )}
              </div>

              <button
                type="button"
                onClick={toggleStudioDrawer}
                title="折叠 / 展开右侧抽屉 (Ctrl+\)"
                aria-label="切换右侧抽屉"
                className={`flex h-7 w-7 items-center justify-center rounded-lg transition-colors ${
                  studioDrawerOpen
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:bg-[var(--bg-hover)] hover:text-ink-primary"
                }`}
              >
                <PanelRight className="h-3.5 w-3.5" />
              </button>
            </div>
          </div>
        )}

        <div
          ref={canvasScrollRef}
          className={`nib-scroll-stable flex-1 overflow-y-auto px-10 pb-32 ${focusMode ? "pt-16" : "pt-7"}`}
        >
          <div className="mx-auto w-full max-w-[var(--prose-max-width)]">
            {/* Pure Calm Chapter Title (Zero Competing Action Buttons) */}
            <div className="mb-7">
              {focusMode && (
                <div className="nib-mono mb-2 text-xs text-ink-muted select-none">
                  第 {formatSeq(activeChapter.globalSeq)} 章
                </div>
              )}
              <input
                type="text"
                value={title}
                readOnly={generating}
                onChange={(e) => {
                  setTitle(e.target.value);
                  setDirty(true);
                }}
                placeholder="输入章节标题"
                className="w-full bg-transparent font-prose text-[28px] leading-snug font-medium text-ink-primary placeholder:text-ink-muted/60 focus:outline-none"
              />
            </div>

            {/* Contextual inline banners (deduplicated actions) */}
            {generating && (
              <div className="mb-5 flex items-center gap-2 rounded-lg border border-accent/30 bg-accent-soft px-3.5 py-2 text-xs text-accent">
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                <span>{genStageText || "正在流式出稿…"}</span>
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
                    aria-label="关闭错误提示"
                    className="text-danger/70 hover:text-danger"
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                </div>
              </div>
            )}

            {!generating && !genError && activeChapter.status === "upstream_dirty" && (
              <div className="mb-5 flex items-center justify-between gap-3 rounded-lg border border-warn/35 bg-warn-soft/60 px-3.5 py-2 text-xs text-warn">
                <div className="flex items-center gap-2">
                  <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
                  <span>前文设定有变动，建议核对上下文或复核本章正文</span>
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
                  查看上下文
                </button>
              </div>
            )}

            {!generating && !genError && activeChapter.status === "partial" && (
              <div className="mb-5 flex items-center gap-2 rounded-lg border border-warn/30 bg-warn-soft px-3.5 py-2 text-xs text-warn">
                <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
                <span>
                  上次生成停在 <span className="nib-mono">{formatNumber(liveWordCount)}</span> 字，可继续手改或点上方「续写 / 重写」
                </span>
              </div>
            )}

            {/* Prose Editor / Live Stream Surface / Interactive Paragraph Audit */}
            {generating || readingMode ? (
              <article
                className="nib-prose-column min-h-[480px]"
                onContextMenu={handleReadingContextMenu}
                onDoubleClick={() => {
                  if (!generating && readingMode) setReadingMode(false);
                }}
              >
                {streamingParagraphs.length === 0 ? (
                  <p className="text-ink-muted">
                    {generating ? "等待首字输出…" : "本章暂无正文，双击进入写作模式"}
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
                        generating ? [] : paragraphAuditMaps.lintByPara.get(idx) ?? []
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
                      entitySnapshots={chapterSnapshots}
                      onFocus={(pIdx) => setActiveParagraphIndex(pIdx)}
                      onRequestPolish={(pIdx) =>
                        void handleRunSurgicalPolish([pIdx])
                      }
                      onApplyDiff={(diffId, accept, customRep) =>
                        void handleApplyInlineDiff(diffId, accept, customRep)
                      }
                      onQuickReplaceIssue={(iss, rep) =>
                        void handleQuickReplaceIssue(iss, rep)
                      }
                      onInspectEntity={handleInspectEntityFromProse}
                    />
                  ))
                )}
              </article>
            ) : (
              <textarea
                ref={textareaRef}
                value={content}
                onSelect={handleEditorSelect}
                onMouseUp={handleEditorSelect}
                onKeyUp={handleEditorSelect}
                onContextMenu={handleTextareaContextMenu}
                onChange={(e) => {
                  setContent(e.target.value);
                  setDirty(true);
                }}
                placeholder="在此直接落笔，或按 Ctrl+Enter 按大纲节拍生成本章…"
                spellCheck={false}
                className="nib-prose-column block min-h-[480px] resize-none overflow-hidden bg-transparent placeholder:text-ink-muted/70 focus:outline-none"
              />
            )}
          </div>
        </div>

        {/* Floating Selection Rewrite Bar (sits cleanly above the floating dock) */}
        {!generating && !readingMode && selectionRange && (
          <div className="pointer-events-none absolute right-6 bottom-16 left-6 z-20 flex justify-center">
            <SelectionRewriteBar
              selectedText={selectionRange.text}
              selectionStage={selectionStage}
              selectionRewriting={selectionRewriting}
              selectionCustomPrompt={selectionCustomPrompt}
              selectionPreview={selectionPreview}
              presets={rewritePresets}
              onChangeCustomPrompt={setSelectionCustomPrompt}
              onRunAction={(action, customOverride) =>
                void runSelectionRewrite(action, undefined, customOverride)
              }
              onSavePreset={handleSaveRewritePreset}
              onDeletePreset={handleDeleteRewritePreset}
              onDismissBar={() => {
                selectionChannel.stop();
                setSelectionRewriting(false);
                setSelectionRange(null);
                setSelectionPreview(null);
              }}
              onCancelPreview={() => {
                selectionChannel.stop();
                setSelectionRewriting(false);
                setSelectionPreview(null);
              }}
              onApplyPreview={applySelectionRewrite}
            />
          </div>
        )}

        {/* Centered Translucent Floating Action Dock */}
        {!focusMode && (
          <div className="pointer-events-none absolute right-4 bottom-4 left-4 z-20 flex justify-center">
            <footer className="pointer-events-auto nib-floating-dock inline-flex max-w-full flex-wrap items-center gap-1 rounded-full px-2.5 py-1.5 text-xs select-none">
              <button
                type="button"
                disabled={generating}
                onClick={() => setReadingMode((v) => !v)}
                title={readingMode ? "切回写作编辑模式" : "切换到校对与 AI 味标亮视图"}
                className={`flex items-center gap-1 rounded-full px-2.5 py-1 font-medium transition-colors disabled:opacity-40 ${
                  readingMode
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:bg-[var(--bg-hover)] hover:text-ink-primary"
                }`}
              >
                {readingMode ? (
                  <>
                    <BookOpen className="h-3 w-3" />
                    <span>校对态</span>
                  </>
                ) : (
                  <>
                    <Pencil className="h-3 w-3" />
                    <span>写作态</span>
                  </>
                )}
              </button>

              {generating ? (
                <button
                  type="button"
                  onClick={stopGeneration}
                  title="停止生成 (Ctrl+Enter)"
                  className="flex items-center gap-1.5 rounded-full bg-danger-soft px-3 py-1 font-medium text-danger transition-opacity hover:opacity-90"
                >
                  <Square className="h-3 w-3 fill-current" />
                  <span>停止</span>
                </button>
              ) : content.trim().length > 0 ? (
                <button
                  type="button"
                  onClick={() => void startGeneration("continue")}
                  title="紧接当前正文结尾继续往下写"
                  className="flex items-center gap-1.5 rounded-full bg-accent px-3 py-1 font-semibold text-canvas transition-opacity hover:opacity-90"
                >
                  <FastForward className="h-3 w-3" />
                  <span>续写</span>
                </button>
              ) : (
                <button
                  type="button"
                  onClick={() => void startGeneration("rewrite")}
                  title="按节拍生成本章 (Ctrl+Enter)"
                  className="flex items-center gap-1.5 rounded-full bg-accent px-3 py-1 font-semibold text-canvas transition-opacity hover:opacity-90"
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
                className="flex items-center gap-1 rounded-full px-2.5 py-1 text-ink-secondary transition-colors hover:bg-[var(--bg-hover)] hover:text-ink-primary disabled:opacity-40"
              >
                <Save className="h-3 w-3" />
                <span>保存</span>
              </button>

              <button
                type="button"
                disabled={generating || finalizing}
                onClick={() => void handleFinalize()}
                title="标记本章定稿并同步 Markdown"
                className={`flex items-center gap-1 rounded-full px-2.5 py-1 transition-colors disabled:opacity-40 ${
                  activeChapter.status === "finalized"
                    ? "bg-ok-soft font-medium text-ok"
                    : "text-ink-secondary hover:bg-[var(--bg-hover)] hover:text-ok"
                }`}
              >
                <CheckCircle2 className="h-3 w-3" />
                <span>
                  {finalizing
                    ? "定稿中…"
                    : activeChapter.status === "finalized"
                      ? "已定稿"
                      : "定稿"}
                </span>
              </button>

              <button
                type="button"
                onClick={() => openAtlasSubTab("audit")}
                title="全书一致性体检与伏笔排查"
                className="flex items-center gap-1 rounded-full px-2.5 py-1 text-ink-secondary transition-colors hover:bg-[var(--bg-hover)] hover:text-ink-primary"
              >
                <ShieldCheck className="h-3 w-3" />
                <span>全书体检</span>
              </button>

              <span className="hidden h-3.5 w-px bg-border-strong sm:block" aria-hidden="true" />

              <QuickModelPicker
                generating={generating}
                genModelName={genModelName}
                lastUsage={lastUsage}
                draftRouteInfo={draftRouteInfo}
              />
            </footer>
          </div>
        )}
      </div>

      {/* Collapsible Floating Glass Right Drawer */}
      {!focusMode && studioDrawerOpen && (
        <aside
          className="nib-glass-panel m-2 mt-0 ml-0 flex shrink-0 flex-col overflow-hidden"
          style={{ width: "var(--studio-drawer-width)" }}
        >
          <div className="flex items-center justify-between border-b border-border-subtle px-2.5 py-2">
            <div className="flex items-center gap-0.5">
              <button
                type="button"
                onClick={() => setDrawerTab("audit")}
                className={`flex items-center gap-1 whitespace-nowrap rounded-lg px-2 py-1 text-xs font-medium transition-colors ${
                  drawerTab === "audit"
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:bg-[var(--bg-hover)] hover:text-ink-primary"
                }`}
              >
                <ShieldCheck className="h-3.5 w-3.5" />
                <span>检查</span>
                {paragraphAuditMaps.totalFlags > 0 && (
                  <span className="nib-mono rounded-full bg-warn-soft px-1.5 text-[11px] font-medium text-warn">
                    {paragraphAuditMaps.totalFlags}
                  </span>
                )}
              </button>
              <button
                type="button"
                onClick={() => setDrawerTab("delta")}
                className={`flex items-center gap-1 whitespace-nowrap rounded-lg px-2 py-1 text-xs font-medium transition-colors ${
                  drawerTab === "delta"
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:bg-[var(--bg-hover)] hover:text-ink-primary"
                }`}
              >
                <FileDiff className="h-3.5 w-3.5" />
                <span>设定更新</span>
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
                className={`flex items-center gap-1 whitespace-nowrap rounded-lg px-2 py-1 text-xs font-medium transition-colors ${
                  drawerTab === "versions"
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:bg-[var(--bg-hover)] hover:text-ink-primary"
                }`}
              >
                <History className="h-3.5 w-3.5" />
                <span>历史</span>
                {versions.length > 0 && (
                  <span className="nib-mono text-[11px] opacity-80">
                    {versions.length}
                  </span>
                )}
              </button>
              <button
                type="button"
                onClick={() => setDrawerTab("briefing")}
                className={`flex items-center gap-1 whitespace-nowrap rounded-lg px-2 py-1 text-xs font-medium transition-colors ${
                  drawerTab === "briefing"
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:bg-[var(--bg-hover)] hover:text-ink-primary"
                }`}
              >
                <FileText className="h-3.5 w-3.5" />
                <span>上下文</span>
              </button>
            </div>

            <button
              type="button"
              onClick={async () => {
                if (drawerTab === "briefing") void loadBriefing();
                else if (drawerTab === "audit") {
                  const ok = await ensureSavedBeforeAudit();
                  if (ok) void runLocalLint(activeChapter.id, false);
                } else if (drawerTab === "delta") void loadPendingDelta();
                else void loadVersions();
              }}
              title="刷新当前面板"
              aria-label="刷新当前面板"
              className="rounded-lg p-1 text-ink-muted transition-colors hover:bg-[var(--bg-hover)] hover:text-ink-primary"
            >
              <RefreshCw
                className={`h-3.5 w-3.5 ${
                  briefingLoading ||
                  versionsLoading ||
                  deltaLoading ||
                  auditLoading
                    ? "animate-spin text-accent"
                    : ""
                }`}
              />
            </button>
          </div>

          {drawerTab === "briefing" && (
            <BriefingDrawerTab
              briefing={briefing}
              briefingLoading={briefingLoading}
              briefingError={briefingError}
              extraInstruction={extraInstruction}
              searchKeywordsText={searchKeywordsText}
              includePending={includePending}
              entities={entities}
              pinnedEntityIds={pinnedEntityIds}
              excludedEntityIds={excludedEntityIds}
              expandedSlots={expandedSlots}
              onChangeExtraInstruction={setExtraInstruction}
              onChangeSearchKeywordsText={setSearchKeywordsText}
              onChangeIncludePending={setIncludePending}
              onTogglePinEntity={togglePinEntity}
              onToggleExcludeEntity={toggleExcludeEntity}
              onToggleSlotExpand={toggleSlotExpand}
            />
          )}

          {drawerTab === "audit" && (
            <QualityAuditPanel
              chapterId={activeChapter.id}
              chapterHasContent={content.trim().length > 0}
              audit={chapterAudit}
              loading={auditLoading}
              activeParagraphIndex={activeParagraphIndex}
              polishingParagraphs={polishingParagraphs}
              applyingDiffId={applyingDiffId}
              onAuditUpdated={setChapterAudit}
              onApplyDiff={(diffId, accept) => handleApplyInlineDiff(diffId, accept)}
              onFocusParagraph={scrollToParagraph}
              onEnsureSavedBeforeAudit={ensureSavedBeforeAudit}
              onRunLocalLint={async () => {
                const ok = await ensureSavedBeforeAudit();
                if (ok) await runLocalLint(activeChapter.id, false);
              }}
              onRunSurgicalPolish={handleRunSurgicalPolish}
              onStopSurgicalPolish={stopSurgicalPolish}
              onQuickReplaceIssue={(iss, rep) =>
                void handleQuickReplaceIssue(iss, rep)
              }
              onNotify={notify}
            />
          )}

          {drawerTab === "delta" && (
            <DeltaReviewPanel
              chapterId={activeChapter.id}
              chapterHasContent={content.trim().length > 0}
              delta={pendingDelta}
              loading={deltaLoading}
              onEnsureSavedBeforeExtract={ensureSavedBeforeAudit}
              onDeltaLoaded={setPendingDelta}
              onDeltaApplied={handleDeltaApplied}
              onNotify={notify}
            />
          )}

          {drawerTab === "versions" && (
            <VersionsDrawerTab
              versions={versions}
              versionsLoading={versionsLoading}
              selectedVersionId={selectedVersionId}
              restoringId={restoringId}
              generating={generating}
              currentContent={content}
              onSelectVersionId={setSelectedVersionId}
              onRestoreVersion={(vid) => void handleRestoreVersion(vid)}
              onInsertParagraph={handleInsertHistoricalParagraph}
              notify={notify}
            />
          )}
        </aside>
      )}
    </div>
  );
}
