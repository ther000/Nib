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
  SettingsService,
  type Book,
  type Chapter,
  type Entity,
  type GenrePreset,
  type ModelRoute,
  type ProviderPublic,
  type Volume,
} from "../mygo";
import { formatErrorMessage, type WorkspaceTab } from "../lib/constants";
import {
  applyDocumentPreferences,
  loadPreferences,
  savePreferences,
  type NibPreferences,
  type ProseFontMode,
  type ThemeMode,
} from "../lib/preferences";

export type ToastTone = "info" | "ok" | "warn" | "danger";

export interface ToastItem {
  id: number;
  tone: ToastTone;
  message: string;
  detail?: string;
  actionLabel?: string;
  onAction?: () => void;
}

export type SettingsTab = "providers" | "routes" | "workspace";

interface AppContextValue {
  // Preferences
  theme: ThemeMode;
  proseFont: ProseFontMode;
  proseIndent: boolean;
  studioDrawerOpen: boolean;
  setTheme: (theme: ThemeMode) => void;
  toggleTheme: () => void;
  setProseFont: (font: ProseFontMode) => void;
  setProseIndent: (indent: boolean) => void;
  setStudioDrawerOpen: (open: boolean) => void;
  toggleStudioDrawer: () => void;

  // Navigation & Modals
  activeTab: WorkspaceTab;
  setActiveTab: (tab: WorkspaceTab) => void;
  settingsOpen: boolean;
  settingsTab: SettingsTab;
  openSettings: (tab?: SettingsTab) => void;
  closeSettings: () => void;
  createBookOpen: boolean;
  openCreateBook: () => void;
  closeCreateBook: () => void;

  // Toasts
  toasts: ToastItem[];
  notify: (input: Omit<ToastItem, "id"> & { durationMs?: number }) => void;
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
  providers: ProviderPublic[];
  modelRoutes: ModelRoute[];

  // Actions
  selectBook: (bookId: number) => Promise<void>;
  selectChapter: (chapterId: number | null) => Promise<void>;
  refreshBooks: (preferBookId?: number | null) => Promise<void>;
  refreshBookData: (bookId?: number | null, preferChapterId?: number | null) => Promise<void>;
  refreshActiveChapter: (chapterId?: number | null) => Promise<Chapter | null>;
  updateChapterInCache: (chapter: Chapter) => void;
  refreshEntities: (bookId?: number | null) => Promise<void>;
  refreshSettingsData: () => Promise<void>;

  // Shortcut hooks for active view
  registerSaveHandler: (handler: (() => void | Promise<void>) | null) => void;
  triggerSave: () => void;
  registerGenerateHandler: (handler: (() => void) | null) => void;
  triggerGenerate: () => void;
}

const AppContext = createContext<AppContextValue | null>(null);

