import { useEffect, useRef, useState } from "react";
import {
  BookOpen,
  Check,
  ChevronDown,
  Feather,
  Moon,
  Plus,
  Search,
  Settings,
  Sun,
} from "lucide-react";
import { useApp } from "../stores/AppContext";
import type { WorkspaceTab } from "../lib/constants";

const WORKSPACE_TABS: Array<{ id: WorkspaceTab; label: string; shortcut: string }> = [
  { id: "architect", label: "编排", shortcut: "Ctrl+1" },
  { id: "studio", label: "审改", shortcut: "Ctrl+2" },
  { id: "atlas", label: "账本", shortcut: "Ctrl+3" },
];

export function TopBar() {
  const {
    theme,
    toggleTheme,
    activeTab,
    setActiveTab,
    books,
    activeBook,
    genrePresets,
    selectBook,
    openSettings,
    openCreateBook,
    openMemorySearch,
  } = useApp();

  const [menuOpen, setMenuOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

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
      className="relative z-30 flex shrink-0 items-center justify-between border-b border-border-subtle bg-surface px-3.5 select-none"
      style={{ height: "var(--topbar-height)" }}
    >
      {/* Left: Book switcher */}
      <div ref={menuRef} className="relative flex min-w-0 items-center">
        {activeBook ? (
          <>
            <button
              type="button"
              onClick={() => setMenuOpen((v) => !v)}
              className="flex h-7 max-w-[260px] items-center gap-2 rounded-md px-2 text-[13px] font-medium text-ink-primary transition-colors hover:bg-elevated"
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
              <div className="absolute top-[calc(100%+6px)] left-0 z-50 w-64 rounded-lg border border-border-strong bg-elevated py-1.5 shadow-[var(--shadow-elevated)]">
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
                            void selectBook(book.id);
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
                          <div className="mt-0.5 flex items-center gap-2 text-[11px] text-ink-secondary">
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

                <div className="px-1">
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
                </div>
              </div>
            )}
          </>
        ) : (
          <div className="flex items-center gap-2 px-1 text-[13px] font-medium tracking-wide text-ink-primary">
            <BookOpen className="h-3.5 w-3.5 text-accent" />
            <span>Nib</span>
          </div>
        )}
      </div>

      {/* Center: 3-segment workspace switcher */}
      {activeBook ? (
        <nav
          aria-label="工作区切换"
          className="flex items-center gap-0.5 rounded-md bg-canvas/70 p-0.5 border border-border-subtle"
        >
          {WORKSPACE_TABS.map((tab) => {
            const active = activeTab === tab.id;
            return (
              <button
                key={tab.id}
                type="button"
                title={`${tab.label} (${tab.shortcut})`}
                onClick={() => setActiveTab(tab.id)}
                className={`rounded px-3.5 py-1 text-xs font-medium transition-colors ${
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
        <div />
      )}

      {/* Right: Memory Search + Theme + Settings */}
      <div className="flex items-center gap-1">
        {activeBook && (
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
