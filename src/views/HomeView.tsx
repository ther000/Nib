import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from "react";
import {
  ArrowRight,
  BarChart3,
  BookMarked,
  BookOpen,
  BookPlus,
  CheckCircle2,
  Download,
  FileText,
  PenLine,
  Plus,
  RefreshCw,
  SlidersHorizontal,
  Trash2,
  Upload,
} from "lucide-react";
import { LibraryService, type Book, type WritingStatsSummary } from "../mygo";
import { useApp } from "../stores/AppContext";
import { useContextMenu } from "../components/ContextMenu";
import { ConfirmDialog, type ConfirmDialogState } from "../components/ui/ConfirmDialog";
import { exportBookWithNotify } from "../lib/bookExport";
import { hasAnyEnabledModel, hasReadyModelRoute } from "../lib/modelReady";
import {
  formatErrorMessage,
  formatNumber,
  formatTimestamp,
} from "../lib/constants";

export function HomeView() {
  const {
    books,
    activeBook,
    providers,
    modelRoutes,
    genrePresets,
    openBook,
    deleteBook,
    openSettings,
    openCreateBook,
    notify,
  } = useApp();
  const { openContextMenu } = useContextMenu();

  const [confirmDialog, setConfirmDialog] = useState<ConfirmDialogState | null>(null);
  const [deleting, setDeleting] = useState(false);

  // Writing output stats state (only loaded when books exist)
  const [statsBookId, setStatsBookId] = useState<number>(0);
  const [writingStats, setWritingStats] = useState<WritingStatsSummary | null>(null);
  const [writingStatsLoading, setWritingStatsLoading] = useState(false);

  const hasEnabledProvider = providers.some((p) => p.enabled);
  const hasEnabledModel = hasAnyEnabledModel(providers);
  const hasDraftRoute = hasReadyModelRoute(providers, modelRoutes, ["draft"]);
  const modelReady = hasEnabledProvider && hasEnabledModel && hasDraftRoute;

  const modelStatusHint = useMemo(() => {
    if (!hasEnabledProvider) return "连接 DeepSeek、Claude、Gemini 或本地 Ollama";
    if (!hasEnabledModel) return "已添加模型服务，请启用至少一个模型";
    if (!hasDraftRoute) return "已启用模型，请指定默认写作模型";
    const draftRoute = modelRoutes.find((r) => r.stage === "draft");
    return draftRoute?.modelName ? `当前写作模型：${draftRoute.modelName}` : "模型已就绪";
  }, [hasEnabledProvider, hasEnabledModel, hasDraftRoute, modelRoutes]);

  const loadWritingStats = useCallback(
    async (bookId: number) => {
      setWritingStatsLoading(true);
      try {
        const res = await LibraryService.getWritingStats(bookId, 14);
        setWritingStats(res);
      } catch (err) {
        notify({
          tone: "danger",
          message: "加载码字统计失败",
          detail: formatErrorMessage(err),
        });
      } finally {
        setWritingStatsLoading(false);
      }
    },
    [notify]
  );

  useEffect(() => {
    if (statsBookId > 0 && !books.some((b) => b.id === statsBookId)) {
      setStatsBookId(0);
    }
  }, [books, statsBookId]);

  const getGenreName = (genreId: string) =>
    genrePresets.find((g) => g.id === genreId)?.name ?? genreId;

  const featuredBook = useMemo(() => {
    if (books.length === 0) return null;
    if (activeBook && books.some((b) => b.id === activeBook.id)) {
      return activeBook;
    }
    return books[0] ?? null;
  }, [books, activeBook]);

  const otherBooks = useMemo(() => {
    if (!featuredBook) return books;
    return books.filter((b) => b.id !== featuredBook.id);
  }, [books, featuredBook]);

  const statsBookInitializedRef = useRef(false);

  useEffect(() => {
    if (books.length === 0) {
      setWritingStats(null);
      return;
    }
    if (!statsBookInitializedRef.current && featuredBook) {
      statsBookInitializedRef.current = true;
      if (statsBookId === 0) {
        setStatsBookId(featuredBook.id);
        return;
      }
    }
    void loadWritingStats(statsBookId);
  }, [books.length, statsBookId, featuredBook, loadWritingStats]);

  const requestDeleteBook = useCallback(
    (book: Book) => {
      setConfirmDialog({
        title: `删除《${book.title}》`,
        message: "删除后将移入回收站（保留 30 天，可在设置中恢复）。确定删除吗？",
        confirmLabel: "移入回收站",
        danger: true,
        onConfirm: async () => {
          setDeleting(true);
          try {
            await deleteBook(book.id);
            setConfirmDialog(null);
          } finally {
            setDeleting(false);
          }
        },
      });
    },
    [deleteBook]
  );

  const openBookContextMenu = useCallback(
    (e: ReactMouseEvent, book: Book) => {
      openContextMenu(e, [
        {
          label: "继续写作",
          icon: <PenLine className="h-3.5 w-3.5" />,
          onSelect: () => void openBook(book.id, "studio"),
        },
        {
          label: "打开大纲",
          icon: <BookOpen className="h-3.5 w-3.5" />,
          onSelect: () => void openBook(book.id, "architect"),
        },
        {
          label: "打开设定",
          icon: <BookMarked className="h-3.5 w-3.5" />,
          onSelect: () => void openBook(book.id, "atlas"),
        },
        { type: "separator" },
        {
          label: "导出整本 TXT",
          icon: <FileText className="h-3.5 w-3.5" />,
          onSelect: () => void exportBookWithNotify(book.id, book.title, "txt", notify),
        },
        {
          label: "导出 EPUB 电子书",
          icon: <Download className="h-3.5 w-3.5" />,
          onSelect: () => void exportBookWithNotify(book.id, book.title, "epub", notify),
        },
        { type: "separator" },
        {
          label: "删除书籍…",
          icon: <Trash2 className="h-3.5 w-3.5" />,
          danger: true,
          onSelect: () => requestDeleteBook(book),
        },
      ]);
    },
    [openContextMenu, openBook, notify, requestDeleteBook]
  );

  // Clean 2-step first-run onboarding when shelf is empty
  if (books.length === 0) {
    return (
      <main className="nib-paper-sheet m-2 mt-0 flex flex-1 items-center justify-center overflow-y-auto px-6 py-12">
        <div className="w-full max-w-[520px]">
          <h1 className="text-2xl font-medium tracking-tight text-ink-primary">
            开始你的第一部长篇
          </h1>
          <p className="mt-2 text-[13px] leading-relaxed text-ink-secondary">
            先连好一个 AI 模型，再建书即可直接开始写第一章；也可以直接手动建书或导入旧稿。
          </p>

          <div className="mt-6 flex flex-col gap-3">
            {/* Step 1: Connect Model */}
            <div className="flex items-center justify-between gap-4 rounded-xl border border-border-subtle bg-surface px-5 py-4">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  {modelReady ? (
                    <CheckCircle2 className="h-4 w-4 shrink-0 text-ok" />
                  ) : (
                    <span className="nib-mono flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-accent-soft text-xs font-semibold text-accent">
                      1
                    </span>
                  )}
                  <span className="text-sm font-medium text-ink-primary">
                    {modelReady ? "AI 模型已就绪" : "连接写作模型"}
                  </span>
                </div>
                <p className="mt-1 pl-7 text-xs text-ink-secondary">{modelStatusHint}</p>
              </div>
              <button
                type="button"
                onClick={() => openSettings(hasEnabledModel && !hasDraftRoute ? "routes" : "providers")}
                className={`shrink-0 rounded-lg px-3.5 py-1.5 text-xs font-medium transition-colors ${
                  modelReady
                    ? "border border-border-subtle bg-canvas text-ink-secondary hover:border-border-strong hover:text-ink-primary"
                    : "bg-accent font-semibold text-canvas hover:opacity-90"
                }`}
              >
                {modelReady ? "调整" : "去连接"}
              </button>
            </div>

            {/* Step 2: Create First Book */}
            <div className="flex items-center justify-between gap-4 rounded-xl border border-border-subtle bg-surface px-5 py-4">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="nib-mono flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-elevated text-xs font-semibold text-ink-secondary">
                    2
                  </span>
                  <span className="text-sm font-medium text-ink-primary">创建第一本书</span>
                </div>
                <p className="mt-1 pl-7 text-xs text-ink-secondary">
                  {modelReady
                    ? "一句话生成书名、角色与前几章大纲，或手动开写"
                    : "不连模型也可以直接手动建书并开始码字"}
                </p>
              </div>
              <button
                type="button"
                onClick={() => openCreateBook(modelReady ? "ai" : "manual")}
                className={`flex shrink-0 items-center gap-1.5 rounded-lg px-3.5 py-1.5 text-xs font-semibold transition-opacity hover:opacity-90 ${
                  modelReady
                    ? "bg-accent text-canvas"
                    : "border border-border-strong bg-elevated text-ink-primary hover:border-accent/50"
                }`}
              >
                <BookPlus className="h-3.5 w-3.5" />
                <span>新建书籍</span>
              </button>
            </div>
          </div>

          <div className="mt-5 flex items-center justify-between text-xs text-ink-muted">
            <button
              type="button"
              onClick={() => openCreateBook("import")}
              className="flex items-center gap-1.5 text-ink-secondary transition-colors hover:text-ink-primary"
            >
              <Upload className="h-3.5 w-3.5" />
              <span>已有存稿？导入本地 TXT / Markdown</span>
            </button>
          </div>
        </div>
      </main>
    );
  }

  return (
    <main className="nib-paper-sheet m-2 mt-0 flex-1 overflow-y-auto px-9 py-9">
      <div className="mx-auto flex w-full max-w-[920px] flex-col gap-7">
        {/* Header Row */}
        <header className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-2.5">
            <h1 className="text-xl font-medium tracking-tight text-ink-primary">
              书架
            </h1>
            <span className="nib-mono rounded-full border border-border-subtle bg-surface px-2 py-0.5 text-xs text-ink-secondary">
              {books.length}
            </span>
          </div>

          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => openSettings(hasEnabledModel && !hasDraftRoute ? "routes" : "providers")}
              title={modelStatusHint}
              className="flex items-center gap-2 rounded-lg border border-border-subtle bg-surface px-3 py-1.5 text-[13px] font-medium text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary"
            >
              <span
                className={`h-1.5 w-1.5 rounded-full ${
                  modelReady ? "bg-ok" : "bg-warn"
                }`}
              />
              <span>{modelReady ? "模型就绪" : "连接模型"}</span>
            </button>

            <button
              type="button"
              onClick={() => openCreateBook("import")}
              className="flex items-center gap-1.5 rounded-lg border border-border-subtle bg-surface px-3 py-1.5 text-[13px] font-medium text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary"
            >
              <Upload className="h-3.5 w-3.5" />
              <span>导入旧稿</span>
            </button>

            <button
              type="button"
              onClick={() => openCreateBook("ai")}
              className="flex items-center gap-1.5 rounded-lg bg-accent px-3.5 py-1.5 text-[13px] font-semibold text-canvas transition-opacity hover:opacity-90"
            >
              <Plus className="h-3.5 w-3.5" />
              <span>新建书籍</span>
            </button>
          </div>
        </header>

        {/* Continue Writing Hero */}
        {featuredBook && (
          <section
            onContextMenu={(e) => openBookContextMenu(e, featuredBook)}
            className="rounded-2xl border border-border-subtle bg-elevated p-6 shadow-[var(--shadow-sheet)] transition-colors hover:border-accent/40"
          >
            <div className="flex flex-col justify-between gap-5 sm:flex-row sm:items-center">
              <div className="min-w-0 flex-1">
                <div className="mb-2 flex flex-wrap items-center gap-2.5 text-xs">
                  <span className="rounded-md bg-accent-soft px-2 py-0.5 font-medium text-accent">
                    最近写作
                  </span>
                  <span className="text-ink-secondary">
                    {getGenreName(featuredBook.genre)}
                  </span>
                  <span className="nib-mono text-ink-muted">
                    {formatNumber(featuredBook.chapterCount)} 章 ·{" "}
                    {featuredBook.totalWords >= 10000
                      ? `${(featuredBook.totalWords / 10000).toFixed(1)} 万字`
                      : `${formatNumber(featuredBook.totalWords)} 字`}
                  </span>
                  {featuredBook.updatedAt && (
                    <span className="nib-mono text-ink-muted">
                      {formatTimestamp(featuredBook.updatedAt)}
                    </span>
                  )}
                </div>

                <h2 className="truncate font-prose text-2xl font-medium text-ink-primary">
                  《{featuredBook.title}》
                </h2>

                {(featuredBook.premise || featuredBook.goldenFinger) && (
                  <p className="mt-2 line-clamp-2 max-w-2xl text-[13px] leading-relaxed text-ink-secondary">
                    {featuredBook.premise || featuredBook.goldenFinger}
                  </p>
                )}
              </div>

              <div className="flex shrink-0 items-center gap-2">
                <button
                  type="button"
                  onClick={() => void openBook(featuredBook.id, "architect")}
                  className="flex items-center gap-1.5 rounded-lg border border-border-strong bg-elevated px-3.5 py-2 text-[13px] font-medium text-ink-primary transition-colors hover:border-accent/50"
                >
                  <BookOpen className="h-3.5 w-3.5 text-ink-secondary" />
                  <span>大纲</span>
                </button>
                <button
                  type="button"
                  onClick={() => void openBook(featuredBook.id, "studio")}
                  className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-[13px] font-semibold text-canvas transition-opacity hover:opacity-90"
                >
                  <PenLine className="h-3.5 w-3.5" />
                  <span>继续写作</span>
                  <ArrowRight className="h-3.5 w-3.5" />
                </button>
              </div>
            </div>
          </section>
        )}

        {/* Other Books Grid (deduplicated from featuredBook) */}
        {otherBooks.length > 0 && (
          <section>
            <h2 className="mb-3 text-[13px] font-medium text-ink-secondary">其他作品</h2>

            <div className="grid grid-cols-1 gap-3.5 sm:grid-cols-2 lg:grid-cols-3">
              {otherBooks.map((book) => (
                <div
                  key={book.id}
                  onContextMenu={(e) => openBookContextMenu(e, book)}
                  className="group relative flex flex-col justify-between rounded-2xl border border-border-subtle bg-surface/65 p-4 transition-colors hover:border-border-strong hover:bg-elevated"
                >
                  <button
                    type="button"
                    onClick={() => void openBook(book.id, "studio")}
                    className="flex flex-1 flex-col text-left"
                  >
                    <div className="mb-2 pr-6">
                      <h3 className="line-clamp-1 font-prose text-base font-medium text-ink-primary transition-colors group-hover:text-accent">
                        《{book.title}》
                      </h3>
                    </div>

                    <div className="mb-2.5 flex flex-wrap items-center gap-x-2.5 gap-y-1 text-xs">
                      <span className="text-ink-secondary">
                        {getGenreName(book.genre)}
                      </span>
                      <span className="nib-mono text-ink-muted">
                        {formatNumber(book.chapterCount)} 章 ·{" "}
                        {book.totalWords >= 10000
                          ? `${(book.totalWords / 10000).toFixed(1)} 万字`
                          : `${formatNumber(book.totalWords)} 字`}
                      </span>
                    </div>

                    <p className="line-clamp-2 min-h-[38px] text-[13px] leading-relaxed text-ink-secondary">
                      {book.premise || book.goldenFinger || "暂无简介"}
                    </p>
                  </button>

                  <div className="mt-4 flex items-center justify-between border-t border-border-subtle pt-2.5 text-xs text-ink-muted">
                    <span className="nib-mono">
                      {book.updatedAt ? formatTimestamp(book.updatedAt) : ""}
                    </span>
                    <div className="flex items-center gap-3">
                      <button
                        type="button"
                        onClick={() => void openBook(book.id, "architect")}
                        className="text-ink-muted transition-colors hover:text-ink-primary"
                      >
                        大纲
                      </button>
                      <button
                        type="button"
                        onClick={() => void openBook(book.id, "studio")}
                        className="flex items-center gap-1 text-ink-secondary transition-colors group-hover:text-accent"
                      >
                        <span>写作</span>
                        <ArrowRight className="h-3 w-3 transition-transform group-hover:translate-x-0.5" />
                      </button>
                    </div>
                  </div>

                  <button
                    type="button"
                    title="删除书籍"
                    aria-label={`删除《${book.title}》`}
                    onClick={(e) => {
                      e.stopPropagation();
                      requestDeleteBook(book);
                    }}
                    className="absolute top-3.5 right-3 rounded-lg p-1 text-ink-muted opacity-0 transition-all hover:bg-danger-soft hover:text-danger group-hover:opacity-100 focus:opacity-100"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </div>
              ))}
            </div>
          </section>
        )}

        {/* Writing Output Section */}
        <section className="flex flex-col gap-3.5 border-t border-border-subtle pt-6">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <PenLine className="h-4 w-4 text-accent" />
              <h2 className="text-[13px] font-medium text-ink-secondary">
                码字进度
              </h2>
            </div>

            <div className="flex items-center gap-2">
              {books.length > 1 && (
                <select
                  aria-label="码字统计范围"
                  value={statsBookId}
                  onChange={(e) => setStatsBookId(Number(e.target.value) || 0)}
                  className="rounded-lg border border-border-subtle bg-surface px-2.5 py-1 text-xs text-ink-primary focus:border-accent"
                >
                  <option value={0}>全部作品</option>
                  {books.map((b) => (
                    <option key={b.id} value={b.id}>
                      《{b.title}》
                    </option>
                  ))}
                </select>
              )}

              <button
                type="button"
                disabled={writingStatsLoading}
                onClick={() => void loadWritingStats(statsBookId)}
                className="flex items-center gap-1 rounded-lg border border-border-subtle bg-surface px-2.5 py-1 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary disabled:opacity-50"
              >
                <RefreshCw
                  className={`h-3 w-3 ${
                    writingStatsLoading ? "animate-spin text-accent" : ""
                  }`}
                />
                <span>刷新</span>
              </button>

              <button
                type="button"
                onClick={() => openSettings("usage")}
                className="flex items-center gap-1 rounded-lg border border-border-subtle bg-surface px-2.5 py-1 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary"
              >
                <BarChart3 className="h-3 w-3" />
                <span>AI 用量</span>
              </button>
            </div>
          </div>

          <div className="rounded-xl border border-border-subtle bg-surface p-4">
            <div className="flex flex-wrap items-end justify-between gap-3">
              <div>
                <div className="text-xs text-ink-muted">今日码字</div>
                <div className="nib-mono mt-1 text-2xl font-medium text-ink-primary">
                  {formatNumber(writingStats?.todayWords ?? 0)}
                  <span className="ml-1 font-ui text-xs font-normal text-ink-muted">
                    字
                  </span>
                </div>
                <div className="mt-1 text-xs text-ink-secondary">
                  {writingStats
                    ? `日更目标 ${formatNumber(writingStats.dailyGoal)} 字 · 连续更新 ${writingStats.streakDays} 天`
                    : "暂无统计"}
                </div>
              </div>

              <div className="flex items-end gap-6 text-right">
                <div>
                  <div className="text-xs text-ink-muted">总字数</div>
                  <div className="nib-mono mt-1 text-sm font-medium text-ink-primary">
                    {writingStats && writingStats.totalWords >= 10000
                      ? `${(writingStats.totalWords / 10000).toFixed(1)} 万`
                      : formatNumber(writingStats?.totalWords ?? 0)}
                  </div>
                </div>
                <div>
                  <div className="text-xs text-ink-muted">已定稿</div>
                  <div className="nib-mono mt-1 text-sm font-medium text-ink-primary">
                    {formatNumber(writingStats?.finalizedChapters ?? 0)} 章
                  </div>
                </div>
              </div>
            </div>

            <div className="mt-4 h-1.5 overflow-hidden rounded-full bg-canvas">
              <div
                className="h-full rounded-full bg-accent transition-[width]"
                style={{
                  width: `${Math.min(
                    100,
                    ((writingStats?.todayWords ?? 0) / Math.max(1, writingStats?.dailyGoal ?? 1)) * 100
                  )}%`,
                }}
              />
            </div>

            {/* Fixed-height track + separate date row prevents vertical overflow */}
            <div className="mt-5 flex items-end gap-1">
              {(writingStats?.daily ?? []).map((d) => {
                const max = Math.max(
                  1,
                  ...(writingStats?.daily ?? []).map((item) => item.wordsDelta)
                );
                const height = Math.max(4, Math.round((d.wordsDelta / max) * 56));
                return (
                  <div
                    key={d.date}
                    title={`${d.date}：${formatNumber(d.wordsDelta)} 字`}
                    className="group flex flex-1 flex-col items-center gap-1.5"
                  >
                    <div className="flex h-14 w-full items-end">
                      <div
                        className={`w-full rounded-sm transition-colors ${
                          d.wordsDelta > 0
                            ? "bg-accent/70 group-hover:bg-accent"
                            : "bg-border-subtle"
                        }`}
                        style={{ height }}
                      />
                    </div>
                    <span className="nib-mono text-[11px] leading-none text-ink-muted">
                      {d.date.slice(8)}
                    </span>
                  </div>
                );
              })}
            </div>
            <div className="mt-2 flex justify-between text-xs text-ink-muted">
              <span>近 14 天码字趋势</span>
              <button
                type="button"
                onClick={() => {
                  if (featuredBook) {
                    void openBook(featuredBook.id, "book_settings");
                  } else {
                    openSettings("workspace");
                  }
                }}
                className="flex items-center gap-1 text-ink-secondary transition-colors hover:text-ink-primary"
              >
                <SlidersHorizontal className="h-3 w-3" />
                <span>调整日更目标</span>
              </button>
            </div>
          </div>
        </section>
      </div>

      <ConfirmDialog
        dialog={confirmDialog}
        busy={deleting}
        onClose={() => setConfirmDialog(null)}
      />
    </main>
  );
}
