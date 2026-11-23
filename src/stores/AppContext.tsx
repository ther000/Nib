import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  AtlasService,
  LibraryService,
  MemoryService,
  SettingsService,
  type Book,
  type Chapter,
  type Entity,
  type Foreshadowing,
  type GenrePreset,
  type ModelRoute,
  type ProviderPublic,
  type Volume,
} from "../mygo";
import {
  formatErrorMessage,
  type AtlasSubTab,
  type WorkspaceTab,
} from "../lib/constants";
import {
  applyDocumentPreferences,
  DEFAULT_TYPOGRAPHY,
  loadPreferences,
  savePreferences,
  type NibPreferences,
  type ProseFontMode,
  type ThemeMode,
} from "../lib/preferences";
import {
  ToastProvider,
  useToastActions,
  type NotifyInput,
  type ToastItem,
  type ToastTone,
} from "./ToastContext";
import { AutopilotProvider } from "./AutopilotContext";

export type { ToastItem, ToastTone };

export type SettingsTab = "providers" | "routes" | "workspace" | "usage";
export type AppViewMode = "home" | "workspace";
export type DirectorTab = "chat" | "autopilot";
export type CreateBookMode = "ai" | "manual" | "import";

interface AppContextValue {
  // Preferences
  theme: ThemeMode;
  proseFont: ProseFontMode;
  proseIndent: boolean;
  proseFontSize: number;
  proseLineHeight: number;
  proseMaxWidth: number;
  studioDrawerOpen: boolean;
  focusMode: boolean;
  setTheme: (theme: ThemeMode) => void;
  toggleTheme: () => void;
  setProseFont: (font: ProseFontMode) => void;
  setProseIndent: (indent: boolean) => void;
  setProseFontSize: (size: number) => void;
  setProseLineHeight: (lh: number) => void;
  setProseMaxWidth: (w: number) => void;
  resetTypography: () => void;
  setStudioDrawerOpen: (open: boolean) => void;
  toggleStudioDrawer: () => void;
  setFocusMode: (focus: boolean) => void;
  toggleFocusMode: () => void;
  commandPaletteOpen: boolean;
  openCommandPalette: () => void;
  closeCommandPalette: () => void;
  toggleCommandPalette: () => void;

  // Navigation & Modals
  viewMode: AppViewMode;
  openHome: () => void;
  openBook: (bookId: number, tab?: WorkspaceTab) => Promise<void>;
  deleteBook: (bookId: number) => Promise<void>;
  activeTab: WorkspaceTab;
  setActiveTab: (tab: WorkspaceTab) => void;
  atlasSubTab: AtlasSubTab;
  setAtlasSubTab: (sub: AtlasSubTab) => void;
  openAtlasSubTab: (sub: AtlasSubTab) => void;
  atlasFocusEntityId: number | null;
  clearAtlasFocusEntity: () => void;
  jumpToChapter: (chapterId: number) => Promise<void>;
  jumpToEntity: (entityId: number) => void;
  settingsOpen: boolean;
  settingsTab: SettingsTab;
  openSettings: (tab?: SettingsTab) => void;
  closeSettings: () => void;
  createBookOpen: boolean;
  createBookInitialMode: CreateBookMode;
  openCreateBook: (mode?: CreateBookMode) => void;
  closeCreateBook: () => void;
  memorySearchOpen: boolean;
  openMemorySearch: () => void;
  closeMemorySearch: () => void;
  toggleMemorySearch: () => void;
  directorOpen: boolean;
  directorTab: DirectorTab;
  setDirectorTab: (tab: DirectorTab) => void;
  openDirector: (tab?: DirectorTab) => void;
  closeDirector: () => void;
  toggleDirector: () => void;
  notesOpen: boolean;
  openNotes: () => void;
  closeNotes: () => void;
  toggleNotes: () => void;

  // Toasts (stable actions only; state lives in ToastContext to prevent full-tree re-renders)
  notify: (input: NotifyInput) => void;
  dismissToast: (id: number) => void;

