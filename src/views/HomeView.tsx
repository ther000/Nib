import { useCallback, useEffect, useMemo, useState } from "react";
import {
  ArrowRight,
  BarChart3,
  BookOpen,
  BookPlus,
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
  formatCompactTokens,
  formatErrorMessage,
  formatLatency,
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

  const [confirmDeleteId, setConfirmDeleteId] = useState<number | null>(null);
  const [deletingId, setDeletingId] = useState<number | null>(null);

  // Usage analytics state
  const [usageBookId, setUsageBookId] = useState<number>(0);
  const [usageSummary, setUsageSummary] = useState<UsageSummary | null>(null);
  const [usageLoading, setUsageLoading] = useState(false);
  const [usageDetailTab, setUsageDetailTab] = useState<"stage" | "model">("stage");

  const enabledProviders = useMemo(
    () => providers.filter((p) => p.enabled),
    [providers]
  );
  const hasEnabledProvider = enabledProviders.length > 0;
  const hasEnabledModel = enabledProviders.some((p) => (p.models?.length ?? 0) > 0);
  const hasDraftRoute = modelRoutes.some(
    (r) => r.stage === "draft" && Boolean(r.providerId) && Boolean(r.modelName?.trim())
  );
  const modelReady = hasEnabledProvider && hasEnabledModel && hasDraftRoute;

  const modelStatusHint = useMemo(() => {
    if (!hasEnabledProvider) return "连接 DeepSeek、Claude、Gemini 等模型";
    if (!hasEnabledModel) return "已添加模型服务，请勾选要启用的模型";
    if (!hasDraftRoute) return "已启用模型，请指定默认写作模型";
    const draftRoute = modelRoutes.find((r) => r.stage === "draft");
    return draftRoute?.modelName ? `默认写作模型：${draftRoute.modelName}` : "模型已就绪";
  }, [hasEnabledProvider, hasEnabledModel, hasDraftRoute, modelRoutes]);

  const loadUsage = useCallback(
    async (bookId: number) => {
      setUsageLoading(true);
      try {
        const res = await SettingsService.getUsageSummary(bookId);
        setUsageSummary(res);
      } catch (err) {
        notify({
          tone: "danger",
          message: "加载用量统计失败",
          detail: formatErrorMessage(err),
        });
      } finally {
        setUsageLoading(false);
      }
    },
    [notify]
  );

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

  return (
    <main className="flex-1 overflow-y-auto px-8 py-10">
      <div className="mx-auto flex w-full max-w-[940px] flex-col gap-8">
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
              className="flex items-center gap-2 rounded-lg border border-border-subtle bg-surface px-3 py-1.5 text-[13px] font-medium text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary"
            >
              <span
                className={`h-1.5 w-1.5 rounded-full ${
                  modelReady ? "bg-ok" : "bg-warn"
                }`}
              />
              <span>{modelReady ? "模型设置" : "连接模型"}</span>
            </button>

            <button
              type="button"
              onClick={openCreateBook}
              className="flex items-center gap-1.5 rounded-lg bg-accent px-3.5 py-1.5 text-[13px] font-semibold text-canvas transition-opacity hover:opacity-90"
            >
              <Plus className="h-3.5 w-3.5" />
              <span>新建书籍</span>
            </button>
          </div>
        </header>

        {/* Empty Shelf Placeholder */}
        {books.length === 0 && (
          <section className="flex flex-col items-start justify-between gap-5 rounded-xl border border-border-subtle bg-surface px-6 py-7 sm:flex-row sm:items-center">
            <div className="min-w-0">
              <h2 className="text-base font-medium text-ink-primary">
                还没有作品
              </h2>
              <p className="mt-1 text-[13px] text-ink-secondary">
                {modelReady
                  ? "写一句故事灵感，或手动创建第一本书。"
                  : modelStatusHint}
              </p>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              {!modelReady && (
                <button
                  type="button"
                  onClick={() =>
                    openSettings(hasEnabledModel && !hasDraftRoute ? "routes" : "providers")
                  }
                  className="flex items-center gap-1.5 rounded-lg border border-border-strong bg-elevated px-3.5 py-2 text-[13px] font-medium text-ink-primary transition-colors hover:border-accent/50"
                >
                  <SlidersHorizontal className="h-3.5 w-3.5 text-ink-secondary" />
                  <span>连接模型</span>
                </button>
              )}
              <button
                type="button"
                onClick={openCreateBook}
                className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-[13px] font-semibold text-canvas transition-opacity hover:opacity-90"
              >
                <BookPlus className="h-3.5 w-3.5" />
                <span>新建书籍</span>
              </button>
            </div>
          </section>
        )}

        {/* Continue Writing Hero */}
        {featuredBook && (
          <section className="rounded-xl border border-accent/25 bg-surface p-6 transition-colors hover:border-accent/45">
            <div className="flex flex-col justify-between gap-5 sm:flex-row sm:items-center">
              <div className="min-w-0 flex-1">
                <div className="mb-2.5 flex flex-wrap items-center gap-2.5 text-xs">
                  <span className="rounded-md bg-accent-soft px-2 py-0.5 font-medium text-accent">
                    最近写作
                  </span>
                  <span className="text-ink-secondary">
                    {getGenreName(featuredBook.genre)}
                  </span>
                  <span className="nib-mono text-ink-muted">
                    {formatNumber(featuredBook.targetChapterWords)} 字/章
                  </span>
                  {featuredBook.updatedAt && (
                    <span className="nib-mono text-ink-muted">
                      {formatTimestamp(featuredBook.updatedAt)}
                    </span>
                  )}
                </div>

                <h2 className="truncate text-xl font-medium tracking-tight text-ink-primary">
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

        {/* All Books Grid */}
        {books.length > 0 && (
          <section>
            <h2 className="mb-3 text-[13px] font-medium text-ink-secondary">全部作品</h2>

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
                      <div className="mb-2 pr-6">
                        <h3 className="line-clamp-1 text-base font-medium text-ink-primary transition-colors group-hover:text-accent">
                          《{book.title}》
                        </h3>
                      </div>

                      <div className="mb-2.5 flex items-center gap-2.5 text-xs">
                        <span className="text-ink-secondary">
                          {getGenreName(book.genre)}
                        </span>
                        <span className="nib-mono text-ink-muted">
                          {formatNumber(book.targetChapterWords)} 字/章
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

                      {isConfirming ? (
                        <div className="flex items-center gap-2">
                          <button
                            type="button"
                            disabled={isDeleting}
                            onClick={() => void handleConfirmDelete(book.id)}
                            className="rounded-md border border-danger/40 bg-danger-soft px-2 py-0.5 text-xs font-medium text-danger transition-colors hover:bg-danger/20 disabled:opacity-50"
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
                        className="absolute top-3.5 right-3 rounded-lg p-1 text-ink-muted opacity-0 transition-all hover:bg-danger-soft hover:text-danger group-hover:opacity-100 focus:opacity-100"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    )}
                  </div>
                );
              })}
            </div>
          </section>
        )}

        {/* Usage Analytics Section */}
        <section className="flex flex-col gap-4 border-t border-border-subtle pt-6">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <BarChart3 className="h-4 w-4 text-accent" />
              <h2 className="text-[13px] font-medium text-ink-secondary">
                用量统计
              </h2>
            </div>

            <div className="flex items-center gap-2">
              <select
                aria-label="统计范围"
                value={usageBookId}
                onChange={(e) => setUsageBookId(Number(e.target.value) || 0)}
                className="rounded-lg border border-border-subtle bg-surface px-2.5 py-1 text-xs text-ink-primary focus:border-accent focus:outline-none"
              >
                <option value={0}>全部作品</option>
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
                className="flex items-center gap-1 rounded-lg border border-border-subtle bg-surface px-2.5 py-1 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary disabled:opacity-50"
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
            <div className="rounded-xl border border-border-subtle bg-surface p-4">
              <div className="text-xs text-ink-muted">调用次数</div>
              <div className="nib-mono mt-1.5 text-xl font-medium text-ink-primary">
                {formatNumber(usageSummary?.totalCalls ?? 0)}
                <span className="ml-1 font-ui text-xs font-normal text-ink-muted">
                  次
                </span>
              </div>
            </div>
            <div
              title={`输入 ${formatNumber(usageSummary?.totalPromptTokens ?? 0)} Token`}
              className="rounded-xl border border-border-subtle bg-surface p-4"
            >
              <div className="text-xs text-ink-muted">输入 Token</div>
              <div className="nib-mono mt-1.5 text-xl font-medium text-ink-primary">
                {formatCompactTokens(usageSummary?.totalPromptTokens ?? 0)}
              </div>
            </div>
            <div
              title={`输出 ${formatNumber(usageSummary?.totalCompletionTokens ?? 0)} Token`}
              className="rounded-xl border border-border-subtle bg-surface p-4"
            >
              <div className="text-xs text-ink-muted">输出 Token</div>
              <div className="nib-mono mt-1.5 text-xl font-medium text-ink-primary">
                {formatCompactTokens(usageSummary?.totalCompletionTokens ?? 0)}
              </div>
            </div>
            <div
              title={`总计 ${formatNumber(usageSummary?.totalTokens ?? 0)} Token`}
              className="rounded-xl border border-accent/30 bg-accent-soft/45 p-4"
            >
              <div className="text-xs text-accent">总用量 Token</div>
              <div className="nib-mono mt-1.5 text-xl font-medium text-accent">
                {formatCompactTokens(usageSummary?.totalTokens ?? 0)}
              </div>
            </div>
          </div>

          {/* Breakdown Table (shown when there is usage data) */}
          {(usageSummary?.totalCalls ?? 0) > 0 && (
            <div className="overflow-hidden rounded-xl border border-border-subtle bg-surface">
              <div className="flex items-center justify-between border-b border-border-subtle px-4 py-2.5">
                <div className="flex items-center gap-1 rounded-lg border border-border-subtle bg-canvas p-0.5 text-xs">
                  <button
                    type="button"
                    onClick={() => setUsageDetailTab("stage")}
                    className={`rounded-[6px] px-2.5 py-1 transition-colors ${
                      usageDetailTab === "stage"
                        ? "bg-accent-soft font-medium text-accent"
                        : "text-ink-secondary hover:text-ink-primary"
                    }`}
                  >
                    按写作环节
                  </button>
                  <button
                    type="button"
                    onClick={() => setUsageDetailTab("model")}
                    className={`rounded-[6px] px-2.5 py-1 transition-colors ${
                      usageDetailTab === "model"
                        ? "bg-accent-soft font-medium text-accent"
                        : "text-ink-secondary hover:text-ink-primary"
                    }`}
                  >
                    按模型服务
                  </button>
                </div>
                <span className="text-xs text-ink-muted">单位：Token</span>
              </div>

              {usageDetailTab === "stage" ? (
                <table className="w-full text-left text-xs">
                  <thead className="border-b border-border-subtle bg-canvas/50 text-ink-muted">
                    <tr>
                      <th className="px-4 py-2.5 font-medium">环节</th>
                      <th className="px-4 py-2.5 text-right font-medium">调用</th>
                      <th className="px-4 py-2.5 text-right font-medium">输入</th>
                      <th className="px-4 py-2.5 text-right font-medium">输出</th>
                      <th className="px-4 py-2.5 text-right font-medium">合计</th>
                      <th className="px-4 py-2.5 text-right font-medium">平均耗时</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border-subtle">
                    {(usageSummary?.byStage ?? []).map((row) => {
                      const label =
                        PIPELINE_STAGES.find((s) => s.stage === row.stage)?.label ??
                        (row.stage === "style" ? "文风分析" : row.stage);
                      return (
                        <tr key={row.stage}>
                          <td className="px-4 py-2.5 text-[13px] font-medium text-ink-primary">
                            {label}
                          </td>
                          <td className="nib-mono px-4 py-2.5 text-right text-ink-secondary">
                            {formatNumber(row.calls)}
                          </td>
                          <td
                            title={formatNumber(row.promptTokens)}
                            className="nib-mono px-4 py-2.5 text-right text-ink-secondary"
                          >
                            {formatCompactTokens(row.promptTokens)}
                          </td>
                          <td
                            title={formatNumber(row.completionTokens)}
                            className="nib-mono px-4 py-2.5 text-right text-ink-secondary"
                          >
                            {formatCompactTokens(row.completionTokens)}
                          </td>
                          <td
                            title={formatNumber(row.totalTokens)}
                            className="nib-mono px-4 py-2.5 text-right font-medium text-ink-primary"
                          >
                            {formatCompactTokens(row.totalTokens)}
                          </td>
                          <td className="nib-mono px-4 py-2.5 text-right text-ink-muted">
                            {formatLatency(row.avgLatencyMs)}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              ) : (
                <table className="w-full text-left text-xs">
                  <thead className="border-b border-border-subtle bg-canvas/50 text-ink-muted">
                    <tr>
                      <th className="px-4 py-2.5 font-medium">模型服务</th>
                      <th className="px-4 py-2.5 font-medium">模型</th>
                      <th className="px-4 py-2.5 text-right font-medium">调用</th>
                      <th className="px-4 py-2.5 text-right font-medium">输入</th>
                      <th className="px-4 py-2.5 text-right font-medium">输出</th>
                      <th className="px-4 py-2.5 text-right font-medium">合计</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border-subtle">
                    {(usageSummary?.byModel ?? []).map((row, idx) => (
                      <tr key={`${row.providerName}-${row.modelName}-${idx}`}>
                        <td className="px-4 py-2.5 text-[13px] text-ink-primary">
                          {row.providerName || "—"}
                        </td>
                        <td className="nib-mono px-4 py-2.5 text-ink-secondary">
                          {row.modelName || "—"}
                        </td>
                        <td className="nib-mono px-4 py-2.5 text-right text-ink-secondary">
                          {formatNumber(row.calls)}
                        </td>
                        <td
                          title={formatNumber(row.promptTokens)}
                          className="nib-mono px-4 py-2.5 text-right text-ink-secondary"
                        >
                          {formatCompactTokens(row.promptTokens)}
                        </td>
                        <td
                          title={formatNumber(row.completionTokens)}
                          className="nib-mono px-4 py-2.5 text-right text-ink-secondary"
                        >
                          {formatCompactTokens(row.completionTokens)}
                        </td>
                        <td
                          title={formatNumber(row.totalTokens)}
                          className="nib-mono px-4 py-2.5 text-right font-medium text-ink-primary"
                        >
                          {formatCompactTokens(row.totalTokens)}
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
