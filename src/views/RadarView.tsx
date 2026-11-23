import { useCallback, useEffect, useMemo, useState } from "react";
import {
  AlertCircle,
  ArrowLeft,
  ArrowRight,
  Check,
  ExternalLink,
  Info,
  Loader2,
  RefreshCw,
  Sparkles,
  Square,
  X,
} from "lucide-react";
import {
  RadarService,
  type RadarAnalysis,
  type RadarAnalyzeChunk,
  type RadarBoard,
  type RadarBoardSpec,
  type RadarIdeaAngle,
  type RadarItem,
  type RadarPlatform,
} from "../mygo";
import { useApp } from "../stores/AppContext";
import { useMygoChannel } from "../hooks/useMygoStream";
import { hasAnyEnabledModel, hasReadyModelRoute } from "../lib/modelReady";
import { formatErrorMessage } from "../lib/constants";

const MAX_SELECTED_ITEMS = 8;

const QIDIAN_CATEGORIES: { id: number; label: string }[] = [
  { id: -1, label: "全站" },
  { id: 21, label: "玄幻" },
  { id: 22, label: "仙侠" },
  { id: 4, label: "都市" },
  { id: 5, label: "历史" },
  { id: 9, label: "科幻" },
  { id: 10, label: "悬疑灵异" },
  { id: 7, label: "游戏" },
  { id: 20109, label: "诸天无限" },
  { id: 12, label: "轻小说" },
  { id: 1, label: "奇幻" },
  { id: 2, label: "武侠" },
  { id: 15, label: "现实" },
  { id: 6, label: "军事" },
  { id: 8, label: "体育" },
];

