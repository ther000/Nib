import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { Channel } from "mygo-runtime";
import {
  Check,
  FolderOpen,
  Loader2,
  RotateCcw,
  Sparkles,
  Square,
  X,
} from "lucide-react";
import {
  LibraryService,
  SettingsService,
  type BookGenesisPlan,
  type GenesisChunk,
} from "../mygo";
import { useApp } from "../stores/AppContext";
import {
  EMOTION_TAGS,
  ENTITY_KINDS,
  formatErrorMessage,
} from "../lib/constants";

type CreateMode = "ai" | "manual";

const IDEA_EXAMPLES: { genre: string; label: string; text: string }[] = [
  {
    genre: "xuanhuan",
    label: "三息残剑",
    text: "外门杂役被构陷废去修为，坠崖前捡到一把每十息可回溯三息时间的残剑，借时间差一路越阶反杀宗门强敌。",
  },
  {
    genre: "xianxia",
    label: "寿元典当行",
    text: "资质平庸的散修意外继承一座能以他人因果换取寿元与功法的黑市典当行，步步为营在各大仙门之间藏拙成道。",
  },
  {
    genre: "urban",
    label: "未来三天快讯",
    text: "刚被裁员的投行分析师手机每天零点收到一条来自三天后的商业快讯，凭信息差精准抄底、手撕前老板并建立资本帝国。",
  },
];