  // Loading & Runtime state
  bootstrapping: boolean;
  bookLoading: boolean;
  runtimeError: string | null;

  // Core data
  genrePresets: GenrePreset[];
  books: Book[];
  activeBookId: number | null;
  activeBook: Book | null;
  activeGenrePreset: GenrePreset | null;
  volumes: Volume[];
  chapters: Chapter[];
  activeChapterId: number | null;
  activeChapter: Chapter | null;
  entities: Entity[];
  foreshadowings: Foreshadowing[];
  providers: ProviderPublic[];
  modelRoutes: ModelRoute[];
  memoryRevision: number;

  // Actions
  selectBook: (bookId: number) => Promise<void>;
  selectChapter: (chapterId: number | null) => Promise<void>;
  refreshBooks: (preferBookId?: number | null) => Promise<void>;
  refreshBookData: (bookId?: number | null, preferChapterId?: number | null) => Promise<void>;
  refreshChapters: (bookId?: number | null) => Promise<void>;
  refreshActiveChapter: (chapterId?: number | null) => Promise<Chapter | null>;
  updateChapterInCache: (chapter: Chapter) => void;
  refreshEntities: (bookId?: number | null) => Promise<void>;
  refreshForeshadowings: (bookId?: number | null) => Promise<void>;
  refreshSettingsData: () => Promise<void>;
  bumpMemoryRevision: () => void;

  // Shortcut hooks for active view
  registerSaveHandler: (handler: (() => void | Promise<void>) | null) => void;
  triggerSave: () => void;
  registerGenerateHandler: (handler: (() => void) | null) => void;
  triggerGenerate: () => void;
}

const AppContext = createContext<AppContextValue | null>(null);

async function safeListForeshadowings(bookId: number): Promise<Foreshadowing[]> {
  try {
    return await MemoryService.listForeshadowings(bookId, "");
  } catch {
    return [];
  }
}

export function AppProvider({ children }: { children: ReactNode }) {
  return (
    <ToastProvider>
      <AppProviderInner>{children}</AppProviderInner>
    </ToastProvider>
  );
}