export function AppProvider({ children }: { children: ReactNode }) {
  const [prefs, setPrefs] = useState<NibPreferences>(() => {
    const initial = loadPreferences();
    applyDocumentPreferences(initial);
    return initial;
  });

  const [activeTab, setActiveTab] = useState<WorkspaceTab>("architect");
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsTab, setSettingsTab] = useState<SettingsTab>("providers");
  const [createBookOpen, setCreateBookOpen] = useState(false);

  const [toasts, setToasts] = useState<ToastItem[]>([]);
  const toastSeqRef = useRef(1);
  const toastTimersRef = useRef<Map<number, number>>(new Map());

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
  const [providers, setProviders] = useState<ProviderPublic[]>([]);
  const [modelRoutes, setModelRoutes] = useState<ModelRoute[]>([]);

  const saveHandlerRef = useRef<(() => void | Promise<void>) | null>(null);
  const generateHandlerRef = useRef<(() => void) | null>(null);

  const updatePrefs = useCallback((updater: (prev: NibPreferences) => NibPreferences) => {
    setPrefs((prev) => {
      const next = updater(prev);
      savePreferences(next);
      applyDocumentPreferences(next);
      return next;
    });
  }, []);

  const dismissToast = useCallback((id: number) => {
    const timer = toastTimersRef.current.get(id);
    if (timer !== undefined) {
      window.clearTimeout(timer);
      toastTimersRef.current.delete(id);
    }
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const notify = useCallback(
    (input: Omit<ToastItem, "id"> & { durationMs?: number }) => {
      const id = toastSeqRef.current++;
      const duration = input.durationMs ?? (input.tone === "danger" ? 6500 : 3800);
      const item: ToastItem = {
        id,
        tone: input.tone,
        message: input.message,
        detail: input.detail,
        actionLabel: input.actionLabel,
        onAction: input.onAction,
      };
      setToasts((prev) => [...prev.slice(-3), item]);
      if (duration > 0) {
        const timer = window.setTimeout(() => {
          toastTimersRef.current.delete(id);
          setToasts((prev) => prev.filter((t) => t.id !== id));
        }, duration);
        toastTimersRef.current.set(id, timer);
      }
    },
    []
  );

  useEffect(() => {
    const timers = toastTimersRef.current;
    return () => {
      for (const timer of timers.values()) {
        window.clearTimeout(timer);
      }
      timers.clear();
    };
  }, []);

  const loadChapterDetail = useCallback(
    async (chapterId: number | null): Promise<Chapter | null> => {
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
        setActiveChapter(chap);
        setChapters((prev) => prev.map((c) => (c.id === chap.id ? chap : c)));
        return chap;
      } catch (err) {
        notify({ tone: "danger", message: "读取章节失败", detail: formatErrorMessage(err) });
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

  const refreshBookData = useCallback(
    async (targetBookId?: number | null, preferChapterId?: number | null) => {
      const bid = targetBookId ?? activeBookId;
      if (!bid) {
        setVolumes([]);
        setChapters([]);
        setEntities([]);
        setActiveChapterId(null);
        setActiveChapter(null);
        return;
      }
      setBookLoading(true);
      try {
        const [vols, chaps, ents] = await Promise.all([
          LibraryService.listVolumes(bid),
          LibraryService.listBookChapters(bid),
          AtlasService.listEntities(bid, {}),
        ]);
        setVolumes(vols);
        setChapters(chaps);
        setEntities(ents);

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
        notify({ tone: "danger", message: "加载书籍内容失败", detail: formatErrorMessage(err) });
      } finally {
        setBookLoading(false);
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
            const [vols, chaps, ents] = await Promise.all([
              LibraryService.listVolumes(chosenBook.id),
              LibraryService.listBookChapters(chosenBook.id),
              AtlasService.listEntities(chosenBook.id, {}),
            ]);
            if (cancelled) return;
            setVolumes(vols);
            setChapters(chaps);
            setEntities(ents);

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
  const setStudioDrawerOpen = useCallback(
    (studioDrawerOpen: boolean) => updatePrefs((p) => ({ ...p, studioDrawerOpen })),
    [updatePrefs]
  );
  const toggleStudioDrawer = useCallback(
    () => updatePrefs((p) => ({ ...p, studioDrawerOpen: !p.studioDrawerOpen })),
    [updatePrefs]
  );

  const openSettings = useCallback((tab: SettingsTab = "providers") => {
    setSettingsTab(tab);
    setSettingsOpen(true);
  }, []);
  const closeSettings = useCallback(() => setSettingsOpen(false), []);
  const openCreateBook = useCallback(() => setCreateBookOpen(true), []);
  const closeCreateBook = useCallback(() => setCreateBookOpen(false), []);

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

  const value = useMemo<AppContextValue>(
    () => ({
      theme: prefs.theme,
      proseFont: prefs.proseFont,
      proseIndent: prefs.proseIndent,
      studioDrawerOpen: prefs.studioDrawerOpen,
      setTheme,
      toggleTheme,
      setProseFont,
      setProseIndent,
      setStudioDrawerOpen,
      toggleStudioDrawer,
      activeTab,
      setActiveTab,
      settingsOpen,
      settingsTab,
      openSettings,
      closeSettings,
      createBookOpen,
      openCreateBook,
      closeCreateBook,
      toasts,
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
      providers,
      modelRoutes,
      selectBook,
      selectChapter: async (cid) => {
        await loadChapterDetail(cid);
      },
      refreshBooks,
      refreshBookData,
      refreshActiveChapter,
      updateChapterInCache,
      refreshEntities,
      refreshSettingsData,
      registerSaveHandler,
      triggerSave,
      registerGenerateHandler,
      triggerGenerate,
    }),
    [
      prefs.theme,
      prefs.proseFont,
      prefs.proseIndent,
      prefs.studioDrawerOpen,
      setTheme,
      toggleTheme,
      setProseFont,
      setProseIndent,
      setStudioDrawerOpen,
      toggleStudioDrawer,
      activeTab,
      settingsOpen,
      settingsTab,
      openSettings,
      closeSettings,
      createBookOpen,
      openCreateBook,
      closeCreateBook,
      toasts,
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
      providers,
      modelRoutes,
      selectBook,
      loadChapterDetail,
      refreshBooks,
      refreshBookData,
      refreshActiveChapter,
      updateChapterInCache,
      refreshEntities,
      refreshSettingsData,
      registerSaveHandler,
      triggerSave,
      registerGenerateHandler,
      triggerGenerate,
    ]
  );

  return <AppContext.Provider value={value}>{children}</AppContext.Provider>;
}

export function useApp(): AppContextValue {
  const ctx = useContext(AppContext);
  if (!ctx) {
    throw new Error("useApp must be used inside AppProvider");
  }
  return ctx;
}
