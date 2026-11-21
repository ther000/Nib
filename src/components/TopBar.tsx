import { useEffect, useRef, useState } from "react";
import {
  Bot,
  Check,
  ChevronDown,
  Command,
  Feather,
  Home,
  Lightbulb,
  Loader2,
  Moon,
  Plus,
  Search,
  Settings,
  Sparkles,
  Sun,
} from "lucide-react";
import { useApp } from "../stores/AppContext";
import type { WorkspaceTab } from "../lib/constants";

const WORKSPACE_TABS: Array<{ id: WorkspaceTab; label: string; shortcut: string }> = [
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
    books,
    activeBook,
    genrePresets,
    openSettings,
    openCreateBook,
    openMemorySearch,
    openAutopilot,
    autopilotState,
    directorOpen,
    toggleDirector,
    openCommandPalette,
  } = useApp();

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

  return (
    <header
      className="nib-drag-region relative z-30 flex shrink-0 items-center justify-between border-b border-border-subtle bg-surface select-none"
      style={{
        height: "var(--mygo-titlebar-height, var(--topbar-height))",
        paddingLeft: "calc(12px + var(--mygo-titlebar-inset-left, 0px))",
        paddingRight: "calc(12px + var(--mygo-titlebar-inset-right, 0px))",
      }}
    >
      {/* Left: Brand or Home + Book switcher */}
      <div ref={menuRef} className="relative flex min-w-0 items-center gap-1">
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
              className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-ink-secondary transition-colors hover:bg-elevated hover:text-ink-primary"
            >
              <Home className="h-3.5 w-3.5" />
            </button>

            <span className="h-3.5 w-px bg-border-subtle" aria-hidden="true" />

            <button
              type="button"
              onClick={() => setMenuOpen((v) => !v)}
              className="flex h-7 max-w-[240px] items-center gap-1.5 rounded-md px-2 text-[13px] font-medium text-ink-primary transition-colors hover:bg-elevated"
            >
              <Feather className="h-3.5 w-3.5 shrink-0 text-accent" />
              <span className="truncate">《{activeBook.title}》</span>
              <ChevronDown
                className={`h-3.5 w-3.5 shrink-0 text-ink-muted transition-transform ${
                  menuOpen ? "rotate-180" : ""
                }`}
              />
            </button>

            {menuOpen && (
              <div className="nib-no-drag absolute top-[calc(100%+6px)] left-0 z-50 w-64 rounded-lg border border-border-strong bg-elevated py-1.5 shadow-[var(--shadow-elevated)]">
                <div className="max-h-72 overflow-y-auto px-1">
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
                        className={`flex w-full items-center justify-between gap-2 rounded-md px-2.5 py-2 text-left text-[13px] transition-colors ${
                          selected
                            ? "bg-accent-soft text-accent"
                            : "text-ink-primary hover:bg-surface"
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

                <div className="my-1 border-t border-border-subtle" />

                <div className="flex flex-col gap-0.5 px-1">
                  <button
                    type="button"
                    onClick={() => {
                      setMenuOpen(false);
                      openCreateBook();
                    }}
                    className="flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-[13px] text-ink-secondary transition-colors hover:bg-surface hover:text-ink-primary"
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
                    className="flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-[13px] text-ink-secondary transition-colors hover:bg-surface hover:text-ink-primary"
                  >
                    <Home className="h-3.5 w-3.5 text-ink-muted" />
                    <span>返回首页书架</span>
                  </button>
                </div>
              </div>
            )}
          </>
        ) : (
          <div className="flex items-center gap-2 px-1 text-[13px] font-medium tracking-wide text-ink-primary">
            <Feather className="h-3.5 w-3.5 text-accent" />
            <span>Nib</span>
          </div>
        )}
      </div>

      {/* Center: 3-segment workspace switcher (workspace only) */}
      {inWorkspace ? (
        <nav
          aria-label="工作区切换"
          className="nib-no-drag flex items-center gap-0.5 rounded-lg border border-border-subtle bg-canvas/70 p-0.5"
        >
          {WORKSPACE_TABS.map((tab) => {
            const active = activeTab === tab.id;
            return (
              <button
                key={tab.id}
                type="button"
                title={`${tab.label} (${tab.shortcut})`}
                onClick={() => setActiveTab(tab.id)}
                className={`rounded-[6px] px-3.5 py-1 text-xs font-medium transition-colors ${
                  active
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:text-ink-primary"
                }`}
              >
                {tab.label}
              </button>
            );
          })}
        </nav>
      ) : (
        <div className="flex-1" />
      )}

      {/* Right: AI Director + Autopilot + Memory Search + Theme + Settings */}
      <div className="flex items-center gap-1">
        {inWorkspace && activeBook && (
          <>
            <button
              type="button"
              onClick={() => window.dispatchEvent(new Event("nib:toggle-notes"))}
              title="灵感便签"
              aria-label="灵感便签"
              className="mr-0.5 flex h-7 w-7 items-center justify-center rounded-md text-ink-secondary transition-colors hover:bg-elevated hover:text-ink-primary"
            >
              <Lightbulb className="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              onClick={toggleDirector}
              title="AI 导演总控 (Ctrl+K)"
              className={`flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-xs font-medium transition-colors ${
                directorOpen
                  ? "border-accent/40 bg-accent-soft text-accent"
                  : "border-border-subtle bg-canvas text-ink-secondary hover:border-border-strong hover:text-ink-primary"
              }`}
            >
              <Sparkles className="h-3.5 w-3.5" />
              <span>AI 导演</span>
            </button>
            <button
              type="button"
              onClick={openAutopilot}
              title="AI 自动托管写作"
              className={`mr-0.5 flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-xs font-medium transition-colors ${
                autopilotState?.bookId === activeBook.id &&
                autopilotState.status === "waiting_decision"
                  ? "border-warn/50 bg-warn-soft text-warn"
                  : autopilotState?.bookId === activeBook.id &&
                      autopilotState.status === "running"
                    ? "border-accent/40 bg-accent-soft text-accent"
                    : "border-border-subtle bg-canvas text-ink-secondary hover:border-border-strong hover:text-ink-primary"
              }`}
            >
              {autopilotState?.bookId === activeBook.id &&
              autopilotState.status === "running" ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Bot className="h-3.5 w-3.5" />
              )}
              <span>
                {autopilotState?.bookId === activeBook.id &&
                autopilotState.status === "waiting_decision"
                  ? "托管待确认"
                  : autopilotState?.bookId === activeBook.id &&
                      autopilotState.status === "running"
                    ? `托管 ${autopilotState.completedChapters}/${autopilotState.targetChapters}`
                    : "AI 托管"}
              </span>
            </button>
          </>
        )}
        {inWorkspace && (
          <button
            type="button"
            onClick={openMemorySearch}
            title="全局记忆检索 (Ctrl+Shift+F)"
            aria-label="全局记忆检索"
            className="flex h-7 w-7 items-center justify-center rounded-md text-ink-secondary transition-colors hover:bg-elevated hover:text-ink-primary"
          >
            <Search className="h-3.5 w-3.5" />
          </button>
        )}
        <button
          type="button"
          onClick={openCommandPalette}
          title="命令面板与快捷键速查 (Ctrl+Shift+P)"
          aria-label="命令面板与快捷键"
          className="flex h-7 w-7 items-center justify-center rounded-md text-ink-secondary transition-colors hover:bg-elevated hover:text-ink-primary"
        >
          <Command className="h-3.5 w-3.5" />
        </button>
        <button
          type="button"
          onClick={toggleTheme}
          title={theme === "dark" ? "切换暖纸浅色" : "切换深色护眼"}
          aria-label="切换主题"
          className="flex h-7 w-7 items-center justify-center rounded-md text-ink-secondary transition-colors hover:bg-elevated hover:text-ink-primary"
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
          title="设置"
          aria-label="设置"
          className="flex h-7 w-7 items-center justify-center rounded-md text-ink-secondary transition-colors hover:bg-elevated hover:text-ink-primary"
        >
          <Settings className="h-3.5 w-3.5" />
        </button>
      </div>
    </header>
  );
}
