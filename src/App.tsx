import { useState } from "react";
import { BookOpen, Moon, Settings, Sun } from "lucide-react";

type Theme = "dark" | "light";

export function App() {
  const [theme, setTheme] = useState<Theme>("dark");

  const toggleTheme = () => {
    const next: Theme = theme === "dark" ? "light" : "dark";
    setTheme(next);
    document.documentElement.setAttribute("data-theme", next);
  };

  return (
    <div className="flex h-full flex-col bg-canvas text-ink-primary">
      <header
        className="flex shrink-0 items-center justify-between border-b border-border-subtle bg-surface px-4 select-none"
        style={{ height: "var(--topbar-height)" }}
      >
        <div className="flex items-center gap-2 text-sm font-medium tracking-wide">
          <BookOpen className="h-4 w-4 text-accent" />
          <span>Nib</span>
        </div>

        <nav className="flex items-center gap-1 text-xs">
          <span className="rounded px-2.5 py-1 text-ink-secondary">编排</span>
          <span className="rounded bg-accent-soft px-2.5 py-1 font-medium text-accent">
            审改
          </span>
          <span className="rounded px-2.5 py-1 text-ink-secondary">账本</span>
        </nav>

        <div className="flex items-center gap-1.5">
          <button
            type="button"
            onClick={toggleTheme}
            aria-label="切换主题"
            className="flex h-7 w-7 items-center justify-center rounded text-ink-secondary transition-colors hover:bg-elevated hover:text-ink-primary"
          >
            {theme === "dark" ? (
              <Sun className="h-3.5 w-3.5" />
            ) : (
              <Moon className="h-3.5 w-3.5" />
            )}
          </button>
          <button
            type="button"
            aria-label="设置"
            className="flex h-7 w-7 items-center justify-center rounded text-ink-secondary transition-colors hover:bg-elevated hover:text-ink-primary"
          >
            <Settings className="h-3.5 w-3.5" />
          </button>
        </div>
      </header>

      <main className="flex flex-1 items-center justify-center overflow-y-auto px-6 py-12">
        <article className="nib-prose-column text-center">
          <h1 className="mb-3 font-ui text-xl font-medium tracking-wide text-ink-primary">
            Nib
          </h1>
          <p className="text-sm text-ink-secondary">
            本地桌面 AI 长篇网文写作工作台 · Phase 1 骨架就绪
          </p>
        </article>
      </main>
    </div>
  );
}