export function CreateBookModal() {
  const {
    createBookOpen,
    closeCreateBook,
    genrePresets,
    providers,
    modelRoutes,
    openSettings,
    refreshBooks,
    openBook,
    notify,
  } = useApp();

  const [mode, setMode] = useState<CreateMode>("ai");

  // Common / Manual fields
  const [title, setTitle] = useState("");
  const [genre, setGenre] = useState("xuanhuan");
  const [premise, setPremise] = useState("");
  const [goldenFinger, setGoldenFinger] = useState("");
  const [targetWords, setTargetWords] = useState("2500");
  const [exportDir, setExportDir] = useState("");
  const [submitting, setSubmitting] = useState(false);

  // AI Genesis state
  const [idea, setIdea] = useState("");
  const [chapterCount, setChapterCount] = useState(3);
  const [generating, setGenerating] = useState(false);
  const [streamChars, setStreamChars] = useState(0);
  const [plan, setPlan] = useState<BookGenesisPlan | null>(null);
  const [selectedEntityIdx, setSelectedEntityIdx] = useState<Set<number>>(new Set());
  const [selectedOutlineIdx, setSelectedOutlineIdx] = useState<Set<number>>(new Set());
  const channelRef = useRef<Channel<GenesisChunk> | null>(null);
  const userStoppedRef = useRef(false);

  const hasReadyModel = useMemo(() => {
    const enabledProviderIds = new Set(
      providers.filter((p) => p.enabled && p.models.length > 0).map((p) => p.id),
    );
    return modelRoutes.some(
      (r) =>
        (r.stage === "outline" || r.stage === "draft") &&
        r.providerId != null &&
        enabledProviderIds.has(r.providerId) &&
        r.modelName.trim().length > 0,
    );
  }, [providers, modelRoutes]);

  useEffect(() => {
    if (!createBookOpen) return;
    setMode("ai");
    setTitle("");
    setGenre(genrePresets[0]?.id ?? "xuanhuan");
    setPremise("");
    setGoldenFinger("");
    setTargetWords("2500");
    setExportDir("");
    setSubmitting(false);
    setIdea("");
    setChapterCount(3);
    setGenerating(false);
    setStreamChars(0);
    setPlan(null);
    setSelectedEntityIdx(new Set());
    setSelectedOutlineIdx(new Set());
  }, [createBookOpen, genrePresets]);

  useEffect(() => {
    if (!createBookOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !generating && !submitting) {
        closeCreateBook();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [createBookOpen, generating, submitting, closeCreateBook]);

  useEffect(() => {
    return () => {
      userStoppedRef.current = true;
      try {
        channelRef.current?.close();
      } catch {
        // ignore
      }
      channelRef.current = null;
    };
  }, []);

  if (!createBookOpen) return null;

  const handlePickExportDir = async () => {
    try {
      const dir = await SettingsService.pickExportDir();
      if (dir) setExportDir(dir);
    } catch (err) {
      notify({ tone: "danger", message: "选择导出目录失败", detail: formatErrorMessage(err) });
    }
  };

  const handleStopGenerate = () => {
    userStoppedRef.current = true;
    try {
      channelRef.current?.close();
    } catch {
      // ignore
    }
  };

  const handleGeneratePlan = async () => {
    const trimmedIdea = idea.trim();
    if (!trimmedIdea) {
      notify({ tone: "warn", message: "请先写一句故事灵感" });
      return;
    }
    const parsedWords = Number.parseInt(targetWords, 10);
    if (!Number.isFinite(parsedWords) || parsedWords < 300 || parsedWords > 50000) {
      notify({ tone: "warn", message: "每章目标字数需在 300 到 50,000 字之间" });
      return;
    }
    if (!hasReadyModel) {
      const needProvider = !providers.some((p) => p.enabled && p.models.length > 0);
      notify({
        tone: "warn",
        message: needProvider ? "请先连接一个模型服务并启用模型" : "请先为写作或大纲阶段分配模型",
        actionLabel: "去设置",
        onAction: () => {
          closeCreateBook();
          openSettings(needProvider ? "providers" : "routes");
        },
      });
      return;
    }

    try {
      channelRef.current?.close();
    } catch {
      // ignore
    }
    userStoppedRef.current = false;

    setGenerating(true);
    setStreamChars(0);
    setPlan(null);

    const ch = new Channel<GenesisChunk>((chunk) => {
      if (chunk.kind === "delta" && chunk.delta) {
        setStreamChars((prev) => prev + chunk.delta!.length);
      } else if (chunk.kind === "completed" && chunk.plan) {
        setPlan(chunk.plan);
        setSelectedEntityIdx(new Set(chunk.plan.entities.map((_, i) => i)));
        setSelectedOutlineIdx(new Set(chunk.plan.initialOutlines.map((_, i) => i)));
      }
    });
    channelRef.current = ch;

    try {
      // 注意：MyGo v0.10.0 中带 Channel 参数的方法不可与 .with({ signal }) 混用（否则 pageCtx 包装会导致 Channel 立即关闭），
      // 取消流式调用直接通过 ch.close() 触发 Go 端 ctx 取消。
      const generated = await LibraryService.generateBookGenesis(
        {
          idea: trimmedIdea,
          genre,
          chapterCount,
          targetChapterWords: parsedWords,
        },
        ch,
      );
      setPlan(generated);
      setSelectedEntityIdx(new Set(generated.entities.map((_, i) => i)));
      setSelectedOutlineIdx(new Set(generated.initialOutlines.map((_, i) => i)));
    } catch (err) {
      if (!userStoppedRef.current) {
        notify({
          tone: "danger",
          message: "生成开书企划失败",
          detail: formatErrorMessage(err),
        });
      }
    } finally {
      try {
        if (!ch.closed) ch.close();
      } catch {
        // ignore
      }
      if (channelRef.current === ch) channelRef.current = null;
      setGenerating(false);
    }
  };

  const handleApplyPlan = async () => {
    if (!plan) return;
    if (!plan.title.trim()) {
      notify({ tone: "warn", message: "请输入书名" });
      return;
    }
    setSubmitting(true);
    try {
      const filteredPlan: BookGenesisPlan = {
        ...plan,
        entities: plan.entities.filter((_, i) => selectedEntityIdx.has(i)),
        initialOutlines: plan.initialOutlines.filter((_, i) => selectedOutlineIdx.has(i)),
      };
      const result = await LibraryService.applyBookGenesis({
        plan: filteredPlan,
        exportDir: exportDir.trim(),
      });
      await refreshBooks(result.book.id);
      await openBook(result.book.id, "architect");
      closeCreateBook();
      notify({
        tone: "ok",
        message: `已建好《${result.book.title}》`,
        detail: `包含 ${result.entities.length} 条设定与 ${result.chapters.length} 章大纲`,
      });
    } catch (err) {
      notify({ tone: "danger", message: "创建书籍失败", detail: formatErrorMessage(err) });
    } finally {
      setSubmitting(false);
    }
  };

  const handleManualSubmit = async (e: FormEvent) => {
    e.preventDefault();
    const trimmedTitle = title.trim();
    if (!trimmedTitle) {
      notify({ tone: "warn", message: "请输入书名" });
      return;
    }
    const parsedWords = Number.parseInt(targetWords, 10);
    if (!Number.isFinite(parsedWords) || parsedWords < 300 || parsedWords > 50000) {
      notify({ tone: "warn", message: "每章目标字数需在 300 到 50,000 字之间" });
      return;
    }

    setSubmitting(true);
    try {
      const created = await LibraryService.createBook({
        title: trimmedTitle,
        genre,
        premise: premise.trim(),
        goldenFinger: goldenFinger.trim(),
        targetChapterWords: parsedWords,
        exportDir: exportDir.trim(),
      });
      await refreshBooks(created.id);
      await openBook(created.id, "architect");
      closeCreateBook();
      notify({ tone: "ok", message: `已新建《${created.title}》` });
    } catch (err) {
      notify({ tone: "danger", message: "新建书籍失败", detail: formatErrorMessage(err) });
    } finally {
      setSubmitting(false);
    }
  };

  const toggleIndex = (set: Set<number>, idx: number) => {
    const next = new Set(set);
    if (next.has(idx)) next.delete(idx);
    else next.add(idx);
    return next;
  };

  const entityKindLabel = (kind: string) =>
    ENTITY_KINDS.find((k) => k.value === kind)?.label ?? kind;

  const emotionLabel = (tag: string) =>
    EMOTION_TAGS.find((t) => t.value === tag)?.shortLabel ?? tag;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/55 p-4"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget && !generating && !submitting) {
          closeCreateBook();
        }
      }}
    >
      <div className="flex max-h-[90vh] w-full max-w-[640px] flex-col overflow-hidden rounded-xl border border-border-strong bg-elevated shadow-[var(--shadow-elevated)]">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border-subtle px-5 py-3.5">
          <div className="flex items-center gap-3">
            <h2 className="text-base font-medium text-ink-primary">新建书籍</h2>
            <div className="flex items-center rounded-lg border border-border-subtle bg-canvas p-0.5">
              <button
                type="button"
                disabled={generating || submitting}
                onClick={() => setMode("ai")}
                className={`flex items-center gap-1.5 rounded-[6px] px-2.5 py-1 text-xs font-medium transition-colors ${
                  mode === "ai"
                    ? "bg-elevated text-ink-primary"
                    : "text-ink-muted hover:text-ink-primary"
                }`}
              >
                <Sparkles className="h-3.5 w-3.5 text-accent" />
                <span>AI 一句话开书</span>
              </button>
              <button
                type="button"
                disabled={generating || submitting}
                onClick={() => setMode("manual")}
                className={`rounded-[6px] px-2.5 py-1 text-xs font-medium transition-colors ${
                  mode === "manual"
                    ? "bg-elevated text-ink-primary"
                    : "text-ink-muted hover:text-ink-primary"
                }`}
              >
                手动填写
              </button>
            </div>
          </div>
          <button
            type="button"
            disabled={generating || submitting}
            onClick={closeCreateBook}
            aria-label="关闭"
            className="rounded-lg p-1 text-ink-muted transition-colors hover:bg-surface hover:text-ink-primary disabled:opacity-40"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {mode === "ai" ? (
          plan ? (
            /* AI Plan Review & Tweak */
            <div className="flex flex-1 flex-col overflow-hidden">
              <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-[1fr_140px]">
                  <div>
                    <label className="mb-1 block text-xs font-medium text-ink-secondary">
                      书名
                    </label>
                    <input
                      type="text"
                      value={plan.title}
                      onChange={(e) => setPlan({ ...plan, title: e.target.value })}
                      className="w-full rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] font-medium text-ink-primary focus:border-accent focus:outline-none"
                    />
                  </div>
                  <div>
                    <label className="mb-1 block text-xs font-medium text-ink-secondary">
                      每章目标字数
                    </label>
                    <div className="relative">
                      <input
                        type="number"
                        min={300}
                        max={50000}
                        step={100}
                        value={plan.targetChapterWords}
                        onChange={(e) =>
                          setPlan({
                            ...plan,
                            targetChapterWords: Number.parseInt(e.target.value, 10) || 2500,
                          })
                        }
                        className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas py-2 pr-8 pl-3 text-[13px] text-ink-primary focus:border-accent focus:outline-none"
                      />
                      <span className="pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-xs text-ink-muted">
                        字
                      </span>
                    </div>
                  </div>
                </div>

                <div>
                  <label className="mb-1 block text-xs font-medium text-ink-secondary">
                    故事简介
                  </label>
                  <textarea
                    rows={2}
                    value={plan.premise}
                    onChange={(e) => setPlan({ ...plan, premise: e.target.value })}
                    className="w-full resize-none rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] leading-relaxed text-ink-primary focus:border-accent focus:outline-none"
                  />
                </div>

                <div>
                  <label className="mb-1 block text-xs font-medium text-ink-secondary">
                    金手指与核心依仗
                  </label>
                  <textarea
                    rows={2}
                    value={plan.goldenFinger}
                    onChange={(e) => setPlan({ ...plan, goldenFinger: e.target.value })}
                    className="w-full resize-none rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] leading-relaxed text-ink-primary focus:border-accent focus:outline-none"
                  />
                </div>

                {/* First Volume */}
                <div className="rounded-lg border border-border-subtle bg-canvas px-3.5 py-3">
                  <div className="mb-2 text-xs font-medium text-ink-secondary">第一卷规划</div>
                  <input
                    type="text"
                    value={plan.firstVolume.title}
                    onChange={(e) =>
                      setPlan({
                        ...plan,
                        firstVolume: { ...plan.firstVolume, title: e.target.value },
                      })
                    }
                    className="mb-2 w-full rounded-[6px] border border-border-subtle bg-surface px-2.5 py-1.5 text-[13px] font-medium text-ink-primary focus:border-accent focus:outline-none"
                  />
                  <div className="text-xs leading-relaxed text-ink-secondary">
                    卷目标：{plan.firstVolume.goal || "未填"}
                  </div>
                  {plan.firstVolume.climaxPlan && (
                    <div className="mt-1 text-xs leading-relaxed text-ink-muted">
                      高潮落点：{plan.firstVolume.climaxPlan}
                    </div>
                  )}
                </div>

                {/* Entities */}
                <div>
                  <div className="mb-2 flex items-center justify-between">
                    <span className="text-xs font-medium text-ink-secondary">
                      初始设定（已选 {selectedEntityIdx.size} / {plan.entities.length}）
                    </span>
                  </div>
                  <div className="divide-y divide-border-subtle rounded-lg border border-border-subtle bg-canvas">
                    {plan.entities.map((ent, idx) => {
                      const checked = selectedEntityIdx.has(idx);
                      return (
                        <button
                          key={`${ent.name}-${idx}`}
                          type="button"
                          onClick={() => setSelectedEntityIdx((s) => toggleIndex(s, idx))}
                          className="flex w-full items-start gap-2.5 px-3 py-2.5 text-left transition-colors hover:bg-surface/60"
                        >
                          <span
                            className={`mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-[4px] border ${
                              checked
                                ? "border-accent bg-accent text-white"
                                : "border-border-strong bg-surface"
                            }`}
                          >
                            {checked && <Check className="h-3 w-3" />}
                          </span>
                          <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-2">
                              <span className="text-[13px] font-medium text-ink-primary">
                                {ent.name}
                              </span>
                              <span className="rounded bg-surface px-1.5 py-0.5 text-xs text-ink-muted">
                                {entityKindLabel(ent.kind)}
                              </span>
                              {ent.isInvariant && (
                                <span className="rounded bg-accent-soft px-1.5 py-0.5 text-xs text-accent">
                                  铁律
                                </span>
                              )}
                            </div>
                            {ent.summary && (
                              <div className="mt-0.5 text-xs leading-relaxed text-ink-secondary">
                                {ent.summary}
                              </div>
                            )}
                          </div>
                        </button>
                      );
                    })}
                  </div>
                </div>

                {/* Initial Outlines */}
                {plan.initialOutlines.length > 0 && (
                  <div>
                    <div className="mb-2 flex items-center justify-between">
                      <span className="text-xs font-medium text-ink-secondary">
                        初始章纲（已选 {selectedOutlineIdx.size} / {plan.initialOutlines.length}）
                      </span>
                    </div>
                    <div className="divide-y divide-border-subtle rounded-lg border border-border-subtle bg-canvas">
                      {plan.initialOutlines.map((ch, idx) => {
                        const checked = selectedOutlineIdx.has(idx);
                        return (
                          <button
                            key={`${ch.title}-${idx}`}
                            type="button"
                            onClick={() => setSelectedOutlineIdx((s) => toggleIndex(s, idx))}
                            className="flex w-full items-start gap-2.5 px-3 py-2.5 text-left transition-colors hover:bg-surface/60"
                          >
                            <span
                              className={`mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-[4px] border ${
                                checked
                                  ? "border-accent bg-accent text-white"
                                  : "border-border-strong bg-surface"
                              }`}
                            >
                              {checked && <Check className="h-3 w-3" />}
                            </span>
                            <div className="min-w-0 flex-1">
                              <div className="flex items-center gap-2">
                                <span className="text-[13px] font-medium text-ink-primary">
                                  第 {idx + 1} 章 {ch.title}
                                </span>
                                <span className="rounded bg-surface px-1.5 py-0.5 text-xs text-ink-muted">
                                  {emotionLabel(ch.emotionTag)}
                                </span>
                                <span className="text-xs text-ink-muted">
                                  {ch.beats.length} 个节拍
                                </span>
                              </div>
                              {ch.outlineSummary && (
                                <div className="mt-0.5 text-xs leading-relaxed text-ink-secondary">
                                  {ch.outlineSummary}
                                </div>
                              )}
                            </div>
                          </button>
                        );
                      })}
                    </div>
                  </div>
                )}
              </div>

              <div className="flex items-center justify-between border-t border-border-subtle bg-surface/50 px-5 py-3">
                <button
                  type="button"
                  disabled={submitting}
                  onClick={() => setPlan(null)}
                  className="flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-[13px] text-ink-secondary transition-colors hover:bg-surface hover:text-ink-primary"
                >
                  <RotateCcw className="h-3.5 w-3.5" />
                  <span>调整灵感</span>
                </button>
                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    disabled={submitting}
                    onClick={closeCreateBook}
                    className="rounded-lg px-3.5 py-1.5 text-[13px] font-medium text-ink-secondary transition-colors hover:bg-surface hover:text-ink-primary"
                  >
                    取消
                  </button>
                  <button
                    type="button"
                    disabled={submitting}
                    onClick={() => void handleApplyPlan()}
                    className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-1.5 text-[13px] font-medium text-white transition-colors hover:bg-accent-hover disabled:opacity-50"
                  >
                    {submitting && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                    <span>{submitting ? "创建中…" : "确认建书并进入大纲"}</span>
                  </button>
                </div>
              </div>
            </div>
          ) : (
            /* AI Idea Input Step */
            <div className="flex flex-1 flex-col overflow-hidden">
              <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
                <div>
                  <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                    写下你的故事灵感
                  </label>
                  <textarea
                    rows={3}
                    autoFocus
                    disabled={generating}
                    value={idea}
                    onChange={(e) => setIdea(e.target.value)}
                    placeholder="例如：落魄外门弟子捡到能回溯三息的残剑，在宗门大比一路越阶反杀。"
                    className="w-full resize-none rounded-lg border border-border-subtle bg-canvas px-3 py-2.5 text-[13px] leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none disabled:opacity-60"
                  />
                  <div className="mt-2 flex flex-wrap items-center gap-1.5">
                    <span className="text-xs text-ink-muted">试试灵感：</span>
                    {IDEA_EXAMPLES.map((ex) => (
                      <button
                        key={ex.label}
                        type="button"
                        disabled={generating}
                        onClick={() => {
                          setIdea(ex.text);
                          setGenre(ex.genre);
                        }}
                        className="rounded-[6px] border border-border-subtle bg-canvas px-2 py-0.5 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary disabled:opacity-50"
                      >
                        {ex.label}
                      </button>
                    ))}
                  </div>
                </div>

                {/* Genre Pack */}
                <div>
                  <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                    题材风格
                  </label>
                  <div className="grid grid-cols-2 gap-2.5">
                    {genrePresets.map((preset) => {
                      const selected = preset.id === genre;
                      return (
                        <button
                          key={preset.id}
                          type="button"
                          disabled={generating}
                          onClick={() => setGenre(preset.id)}
                          className={`rounded-lg border px-3 py-2.5 text-left transition-colors disabled:opacity-60 ${
                            selected
                              ? "border-accent bg-accent-soft"
                              : "border-border-subtle bg-canvas hover:border-border-strong"
                          }`}
                        >
                          <div className="flex items-center justify-between gap-2">
                            <span
                              className={`text-[13px] font-medium ${
                                selected ? "text-accent" : "text-ink-primary"
                              }`}
                            >
                              {preset.name}
                            </span>
                            <span className="text-xs text-ink-muted">
                              {preset.loopCycleChapters}
                            </span>
                          </div>
                          <div className="mt-1 line-clamp-1 text-xs text-ink-secondary">
                            {preset.tagline}
                          </div>
                        </button>
                      );
                    })}
                  </div>
                </div>

                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <div>
                    <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                      同时生成初始章纲
                    </label>
                    <div className="flex gap-2">
                      {[3, 5].map((cnt) => (
                        <button
                          key={cnt}
                          type="button"
                          disabled={generating}
                          onClick={() => setChapterCount(cnt)}
                          className={`flex-1 rounded-lg border py-2 text-[13px] font-medium transition-colors ${
                            chapterCount === cnt
                              ? "border-accent bg-accent-soft text-accent"
                              : "border-border-subtle bg-canvas text-ink-secondary hover:border-border-strong"
                          }`}
                        >
                          前 {cnt} 章
                        </button>
                      ))}
                    </div>
                  </div>
                  <div>
                    <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                      每章目标字数
                    </label>
                    <div className="relative">
                      <input
                        type="number"
                        min={300}
                        max={50000}
                        step={100}
                        disabled={generating}
                        value={targetWords}
                        onChange={(e) => setTargetWords(e.target.value)}
                        className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas py-2 pr-8 pl-3 text-[13px] text-ink-primary focus:border-accent focus:outline-none disabled:opacity-60"
                      />
                      <span className="pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-xs text-ink-muted">
                        字
                      </span>
                    </div>
                  </div>
                </div>

                {generating && (
                  <div className="flex items-center justify-between rounded-lg border border-border-subtle bg-canvas px-3.5 py-2.5">
                    <div className="flex items-center gap-2 text-xs text-ink-primary">
                      <Loader2 className="h-3.5 w-3.5 animate-spin text-accent" />
                      <span>正在构思书名、核心角色、世界规则与前 {chapterCount} 章大纲…</span>
                    </div>
                    {streamChars > 0 && (
                      <span className="nib-mono text-xs text-ink-muted">
                        已生成 {streamChars} 字
                      </span>
                    )}
                  </div>
                )}
              </div>

              <div className="flex items-center justify-end gap-2 border-t border-border-subtle bg-surface/50 px-5 py-3">
                {generating ? (
                  <button
                    type="button"
                    onClick={handleStopGenerate}
                    className="flex items-center gap-1.5 rounded-lg border border-border-strong bg-surface px-4 py-1.5 text-[13px] font-medium text-ink-primary transition-colors hover:bg-canvas"
                  >
                    <Square className="h-3.5 w-3.5" />
                    <span>停止生成</span>
                  </button>
                ) : (
                  <>
                    <button
                      type="button"
                      onClick={closeCreateBook}
                      className="rounded-lg px-3.5 py-1.5 text-[13px] font-medium text-ink-secondary transition-colors hover:bg-surface hover:text-ink-primary"
                    >
                      取消
                    </button>
                    <button
                      type="button"
                      onClick={() => void handleGeneratePlan()}
                      className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-1.5 text-[13px] font-medium text-white transition-colors hover:bg-accent-hover"
                    >
                      <Sparkles className="h-3.5 w-3.5" />
                      <span>生成开书企划</span>
                    </button>
                  </>
                )}
              </div>
            </div>
          )
        ) : (
          /* Manual Create Form */
          <form onSubmit={handleManualSubmit} className="flex flex-1 flex-col overflow-y-auto">
            <div className="flex flex-col gap-4 px-5 py-4">
              <div>
                <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                  书名
                </label>
                <input
                  type="text"
                  autoFocus
                  required
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                  placeholder="例如：太初剑渊"
                  className="w-full rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                />
              </div>

              <div>
                <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                  题材风格
                </label>
                <div className="grid grid-cols-2 gap-2.5">
                  {genrePresets.map((preset) => {
                    const selected = preset.id === genre;
                    return (
                      <button
                        key={preset.id}
                        type="button"
                        onClick={() => setGenre(preset.id)}
                        className={`rounded-lg border px-3 py-2.5 text-left transition-colors ${
                          selected
                            ? "border-accent bg-accent-soft"
                            : "border-border-subtle bg-canvas hover:border-border-strong"
                        }`}
                      >
                        <div className="flex items-center justify-between gap-2">
                          <span
                            className={`text-[13px] font-medium ${
                              selected ? "text-accent" : "text-ink-primary"
                            }`}
                          >
                            {preset.name}
                          </span>
                          <span className="text-xs text-ink-muted">
                            {preset.loopCycleChapters}
                          </span>
                        </div>
                        <div className="mt-1 line-clamp-1 text-xs text-ink-secondary">
                          {preset.tagline}
                        </div>
                      </button>
                    );
                  })}
                </div>
              </div>

              <div>
                <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                  故事简介
                </label>
                <textarea
                  rows={2}
                  value={premise}
                  onChange={(e) => setPremise(e.target.value)}
                  placeholder="主角开局处境、核心矛盾与目标（可稍后补充）"
                  className="w-full resize-none rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                />
              </div>

              <div>
                <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                  金手指与核心依仗
                </label>
                <textarea
                  rows={2}
                  value={goldenFinger}
                  onChange={(e) => setGoldenFinger(e.target.value)}
                  placeholder="主角的特殊能力、触发条件与限制（选填）"
                  className="w-full resize-none rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                />
              </div>

              <div className="grid grid-cols-1 gap-3 sm:grid-cols-[148px_1fr]">
                <div>
                  <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                    每章目标字数
                  </label>
                  <div className="relative">
                    <input
                      type="number"
                      min={300}
                      max={50000}
                      step={100}
                      value={targetWords}
                      onChange={(e) => setTargetWords(e.target.value)}
                      className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas py-2 pr-8 pl-3 text-[13px] text-ink-primary focus:border-accent focus:outline-none"
                    />
                    <span className="pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-xs text-ink-muted">
                      字
                    </span>
                  </div>
                </div>

                <div>
                  <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                    本地备份文件夹（选填）
                  </label>
                  <div className="flex gap-2">
                    <input
                      type="text"
                      value={exportDir}
                      onChange={(e) => setExportDir(e.target.value)}
                      placeholder="选择后，定稿会自动备份为 Markdown"
                      className="min-w-0 flex-1 rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                    />
                    <button
                      type="button"
                      onClick={() => void handlePickExportDir()}
                      className="flex shrink-0 items-center gap-1.5 rounded-lg border border-border-subtle bg-surface px-3 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary"
                    >
                      <FolderOpen className="h-3.5 w-3.5" />
                      <span>选择</span>
                    </button>
                  </div>
                </div>
              </div>
            </div>

            <div className="flex items-center justify-end gap-2 border-t border-border-subtle bg-surface/50 px-5 py-3">
              <button
                type="button"
                onClick={closeCreateBook}
                className="rounded-lg px-3.5 py-1.5 text-[13px] font-medium text-ink-secondary transition-colors hover:bg-surface hover:text-ink-primary"
              >
                取消
              </button>
              <button
                type="submit"
                disabled={submitting}
                className="rounded-lg bg-accent px-4 py-1.5 text-[13px] font-medium text-white transition-colors hover:bg-accent-hover disabled:opacity-50"
              >
                {submitting ? "创建中…" : "创建书籍"}
              </button>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}
