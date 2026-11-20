import { useEffect, useMemo, useRef, useState } from "react";
import {
  BarChart3,
  CheckCircle2,
  Eye,
  EyeOff,
  FolderOpen,
  GitBranch,
  KeyRound,
  Loader2,
  Moon,
  Plus,
  RefreshCw,
  Save,
  Search,
  Sliders,
  Sun,
  Trash2,
  Wifi,
  X,
  XCircle,
} from "lucide-react";
import {
  LibraryService,
  SettingsService,
  type ModelRoute,
  type ProviderProtocol,
  type ProviderPublic,
  type ProviderTestResult,
  type UsageSummary,
} from "../mygo";
import { useApp, type SettingsTab } from "../stores/AppContext";
import {
  PIPELINE_STAGES,
  PROVIDER_PROTOCOLS,
  formatErrorMessage,
  formatNumber,
} from "../lib/constants";

interface ProviderFormState {
  id: number;
  name: string;
  protocol: ProviderProtocol;
  baseUrl: string;
  apiKey: string;
  hasApiKey: boolean;
  apiKeyMasked: string;
  enabledModels: string[];
  fetchedModels: string[];
  enabled: boolean;
}

function dedupeModels(list: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const raw of list) {
    const item = raw.trim();
    if (!item || seen.has(item)) continue;
    seen.add(item);
    out.push(item);
  }
  return out;
}

function createBlankProvider(): ProviderFormState {
  const defaultProto = PROVIDER_PROTOCOLS[0];
  return {
    id: 0,
    name: "",
    protocol: defaultProto?.value ?? "openai_compat",
    baseUrl: defaultProto?.defaultBaseUrl ?? "https://api.deepseek.com/v1",
    apiKey: "",
    hasApiKey: false,
    apiKeyMasked: "",
    enabledModels: [],
    fetchedModels: [],
    enabled: true,
  };
}

function providerToForm(p: ProviderPublic): ProviderFormState {
  return {
    id: p.id,
    name: p.name,
    protocol: p.protocol,
    baseUrl: p.baseUrl,
    apiKey: "",
    hasApiKey: p.hasApiKey,
    apiKeyMasked: p.apiKeyMasked,
    enabledModels: dedupeModels(p.models ?? []),
    fetchedModels: dedupeModels(p.fetchedModels ?? []),
    enabled: p.enabled,
  };
}

