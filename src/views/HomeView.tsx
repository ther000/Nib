import { useCallback, useEffect, useMemo, useState } from "react";
import {
  ArrowRight,
  BarChart3,
  BookOpen,
  BookPlus,
  Check,
  Feather,
  PenLine,
  Plus,
  RefreshCw,
  SlidersHorizontal,
  Trash2,
} from "lucide-react";
import { SettingsService, type UsageSummary } from "../mygo";
import { useApp } from "../stores/AppContext";
import {
  PIPELINE_STAGES,
  formatErrorMessage,
  formatNumber,
  formatTimestamp,
} from "../lib/constants";

export function HomeView() {
  const {
    books,
    activeBook,
    providers,
    genrePresets,
    openBook,
    deleteBook,
    openSettings,
    openCreateBook,
    notify,
  } = useApp();

  const [confirmDeleteId, setConfirmDeleteId] = useState<number | null>(null);
  const [deletingId, setDeletingId] = useState<number | null>(null);

  // Token & usage analytics state
  const [usageBookId, setUsageBookId] = useState<number>(0);
  const [usageSummary, setUsageSummary] = useState<UsageSummary | null>(null);
  const [usageLoading, setUsageLoading] = useState(false);
  const [usageDetailTab, setUsageDetailTab] = useState<"stage" | "model">("stage");

  const hasEnabledProvider = providers.some((p) => p.enabled);

  const loadUsage = useCallback(
    async (bookId: number) => {
      setUsageLoading(true);
      try {
        const res = await SettingsService.getUsageSummary(bookId);
        setUsageSummary(res);
      } catch (err) {
        notify({
          tone: "danger",
          message: "加载 Token 统计失败",
          detail: formatErrorMessage(err),
        });
      } finally {
        setUsageLoading(false);
      }
    },
    [notify]
  );

  // Reset selected book filter if that book was deleted
  useEffect(() => {
    if (usageBookId > 0 && !books.some((b) => b.id === usageBookId)) {
      setUsageBookId(0);
    }
  }, [books, usageBookId]);

  useEffect(() => {
    void loadUsage(usageBookId);
  }, [usageBookId, loadUsage]);

  const getGenreName = (genreId: string) =>
    genrePresets.find((g) => g.id === genreId)?.name ?? genreId;

  const featuredBook = useMemo(() => {
    if (books.length === 0) return null;
    if (activeBook && books.some((b) => b.id === activeBook.id)) {
      return activeBook;
    }
    return books[0] ?? null;
  }, [books, activeBook]);

  const handleConfirmDelete = async (bookId: number) => {
    setDeletingId(bookId);
    try {
      await deleteBook(bookId);
      setConfirmDeleteId(null);
    } finally {
      setDeletingId(null);
    }
  };

  // Empty shelf state
  if (books.length === 0) {
    return (
      <main className="flex flex-1 items-center justify-center overflow-y-auto px-6 py-12">
        <div className="w-full max-w-[440px]">
          <div className="mb-10 flex flex-col items-center text-center">
            <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl border border-border-strong bg-surface shadow-sm">
              <Feather className="h-5 w-5 text-accent" />
            </div>
            <h1 className="text-xl font-medium tracking-wide text-ink-primary">Nib</h1>
            <p className="mt-1.5 text-xs text-ink-secondary">
              长篇男频网文创作工作台
            </p>
          </div>

          <div className="flex flex-col gap-3">
            <button
              type="button"
              onClick={() => openSettings("providers")}
              className="group flex w-full items-center justify-between rounded-xl border border-border-subtle bg-surface px-4 py-3.5 text-left transition-colors hover:border-border-strong hover:bg-elevated"
            >
              <div className="flex items-center gap-3">
                <div
                  className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-xs font-medium nib-mono ${
                    hasEnabledProvider
                      ? "bg-ok-soft text-ok"
                      : "bg-elevated text-ink-secondary"
                  }`}
                >
                  {hasEnabledProvider ? <Check className="h-3.5 w-3.5" /> : "01"}
                </div>
                <div>
                  <div className="text-[14px] font-medium text-ink-primary">
                    配置 API 供应商
                  </div>
                  <div className="mt-0.5 text-xs text-ink-secondary">
                    {hasEnabledProvider
                      ? `已配置 ${providers.length} 个供应商`
                      : "OpenAI 兼容 · Anthropic · Gemini"}
                  </div>
                </div>
              </div>
              <SlidersHorizontal className="h-4 w-4 text-ink-muted transition-colors group-hover:text-ink-primary" />
            </button>

            <button
              type="button"
              onClick={openCreateBook}
              className="group flex w-full items-center justify-between rounded-xl border border-accent/35 bg-accent-soft px-4 py-3.5 text-left transition-colors hover:border-accent/60"
            >
              <div className="flex items-center gap-3">
                <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-accent text-xs font-semibold text-canvas nib-mono">
                  02
                </div>
                <div>
                  <div className="text-[14px] font-medium text-ink-primary">
                    创建第一本书
                  </div>
                  <div className="mt-0.5 text-xs text-ink-secondary">
                    玄幻 · 仙侠 · 都市 · 通用男频
                  </div>
                </div>
              </div>
              <div className="flex items-center gap-1 text-accent">
                <BookPlus className="h-4 w-4" />
                <ArrowRight className="h-3.5 w-3.5 transition-transform group-hover:translate-x-0.5" />
              </div>
            </button>
          </div>

          {usageSummary && usageSummary.totalCalls > 0 && (
            <div className="mt-6 flex items-center justify-between rounded-xl border border-border-subtle bg-surface/60 px-4 py-3 text-xs">
              <div className="flex items-center gap-2 text-ink-secondary">
                <BarChart3 className="h-3.5 w-3.5 text-accent" />
                <span>累计调用 {formatNumber(usageSummary.totalCalls)} 次</span>
              </div>
              <span className="nib-mono font-medium text-accent">
                {formatNumber(usageSummary.totalTokens)} Tokens
              </span>
            </div>
          )}
        </div>
      </main>
    );
  }

  return (
    <main className="flex-1 overflow-y-auto px-8 py-10">
      <div className="mx-auto flex w-full max-w-[940px] flex-col gap-8">
        {/* Header Row */}
        <header className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <div className="flex items-center gap-2.5">
              <h1 className="text-xl font-medium tracking-tight text-ink-primary">
                书架
              </h1>
              <span className="nib-mono rounded-full border border-border-subtle bg-surface px-2 py-0.5 text-[11px] text-ink-secondary">
                {books.length}
              </span>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => openSettings("providers")}
              className="flex items-center gap-1.5 rounded-lg border border-border-subtle bg-surface px-3 py-1.5 text-xs font-medium text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary"
            >
              <span
                className={`h-1.5 w-1.5 rounded-full ${
                  hasEnabledProvider ? "bg-ok" : "bg-warn"
                }`}
              />
              <span>
                {hasEnabledProvider ? `API 供应商 (${providers.length})` : "配置 API"}
              </span>
            </button>

            <button
              type="button"
              onClick={openCreateBook}
              className="flex items-center gap-1.5 rounded-lg bg-accent px-3.5 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90"
            >
              <Plus className="h-3.5 w-3.5" />
              <span>新建书籍</span>
            </button>
          </div>
        </header>

        {/* Continue Writing Hero Card */}
        {featuredBook && (
          <section className="rounded-2xl border border-accent/25 bg-surface p-6 transition-colors hover:border-accent/45">
            <div className="flex flex-col justify-between gap-5 sm:flex-row sm:items-center">
              <div className="min-w-0 flex-1">
                <div className="mb-2 flex items-center gap-2">
                  <span className="rounded bg-accent-soft px-2 py-0.5 text-[11px] font-medium text-accent">
                    继续创作
                  </span>
                  <span className="rounded border border-border-subtle bg-elevated px-2 py-0.5 text-[11px] text-ink-secondary">
                    {getGenreName(featuredBook.genre)}
                  </span>
                  <span className="nib-mono text-[11px] text-ink-muted">
                    {formatNumber(featuredBook.targetChapterWords)} 字/章
                  </span>
                  {featuredBook.updatedAt && (
                    <span className="nib-mono text-[11px] text-ink-muted">
                      · {formatTimestamp(featuredBook.updatedAt)}
                    </span>
                  )}
                </div>

                <h2 className="truncate text-2xl font-medium tracking-tight text-ink-primary">
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
                  className="flex items-center gap-1.5 rounded-lg border border-border-strong bg-elevated px-3.5 py-2 text-xs font-medium text-ink-primary transition-colors hover:border-accent/50"
                >
                  <BookOpen className="h-3.5 w-3.5 text-ink-secondary" />
                  <span>编排</span>
                </button>
                <button
                  type="button"
                  onClick={() => void openBook(featuredBook.id, "studio")}
                  className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-xs font-semibold text-canvas transition-opacity hover:opacity-90"
                >
                  <PenLine className="h-3.5 w-3.5" />
                  <span>进入审改台</span>
                  <ArrowRight className="h-3.5 w-3.5" />
                </button>
              </div>
            </div>
          </section>
        )}

        {/* All Books Grid */}
        <section>
          <div className="mb-3 flex items-center justify-between">
            <h2 className="text-xs font-medium text-ink-secondary">全部作品</h2>
          </div>

          <div className="grid grid-cols-1 gap-3.5 sm:grid-cols-2 lg:grid-cols-3">
            {books.map((book) => {
              const isConfirming = confirmDeleteId === book.id;
              const isDeleting = deletingId === book.id;
              const isFeatured = featuredBook?.id === book.id;

              return (
                <div
                  key={book.id}
                  className={`group relative flex flex-col justify-between rounded-xl border bg-surface p-4 transition-colors ${
                    isFeatured
                      ? "border-border-strong"
                      : "border-border-subtle hover:border-border-strong"
                  }`}
                >
                  <button
                    type="button"
                    onClick={() => void openBook(book.id, "architect")}
                    className="flex flex-1 flex-col text-left"
                  >
                    <div className="mb-2 flex items-start justify-between gap-2 pr-6">
                      <h3 className="line-clamp-1 text-[15px] font-medium text-ink-primary transition-colors group-hover:text-accent">
                        《{book.title}》
                      </h3>
                    </div>

                    <div className="mb-2.5 flex items-center gap-1.5">
                      <span className="rounded border border-border-subtle bg-canvas px-1.5 py-0.5 text-[10.5px] text-ink-secondary">
                        {getGenreName(book.genre)}
                      </span>
                      <span className="nib-mono text-[10.5px] text-ink-muted">
                        {formatNumber(book.targetChapterWords)} 字/章
                      </span>
                    </div>

                    <p className="line-clamp-2 min-h-[34px] text-xs leading-relaxed text-ink-secondary">
                      {book.premise || book.goldenFinger || "暂无简介"}
                    </p>
                  </button>

                  <div className="mt-4 flex items-center justify-between border-t border-border-subtle pt-2.5 text-[11px] text-ink-muted">
                    <span className="nib-mono">
                      {book.updatedAt ? formatTimestamp(book.updatedAt) : ""}
                    </span>

                    {isConfirming ? (
                      <div className="flex items-center gap-1.5">
                        <button
                          type="button"
                          disabled={isDeleting}
                          onClick={() => void handleConfirmDelete(book.id)}
                          className="rounded bg-danger px-2 py-0.5 text-[11px] font-medium text-white disabled:opacity-50"
                        >
                          {isDeleting ? "删除中…" : "确认删除"}
                        </button>
                        <button
                          type="button"
                          onClick={() => setConfirmDeleteId(null)}
                          className="text-ink-secondary hover:text-ink-primary"
                        >
                          取消
                        </button>
                      </div>
                    ) : (
                      <button
                        type="button"
                        onClick={() => void openBook(book.id, "architect")}
                        className="flex items-center gap-1 text-ink-secondary transition-colors group-hover:text-accent"
                      >
                        <span>打开</span>
                        <ArrowRight className="h-3 w-3 transition-transform group-hover:translate-x-0.5" />
                      </button>
                    )}
                  </div>

                  {!isConfirming && (
                    <button
                      type="button"
                      title="删除书籍"
                      aria-label={`删除《${book.title}》`}
                      onClick={(e) => {
                        e.stopPropagation();
                        setConfirmDeleteId(book.id);
                      }}
                      className="absolute top-3.5 right-3 rounded-md p-1 text-ink-muted opacity-0 transition-all hover:bg-danger-soft hover:text-danger group-hover:opacity-100 focus:opacity-100"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  )}
                </div>
              );
            })}
          </div>
        </section>

        {/* Token & Cost Analytics Section */}
        <section className="flex flex-col gap-3.5 border-t border-border-subtle pt-6">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <BarChart3 className="h-4 w-4 text-accent" />
              <h2 className="text-xs font-medium text-ink-secondary">
                Token 与成本统计
              </h2>
            </div>

            <div className="flex items-center gap-2">
              <select
                aria-label="统计范围"
                value={usageBookId}
                onChange={(e) => setUsageBookId(Number(e.target.value) || 0)}
                className="rounded-md border border-border-subtle bg-surface px-2.5 py-1 text-xs text-ink-primary focus:border-accent focus:outline-none"
              >
                <option value={0}>全部作品汇总</option>
                {books.map((b) => (
                  <option key={b.id} value={b.id}>
                    《{b.title}》
                  </option>
                ))}
              </select>

              <button
                type="button"
                disabled={usageLoading}
                onClick={() => void loadUsage(usageBookId)}
                className="flex items-center gap-1 rounded-md border border-border-subtle bg-surface px-2.5 py-1 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary disabled:opacity-50"
              >
                <RefreshCw
                  className={`h-3 w-3 ${
                    usageLoading ? "animate-spin text-accent" : ""
                  }`}
                />
                <span>刷新</span>
              </button>
            </div>
          </div>

          {/* KPI Strip */}
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <div className="rounded-xl border border-border-subtle bg-surface p-3.5">
              <div className="text-[11px] text-ink-muted">总调用次数</div>
              <div className="nib-mono mt-1 text-lg font-semibold text-ink-primary">
                {formatNumber(usageSummary?.totalCalls ?? 0)}
              </div>
            </div>
            <div className="rounded-xl border border-border-subtle bg-surface p-3.5">
              <div className="text-[11px] text-ink-muted">输入 Prompt</div>
              <div className="nib-mono mt-1 text-lg font-semibold text-ink-primary">
                {formatNumber(usageSummary?.totalPromptTokens ?? 0)}
              </div>
            </div>
            <div className="rounded-xl border border-border-subtle bg-surface p-3.5">
              <div className="text-[11px] text-ink-muted">输出 Completion</div>
              <div className="nib-mono mt-1 text-lg font-semibold text-ink-primary">
                {formatNumber(usageSummary?.totalCompletionTokens ?? 0)}
              </div>
            </div>
            <div className="rounded-xl border border-accent/30 bg-accent-soft/45 p-3.5">
              <div className="text-[11px] text-accent">总 Tokens</div>
              <div className="nib-mono mt-1 text-lg font-semibold text-accent">
                {formatNumber(usageSummary?.totalTokens ?? 0)}
              </div>
            </div>
          </div>

          {/* Breakdown Table (shown when there is usage data) */}
          {(usageSummary?.totalCalls ?? 0) > 0 && (
            <div className="overflow-hidden rounded-xl border border-border-subtle bg-surface">
              <div className="flex items-center justify-between border-b border-border-subtle px-3.5 py-2">
                <div className="flex items-center gap-1 rounded-md border border-border-subtle bg-canvas p-0.5 text-[11px]">
                  <button
                    type="button"
                    onClick={() => setUsageDetailTab("stage")}
                    className={`rounded px-2.5 py-1 transition-colors ${
                      usageDetailTab === "stage"
                        ? "bg-accent-soft font-medium text-accent"
                        : "text-ink-secondary hover:text-ink-primary"
                    }`}
                  >
                    按创作环节
                  </button>
                  <button
                    type="button"
                    onClick={() => setUsageDetailTab("model")}
                    className={`rounded px-2.5 py-1 transition-colors ${
                      usageDetailTab === "model"
                        ? "bg-accent-soft font-medium text-accent"
                        : "text-ink-secondary hover:text-ink-primary"
                    }`}
                  >
                    按供应商 / 模型
                  </button>
                </div>
              </div>

              {usageDetailTab === "stage" ? (
                <table className="w-full text-left text-xs">
                  <thead className="border-b border-border-subtle bg-canvas/50 text-[11px] text-ink-muted">
                    <tr>
                      <th className="px-3.5 py-2 font-medium">环节</th>
                      <th className="px-3.5 py-2 text-right font-medium">调用</th>
                      <th className="px-3.5 py-2 text-right font-medium">Prompt</th>
                      <th className="px-3.5 py-2 text-right font-medium">Completion</th>
                      <th className="px-3.5 py-2 text-right font-medium">总计</th>
                      <th className="px-3.5 py-2 text-right font-medium">平均延迟</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border-subtle">
                    {(usageSummary?.byStage ?? []).map((row) => {
                      const label =
                        PIPELINE_STAGES.find((s) => s.stage === row.stage)?.label ??
                        (row.stage === "style" ? "文风提取" : row.stage);
                      return (
                        <tr key={row.stage}>
                          <td className="px-3.5 py-2 font-medium text-ink-primary">
                            <span>{label}</span>
                            <span className="nib-mono ml-1.5 text-[10px] text-ink-muted">
                              {row.stage}
                            </span>
                          </td>
                          <td className="nib-mono px-3.5 py-2 text-right text-ink-secondary">
                            {formatNumber(row.calls)}
                          </td>
                          <td className="nib-mono px-3.5 py-2 text-right text-ink-secondary">
                            {formatNumber(row.promptTokens)}
                          </td>
                          <td className="nib-mono px-3.5 py-2 text-right text-ink-secondary">
                            {formatNumber(row.completionTokens)}
                          </td>
                          <td className="nib-mono px-3.5 py-2 text-right font-medium text-ink-primary">
                            {formatNumber(row.totalTokens)}
                          </td>
                          <td className="nib-mono px-3.5 py-2 text-right text-ink-muted">
                            {row.avgLatencyMs > 0
                              ? `${formatNumber(row.avgLatencyMs)}ms`
                              : "—"}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              ) : (
                <table className="w-full text-left text-xs">
                  <thead className="border-b border-border-subtle bg-canvas/50 text-[11px] text-ink-muted">
                    <tr>
                      <th className="px-3.5 py-2 font-medium">供应商</th>
                      <th className="px-3.5 py-2 font-medium">模型</th>
                      <th className="px-3.5 py-2 text-right font-medium">调用</th>
                      <th className="px-3.5 py-2 text-right font-medium">Prompt</th>
                      <th className="px-3.5 py-2 text-right font-medium">Completion</th>
                      <th className="px-3.5 py-2 text-right font-medium">总计</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border-subtle">
                    {(usageSummary?.byModel ?? []).map((row, idx) => (
                      <tr key={`${row.providerName}-${row.modelName}-${idx}`}>
                        <td className="px-3.5 py-2 text-ink-primary">
                          {row.providerName || "—"}
                        </td>
                        <td className="nib-mono px-3.5 py-2 text-ink-secondary">
                          {row.modelName || "—"}
                        </td>
                        <td className="nib-mono px-3.5 py-2 text-right text-ink-secondary">
                          {formatNumber(row.calls)}
                        </td>
                        <td className="nib-mono px-3.5 py-2 text-right text-ink-secondary">
                          {formatNumber(row.promptTokens)}
                        </td>
                        <td className="nib-mono px-3.5 py-2 text-right text-ink-secondary">
                          {formatNumber(row.completionTokens)}
                        </td>
                        <td className="nib-mono px-3.5 py-2 text-right font-medium text-ink-primary">
                          {formatNumber(row.totalTokens)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          )}
        </section>
      </div>
    </main>
  );
}
