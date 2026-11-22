import { useEffect, useRef, useState } from "react";
import {
  Check,
  ChevronDown,
  Feather,
  Home,
  Lightbulb,
  Loader2,
  Maximize2,
  Moon,
  Plus,
  Search,
  Settings,
  ShieldCheck,
  SlidersHorizontal,
  Sparkles,
  Sun,
} from "lucide-react";
import { useApp } from "../stores/AppContext";
import { useAutopilotState } from "../stores/AutopilotContext";
import type { WorkspaceTab } from "../lib/constants";

const CORE_TABS: Array<{ id: WorkspaceTab; label: string; shortcut: string }> = [
  { id: "architect", label: "大纲", shortcut: "Ctrl+1" },
  { id: "studio", label: "写作", shortcut: "Ctrl+2" },
  { id: "atlas", label: "设定", shortcut: "Ctrl+3" },
];

export function TopBar() {
  const {
    theme,
    toggleTheme,
    viewMode,
    openHome,
    openBook,
    activeTab,
    setActiveTab,
    openAtlasSubTab,
    books,
    activeBook,
    activeChapter,
    genrePresets,
    openSettings,
    openCreateBook,
    openMemorySearch,
    directorOpen,
    openDirector,
    toggleDirector,
    notesOpen,
    toggleNotes,
    setFocusMode,
    toggleFocusMode,
  } = useApp();
  const { autopilotState } = useAutopilotState();

  const [menuOpen, setMenuOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  const inWorkspace = viewMode === "workspace" && Boolean(activeBook);

  useEffect(() => {
    if (!menuOpen) return;
    const handlePointerDown = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setMenuOpen(false);
      }
    };
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setMenuOpen(false);
    };
    document.addEventListener("mousedown", handlePointerDown);
    document.addEventListener("keydown", handleKey);
    return () => {
      document.removeEventListener("mousedown", handlePointerDown);
      document.removeEventListener("keydown", handleKey);
    };
  }, [menuOpen]);

  const getGenreName = (genreId: string) =>
    genrePresets.find((g) => g.id === genreId)?.name ?? genreId;

  const isAutopilotWaiting =
    Boolean(activeBook) &&
    autopilotState?.bookId === activeBook?.id &&
    autopilotState?.status === "waiting_decision";
  const isAutopilotRunning =
    Boolean(activeBook) &&
    autopilotState?.bookId === activeBook?.id &&
    autopilotState?.status === "running";

  return (
    <header
      className="nib-drag-region relative z-30 grid shrink-0 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-2 select-none"
      style={{
        height: "var(--mygo-titlebar-height, var(--topbar-height))",
        paddingLeft: "calc(10px + var(--mygo-titlebar-inset-left, 0px))",
        paddingRight: "calc(10px + var(--mygo-titlebar-inset-right, 0px))",
      }}
    >
      {/* Left: Home + Book Switcher */}
      <div ref={menuRef} className="relative flex min-w-0 items-center justify-self-start gap-1">
        {inWorkspace && activeBook ? (
          <>
            <button
              type="button"
              onClick={() => {
                setMenuOpen(false);
                openHome();
              }}
              title="返回首页书架"
              aria-label="返回首页书架"
              className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-ink-secondary transition-colors hover:bg-[var(--bg-hover)] hover:text-ink-primary"
            >
              <Home className="h-3.5 w-3.5" />
            </button>

            <button
              type="button"
              onClick={() => setMenuOpen((v) => !v)}
              className={`flex h-7 max-w-[232px] items-center gap-1.5 rounded-lg px-2 text-[13px] font-medium transition-colors ${
                menuOpen
                  ? "bg-elevated text-ink-primary shadow-xs"
                  : "text-ink-primary hover:bg-[var(--bg-hover)]"
              }`}
            >
              <Feather className="h-3.5 w-3.5 shrink-0 text-accent" />
              <span className="truncate">《{activeBook.title}》</span>
              <ChevronDown
                className={`h-3.5 w-3.5 shrink-0 text-ink-muted transition-transform duration-150 ${
                  menuOpen ? "rotate-180" : ""
                }`}
              />
            </button>

            {menuOpen && (
              <div className="nib-no-drag nib-floating-dock absolute top-[calc(100%+6px)] left-0 z-50 w-64 rounded-xl py-1.5">
                <div className="max-h-68 overflow-y-auto px-1.5">
                  {books.map((book) => {
                    const selected = book.id === activeBook.id;
                    return (
                      <button
                        key={book.id}
                        type="button"
                        onClick={() => {
                          setMenuOpen(false);
                          if (!selected) {
                            void openBook(book.id);
                          }
                        }}
                        className={`flex w-full items-center justify-between gap-2 rounded-lg px-2.5 py-2 text-left text-[13px] transition-colors ${
                          selected
                            ? "bg-accent-soft text-accent"
                            : "text-ink-primary hover:bg-[var(--bg-hover)]"
                        }`}
                      >
                        <div className="min-w-0 flex-1">
                          <div className="truncate font-medium">{book.title}</div>
                          <div className="mt-0.5 flex items-center gap-2 text-xs text-ink-secondary">
                            <span>{getGenreName(book.genre)}</span>
                            <span className="nib-mono">
                              {book.targetChapterWords.toLocaleString("zh-CN")} 字/章
                            </span>
                          </div>
                        </div>
                        {selected && <Check className="h-3.5 w-3.5 shrink-0 text-accent" />}
                      </button>
                    );
                  })}
                </div>

                <div className="my-1.5 border-t border-border-subtle" />

                <div className="flex flex-col gap-0.5 px-1.5">
                  <button
                    type="button"
                    onClick={() => {
                      setMenuOpen(false);
                      setActiveTab("book_settings");
                    }}
                    className={`flex w-full items-center justify-between rounded-lg px-2.5 py-1.5 text-xs transition-colors ${
                      activeTab === "book_settings"
                        ? "bg-accent-soft font-medium text-accent"
                        : "text-ink-secondary hover:bg-[var(--bg-hover)] hover:text-ink-primary"
                    }`}
                  >
                    <span className="flex items-center gap-2">
                      <SlidersHorizontal className="h-3.5 w-3.5" />
                      <span>本书设置与导出</span>
                    </span>
                    <span className="nib-mono text-[10px] opacity-65">Ctrl+4</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setMenuOpen(false);
                      openAtlasSubTab("audit");
                    }}
                    className="flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-xs text-ink-secondary transition-colors hover:bg-[var(--bg-hover)] hover:text-ink-primary"
                  >
                    <ShieldCheck className="h-3.5 w-3.5" />
                    <span>全书体检</span>
                  </button>
                </div>

                <div className="my-1.5 border-t border-border-subtle" />

                <div className="flex flex-col gap-0.5 px-1.5">
                  <button
                    type="button"
                    onClick={() => {
                      setMenuOpen(false);
                      openCreateBook();
                    }}
                    className="flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-xs text-ink-secondary transition-colors hover:bg-[var(--bg-hover)] hover:text-ink-primary"
                  >
                    <Plus className="h-3.5 w-3.5 text-accent" />
                    <span>新建书籍</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setMenuOpen(false);
                      openHome();
                    }}
                    className="flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-xs text-ink-secondary transition-colors hover:bg-[var(--bg-hover)] hover:text-ink-primary"
                  >
                    <Home className="h-3.5 w-3.5 text-ink-muted" />
                    <span>返回首页书架</span>
                  </button>
                </div>
              </div>
            )}
          </>
        ) : (
          <div className="flex items-center gap-2 px-1.5 text-[13px] font-medium tracking-wide text-ink-primary">
            <Feather className="h-3.5 w-3.5 text-accent" />
            <span>Nib</span>
          </div>
        )}
      </div>

      {/* Center: Clean 3-Segment Native Pill */}
      {inWorkspace ? (
        <nav
          aria-label="工作区切换"
          className="nib-no-drag flex shrink-0 items-center justify-self-center gap-0.5 whitespace-nowrap rounded-lg bg-[var(--bg-hover)] p-0.5"
        >
          {CORE_TABS.map((tab) => {
            const active = activeTab === tab.id;
            return (
              <button
                key={tab.id}
                type="button"
                title={`${tab.label} (${tab.shortcut})`}
                onClick={() => setActiveTab(tab.id)}
                className={`rounded-[6px] px-3.5 py-1 text-xs font-medium transition-colors ${
                  active
                    ? "bg-elevated text-ink-primary shadow-xs"
                    : "text-ink-secondary hover:text-ink-primary"
                }`}
              >
                {tab.label}
              </button>
            );
          })}
          {activeTab === "book_settings" && (
            <button
              type="button"
              title="本书设置 (Ctrl+4)"
              onClick={() => setActiveTab("book_settings")}
              className="rounded-[6px] bg-elevated px-3 py-1 text-xs font-medium text-accent shadow-xs"
            >
              本书设置
            </button>
          )}
        </nav>
      ) : (
        <div aria-hidden="true" />
      )}

      {/* Right: Calm High-Frequency Utilities */}
      <div className="flex shrink-0 items-center justify-self-end gap-1">
        {inWorkspace && activeBook && (
          <>
            <button
              type="button"
              onClick={openMemorySearch}
              title="全书检索 (Ctrl+Shift+F)"
              aria-label="全书检索"
              className="flex h-7 items-center gap-1.5 rounded-lg bg-[var(--bg-hover)] px-2.5 text-xs text-ink-secondary transition-colors hover:bg-elevated hover:text-ink-primary"
            >
              <Search className="h-3.5 w-3.5" />
              <span className="hidden md:inline">检索</span>
            </button>

            <button
              type="button"
              onClick={() => {
                if (!directorOpen && isAutopilotWaiting) {
                  openDirector("autopilot");
                } else {
                  toggleDirector();
                }
              }}
              title="AI 导演与自动托管 (Ctrl+K)"
              className={`flex h-7 items-center gap-1.5 rounded-lg px-2.5 text-xs font-medium transition-colors ${
                isAutopilotWaiting
                  ? "bg-warn-soft text-warn"
                  : directorOpen || isAutopilotRunning
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:bg-[var(--bg-hover)] hover:text-ink-primary"
              }`}
            >
              {isAutopilotRunning ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Sparkles className="h-3.5 w-3.5" />
              )}
              <span>
                {isAutopilotWaiting
                  ? "托管待确认"
                  : isAutopilotRunning && autopilotState
                    ? `托管 ${autopilotState.completedChapters}/${autopilotState.targetChapters}`
                    : "导演"}
              </span>
            </button>

            <button
              type="button"
              onClick={toggleNotes}
              title="灵感便签"
              aria-label="灵感便签"
              className={`flex h-7 w-7 items-center justify-center rounded-lg transition-colors ${
                notesOpen
                  ? "bg-accent-soft text-accent"
                  : "text-ink-secondary hover:bg-[var(--bg-hover)] hover:text-ink-primary"
              }`}
            >
              <Lightbulb className="h-3.5 w-3.5" />
            </button>

            <span className="mx-0.5 h-3.5 w-px bg-border-subtle" aria-hidden="true" />
          </>
        )}

        {inWorkspace && activeBook && (
          <button
            type="button"
            disabled={!activeChapter}
            onClick={() => {
              if (!activeChapter) return;
              if (activeTab !== "studio") {
                setActiveTab("studio");
                setFocusMode(true);
              } else {
                toggleFocusMode();
              }
            }}
            title={activeChapter ? "专注写作模式 (F11 / Ctrl+Shift+Z)" : "请先选择或新建章节"}
            aria-label="专注写作模式"
            className="flex h-7 w-7 items-center justify-center rounded-lg text-ink-secondary transition-colors hover:bg-[var(--bg-hover)] hover:text-ink-primary disabled:cursor-not-allowed disabled:opacity-35"
          >
            <Maximize2 className="h-3.5 w-3.5" />
          </button>
        )}

        <button
          type="button"
          onClick={toggleTheme}
          title={theme === "dark" ? "切换清墨浅色" : "切换清墨深色"}
          aria-label="切换主题"
          className="flex h-7 w-7 items-center justify-center rounded-lg text-ink-secondary transition-colors hover:bg-[var(--bg-hover)] hover:text-ink-primary"
        >
          {theme === "dark" ? (
            <Sun className="h-3.5 w-3.5" />
          ) : (
            <Moon className="h-3.5 w-3.5" />
          )}
        </button>

        <button
          type="button"
          onClick={() => openSettings("providers")}
          title="全局设置 (命令面板 Ctrl+Shift+P)"
          aria-label="全局设置"
          className="flex h-7 w-7 items-center justify-center rounded-lg text-ink-secondary transition-colors hover:bg-[var(--bg-hover)] hover:text-ink-primary"
        >
          <Settings className="h-3.5 w-3.5" />
        </button>
      </div>
    </header>
  );
}