function AppProviderInner({ children }: { children: ReactNode }) {
  const { notify, dismissToast } = useToastActions();

  const [prefs, setPrefs] = useState<NibPreferences>(() => {
    const initial = loadPreferences();
    applyDocumentPreferences(initial);
    return initial;
  });

  const [viewMode, setViewMode] = useState<AppViewMode>("home");
  const [activeTab, setActiveTab] = useState<WorkspaceTab>("architect");
  const [atlasSubTab, setAtlasSubTab] = useState<AtlasSubTab>("entities");
  const [atlasFocusEntityId, setAtlasFocusEntityId] = useState<number | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsTab, setSettingsTab] = useState<SettingsTab>("providers");
  const [createBookOpen, setCreateBookOpen] = useState(false);
  const [createBookInitialMode, setCreateBookInitialMode] = useState<CreateBookMode>("ai");
  const [memorySearchOpen, setMemorySearchOpen] = useState(false);
  const [directorOpen, setDirectorOpen] = useState(false);
  const [directorTab, setDirectorTab] = useState<DirectorTab>("chat");
  const [notesOpen, setNotesOpen] = useState(false);
  const [focusMode, setFocusMode] = useState(false);
  const [commandPaletteOpen, setCommandPaletteOpen] = useState(false);

  const [bootstrapping, setBootstrapping] = useState(true);
  const [bookLoading, setBookLoading] = useState(false);
  const [runtimeError, setRuntimeError] = useState<string | null>(null);

  const [genrePresets, setGenrePresets] = useState<GenrePreset[]>([]);
  const [books, setBooks] = useState<Book[]>([]);
  const [activeBookId, setActiveBookId] = useState<number | null>(null);
  const [volumes, setVolumes] = useState<Volume[]>([]);
  const [chapters, setChapters] = useState<Chapter[]>([]);
  const [activeChapterId, setActiveChapterId] = useState<number | null>(null);
  const [activeChapter, setActiveChapter] = useState<Chapter | null>(null);
  const [entities, setEntities] = useState<Entity[]>([]);
  const [foreshadowings, setForeshadowings] = useState<Foreshadowing[]>([]);
  const [providers, setProviders] = useState<ProviderPublic[]>([]);
  const [modelRoutes, setModelRoutes] = useState<ModelRoute[]>([]);
  const [memoryRevision, setMemoryRevision] = useState(0);

  const saveHandlerRef = useRef<(() => void | Promise<void>) | null>(null);
  const generateHandlerRef = useRef<(() => void) | null>(null);
  const chapterReqIdRef = useRef(0);
  const bookReqIdRef = useRef(0);
  const activeBookIdRef = useRef<number | null>(activeBookId);
  activeBookIdRef.current = activeBookId;

  const updatePrefs = useCallback((updater: (prev: NibPreferences) => NibPreferences) => {
    setPrefs((prev) => {
      const next = updater(prev);
      savePreferences(next);
      applyDocumentPreferences(next);
      return next;
    });
  }, []);

  const loadChapterDetail = useCallback(
    async (chapterId: number | null): Promise<Chapter | null> => {
      const reqId = ++chapterReqIdRef.current;
      if (!chapterId) {
        setActiveChapterId(null);
        setActiveChapter(null);
        updatePrefs((p) => ({ ...p, lastChapterId: null }));
        return null;
      }
      setActiveChapterId(chapterId);
      updatePrefs((p) => ({ ...p, lastChapterId: chapterId }));
      try {
        const chap = await LibraryService.getChapter(chapterId);
        if (chapterReqIdRef.current !== reqId) {
          return null;
        }
        setActiveChapter(chap);
        setChapters((prev) => prev.map((c) => (c.id === chap.id ? chap : c)));
        return chap;
      } catch (err) {
        if (chapterReqIdRef.current === reqId) {
          notify({ tone: "danger", message: "读取章节失败", detail: formatErrorMessage(err) });
        }
        return null;
      }
    },
    [notify, updatePrefs]
  );

  const refreshEntities = useCallback(
    async (targetBookId?: number | null) => {
      const bid = targetBookId ?? activeBookId;
      if (!bid) {
        setEntities([]);
        return;
      }
      try {
        const list = await AtlasService.listEntities(bid, {});
        setEntities(list);
      } catch (err) {
        notify({ tone: "danger", message: "加载设定实体失败", detail: formatErrorMessage(err) });
      }
    },
    [activeBookId, notify]
  );

  const refreshForeshadowings = useCallback(
    async (targetBookId?: number | null) => {
      const bid = targetBookId ?? activeBookId;
      if (!bid) {
        setForeshadowings([]);
        return;
      }
      const list = await safeListForeshadowings(bid);
      setForeshadowings(list);
    },
    [activeBookId]
  );

  const refreshChapters = useCallback(
    async (targetBookId?: number | null) => {
      const bid = targetBookId ?? activeBookId;
      if (!bid) {
        setChapters([]);
        return;
      }
      try {
        const chaps = await LibraryService.listBookChapters(bid);
        setChapters(chaps);
      } catch (err) {
        notify({ tone: "danger", message: "刷新章节列表失败", detail: formatErrorMessage(err) });
      }
    },
    [activeBookId, notify]
  );

  const refreshBookData = useCallback(
    async (targetBookId?: number | null, preferChapterId?: number | null) => {
      const reqId = ++bookReqIdRef.current;
      const bid = targetBookId ?? activeBookId;
      if (!bid) {
        setVolumes([]);
        setChapters([]);
        setEntities([]);
        setForeshadowings([]);
        setActiveChapterId(null);
        setActiveChapter(null);
        return;
      }
      setBookLoading(true);
      try {
        const [vols, chaps, ents, fss] = await Promise.all([
          LibraryService.listVolumes(bid),
          LibraryService.listBookChapters(bid),
          AtlasService.listEntities(bid, {}),
          safeListForeshadowings(bid),
        ]);
        if (bookReqIdRef.current !== reqId) return;
        setVolumes(vols);
        setChapters(chaps);
        setEntities(ents);
        setForeshadowings(fss);

        const desiredChapterId =
          preferChapterId !== undefined ? preferChapterId : activeChapterId;
        const matched =
          desiredChapterId != null
            ? chaps.find((c) => c.id === desiredChapterId)
            : undefined;
        const nextChap = matched ?? chaps[0] ?? null;

        if (nextChap) {
          await loadChapterDetail(nextChap.id);
        } else {
          setActiveChapterId(null);
          setActiveChapter(null);
        }
      } catch (err) {
        if (bookReqIdRef.current === reqId) {
          notify({ tone: "danger", message: "加载书籍内容失败", detail: formatErrorMessage(err) });
        }
      } finally {
        if (bookReqIdRef.current === reqId) {
          setBookLoading(false);
        }
      }
    },
    [activeBookId, activeChapterId, loadChapterDetail, notify]
  );

  const selectBook = useCallback(
    async (bookId: number) => {
      setActiveBookId(bookId);
      updatePrefs((p) => ({ ...p, lastBookId: bookId }));
      await refreshBookData(bookId, null);
    },
    [refreshBookData, updatePrefs]
  );

  const openHome = useCallback(() => {
    setCreateBookOpen(false);
    setDirectorOpen(false);
    setNotesOpen(false);
    setViewMode("home");
  }, []);

  const openBook = useCallback(
    async (bookId: number, tab?: WorkspaceTab) => {
      setCreateBookOpen(false);
      if (tab) {
        setActiveTab(tab);
      }
      setViewMode("workspace");
      if (bookId !== activeBookId) {
        setActiveBookId(bookId);
        updatePrefs((p) => ({ ...p, lastBookId: bookId }));
        await refreshBookData(bookId, null);
      } else if (volumes.length === 0 && chapters.length === 0) {
        await refreshBookData(bookId, activeChapterId);
      }
    },
    [activeBookId, activeChapterId, chapters.length, refreshBookData, updatePrefs, volumes.length]
  );

  const refreshSettingsData = useCallback(async () => {
    try {
      const [provList, routeList] = await Promise.all([
        SettingsService.listProviders(),
        SettingsService.listModelRoutes(),
      ]);
      setProviders(provList);
      setModelRoutes(routeList);
    } catch (err) {
      notify({ tone: "danger", message: "加载模型配置失败", detail: formatErrorMessage(err) });
    }
  }, [notify]);

  const refreshBooks = useCallback(
    async (preferBookId?: number | null) => {
      try {
        const bookList = await LibraryService.listBooks();
        setBooks(bookList);
        if (bookList.length === 0) {
          setActiveBookId(null);
          setVolumes([]);
          setChapters([]);
          setEntities([]);
          setForeshadowings([]);
          setActiveChapterId(null);
          setActiveChapter(null);
          updatePrefs((p) => ({ ...p, lastBookId: null, lastChapterId: null }));
          return;
        }
        const targetId =
          preferBookId !== undefined ? preferBookId : activeBookId;
        const matched =
          targetId != null ? bookList.find((b) => b.id === targetId) : undefined;
        const nextBook = matched ?? bookList[0];
        if (nextBook) {
          setActiveBookId(nextBook.id);
          updatePrefs((p) => ({ ...p, lastBookId: nextBook.id }));
          await refreshBookData(
            nextBook.id,
            nextBook.id === activeBookId ? activeChapterId : null
          );
        }
      } catch (err) {
        notify({ tone: "danger", message: "加载书架失败", detail: formatErrorMessage(err) });
      }
    },
    [activeBookId, activeChapterId, notify, refreshBookData, updatePrefs]
  );

  const deleteBook = useCallback(
    async (bookId: number) => {
      try {
        await LibraryService.deleteBook(bookId);
        await refreshBooks(activeBookId === bookId ? null : activeBookId);
        notify({ tone: "ok", message: "已删除书籍" });
      } catch (err) {
        notify({ tone: "danger", message: "删除书籍失败", detail: formatErrorMessage(err) });
      }
    },
    [activeBookId, notify, refreshBooks]
  );

  const updateChapterInCache = useCallback((chapter: Chapter) => {
    setChapters((prev) => {
      const exists = prev.some((c) => c.id === chapter.id);
      if (!exists) return [...prev, chapter];
      return prev.map((c) => (c.id === chapter.id ? chapter : c));
    });
    setActiveChapter((prev) => (prev && prev.id === chapter.id ? chapter : prev));
  }, []);

  const refreshActiveChapter = useCallback(
    async (chapterId?: number | null): Promise<Chapter | null> => {
      const cid = chapterId ?? activeChapterId;
      if (!cid) return null;
      return loadChapterDetail(cid);
    },
    [activeChapterId, loadChapterDetail]
  );

  const bumpMemoryRevision = useCallback(() => {
    setMemoryRevision((v) => v + 1);
  }, []);

  // Sync native hidden-titlebar window control button theme with app theme
  useEffect(() => {
    void SettingsService.setWindowTheme(prefs.theme).catch(() => {});
  }, [prefs.theme]);

  // Initial bootstrap
  useEffect(() => {
    let cancelled = false;
    async function boot() {
      setBootstrapping(true);
      setRuntimeError(null);
      try {
        const [presets, bookList, provList, routeList] = await Promise.all([
          LibraryService.listGenrePresets(),
          LibraryService.listBooks(),
          SettingsService.listProviders(),
          SettingsService.listModelRoutes(),
        ]);
        if (cancelled) return;
        setGenrePresets(presets);
        setBooks(bookList);
        setProviders(provList);
        setModelRoutes(routeList);

        if (bookList.length > 0) {
          const initialPrefs = loadPreferences();
          const chosenBook =
            (initialPrefs.lastBookId
              ? bookList.find((b) => b.id === initialPrefs.lastBookId)
              : undefined) ?? bookList[0];

          if (chosenBook) {
            setActiveBookId(chosenBook.id);
            const [vols, chaps, ents, fss] = await Promise.all([
              LibraryService.listVolumes(chosenBook.id),
              LibraryService.listBookChapters(chosenBook.id),
              AtlasService.listEntities(chosenBook.id, {}),
              safeListForeshadowings(chosenBook.id),
            ]);
            if (cancelled) return;
            setVolumes(vols);
            setChapters(chaps);
            setEntities(ents);
            setForeshadowings(fss);

            const chosenChap =
              (initialPrefs.lastChapterId
                ? chaps.find((c) => c.id === initialPrefs.lastChapterId)
                : undefined) ?? chaps[0];

            if (chosenChap) {
              setActiveChapterId(chosenChap.id);
              const detail = await LibraryService.getChapter(chosenChap.id);
              if (!cancelled) {
                setActiveChapter(detail);
              }
            }
          }
        }
      } catch (err) {
        if (!cancelled) {
          setRuntimeError(formatErrorMessage(err));
        }
      } finally {
        if (!cancelled) {
          setBootstrapping(false);
        }
      }
    }
    void boot();
    return () => {
      cancelled = true;
    };
  }, []);

  const activeBook = useMemo(
    () => books.find((b) => b.id === activeBookId) ?? null,
    [books, activeBookId]
  );

  const activeGenrePreset = useMemo(() => {
    if (!activeBook) return genrePresets[0] ?? null;
    return genrePresets.find((g) => g.id === activeBook.genre) ?? genrePresets[0] ?? null;
  }, [activeBook, genrePresets]);

  const setTheme = useCallback(
    (theme: ThemeMode) => updatePrefs((p) => ({ ...p, theme })),
    [updatePrefs]
  );
  const toggleTheme = useCallback(
    () => updatePrefs((p) => ({ ...p, theme: p.theme === "dark" ? "light" : "dark" })),
    [updatePrefs]
  );
  const setProseFont = useCallback(
    (proseFont: ProseFontMode) => updatePrefs((p) => ({ ...p, proseFont })),
    [updatePrefs]
  );
  const setProseIndent = useCallback(
    (proseIndent: boolean) => updatePrefs((p) => ({ ...p, proseIndent })),
    [updatePrefs]
  );
  const setProseFontSize = useCallback(
    (proseFontSize: number) =>
      updatePrefs((p) => ({
        ...p,
        proseFontSize: Math.min(24, Math.max(14, Math.round(proseFontSize))),
      })),
    [updatePrefs]
  );
  const setProseLineHeight = useCallback(
    (proseLineHeight: number) =>
      updatePrefs((p) => ({
        ...p,
        proseLineHeight: Math.min(2.3, Math.max(1.5, Math.round(proseLineHeight * 100) / 100)),
      })),
    [updatePrefs]
  );
  const setProseMaxWidth = useCallback(
    (proseMaxWidth: number) =>
      updatePrefs((p) => ({
        ...p,
        proseMaxWidth: Math.min(960, Math.max(580, Math.round(proseMaxWidth))),
      })),
    [updatePrefs]
  );
  const resetTypography = useCallback(
    () =>
      updatePrefs((p) => ({
        ...p,
        proseFontSize: DEFAULT_TYPOGRAPHY.proseFontSize,
        proseLineHeight: DEFAULT_TYPOGRAPHY.proseLineHeight,
        proseMaxWidth: DEFAULT_TYPOGRAPHY.proseMaxWidth,
      })),
    [updatePrefs]
  );
  const toggleFocusMode = useCallback(() => {
    setFocusMode((v) => {
      const next = !v;
      if (next) {
        setDirectorOpen(false);
        setNotesOpen(false);
      }
      return next;
    });
  }, []);
  const openCommandPalette = useCallback(() => {
    setMemorySearchOpen(false);
    setCommandPaletteOpen(true);
  }, []);
  const closeCommandPalette = useCallback(() => setCommandPaletteOpen(false), []);
  const toggleCommandPalette = useCallback(() => {
    setCommandPaletteOpen((v) => {
      if (!v) setMemorySearchOpen(false);
      return !v;
    });
  }, []);
  const setStudioDrawerOpen = useCallback(
    (studioDrawerOpen: boolean) => {
      if (studioDrawerOpen) {
        setDirectorOpen(false);
        setNotesOpen(false);
      }
      updatePrefs((p) => ({ ...p, studioDrawerOpen }));
    },
    [updatePrefs]
  );
  const toggleStudioDrawer = useCallback(() => {
    updatePrefs((p) => {
      const next = !p.studioDrawerOpen;
      if (next) {
        setDirectorOpen(false);
        setNotesOpen(false);
      }
      return { ...p, studioDrawerOpen: next };
    });
  }, [updatePrefs]);

  const openAtlasSubTab = useCallback((sub: AtlasSubTab) => {
    setAtlasSubTab(sub);
    setActiveTab("atlas");
  }, []);

  const clearAtlasFocusEntity = useCallback(() => {
    setAtlasFocusEntityId(null);
  }, []);

  const jumpToChapter = useCallback(
    async (chapterId: number) => {
      await loadChapterDetail(chapterId);
      setActiveTab("studio");
    },
    [loadChapterDetail]
  );

  const jumpToEntity = useCallback((entityId: number) => {
    setAtlasSubTab("entities");
    setAtlasFocusEntityId(entityId);
    setActiveTab("atlas");
  }, []);

  const openSettings = useCallback((tab: SettingsTab = "providers") => {
    setCommandPaletteOpen(false);
    setMemorySearchOpen(false);
    setSettingsTab(tab);
    setSettingsOpen(true);
  }, []);
  const closeSettings = useCallback(() => setSettingsOpen(false), []);
  const openCreateBook = useCallback((mode: CreateBookMode = "ai") => {
    setCommandPaletteOpen(false);
    setMemorySearchOpen(false);
    setCreateBookInitialMode(mode);
    setCreateBookOpen(true);
  }, []);
  const closeCreateBook = useCallback(() => setCreateBookOpen(false), []);
  const openMemorySearch = useCallback(() => {
    setCommandPaletteOpen(false);
    setMemorySearchOpen(true);
  }, []);
  const closeMemorySearch = useCallback(() => setMemorySearchOpen(false), []);
  const toggleMemorySearch = useCallback(() => {
    setMemorySearchOpen((v) => {
      if (!v) setCommandPaletteOpen(false);
      return !v;
    });
  }, []);

  const registerSaveHandler = useCallback(
    (handler: (() => void | Promise<void>) | null) => {
      saveHandlerRef.current = handler;
    },
    []
  );
  const triggerSave = useCallback(() => {
    if (saveHandlerRef.current) {
      void saveHandlerRef.current();
    }
  }, []);

  const registerGenerateHandler = useCallback((handler: (() => void) | null) => {
    generateHandlerRef.current = handler;
  }, []);
  const triggerGenerate = useCallback(() => {
    if (generateHandlerRef.current) {
      generateHandlerRef.current();
    }
  }, []);

  const openDirector = useCallback(
    (tab?: DirectorTab) => {
      if (tab) setDirectorTab(tab);
      setNotesOpen(false);
      updatePrefs((p) => (p.studioDrawerOpen ? { ...p, studioDrawerOpen: false } : p));
      setDirectorOpen(true);
    },
    [updatePrefs]
  );
  const closeDirector = useCallback(() => setDirectorOpen(false), []);
  const toggleDirector = useCallback(() => {
    setDirectorOpen((v) => {
      const next = !v;
      if (next) {
        setNotesOpen(false);
        updatePrefs((p) => (p.studioDrawerOpen ? { ...p, studioDrawerOpen: false } : p));
      }
      return next;
    });
  }, [updatePrefs]);

  const openNotes = useCallback(() => {
    setDirectorOpen(false);
    updatePrefs((p) => (p.studioDrawerOpen ? { ...p, studioDrawerOpen: false } : p));
    setNotesOpen(true);
  }, [updatePrefs]);
  const closeNotes = useCallback(() => setNotesOpen(false), []);
  const toggleNotes = useCallback(() => {
    setNotesOpen((v) => {
      const next = !v;
      if (next) {
        setDirectorOpen(false);
        updatePrefs((p) => (p.studioDrawerOpen ? { ...p, studioDrawerOpen: false } : p));
      }
      return next;
    });
  }, [updatePrefs]);

  const refreshBookDataRef = useRef(refreshBookData);
  refreshBookDataRef.current = refreshBookData;
  const handleAutopilotProgressRefresh = useCallback(
    (bookId: number) => {
      void refreshBookDataRef.current(bookId);
      bumpMemoryRevision();
    },
    [bumpMemoryRevision]
  );

  const value = useMemo<AppContextValue>(
    () => ({
      theme: prefs.theme,
      proseFont: prefs.proseFont,
      proseIndent: prefs.proseIndent,
      proseFontSize: prefs.proseFontSize,
      proseLineHeight: prefs.proseLineHeight,
      proseMaxWidth: prefs.proseMaxWidth,
      studioDrawerOpen: prefs.studioDrawerOpen,
      focusMode,
      setTheme,
      toggleTheme,
      setProseFont,
      setProseIndent,
      setProseFontSize,
      setProseLineHeight,
      setProseMaxWidth,
      resetTypography,
      setStudioDrawerOpen,
      toggleStudioDrawer,
      setFocusMode,
      toggleFocusMode,
      commandPaletteOpen,
      openCommandPalette,
      closeCommandPalette,
      toggleCommandPalette,
      viewMode,
      openHome,
      openBook,
      deleteBook,
      activeTab,
      setActiveTab,
      atlasSubTab,
      setAtlasSubTab,
      openAtlasSubTab,
      atlasFocusEntityId,
      clearAtlasFocusEntity,
      jumpToChapter,
      jumpToEntity,
      settingsOpen,
      settingsTab,
      openSettings,
      closeSettings,
      createBookOpen,
      createBookInitialMode,
      openCreateBook,
      closeCreateBook,
      memorySearchOpen,
      openMemorySearch,
      closeMemorySearch,
      toggleMemorySearch,
      directorOpen,
      directorTab,
      setDirectorTab,
      openDirector,
      closeDirector,
      toggleDirector,
      notesOpen,
      openNotes,
      closeNotes,
      toggleNotes,
      notify,
      dismissToast,
      bootstrapping,
      bookLoading,
      runtimeError,
      genrePresets,
      books,
      activeBookId,
      activeBook,
      activeGenrePreset,
      volumes,
      chapters,
      activeChapterId,
      activeChapter,
      entities,
      foreshadowings,
      providers,
      modelRoutes,
      memoryRevision,
      selectBook,
      selectChapter: async (cid) => {
        await loadChapterDetail(cid);
      },
      refreshBooks,
      refreshBookData,
      refreshChapters,
      refreshActiveChapter,
      updateChapterInCache,
      refreshEntities,
      refreshForeshadowings,
      refreshSettingsData,
      bumpMemoryRevision,
      registerSaveHandler,
      triggerSave,
      registerGenerateHandler,
      triggerGenerate,
    }),
    [
      prefs.theme,
      prefs.proseFont,
      prefs.proseIndent,
      prefs.proseFontSize,
      prefs.proseLineHeight,
      prefs.proseMaxWidth,
      prefs.studioDrawerOpen,
      focusMode,
      setTheme,
      toggleTheme,
      setProseFont,
      setProseIndent,
      setProseFontSize,
      setProseLineHeight,
      setProseMaxWidth,
      resetTypography,
      setStudioDrawerOpen,
      toggleStudioDrawer,
      toggleFocusMode,
      commandPaletteOpen,
      openCommandPalette,
      closeCommandPalette,
      toggleCommandPalette,
      viewMode,
      openHome,
      openBook,
      deleteBook,
      activeTab,
      atlasSubTab,
      openAtlasSubTab,
      atlasFocusEntityId,
      clearAtlasFocusEntity,
      jumpToChapter,
      jumpToEntity,
      settingsOpen,
      settingsTab,
      openSettings,
      closeSettings,
      createBookOpen,
      createBookInitialMode,
      openCreateBook,
      closeCreateBook,
      memorySearchOpen,
      openMemorySearch,
      closeMemorySearch,
      toggleMemorySearch,
      directorOpen,
      directorTab,
      setDirectorTab,
      openDirector,
      closeDirector,
      toggleDirector,
      notesOpen,
      openNotes,
      closeNotes,
      toggleNotes,
      notify,
      dismissToast,
      bootstrapping,
      bookLoading,
      runtimeError,
      genrePresets,
      books,
      activeBookId,
      activeBook,
      activeGenrePreset,
      volumes,
      chapters,
      activeChapterId,
      activeChapter,
      entities,
      foreshadowings,
      providers,
      modelRoutes,
      memoryRevision,
      selectBook,
      loadChapterDetail,
      refreshBooks,
      refreshBookData,
      refreshChapters,
      refreshActiveChapter,
      updateChapterInCache,
      refreshEntities,
      refreshForeshadowings,
      refreshSettingsData,
      bumpMemoryRevision,
      registerSaveHandler,
      triggerSave,
      registerGenerateHandler,
      triggerGenerate,
    ]
  );

  return (
    <AutopilotProvider
      activeBookId={activeBookId}
      onProgressRefresh={handleAutopilotProgressRefresh}
    >
      <AppContext.Provider value={value}>{children}</AppContext.Provider>
    </AutopilotProvider>
  );
}

export function useApp(): AppContextValue {
  const ctx = useContext(AppContext);
  if (!ctx) {
    throw new Error("useApp must be used inside AppProvider");
  }
  return ctx;
}