export function SettingsModal() {
  const {
    settingsOpen,
    settingsTab,
    closeSettings,
    theme,
    setTheme,
    proseFont,
    setProseFont,
    proseIndent,
    setProseIndent,
    providers,
    modelRoutes,
    activeBook,
    refreshSettingsData,
    refreshBooks,
    notify,
  } = useApp();

  const [tab, setTab] = useState<SettingsTab>(settingsTab);

  // Provider editor state
  const [selectedProviderId, setSelectedProviderId] = useState<number | "new">("new");
  const [provForm, setProvForm] = useState<ProviderFormState>(createBlankProvider);
  const [showApiKey, setShowApiKey] = useState(false);
  const [savingProv, setSavingProv] = useState(false);
  const [fetchingModelsId, setFetchingModelsId] = useState<number | null>(null);
  const [syncingModels, setSyncingModels] = useState(false);
  const [modelQuery, setModelQuery] = useState("");
  const [onlyEnabledModels, setOnlyEnabledModels] = useState(false);
  const [customModelInput, setCustomModelInput] = useState("");
  const [confirmDeleteProv, setConfirmDeleteProv] = useState(false);
  const [testingId, setTestingId] = useState<number | null>(null);
  const [testResults, setTestResults] = useState<Record<number, ProviderTestResult>>({});

  // Route editor state
  const [routeDrafts, setRouteDrafts] = useState<Record<string, ModelRoute>>({});
  const [dirtyStages, setDirtyStages] = useState<Set<string>>(new Set());
  const dirtyStagesRef = useRef<Set<string>>(new Set());
  const prevSettingsOpenRef = useRef(false);
  const [savingStage, setSavingStage] = useState<string | null>(null);

  dirtyStagesRef.current = dirtyStages;

  // Workspace / Active Book settings state
  const [bookExportDir, setBookExportDir] = useState("");
  const [bookTargetWords, setBookTargetWords] = useState("2500");
  const [savingBookSettings, setSavingBookSettings] = useState(false);

  // Usage summary state
  const [usageScope, setUsageScope] = useState<"book" | "all">("book");
  const [usageSummary, setUsageSummary] = useState<UsageSummary | null>(null);
  const [usageLoading, setUsageLoading] = useState(false);

  const loadUsageSummary = async (scope: "book" | "all") => {
    const bid = scope === "book" && activeBook ? activeBook.id : 0;
    setUsageLoading(true);
    try {
      const res = await SettingsService.getUsageSummary(bid);
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
  };

  useEffect(() => {
    if (!settingsOpen || tab !== "usage") return;
    const effectiveScope = activeBook ? usageScope : "all";
    void loadUsageSummary(effectiveScope);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settingsOpen, tab, usageScope, activeBook?.id]);

  // Sync tab when modal opens
  useEffect(() => {
    if (!settingsOpen) return;
    setTab(settingsTab);
    setConfirmDeleteProv(false);
    setShowApiKey(false);
  }, [settingsOpen, settingsTab]);

  // Sync provider form when providers list changes (without clobbering unsaved edits on same provider)
  useEffect(() => {
    if (!settingsOpen) return;
    if (providers.length === 0) {
      setSelectedProviderId("new");
      setProvForm(createBlankProvider());
      return;
    }
    if (selectedProviderId === "new") {
      return;
    }
    const found = providers.find((p) => p.id === selectedProviderId);
    if (found) {
      setProvForm((prev) =>
        prev.id === found.id
          ? {
              ...prev,
              hasApiKey: found.hasApiKey,
              apiKeyMasked: found.apiKeyMasked,
              enabledModels: dedupeModels(found.models ?? []),
              fetchedModels: dedupeModels(found.fetchedModels ?? []),
            }
          : providerToForm(found)
      );
    } else if (providers[0]) {
      setSelectedProviderId(providers[0].id);
      setProvForm(providerToForm(providers[0]));
    }
  }, [settingsOpen, providers, selectedProviderId]);

  // On modal open, select first provider if any exist
  useEffect(() => {
    if (!settingsOpen) return;
    setModelQuery("");
    setOnlyEnabledModels(false);
    setCustomModelInput("");
    if (providers[0]) {
      setSelectedProviderId(providers[0].id);
      setProvForm(providerToForm(providers[0]));
    } else {
      setSelectedProviderId("new");
      setProvForm(createBlankProvider());
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settingsOpen]);

  // Sync route drafts: full reset on modal open; preserve unsaved dirty stages on refresh
  useEffect(() => {
    if (!settingsOpen) {
      prevSettingsOpenRef.current = false;
      return;
    }
    const justOpened = !prevSettingsOpenRef.current;
    prevSettingsOpenRef.current = true;

    const buildDefaultMap = (): Record<string, ModelRoute> => {
      const map: Record<string, ModelRoute> = {};
      for (const def of PIPELINE_STAGES) {
        const existing = modelRoutes.find((r) => r.stage === def.stage);
        map[def.stage] = existing
          ? { ...existing }
          : {
              stage: def.stage,
              providerId: undefined,
              modelName: "",
              temperature: def.defaultTemp,
              maxTokens: def.defaultMaxTokens,
              updatedAt: "",
            };
      }
      return map;
    };

    if (justOpened) {
      setRouteDrafts(buildDefaultMap());
      setDirtyStages(new Set());
      dirtyStagesRef.current = new Set();
      return;
    }

    const freshMap = buildDefaultMap();
    setRouteDrafts((prev) => {
      const merged: Record<string, ModelRoute> = { ...prev };
      for (const def of PIPELINE_STAGES) {
        if (!dirtyStagesRef.current.has(def.stage) && freshMap[def.stage]) {
          merged[def.stage] = freshMap[def.stage]!;
        }
      }
      return merged;
    });
  }, [settingsOpen, modelRoutes]);

  // Sync active book export dir & target words
  useEffect(() => {
    if (!settingsOpen) return;
    setBookExportDir(activeBook?.exportDir ?? "");
    setBookTargetWords(String(activeBook?.targetChapterWords ?? 2500));
  }, [settingsOpen, activeBook]);

  // Close on Escape
  useEffect(() => {
    if (!settingsOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") closeSettings();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [settingsOpen, closeSettings]);

  const providerMap = useMemo(() => {
    const m = new Map<number, ProviderPublic>();
    for (const p of providers) m.set(p.id, p);
    return m;
  }, [providers]);

  if (!settingsOpen) return null;

  const selectProvider = (p: ProviderPublic) => {
    setSelectedProviderId(p.id);
    setProvForm(providerToForm(p));
    setShowApiKey(false);
    setConfirmDeleteProv(false);
    setModelQuery("");
    setOnlyEnabledModels(false);
    setCustomModelInput("");
  };

  const startNewProvider = () => {
    setSelectedProviderId("new");
    setProvForm(createBlankProvider());
    setShowApiKey(false);
    setConfirmDeleteProv(false);
    setModelQuery("");
    setOnlyEnabledModels(false);
    setCustomModelInput("");
  };

  const handleProtocolChange = (nextProto: ProviderProtocol) => {
    const prevDef = PROVIDER_PROTOCOLS.find((p) => p.value === provForm.protocol);
    const nextDef = PROVIDER_PROTOCOLS.find((p) => p.value === nextProto);
    const shouldReplaceUrl =
      !provForm.baseUrl.trim() || provForm.baseUrl === prevDef?.defaultBaseUrl;

    setProvForm({
      ...provForm,
      protocol: nextProto,
      baseUrl: shouldReplaceUrl ? nextDef?.defaultBaseUrl ?? provForm.baseUrl : provForm.baseUrl,
    });
  };

  const persistEnabledModels = async (nextEnabled: string[]) => {
    const cleaned = dedupeModels(nextEnabled);
    setProvForm((prev) => ({ ...prev, enabledModels: cleaned }));

    if (provForm.id <= 0) return;
    const savedRecord = providerMap.get(provForm.id);
    if (!savedRecord) return;

    setSyncingModels(true);
    try {
      await SettingsService.upsertProvider({
        id: savedRecord.id,
        name: savedRecord.name,
        protocol: savedRecord.protocol,
        baseUrl: savedRecord.baseUrl,
        apiKey: "",
        models: cleaned,
        fetchedModels: provForm.fetchedModels,
        enabled: savedRecord.enabled,
      });
      await refreshSettingsData();
    } catch (err) {
      notify({
        tone: "danger",
        message: "同步启用模型失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setSyncingModels(false);
    }
  };

  const handleToggleModel = (modelId: string) => {
    const exists = provForm.enabledModels.includes(modelId);
    const next = exists
      ? provForm.enabledModels.filter((m) => m !== modelId)
      : [...provForm.enabledModels, modelId];
    void persistEnabledModels(next);
  };

  const handleAddCustomModel = () => {
    const raw = customModelInput.trim();
    if (!raw) return;
    const parts = raw
      .split(/[\n,]+/)
      .map((s) => s.trim())
      .filter(Boolean);
    if (parts.length === 0) return;
    setCustomModelInput("");
    const next = dedupeModels([...provForm.enabledModels, ...parts]);
    void persistEnabledModels(next);
  };

  const handleFetchModels = async (providerId: number, autoTriggered = false) => {
    setFetchingModelsId(providerId);
    try {
      const res = await SettingsService.fetchProviderModels(providerId);
      await refreshSettingsData();
      setProvForm((prev) =>
        prev.id === providerId
          ? {
              ...prev,
              fetchedModels: dedupeModels(res.models ?? []),
              enabledModels: dedupeModels(res.enabledModels ?? prev.enabledModels),
            }
          : prev
      );
      if (res.count > 0) {
        const enabledCount = (res.enabledModels ?? []).length;
        notify({
          tone: "ok",
          message: autoTriggered
            ? enabledCount > 0
              ? `已保存并默认启用 ${enabledCount} 个模型`
              : `已保存并拉取 ${res.count} 个模型，请勾选需要启用的模型`
            : res.message || `已拉取 ${res.count} 个模型`,
        });
      } else {
        notify({
          tone: "warn",
          message: res.message || "未拉取到可用模型，请手动添加",
        });
      }
    } catch (err) {
      notify({
        tone: autoTriggered ? "warn" : "danger",
        message: autoTriggered
          ? "供应商已保存，但自动拉取模型列表失败（可手动添加）"
          : "拉取模型列表失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setFetchingModelsId(null);
    }
  };

  const handleSaveProvider = async () => {
    const name = provForm.name.trim();
    if (!name) {
      notify({ tone: "warn", message: "请填写供应商名称" });
      return;
    }
    const baseUrl = provForm.baseUrl.trim();
    if (!baseUrl) {
      notify({ tone: "warn", message: "请填写 Base URL" });
      return;
    }
    const isNew = provForm.id <= 0;
    const models = dedupeModels(provForm.enabledModels);

    setSavingProv(true);
    try {
      const saved = await SettingsService.upsertProvider({
        id: provForm.id,
        name,
        protocol: provForm.protocol,
        baseUrl,
        apiKey: provForm.apiKey.trim(),
        models,
        fetchedModels: provForm.fetchedModels,
        enabled: provForm.enabled,
      });
      await refreshSettingsData();
      setSelectedProviderId(saved.id);
      setProvForm(providerToForm(saved));
      setShowApiKey(false);

      if (isNew || (models.length === 0 && (saved.fetchedModels ?? []).length === 0)) {
        setSavingProv(false);
        await handleFetchModels(saved.id, true);
        return;
      }

      notify({ tone: "ok", message: `已保存供应商「${saved.name}」` });
    } catch (err) {
      notify({ tone: "danger", message: "保存供应商失败", detail: formatErrorMessage(err) });
    } finally {
      setSavingProv(false);
    }
  };

  const handleTestProvider = async (id: number) => {
    setTestingId(id);
    try {
      const result = await SettingsService.testProvider(id);
      setTestResults((prev) => ({ ...prev, [id]: result }));
      if (result.ok) {
        notify({
          tone: "ok",
          message: `连接成功（${result.latencyMs}ms）`,
          detail: result.modelUsed ? `测试模型：${result.modelUsed}` : result.message,
        });
      } else {
        notify({
          tone: "danger",
          message: "供应商连通性探测失败",
          detail: formatErrorMessage(result.message),
        });
      }
    } catch (err) {
      const msg = formatErrorMessage(err);
      setTestResults((prev) => ({
        ...prev,
        [id]: { ok: false, latencyMs: 0, modelUsed: "", message: msg },
      }));
      notify({ tone: "danger", message: "连通性探测出错", detail: msg });
    } finally {
      setTestingId(null);
    }
  };

  const handleDeleteProvider = async () => {
    if (provForm.id <= 0) return;
    setSavingProv(true);
    try {
      await SettingsService.deleteProvider(provForm.id);
      await refreshSettingsData();
      setConfirmDeleteProv(false);
      setSelectedProviderId("new");
      setProvForm(createBlankProvider());
      notify({ tone: "ok", message: "已删除供应商" });
    } catch (err) {
      notify({ tone: "danger", message: "删除供应商失败", detail: formatErrorMessage(err) });
    } finally {
      setSavingProv(false);
    }
  };

  // Route handlers
  const updateRouteField = (stage: string, patch: Partial<ModelRoute>) => {
    setRouteDrafts((prev) => {
      const current = prev[stage];
      if (!current) return prev;
      return { ...prev, [stage]: { ...current, ...patch } };
    });
    setDirtyStages((prev) => {
      const next = new Set(prev);
      next.add(stage);
      dirtyStagesRef.current = next;
      return next;
    });
  };

  const handleSaveRoute = async (stage: string) => {
    const draft = routeDrafts[stage];
    if (!draft) return;
    setSavingStage(stage);
    try {
      const saved = await SettingsService.saveModelRoute({
        stage: draft.stage,
        providerId: draft.providerId && draft.providerId > 0 ? draft.providerId : undefined,
        modelName: draft.modelName.trim(),
        temperature: Number.isFinite(draft.temperature) ? draft.temperature : 0.7,
        maxTokens: Number.isFinite(draft.maxTokens) ? draft.maxTokens : 4096,
        updatedAt: draft.updatedAt,
      });
      setRouteDrafts((prev) => ({ ...prev, [stage]: saved }));
      setDirtyStages((prev) => {
        const next = new Set(prev);
        next.delete(stage);
        dirtyStagesRef.current = next;
        return next;
      });
      await refreshSettingsData();
      const stageLabel =
        PIPELINE_STAGES.find((s) => s.stage === stage)?.label ?? stage;
      notify({ tone: "ok", message: `已保存「${stageLabel}」路由` });
    } catch (err) {
      notify({ tone: "danger", message: "保存模型路由失败", detail: formatErrorMessage(err) });
    } finally {
      setSavingStage(null);
    }
  };

  // Workspace book settings
  const handlePickExportDir = async () => {
    try {
      const dir = await SettingsService.pickExportDir();
      if (dir) setBookExportDir(dir);
    } catch (err) {
      notify({ tone: "danger", message: "选择导出目录失败", detail: formatErrorMessage(err) });
    }
  };

  const handleSaveBookSettings = async () => {
    if (!activeBook) return;
    const parsedWords = Number.parseInt(bookTargetWords, 10);
    const finalWords =
      Number.isFinite(parsedWords) && parsedWords >= 300
        ? parsedWords
        : activeBook.targetChapterWords;

    setSavingBookSettings(true);
    try {
      await LibraryService.updateBook({
        id: activeBook.id,
        title: activeBook.title,
        genre: activeBook.genre,
        premise: activeBook.premise,
        goldenFinger: activeBook.goldenFinger,
        targetChapterWords: finalWords,
        exportDir: bookExportDir.trim(),
      });
      await refreshBooks(activeBook.id);
      notify({ tone: "ok", message: "书籍工作区配置已保存" });
    } catch (err) {
      notify({ tone: "danger", message: "保存配置失败", detail: formatErrorMessage(err) });
    } finally {
      setSavingBookSettings(false);
    }
  };

  const currentTest = provForm.id > 0 ? testResults[provForm.id] : undefined;

  // Model catalog derived state (custom enabled models on top, fetched models in stable order below)
  const fetchedSet = new Set(provForm.fetchedModels);
  const enabledSet = new Set(provForm.enabledModels);
  const customModels = provForm.enabledModels.filter((m) => !fetchedSet.has(m));
  const allCandidateModels = [...customModels, ...provForm.fetchedModels];
  const normalizedQuery = modelQuery.trim().toLowerCase();
  const visibleModels = allCandidateModels.filter((m) => {
    if (onlyEnabledModels && !enabledSet.has(m)) return false;
    if (normalizedQuery && !m.toLowerCase().includes(normalizedQuery)) return false;
    return true;
  });

  const handleEnableVisible = () => {
    if (visibleModels.length === 0) return;
    void persistEnabledModels([...provForm.enabledModels, ...visibleModels]);
  };

  const handleClearEnabled = () => {
    if (provForm.enabledModels.length === 0) return;
    void persistEnabledModels([]);
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-[2px]"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) closeSettings();
      }}
    >
      <div className="flex h-[82vh] max-h-[700px] w-full max-w-[880px] overflow-hidden rounded-xl border border-border-strong bg-elevated shadow-[var(--shadow-elevated)]">
        {/* Left Navigation Rail */}
        <aside className="flex w-48 shrink-0 flex-col border-r border-border-subtle bg-surface p-3">
          <div className="mb-3 px-2.5 pt-1 text-[13px] font-medium text-ink-primary">
            设置
          </div>
          <nav className="flex flex-col gap-1">
            <button
              type="button"
              onClick={() => setTab("providers")}
              className={`flex items-center gap-2 rounded-md px-2.5 py-2 text-left text-xs font-medium transition-colors ${
                tab === "providers"
                  ? "bg-accent-soft text-accent"
                  : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
              }`}
            >
              <KeyRound className="h-3.5 w-3.5" />
              <span>API 供应商</span>
            </button>
            <button
              type="button"
              onClick={() => setTab("routes")}
              className={`flex items-center gap-2 rounded-md px-2.5 py-2 text-left text-xs font-medium transition-colors ${
                tab === "routes"
                  ? "bg-accent-soft text-accent"
                  : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
              }`}
            >
              <GitBranch className="h-3.5 w-3.5" />
              <span>模型路由</span>
            </button>
            <button
              type="button"
              onClick={() => setTab("workspace")}
              className={`flex items-center gap-2 rounded-md px-2.5 py-2 text-left text-xs font-medium transition-colors ${
                tab === "workspace"
                  ? "bg-accent-soft text-accent"
                  : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
              }`}
            >
              <Sliders className="h-3.5 w-3.5" />
              <span>阅读与工作区</span>
            </button>
            <button
              type="button"
              onClick={() => setTab("usage")}
              className={`flex items-center gap-2 rounded-md px-2.5 py-2 text-left text-xs font-medium transition-colors ${
                tab === "usage"
                  ? "bg-accent-soft text-accent"
                  : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
              }`}
            >
              <BarChart3 className="h-3.5 w-3.5" />
              <span>Token 与成本统计</span>
            </button>
          </nav>
        </aside>

        {/* Right Panel */}
        <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
          <div className="flex items-center justify-between border-b border-border-subtle px-5 py-3">
            <h2 className="text-sm font-medium text-ink-primary">
              {tab === "providers"
                ? "API 供应商管理"
                : tab === "routes"
                ? "5 环节独立模型路由"
                : tab === "usage"
                ? "Token 消耗与调用统计"
                : "阅读排版与工作区"}
            </h2>
            <button
              type="button"
              onClick={closeSettings}
              aria-label="关闭设置"
              className="rounded p-1 text-ink-muted transition-colors hover:bg-surface hover:text-ink-primary"
            >
              <X className="h-4 w-4" />
            </button>
          </div>

          {/* Tab 1: Providers */}
          {tab === "providers" && (
            <div className="flex min-h-0 flex-1 overflow-hidden">
              {/* Provider List */}
              <div className="flex w-52 shrink-0 flex-col border-r border-border-subtle bg-surface/40 p-2.5">
                <button
                  type="button"
                  onClick={startNewProvider}
                  className={`mb-2 flex items-center justify-center gap-1.5 rounded-md border px-2.5 py-1.5 text-xs font-medium transition-colors ${
                    selectedProviderId === "new"
                      ? "border-accent bg-accent-soft text-accent"
                      : "border-dashed border-border-strong text-ink-secondary hover:border-accent hover:text-accent"
                  }`}
                >
                  <Plus className="h-3.5 w-3.5" />
                  <span>新增供应商</span>
                </button>

                <div className="flex flex-1 flex-col gap-1 overflow-y-auto">
                  {providers.map((p) => {
                    const selected = selectedProviderId === p.id;
                    const tr = testResults[p.id];
                    return (
                      <button
                        key={p.id}
                        type="button"
                        onClick={() => selectProvider(p)}
                        className={`flex flex-col gap-1 rounded-lg border px-2.5 py-2 text-left transition-colors ${
                          selected
                            ? "border-accent/50 bg-accent-soft"
                            : "border-transparent hover:bg-surface"
                        }`}
                      >
                        <div className="flex items-center justify-between gap-1.5">
                          <span
                            className={`truncate text-xs font-medium ${
                              selected ? "text-accent" : "text-ink-primary"
                            }`}
                          >
                            {p.name}
                          </span>
                          <span
                            className={`h-1.5 w-1.5 shrink-0 rounded-full ${
                              p.enabled ? "bg-ok" : "bg-ink-muted"
                            }`}
                          />
                        </div>
                        <div className="flex items-center justify-between gap-1 text-[10.5px] text-ink-muted">
                          <span className="truncate">
                            {PROVIDER_PROTOCOLS.find((x) => x.value === p.protocol)
                              ?.label ?? p.protocol}
                            {(p.models?.length ?? 0) > 0 && ` · ${p.models.length} 模型`}
                          </span>
                          {tr && (
                            <span
                              className={`nib-mono shrink-0 ${
                                tr.ok ? "text-ok" : "text-danger"
                              }`}
                            >
                              {tr.ok ? `${tr.latencyMs}ms` : "失败"}
                            </span>
                          )}
                        </div>
                      </button>
                    );
                  })}
                </div>
              </div>

              {/* Provider Form */}
              <div className="flex min-w-0 flex-1 flex-col overflow-y-auto px-5 py-4">
                <div className="flex flex-col gap-3.5">
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    <div>
                      <label className="mb-1 block text-xs text-ink-secondary">
                        供应商名称
                      </label>
                      <input
                        type="text"
                        value={provForm.name}
                        onChange={(e) =>
                          setProvForm({ ...provForm, name: e.target.value })
                        }
                        placeholder="如：DeepSeek / OpenRouter / Ollama"
                        className="w-full rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                      />
                    </div>
                    <div>
                      <label className="mb-1 block text-xs text-ink-secondary">
                        接口协议
                      </label>
                      <select
                        value={provForm.protocol}
                        onChange={(e) => handleProtocolChange(e.target.value)}
                        className="w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                      >
                        {PROVIDER_PROTOCOLS.map((proto) => (
                          <option key={proto.value} value={proto.value}>
                            {proto.label}
                          </option>
                        ))}
                      </select>
                    </div>
                  </div>

                  <div>
                    <label className="mb-1 block text-xs text-ink-secondary">
                      Base URL
                    </label>
                    <input
                      type="text"
                      value={provForm.baseUrl}
                      onChange={(e) =>
                        setProvForm({ ...provForm, baseUrl: e.target.value })
                      }
                      placeholder="https://api.deepseek.com/v1"
                      className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                    />
                  </div>

                  <div>
                    <div className="mb-1 flex items-center justify-between gap-2">
                      <label className="text-xs text-ink-secondary">API Key</label>
                      {provForm.id > 0 && provForm.hasApiKey && (
                        <span className="nib-mono rounded bg-ok-soft px-1.5 py-0.5 text-[10.5px] text-ok">
                          已保存：{provForm.apiKeyMasked}
                        </span>
                      )}
                    </div>
                    <div className="relative">
                      <input
                        type={showApiKey ? "text" : "password"}
                        value={provForm.apiKey}
                        onChange={(e) =>
                          setProvForm({ ...provForm, apiKey: e.target.value })
                        }
                        placeholder={
                          provForm.id > 0 && provForm.hasApiKey
                            ? "留空保留已保存密钥"
                            : "输入 API Key（本地模型可留空）"
                        }
                        autoComplete="off"
                        className="nib-mono w-full rounded-md border border-border-subtle bg-canvas py-1.5 pr-9 pl-3 text-xs text-ink-primary placeholder:font-ui placeholder:text-ink-muted focus:border-accent focus:outline-none"
                      />
                      <button
                        type="button"
                        onClick={() => setShowApiKey((v) => !v)}
                        aria-label={showApiKey ? "隐藏密钥" : "显示密钥"}
                        className="absolute top-1/2 right-2 -translate-y-1/2 rounded p-1 text-ink-muted hover:text-ink-primary"
                      >
                        {showApiKey ? (
                          <EyeOff className="h-3.5 w-3.5" />
                        ) : (
                          <Eye className="h-3.5 w-3.5" />
                        )}
                      </button>
                    </div>
                  </div>

                  {/* Model Enablement Picker */}
                  <div className="overflow-hidden rounded-lg border border-border-subtle bg-canvas/60">
                    {/* Header */}
                    <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border-subtle bg-surface/65 px-3 py-2">
                      <div className="flex items-center gap-2">
                        <span className="text-xs font-medium text-ink-primary">模型</span>
                        <button
                          type="button"
                          onClick={() => setOnlyEnabledModels((v) => !v)}
                          title="点击切换仅显示已启用模型"
                          className={`nib-mono rounded-full px-2 py-0.5 text-[10.5px] transition-colors ${
                            onlyEnabledModels
                              ? "bg-accent text-canvas font-medium"
                              : provForm.enabledModels.length > 0
                              ? "bg-accent-soft text-accent hover:bg-accent/20"
                              : "bg-elevated text-ink-muted hover:text-ink-primary"
                          }`}
                        >
                          已启用 {provForm.enabledModels.length}
                          {allCandidateModels.length > 0
                            ? ` / ${allCandidateModels.length}`
                            : ""}
                        </button>
                        {syncingModels && (
                          <Loader2 className="h-3 w-3 animate-spin text-ink-muted" />
                        )}
                      </div>

                      <div className="flex items-center gap-1.5">
                        {visibleModels.length > 0 && (
                          <button
                            type="button"
                            onClick={handleEnableVisible}
                            className="rounded px-1.5 py-0.5 text-[11px] text-ink-secondary transition-colors hover:bg-elevated hover:text-ink-primary"
                          >
                            {normalizedQuery ? "全选筛选" : "全选"}
                          </button>
                        )}
                        {provForm.enabledModels.length > 0 && (
                          <button
                            type="button"
                            onClick={handleClearEnabled}
                            className="rounded px-1.5 py-0.5 text-[11px] text-ink-secondary transition-colors hover:bg-elevated hover:text-ink-primary"
                          >
                            清空
                          </button>
                        )}
                        {provForm.id > 0 && (
                          <button
                            type="button"
                            disabled={fetchingModelsId === provForm.id}
                            onClick={() => void handleFetchModels(provForm.id, false)}
                            className="flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-medium text-accent transition-colors hover:bg-accent-soft disabled:opacity-50"
                          >
                            <RefreshCw
                              className={`h-3 w-3 ${
                                fetchingModelsId === provForm.id ? "animate-spin" : ""
                              }`}
                            />
                            <span>
                              {fetchingModelsId === provForm.id ? "拉取中…" : "拉取模型"}
                            </span>
                          </button>
                        )}
                      </div>
                    </div>

                    {/* Search Bar (shown when catalog has >= 6 models) */}
                    {allCandidateModels.length >= 6 && (
                      <div className="relative border-b border-border-subtle bg-canvas/80 px-2.5 py-1.5">
                        <Search className="pointer-events-none absolute top-1/2 left-4 h-3.5 w-3.5 -translate-y-1/2 text-ink-muted" />
                        <input
                          type="text"
                          value={modelQuery}
                          onChange={(e) => setModelQuery(e.target.value)}
                          placeholder="搜索模型 ID…"
                          className="nib-mono w-full rounded border border-border-subtle bg-surface/60 py-1 pr-7 pl-7 text-xs text-ink-primary placeholder:font-ui placeholder:text-ink-muted focus:border-accent focus:outline-none"
                        />
                        {modelQuery && (
                          <button
                            type="button"
                            onClick={() => setModelQuery("")}
                            aria-label="清除搜索"
                            className="absolute top-1/2 right-4 -translate-y-1/2 rounded p-0.5 text-ink-muted hover:text-ink-primary"
                          >
                            <X className="h-3 w-3" />
                          </button>
                        )}
                      </div>
                    )}

                    {/* Hint when catalog fetched but none enabled yet */}
                    {provForm.id > 0 &&
                      allCandidateModels.length > 0 &&
                      provForm.enabledModels.length === 0 && (
                        <div className="border-b border-warn/25 bg-warn-soft/70 px-3 py-1.5 text-[11px] text-warn">
                          开启下方开关以启用模型，已启用的模型会同步至「模型路由」下拉列表。
                        </div>
                      )}

                    {/* Candidate Model Rows */}
                    <div className="max-h-52 overflow-y-auto divide-y divide-border-subtle/50">
                      {fetchingModelsId === provForm.id && allCandidateModels.length === 0 ? (
                        <div className="flex items-center justify-center gap-2 py-6 text-xs text-ink-muted">
                          <Loader2 className="h-3.5 w-3.5 animate-spin text-accent" />
                          <span>正在拉取上游模型列表…</span>
                        </div>
                      ) : visibleModels.length === 0 ? (
                        <div className="px-3 py-5 text-center text-xs text-ink-muted">
                          {allCandidateModels.length === 0
                            ? provForm.id > 0
                              ? "暂无候选模型，点击右上角「拉取模型」或在下方手动添加"
                              : "保存供应商后将自动拉取可用模型，也可先在下方手动添加"
                            : onlyEnabledModels
                            ? "当前未启用任何模型"
                            : "无匹配模型"}
                        </div>
                      ) : (
                        visibleModels.map((modelId) => {
                          const isEnabled = enabledSet.has(modelId);
                          const isCustom = !fetchedSet.has(modelId);
                          return (
                            <div
                              key={modelId}
                              onClick={() => handleToggleModel(modelId)}
                              className={`flex cursor-pointer items-center justify-between gap-2 px-3 py-1.5 transition-colors ${
                                isEnabled
                                  ? "bg-accent-soft/35 hover:bg-accent-soft/55"
                                  : "hover:bg-surface/70"
                              }`}
                            >
                              <div className="flex min-w-0 items-center gap-2">
                                <span
                                  className={`nib-mono truncate text-xs ${
                                    isEnabled
                                      ? "font-medium text-ink-primary"
                                      : "text-ink-secondary"
                                  }`}
                                >
                                  {modelId}
                                </span>
                                {isCustom && (
                                  <span className="shrink-0 rounded bg-elevated px-1.5 py-0.2 text-[10px] text-ink-muted">
                                    自定义
                                  </span>
                                )}
                              </div>

                              <button
                                type="button"
                                role="switch"
                                aria-checked={isEnabled}
                                aria-label={`启用模型 ${modelId}`}
                                onClick={(e) => {
                                  e.stopPropagation();
                                  handleToggleModel(modelId);
                                }}
                                className={`relative inline-flex h-4 w-7 shrink-0 items-center rounded-full transition-colors ${
                                  isEnabled ? "bg-accent" : "bg-border-strong"
                                }`}
                              >
                                <span
                                  className={`inline-block h-3 w-3 rounded-full bg-canvas shadow-sm transition-transform ${
                                    isEnabled ? "translate-x-3.5" : "translate-x-0.5"
                                  }`}
                                />
                              </button>
                            </div>
                          );
                        })
                      )}
                    </div>

                    {/* Manual Add Custom Model */}
                    <div className="flex items-center gap-1.5 border-t border-border-subtle bg-surface/40 px-2.5 py-1.5">
                      <input
                        type="text"
                        value={customModelInput}
                        onChange={(e) => setCustomModelInput(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") {
                            e.preventDefault();
                            handleAddCustomModel();
                          }
                        }}
                        placeholder="手动添加模型 ID（回车加入并启用）"
                        className="nib-mono min-w-0 flex-1 rounded border border-border-subtle bg-canvas px-2.5 py-1 text-xs text-ink-primary placeholder:font-ui placeholder:text-ink-muted focus:border-accent focus:outline-none"
                      />
                      <button
                        type="button"
                        disabled={!customModelInput.trim()}
                        onClick={handleAddCustomModel}
                        className="flex shrink-0 items-center gap-1 rounded border border-border-subtle bg-elevated px-2.5 py-1 text-xs text-ink-primary transition-colors hover:border-accent hover:text-accent disabled:opacity-40"
                      >
                        <Plus className="h-3 w-3" />
                        <span>添加</span>
                      </button>
                    </div>
                  </div>

                  <label className="flex cursor-pointer items-center justify-between rounded-md border border-border-subtle bg-canvas/60 px-3 py-2">
                    <span className="text-xs text-ink-primary">启用此供应商</span>
                    <input
                      type="checkbox"
                      checked={provForm.enabled}
                      onChange={(e) =>
                        setProvForm({ ...provForm, enabled: e.target.checked })
                      }
                      className="h-4 w-4 accent-[var(--accent)]"
                    />
                  </label>

                  {/* Live Test Result Badge */}
                  {currentTest && (
                    <div
                      className={`flex items-start gap-2 rounded-lg border px-3 py-2 text-xs ${
                        currentTest.ok
                          ? "border-ok/35 bg-ok-soft text-ok"
                          : "border-danger/35 bg-danger-soft text-danger"
                      }`}
                    >
                      {currentTest.ok ? (
                        <CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                      ) : (
                        <XCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                      )}
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2 font-medium">
                          <span>{currentTest.ok ? "连通正常" : "连通失败"}</span>
                          {currentTest.latencyMs > 0 && (
                            <span className="nib-mono text-[11px]">
                              {currentTest.latencyMs}ms
                            </span>
                          )}
                          {currentTest.modelUsed && (
                            <span className="nib-mono text-[11px] opacity-85">
                              · {currentTest.modelUsed}
                            </span>
                          )}
                        </div>
                        {currentTest.message && (
                          <div className="mt-0.5 break-words text-[11px] opacity-90">
                            {currentTest.message}
                          </div>
                        )}
                      </div>
                    </div>
                  )}

                  {/* Form Actions */}
                  <div className="mt-1 flex flex-wrap items-center justify-between gap-2 border-t border-border-subtle pt-3.5">
                    {provForm.id > 0 ? (
                      confirmDeleteProv ? (
                        <div className="flex items-center gap-1.5">
                          <button
                            type="button"
                            disabled={savingProv}
                            onClick={() => void handleDeleteProvider()}
                            className="rounded bg-danger px-2.5 py-1 text-xs font-medium text-white"
                          >
                            确认删除
                          </button>
                          <button
                            type="button"
                            onClick={() => setConfirmDeleteProv(false)}
                            className="text-xs text-ink-secondary hover:text-ink-primary"
                          >
                            取消
                          </button>
                        </div>
                      ) : (
                        <button
                          type="button"
                          onClick={() => setConfirmDeleteProv(true)}
                          className="flex items-center gap-1 rounded-md px-2 py-1 text-xs text-ink-muted transition-colors hover:bg-danger-soft hover:text-danger"
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                          <span>删除</span>
                        </button>
                      )
                    ) : (
                      <div />
                    )}

                    <div className="flex items-center gap-2">
                      {provForm.id > 0 && (
                        <button
                          type="button"
                          disabled={testingId === provForm.id}
                          onClick={() => void handleTestProvider(provForm.id)}
                          className="flex items-center gap-1.5 rounded-md border border-border-subtle bg-surface px-3 py-1.5 text-xs font-medium text-ink-primary transition-colors hover:border-border-strong disabled:opacity-50"
                        >
                          {testingId === provForm.id ? (
                            <Loader2 className="h-3.5 w-3.5 animate-spin text-accent" />
                          ) : (
                            <Wifi className="h-3.5 w-3.5 text-accent" />
                          )}
                          <span>
                            {testingId === provForm.id ? "探测中…" : "测试连通性"}
                          </span>
                        </button>
                      )}

                      <button
                        type="button"
                        disabled={savingProv || (provForm.id > 0 && fetchingModelsId === provForm.id)}
                        onClick={() => void handleSaveProvider()}
                        className="flex items-center gap-1.5 rounded-md bg-accent px-4 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
                      >
                        <Save className="h-3.5 w-3.5" />
                        <span>
                          {savingProv
                            ? "保存中…"
                            : provForm.id > 0 && fetchingModelsId === provForm.id
                            ? "拉取模型中…"
                            : "保存供应商"}
                        </span>
                      </button>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          )}

          {/* Tab 2: 5-Stage Model Routes */}
          {tab === "routes" && (
            <div className="flex-1 overflow-y-auto px-5 py-4">
              {providers.length === 0 && (
                <div className="mb-4 flex items-center justify-between rounded-lg border border-warn/35 bg-warn-soft px-3.5 py-2.5 text-xs text-warn">
                  <span>请先添加一个 API 供应商，再绑定各创作环节模型。</span>
                  <button
                    type="button"
                    onClick={() => setTab("providers")}
                    className="font-medium underline underline-offset-2"
                  >
                    去添加
                  </button>
                </div>
              )}

              <div className="flex flex-col gap-3">
                {PIPELINE_STAGES.map((stageDef) => {
                  const route = routeDrafts[stageDef.stage];
                  if (!route) return null;
                  const selectedProv = route.providerId
                    ? providerMap.get(route.providerId)
                    : undefined;
                  const modelSuggestions = selectedProv?.models ?? [];
                  const isDirty = dirtyStages.has(stageDef.stage);
                  const isSaving = savingStage === stageDef.stage;

                  return (
                    <div
                      key={stageDef.stage}
                      className="rounded-lg border border-border-subtle bg-surface/65 p-3.5"
                    >
                      <div className="mb-2.5 flex items-center justify-between gap-2">
                        <div className="flex items-center gap-2">
                          <span className="text-[13px] font-medium text-ink-primary">
                            {stageDef.label}
                          </span>
                          <span className="nib-mono rounded bg-elevated px-1.5 py-0.5 text-[10px] text-ink-muted">
                            {stageDef.stage}
                          </span>
                          <span className="text-[11px] text-ink-secondary">
                            {stageDef.desc}
                          </span>
                        </div>

                        <button
                          type="button"
                          disabled={isSaving || !isDirty}
                          onClick={() => void handleSaveRoute(stageDef.stage)}
                          className="flex items-center gap-1 rounded-md bg-accent-soft border border-accent/35 px-2.5 py-1 text-[11px] font-medium text-accent transition-colors hover:border-accent disabled:opacity-35"
                        >
                          <Save className="h-3 w-3" />
                          <span>{isSaving ? "保存中…" : "保存"}</span>
                        </button>
                      </div>

                      <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-[170px_1fr_92px_104px]">
                        {/* Provider Select */}
                        <div>
                          <label className="mb-1 block text-[10.5px] text-ink-muted">
                            供应商
                          </label>
                          <select
                            value={route.providerId ?? ""}
                            onChange={(e) => {
                              const raw = e.target.value;
                              const nextProvId = raw ? Number(raw) : undefined;
                              const nextProv = nextProvId
                                ? providerMap.get(nextProvId)
                                : undefined;
                              const nextModel =
                                nextProv &&
                                nextProv.models.length > 0 &&
                                !nextProv.models.includes(route.modelName)
                                  ? nextProv.models[0] ?? route.modelName
                                  : route.modelName;
                              updateRouteField(stageDef.stage, {
                                providerId: nextProvId,
                                modelName: nextModel,
                              });
                            }}
                            className="w-full rounded-md border border-border-subtle bg-canvas px-2 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                          >
                            <option value="">未绑定</option>
                            {providers.map((p) => (
                              <option key={p.id} value={p.id}>
                                {p.name}
                              </option>
                            ))}
                          </select>
                        </div>

                        {/* Model Input + Datalist */}
                        <div>
                          <label className="mb-1 block text-[10.5px] text-ink-muted">
                            模型名
                          </label>
                          <input
                            type="text"
                            list={`models-${stageDef.stage}`}
                            value={route.modelName}
                            onChange={(e) =>
                              updateRouteField(stageDef.stage, {
                                modelName: e.target.value,
                              })
                            }
                            placeholder="选择或输入模型 ID"
                            className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:font-ui placeholder:text-ink-muted focus:border-accent focus:outline-none"
                          />
                          <datalist id={`models-${stageDef.stage}`}>
                            {modelSuggestions.map((m) => (
                              <option key={m} value={m} />
                            ))}
                          </datalist>
                        </div>

                        {/* Temperature */}
                        <div>
                          <label className="mb-1 block text-[10.5px] text-ink-muted">
                            温度
                          </label>
                          <input
                            type="number"
                            min={0}
                            max={2}
                            step={0.05}
                            value={route.temperature}
                            onChange={(e) =>
                              updateRouteField(stageDef.stage, {
                                temperature: Number.parseFloat(e.target.value) || 0,
                              })
                            }
                            className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-2 py-1.5 text-right text-xs text-ink-primary focus:border-accent focus:outline-none"
                          />
                        </div>

                        {/* Max Tokens */}
                        <div>
                          <label className="mb-1 block text-[10.5px] text-ink-muted">
                            Max Tokens
                          </label>
                          <input
                            type="number"
                            min={256}
                            max={200000}
                            step={256}
                            value={route.maxTokens}
                            onChange={(e) =>
                              updateRouteField(stageDef.stage, {
                                maxTokens: Number.parseInt(e.target.value, 10) || 0,
                              })
                            }
                            className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-2 py-1.5 text-right text-xs text-ink-primary focus:border-accent focus:outline-none"
                          />
                        </div>
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {/* Tab 3: Reading Preferences & Workspace Export Dir */}
          {tab === "workspace" && (
            <div className="flex-1 overflow-y-auto px-5 py-4">
              <div className="flex flex-col gap-5">
                {/* Theme Selector */}
                <section>
                  <div className="mb-2 text-xs font-medium text-ink-secondary">
                    界面主题
                  </div>
                  <div className="grid grid-cols-2 gap-3">
                    <button
                      type="button"
                      onClick={() => setTheme("dark")}
                      className={`flex items-center gap-3 rounded-lg border p-3 text-left transition-colors ${
                        theme === "dark"
                          ? "border-accent bg-accent-soft"
                          : "border-border-subtle bg-canvas/60 hover:border-border-strong"
                      }`}
                    >
                      <span
                        className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-white/15"
                        style={{ backgroundColor: "#141416", color: "#D4A95E" }}
                      >
                        <Moon className="h-4 w-4" />
                      </span>
                      <div>
                        <div className="text-xs font-medium text-ink-primary">
                          深色护眼
                        </div>
                        <div className="nib-mono text-[10.5px] text-ink-muted">
                          #141416
                        </div>
                      </div>
                    </button>

                    <button
                      type="button"
                      onClick={() => setTheme("light")}
                      className={`flex items-center gap-3 rounded-lg border p-3 text-left transition-colors ${
                        theme === "light"
                          ? "border-accent bg-accent-soft"
                          : "border-border-subtle bg-canvas/60 hover:border-border-strong"
                      }`}
                    >
                      <span
                        className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-black/15"
                        style={{ backgroundColor: "#F6F3EC", color: "#9C6D24" }}
                      >
                        <Sun className="h-4 w-4" />
                      </span>
                      <div>
                        <div className="text-xs font-medium text-ink-primary">
                          暖纸浅色
                        </div>
                        <div className="nib-mono text-[10.5px] text-ink-muted">
                          #F6F3EC
                        </div>
                      </div>
                    </button>
                  </div>
                </section>

                {/* Prose Typography */}
                <section>
                  <div className="mb-2 text-xs font-medium text-ink-secondary">
                    正文阅读排版
                  </div>
                  <div className="flex flex-col gap-2.5 rounded-lg border border-border-subtle bg-surface/50 p-3.5">
                    <div className="flex items-center justify-between gap-3">
                      <span className="text-xs text-ink-primary">正文字体</span>
                      <div className="flex items-center gap-1 rounded-md border border-border-subtle bg-canvas p-0.5">
                        <button
                          type="button"
                          onClick={() => setProseFont("serif")}
                          className={`rounded px-2.5 py-1 text-xs transition-colors ${
                            proseFont === "serif"
                              ? "bg-accent-soft font-medium text-accent"
                              : "text-ink-secondary hover:text-ink-primary"
                          }`}
                        >
                          衬线阅读体
                        </button>
                        <button
                          type="button"
                          onClick={() => setProseFont("sans")}
                          className={`rounded px-2.5 py-1 text-xs transition-colors ${
                            proseFont === "sans"
                              ? "bg-accent-soft font-medium text-accent"
                              : "text-ink-secondary hover:text-ink-primary"
                          }`}
                        >
                          无衬线码字体
                        </button>
                      </div>
                    </div>

                    <label className="flex cursor-pointer items-center justify-between border-t border-border-subtle pt-2.5">
                      <span className="text-xs text-ink-primary">
                        段落首行缩进两字符（2em）
                      </span>
                      <input
                        type="checkbox"
                        checked={proseIndent}
                        onChange={(e) => setProseIndent(e.target.checked)}
                        className="h-4 w-4 accent-[var(--accent)]"
                      />
                    </label>

                    <div className="mt-1 rounded-md border border-border-subtle bg-canvas px-4 py-3">
                      <div className="nib-prose-column text-[15px]">
                        <p>
                          残阵灵光骤灭，焦糊味混着血腥气在石室散开。他俯身扣紧那枚微温玉瓶，转身朝暗门掠去。
                        </p>
                      </div>
                    </div>
                  </div>
                </section>

                {/* Active Book Export Dir */}
                {activeBook && (
                  <section className="border-t border-border-subtle pt-4">
                    <div className="mb-2.5 flex items-center justify-between">
                      <span className="text-xs font-medium text-ink-secondary">
                        当前书籍导出与字数设置 · 《{activeBook.title}》
                      </span>
                    </div>

                    <div className="flex flex-col gap-3 rounded-lg border border-border-subtle bg-surface/50 p-3.5">
                      <div>
                        <label className="mb-1 block text-xs text-ink-secondary">
                          本地 Markdown 单向镜像目录
                        </label>
                        <div className="flex gap-2">
                          <input
                            type="text"
                            value={bookExportDir}
                            onChange={(e) => setBookExportDir(e.target.value)}
                            placeholder="未配置导出目录"
                            className="min-w-0 flex-1 rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                          />
                          <button
                            type="button"
                            onClick={() => void handlePickExportDir()}
                            className="flex shrink-0 items-center gap-1.5 rounded-md border border-border-subtle bg-elevated px-3 py-1.5 text-xs text-ink-primary transition-colors hover:border-border-strong"
                          >
                            <FolderOpen className="h-3.5 w-3.5 text-accent" />
                            <span>选择目录</span>
                          </button>
                        </div>
                      </div>

                      <div className="flex items-end justify-between gap-3">
                        <div className="w-40">
                          <label className="mb-1 block text-xs text-ink-secondary">
                            单章目标字数
                          </label>
                          <input
                            type="number"
                            min={300}
                            max={50000}
                            step={100}
                            value={bookTargetWords}
                            onChange={(e) => setBookTargetWords(e.target.value)}
                            className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                          />
                        </div>

                        <button
                          type="button"
                          disabled={savingBookSettings}
                          onClick={() => void handleSaveBookSettings()}
                          className="flex items-center gap-1.5 rounded-md bg-accent px-4 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
                        >
                          <Save className="h-3.5 w-3.5" />
                          <span>{savingBookSettings ? "保存中…" : "保存书籍设置"}</span>
                        </button>
                      </div>
                    </div>
                  </section>
                )}
              </div>
            </div>
          )}

          {/* Tab 4: Token & Cost Usage Summary */}
          {tab === "usage" && (
            <div className="flex-1 overflow-y-auto px-5 py-4">
              <div className="flex flex-col gap-5">
                {/* Scope switcher + refresh */}
                <div className="flex items-center justify-between gap-2">
                  <div className="flex items-center gap-1 rounded-md border border-border-subtle bg-canvas p-0.5 text-xs">
                    {activeBook && (
                      <button
                        type="button"
                        onClick={() => setUsageScope("book")}
                        className={`rounded px-2.5 py-1 transition-colors ${
                          usageScope === "book"
                            ? "bg-accent-soft font-medium text-accent"
                            : "text-ink-secondary hover:text-ink-primary"
                        }`}
                      >
                        当前书籍《{activeBook.title}》
                      </button>
                    )}
                    <button
                      type="button"
                      onClick={() => setUsageScope("all")}
                      className={`rounded px-2.5 py-1 transition-colors ${
                        !activeBook || usageScope === "all"
                          ? "bg-accent-soft font-medium text-accent"
                          : "text-ink-secondary hover:text-ink-primary"
                      }`}
                    >
                      全局汇总
                    </button>
                  </div>

                  <button
                    type="button"
                    disabled={usageLoading}
                    onClick={() =>
                      void loadUsageSummary(activeBook ? usageScope : "all")
                    }
                    className="flex items-center gap-1 rounded-md border border-border-subtle bg-surface px-2.5 py-1 text-xs text-ink-secondary hover:text-ink-primary"
                  >
                    <RefreshCw
                      className={`h-3.5 w-3.5 ${
                        usageLoading ? "animate-spin text-accent" : ""
                      }`}
                    />
                    <span>刷新</span>
                  </button>
                </div>

                {/* KPI Strip */}
                <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                  <div className="rounded-lg border border-border-subtle bg-surface/65 p-3">
                    <div className="text-[11px] text-ink-muted">总调用次数</div>
                    <div className="nib-mono mt-1 text-lg font-semibold text-ink-primary">
                      {formatNumber(usageSummary?.totalCalls ?? 0)}
                    </div>
                  </div>
                  <div className="rounded-lg border border-border-subtle bg-surface/65 p-3">
                    <div className="text-[11px] text-ink-muted">输入 Prompt</div>
                    <div className="nib-mono mt-1 text-lg font-semibold text-ink-primary">
                      {formatNumber(usageSummary?.totalPromptTokens ?? 0)}
                    </div>
                  </div>
                  <div className="rounded-lg border border-border-subtle bg-surface/65 p-3">
                    <div className="text-[11px] text-ink-muted">输出 Completion</div>
                    <div className="nib-mono mt-1 text-lg font-semibold text-ink-primary">
                      {formatNumber(usageSummary?.totalCompletionTokens ?? 0)}
                    </div>
                  </div>
                  <div className="rounded-lg border border-accent/35 bg-accent-soft/40 p-3">
                    <div className="text-[11px] text-accent">总 Tokens</div>
                    <div className="nib-mono mt-1 text-lg font-semibold text-accent">
                      {formatNumber(usageSummary?.totalTokens ?? 0)}
                    </div>
                  </div>
                </div>

                {/* By Stage Table */}
                <section>
                  <div className="mb-2 text-xs font-medium text-ink-secondary">
                    按创作环节分布
                  </div>
                  <div className="overflow-hidden rounded-lg border border-border-subtle bg-canvas/70">
                    <table className="w-full text-left text-xs">
                      <thead className="border-b border-border-subtle bg-surface/70 text-[11px] text-ink-muted">
                        <tr>
                          <th className="px-3 py-2 font-medium">环节</th>
                          <th className="px-3 py-2 text-right font-medium">调用</th>
                          <th className="px-3 py-2 text-right font-medium">Prompt</th>
                          <th className="px-3 py-2 text-right font-medium">Completion</th>
                          <th className="px-3 py-2 text-right font-medium">总计</th>
                          <th className="px-3 py-2 text-right font-medium">平均延迟</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-border-subtle">
                        {(usageSummary?.byStage ?? []).length === 0 ? (
                          <tr>
                            <td
                              colSpan={6}
                              className="px-3 py-6 text-center text-ink-muted"
                            >
                              暂无调用记录
                            </td>
                          </tr>
                        ) : (
                          (usageSummary?.byStage ?? []).map((row) => {
                            const label =
                              PIPELINE_STAGES.find((s) => s.stage === row.stage)
                                ?.label ??
                              (row.stage === "style" ? "文风提取" : row.stage);
                            return (
                              <tr key={row.stage}>
                                <td className="px-3 py-2 font-medium text-ink-primary">
                                  <span>{label}</span>
                                  <span className="nib-mono ml-1.5 text-[10px] text-ink-muted">
                                    {row.stage}
                                  </span>
                                </td>
                                <td className="nib-mono px-3 py-2 text-right text-ink-secondary">
                                  {formatNumber(row.calls)}
                                </td>
                                <td className="nib-mono px-3 py-2 text-right text-ink-secondary">
                                  {formatNumber(row.promptTokens)}
                                </td>
                                <td className="nib-mono px-3 py-2 text-right text-ink-secondary">
                                  {formatNumber(row.completionTokens)}
                                </td>
                                <td className="nib-mono px-3 py-2 text-right font-medium text-ink-primary">
                                  {formatNumber(row.totalTokens)}
                                </td>
                                <td className="nib-mono px-3 py-2 text-right text-ink-muted">
                                  {row.avgLatencyMs > 0
                                    ? `${formatNumber(row.avgLatencyMs)}ms`
                                    : "—"}
                                </td>
                              </tr>
                            );
                          })
                        )}
                      </tbody>
                    </table>
                  </div>
                </section>

                {/* By Provider / Model Table */}
                <section>
                  <div className="mb-2 text-xs font-medium text-ink-secondary">
                    按供应商 / 模型分布
                  </div>
                  <div className="overflow-hidden rounded-lg border border-border-subtle bg-canvas/70">
                    <table className="w-full text-left text-xs">
                      <thead className="border-b border-border-subtle bg-surface/70 text-[11px] text-ink-muted">
                        <tr>
                          <th className="px-3 py-2 font-medium">供应商</th>
                          <th className="px-3 py-2 font-medium">模型</th>
                          <th className="px-3 py-2 text-right font-medium">调用</th>
                          <th className="px-3 py-2 text-right font-medium">Prompt</th>
                          <th className="px-3 py-2 text-right font-medium">Completion</th>
                          <th className="px-3 py-2 text-right font-medium">总计</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-border-subtle">
                        {(usageSummary?.byModel ?? []).length === 0 ? (
                          <tr>
                            <td
                              colSpan={6}
                              className="px-3 py-6 text-center text-ink-muted"
                            >
                              暂无模型统计数据
                            </td>
                          </tr>
                        ) : (
                          (usageSummary?.byModel ?? []).map((row, idx) => (
                            <tr key={`${row.providerName}-${row.modelName}-${idx}`}>
                              <td className="px-3 py-2 text-ink-primary">
                                {row.providerName || "—"}
                              </td>
                              <td className="nib-mono px-3 py-2 text-ink-secondary">
                                {row.modelName || "—"}
                              </td>
                              <td className="nib-mono px-3 py-2 text-right text-ink-secondary">
                                {formatNumber(row.calls)}
                              </td>
                              <td className="nib-mono px-3 py-2 text-right text-ink-secondary">
                                {formatNumber(row.promptTokens)}
                              </td>
                              <td className="nib-mono px-3 py-2 text-right text-ink-secondary">
                                {formatNumber(row.completionTokens)}
                              </td>
                              <td className="nib-mono px-3 py-2 text-right font-medium text-ink-primary">
                                {formatNumber(row.totalTokens)}
                              </td>
                            </tr>
                          ))
                        )}
                      </tbody>
                    </table>
                  </div>
                </section>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
