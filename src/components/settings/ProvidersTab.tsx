import { useEffect, useMemo, useRef, useState } from "react";
import {
  CheckCircle2,
  Eye,
  EyeOff,
  Loader2,
  Plus,
  RefreshCw,
  Save,
  Search,
  Trash2,
  Wifi,
  X,
  XCircle,
} from "lucide-react";
import {
  SettingsService,
  type ProviderProtocol,
  type ProviderPublic,
  type ProviderTestResult,
} from "../../mygo";
import { useApp } from "../../stores/AppContext";
import {
  PIPELINE_STAGES,
  PROVIDER_PROTOCOLS,
  formatErrorMessage,
  isIMEComposing,
} from "../../lib/constants";

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

interface VendorQuickPreset {
  label: string;
  name: string;
  protocol: ProviderProtocol;
  baseUrl: string;
  hint: string;
}

const VENDOR_PRESETS: VendorQuickPreset[] = [
  {
    label: "DeepSeek",
    name: "DeepSeek",
    protocol: "openai_compat",
    baseUrl: "https://api.deepseek.com/v1",
    hint: "粘贴 DeepSeek API Key 即可使用",
  },
  {
    label: "OpenRouter",
    name: "OpenRouter",
    protocol: "openai_compat",
    baseUrl: "https://openrouter.ai/api/v1",
    hint: "一个 Key 调用多家主流模型",
  },
  {
    label: "Claude",
    name: "Anthropic",
    protocol: "anthropic",
    baseUrl: "https://api.anthropic.com",
    hint: "Anthropic 官方接口",
  },
  {
    label: "Gemini",
    name: "Gemini",
    protocol: "gemini",
    baseUrl: "https://generativelanguage.googleapis.com",
    hint: "Google AI Studio 密钥",
  },
  {
    label: "本地 Ollama",
    name: "Ollama",
    protocol: "openai_compat",
    baseUrl: "http://127.0.0.1:11434/v1",
    hint: "本地运行，密钥留空即可",
  },
];

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

interface ProvidersTabProps {
  focusProviderId?: number | null;
  onConsumeFocus?: () => void;
}

