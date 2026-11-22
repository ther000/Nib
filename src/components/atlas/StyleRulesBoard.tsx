import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ChevronDown,
  ChevronRight,
  FileText,
  Loader2,
  Pencil,
  Plus,
  Save,
  ShieldBan,
  Sliders,
  Sparkles,
  Square,
  Trash2,
  Wand2,
} from "lucide-react";
import {
  StyleService,
  type DeaiCategory,
  type DeaiRule,
  type LintSeverity,
  type PovRule,
  type SceneType,
  type StyleProfile,
  type StyleSample,
} from "../../mygo";
import { useApp } from "../../stores/AppContext";
import {
  DEAI_CATEGORIES,
  LINT_SEVERITIES,
  POV_RULES,
  SCENE_TYPES,
  formatErrorMessage,
  formatNumber,
  getDeaiCategoryLabel,
  getLintSeverityMeta,
  getSceneTypeLabel,
} from "../../lib/constants";
import { parseSensitiveImportLines } from "../../lib/studioHelpers";
import { ModalShell } from "../ui/ModalShell";

interface SampleEditorState {
  id: number;
  sceneType: SceneType;
  title: string;
  excerpt: string;
}

interface RuleEditorState {
  id: number;
  bookId: number;
  category: DeaiCategory;
  pattern: string;
  isRegex: boolean;
  severity: LintSeverity;
  maxPerChapter: string;
  hint: string;
  replacement: string;
  enabled: boolean;
}

