import { useEffect, useMemo, useState, type FormEvent } from "react";
import {
  ArrowLeft,
  ChevronDown,
  ChevronRight,
  FilePlus2,
  FolderOpen,
  Loader2,
  Sparkles,
  Square,
  Upload,
  Zap,
} from "lucide-react";
import {
  LibraryService,
  SettingsService,
  type BlueprintChunk,
  type BookBlueprint,
  type BookConcept,
  type BookGenesisPlan,
  type ConceptChunk,
  type GenesisChunk,
  type GenreID,
} from "../mygo";
import { useApp, type CreateBookMode } from "../stores/AppContext";
import { useMygoChannel } from "../hooks/useMygoStream";
import { hasAnyEnabledModel, hasReadyModelRoute } from "../lib/modelReady";
import { formatErrorMessage, formatNumber } from "../lib/constants";
import {
  formatStyleTags,
  GenrePillPicker,
  IDEA_EXAMPLES,
  POV_OPTIONS,
  StepIndicator,
  StyleTagPicker,
  type AIStep,
} from "../components/genesis/GenesisShared";
import { ConceptStep } from "../components/genesis/ConceptStep";
import { BlueprintStep } from "../components/genesis/BlueprintStep";

export function CreateBookView() {
  const {
    createBookInitialMode,
    createBookPrefill,
    clearCreateBookPrefill,
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

  const defaultGenre = genrePresets[0]?.id ?? "xuanhuan";

  const [mode, setMode] = useState<CreateBookMode>(createBookInitialMode);
  const [genre, setGenre] = useState<string>(defaultGenre);
  const [styleTags, setStyleTags] = useState<string[]>([]);
  const [povMode, setPovMode] = useState<string>("third_limited");
  const [targetWords, setTargetWords] = useState<string>("2500");
  const [exportDir, setExportDir] = useState<string>("");
  const [submitting, setSubmitting] = useState(false);

  // Manual mode state
  const [manualTitle, setManualTitle] = useState("");
  const [manualPremise, setManualPremise] = useState("");
  const [manualGoldenFinger, setManualGoldenFinger] = useState("");
  const [manualAdvancedOpen, setManualAdvancedOpen] = useState(false);

  // Import mode state
  const [importingBook, setImportingBook] = useState(false);

  // AI Co-creation state
  const [aiStep, setAiStep] = useState<AIStep>(1);
  const [maxReachedStep, setMaxReachedStep] = useState<AIStep>(1);
  const [idea, setIdea] = useState("");
  const [chapterCount, setChapterCount] = useState<number>(3);
  const [concept, setConcept] = useState<BookConcept | null>(null);
  const [blueprint, setBlueprint] = useState<BookBlueprint | null>(null);
  const [selectedEntityIdx, setSelectedEntityIdx] = useState<Set<number>>(new Set());
  const [selectedOutlineIdx, setSelectedOutlineIdx] = useState<Set<number>>(new Set());

  const [busyStage, setBusyStage] = useState<
    "concept" | "refine" | "blueprint" | "oneshot" | "entities" | "outlines" | null
  >(null);
  const [streamChars, setStreamChars] = useState(0);

  const conceptStream = useMygoChannel<ConceptChunk>();
  const blueprintStream = useMygoChannel<BlueprintChunk>();
  const oneshotStream = useMygoChannel<GenesisChunk>();

  const stopAllStreams = () => {
    conceptStream.stop();
    blueprintStream.stop();
    oneshotStream.stop();
    setBusyStage(null);
  };

  useEffect(() => {
    setMode(createBookInitialMode);
  }, [createBookInitialMode]);

  useEffect(() => {
    if (!createBookPrefill) return;
    if (createBookPrefill.idea) setIdea(createBookPrefill.idea);
    if (createBookPrefill.genre) setGenre(createBookPrefill.genre);
    if (createBookPrefill.styleTags && createBookPrefill.styleTags.length > 0) {
      setStyleTags(createBookPrefill.styleTags.slice(0, 5));
    }
    setAiStep(1);
    clearCreateBookPrefill();
  }, [createBookPrefill, clearCreateBookPrefill]);

  useEffect(() => {
    return () => {
      conceptStream.stop();
      blueprintStream.stop();
      oneshotStream.stop();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const genreName = useMemo(
    () => genrePresets.find((g) => g.id === genre)?.name ?? genre,
    [genrePresets, genre]
  );

  const parsedTargetWords = (): number | null => {
    const n = Number.parseInt(targetWords, 10);
    if (!Number.isFinite(n) || n < 300 || n > 50000) {
      notify({ tone: "warn", message: "每章目标字数需在 300 到 50,000 字之间" });
      return null;
    }
    return n;
  };

  const handleGoConfigureModel = () => {
    const needProvider = !hasAnyEnabledModel(providers);
    openSettings(needProvider ? "providers" : "routes");
  };

  const handlePickExportDir = async () => {
    try {
      const dir = await SettingsService.pickExportDir();
      if (dir) setExportDir(dir);
    } catch (err) {
      notify({
        tone: "danger",
        message: "选择导出目录失败",
        detail: formatErrorMessage(err),
      });
    }
  };

  // Step 1 -> Step 2: Generate Concept
  const handleGenerateConcept = async (feedback = "") => {
    const trimmedIdea = idea.trim();
    if (!trimmedIdea && !concept) {
      notify({ tone: "warn", message: "请先写下你的故事灵感" });
      return;
    }
    if (!hasReadyModel) {
      handleGoConfigureModel();
      return;
    }

    const isRefine = Boolean(feedback.trim() && concept);
    setBusyStage(isRefine ? "refine" : "concept");
    setStreamChars(0);

    const { channel: ch, runId } = conceptStream.open((chunk) => {
      if (!conceptStream.isCurrent(runId)) return;
      if (chunk.kind === "delta" && chunk.delta) {
        setStreamChars((prev) => prev + chunk.delta!.length);
      } else if (chunk.kind === "completed" && chunk.concept) {
        setConcept(chunk.concept);
      }
    });

    try {
      const generated = await LibraryService.generateBookConcept(
        {
          idea: trimmedIdea,
          genre: genre as GenreID,
          povMode,
          pacingStyle: formatStyleTags(styleTags),
          feedback: feedback.trim(),
          currentConcept: isRefine && concept ? concept : undefined,
        },
        ch
      );
      if (!conceptStream.isCurrent(runId)) return;
      setConcept(generated);
      setAiStep(2);
      setMaxReachedStep((prev) => (prev < 2 ? 2 : prev));
    } catch (err) {
      if (conceptStream.isCurrent(runId) && !conceptStream.wasStopped()) {
        notify({
          tone: "danger",
          message: isRefine ? "微调立意失败" : "生成核心立意失败",
          detail: formatErrorMessage(err),
        });
      }
    } finally {
      conceptStream.release(ch);
      if (conceptStream.isCurrent(runId)) {
        setBusyStage(null);
      }
    }
  };

  // Step 1 -> Step 3 shortcut: One-shot full plan
  const handleOneShotGenerate = async () => {
    const trimmedIdea = idea.trim();
    if (!trimmedIdea) {
      notify({ tone: "warn", message: "请先写下你的故事灵感" });
      return;
    }
    const words = parsedTargetWords();
    if (words === null) return;
    if (!hasReadyModel) {
      handleGoConfigureModel();
      return;
    }

    setBusyStage("oneshot");
    setStreamChars(0);

    const { channel: ch, runId } = oneshotStream.open((chunk) => {
      if (!oneshotStream.isCurrent(runId)) return;
      if (chunk.kind === "delta" && chunk.delta) {
        setStreamChars((prev) => prev + chunk.delta!.length);
      }
    });

    try {
      const plan = await LibraryService.generateBookGenesis(
        {
          idea: trimmedIdea,
          genre: genre as GenreID,
          povMode,
          pacingStyle: formatStyleTags(styleTags),
          chapterCount: chapterCount > 0 ? chapterCount : 3,
          targetChapterWords: words,
        },
        ch
      );
      if (!oneshotStream.isCurrent(runId)) return;
      const derivedConcept: BookConcept = {
        title: plan.title,
        titleCandidates: [plan.title],
        genre: plan.genre,
        povMode: plan.povMode || povMode,
        pacingStyle: plan.pacingStyle || formatStyleTags(styleTags),
        premise: plan.premise,
        goldenFinger: plan.goldenFinger,
        coreConflict: "",
      };
      const derivedBlueprint: BookBlueprint = {
        firstVolume: plan.firstVolume,
        entities: plan.entities,
        initialOutlines: plan.initialOutlines,
      };
      setConcept(derivedConcept);
      setBlueprint(derivedBlueprint);
      setSelectedEntityIdx(new Set(derivedBlueprint.entities.map((_, i) => i)));
      setSelectedOutlineIdx(new Set(derivedBlueprint.initialOutlines.map((_, i) => i)));
      setAiStep(3);
      setMaxReachedStep(3);
    } catch (err) {
      if (oneshotStream.isCurrent(runId) && !oneshotStream.wasStopped()) {
        notify({
          tone: "danger",
          message: "生成开书企划失败",
          detail: formatErrorMessage(err),
        });
      }
    } finally {
      oneshotStream.release(ch);
      if (oneshotStream.isCurrent(runId)) {
        setBusyStage(null);
      }
    }
  };

  // Step 2 -> Step 3 or partial regenerate in Step 3
  const handleGenerateBlueprint = async (section: "all" | "entities" | "outlines") => {
    if (!concept || !concept.title.trim()) {
      notify({ tone: "warn", message: "请先确认书名" });
      return;
    }
    const words = parsedTargetWords();
    if (words === null) return;
    if (!hasReadyModel) {
      handleGoConfigureModel();
      return;
    }

    setBusyStage(section === "all" ? "blueprint" : section);
    setStreamChars(0);

    const { channel: ch, runId } = blueprintStream.open((chunk) => {
      if (!blueprintStream.isCurrent(runId)) return;
      if (chunk.kind === "delta" && chunk.delta) {
        setStreamChars((prev) => prev + chunk.delta!.length);
      }
    });

    try {
      const existingEntities =
        section === "outlines" && blueprint
          ? blueprint.entities.filter((_, i) => selectedEntityIdx.has(i))
          : undefined;

      const generated = await LibraryService.generateBookBlueprint(
        {
          concept: {
            ...concept,
            genre: genre as GenreID,
            povMode,
            pacingStyle: concept.pacingStyle || formatStyleTags(styleTags),
          },
          chapterCount: section === "outlines" && chapterCount === 0 ? 3 : chapterCount,
          targetChapterWords: words,
          section,
          existingEntities,
        },
        ch
      );
      if (!blueprintStream.isCurrent(runId)) return;

      if (section === "entities" && blueprint) {
        setBlueprint({ ...blueprint, entities: generated.entities });
        setSelectedEntityIdx(new Set(generated.entities.map((_, i) => i)));
      } else if (section === "outlines" && blueprint) {
        setBlueprint({
          ...blueprint,
          firstVolume: generated.firstVolume.title
            ? generated.firstVolume
            : blueprint.firstVolume,
          initialOutlines: generated.initialOutlines,
        });
        setSelectedOutlineIdx(new Set(generated.initialOutlines.map((_, i) => i)));
      } else {
        setBlueprint(generated);
        setSelectedEntityIdx(new Set(generated.entities.map((_, i) => i)));
        setSelectedOutlineIdx(new Set(generated.initialOutlines.map((_, i) => i)));
        setAiStep(3);
        setMaxReachedStep(3);
      }
    } catch (err) {
      if (blueprintStream.isCurrent(runId) && !blueprintStream.wasStopped()) {
        notify({
          tone: "danger",
          message: "生成开书蓝图失败",
          detail: formatErrorMessage(err),
        });
      }
    } finally {
      blueprintStream.release(ch);
      if (blueprintStream.isCurrent(runId)) {
        setBusyStage(null);
      }
    }
  };

  // Apply AI Genesis plan
  const handleApplyBlueprint = async (targetTab: "studio" | "architect") => {
    if (!concept || !blueprint) return;
    if (!concept.title.trim()) {
      notify({ tone: "warn", message: "请输入书名" });
      return;
    }
    const words = parsedTargetWords();
    if (words === null) return;

    setSubmitting(true);
    try {
      const plan: BookGenesisPlan = {
        title: concept.title.trim(),
        genre: (concept.genre || genre) as GenreID,
        povMode: concept.povMode || povMode,
        pacingStyle: concept.pacingStyle || formatStyleTags(styleTags),
        premise: concept.premise.trim(),
        goldenFinger: concept.goldenFinger.trim(),
        targetChapterWords: words,
        firstVolume: blueprint.firstVolume,
        entities: blueprint.entities.filter((_, i) => selectedEntityIdx.has(i)),
        initialOutlines: blueprint.initialOutlines.filter((_, i) =>
          selectedOutlineIdx.has(i)
        ),
      };
      const result = await LibraryService.applyBookGenesis({
        plan,
        exportDir: exportDir.trim(),
      });
      await refreshBooks(result.book.id);
      await openBook(result.book.id, targetTab);
      closeCreateBook();
      notify({
        tone: "ok",
        message: `已建好《${result.book.title}》`,
        detail: `包含 ${result.entities.length} 条设定与 ${result.chapters.length} 章`,
      });
    } catch (err) {
      notify({
        tone: "danger",
        message: "创建书籍失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setSubmitting(false);
    }
  };

  // Manual quick create
  const handleManualSubmit = async (e: FormEvent) => {
    e.preventDefault();
    const trimmedTitle = manualTitle.trim();
    if (!trimmedTitle) {
      notify({ tone: "warn", message: "请输入书名" });
      return;
    }
    const words = parsedTargetWords();
    if (words === null) return;

    setSubmitting(true);
    try {
      const created = await LibraryService.createBook({
        title: trimmedTitle,
        genre: genre as GenreID,
        povMode,
        pacingStyle: formatStyleTags(styleTags),
        premise: manualPremise.trim(),
        goldenFinger: manualGoldenFinger.trim(),
        targetChapterWords: words,
        exportDir: exportDir.trim(),
      });
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
      notify({
        tone: "danger",
        message: "新建书籍失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setSubmitting(false);
    }
  };

  // Import existing manuscript
  const handleImportBook = async () => {
    if (importingBook) return;
    setImportingBook(true);
    try {
      const res = await LibraryService.importBookFromFile(genre);
      if (!res) return;
      await refreshBooks(res.book.id);
      await openBook(res.book.id, "studio");
      closeCreateBook();
      notify({
        tone: "ok",
        message: `已导入《${res.book.title}》（${res.chapterCount} 章，${formatNumber(res.totalWords)} 字）`,
        actionLabel: "AI 反推设定",
        onAction: () => {
          window.dispatchEvent(
            new CustomEvent("nib:reverse-extract", { detail: { bookId: res.book.id } })
          );
        },
      });
    } catch (err) {
      notify({
        tone: "danger",
        message: "导入旧稿失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setImportingBook(false);
    }
  };

  const toggleIndex = (set: Set<number>, idx: number) => {
    const next = new Set(set);
    if (next.has(idx)) next.delete(idx);
    else next.add(idx);
    return next;
  };

  const handleAddEntity = (): number => {
    if (!blueprint) return -1;
    const newIdx = blueprint.entities.length;
    setBlueprint({
      ...blueprint,
      entities: [
        ...blueprint.entities,
        {
          kind: "character",
          name: "",
          aliases: [],
          isInvariant: false,
          summary: "",
          attributes: {},
        },
      ],
    });
    setSelectedEntityIdx((prev) => new Set(prev).add(newIdx));
    return newIdx;
  };

  const handleDeleteEntity = (idx: number) => {
    if (!blueprint) return;
    setBlueprint({
      ...blueprint,
      entities: blueprint.entities.filter((_, i) => i !== idx),
    });
    setSelectedEntityIdx((prev) => {
      const next = new Set<number>();
      for (const i of prev) {
        if (i < idx) next.add(i);
        else if (i > idx) next.add(i - 1);
      }
      return next;
    });
  };

  const anyBusy = busyStage !== null || submitting || importingBook;

  return (
    <main className="nib-paper-sheet m-2 mt-0 flex flex-1 flex-col overflow-hidden">
      {/* Top Sub-header */}
      <header className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-b border-border-subtle px-6 py-3.5">
        <div className="flex items-center gap-3">
          <button
            type="button"
            disabled={anyBusy}
            onClick={closeCreateBook}
            className="flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-xs font-medium text-ink-secondary transition-colors hover:bg-surface hover:text-ink-primary disabled:opacity-40"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            <span>返回</span>
          </button>
          <span className="h-4 w-px bg-border-subtle" />
          <h1 className="text-sm font-medium text-ink-primary">新建书籍</h1>
        </div>

        {/* 3 Entry Modes Segmented Control */}
        <div className="flex items-center rounded-xl border border-border-subtle bg-surface p-0.5">
          {(
            [
              { id: "ai", label: "AI 协同开书", icon: Sparkles },
              { id: "manual", label: "空白开书", icon: FilePlus2 },
              { id: "import", label: "导入旧稿", icon: Upload },
            ] as const
          ).map((item) => {
            const Icon = item.icon;
            const active = mode === item.id;
            return (
              <button
                key={item.id}
                type="button"
                disabled={anyBusy}
                onClick={() => setMode(item.id)}
                className={`flex items-center gap-1.5 rounded-[10px] px-3 py-1.5 text-xs font-medium transition-colors disabled:opacity-50 ${
                  active
                    ? "bg-elevated text-ink-primary shadow-xs"
                    : "text-ink-muted hover:text-ink-primary"
                }`}
              >
                <Icon
                  className={`h-3.5 w-3.5 ${active && item.id === "ai" ? "text-accent" : ""}`}
                />
                <span>{item.label}</span>
              </button>
            );
          })}
        </div>

        <div className="hidden w-20 sm:block" />
      </header>

      {/* Main Content Canvas */}
      <div className="flex-1 overflow-y-auto px-6 py-8">
        <div className="mx-auto flex w-full max-w-[820px] flex-col gap-6">
          {mode === "ai" && (
            <>
              <div className="flex flex-wrap items-center justify-between gap-3">
                <StepIndicator
                  current={aiStep}
                  maxReached={maxReachedStep}
                  disabled={anyBusy}
                  onNavigate={(step) => setAiStep(step)}
                />
                {!hasReadyModel && (
                  <button
                    type="button"
                    onClick={handleGoConfigureModel}
                    className="rounded-lg border border-warn/40 bg-warn-soft/70 px-3 py-1 text-xs font-medium text-warn transition-colors hover:bg-warn-soft"
                  >
                    未连接模型 · 去配置
                  </button>
                )}
              </div>

              {aiStep === 1 && (
                <div className="flex flex-col gap-6">
                  {/* Idea input */}
                  <section className="flex flex-col gap-2.5">
                    <label className="text-xs font-medium text-ink-secondary">
                      故事灵感
                    </label>
                    <textarea
                      rows={4}
                      autoFocus
                      disabled={anyBusy}
                      value={idea}
                      onChange={(e) => setIdea(e.target.value)}
                      placeholder="写下你的核心脑洞、主角开局或金手指设定，例如：外门杂役捡到能回溯三息的残剑，借时间差一路越阶反杀。"
                      className="w-full resize-none rounded-xl border border-border-subtle bg-surface px-4 py-3.5 text-sm leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent disabled:opacity-60"
                    />
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="text-xs text-ink-muted">灵感示例：</span>
                      {IDEA_EXAMPLES.map((ex) => (
                        <button
                          key={ex.label}
                          type="button"
                          disabled={anyBusy}
                          onClick={() => {
                            setIdea(ex.text);
                            setGenre(ex.genre);
                            setStyleTags(ex.tags);
                          }}
                          className="rounded-md border border-border-subtle bg-surface px-2.5 py-1 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary disabled:opacity-50"
                        >
                          {ex.label}
                        </button>
                      ))}
                    </div>
                  </section>

                  {/* Genre Base */}
                  <section className="flex flex-col gap-2">
                    <label className="text-xs font-medium text-ink-secondary">
                      题材基底
                    </label>
                    <GenrePillPicker
                      presets={genrePresets}
                      selectedId={genre}
                      disabled={anyBusy}
                      onSelect={setGenre}
                    />
                  </section>

                  {/* Style Tags */}
                  <section className="flex flex-col gap-2">
                    <div className="flex items-center justify-between">
                      <label className="text-xs font-medium text-ink-secondary">
                        风格标签（选填）
                      </label>
                      {styleTags.length > 0 && (
                        <button
                          type="button"
                          disabled={anyBusy}
                          onClick={() => setStyleTags([])}
                          className="text-xs text-ink-muted transition-colors hover:text-ink-primary"
                        >
                          清空
                        </button>
                      )}
                    </div>
                    <StyleTagPicker
                      tags={styleTags}
                      disabled={anyBusy}
                      onChange={setStyleTags}
                    />
                  </section>

                  {/* POV + Chapter Count + Target Words */}
                  <section className="grid grid-cols-1 gap-4 sm:grid-cols-3">
                    <div className="flex flex-col gap-2">
                      <label className="text-xs font-medium text-ink-secondary">
                        叙事视角
                      </label>
                      <select
                        disabled={anyBusy}
                        value={povMode}
                        onChange={(e) => setPovMode(e.target.value)}
                        className="rounded-xl border border-border-subtle bg-surface px-3 py-2.5 text-[13px] text-ink-primary focus:border-accent disabled:opacity-60"
                      >
                        {POV_OPTIONS.map((opt) => (
                          <option key={opt.value} value={opt.value}>
                            {opt.label}
                          </option>
                        ))}
                      </select>
                    </div>

                    <div className="flex flex-col gap-2">
                      <label className="text-xs font-medium text-ink-secondary">
                        初始章纲
                      </label>
                      <div className="flex gap-1.5">
                        {[
                          { value: 0, label: "暂不生成" },
                          { value: 3, label: "前 3 章" },
                          { value: 5, label: "前 5 章" },
                        ].map((opt) => (
                          <button
                            key={opt.value}
                            type="button"
                            disabled={anyBusy}
                            onClick={() => setChapterCount(opt.value)}
                            className={`flex-1 rounded-xl border py-2.5 text-xs font-medium transition-colors disabled:opacity-50 ${
                              chapterCount === opt.value
                                ? "border-accent bg-accent-soft text-accent"
                                : "border-border-subtle bg-surface text-ink-secondary hover:border-border-strong"
                            }`}
                          >
                            {opt.label}
                          </button>
                        ))}
                      </div>
                    </div>

                    <div className="flex flex-col gap-2">
                      <label className="text-xs font-medium text-ink-secondary">
                        每章目标字数
                      </label>
                      <div className="relative">
                        <input
                          type="number"
                          min={300}
                          max={50000}
                          step={100}
                          disabled={anyBusy}
                          value={targetWords}
                          onChange={(e) => setTargetWords(e.target.value)}
                          className="nib-mono w-full rounded-xl border border-border-subtle bg-surface py-2.5 pr-9 pl-3.5 text-[13px] text-ink-primary focus:border-accent disabled:opacity-60"
                        />
                        <span className="pointer-events-none absolute top-1/2 right-3.5 -translate-y-1/2 text-xs text-ink-muted">
                          字
                        </span>
                      </div>
                    </div>
                  </section>

                  {/* Streaming Progress */}
                  {(busyStage === "concept" || busyStage === "oneshot") && (
                    <div className="flex items-center justify-between rounded-xl border border-border-subtle bg-surface px-4 py-3">
                      <div className="flex items-center gap-2.5 text-xs text-ink-primary">
                        <Loader2 className="h-4 w-4 animate-spin text-accent" />
                        <span>
                          {busyStage === "concept"
                            ? "正在构思备选书名、核心立意、金手指与主线冲突…"
                            : "正在一次性生成完整开书企划与初始章纲…"}
                        </span>
                      </div>
                      {streamChars > 0 && (
                        <span className="nib-mono text-xs text-ink-muted">
                          已生成 {streamChars} 字
                        </span>
                      )}
                    </div>
                  )}

                  {/* Step 1 Footer */}
                  <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border-subtle pt-4">
                    <button
                      type="button"
                      disabled={anyBusy}
                      onClick={() => void handleOneShotGenerate()}
                      className="flex items-center gap-1.5 rounded-lg border border-border-subtle bg-surface px-3.5 py-2 text-xs font-medium text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary disabled:opacity-40"
                    >
                      <Zap className="h-3.5 w-3.5" />
                      <span>跳过立意确认，一步直出全套</span>
                    </button>

                    {busyStage === "concept" || busyStage === "oneshot" ? (
                      <button
                        type="button"
                        onClick={stopAllStreams}
                        className="flex items-center gap-1.5 rounded-lg border border-border-strong bg-surface px-4 py-2 text-[13px] font-medium text-ink-primary"
                      >
                        <Square className="h-3.5 w-3.5" />
                        <span>停止生成</span>
                      </button>
                    ) : (
                      <button
                        type="button"
                        onClick={() => void handleGenerateConcept()}
                        className="flex items-center gap-1.5 rounded-lg bg-accent px-5 py-2 text-[13px] font-semibold text-canvas transition-opacity hover:opacity-90"
                      >
                        <Sparkles className="h-3.5 w-3.5" />
                        <span>{hasReadyModel ? "生成核心立意" : "去连接模型"}</span>
                      </button>
                    )}
                  </div>
                </div>
              )}

              {aiStep === 2 && concept && (
                <ConceptStep
                  concept={concept}
                  genreName={genreName}
                  chapterCount={chapterCount}
                  targetWords={targetWords}
                  busy={busyStage === "refine" || busyStage === "blueprint"}
                  busyKind={
                    busyStage === "refine"
                      ? "refine"
                      : busyStage === "blueprint"
                        ? "blueprint"
                        : null
                  }
                  streamChars={streamChars}
                  onChange={setConcept}
                  onBack={() => setAiStep(1)}
                  onRefine={(fb) => void handleGenerateConcept(fb)}
                  onProceed={() => void handleGenerateBlueprint("all")}
                  onStop={stopAllStreams}
                />
              )}

              {aiStep === 3 && concept && blueprint && (
                <BlueprintStep
                  concept={concept}
                  blueprint={blueprint}
                  genreName={genreName}
                  targetWords={Number.parseInt(targetWords, 10) || 2500}
                  exportDir={exportDir}
                  selectedEntityIdx={selectedEntityIdx}
                  selectedOutlineIdx={selectedOutlineIdx}
                  regeneratingSection={
                    busyStage === "entities" || busyStage === "outlines"
                      ? busyStage
                      : null
                  }
                  streamChars={streamChars}
                  submitting={submitting}
                  onChangeBlueprint={setBlueprint}
                  onChangeTargetWords={(w) => setTargetWords(String(w))}
                  onChangeExportDir={setExportDir}
                  onPickExportDir={() => void handlePickExportDir()}
                  onToggleEntity={(idx) =>
                    setSelectedEntityIdx((s) => toggleIndex(s, idx))
                  }
                  onToggleOutline={(idx) =>
                    setSelectedOutlineIdx((s) => toggleIndex(s, idx))
                  }
                  onAddEntity={handleAddEntity}
                  onDeleteEntity={handleDeleteEntity}
                  onRegenerateSection={(sec) => void handleGenerateBlueprint(sec)}
                  onStopRegenerate={stopAllStreams}
                  onBackToConcept={() => setAiStep(2)}
                  onApply={(tab) => void handleApplyBlueprint(tab)}
                />
              )}
            </>
          )}

          {mode === "manual" && (
            <form onSubmit={handleManualSubmit} className="flex flex-col gap-6">
              <section className="flex flex-col gap-2">
                <label className="text-xs font-medium text-ink-secondary">书名</label>
                <input
                  type="text"
                  autoFocus
                  required
                  disabled={submitting}
                  value={manualTitle}
                  onChange={(e) => setManualTitle(e.target.value)}
                  placeholder="例如：太初剑渊"
                  className="w-full rounded-xl border border-border-subtle bg-surface px-4 py-3 font-prose text-lg font-medium text-ink-primary placeholder:text-ink-muted focus:border-accent disabled:opacity-60"
                />
              </section>

              <section className="flex flex-col gap-2">
                <label className="text-xs font-medium text-ink-secondary">
                  题材基底
                </label>
                <GenrePillPicker
                  presets={genrePresets}
                  selectedId={genre}
                  disabled={submitting}
                  onSelect={setGenre}
                />
              </section>

              <section className="flex flex-col gap-2">
                <label className="text-xs font-medium text-ink-secondary">
                  风格标签（选填）
                </label>
                <StyleTagPicker
                  tags={styleTags}
                  disabled={submitting}
                  onChange={setStyleTags}
                />
              </section>

              <section className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div className="flex flex-col gap-2">
                  <label className="text-xs font-medium text-ink-secondary">
                    叙事视角
                  </label>
                  <select
                    disabled={submitting}
                    value={povMode}
                    onChange={(e) => setPovMode(e.target.value)}
                    className="rounded-xl border border-border-subtle bg-surface px-3 py-2.5 text-[13px] text-ink-primary focus:border-accent"
                  >
                    {POV_OPTIONS.map((opt) => (
                      <option key={opt.value} value={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>
                </div>

                <div className="flex flex-col gap-2">
                  <label className="text-xs font-medium text-ink-secondary">
                    每章目标字数
                  </label>
                  <div className="relative">
                    <input
                      type="number"
                      min={300}
                      max={50000}
                      step={100}
                      disabled={submitting}
                      value={targetWords}
                      onChange={(e) => setTargetWords(e.target.value)}
                      className="nib-mono w-full rounded-xl border border-border-subtle bg-surface py-2.5 pr-9 pl-3.5 text-[13px] text-ink-primary focus:border-accent"
                    />
                    <span className="pointer-events-none absolute top-1/2 right-3.5 -translate-y-1/2 text-xs text-ink-muted">
                      字
                    </span>
                  </div>
                </div>
              </section>

              {/* Collapsible optional details */}
              <section className="rounded-xl border border-border-subtle bg-surface/60">
                <button
                  type="button"
                  onClick={() => setManualAdvancedOpen((v) => !v)}
                  className="flex w-full items-center justify-between px-4 py-3 text-xs font-medium text-ink-secondary transition-colors hover:text-ink-primary"
                >
                  <span>简介、金手指与本地备份目录（选填）</span>
                  {manualAdvancedOpen ? (
                    <ChevronDown className="h-4 w-4" />
                  ) : (
                    <ChevronRight className="h-4 w-4" />
                  )}
                </button>

                {manualAdvancedOpen && (
                  <div className="flex flex-col gap-4 border-t border-border-subtle px-4 py-4">
                    <div className="flex flex-col gap-1.5">
                      <label className="text-xs text-ink-secondary">故事简介</label>
                      <textarea
                        rows={2}
                        value={manualPremise}
                        onChange={(e) => setManualPremise(e.target.value)}
                        placeholder="主角开局处境、核心矛盾与目标，可稍后补充"
                        className="w-full resize-none rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent"
                      />
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <label className="text-xs text-ink-secondary">
                        核心设定 / 金手指
                      </label>
                      <textarea
                        rows={2}
                        value={manualGoldenFinger}
                        onChange={(e) => setManualGoldenFinger(e.target.value)}
                        placeholder="主角的特殊能力、触发条件与限制"
                        className="w-full resize-none rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent"
                      />
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <label className="text-xs text-ink-secondary">
                        本地备份文件夹
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
                )}
              </section>

              <div className="flex items-center justify-end gap-2.5 border-t border-border-subtle pt-4">
                <button
                  type="button"
                  disabled={submitting}
                  onClick={closeCreateBook}
                  className="rounded-lg px-4 py-2 text-[13px] font-medium text-ink-secondary transition-colors hover:bg-surface hover:text-ink-primary"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={submitting}
                  className="flex items-center gap-1.5 rounded-lg bg-accent px-5 py-2 text-[13px] font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
                >
                  {submitting && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                  <span>{submitting ? "创建中…" : "建书并开始写作"}</span>
                </button>
              </div>
            </form>
          )}

          {mode === "import" && (
            <div className="flex flex-col gap-6">
              <section className="flex flex-col gap-2">
                <label className="text-xs font-medium text-ink-secondary">
                  为旧稿选择题材基底
                </label>
                <GenrePillPicker
                  presets={genrePresets}
                  selectedId={genre}
                  disabled={importingBook}
                  onSelect={setGenre}
                />
              </section>

              <section className="flex flex-col items-center justify-center gap-4 rounded-2xl border border-dashed border-border-strong bg-surface/60 px-6 py-12 text-center">
                <div className="flex h-11 w-11 items-center justify-center rounded-xl bg-accent-soft text-accent">
                  <Upload className="h-5 w-5" />
                </div>
                <div>
                  <div className="text-sm font-medium text-ink-primary">
                    导入本地 TXT / Markdown 存稿
                  </div>
                  <p className="mt-1.5 max-w-md text-xs leading-relaxed text-ink-secondary">
                    自动按章标题拆分卷章并建书，导入后可一键调用「AI 反推设定」从正文提炼角色档案与世界铁律。
                  </p>
                </div>
                <button
                  type="button"
                  disabled={importingBook}
                  onClick={() => void handleImportBook()}
                  className="flex items-center gap-2 rounded-lg bg-accent px-5 py-2 text-[13px] font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
                >
                  {importingBook ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <Upload className="h-4 w-4" />
                  )}
                  <span>{importingBook ? "正在拆章导入…" : "选择本地文件"}</span>
                </button>
              </section>
            </div>
          )}
        </div>
      </div>
    </main>
  );
}
