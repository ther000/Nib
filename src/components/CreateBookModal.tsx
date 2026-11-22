import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import {
  Check,
  FolderOpen,
  Loader2,
  RotateCcw,
  Sparkles,
  Square,
} from "lucide-react";
import {
  LibraryService,
  SettingsService,
  type BookGenesisPlan,
  type GenesisChunk,
  type GenrePreset,
} from "../mygo";
import { useApp } from "../stores/AppContext";
import { useMygoChannel } from "../hooks/useMygoStream";
import { hasAnyEnabledModel, hasReadyModelRoute } from "../lib/modelReady";
import { ModalShell } from "./ui/ModalShell";
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

function GenrePresetGrid({
  presets,
  selectedId,
  disabled,
  onSelect,
}: {
  presets: GenrePreset[];
  selectedId: string;
  disabled?: boolean;
  onSelect: (id: string) => void;
}) {
  return (
    <div className="grid grid-cols-2 gap-2.5">
      {presets.map((preset) => {
        const selected = preset.id === selectedId;
        return (
          <button
            key={preset.id}
            type="button"
            disabled={disabled}
            onClick={() => onSelect(preset.id)}
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
              {preset.loopCycleChapters && (
                <span className="text-xs text-ink-muted">
                  节奏 {preset.loopCycleChapters}
                </span>
              )}
            </div>
            <div className="mt-1 line-clamp-1 text-xs text-ink-secondary">
              {preset.tagline}
            </div>
          </button>
        );
      })}
    </div>
  );
}

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

  const hasReadyModel = useMemo(
    () => hasReadyModelRoute(providers, modelRoutes, ["outline", "draft"]),
    [providers, modelRoutes]
  );

  // Preserve draft idea if the user clicks "去设置" mid-flow and returns
  const savedIdeaRef = useRef("");

  const [mode, setMode] = useState<CreateMode>("ai");
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
  const {
    open: openGenesisStream,
    stop: stopGenesisStream,
    release: releaseGenesisStream,
    isCurrent: isCurrentGenesis,
    wasStopped: wasGenesisStopped,
  } = useMygoChannel<GenesisChunk>();

  useEffect(() => {
    if (!createBookOpen) {
      stopGenesisStream();
      return;
    }
    setMode(hasReadyModel || savedIdeaRef.current ? "ai" : "manual");
    setTitle("");
    setGenre(genrePresets[0]?.id ?? "xuanhuan");
    setPremise("");
    setGoldenFinger("");
    setTargetWords("2500");
    setExportDir("");
    setSubmitting(false);
    setIdea(savedIdeaRef.current);
    setChapterCount(3);
    setGenerating(false);
    setStreamChars(0);
    setPlan(null);
    setSelectedEntityIdx(new Set());
    setSelectedOutlineIdx(new Set());
    // 仅在弹窗打开时初始化
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [createBookOpen, stopGenesisStream]);

  if (!createBookOpen) return null;

  const handlePickExportDir = async () => {
    try {
      const dir = await SettingsService.pickExportDir();
      if (dir) setExportDir(dir);
    } catch (err) {
      notify({ tone: "danger", message: "选择导出目录失败", detail: formatErrorMessage(err) });
    }
  };

  const handleGoConfigureModel = () => {
    savedIdeaRef.current = idea;
    const needProvider = !hasAnyEnabledModel(providers);
    closeCreateBook();
    openSettings(needProvider ? "providers" : "routes");
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
      handleGoConfigureModel();
      return;
    }

    setGenerating(true);
    setStreamChars(0);
    setPlan(null);

    const { channel: ch, runId } = openGenesisStream((chunk) => {
      if (!isCurrentGenesis(runId)) return;
      if (chunk.kind === "delta" && chunk.delta) {
        setStreamChars((prev) => prev + chunk.delta!.length);
      } else if (chunk.kind === "completed" && chunk.plan) {
        setPlan(chunk.plan);
        setSelectedEntityIdx(new Set(chunk.plan.entities.map((_, i) => i)));
        setSelectedOutlineIdx(new Set(chunk.plan.initialOutlines.map((_, i) => i)));
      }
    });

    try {
      const generated = await LibraryService.generateBookGenesis(
        {
          idea: trimmedIdea,
          genre,
          chapterCount,
          targetChapterWords: parsedWords,
        },
        ch
      );
      if (!isCurrentGenesis(runId)) return;
      setPlan(generated);
      setSelectedEntityIdx(new Set(generated.entities.map((_, i) => i)));
      setSelectedOutlineIdx(new Set(generated.initialOutlines.map((_, i) => i)));
    } catch (err) {
      if (isCurrentGenesis(runId) && !wasGenesisStopped()) {
        notify({
          tone: "danger",
          message: "生成开书企划失败",
          detail: formatErrorMessage(err),
        });
      }
    } finally {
      releaseGenesisStream(ch);
      if (isCurrentGenesis(runId)) {
        setGenerating(false);
      }
    }
  };

  const handleApplyPlan = async (targetTab: "studio" | "architect") => {
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
      savedIdeaRef.current = "";
      await refreshBooks(result.book.id);
      await openBook(result.book.id, targetTab);
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
      // 自动预建第一卷与第一章，一步直达写作页
      try {
        const vol = await LibraryService.upsertVolume({
          id: 0,
          bookId: created.id,
          seq: 1,
          title: "第一卷",
          goal: "",
          climaxPlan: "",
        });
        await LibraryService.createChapter({
          bookId: created.id,
          volumeId: vol.id,
          title: "初章",
          emotionTag: "setup",
          hookType: "mystery",
          hookNote: "",
          outlineSummary: "",
        });
      } catch {
        // 即使预建首章失败也不影响建书成功
      }
      await refreshBooks(created.id);
      await openBook(created.id, "studio");
      closeCreateBook();
      notify({ tone: "ok", message: `已新建《${created.title}》，可以直接开写第一章` });
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
    <ModalShell
      open={createBookOpen}
      onClose={closeCreateBook}
      closeDisabled={generating || submitting}
      maxWidthClass="max-w-[640px]"
      maxHeightClass="max-h-[90vh]"
      title="新建书籍"
      headerExtra={
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
      }
    >
      {mode === "ai" ? (
        plan ? (
          /* AI Plan Review & Tweak */
          <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
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
                    className="w-full rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] font-medium text-ink-primary focus:border-accent"
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
                      className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas py-2 pr-8 pl-3 text-[13px] text-ink-primary focus:border-accent"
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
                  className="w-full resize-none rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] leading-relaxed text-ink-primary focus:border-accent"
                />
              </div>

              <div>
                <label className="mb-1 block text-xs font-medium text-ink-secondary">
                  核心设定 / 主角依仗
                </label>
                <textarea
                  rows={2}
                  value={plan.goldenFinger}
                  onChange={(e) => setPlan({ ...plan, goldenFinger: e.target.value })}
                  className="w-full resize-none rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] leading-relaxed text-ink-primary focus:border-accent"
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
                  className="mb-2 w-full rounded-[6px] border border-border-subtle bg-surface px-2.5 py-1.5 text-[13px] font-medium text-ink-primary focus:border-accent"
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
                              ? "border-accent bg-accent text-canvas"
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
                                ? "border-accent bg-accent text-canvas"
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
                  onClick={() => void handleApplyPlan("architect")}
                  className="rounded-lg border border-border-strong bg-surface px-3.5 py-1.5 text-[13px] font-medium text-ink-primary transition-colors hover:border-accent/50 disabled:opacity-50"
                >
                  建书并看大纲
                </button>
                <button
                  type="button"
                  disabled={submitting}
                  onClick={() => void handleApplyPlan("studio")}
                  className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-1.5 text-[13px] font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
                >
                  {submitting && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                  <span>{submitting ? "创建中…" : "建书并开始写作"}</span>
                </button>
              </div>
            </div>
          </div>
        ) : (
          /* AI Idea Input Step */
          <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
            <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
              {!hasReadyModel && (
                <div className="flex items-center justify-between gap-3 rounded-lg border border-warn/35 bg-warn-soft/70 px-3.5 py-2.5 text-xs text-warn">
                  <span>需先连接模型才能 AI 生成企划（当前填写的灵感会自动保留）。</span>
                  <button
                    type="button"
                    onClick={handleGoConfigureModel}
                    className="shrink-0 font-semibold underline underline-offset-2"
                  >
                    去连接模型
                  </button>
                </div>
              )}

              <div>
                <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                  写下你的故事灵感
                </label>
                <textarea
                  rows={3}
                  autoFocus
                  disabled={generating}
                  value={idea}
                  onChange={(e) => {
                    setIdea(e.target.value);
                    savedIdeaRef.current = e.target.value;
                  }}
                  placeholder="例如：落魄外门弟子捡到能回溯三息的残剑，在宗门大比一路越阶反杀。"
                  className="w-full resize-none rounded-lg border border-border-subtle bg-canvas px-3 py-2.5 text-[13px] leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent disabled:opacity-60"
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
                        savedIdeaRef.current = ex.text;
                        setGenre(ex.genre);
                      }}
                      className="rounded-[6px] border border-border-subtle bg-canvas px-2 py-0.5 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary disabled:opacity-50"
                    >
                      {ex.label}
                    </button>
                  ))}
                </div>
              </div>

              <div>
                <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                  题材风格
                </label>
                <GenrePresetGrid
                  presets={genrePresets}
                  selectedId={genre}
                  disabled={generating}
                  onSelect={setGenre}
                />
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
                      className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas py-2 pr-8 pl-3 text-[13px] text-ink-primary focus:border-accent disabled:opacity-60"
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
                  onClick={() => stopGenesisStream()}
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
                    className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-1.5 text-[13px] font-semibold text-canvas transition-opacity hover:opacity-90"
                  >
                    <Sparkles className="h-3.5 w-3.5" />
                    <span>{hasReadyModel ? "生成开书企划" : "去连接模型"}</span>
                  </button>
                </>
              )}
            </div>
          </div>
        )
      ) : (
        /* Manual Create Form */
        <form onSubmit={handleManualSubmit} className="flex min-h-0 flex-1 flex-col overflow-hidden">
          <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
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
                className="w-full rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] text-ink-primary placeholder:text-ink-muted focus:border-accent"
              />
            </div>

            <div>
              <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                题材风格
              </label>
              <GenrePresetGrid
                presets={genrePresets}
                selectedId={genre}
                onSelect={setGenre}
              />
            </div>

            <div>
              <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                故事简介（选填）
              </label>
              <textarea
                rows={2}
                value={premise}
                onChange={(e) => setPremise(e.target.value)}
                placeholder="主角开局处境、核心矛盾与目标，可稍后补充"
                className="w-full resize-none rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent"
              />
            </div>

            <div>
              <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                核心设定 / 主角依仗（选填）
              </label>
              <textarea
                rows={2}
                value={goldenFinger}
                onChange={(e) => setGoldenFinger(e.target.value)}
                placeholder="主角的特殊能力、触发条件与限制"
                className="w-full resize-none rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent"
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
                    className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas py-2 pr-8 pl-3 text-[13px] text-ink-primary focus:border-accent"
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
                    className="min-w-0 flex-1 rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] text-ink-primary placeholder:text-ink-muted focus:border-accent"
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
              className="rounded-lg bg-accent px-4 py-1.5 text-[13px] font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
            >
              {submitting ? "创建中…" : "建书并开始写作"}
            </button>
          </div>
        </form>
      )}
    </ModalShell>
  );
}