export function StyleRulesBoard() {
  const { activeBook, notify } = useApp();

  // Section 1: 文风设定
  const [avgSentence, setAvgSentence] = useState("18");
  const [maxPara, setMaxPara] = useState("140");
  const [dlgMin, setDlgMin] = useState("25");
  const [dlgMax, setDlgMax] = useState("55");
  const [povRule, setPovRule] = useState<PovRule>("third_limited");
  const [toneDirectives, setToneDirectives] = useState("");
  const [forbiddenTropes, setForbiddenTropes] = useState("");
  const [metricsExpanded, setMetricsExpanded] = useState(false);
  const [profileDirty, setProfileDirty] = useState(false);
  const [savingProfile, setSavingProfile] = useState(false);

  // Extract Style DNA modal
  const [extractModalOpen, setExtractModalOpen] = useState(false);
  const [sampleInputs, setSampleInputs] = useState<string[]>([""]);
  const [extractingProfile, setExtractingProfile] = useState(false);
  const extractAbortRef = useRef<AbortController | null>(null);

  // Section 2: Style Samples
  const [sceneFilter, setSceneFilter] = useState<string>("all");
  const [samples, setSamples] = useState<StyleSample[]>([]);
  const [sampleEditor, setSampleEditor] = useState<SampleEditorState | null>(null);
  const [savingSample, setSavingSample] = useState(false);
  const [confirmDeleteSampleId, setConfirmDeleteSampleId] = useState<number | null>(null);

  // Section 3: De-AI Rules & Sensitive Word Bulk Import
  const [catFilter, setCatFilter] = useState<string>("all");
  const [rules, setRules] = useState<DeaiRule[]>([]);
  const [ruleEditor, setRuleEditor] = useState<RuleEditorState | null>(null);
  const [savingRule, setSavingRule] = useState(false);
  const [confirmDeleteRuleId, setConfirmDeleteRuleId] = useState<number | null>(null);
  const [sensitiveModalOpen, setSensitiveModalOpen] = useState(false);
  const [sensitiveText, setSensitiveText] = useState("");
  const [importingSensitive, setImportingSensitive] = useState(false);

  const syncProfileForm = useCallback((p: StyleProfile) => {
    setAvgSentence(String(Math.round((p.avgSentenceChars || 18) * 10) / 10));
    setMaxPara(String(p.maxParagraphChars || 140));
    setDlgMin(String(Math.round((p.dialogueRatioMin ?? 0.25) * 100)));
    setDlgMax(String(Math.round((p.dialogueRatioMax ?? 0.55) * 100)));
    setPovRule(p.povRule || "third_limited");
    setToneDirectives(p.toneDirectives || "");
    setForbiddenTropes(p.forbiddenTropesNote || "");
    setProfileDirty(false);
  }, []);

  const loadProfileAndRules = useCallback(async () => {
    if (!activeBook) return;
    try {
      const [prof, ruleList] = await Promise.all([
        StyleService.getStyleProfile(activeBook.id),
        StyleService.listDeaiRules(activeBook.id),
      ]);
      syncProfileForm(prof);
      setRules(ruleList);
    } catch (err) {
      notify({
        tone: "danger",
        message: "加载文风与去 AI 味规则失败",
        detail: formatErrorMessage(err),
      });
    }
  }, [activeBook, syncProfileForm, notify]);

  const loadSamples = useCallback(async () => {
    if (!activeBook) return;
    try {
      const list = await StyleService.listStyleSamples(
        activeBook.id,
        sceneFilter === "all" ? "" : sceneFilter
      );
      setSamples(list);
    } catch (err) {
      notify({ tone: "danger", message: "加载样章失败", detail: formatErrorMessage(err) });
    }
  }, [activeBook, sceneFilter, notify]);

  const loadRules = useCallback(async () => {
    if (!activeBook) return;
    try {
      const list = await StyleService.listDeaiRules(activeBook.id);
      setRules(list);
    } catch (err) {
      notify({ tone: "danger", message: "加载去 AI 味规则失败", detail: formatErrorMessage(err) });
    }
  }, [activeBook, notify]);

  useEffect(() => {
    void loadProfileAndRules();
  }, [loadProfileAndRules]);

  useEffect(() => {
    void loadSamples();
  }, [loadSamples]);

  useEffect(() => {
    return () => {
      if (extractAbortRef.current) {
        extractAbortRef.current.abort();
        extractAbortRef.current = null;
      }
    };
  }, []);

  const handleSaveProfile = async () => {
    if (!activeBook) return;
    const avg = Number.parseFloat(avgSentence) || 18;
    const maxP = Number.parseInt(maxPara, 10) || 140;
    const dMin = Math.min(100, Math.max(0, Number.parseFloat(dlgMin) || 25)) / 100;
    const dMax = Math.min(100, Math.max(dMin * 100, Number.parseFloat(dlgMax) || 55)) / 100;

    setSavingProfile(true);
    try {
      const saved = await StyleService.saveStyleProfile({
        bookId: activeBook.id,
        avgSentenceChars: avg,
        maxParagraphChars: maxP,
        dialogueRatioMin: dMin,
        dialogueRatioMax: dMax,
        povRule,
        toneDirectives: toneDirectives.trim(),
        forbiddenTropesNote: forbiddenTropes.trim(),
      });
      syncProfileForm(saved);
      notify({ tone: "ok", message: "已保存本书文风设定" });
    } catch (err) {
      notify({ tone: "danger", message: "保存文风设定失败", detail: formatErrorMessage(err) });
    } finally {
      setSavingProfile(false);
    }
  };

  const handleExtractProfile = async () => {
    if (!activeBook || extractingProfile) return;
    const texts = sampleInputs.map((s) => s.trim()).filter(Boolean);
    if (texts.length === 0) {
      notify({ tone: "warn", message: "请至少粘贴一段样章正文" });
      return;
    }
    const ac = new AbortController();
    extractAbortRef.current = ac;
    setExtractingProfile(true);
    try {
      const extracted = await StyleService.extractStyleProfile.with({
        signal: ac.signal,
      })(activeBook.id, texts);
      syncProfileForm(extracted);
      await loadSamples();
      setExtractModalOpen(false);
      notify({
        tone: "ok",
        message: "已从样章提取文风与示范片段",
        detail: `平均句长 ${Math.round(extracted.avgSentenceChars)} 字 · 单段上限 ${extracted.maxParagraphChars} 字`,
      });
    } catch (err) {
      if (ac.signal.aborted) {
        notify({ tone: "info", message: "已取消文风提取" });
        return;
      }
      notify({ tone: "danger", message: "提取文风失败", detail: formatErrorMessage(err) });
    } finally {
      if (extractAbortRef.current === ac) extractAbortRef.current = null;
      setExtractingProfile(false);
    }
  };

  // Style Sample CRUD
  const handleSaveSample = async () => {
    if (!activeBook || !sampleEditor) return;
    if (!sampleEditor.title.trim() || !sampleEditor.excerpt.trim()) {
      notify({ tone: "warn", message: "样章标题与正文片段不能为空" });
      return;
    }
    setSavingSample(true);
    try {
      await StyleService.upsertStyleSample({
        id: sampleEditor.id,
        bookId: activeBook.id,
        sceneType: sampleEditor.sceneType,
        title: sampleEditor.title.trim(),
        excerpt: sampleEditor.excerpt.trim(),
      });
      await loadSamples();
      setSampleEditor(null);
      notify({ tone: "ok", message: "分场景样章锚点已保存" });
    } catch (err) {
      notify({ tone: "danger", message: "保存样章失败", detail: formatErrorMessage(err) });
    } finally {
      setSavingSample(false);
    }
  };

  const handleDeleteSample = async (id: number) => {
    try {
      await StyleService.deleteStyleSample(id);
      setConfirmDeleteSampleId(null);
      await loadSamples();
      notify({ tone: "ok", message: "已删除样章片段" });
    } catch (err) {
      notify({ tone: "danger", message: "删除样章失败", detail: formatErrorMessage(err) });
    }
  };

  // De-AI Rule CRUD & 1-click toggle
  const handleToggleRule = async (rule: DeaiRule) => {
    if (!activeBook) return;
    try {
      await StyleService.upsertDeaiRule({
        id: rule.id,
        bookId: rule.bookId,
        category: rule.category,
        pattern: rule.pattern,
        isRegex: rule.isRegex,
        severity: rule.severity,
        maxPerChapter: rule.maxPerChapter,
        hint: rule.hint,
        replacement: rule.replacement ?? "",
        enabled: !rule.enabled,
      });
      await loadRules();
    } catch (err) {
      notify({ tone: "danger", message: "切换规则状态失败", detail: formatErrorMessage(err) });
    }
  };

  const handleSaveRule = async () => {
    if (!activeBook || !ruleEditor) return;
    if (!ruleEditor.pattern.trim()) {
      notify({ tone: "warn", message: "匹配词条或正则不能为空" });
      return;
    }
    const maxPer = Math.max(0, Number.parseInt(ruleEditor.maxPerChapter, 10) || 0);
    setSavingRule(true);
    try {
      await StyleService.upsertDeaiRule({
        id: ruleEditor.id,
        bookId: ruleEditor.id > 0 ? ruleEditor.bookId : activeBook.id,
        category: ruleEditor.category,
        pattern: ruleEditor.pattern.trim(),
        isRegex: ruleEditor.isRegex,
        severity: ruleEditor.severity,
        maxPerChapter: maxPer,
        hint: ruleEditor.hint.trim(),
        replacement: ruleEditor.replacement.trim(),
        enabled: ruleEditor.enabled,
      });
      await loadRules();
      setRuleEditor(null);
      notify({ tone: "ok", message: "规则已保存" });
    } catch (err) {
      notify({ tone: "danger", message: "保存规则失败", detail: formatErrorMessage(err) });
    } finally {
      setSavingRule(false);
    }
  };

  const handleDeleteRule = async (id: number) => {
    try {
      await StyleService.deleteDeaiRule(id);
      setConfirmDeleteRuleId(null);
      await loadRules();
      notify({ tone: "ok", message: "已删除自定义规则" });
    } catch (err) {
      notify({ tone: "danger", message: "删除规则失败", detail: formatErrorMessage(err) });
    }
  };

  const handleImportSensitiveWords = async () => {
    if (!activeBook || importingSensitive) return;
    const entries = parseSensitiveImportLines(sensitiveText);
    const existing = new Set(rules.map((r) => r.pattern.trim()));
    const toAdd = entries.filter((e) => !existing.has(e.pattern));
    if (toAdd.length === 0) {
      notify({ tone: "warn", message: "没有需要新增的敏感词或禁词（可能均已存在）" });
      return;
    }
    setImportingSensitive(true);
    try {
      let added = 0;
      for (const entry of toAdd) {
        await StyleService.upsertDeaiRule({
          id: 0,
          bookId: activeBook.id,
          category: "sensitive",
          pattern: entry.pattern,
          isRegex: false,
          severity: "error",
          maxPerChapter: 0,
          hint: entry.replacement
            ? `敏感/违禁词：建议一键替换为「${entry.replacement}」`
            : "发文敏感/违禁词：请替换或删改后再发布",
          replacement: entry.replacement,
          enabled: true,
        });
        added++;
      }
      await loadRules();
      setSensitiveModalOpen(false);
      setSensitiveText("");
      setCatFilter("sensitive");
      notify({
        tone: "ok",
        message: `已批量导入 ${added} 条敏感/违禁词规则`,
        detail: "写作台本地扫描将 0 成本实时标红告警并支持一键替换",
      });
    } catch (err) {
      notify({ tone: "danger", message: "批量导入敏感词失败", detail: formatErrorMessage(err) });
    } finally {
      setImportingSensitive(false);
    }
  };

  const filteredRules = useMemo(() => {
    if (catFilter === "all") return rules;
    return rules.filter((r) => r.category === catFilter);
  }, [rules, catFilter]);

  return (
    <div className="flex-1 overflow-y-auto px-6 py-5">
      <div className="mx-auto flex w-full max-w-[1160px] flex-col gap-6 pb-10">
        {/* 1. 文风设定 */}
        <section className="rounded-xl border border-border-subtle bg-surface p-5">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border-subtle pb-3.5">
            <div className="flex flex-wrap items-center gap-2">
              <Sliders className="h-4 w-4 text-accent" />
              <h2 className="text-sm font-medium text-ink-primary">文风设定</h2>
              {profileDirty && (
                <span className="rounded bg-warn-soft px-1.5 py-0.5 text-[11px] font-medium text-warn">
                  未保存
                </span>
              )}
            </div>

            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => {
                  setSampleInputs([""]);
                  setExtractModalOpen(true);
                }}
                className="flex items-center gap-1.5 rounded-md border border-border-subtle bg-elevated px-3 py-1.5 text-xs font-medium text-ink-primary transition-colors hover:border-accent hover:text-accent"
              >
                <Wand2 className="h-3.5 w-3.5 text-accent" />
                <span>从样章提取文风</span>
              </button>

              <button
                type="button"
                disabled={savingProfile || !profileDirty}
                onClick={() => void handleSaveProfile()}
                className="flex items-center gap-1.5 rounded-md bg-accent px-3.5 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
              >
                <Save className="h-3.5 w-3.5" />
                <span>{savingProfile ? "保存中…" : "保存文风"}</span>
              </button>
            </div>
          </div>

          <div className="mt-4 grid grid-cols-1 gap-3.5 md:grid-cols-2">
            <div>
              <label className="mb-1 block text-xs text-ink-secondary">
                叙述语感与文风要求
              </label>
              <textarea
                rows={2}
                value={toneDirectives}
                onChange={(e) => {
                  setToneDirectives(e.target.value);
                  setProfileDirty(true);
                }}
                placeholder="例如：冷峻克制、少形容词堆砌、动作与利益交锋先于心理活动"
                className="w-full resize-y rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
              />
            </div>

            <div>
              <label className="mb-1 block text-xs text-ink-secondary">
                本书专属避坑说明
              </label>
              <textarea
                rows={2}
                value={forbiddenTropes}
                onChange={(e) => {
                  setForbiddenTropes(e.target.value);
                  setProfileDirty(true);
                }}
                placeholder="例如：主角行事果决不留活口，严禁无脑饶恕死敌或临阵嘴炮"
                className="w-full resize-y rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
              />
            </div>
          </div>

          {/* Collapsible Numeric Style Metrics */}
          <div className="mt-3.5 border-t border-border-subtle pt-3">
            <button
              type="button"
              onClick={() => setMetricsExpanded((v) => !v)}
              className="flex flex-wrap items-center gap-2 text-xs text-ink-secondary hover:text-ink-primary"
            >
              {metricsExpanded ? (
                <ChevronDown className="h-3.5 w-3.5 text-ink-muted" />
              ) : (
                <ChevronRight className="h-3.5 w-3.5 text-ink-muted" />
              )}
              <span className="font-medium">量化指标与视角详情</span>
              <span className="nib-mono rounded bg-canvas px-2 py-0.5 text-[11px] text-ink-muted">
                {POV_RULES.find((p) => p.value === povRule)?.label ?? povRule} · 句长{" "}
                {Math.round(Number(avgSentence) || 18)} 字 · 单段 &le;{" "}
                {Number(maxPara) || 140} 字 · 对话 {dlgMin}%~{dlgMax}%
              </span>
            </button>

            {metricsExpanded && (
              <div className="mt-3 grid grid-cols-1 gap-3.5 sm:grid-cols-2 lg:grid-cols-4">
                <div>
                  <label className="mb-1 block text-xs text-ink-secondary">
                    平均句长（字）
                  </label>
                  <input
                    type="number"
                    min={5}
                    max={120}
                    step={0.5}
                    value={avgSentence}
                    onChange={(e) => {
                      setAvgSentence(e.target.value);
                      setProfileDirty(true);
                    }}
                    className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                  />
                </div>

                <div>
                  <label className="mb-1 block text-xs text-ink-secondary">
                    单段字数上限
                  </label>
                  <input
                    type="number"
                    min={40}
                    max={1000}
                    step={10}
                    value={maxPara}
                    onChange={(e) => {
                      setMaxPara(e.target.value);
                      setProfileDirty(true);
                    }}
                    className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                  />
                </div>

                <div>
                  <label className="mb-1 block text-xs text-ink-secondary">
                    对话占比区间（%）
                  </label>
                  <div className="flex items-center gap-1.5">
                    <input
                      type="number"
                      min={0}
                      max={100}
                      value={dlgMin}
                      onChange={(e) => {
                        setDlgMin(e.target.value);
                        setProfileDirty(true);
                      }}
                      className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-2 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                    />
                    <span className="text-xs text-ink-muted">~</span>
                    <input
                      type="number"
                      min={0}
                      max={100}
                      value={dlgMax}
                      onChange={(e) => {
                        setDlgMax(e.target.value);
                        setProfileDirty(true);
                      }}
                      className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-2 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                    />
                  </div>
                </div>

                <div>
                  <label className="mb-1 block text-xs text-ink-secondary">
                    叙事视角
                  </label>
                  <select
                    value={povRule}
                    onChange={(e) => {
                      setPovRule(e.target.value);
                      setProfileDirty(true);
                    }}
                    className="w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                  >
                    {POV_RULES.map((p) => (
                      <option key={p.value} value={p.value}>
                        {p.label}
                      </option>
                    ))}
                  </select>
                </div>
              </div>
            )}
          </div>
        </section>

        {/* 2. 示范样章库 (styleSamples) */}
        <section className="rounded-xl border border-border-subtle bg-surface p-5">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex items-center gap-2">
              <FileText className="h-4 w-4 text-accent" />
              <h2 className="text-sm font-medium text-ink-primary">
                示范样章库（写作时按场景自动参考）
              </h2>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <div className="flex items-center gap-1 rounded-lg border border-border-subtle bg-canvas p-0.5">
                <button
                  type="button"
                  onClick={() => setSceneFilter("all")}
                  className={`rounded px-2 py-1 text-xs transition-colors ${
                    sceneFilter === "all"
                      ? "bg-accent-soft font-medium text-accent"
                      : "text-ink-secondary hover:text-ink-primary"
                  }`}
                >
                  全部
                </button>
                {SCENE_TYPES.map((sc) => (
                  <button
                    key={sc.value}
                    type="button"
                    onClick={() => setSceneFilter(sc.value)}
                    className={`rounded px-2 py-1 text-xs transition-colors ${
                      sceneFilter === sc.value
                        ? "bg-accent-soft font-medium text-accent"
                        : "text-ink-secondary hover:text-ink-primary"
                    }`}
                  >
                    {sc.shortLabel ?? sc.label}
                  </button>
                ))}
              </div>

              <button
                type="button"
                onClick={() =>
                  setSampleEditor({
                    id: 0,
                    sceneType: sceneFilter === "all" ? "combat" : sceneFilter,
                    title: "",
                    excerpt: "",
                  })
                }
                className="flex items-center gap-1 rounded-md border border-border-subtle bg-elevated px-2.5 py-1.5 text-xs font-medium text-ink-primary transition-colors hover:border-accent hover:text-accent"
              >
                <Plus className="h-3.5 w-3.5 text-accent" />
                <span>新增样章</span>
              </button>
            </div>
          </div>

          {samples.length === 0 ? (
            <div className="mt-4 rounded-lg border border-dashed border-border-strong bg-canvas/35 py-8 text-center text-xs text-ink-muted">
              当前场景分类下暂无样章锚点。可点击「导入样章提取文风」或手动添加示范段落。
            </div>
          ) : (
            <div className="mt-4 grid grid-cols-1 gap-3 md:grid-cols-2">
              {samples.map((sp) => (
                <div
                  key={sp.id}
                  className="flex flex-col justify-between rounded-lg border border-border-subtle bg-canvas/70 p-3.5"
                >
                  <div>
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex min-w-0 items-center gap-2">
                        <span className="shrink-0 rounded bg-accent-soft px-1.5 py-0.5 text-[10.5px] font-medium text-accent">
                          {getSceneTypeLabel(sp.sceneType)}
                        </span>
                        <span className="truncate text-xs font-medium text-ink-primary">
                          {sp.title}
                        </span>
                      </div>
                      <span className="nib-mono shrink-0 text-[11px] text-ink-muted">
                        {formatNumber(sp.charCount)} 字
                      </span>
                    </div>
                    <p className="mt-2 line-clamp-3 font-prose text-xs leading-relaxed text-ink-secondary whitespace-pre-wrap">
                      {sp.excerpt}
                    </p>
                  </div>

                  <div className="mt-3 flex items-center justify-end gap-2 border-t border-border-subtle pt-2">
                    <button
                      type="button"
                      onClick={() =>
                        setSampleEditor({
                          id: sp.id,
                          sceneType: sp.sceneType,
                          title: sp.title,
                          excerpt: sp.excerpt,
                        })
                      }
                      className="flex items-center gap-1 text-[11px] text-ink-secondary hover:text-ink-primary"
                    >
                      <Pencil className="h-3 w-3" />
                      <span>编辑</span>
                    </button>
                    {confirmDeleteSampleId === sp.id ? (
                      <div className="flex items-center gap-1">
                        <button
                          type="button"
                          onClick={() => void handleDeleteSample(sp.id)}
                          className="rounded bg-danger px-2 py-0.5 text-[11px] font-medium text-canvas"
                        >
                          确认
                        </button>
                        <button
                          type="button"
                          onClick={() => setConfirmDeleteSampleId(null)}
                          className="text-[11px] text-ink-muted"
                        >
                          取消
                        </button>
                      </div>
                    ) : (
                      <button
                        type="button"
                        onClick={() => setConfirmDeleteSampleId(sp.id)}
                        className="flex items-center gap-1 text-[11px] text-ink-muted hover:text-danger"
                      >
                        <Trash2 className="h-3 w-3" />
                        <span>删除</span>
                      </button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </section>

        {/* 3. 禁用词与敏感词 (deaiRules) */}
        <section className="rounded-xl border border-border-subtle bg-surface p-5">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex items-center gap-2">
              <ShieldBan className="h-4 w-4 text-danger" />
              <h2 className="text-sm font-medium text-ink-primary">
                禁用词与敏感词
              </h2>
              <span className="nib-mono text-xs text-ink-muted">
                {rules.filter((r) => r.enabled).length} / {rules.length} 启用
              </span>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <div className="flex items-center gap-1 rounded-lg border border-border-subtle bg-canvas p-0.5">
                <button
                  type="button"
                  onClick={() => setCatFilter("all")}
                  className={`rounded px-2 py-1 text-xs transition-colors ${
                    catFilter === "all"
                      ? "bg-accent-soft font-medium text-accent"
                      : "text-ink-secondary hover:text-ink-primary"
                  }`}
                >
                  全部
                </button>
                {DEAI_CATEGORIES.map((cat) => (
                  <button
                    key={cat.value}
                    type="button"
                    onClick={() => setCatFilter(cat.value)}
                    className={`rounded px-2 py-1 text-xs transition-colors ${
                      catFilter === cat.value
                        ? cat.value === "sensitive"
                          ? "bg-danger-soft font-medium text-danger"
                          : "bg-accent-soft font-medium text-accent"
                        : "text-ink-secondary hover:text-ink-primary"
                    }`}
                  >
                    {cat.label}
                  </button>
                ))}
              </div>

              <button
                type="button"
                onClick={() => setSensitiveModalOpen(true)}
                className="flex items-center gap-1 rounded-md border border-danger/35 bg-danger-soft/60 px-2.5 py-1.5 text-xs font-medium text-danger transition-colors hover:bg-danger-soft"
              >
                <ShieldBan className="h-3.5 w-3.5" />
                <span>批量导敏感词</span>
              </button>

              <button
                type="button"
                onClick={() =>
                  setRuleEditor({
                    id: 0,
                    bookId: activeBook?.id ?? 0,
                    category: catFilter === "all" ? "cliche" : catFilter,
                    pattern: "",
                    isRegex: false,
                    severity: "error",
                    maxPerChapter: "0",
                    hint:
                      catFilter === "sensitive"
                        ? "发文敏感/违禁词：请替换或删改后再发布"
                        : "",
                    replacement: "",
                    enabled: true,
                  })
                }
                className="flex items-center gap-1 rounded-md bg-accent px-3 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90"
              >
                <Plus className="h-3.5 w-3.5" />
                <span>新增禁用词条</span>
              </button>
            </div>
          </div>

          <div className="mt-4 grid grid-cols-1 gap-2.5 md:grid-cols-2">
            {filteredRules.map((rule) => {
              const sevMeta = getLintSeverityMeta(rule.severity);
              const isBuiltin = rule.bookId === 0;
              return (
                <div
                  key={rule.id}
                  className={`flex items-start justify-between gap-3 rounded-lg border border-border-subtle bg-canvas/70 px-3.5 py-2.5 transition-opacity ${
                    rule.enabled ? "" : "opacity-50"
                  }`}
                >
                  <div className="flex min-w-0 flex-1 items-start gap-2.5">
                    <input
                      type="checkbox"
                      checked={rule.enabled}
                      onChange={() => void handleToggleRule(rule)}
                      title={rule.enabled ? "点击禁用此规则" : "点击启用此规则"}
                      className="mt-1 h-3.5 w-3.5 shrink-0 cursor-pointer accent-[var(--accent)]"
                    />
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <span className="nib-mono rounded bg-surface px-1.5 py-0.5 text-xs font-medium text-ink-primary">
                          {rule.pattern}
                        </span>
                        <span
                          className={`rounded border px-1.5 py-0.5 text-[10px] ${sevMeta.badgeClass}`}
                        >
                          {sevMeta.label}
                        </span>
                        <span className="rounded bg-surface px-1.5 py-0.5 text-[10px] text-ink-muted">
                          {getDeaiCategoryLabel(rule.category)}
                        </span>
                        {rule.isRegex && (
                          <span className="nib-mono rounded bg-surface px-1 py-0.5 text-[9.5px] text-accent">
                            Regex
                          </span>
                        )}
                        {rule.replacement && (
                          <span className="rounded bg-ok-soft px-1.5 py-0.5 text-[10px] font-medium text-ok">
                            →「{rule.replacement}」
                          </span>
                        )}
                        <span className="nib-mono text-[10.5px] text-ink-muted">
                          上限 {rule.maxPerChapter}/章
                        </span>
                      </div>
                      {rule.hint && (
                        <div className="mt-1 text-[11px] leading-relaxed text-ink-secondary">
                          {rule.hint}
                        </div>
                      )}
                    </div>
                  </div>

                  <div className="flex shrink-0 items-center gap-1">
                    <button
                      type="button"
                      onClick={() =>
                        setRuleEditor({
                          id: rule.id,
                          bookId: rule.bookId,
                          category: rule.category,
                          pattern: rule.pattern,
                          isRegex: rule.isRegex,
                          severity: rule.severity,
                          maxPerChapter: String(rule.maxPerChapter),
                          hint: rule.hint,
                          replacement: rule.replacement ?? "",
                          enabled: rule.enabled,
                        })
                      }
                      title="编辑规则"
                      className="rounded p-1 text-ink-muted hover:bg-surface hover:text-ink-primary"
                    >
                      <Pencil className="h-3 w-3" />
                    </button>

                    {!isBuiltin &&
                      (confirmDeleteRuleId === rule.id ? (
                        <div className="flex items-center gap-1">
                          <button
                            type="button"
                            onClick={() => void handleDeleteRule(rule.id)}
                            className="rounded bg-danger px-1.5 py-0.5 text-[11px] font-medium text-canvas"
                          >
                            删
                          </button>
                          <button
                            type="button"
                            onClick={() => setConfirmDeleteRuleId(null)}
                            className="text-[10px] text-ink-muted"
                          >
                            ×
                          </button>
                        </div>
                      ) : (
                        <button
                          type="button"
                          onClick={() => setConfirmDeleteRuleId(rule.id)}
                          title="删除自定义规则"
                          className="rounded p-1 text-ink-muted hover:bg-danger-soft hover:text-danger"
                        >
                          <Trash2 className="h-3 w-3" />
                        </button>
                      ))}
                  </div>
                </div>
              );
            })}
          </div>
        </section>
      </div>

      {/* Extract Style Profile Modal */}
      <ModalShell
        open={extractModalOpen}
        onClose={() => setExtractModalOpen(false)}
        closeDisabled={extractingProfile}
        title="从样章提取文风"
        icon={<Sparkles className="h-4 w-4 text-accent" />}
        maxWidthClass="max-w-[600px]"
        footer={
          <>
            {extractingProfile ? (
              <button
                type="button"
                onClick={() => extractAbortRef.current?.abort()}
                className="flex items-center gap-1.5 rounded-md border border-danger/40 bg-danger-soft px-3.5 py-1.5 text-xs font-medium text-danger"
              >
                <Square className="h-3 w-3 fill-current" />
                <span>取消提取</span>
              </button>
            ) : (
              <button
                type="button"
                onClick={() => setExtractModalOpen(false)}
                className="rounded-md px-3 py-1.5 text-xs text-ink-secondary hover:bg-surface hover:text-ink-primary"
              >
                取消
              </button>
            )}
            <button
              type="button"
              disabled={extractingProfile}
              onClick={() => void handleExtractProfile()}
              className="flex items-center gap-1.5 rounded-md bg-accent px-4 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
            >
              {extractingProfile ? (
                <>
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  <span>提取中…</span>
                </>
              ) : (
                <>
                  <Wand2 className="h-3.5 w-3.5" />
                  <span>提取并保存</span>
                </>
              )}
            </button>
          </>
        }
      >
        <div className="flex flex-col gap-3 overflow-y-auto p-5">
          <div className="text-xs text-ink-secondary">
            粘贴 1~5 章满意的样章或标杆片段，自动统计句长、段长、对话占比并切分场景示范片段。
          </div>

          {sampleInputs.map((val, idx) => (
            <div key={idx} className="flex flex-col gap-1">
              <div className="flex items-center justify-between text-xs text-ink-secondary">
                <span>样章 #{idx + 1}</span>
                {sampleInputs.length > 1 && (
                  <button
                    type="button"
                    onClick={() =>
                      setSampleInputs((prev) =>
                        prev.filter((_, i) => i !== idx)
                      )
                    }
                    className="text-xs text-ink-muted hover:text-danger"
                  >
                    移除
                  </button>
                )}
              </div>
              <textarea
                rows={5}
                value={val}
                onChange={(e) =>
                  setSampleInputs((prev) =>
                    prev.map((item, i) => (i === idx ? e.target.value : item))
                  )
                }
                placeholder="在此粘贴样章正文…"
                className="w-full resize-y rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
              />
            </div>
          ))}

          {sampleInputs.length < 5 && (
            <button
              type="button"
              onClick={() => setSampleInputs((prev) => [...prev, ""])}
              className="flex items-center justify-center gap-1 rounded-md border border-dashed border-border-strong py-1.5 text-xs text-ink-secondary hover:border-accent hover:text-accent"
            >
              <Plus className="h-3.5 w-3.5" />
              <span>添加一段样章（{sampleInputs.length}/5）</span>
            </button>
          )}
        </div>
      </ModalShell>

      {/* Style Sample Editor Modal */}
      <ModalShell
        open={Boolean(sampleEditor)}
        onClose={() => setSampleEditor(null)}
        closeDisabled={savingSample}
        title={sampleEditor && sampleEditor.id > 0 ? "编辑示范样章" : "新增示范样章"}
        icon={<FileText className="h-4 w-4 text-accent" />}
        maxWidthClass="max-w-[520px]"
        footer={
          <>
            <button
              type="button"
              disabled={savingSample}
              onClick={() => setSampleEditor(null)}
              className="rounded-md px-3 py-1.5 text-xs text-ink-secondary hover:bg-surface hover:text-ink-primary disabled:opacity-40"
            >
              取消
            </button>
            <button
              type="button"
              disabled={savingSample}
              onClick={() => void handleSaveSample()}
              className="rounded-md bg-accent px-4 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
            >
              {savingSample ? "保存中…" : "保存样章"}
            </button>
          </>
        }
      >
        {sampleEditor && (
          <div className="flex flex-col gap-3.5 p-5">
            <div className="grid grid-cols-[140px_1fr] gap-3">
              <div>
                <label className="mb-1 block text-xs text-ink-secondary">
                  场景类型
                </label>
                <select
                  value={sampleEditor.sceneType}
                  onChange={(e) =>
                    setSampleEditor({ ...sampleEditor, sceneType: e.target.value })
                  }
                  className="w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                >
                  {SCENE_TYPES.map((sc) => (
                    <option key={sc.value} value={sc.value}>
                      {sc.label}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label className="mb-1 block text-xs text-ink-secondary">
                  片段标题
                </label>
                <input
                  type="text"
                  autoFocus
                  value={sampleEditor.title}
                  onChange={(e) =>
                    setSampleEditor({ ...sampleEditor, title: e.target.value })
                  }
                  placeholder="例如：石室短兵相接示范段"
                  className="w-full rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                />
              </div>
            </div>

            <div>
              <label className="mb-1 block text-xs text-ink-secondary">
                示范片段（建议 300~600 字）
              </label>
              <textarea
                rows={6}
                value={sampleEditor.excerpt}
                onChange={(e) =>
                  setSampleEditor({ ...sampleEditor, excerpt: e.target.value })
                }
                placeholder="只示范用词节奏与短段动作感，AI 不会照搬其中剧情"
                className="w-full resize-y rounded-md border border-border-subtle bg-canvas px-3 py-2 font-prose text-xs leading-relaxed text-ink-primary placeholder:font-ui placeholder:text-ink-muted focus:border-accent focus:outline-none"
              />
            </div>
          </div>
        )}
      </ModalShell>

      {/* Bulk Sensitive / Forbidden Words Modal */}
      <ModalShell
        open={sensitiveModalOpen}
        onClose={() => setSensitiveModalOpen(false)}
        closeDisabled={importingSensitive}
        title="批量导入敏感词 / 违禁词"
        icon={<ShieldBan className="h-4 w-4 text-danger" />}
        maxWidthClass="max-w-[480px]"
        footer={
          <>
            <button
              type="button"
              disabled={importingSensitive}
              onClick={() => setSensitiveModalOpen(false)}
              className="rounded-md px-3 py-1.5 text-xs text-ink-secondary hover:bg-surface hover:text-ink-primary disabled:opacity-40"
            >
              取消
            </button>
            <button
              type="button"
              disabled={importingSensitive || !sensitiveText.trim()}
              onClick={() => void handleImportSensitiveWords()}
              className="rounded-md bg-accent px-4 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
            >
              {importingSensitive ? "导入中…" : "确认批量导入"}
            </button>
          </>
        }
      >
        <div className="flex flex-col gap-3 p-5">
          <div className="text-xs leading-relaxed text-ink-secondary">
            每行一个词（支持逗号、顿号分隔），也支持{" "}
            <span className="nib-mono text-ink-primary">敏感词=替换词</span> 或{" "}
            <span className="nib-mono text-ink-primary">敏感词-&gt;替换词</span>{" "}
            指定一键替换词。
          </div>
          <textarea
            rows={7}
            autoFocus
            value={sensitiveText}
            onChange={(e) => setSensitiveText(e.target.value)}
            placeholder={"例如：\n违禁词A=安全词A\n违禁词B -> 安全词B\n角色旧名C"}
            className="w-full resize-y rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
          />
        </div>
      </ModalShell>

      {/* De-AI Rule Editor Modal */}
      <ModalShell
        open={Boolean(ruleEditor)}
        onClose={() => setRuleEditor(null)}
        closeDisabled={savingRule}
        title={ruleEditor && ruleEditor.id > 0 ? "编辑词条规则" : "新增禁用词条"}
        icon={<ShieldBan className="h-4 w-4 text-accent" />}
        maxWidthClass="max-w-[480px]"
        footer={
          <>
            <button
              type="button"
              disabled={savingRule}
              onClick={() => setRuleEditor(null)}
              className="rounded-md px-3 py-1.5 text-xs text-ink-secondary hover:bg-surface hover:text-ink-primary disabled:opacity-40"
            >
              取消
            </button>
            <button
              type="button"
              disabled={savingRule}
              onClick={() => void handleSaveRule()}
              className="rounded-md bg-accent px-4 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
            >
              {savingRule ? "保存中…" : "保存规则"}
            </button>
          </>
        }
      >
        {ruleEditor && (
          <div className="flex flex-col gap-3.5 p-5">
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="mb-1 block text-xs text-ink-secondary">
                  规则分类
                </label>
                <select
                  value={ruleEditor.category}
                  onChange={(e) =>
                    setRuleEditor({ ...ruleEditor, category: e.target.value })
                  }
                  className="w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                >
                  {DEAI_CATEGORIES.map((c) => (
                    <option key={c.value} value={c.value}>
                      {c.label}
                    </option>
                  ))}
                </select>
              </div>

              <div>
                <label className="mb-1 block text-xs text-ink-secondary">
                  提醒级别
                </label>
                <select
                  value={ruleEditor.severity}
                  onChange={(e) =>
                    setRuleEditor({ ...ruleEditor, severity: e.target.value })
                  }
                  className="w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                >
                  {LINT_SEVERITIES.map((s) => (
                    <option key={s.value} value={s.value}>
                      {s.label}
                    </option>
                  ))}
                </select>
              </div>
            </div>

            <div>
              <div className="mb-1 flex items-center justify-between">
                <label className="text-xs text-ink-secondary">匹配词条</label>
                <label
                  title="仅在需要复杂模式匹配时开启"
                  className="flex cursor-pointer items-center gap-1 text-[11px] text-ink-muted hover:text-ink-secondary"
                >
                  <input
                    type="checkbox"
                    checked={ruleEditor.isRegex}
                    onChange={(e) =>
                      setRuleEditor({ ...ruleEditor, isRegex: e.target.checked })
                    }
                    className="h-3 w-3 accent-[var(--accent)]"
                  />
                  <span>高级：正则匹配</span>
                </label>
              </div>
              <input
                type="text"
                autoFocus
                value={ruleEditor.pattern}
                onChange={(e) =>
                  setRuleEditor({ ...ruleEditor, pattern: e.target.value })
                }
                placeholder="例如：嘴角勾起一抹弧度"
                className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
              />
            </div>

            <div className="grid grid-cols-[140px_1fr] gap-3">
              <div>
                <label className="mb-1 block text-xs text-ink-secondary">
                  单章允许上限
                </label>
                <input
                  type="number"
                  min={0}
                  max={50}
                  value={ruleEditor.maxPerChapter}
                  onChange={(e) =>
                    setRuleEditor({ ...ruleEditor, maxPerChapter: e.target.value })
                  }
                  className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                />
              </div>
              <div>
                <label className="mb-1 block text-xs text-ink-secondary">
                  一键替换词（可选）
                </label>
                <input
                  type="text"
                  value={ruleEditor.replacement}
                  onChange={(e) =>
                    setRuleEditor({ ...ruleEditor, replacement: e.target.value })
                  }
                  placeholder="用于敏感词或固定禁用词的一键替换"
                  className="w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                />
              </div>
            </div>

            <div>
              <label className="mb-1 block text-xs text-ink-secondary">
                修改建议提示
              </label>
              <input
                type="text"
                value={ruleEditor.hint}
                onChange={(e) =>
                  setRuleEditor({ ...ruleEditor, hint: e.target.value })
                }
                placeholder="改用具体动作或短句台词"
                className="w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
              />
            </div>
          </div>
        )}
      </ModalShell>
    </div>
  );
}