export function RadarView() {
  const {
    closeRadar,
    openCreateBook,
    openSettings,
    genrePresets,
    providers,
    modelRoutes,
    notify,
  } = useApp();

  const hasReadyModel = useMemo(
    () => hasReadyModelRoute(providers, modelRoutes, ["outline", "draft"]),
    [providers, modelRoutes]
  );

  const [boards, setBoards] = useState<RadarBoardSpec[]>([]);
  const [platform, setPlatform] = useState<RadarPlatform>("qidian");
  const [qidianCatId, setQidianCatId] = useState<number>(-1);
  const [fanqieGroup, setFanqieGroup] = useState<string>("全站总榜");
  const [boardKey, setBoardKey] = useState<string>("hotsales");
  const [boardData, setBoardData] = useState<RadarBoard | null>(null);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);

  // Selected items for AI trend analysis (Qidian plaintext)
  const [selectedItems, setSelectedItems] = useState<RadarItem[]>([]);
  const [rawText, setRawText] = useState("");
  const [focus, setFocus] = useState("");

  // AI Analysis stream state
  const [analyzing, setAnalyzing] = useState(false);
  const [streamChars, setStreamChars] = useState(0);
  const [analysis, setAnalysis] = useState<RadarAnalysis | null>(null);
  const analyzeStream = useMygoChannel<RadarAnalyzeChunk>();

  useEffect(() => {
    let cancelled = false;
    void RadarService.listBoards()
      .then((list) => {
        if (!cancelled) setBoards(list);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
      analyzeStream.stop();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const platformBoards = useMemo(
    () => boards.filter((b) => b.platform === platform),
    [boards, platform]
  );

  const fanqieGroups = useMemo(() => {
    const seen = new Set<string>();
    const list: string[] = [];
    for (const b of boards) {
      if (b.platform !== "fanqie" || !b.category || seen.has(b.category)) continue;
      seen.add(b.category);
      list.push(b.category);
    }
    return list;
  }, [boards]);

  const visibleBoards = useMemo(() => {
    if (platform !== "fanqie") return platformBoards;
    return platformBoards.filter((b) => b.category === fanqieGroup);
  }, [platform, platformBoards, fanqieGroup]);

  const fetchBoard = useCallback(
    async (plat: RadarPlatform, key: string, forceRefresh = false) => {
      setLoading(true);
      setLoadError(null);
      try {
        const res = await RadarService.fetchBoard(plat, key, forceRefresh);
        setBoardData(res);
      } catch (err) {
        setLoadError(formatErrorMessage(err));
      } finally {
        setLoading(false);
      }
    },
    []
  );

  const effectiveBoardKey =
    platform === "qidian" && qidianCatId !== -1
      ? `${boardKey}:${qidianCatId}`
      : boardKey;

  useEffect(() => {
    void fetchBoard(platform, effectiveBoardKey, false);
  }, [platform, effectiveBoardKey, fetchBoard]);

  const handleSwitchPlatform = (next: RadarPlatform) => {
    if (next === platform) return;
    setPlatform(next);
    if (next === "fanqie") {
      setFanqieGroup("全站总榜");
      setBoardKey("top_all");
      return;
    }
    const first = boards.find((b) => b.platform === next);
    setBoardKey(first?.key ?? "hotsales");
  };

  const handleSwitchFanqieGroup = (group: string) => {
    setFanqieGroup(group);
    const first = boards.find((b) => b.platform === "fanqie" && b.category === group);
    if (first) setBoardKey(first.key);
  };

  const toggleSelectItem = (item: RadarItem) => {
    setSelectedItems((prev) => {
      const exists = prev.some((x) => x.bookId === item.bookId && x.title === item.title);
      if (exists) {
        return prev.filter((x) => !(x.bookId === item.bookId && x.title === item.title));
      }
      if (prev.length >= MAX_SELECTED_ITEMS) {
        notify({ tone: "warn", message: `最多同时勾选 ${MAX_SELECTED_ITEMS} 本上榜作品` });
        return prev;
      }
      return [...prev, item];
    });
  };

  const handleGoConfigureModel = () => {
    const needProvider = !hasAnyEnabledModel(providers);
    openSettings(needProvider ? "providers" : "routes");
  };

  const handleAnalyze = async () => {
    if (selectedItems.length === 0 && !rawText.trim()) {
      notify({
        tone: "warn",
        message: "请先勾选至少一本起点上榜书，或在右侧粘贴榜单文本",
      });
      return;
    }
    if (!hasReadyModel) {
      handleGoConfigureModel();
      return;
    }

    setAnalyzing(true);
    setStreamChars(0);

    const { channel: ch, runId } = analyzeStream.open((chunk) => {
      if (!analyzeStream.isCurrent(runId)) return;
      if (chunk.kind === "delta" && chunk.delta) {
        setStreamChars((prev) => prev + chunk.delta!.length);
      } else if (chunk.kind === "completed" && chunk.analysis) {
        setAnalysis(chunk.analysis);
      }
    });

    try {
      const res = await RadarService.analyzeTrend(
        {
          platform: boardData?.platform ?? platform,
          boardLabel: boardData?.boardLabel ?? "",
          items: selectedItems,
          rawText: rawText.trim(),
          focus: focus.trim(),
        },
        ch
      );
      if (!analyzeStream.isCurrent(runId)) return;
      setAnalysis(res);
    } catch (err) {
      if (analyzeStream.isCurrent(runId) && !analyzeStream.wasStopped()) {
        notify({
          tone: "danger",
          message: "AI 拆榜失败",
          detail: formatErrorMessage(err),
        });
      }
    } finally {
      analyzeStream.release(ch);
      if (analyzeStream.isCurrent(runId)) {
        setAnalyzing(false);
      }
    }
  };

  const handleBringAngleToCreate = (angle: RadarIdeaAngle) => {
    openCreateBook("ai", {
      idea: angle.idea,
      genre: angle.genre,
      styleTags: angle.styleTags,
    });
  };

  const getGenreName = (genreId: string) =>
    genrePresets.find((g) => g.id === genreId)?.name ?? genreId;

  const fanqieFontStyle =
    boardData?.fontProtected && boardData.fontFamily
      ? { fontFamily: `"${boardData.fontFamily}", var(--font-ui)` }
      : undefined;

  return (
    <main className="nib-paper-sheet m-2 mt-0 flex flex-1 flex-col overflow-hidden">
      {/* Inject Fanqie official @font-face CSS only while viewing Fanqie rank */}
      {boardData?.fontProtected && boardData.fontCss && (
        <style>{boardData.fontCss}</style>
      )}

      {/* Sub-header */}
      <header className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-b border-border-subtle px-6 py-3.5">
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={closeRadar}
            className="flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-xs font-medium text-ink-secondary transition-colors hover:bg-surface hover:text-ink-primary"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            <span>返回</span>
          </button>
          <span className="h-4 w-px bg-border-subtle" />
          <h1 className="text-sm font-medium text-ink-primary">扫榜雷达</h1>
        </div>

        {/* Platform Switcher */}
        <div className="flex items-center rounded-xl border border-border-subtle bg-surface p-0.5">
          {(
            [
              { id: "qidian", label: "起点中文网" },
              { id: "fanqie", label: "番茄小说网" },
            ] as const
          ).map((tab) => {
            const active = platform === tab.id;
            return (
              <button
                key={tab.id}
                type="button"
                onClick={() => handleSwitchPlatform(tab.id)}
                className={`rounded-[10px] px-3.5 py-1.5 text-xs font-medium transition-colors ${
                  active
                    ? "bg-elevated text-ink-primary shadow-xs"
                    : "text-ink-muted hover:text-ink-primary"
                }`}
              >
                {tab.label}
              </button>
            );
          })}
        </div>

        <div className="flex items-center gap-2">
          {boardData?.sourceUrl && (
            <a
              href={boardData.sourceUrl}
              target="_blank"
              rel="noreferrer"
              className="flex items-center gap-1 rounded-lg border border-border-subtle bg-surface px-2.5 py-1.5 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary"
            >
              <span>官网</span>
              <ExternalLink className="h-3 w-3" />
            </a>
          )}
          <button
            type="button"
            disabled={loading}
            onClick={() => void fetchBoard(platform, effectiveBoardKey, true)}
            className="flex items-center gap-1.5 rounded-lg border border-border-subtle bg-surface px-3 py-1.5 text-xs font-medium text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary disabled:opacity-50"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin text-accent" : ""}`} />
            <span>刷新</span>
          </button>
        </div>
      </header>

      {/* Board Type Pills */}
      <div className="flex shrink-0 flex-col gap-2 border-b border-border-subtle bg-surface/40 px-6 py-2.5">
        {platform === "fanqie" && fanqieGroups.length > 0 && (
          <div className="flex flex-wrap items-center gap-1.5">
            {fanqieGroups.map((group) => {
              const active = group === fanqieGroup;
              return (
                <button
                  key={group}
                  type="button"
                  disabled={loading}
                  onClick={() => handleSwitchFanqieGroup(group)}
                  className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors disabled:opacity-50 ${
                    active
                      ? "bg-elevated text-ink-primary shadow-xs"
                      : "text-ink-muted hover:text-ink-primary"
                  }`}
                >
                  {group}
                </button>
              );
            })}
          </div>
        )}
        <div className="flex flex-wrap items-center gap-1.5">
          {visibleBoards.map((spec) => {
            const active = spec.key === boardKey;
            return (
              <button
                key={spec.key}
                type="button"
                disabled={loading}
                onClick={() => setBoardKey(spec.key)}
                className={`rounded-lg border px-2.5 py-1 text-xs transition-colors disabled:opacity-50 ${
                  active
                    ? "border-accent bg-accent-soft font-medium text-accent"
                    : "border-border-subtle bg-surface text-ink-secondary hover:border-border-strong hover:text-ink-primary"
                }`}
              >
                {spec.label}
              </button>
            );
          })}
        </div>
        {platform === "qidian" && (
          <div className="flex flex-wrap items-center gap-1">
            {QIDIAN_CATEGORIES.map((cat) => {
              const active = cat.id === qidianCatId;
              return (
                <button
                  key={cat.id}
                  type="button"
                  disabled={loading}
                  onClick={() => setQidianCatId(cat.id)}
                  className={`rounded-md px-2 py-0.5 text-[11px] transition-colors disabled:opacity-50 ${
                    active
                      ? "bg-elevated font-medium text-ink-primary shadow-xs"
                      : "text-ink-muted hover:text-ink-primary"
                  }`}
                >
                  {cat.label}
                </button>
              );
            })}
          </div>
        )}
      </div>

      {/* Main Split Workspace */}
      <div className="grid min-h-0 flex-1 grid-cols-1 overflow-hidden lg:grid-cols-[minmax(0,1fr)_390px]">
        {/* Left: Rank List */}
        <section className="flex min-h-0 flex-col overflow-y-auto px-6 py-5">
          {boardData?.fontProtected && (
            <div className="mb-4 flex items-start gap-2 rounded-xl border border-border-subtle bg-surface px-3.5 py-2.5 text-xs leading-relaxed text-ink-secondary">
              <Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-accent" />
              <span>
                番茄榜单已加载原站字体供直接浏览风向；受官方字体保护限制，如需 AI 拆解番茄书目，可在右侧文本框输入或粘贴明文书名与简介。
              </span>
            </div>
          )}

          {loading && !boardData ? (
            <div className="flex flex-1 flex-col items-center justify-center gap-2 py-20 text-xs text-ink-muted">
              <Loader2 className="h-5 w-5 animate-spin text-accent" />
              <span>正在抓取最新榜单…</span>
            </div>
          ) : loadError ? (
            <div className="flex flex-col items-center justify-center gap-3 rounded-2xl border border-danger/30 bg-surface p-8 text-center">
              <AlertCircle className="h-6 w-6 text-danger" />
              <div className="text-sm font-medium text-ink-primary">抓取榜单失败</div>
              <p className="max-w-md text-xs leading-relaxed text-ink-secondary">
                {loadError}
              </p>
              <button
                type="button"
                onClick={() => void fetchBoard(platform, effectiveBoardKey, true)}
                className="rounded-lg bg-accent px-4 py-1.5 text-xs font-semibold text-canvas hover:opacity-90"
              >
                重试抓取
              </button>
            </div>
          ) : boardData && boardData.items.length > 0 ? (
            <div className="flex flex-col gap-2.5">
              <div className="flex items-center justify-between text-xs text-ink-muted">
                <span>
                  {boardData.boardLabel} · 共 {boardData.items.length} 部
                  {boardData.cached ? "（缓存）" : ""}
                </span>
                {!boardData.fontProtected && (
                  <span>点击作品勾选加入右侧 AI 拆榜</span>
                )}
              </div>

              <div className="divide-y divide-border-subtle rounded-xl border border-border-subtle bg-surface">
                {boardData.items.map((item) => {
                  const selectable = !boardData.fontProtected;
                  const checked = selectedItems.some(
                    (x) => x.bookId === item.bookId && x.title === item.title
                  );
                  return (
                    <div
                      key={`${boardData.platform}-${item.bookId || item.rank}`}
                      onClick={() => selectable && toggleSelectItem(item)}
                      className={`flex items-start gap-3 px-4 py-3.5 transition-colors ${
                        selectable ? "cursor-pointer hover:bg-canvas/60" : ""
                      } ${checked ? "bg-accent-soft/40" : ""}`}
                    >
                      {/* Rank Number / Checkbox */}
                      <div className="flex shrink-0 items-center gap-2.5 pt-0.5">
                        {selectable && (
                          <span
                            className={`flex h-4 w-4 items-center justify-center rounded-[4px] border transition-colors ${
                              checked
                                ? "border-accent bg-accent text-canvas"
                                : "border-border-strong bg-canvas"
                            }`}
                          >
                            {checked && <Check className="h-3 w-3" />}
                          </span>
                        )}
                        <span
                          className={`nib-mono w-6 text-center text-xs font-semibold ${
                            item.rank <= 3 ? "text-accent" : "text-ink-muted"
                          }`}
                        >
                          {String(item.rank).padStart(2, "0")}
                        </span>
                      </div>

                      {/* Book Content */}
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <span
                            style={fanqieFontStyle}
                            className="text-sm font-medium text-ink-primary"
                          >
                            {item.title}
                          </span>
                          {item.author && (
                            <span
                              style={fanqieFontStyle}
                              className="text-xs text-ink-secondary"
                            >
                              {item.author}
                            </span>
                          )}
                          {item.category && (
                            <span className="rounded bg-canvas px-1.5 py-0.5 text-[11px] text-ink-muted">
                              {item.subCategory
                                ? `${item.category} · ${item.subCategory}`
                                : item.category}
                            </span>
                          )}
                          {item.wordCount && (
                            <span
                              style={fanqieFontStyle}
                              className="nib-mono text-[11px] text-ink-muted"
                            >
                              {item.wordCount}
                            </span>
                          )}
                          {item.heatText && (
                            <span
                              style={fanqieFontStyle}
                              className="nib-mono text-[11px] text-accent"
                            >
                              {item.heatText}
                            </span>
                          )}
                          {item.statusText && (
                            <span className="text-[11px] text-ink-muted">
                              {item.statusText}
                            </span>
                          )}
                        </div>
                        {item.summary && (
                          <p
                            style={fanqieFontStyle}
                            className="mt-1.5 line-clamp-2 text-xs leading-relaxed text-ink-secondary"
                          >
                            {item.summary}
                          </p>
                        )}
                      </div>

                      {item.detailUrl && (
                        <a
                          href={item.detailUrl}
                          target="_blank"
                          rel="noreferrer"
                          onClick={(e) => e.stopPropagation()}
                          title="在浏览器查看详情"
                          className="shrink-0 rounded-md p-1.5 text-ink-muted transition-colors hover:bg-canvas hover:text-ink-primary"
                        >
                          <ExternalLink className="h-3.5 w-3.5" />
                        </a>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
          ) : null}
        </section>

        {/* Right: AI Trend Analysis & One-Click Create */}
        <aside className="flex min-h-0 flex-col gap-4 overflow-y-auto border-t border-border-subtle bg-surface/50 px-5 py-5 lg:border-t-0 lg:border-l">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-1.5">
              <Sparkles className="h-4 w-4 text-accent" />
              <h2 className="text-xs font-semibold text-ink-primary">
                AI 拆榜与借势开书
              </h2>
            </div>
            {!hasReadyModel && (
              <button
                type="button"
                onClick={handleGoConfigureModel}
                className="text-xs font-medium text-warn underline underline-offset-2"
              >
                去连接模型
              </button>
            )}
          </div>

          {/* Selected Qidian Books */}
          {selectedItems.length > 0 && (
            <div className="flex flex-col gap-1.5">
              <div className="flex items-center justify-between text-[11px] text-ink-muted">
                <span>已选样本（{selectedItems.length}/{MAX_SELECTED_ITEMS}）</span>
                <button
                  type="button"
                  disabled={analyzing}
                  onClick={() => setSelectedItems([])}
                  className="hover:text-ink-primary"
                >
                  清空
                </button>
              </div>
              <div className="flex flex-wrap gap-1.5">
                {selectedItems.map((it) => (
                  <span
                    key={`sel-${it.bookId}-${it.title}`}
                    className="flex items-center gap-1 rounded-md border border-accent/40 bg-accent-soft px-2 py-0.5 text-xs text-accent"
                  >
                    <span className="max-w-[140px] truncate">{it.title}</span>
                    <button
                      type="button"
                      disabled={analyzing}
                      onClick={() => toggleSelectItem(it)}
                      className="rounded hover:bg-accent/20"
                    >
                      <X className="h-3 w-3" />
                    </button>
                  </span>
                ))}
              </div>
            </div>
          )}

          {/* Optional Pasted Text (works for Fanqie or any source) */}
          <div className="flex flex-col gap-1.5">
            <label className="text-[11px] font-medium text-ink-secondary">
              补充 / 粘贴榜单文本（选填，支持番茄或任意平台）
            </label>
            <textarea
              rows={3}
              disabled={analyzing}
              value={rawText}
              onChange={(e) => setRawText(e.target.value)}
              placeholder="可直接粘贴感兴趣的书名、简介或番茄热榜书目，和已选样本一起拆解…"
              className="w-full resize-none rounded-xl border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent disabled:opacity-60"
            />
          </div>

          <input
            type="text"
            disabled={analyzing}
            value={focus}
            onChange={(e) => setFocus(e.target.value)}
            placeholder="关注方向（选填，如：偏苟道流 / 适合新手日更 / 悬疑反差）"
            className="w-full rounded-xl border border-border-subtle bg-canvas px-3 py-2 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent disabled:opacity-60"
          />

          {analyzing ? (
            <button
              type="button"
              onClick={() => {
                analyzeStream.stop();
                setAnalyzing(false);
              }}
              className="flex w-full items-center justify-center gap-1.5 rounded-xl border border-border-strong bg-elevated py-2 text-xs font-medium text-ink-primary"
            >
              <Square className="h-3.5 w-3.5" />
              <span>停止拆解{streamChars > 0 ? `（已生成 ${streamChars} 字）` : "…"}</span>
            </button>
          ) : (
            <button
              type="button"
              onClick={() => void handleAnalyze()}
              className="flex w-full items-center justify-center gap-1.5 rounded-xl bg-accent py-2 text-xs font-semibold text-canvas transition-opacity hover:opacity-90"
            >
              <Sparkles className="h-3.5 w-3.5" />
              <span>{hasReadyModel ? "AI 拆解爆款规律" : "去连接模型"}</span>
            </button>
          )}

          {/* Analysis Results */}
          {analysis && (
            <div className="flex flex-col gap-4 border-t border-border-subtle pt-4">
              {analysis.summary && (
                <div className="rounded-xl border border-border-subtle bg-canvas p-3.5">
                  <div className="mb-1 text-[11px] font-medium text-ink-muted">
                    风向总评
                  </div>
                  <p className="text-xs leading-relaxed text-ink-primary">
                    {analysis.summary}
                  </p>
                </div>
              )}

              {analysis.hotElements.length > 0 && (
                <div>
                  <div className="mb-1.5 text-[11px] font-medium text-ink-muted">
                    高频卖点元素
                  </div>
                  <div className="flex flex-wrap gap-1.5">
                    {analysis.hotElements.map((el) => (
                      <span
                        key={el}
                        className="rounded-md border border-border-subtle bg-canvas px-2 py-0.5 text-xs text-ink-secondary"
                      >
                        {el}
                      </span>
                    ))}
                  </div>
                </div>
              )}

              {analysis.hookPatterns.length > 0 && (
                <div>
                  <div className="mb-1.5 text-[11px] font-medium text-ink-muted">
                    钩子与标题公式
                  </div>
                  <ul className="flex flex-col gap-1 text-xs leading-relaxed text-ink-secondary">
                    {analysis.hookPatterns.map((hk) => (
                      <li key={hk} className="flex items-start gap-1.5">
                        <span className="text-accent">·</span>
                        <span>{hk}</span>
                      </li>
                    ))}
                  </ul>
                </div>
              )}

              {analysis.angles.length > 0 && (
                <div className="flex flex-col gap-2.5">
                  <div className="text-[11px] font-medium text-ink-muted">
                    差异化开书切入点
                  </div>
                  {analysis.angles.map((angle, idx) => (
                    <div
                      key={`${angle.title}-${idx}`}
                      className="flex flex-col gap-2 rounded-xl border border-border-subtle bg-canvas p-3.5"
                    >
                      <div className="flex flex-wrap items-center gap-1.5">
                        <span className="text-[13px] font-medium text-ink-primary">
                          {angle.title}
                        </span>
                        <span className="rounded bg-accent-soft px-1.5 py-0.5 text-[11px] text-accent">
                          {getGenreName(angle.genre)}
                        </span>
                        {angle.styleTags.map((tag) => (
                          <span
                            key={tag}
                            className="rounded border border-border-subtle bg-surface px-1.5 py-0.5 text-[11px] text-ink-secondary"
                          >
                            {tag}
                          </span>
                        ))}
                      </div>
                      <p className="text-xs leading-relaxed text-ink-primary">
                        {angle.idea}
                      </p>
                      {angle.whyItWorks && (
                        <p className="text-[11px] leading-relaxed text-ink-muted">
                          切入理由：{angle.whyItWorks}
                        </p>
                      )}
                      <button
                        type="button"
                        onClick={() => handleBringAngleToCreate(angle)}
                        className="mt-1 flex items-center justify-center gap-1 rounded-lg border border-accent/40 bg-accent-soft/70 py-1.5 text-xs font-medium text-accent transition-colors hover:bg-accent hover:text-canvas"
                      >
                        <span>带入开书工坊</span>
                        <ArrowRight className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </aside>
      </div>
    </main>
  );
}