export function ProvidersTab({ focusProviderId, onConsumeFocus }: ProvidersTabProps) {
  const { providers, modelRoutes, refreshSettingsData, notify } = useApp();

  const [selectedProviderId, setSelectedProviderId] = useState<number | "new">(() => {
    if (focusProviderId && providers.some((p) => p.id === focusProviderId)) {
      return focusProviderId;
    }
    return providers[0]?.id ?? "new";
  });
  const [provForm, setProvForm] = useState<ProviderFormState>(() => {
    if (focusProviderId) {
      const target = providers.find((p) => p.id === focusProviderId);
      if (target) return providerToForm(target);
    }
    return providers[0] ? providerToForm(providers[0]) : createBlankProvider();
  });
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

  // Refs to serialize rapid enabledModels mutations and prevent out-of-order overwrites.
  const syncQueueRef = useRef<Promise<void>>(Promise.resolve());
  const pendingSyncCountRef = useRef(0);
  const latestEnabledRef = useRef<string[]>(provForm.enabledModels);
  const latestFetchedRef = useRef<string[]>(provForm.fetchedModels);
  latestEnabledRef.current = provForm.enabledModels;
  latestFetchedRef.current = provForm.fetchedModels;

  const providerMap = useMemo(() => {
    const m = new Map<number, ProviderPublic>();
    for (const p of providers) m.set(p.id, p);
    return m;
  }, [providers]);

  useEffect(() => {
    if (!focusProviderId) return;
    const found = providers.find((p) => p.id === focusProviderId);
    if (found) {
      setSelectedProviderId(found.id);
      const next = providerToForm(found);
      setProvForm(next);
      latestEnabledRef.current = next.enabledModels;
      latestFetchedRef.current = next.fetchedModels;
      setShowApiKey(false);
      setConfirmDeleteProv(false);
      setModelQuery("");
      setOnlyEnabledModels(false);
      setCustomModelInput("");
    }
    onConsumeFocus?.();
  }, [focusProviderId, providers, onConsumeFocus]);

  // Sync provider form when providers list changes (without clobbering unsaved edits or in-flight model syncs).
  useEffect(() => {
    if (providers.length === 0) {
      setSelectedProviderId("new");
      setProvForm(createBlankProvider());
      return;
    }
    if (selectedProviderId === "new") return;

    const found = providers.find((p) => p.id === selectedProviderId);
    if (found) {
      if (pendingSyncCountRef.current > 0) return;
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
  }, [providers, selectedProviderId]);

  const selectProvider = (p: ProviderPublic) => {
    setSelectedProviderId(p.id);
    const next = providerToForm(p);
    setProvForm(next);
    latestEnabledRef.current = next.enabledModels;
    latestFetchedRef.current = next.fetchedModels;
    setShowApiKey(false);
    setConfirmDeleteProv(false);
    setModelQuery("");
    setOnlyEnabledModels(false);
    setCustomModelInput("");
  };

  const startNewProvider = () => {
    setSelectedProviderId("new");
    const blank = createBlankProvider();
    setProvForm(blank);
    latestEnabledRef.current = [];
    latestFetchedRef.current = [];
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

  const autoAssignEmptyRoutes = async (
    providerId: number,
    modelName: string
  ): Promise<number> => {
    const trimmed = modelName.trim();
    if (providerId <= 0 || !trimmed) return 0;
    const unboundStages = PIPELINE_STAGES.filter((def) => {
      const existing = modelRoutes.find((r) => r.stage === def.stage);
      return !existing || !existing.providerId || !existing.modelName.trim();
    });
    if (unboundStages.length === 0) return 0;

    let assigned = 0;
    for (const def of unboundStages) {
      const existing = modelRoutes.find((r) => r.stage === def.stage);
      try {
        await SettingsService.saveModelRoute({
          stage: def.stage,
          providerId,
          modelName: trimmed,
          temperature: existing?.temperature ?? def.defaultTemp,
          maxTokens: existing?.maxTokens ?? def.defaultMaxTokens,
          updatedAt: existing?.updatedAt ?? "",
        });
        assigned += 1;
      } catch {
        // ignore individual stage failure
      }
    }
    if (assigned > 0) {
      await refreshSettingsData();
    }
    return assigned;
  };

  const persistEnabledModels = (nextEnabled: string[]) => {
    const cleaned = dedupeModels(nextEnabled);
    latestEnabledRef.current = cleaned;
    setProvForm((prev) => ({ ...prev, enabledModels: cleaned }));

    const targetProviderId = provForm.id;
    if (targetProviderId <= 0) return;
    const savedRecord = providerMap.get(targetProviderId);
    if (!savedRecord) return;

    const fetchedSnapshot = latestFetchedRef.current;
    pendingSyncCountRef.current += 1;
    setSyncingModels(true);

    syncQueueRef.current = syncQueueRef.current.then(async () => {
      try {
        await SettingsService.upsertProvider({
          id: savedRecord.id,
          name: savedRecord.name,
          protocol: savedRecord.protocol,
          baseUrl: savedRecord.baseUrl,
          apiKey: "",
          models: cleaned,
          fetchedModels: fetchedSnapshot,
          enabled: savedRecord.enabled,
        });
        await refreshSettingsData();
        if (cleaned.length > 0 && savedRecord.enabled) {
          const assigned = await autoAssignEmptyRoutes(savedRecord.id, cleaned[0]!);
          if (assigned > 0) {
            notify({
              tone: "ok",
              message: `已自动将默认写作模型设为 ${cleaned[0]}`,
              detail: "可在「模型分配」中按环节单独调整",
            });
          }
        }
      } catch (err) {
        notify({
          tone: "danger",
          message: "同步启用模型失败",
          detail: formatErrorMessage(err),
        });
      } finally {
        pendingSyncCountRef.current = Math.max(0, pendingSyncCountRef.current - 1);
        if (pendingSyncCountRef.current === 0) {
          setSyncingModels(false);
        }
      }
    });
  };

  const handleToggleModel = (modelId: string) => {
    const current = latestEnabledRef.current;
    const exists = current.includes(modelId);
    const next = exists
      ? current.filter((m) => m !== modelId)
      : [...current, modelId];
    persistEnabledModels(next);
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
    persistEnabledModels([...latestEnabledRef.current, ...parts]);
  };

  const handleFetchModels = async (providerId: number, autoTriggered = false) => {
    setFetchingModelsId(providerId);
    try {
      const res = await SettingsService.fetchProviderModels(providerId);
      await refreshSettingsData();
      setProvForm((prev) => {
        if (prev.id !== providerId) return prev;
        const nextFetched = dedupeModels(res.models ?? []);
        const nextEnabled = dedupeModels(res.enabledModels ?? prev.enabledModels);
        latestFetchedRef.current = nextFetched;
        latestEnabledRef.current = nextEnabled;
        return {
          ...prev,
          fetchedModels: nextFetched,
          enabledModels: nextEnabled,
        };
      });
      if (res.count > 0) {
        const enabledList = dedupeModels(res.enabledModels ?? []);
        let autoBound = 0;
        if (enabledList.length > 0) {
          autoBound = await autoAssignEmptyRoutes(providerId, enabledList[0]!);
        }
        notify({
          tone: "ok",
          message: autoTriggered
            ? enabledList.length > 0
              ? autoBound > 0
                ? `已连接并启用 ${enabledList.length} 个模型，默认设为 ${enabledList[0]}`
                : `已保存并默认启用 ${enabledList.length} 个模型`
              : `已保存并拉取 ${res.count} 个模型，请开启需要使用的模型`
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
          ? "模型服务已保存，但自动拉取模型列表失败（可手动添加）"
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
      notify({ tone: "warn", message: "请填写模型服务名称" });
      return;
    }
    const baseUrl = provForm.baseUrl.trim();
    if (!baseUrl) {
      notify({ tone: "warn", message: "请填写接口地址" });
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
      const next = providerToForm(saved);
      setProvForm(next);
      latestEnabledRef.current = next.enabledModels;
      latestFetchedRef.current = next.fetchedModels;
      setShowApiKey(false);

      if (isNew || (models.length === 0 && (saved.fetchedModels ?? []).length === 0)) {
        setSavingProv(false);
        await handleFetchModels(saved.id, true);
        return;
      }

      notify({ tone: "ok", message: `已保存模型服务「${saved.name}」` });
    } catch (err) {
      notify({ tone: "danger", message: "保存模型服务失败", detail: formatErrorMessage(err) });
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
          message: "模型服务连接测试失败",
          detail: formatErrorMessage(result.message),
        });
      }
    } catch (err) {
      const msg = formatErrorMessage(err);
      setTestResults((prev) => ({
        ...prev,
        [id]: { ok: false, latencyMs: 0, modelUsed: "", message: msg },
      }));
      notify({ tone: "danger", message: "连接测试出错", detail: msg });
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
      const blank = createBlankProvider();
      setProvForm(blank);
      latestEnabledRef.current = [];
      latestFetchedRef.current = [];
      notify({ tone: "ok", message: "已删除模型服务" });
    } catch (err) {
      notify({ tone: "danger", message: "删除模型服务失败", detail: formatErrorMessage(err) });
    } finally {
      setSavingProv(false);
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
    persistEnabledModels([...latestEnabledRef.current, ...visibleModels]);
  };

  const handleClearEnabled = () => {
    if (latestEnabledRef.current.length === 0) return;
    persistEnabledModels([]);
  };

  return (
    <div className="flex min-h-0 flex-1 overflow-hidden">
      {/* Provider List */}
      <div className="flex w-52 shrink-0 flex-col border-r border-border-subtle bg-surface/40 p-2.5">
        <button
          type="button"
          onClick={startNewProvider}
          className={`mb-2 flex items-center justify-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-xs font-medium transition-colors ${
            selectedProviderId === "new"
              ? "border-accent bg-accent-soft text-accent"
              : "border-dashed border-border-strong text-ink-secondary hover:border-accent hover:text-accent"
          }`}
        >
          <Plus className="h-3.5 w-3.5" />
          <span>新建服务</span>
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
                <div className="flex items-center justify-between gap-1 text-xs text-ink-muted">
                  <span className="truncate">
                    {(p.models?.length ?? 0) > 0
                      ? `已启用 ${p.models.length} 个模型`
                      : PROVIDER_PROTOCOLS.find((x) => x.value === p.protocol)?.label ??
                        p.protocol}
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
          {provForm.id <= 0 && (
            <div className="rounded-lg border border-border-subtle bg-surface/60 px-3.5 py-2.5">
              <div className="mb-2 text-xs font-medium text-ink-secondary">
                常用服务快速填充（点击后只需填密钥）
              </div>
              <div className="flex flex-wrap gap-1.5">
                {VENDOR_PRESETS.map((preset) => {
                  const active =
                    provForm.name === preset.name &&
                    provForm.baseUrl === preset.baseUrl;
                  return (
                    <button
                      key={preset.label}
                      type="button"
                      title={preset.hint}
                      onClick={() =>
                        setProvForm((prev) => ({
                          ...prev,
                          name: preset.name,
                          protocol: preset.protocol,
                          baseUrl: preset.baseUrl,
                        }))
                      }
                      className={`rounded-md border px-2.5 py-1 text-xs font-medium transition-colors ${
                        active
                          ? "border-accent bg-accent-soft text-accent"
                          : "border-border-subtle bg-canvas text-ink-secondary hover:border-border-strong hover:text-ink-primary"
                      }`}
                    >
                      {preset.label}
                    </button>
                  );
                })}
              </div>
            </div>
          )}

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <label className="mb-1 block text-xs text-ink-secondary">
                服务名称
              </label>
              <input
                type="text"
                value={provForm.name}
                onChange={(e) =>
                  setProvForm({ ...provForm, name: e.target.value })
                }
                placeholder="如：DeepSeek、OpenRouter"
                className="w-full rounded-lg border border-border-subtle bg-canvas px-3 py-1.5 text-[13px] text-ink-primary focus:border-accent focus:outline-none"
              />
            </div>
            <div>
              <label className="mb-1 block text-xs text-ink-secondary">
                接口类型
              </label>
              <select
                value={provForm.protocol}
                onChange={(e) => handleProtocolChange(e.target.value as ProviderProtocol)}
                className="w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-[13px] text-ink-primary focus:border-accent focus:outline-none"
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
              接口地址
            </label>
            <input
              type="text"
              value={provForm.baseUrl}
              onChange={(e) =>
                setProvForm({ ...provForm, baseUrl: e.target.value })
              }
              placeholder="https://api.deepseek.com/v1"
              className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-3 py-1.5 text-[13px] text-ink-primary focus:border-accent focus:outline-none"
            />
          </div>

          <div>
            <div className="mb-1 flex items-center justify-between gap-2">
              <label className="text-xs text-ink-secondary">密钥（API Key）</label>
              {provForm.id > 0 && provForm.hasApiKey && (
                <span className="nib-mono rounded bg-ok-soft px-1.5 py-0.5 text-xs text-ok">
                  已保存 {provForm.apiKeyMasked}
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
                    : "输入密钥（本地模型可留空）"
                }
                autoComplete="off"
                className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas py-1.5 pr-9 pl-3 text-[13px] text-ink-primary placeholder:font-ui placeholder:text-ink-muted focus:border-accent focus:outline-none"
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
                <div className="border-b border-warn/25 bg-warn-soft/70 px-3 py-1.5 text-xs text-warn">
                  开启下方开关即可启用模型，已启用的模型可在「模型分配」中选用。
                </div>
              )}

            {/* Candidate Model Rows */}
            <div className="max-h-52 overflow-y-auto divide-y divide-border-subtle/50">
              {fetchingModelsId === provForm.id && allCandidateModels.length === 0 ? (
                <div className="flex items-center justify-center gap-2 py-6 text-xs text-ink-muted">
                  <Loader2 className="h-3.5 w-3.5 animate-spin text-accent" />
                  <span>正在拉取可用模型列表…</span>
                </div>
              ) : visibleModels.length === 0 ? (
                <div className="px-3 py-5 text-center text-xs text-ink-muted">
                  {allCandidateModels.length === 0
                    ? provForm.id > 0
                      ? "暂无候选模型，点击右上角「拉取模型」或在下方手动添加"
                      : "保存服务后会自动拉取可用模型，也可先在下方手动添加"
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
                          <span className="shrink-0 rounded bg-elevated px-1.5 py-0.5 text-[10px] text-ink-muted">
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
                          className={`inline-block h-3 w-3 rounded-full bg-canvas transition-transform ${
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
                  if (e.key === "Enter" && !isIMEComposing(e)) {
                    e.preventDefault();
                    handleAddCustomModel();
                  }
                }}
                placeholder="手动添加模型名称（回车加入并启用）"
                className="nib-mono min-w-0 flex-1 rounded border border-border-subtle bg-canvas px-2.5 py-1 text-xs text-ink-primary placeholder:font-ui placeholder:text-ink-muted focus:border-accent"
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
            <span className="text-xs text-ink-primary">启用此模型服务</span>
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
                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    disabled={savingProv}
                    onClick={() => void handleDeleteProvider()}
                    className="rounded-lg border border-danger/40 bg-danger-soft px-2.5 py-1 text-xs font-medium text-danger transition-colors hover:bg-danger/20"
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
                  className="flex items-center gap-1 rounded-lg px-2 py-1 text-xs text-ink-muted transition-colors hover:bg-danger-soft hover:text-danger"
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
                  className="flex items-center gap-1.5 rounded-lg border border-border-subtle bg-surface px-3 py-1.5 text-xs font-medium text-ink-primary transition-colors hover:border-border-strong disabled:opacity-50"
                >
                  {testingId === provForm.id ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin text-accent" />
                  ) : (
                    <Wifi className="h-3.5 w-3.5 text-accent" />
                  )}
                  <span>
                    {testingId === provForm.id ? "连接中…" : "测试连接"}
                  </span>
                </button>
              )}

              <button
                type="button"
                disabled={savingProv || (provForm.id > 0 && fetchingModelsId === provForm.id)}
                onClick={() => void handleSaveProvider()}
                className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
              >
                <Save className="h-3.5 w-3.5" />
                <span>
                  {savingProv
                    ? "保存中…"
                    : provForm.id > 0 && fetchingModelsId === provForm.id
                    ? "拉取模型中…"
                    : "保存"}
                </span>
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
