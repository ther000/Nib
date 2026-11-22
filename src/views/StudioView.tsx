import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from "react";
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
  Clipboard,
  Copy,
  Download,
  FastForward,
  FileDiff,
  FileText,
  History,
  Loader2,
  Maximize2,
  Minimize2,
  PanelRight,
  Pencil,
  Pin,
  Play,
  RefreshCw,
  RotateCcw,
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
import { useContextMenu, type ContextMenuItem } from "../components/ContextMenu";
import { useMygoChannel } from "../hooks/useMygoStream";
import { AuditProseParagraph } from "../components/AuditProseParagraph";
import { DeltaReviewPanel } from "../components/DeltaReviewPanel";
import { QualityAuditPanel } from "../components/QualityAuditPanel";
import { diffParagraphs, formatWebNovelProse } from "../lib/textDiff";
import {
  countWords,
  formatCompactTokens,
  formatErrorMessage,
  formatNumber,
  formatSeq,
  formatTimestamp,
  getChapterStatusMeta,
  getEntityKindLabel,
  getVersionSourceLabel,
  isIMEComposing,
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
    proseFontSize,
    proseLineHeight,
    proseMaxWidth,
    studioDrawerOpen,
    setStudioDrawerOpen,
    toggleStudioDrawer,
    focusMode,
    setFocusMode,
    toggleFocusMode,
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
  const [bookReplaceStats, setBookReplaceStats] = useState<{ affectedChapters: number; totalMatches: number } | null>(null);
  const [replacingBook, setReplacingBook] = useState(false);

  // Selection AI micro-edit state
  const [selectionRange, setSelectionRange] = useState<{ start: number; end: number; text: string } | null>(null);
  const [selectionCustomPrompt, setSelectionCustomPrompt] = useState("");
  const [selectionRewriting, setSelectionRewriting] = useState(false);
  const [selectionStage, setSelectionStage] = useState("");
  const [selectionPreview, setSelectionPreview] = useState<string | null>(null);
  const selectionChannel = useMygoChannel<GenChunk>();

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
  const activeChapterRef = useRef(activeChapter);
  const titleRef = useRef(title);
  const contentRef = useRef(content);
  const dirtyRef = useRef(dirty);
  const generatingRef = useRef(generating);

  activeChapterIdRef.current = activeChapterId;
  activeChapterRef.current = activeChapter;
  titleRef.current = title;
  contentRef.current = content;
  dirtyRef.current = dirty;
  generatingRef.current = generating;

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
  const [versionViewMode, setVersionViewMode] = useState<"diff" | "raw">("diff");
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
    setLastAutoSavedAt(null);
    setSelectionRange(null);
    setSelectionPreview(null);
    setSelectionRewriting(false);
    setSelectionStage("");
    selectionChannel.stop();
    setReadingMode(false);
    setGenError(null);
    setPinnedEntityIds(new Set());
    setExcludedEntityIds(new Set());
    setExtraInstruction("");
    setSearchKeywordsText("");
    setExpandedSlots(new Set());
    setPendingDelta(null);
    setChapterAudit(null);
    setActiveParagraphIndex(null);
    return () => {
      // 切章前若存在未落盘修改，立即后台保存，避免静默丢稿
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
          .then((saved) => {
            updateChapterInCache(saved);
          })
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

  // 接收「灵感便签」面板的插入请求，在正文光标处插入文本。
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

  // ResizeObserver 仅随编辑区挂载创建一次，避免每次击键重建观察器
  useEffect(() => {
    const el = textareaRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => syncTextareaHeight());
    ro.observe(el);
    return () => ro.disconnect();
  }, [activeChapter?.id, generating, readingMode, syncTextareaHeight]);

  // Auto-resize textarea height cleanly after content/mode/layout/typography changes
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

  // Abort active generation & flush dirty draft if unmounting StudioView
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
          .then((saved) => {
            updateChapterInCache(saved);
          })
          .catch(() => {
            // ignore unmount background flush error
          });
      }
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

  // Save draft (silent=true for background AutoSave)
  const handleSave = useCallback(async (silent = false): Promise<boolean> => {
    if (!activeChapter || generating) return false;
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
          `${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}`
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
        notify({ tone: "danger", message: "保存正文失败", detail: formatErrorMessage(err) });
      }
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

  // Debounced AutoSave: 3s after user stops typing
  useEffect(() => {
    if (!dirty || generating || saving || !activeChapter) return;
    const timer = window.setTimeout(() => {
      void handleSave(true);
    }, 3000);
    return () => window.clearTimeout(timer);
  }, [dirty, generating, saving, activeChapter, content, title, handleSave]);

  // Ctrl+H toggles inline Find & Replace bar
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

  // Copy clean prose without markdown heading symbols for direct web-novel portal paste
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

  // 一键规范网文排版（全角标点、中文弯引号、清理多余空格与空行）
  const handleFormatProse = useCallback(() => {
    if (generating || !content.trim()) return;
    const { text, changed } = formatWebNovelProse(content);
    if (!changed) {
      notify({ tone: "info", message: "当前正文排版已经很规范，无需调整" });
      return;
    }
    setContent(text);
    setDirty(true);
    notify({ tone: "ok", message: "已整理标点、引号与段落间距（3 秒后自动保存）" });
  }, [content, generating, notify]);

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
      overrideRange?: { start: number; end: number; text: string }
    ) => {
      const activeRange = overrideRange ?? selectionRange;
      if (!activeChapter || !activeRange || selectionRewriting) return;
      if (overrideRange) {
        setSelectionRange(overrideRange);
      }
      if (action === "custom" && !selectionCustomPrompt.trim()) {
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
            beforeContext: content.slice(Math.max(0, activeRange.start - 360), activeRange.start),
            afterContext: content.slice(activeRange.end, Math.min(content.length, activeRange.end + 360)),
            action,
            customInstruction: action === "custom" ? selectionCustomPrompt.trim() : "",
          },
          channel
        );
        if (selectionChannel.isCurrent(runId)) {
          setSelectionPreview(res.rewrittenText);
        }
      } catch (err) {
        if (!selectionChannel.wasStopped()) {
          notify({ tone: "danger", message: "选区微调失败", detail: formatErrorMessage(err) });
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
    [activeChapter, selectionRange, selectionRewriting, selectionCustomPrompt, content, selectionChannel, notify]
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
    const nextContent =
      content.slice(0, start) + selectionPreview + content.slice(end);
    setContent(nextContent);
    setDirty(true);
    setSelectionRange(null);
    setSelectionPreview(null);
    setSelectionCustomPrompt("");
    notify({ tone: "ok", message: "已替换选中片段，将在 3 秒内自动保存" });
  }, [selectionRange, selectionPreview, content, notify]);

  // Chapter & whole-book find/replace helpers
  const chapterFindMatches = useMemo(() => {
    if (!findQuery) return 0;
    return content.split(findQuery).length - 1;
  }, [content, findQuery]);

  const handleReplaceInChapter = useCallback(() => {
    if (!findQuery) return;
    const count = content.split(findQuery).length - 1;
    if (count === 0) {
      notify({ tone: "warn", message: "当前章节未找到匹配内容" });
      return;
    }
    setContent(content.split(findQuery).join(replaceText));
    setDirty(true);
    notify({ tone: "ok", message: `已在本章替换 ${count} 处` });
  }, [content, findQuery, replaceText, notify]);

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
      notify({ tone: "danger", message: "统计全书匹配失败", detail: formatErrorMessage(err) });
    } finally {
      setReplacingBook(false);
    }
  }, [activeBook, findQuery, replaceText, notify]);

  const handleExecuteBookReplace = useCallback(async () => {
    if (!activeBook || !findQuery) return;
    setReplacingBook(true);
    try {
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
      notify({ tone: "danger", message: "全书替换失败", detail: formatErrorMessage(err) });
    } finally {
      setReplacingBook(false);
    }
  }, [activeBook, activeChapter, findQuery, replaceText, refreshChapters, refreshActiveChapter, notify]);

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

  // Start generation (supports full "rewrite" and seamless "continue" mode)
  const startGeneration = useCallback(async (mode: "rewrite" | "continue" = "rewrite") => {
    if (!activeChapter || generating) return;

    if (!draftRouteInfo.configured) {
      const hasEnabledModel = providers.some(
        (p) => p.enabled && (p.models?.length ?? 0) > 0
      );
      notify({
        tone: "warn",
        message: "尚未设置写作模型",
        detail: hasEnabledModel
          ? "请先在「模型分配」中指定正文写作模型"
          : "请先在「模型服务」中连接并启用模型",
      });
      openSettings(hasEnabledModel ? "routes" : "providers");
      return;
    }

    // 续写模式若编辑器有未保存修改，先保存最新正文让后端读取到准确结尾。
    const effectiveMode: "rewrite" | "continue" =
      mode === "continue" && content.trim().length > 0 ? "continue" : "rewrite";
    if (effectiveMode === "continue" && dirty) {
      await handleSave(true);
    }

    const genChapterId = activeChapter.id;
    const runId = ++streamRunIdRef.current;
    const ac = new AbortController();
    abortRef.current = ac;
    userStoppedRef.current = false;
    const continueBase = effectiveMode === "continue" ? content.trimEnd() : "";
    streamTextRef.current = continueBase;
    receivedDeltaRef.current = false;

    setGenerating(true);
    setGenError(null);
    setGenStageText(effectiveMode === "continue" ? "正在衔接前文准备续写…" : "正在整理参考资料…");
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
          setGenStageText(`${effectiveMode === "continue" ? "正在续写" : "正在写作"}（${model}）`);
          break;
        }
        case "thinking": {
          const model = chunk.modelName || draftRouteInfo.label;
          setGenStageText(`模型思考中（${model}）…`);
          break;
        }
        case "delta": {
          if (chunk.delta) {
            if (!receivedDeltaRef.current) {
              receivedDeltaRef.current = true;
              if (effectiveMode === "continue" && continueBase) {
                const sep = /[。！？”」』…!?]$/.test(continueBase) ? "\n\n" : "";
                streamTextRef.current = continueBase + sep + chunk.delta.replace(/^[\r\n]+/, "");
              } else {
                streamTextRef.current = chunk.delta;
              }
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
      mode: effectiveMode,
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
      // 注意：MyGo v0.10.0 中带 Channel 参数的方法不可与 .with({ signal }) 混用（否则 pageCtx 包装会导致 Channel 立即关闭），
      // 取消流式生成通过 stopGeneration 调用 ch.close() 触发 Go 端 ctx 取消并安全落盘 partial 快照。
      const updated = await StudioService.generateChapter(genChapterId, opts, ch);

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
    content,
    dirty,
    handleSave,
    providers,
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
      await loadVersions();
      notify({
        tone: "ok",
        message: "已恢复到此历史版本",
        detail: "原正文已自动存入历史版本",
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

  const selectedVersionDiff = useMemo(() => {
    if (
      !studioDrawerOpen ||
      drawerTab !== "versions" ||
      !selectedVersion ||
      versionViewMode !== "diff"
    ) {
      return null;
    }
    return diffParagraphs(selectedVersion.content, content);
  }, [studioDrawerOpen, drawerTab, selectedVersion, versionViewMode, content]);

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

  // 正文编辑区右键菜单（结合选区 AI 微调 + 桌面基础编辑 + 章节高频动作）
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
      if (range) {
        setSelectionRange(range);
      }

      const items: ContextMenuItem[] = [];
      if (range) {
        items.push({ type: "label", label: `AI 选区微调（${countWords(range.text)} 字）` });
        const actions: Array<{ key: "expand" | "condense" | "dialogue" | "vivid" | "deai"; label: string }> = [
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

  // 校对 / 流式阅读区右键菜单
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
    <div className="relative flex min-h-0 flex-1 overflow-hidden bg-canvas">
      {/* 专注写作模式右上角悬浮微弱状态条 */}
      {focusMode && (
        <div className="fixed top-3 right-4 z-30 flex items-center gap-2 rounded-full border border-border-subtle bg-surface/85 px-3 py-1 text-xs text-ink-secondary backdrop-blur-md transition-opacity hover:opacity-100 opacity-70">
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
      )}

      {/* Main Quiet Prose Canvas + Minimal Bottom Bar */}
      <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
        {/* Scrollable Prose Canvas */}
        <div
          ref={canvasScrollRef}
          className={`flex-1 overflow-y-auto px-8 pb-24 ${focusMode ? "pt-14" : "pt-8"}`}
        >
          <div className="mx-auto w-full max-w-[var(--prose-max-width)]">
            {/* Subtle chapter breadcrumb & quick switcher */}
            {!focusMode && (
            <div className="mb-5 flex items-center justify-between gap-2 text-xs text-ink-muted select-none">
              <div className="flex min-w-0 items-center gap-2.5">
                {activeVolumeTitle && (
                  <span className="truncate text-ink-muted">{activeVolumeTitle}</span>
                )}
                <span className="text-ink-secondary">
                  第 {formatSeq(activeChapter.globalSeq)} 章
                </span>
                <span
                  className={`rounded-full border px-2 py-0.5 text-xs ${statusMeta.badgeClass}`}
                >
                  {statusMeta.label}
                </span>
              </div>

              <div className="flex items-center gap-1">
                {paragraphAuditMaps.flaggedParagraphs.length > 0 && (
                  <div className="mr-1 flex items-center gap-0.5 rounded-lg border border-border-subtle bg-surface px-1 py-0.5">
                    <button
                      type="button"
                      onClick={() => cycleFlaggedParagraph(-1)}
                      title="上一处建议 (Alt+[)"
                      className="rounded px-1.5 py-0.5 text-xs text-ink-secondary hover:bg-elevated hover:text-ink-primary"
                    >
                      上一处
                    </button>
                    <span className="nib-mono px-1 text-xs text-warn">
                      {paragraphAuditMaps.flaggedParagraphs.length} 处建议
                    </span>
                    <button
                      type="button"
                      onClick={() => cycleFlaggedParagraph(1)}
                      title="下一处建议 (Alt+])"
                      className="rounded px-1.5 py-0.5 text-xs text-ink-secondary hover:bg-elevated hover:text-ink-primary"
                    >
                      下一处
                    </button>
                  </div>
                )}
                <button
                  type="button"
                  onClick={() => setFindOpen((v) => !v)}
                  title="查找与替换 (Ctrl+H)"
                  className={`flex items-center gap-1 rounded-md px-2 py-1 text-xs transition-colors ${
                    findOpen
                      ? "bg-accent-soft text-accent"
                      : "text-ink-muted hover:bg-surface hover:text-ink-primary"
                  }`}
                >
                  <Search className="h-3 w-3" />
                  <span>查找替换</span>
                </button>
                <button
                  type="button"
                  disabled={generating || !content.trim()}
                  onClick={handleFormatProse}
                  title="一键规范全角标点、中文引号与段落间距"
                  className="flex items-center gap-1 rounded-md px-2 py-1 text-xs text-ink-muted transition-colors hover:bg-surface hover:text-ink-primary disabled:opacity-30"
                >
                  <Wand2 className="h-3 w-3" />
                  <span>排版</span>
                </button>
                <button
                  type="button"
                  disabled={generating || !content.trim()}
                  onClick={() => void handleCopyCleanProse()}
                  title="复制无格式纯净正文（可直接贴起点/番茄）"
                  className="flex items-center gap-1 rounded-md px-2 py-1 text-xs text-ink-muted transition-colors hover:bg-surface hover:text-ink-primary disabled:opacity-30"
                >
                  {copiedClean ? (
                    <>
                      <Check className="h-3 w-3 text-ok" />
                      <span className="text-ok">已复制</span>
                    </>
                  ) : (
                    <>
                      <Copy className="h-3 w-3" />
                      <span>复制纯净正文</span>
                    </>
                  )}
                </button>
                <button
                  type="button"
                  disabled={generating}
                  onClick={() => setReadingMode((v) => !v)}
                  title={readingMode ? "切回编辑模式" : "切换校对视图"}
                  className={`flex items-center gap-1 rounded-md px-2 py-1 text-xs transition-colors disabled:opacity-30 ${
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
                      <span>校对视图</span>
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
                <button
                  type="button"
                  onClick={toggleFocusMode}
                  title="专注写作模式 (F11 / Ctrl+Shift+Z)"
                  className="rounded p-1 text-ink-muted transition-colors hover:bg-surface hover:text-ink-primary"
                >
                  <Maximize2 className="h-3.5 w-3.5" />
                </button>
              </div>
            </div>
            )}

            {/* Find & Replace Bar (Ctrl+H) */}
            {findOpen && (
              <div className="mb-5 flex flex-col gap-2.5 rounded-xl border border-border-subtle bg-surface p-3 text-xs">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex items-center gap-1 rounded-lg border border-border-subtle bg-canvas p-0.5">
                    <button
                      type="button"
                      onClick={() => {
                        setFindScope("chapter");
                        setBookReplaceStats(null);
                      }}
                      className={`rounded-md px-2.5 py-1 text-xs font-medium ${
                        findScope === "chapter"
                          ? "bg-accent-soft text-accent"
                          : "text-ink-secondary hover:text-ink-primary"
                      }`}
                    >
                      当前章节
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        setFindScope("book");
                        setBookReplaceStats(null);
                      }}
                      className={`rounded-md px-2.5 py-1 text-xs font-medium ${
                        findScope === "book"
                          ? "bg-accent-soft text-accent"
                          : "text-ink-secondary hover:text-ink-primary"
                      }`}
                    >
                      全书改名/替换
                    </button>
                  </div>
                  <button
                    type="button"
                    onClick={() => {
                      setFindOpen(false);
                      setBookReplaceStats(null);
                    }}
                    className="rounded p-1 text-ink-muted hover:text-ink-primary"
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                </div>

                <div className="flex flex-wrap items-center gap-2">
                  <input
                    type="text"
                    value={findQuery}
                    onChange={(e) => {
                      setFindQuery(e.target.value);
                      setBookReplaceStats(null);
                    }}
                    placeholder="查找内容（如旧人名/招式名）"
                    className="min-w-[160px] flex-1 rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                  />
                  <input
                    type="text"
                    value={replaceText}
                    onChange={(e) => setReplaceText(e.target.value)}
                    placeholder="替换为…"
                    className="min-w-[160px] flex-1 rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                  />

                  {findScope === "chapter" ? (
                    <div className="flex items-center gap-2">
                      {findQuery && (
                        <span className="nib-mono text-ink-muted">
                          {chapterFindMatches} 处匹配
                        </span>
                      )}
                      <button
                        type="button"
                        disabled={!findQuery || chapterFindMatches === 0}
                        onClick={handleReplaceInChapter}
                        className="rounded-lg bg-accent px-3 py-1.5 font-medium text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
                      >
                        全部替换
                      </button>
                    </div>
                  ) : (
                    <div className="flex items-center gap-2">
                      <button
                        type="button"
                        disabled={!findQuery || replacingBook}
                        onClick={() => void handlePreviewBookReplace()}
                        className="rounded-lg border border-border-subtle bg-elevated px-3 py-1.5 text-ink-primary hover:border-border-strong disabled:opacity-40"
                      >
                        {replacingBook ? "统计中…" : "预检影响范围"}
                      </button>
                      {bookReplaceStats && bookReplaceStats.totalMatches > 0 && (
                        <button
                          type="button"
                          disabled={replacingBook}
                          onClick={() => void handleExecuteBookReplace()}
                          className="rounded-lg bg-accent px-3 py-1.5 font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
                        >
                          确认替换（{bookReplaceStats.affectedChapters} 章 {bookReplaceStats.totalMatches} 处）
                        </button>
                      )}
                    </div>
                  )}
                </div>
                {findScope === "book" && bookReplaceStats && bookReplaceStats.totalMatches === 0 && (
                  <div className="text-ink-muted">全书正文、标题与章纲中均未找到「{findQuery}」</div>
                )}
              </div>
            )}

            {/* Selection AI Micro-Edit Bar */}
            {!generating && !readingMode && selectionRange && (
              <div className="mb-4 flex flex-col gap-2.5 rounded-xl border border-accent/35 bg-surface p-3 text-xs">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex items-center gap-1.5 text-accent">
                    <Sparkles className="h-3.5 w-3.5" />
                    <span className="font-medium">
                      已选中 {countWords(selectionRange.text)} 字
                    </span>
                    {selectionStage && (
                      <span className="text-ink-muted">· {selectionStage}</span>
                    )}
                  </div>
                  <div className="flex flex-wrap items-center gap-1.5">
                    {(
                      [
                        { key: "expand", label: "扩写细节" },
                        { key: "condense", label: "精简紧凑" },
                        { key: "dialogue", label: "优化对话" },
                        { key: "vivid", label: "增强动作感" },
                        { key: "deai", label: "去 AI 味" },
                      ] as const
                    ).map((item) => (
                      <button
                        key={item.key}
                        type="button"
                        disabled={selectionRewriting}
                        onClick={() => void runSelectionRewrite(item.key)}
                        className="rounded-md border border-border-subtle bg-elevated px-2 py-1 text-xs text-ink-primary transition-colors hover:border-accent/50 hover:text-accent disabled:opacity-40"
                      >
                        {item.label}
                      </button>
                    ))}
                    <button
                      type="button"
                      onClick={() => {
                        selectionChannel.stop();
                        setSelectionRewriting(false);
                        setSelectionRange(null);
                        setSelectionPreview(null);
                      }}
                      className="rounded p-1 text-ink-muted hover:text-ink-primary"
                    >
                      <X className="h-3.5 w-3.5" />
                    </button>
                  </div>
                </div>

                <div className="flex items-center gap-2">
                  <input
                    type="text"
                    value={selectionCustomPrompt}
                    disabled={selectionRewriting}
                    onChange={(e) => setSelectionCustomPrompt(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" && !isIMEComposing(e) && selectionCustomPrompt.trim()) {
                        e.preventDefault();
                        void runSelectionRewrite("custom");
                      }
                    }}
                    placeholder="或输入自定义修改要求（例如：改成冷嘲热讽语气、加入拔剑动作）"
                    className="min-w-0 flex-1 rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                  />
                  <button
                    type="button"
                    disabled={selectionRewriting || !selectionCustomPrompt.trim()}
                    onClick={() => void runSelectionRewrite("custom")}
                    className="rounded-lg bg-accent px-3 py-1.5 font-medium text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
                  >
                    {selectionRewriting ? "改写中…" : "按要求改写"}
                  </button>
                </div>

                {selectionPreview !== null && (
                  <div className="flex flex-col gap-2 rounded-lg border border-border-subtle bg-canvas p-3">
                    <div className="text-xs font-medium text-ink-secondary">改写预览</div>
                    <div className="max-h-48 overflow-y-auto whitespace-pre-wrap text-[13px] leading-relaxed text-ink-primary">
                      {selectionPreview || "正在生成…"}
                    </div>
                    <div className="flex items-center justify-end gap-2">
                      <button
                        type="button"
                        onClick={() => {
                          selectionChannel.stop();
                          setSelectionRewriting(false);
                          setSelectionPreview(null);
                        }}
                        className="rounded-md border border-border-subtle px-2.5 py-1 text-xs text-ink-secondary hover:text-ink-primary"
                      >
                        放弃
                      </button>
                      <button
                        type="button"
                        disabled={selectionRewriting || !selectionPreview.trim()}
                        onClick={applySelectionRewrite}
                        className="rounded-md bg-accent px-3 py-1 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
                      >
                        替换选区
                      </button>
                    </div>
                  </div>
                )}
              </div>
            )}

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
                <div className="flex shrink-0 items-center gap-3">
                  <button
                    type="button"
                    onClick={() => void startGeneration("continue")}
                    className="font-medium underline underline-offset-2 hover:opacity-80"
                  >
                    接着续写
                  </button>
                  <button
                    type="button"
                    onClick={() => void startGeneration("rewrite")}
                    className="font-medium text-ink-secondary underline underline-offset-2 hover:opacity-80"
                  >
                    重新生成
                  </button>
                </div>
              </div>
            )}

            {/* Prose Editor / Live Stream Surface / Interactive Paragraph Audit & Surgical Diff */}
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
                onSelect={handleEditorSelect}
                onMouseUp={handleEditorSelect}
                onKeyUp={handleEditorSelect}
                onContextMenu={handleTextareaContextMenu}
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
        {!focusMode && (
        <footer className="flex h-10 shrink-0 items-center justify-between border-t border-border-subtle bg-surface/90 px-4 text-xs select-none">
          {/* Left metrics & status */}
          <div className="flex min-w-0 items-center gap-3 text-ink-secondary">
            <span className="nib-mono font-medium text-ink-primary">
              {formatNumber(liveWordCount)} 字
            </span>

            <span className="h-3 w-px bg-border-subtle" aria-hidden="true" />

            <button
              type="button"
              onClick={() => openSettings(draftRouteInfo.configured ? "routes" : "providers")}
              title="调整正文写作模型"
              className={`truncate transition-colors hover:text-ink-primary ${
                draftRouteInfo.configured ? "nib-mono text-ink-secondary" : "text-warn"
              }`}
            >
              {genModelName || draftRouteInfo.label}
            </button>

            <span className="h-3 w-px bg-border-subtle" aria-hidden="true" />

            {generating ? (
              <span className="text-accent">写作中</span>
            ) : saving ? (
              <span className="text-ink-muted">保存中…</span>
            ) : dirty ? (
              <span className="text-warn">未保存（3 秒后自动保存）</span>
            ) : activeChapter.status === "partial" ? (
              <span className="text-warn">未写完</span>
            ) : lastAutoSavedAt ? (
              <span className="text-ink-muted">已自动保存 {lastAutoSavedAt}</span>
            ) : (
              <span className="text-ink-muted">已保存</span>
            )}

            {lastUsage && !generating && (
              <span
                title={`输入 ${formatNumber(lastUsage.promptTokens)} / 输出 ${formatNumber(lastUsage.completionTokens)} Token`}
                className="nib-mono hidden text-xs text-ink-muted lg:inline"
              >
                输入 {formatCompactTokens(lastUsage.promptTokens)} / 输出{" "}
                {formatCompactTokens(lastUsage.completionTokens)} Token
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
            ) : content.trim().length > 0 ? (
              <div className="flex items-center gap-1.5">
                <button
                  type="button"
                  onClick={() => void startGeneration("continue")}
                  title="紧接当前正文结尾继续往下写"
                  className="flex items-center gap-1.5 rounded-md bg-accent px-3 py-1 font-semibold text-canvas transition-opacity hover:opacity-90"
                >
                  <FastForward className="h-3 w-3" />
                  <span>续写</span>
                </button>
                <button
                  type="button"
                  onClick={() => void startGeneration("rewrite")}
                  title="存档当前正文后按节拍重新生成整章"
                  className="flex items-center gap-1 rounded-md border border-border-subtle bg-elevated px-2.5 py-1 text-ink-primary transition-colors hover:border-border-strong"
                >
                  <RefreshCw className="h-3 w-3 text-ink-secondary" />
                  <span>重写</span>
                </button>
              </div>
            ) : (
              <button
                type="button"
                onClick={() => void startGeneration("rewrite")}
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
        )}
      </div>

      {/* Collapsible Right Drawer (360px) */}
      {!focusMode && studioDrawerOpen && (
        <aside
          className="flex shrink-0 flex-col border-l border-border-subtle bg-surface"
          style={{ width: "var(--studio-drawer-width)" }}
        >
          {/* Drawer Header Tabs: 参考 / 检查 / 设定更新 / 历史 */}
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
                <span>参考</span>
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
                <span>检查</span>
                {paragraphAuditMaps.totalFlags > 0 && (
                  <span className="nib-mono rounded-full bg-warn-soft px-1.5 text-xs font-medium text-warn">
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
                className={`flex items-center gap-1 whitespace-nowrap rounded-md px-2 py-1 text-xs font-medium transition-colors ${
                  drawerTab === "versions"
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
                }`}
              >
                <History className="h-3.5 w-3.5" />
                <span>历史</span>
                {versions.length > 0 && (
                  <span className="nib-mono text-xs opacity-80">
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
                  <label className="mb-1 block text-xs font-medium text-ink-secondary">
                    本章写作补充要求
                  </label>
                  <textarea
                    rows={2}
                    value={extraInstruction}
                    onChange={(e) => setExtraInstruction(e.target.value)}
                    placeholder="例如：压低配角台词字数，突出招式细节"
                    className="w-full resize-none rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                  />
                </div>

                <div>
                  <label className="mb-1 block text-xs font-medium text-ink-secondary">
                    补充关联词（逗号分隔）
                  </label>
                  <input
                    type="text"
                    value={searchKeywordsText}
                    onChange={(e) => setSearchKeywordsText(e.target.value)}
                    placeholder="例如：血玉令牌, 破障丹, 残阵"
                    className="w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                  />
                </div>

                <label className="flex cursor-pointer items-center justify-between rounded-lg border border-border-subtle bg-canvas/60 px-2.5 py-1.5 text-xs text-ink-secondary select-none">
                  <span>包含未确认的连载变动</span>
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
                            <span className="nib-mono text-xs text-ink-muted">
                              {formatCompactTokens(slot.tokenEst)} Token
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
              onEnsureSavedBeforeExtract={ensureSavedBeforeAudit}
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
                      <div className="flex items-center justify-between gap-2 border-b border-border-subtle bg-surface px-3 py-2">
                        <div className="flex items-center gap-1 rounded-md border border-border-subtle bg-canvas p-0.5 text-[11px]">
                          <button
                            type="button"
                            onClick={() => setVersionViewMode("diff")}
                            className={`rounded px-2 py-0.5 transition-colors ${
                              versionViewMode === "diff"
                                ? "bg-accent-soft font-medium text-accent"
                                : "text-ink-secondary hover:text-ink-primary"
                            }`}
                          >
                            对比当前
                          </button>
                          <button
                            type="button"
                            onClick={() => setVersionViewMode("raw")}
                            className={`rounded px-2 py-0.5 transition-colors ${
                              versionViewMode === "raw"
                                ? "bg-accent-soft font-medium text-accent"
                                : "text-ink-secondary hover:text-ink-primary"
                            }`}
                          >
                            快照全文
                          </button>
                        </div>
                        <button
                          type="button"
                          disabled={generating || restoringId === selectedVersion.id}
                          onClick={() => void handleRestoreVersion(selectedVersion.id)}
                          className="flex shrink-0 items-center gap-1 rounded-lg border border-border-subtle bg-elevated px-2.5 py-1 text-xs font-medium text-ink-primary transition-colors hover:border-accent hover:text-accent disabled:opacity-40"
                        >
                          <RotateCcw className="h-3 w-3" />
                          <span>
                            {restoringId === selectedVersion.id ? "恢复中…" : "整章恢复"}
                          </span>
                        </button>
                      </div>

                      {versionViewMode === "diff" && selectedVersionDiff ? (
                        <div className="flex flex-1 flex-col overflow-hidden bg-canvas/50">
                          <div className="flex items-center justify-between border-b border-border-subtle px-3.5 py-1.5 text-[11px] text-ink-muted">
                            <span>历史快照 → 当前正文</span>
                            <span className="nib-mono flex items-center gap-2">
                              <span className="text-ok">+{selectedVersionDiff.added} 段</span>
                              <span className="text-danger">-{selectedVersionDiff.removed} 段</span>
                            </span>
                          </div>
                          <div className="flex-1 overflow-y-auto px-3 py-2.5">
                            {selectedVersionDiff.added === 0 && selectedVersionDiff.removed === 0 ? (
                              <div className="py-8 text-center text-xs text-ink-muted">
                                此历史版本与当前正文内容完全一致
                              </div>
                            ) : (
                              <div className="flex flex-col gap-2">
                                {selectedVersionDiff.blocks.map((blk, idx) => (
                                  <div
                                    key={idx}
                                    className={`group rounded-lg border px-2.5 py-2 text-[12.5px] leading-relaxed ${
                                      blk.kind === "added"
                                        ? "border-ok/30 bg-ok-soft/60 text-ink-primary"
                                        : blk.kind === "removed"
                                          ? "border-danger/30 bg-danger-soft/55 text-ink-secondary"
                                          : "border-transparent bg-transparent text-ink-muted"
                                    }`}
                                  >
                                    <div className="flex items-start justify-between gap-2">
                                      <span className="nib-mono shrink-0 text-[10.5px] font-semibold opacity-75">
                                        {blk.kind === "added"
                                          ? "+"
                                          : blk.kind === "removed"
                                            ? "-"
                                            : "·"}
                                      </span>
                                      <p className="min-w-0 flex-1 font-prose whitespace-pre-wrap">
                                        {blk.text}
                                      </p>
                                    </div>
                                    {blk.kind === "removed" && (
                                      <div className="mt-1.5 flex items-center justify-end gap-2 opacity-0 transition-opacity group-hover:opacity-100">
                                        <button
                                          type="button"
                                          onClick={async () => {
                                            try {
                                              await navigator.clipboard.writeText(blk.text);
                                              notify({ tone: "ok", message: "已复制该历史段落" });
                                            } catch {
                                              // ignore
                                            }
                                          }}
                                          className="rounded px-1.5 py-0.5 text-[11px] text-ink-secondary hover:bg-elevated hover:text-ink-primary"
                                        >
                                          复制
                                        </button>
                                        <button
                                          type="button"
                                          disabled={generating}
                                          onClick={() => handleInsertHistoricalParagraph(blk.text)}
                                          className="rounded bg-elevated px-1.5 py-0.5 text-[11px] font-medium text-accent hover:bg-accent-soft disabled:opacity-40"
                                        >
                                          插回当前稿
                                        </button>
                                      </div>
                                    )}
                                  </div>
                                ))}
                              </div>
                            )}
                          </div>
                        </div>
                      ) : (
                        <div className="flex-1 overflow-y-auto bg-canvas/50 px-4 py-3">
                          <div className="font-prose text-[13.5px] leading-relaxed whitespace-pre-wrap text-ink-secondary">
                            {selectedVersion.content || "（空正文）"}
                          </div>
                        </div>
                      )}
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
