import { useEffect, useMemo, useRef, useState } from "react";
import {
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
  type AutoBackupInfo,
  type ModelPricing,
  type ModelRoute,
  type ProviderProtocol,
  type ProviderPublic,
  type ProviderTestResult,
  type TrashItem,
} from "../mygo";
import { useApp, type SettingsTab } from "../stores/AppContext";
import {
  PIPELINE_STAGES,
  PROVIDER_PROTOCOLS,
  formatErrorMessage,
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
    proseFontSize,
    setProseFontSize,
    proseLineHeight,
    setProseLineHeight,
    proseMaxWidth,
    setProseMaxWidth,
    resetTypography,
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
  const [customModelStages, setCustomModelStages] = useState<Set<string>>(new Set());

  dirtyStagesRef.current = dirtyStages;

  // Workspace / Active Book settings state
  const [bookExportDir, setBookExportDir] = useState("");
  const [bookTargetWords, setBookTargetWords] = useState("2500");
  const [bookPovMode, setBookPovMode] = useState("third_limited");
  const [bookPacingStyle, setBookPacingStyle] = useState("");
  const [bookWritingPrompt, setBookWritingPrompt] = useState("");
  const [bookDailyGoal, setBookDailyGoal] = useState("4000");
  const [savingBookSettings, setSavingBookSettings] = useState(false);
  const [exportingFormat, setExportingFormat] = useState<"txt" | "epub" | "db" | null>(null);

  // Auto backup & trash state
  const [autoBackups, setAutoBackups] = useState<AutoBackupInfo[]>([]);
  const [backupsLoading, setBackupsLoading] = useState(false);
  const [restoringBackupPath, setRestoringBackupPath] = useState<string | null>(null);
  const [trashItems, setTrashItems] = useState<TrashItem[]>([]);
  const [trashLoading, setTrashLoading] = useState(false);
  const [restoringTrashId, setRestoringTrashId] = useState<number | null>(null);
  const [clearingTrash, setClearingTrash] = useState(false);

  const handleExportTXT = async () => {
    if (!activeBook || exportingFormat) return;
    setExportingFormat("txt");
    try {
      const savedPath = await LibraryService.exportBookTXT(activeBook.id);
      if (savedPath) {
        notify({ tone: "ok", message: "已导出整本 TXT", detail: savedPath });
      }
    } catch (err) {
      notify({ tone: "danger", message: "导出 TXT 失败", detail: formatErrorMessage(err) });
    } finally {
      setExportingFormat(null);
    }
  };

  const handleExportEPUB = async () => {
    if (!activeBook || exportingFormat) return;
    setExportingFormat("epub");
    try {
      const savedPath = await LibraryService.exportBookEPUB(activeBook.id);
      if (savedPath) {
        notify({ tone: "ok", message: "已导出 EPUB 电子书", detail: savedPath });
      }
    } catch (err) {
      notify({ tone: "danger", message: "导出 EPUB 失败", detail: formatErrorMessage(err) });
    } finally {
      setExportingFormat(null);
    }
  };

  const handleBackupDatabase = async () => {
    if (exportingFormat) return;
    setExportingFormat("db");
    try {
      const savedPath = await SettingsService.backupDatabase();
      if (savedPath) {
        notify({ tone: "ok", message: "已安全备份全部书库数据", detail: savedPath });
      }
    } catch (err) {
      notify({ tone: "danger", message: "备份数据库失败", detail: formatErrorMessage(err) });
    } finally {
      setExportingFormat(null);
    }
  };

  const loadAutoBackups = async () => {
    setBackupsLoading(true);
    try {
      setAutoBackups(await SettingsService.listAutoBackups());
    } catch (err) {
      notify({ tone: "danger", message: "加载自动备份失败", detail: formatErrorMessage(err) });
    } finally {
      setBackupsLoading(false);
    }
  };

  const loadTrash = async () => {
    setTrashLoading(true);
    try {
      setTrashItems(await SettingsService.listTrash());
    } catch (err) {
      notify({ tone: "danger", message: "加载回收站失败", detail: formatErrorMessage(err) });
    } finally {
      setTrashLoading(false);
    }
  };

  useEffect(() => {
    if (!settingsOpen || tab !== "workspace") return;
    void loadAutoBackups();
    void loadTrash();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settingsOpen, tab]);

  const handleRestoreBackup = async (backup: AutoBackupInfo) => {
    const confirmed = window.confirm(
      `恢复后会覆盖当前书库的全部数据。系统会先自动备份当前数据库，确认恢复「${backup.name}」吗？`
    );
    if (!confirmed) return;
    setRestoringBackupPath(backup.path);
    try {
      const preRestore = await SettingsService.restoreDatabaseFromFile(backup.path);
      notify({ tone: "ok", message: "书库已恢复，正在重新载入…", detail: preRestore });
      setTimeout(() => window.location.reload(), 500);
    } catch (err) {
      notify({ tone: "danger", message: "恢复书库失败", detail: formatErrorMessage(err) });
    } finally {
      setRestoringBackupPath(null);
    }
  };

  const handleRestoreFromFile = async () => {
    try {
      const path = await SettingsService.chooseBackupFile();
      if (!path) return;
      const confirmed = window.confirm("恢复后会覆盖当前书库的全部数据，系统会先自动备份当前数据库。确认继续吗？");
      if (!confirmed) return;
      setRestoringBackupPath(path);
      const preRestore = await SettingsService.restoreDatabaseFromFile(path);
      notify({ tone: "ok", message: "书库已恢复，正在重新载入…", detail: preRestore });
      setTimeout(() => window.location.reload(), 500);
    } catch (err) {
      notify({ tone: "danger", message: "恢复书库失败", detail: formatErrorMessage(err) });
    } finally {
      setRestoringBackupPath(null);
    }
  };

  const handleRestoreTrash = async (item: TrashItem) => {
    setRestoringTrashId(item.id);
    try {
      const result = await SettingsService.restoreTrash(item.id);
      await refreshBooks();
      await loadTrash();
      notify({ tone: "ok", message: result.message || `已恢复「${item.title}」` });
    } catch (err) {
      notify({ tone: "danger", message: "恢复失败", detail: formatErrorMessage(err) });
    } finally {
      setRestoringTrashId(null);
    }
  };

  const handleDeleteTrash = async (item: TrashItem) => {
    try {
      await SettingsService.deleteTrash(item.id);
      await loadTrash();
    } catch (err) {
      notify({ tone: "danger", message: "删除回收站条目失败", detail: formatErrorMessage(err) });
    }
  };

  const handleClearTrash = async () => {
    if (trashItems.length === 0) return;
    const confirmed = window.confirm("清空后回收站内的内容将无法再恢复，确认清空吗？");
    if (!confirmed) return;
    setClearingTrash(true);
    try {
      const count = await SettingsService.clearTrash();
      await loadTrash();
      notify({ tone: "ok", message: `已清空回收站（${count} 项）` });
    } catch (err) {
      notify({ tone: "danger", message: "清空回收站失败", detail: formatErrorMessage(err) });
    } finally {
      setClearingTrash(false);
    }
  };

  const formatFileSize = (bytes: number) => {
    if (bytes >= 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`;
    if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
    if (bytes >= 1024) return `${(bytes / 1024).toFixed(0)} KB`;
    return `${bytes} B`;
  };

  // Model pricing state
  const [pricingList, setPricingList] = useState<ModelPricing[]>([]);
  const [pricingProvider, setPricingProvider] = useState("");
  const [pricingModel, setPricingModel] = useState("");
  const [pricingInput, setPricingInput] = useState("");
  const [pricingOutput, setPricingOutput] = useState("");
  const [pricingCurrency, setPricingCurrency] = useState<"CNY" | "USD">("CNY");
  const [savingPricing, setSavingPricing] = useState(false);

  const loadPricing = async () => {
    try {
      const list = await SettingsService.listModelPricing();
      setPricingList(list);
    } catch {
      // ignore
    }
  };

  useEffect(() => {
    if (!settingsOpen || tab !== "routes") return;
    void loadPricing();
  }, [settingsOpen, tab]);

  const handleSavePricing = async () => {
    if (!pricingProvider.trim() || !pricingModel.trim()) {
      notify({ tone: "warn", message: "请填写模型服务名称与模型 ID" });
      return;
    }
    const inPrice = Number.parseFloat(pricingInput) || 0;
    const outPrice = Number.parseFloat(pricingOutput) || 0;
    if (inPrice < 0 || outPrice < 0) {
      notify({ tone: "warn", message: "单价不能为负数" });
      return;
    }
    setSavingPricing(true);
    try {
      await SettingsService.upsertModelPricing({
        providerName: pricingProvider.trim(),
        modelName: pricingModel.trim(),
        inputPricePerM: inPrice,
        outputPricePerM: outPrice,
        currency: pricingCurrency,
      });
      await loadPricing();
      setPricingModel("");
      setPricingInput("");
      setPricingOutput("");
      notify({ tone: "ok", message: "模型单价已保存，首页将自动计算估算花费" });
    } catch (err) {
      notify({ tone: "danger", message: "保存单价失败", detail: formatErrorMessage(err) });
    } finally {
      setSavingPricing(false);
    }
  };

  const handleDeletePricing = async (providerName: string, modelName: string) => {
    try {
      await SettingsService.deleteModelPricing(providerName, modelName);
      await loadPricing();
    } catch (err) {
      notify({ tone: "danger", message: "删除单价失败", detail: formatErrorMessage(err) });
    }
  };

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
      setCustomModelStages(new Set());
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

  // Sync active book writing profile & export settings
  useEffect(() => {
    if (!settingsOpen) return;
    setBookExportDir(activeBook?.exportDir ?? "");
    setBookTargetWords(String(activeBook?.targetChapterWords ?? 2500));
    setBookPovMode(activeBook?.povMode || "third_limited");
    setBookPacingStyle(activeBook?.pacingStyle ?? "");
    setBookWritingPrompt(activeBook?.writingPrompt ?? "");
    setBookDailyGoal(String(activeBook?.dailyWordGoal ?? 4000));
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
    if (!Number.isFinite(draft.temperature) || draft.temperature < 0 || draft.temperature > 2) {
      notify({ tone: "warn", message: "创意度需在 0.0 到 2.0 之间" });
      return;
    }
    if (!Number.isFinite(draft.maxTokens) || draft.maxTokens < 256 || draft.maxTokens > 200000) {
      notify({ tone: "warn", message: "输出上限需在 256 到 200,000 Token 之间" });
      return;
    }
    setSavingStage(stage);
    try {
      const saved = await SettingsService.saveModelRoute({
        stage: draft.stage,
        providerId: draft.providerId && draft.providerId > 0 ? draft.providerId : undefined,
        modelName: draft.modelName.trim(),
        fallbackProviderId:
          draft.fallbackProviderId && draft.fallbackProviderId > 0
            ? draft.fallbackProviderId
            : undefined,
        fallbackModelName: (draft.fallbackModelName ?? "").trim(),
        temperature: draft.temperature,
        maxTokens: draft.maxTokens,
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
      notify({ tone: "ok", message: `已保存「${stageLabel}」配置` });
    } catch (err) {
      notify({ tone: "danger", message: "保存模型分配失败", detail: formatErrorMessage(err) });
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
      notify({ tone: "danger", message: "选择导出文件夹失败", detail: formatErrorMessage(err) });
    }
  };

  const handleSaveBookSettings = async () => {
    if (!activeBook) return;
    const parsedWords = Number.parseInt(bookTargetWords, 10);
    if (!Number.isFinite(parsedWords) || parsedWords < 300 || parsedWords > 50000) {
      notify({ tone: "warn", message: "每章目标字数需在 300 到 50,000 字之间" });
      return;
    }
    const parsedDailyGoal = Number.parseInt(bookDailyGoal, 10);
    if (!Number.isFinite(parsedDailyGoal) || parsedDailyGoal < 100 || parsedDailyGoal > 200000) {
      notify({ tone: "warn", message: "日更目标需在 100 到 200,000 字之间" });
      return;
    }
    if (bookWritingPrompt.length > 4000) {
      notify({ tone: "warn", message: "固定写作指令不能超过 4,000 字" });
      return;
    }

    setSavingBookSettings(true);
    try {
      await LibraryService.updateBook({
        id: activeBook.id,
        title: activeBook.title,
        genre: activeBook.genre,
        premise: activeBook.premise,
        goldenFinger: activeBook.goldenFinger,
        targetChapterWords: parsedWords,
        exportDir: bookExportDir.trim(),
        povMode: bookPovMode,
        pacingStyle: bookPacingStyle.trim(),
        writingPrompt: bookWritingPrompt.trim(),
        dailyWordGoal: parsedDailyGoal,
      });
      await refreshBooks(activeBook.id);
      notify({ tone: "ok", message: "书籍设置已保存" });
    } catch (err) {
      notify({ tone: "danger", message: "保存设置失败", detail: formatErrorMessage(err) });
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
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/55 p-4"
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
              className={`flex items-center gap-2 rounded-lg px-2.5 py-2 text-left text-[13px] font-medium transition-colors ${
                tab === "providers"
                  ? "bg-accent-soft text-accent"
                  : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
              }`}
            >
              <KeyRound className="h-3.5 w-3.5" />
              <span>模型服务</span>
            </button>
            <button
              type="button"
              onClick={() => setTab("routes")}
              className={`flex items-center gap-2 rounded-lg px-2.5 py-2 text-left text-[13px] font-medium transition-colors ${
                tab === "routes"
                  ? "bg-accent-soft text-accent"
                  : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
              }`}
            >
              <GitBranch className="h-3.5 w-3.5" />
              <span>模型分配</span>
            </button>
            <button
              type="button"
              onClick={() => setTab("workspace")}
              className={`flex items-center gap-2 rounded-lg px-2.5 py-2 text-left text-[13px] font-medium transition-colors ${
                tab === "workspace"
                  ? "bg-accent-soft text-accent"
                  : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
              }`}
            >
              <Sliders className="h-3.5 w-3.5" />
              <span>写作与备份</span>
            </button>
          </nav>
        </aside>

        {/* Right Panel */}
        <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
          <div className="flex items-center justify-between border-b border-border-subtle px-5 py-3">
            <h2 className="text-sm font-medium text-ink-primary">
              {tab === "providers"
                ? "模型服务"
                : tab === "routes"
                ? "各环节模型分配"
                : "写作规范、阅读排版与本地备份"}
            </h2>
            <button
              type="button"
              onClick={closeSettings}
              aria-label="关闭设置"
              className="rounded-lg p-1 text-ink-muted transition-colors hover:bg-surface hover:text-ink-primary"
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
                              : PROVIDER_PROTOCOLS.find((x) => x.value === p.protocol)
                                  ?.label ?? p.protocol}
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
                        onChange={(e) => handleProtocolChange(e.target.value)}
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
          )}

          {/* Tab 2: 5-Stage Model Assignment */}
          {tab === "routes" && (
            <div className="flex-1 overflow-y-auto px-5 py-4">
              {providers.length === 0 && (
                <div className="mb-4 flex items-center justify-between rounded-lg border border-warn/35 bg-warn-soft px-3.5 py-2.5 text-xs text-warn">
                  <span>请先添加模型服务，再为各写作环节指定模型。</span>
                  <button
                    type="button"
                    onClick={() => setTab("providers")}
                    className="font-medium underline underline-offset-2"
                  >
                    去设置
                  </button>
                </div>
              )}

              <div className="divide-y divide-border-subtle">
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
                      className="py-4 first:pt-1 last:pb-1"
                    >
                      <div className="mb-2.5 flex items-center justify-between gap-2">
                        <div className="flex items-baseline gap-2.5">
                          <span className="text-[13px] font-medium text-ink-primary">
                            {stageDef.label}
                          </span>
                          <span className="text-xs text-ink-muted">
                            {stageDef.desc}
                          </span>
                        </div>

                        <button
                          type="button"
                          disabled={isSaving || !isDirty}
                          onClick={() => void handleSaveRoute(stageDef.stage)}
                          className="flex items-center gap-1 rounded-lg border border-accent/35 bg-accent-soft px-2.5 py-1 text-xs font-medium text-accent transition-colors hover:border-accent disabled:opacity-35"
                        >
                          <Save className="h-3 w-3" />
                          <span>{isSaving ? "保存中…" : "保存"}</span>
                        </button>
                      </div>

                      <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-[170px_1fr_92px_104px]">
                        {/* Provider Select */}
                        <div>
                          <label className="mb-1 block text-xs text-ink-muted">
                            模型服务
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
                            className="w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                          >
                            <option value="">未选择</option>
                            {providers.map((p) => (
                              <option key={p.id} value={p.id}>
                                {p.name}
                              </option>
                            ))}
                          </select>
                        </div>

                        {/* Model Selector */}
                        <div>
                          <div className="mb-1 flex items-center justify-between gap-2">
                            <label className="block text-xs text-ink-muted">
                              模型
                            </label>
                            {selectedProv && modelSuggestions.length === 0 ? (
                              <button
                                type="button"
                                onClick={() => {
                                  selectProvider(selectedProv);
                                  setTab("providers");
                                }}
                                className="text-xs text-accent transition-opacity hover:opacity-80"
                              >
                                去启用模型
                              </button>
                            ) : (
                              modelSuggestions.length > 0 &&
                              customModelStages.has(stageDef.stage) && (
                                <button
                                  type="button"
                                  onClick={() =>
                                    setCustomModelStages((prev) => {
                                      const next = new Set(prev);
                                      next.delete(stageDef.stage);
                                      return next;
                                    })
                                  }
                                  className="text-xs text-accent transition-opacity hover:opacity-80"
                                >
                                  从列表选择
                                </button>
                              )
                            )}
                          </div>

                          {modelSuggestions.length > 0 &&
                          !customModelStages.has(stageDef.stage) ? (
                            <select
                              value={route.modelName}
                              onChange={(e) => {
                                const val = e.target.value;
                                if (val === "__custom__") {
                                  setCustomModelStages((prev) => {
                                    const next = new Set(prev);
                                    next.add(stageDef.stage);
                                    return next;
                                  });
                                  return;
                                }
                                updateRouteField(stageDef.stage, {
                                  modelName: val,
                                });
                              }}
                              className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                            >
                              {!route.modelName && <option value="">选择模型</option>}
                              {route.modelName &&
                                !modelSuggestions.includes(route.modelName) && (
                                  <option value={route.modelName}>
                                    {route.modelName}（当前值）
                                  </option>
                                )}
                              {modelSuggestions.map((m) => (
                                <option key={m} value={m}>
                                  {m}
                                </option>
                              ))}
                              <option value="__custom__">手动输入其他模型…</option>
                            </select>
                          ) : (
                            <input
                              type="text"
                              value={route.modelName}
                              onChange={(e) =>
                                updateRouteField(stageDef.stage, {
                                  modelName: e.target.value,
                                })
                              }
                              placeholder={
                                selectedProv && modelSuggestions.length === 0
                                  ? "该服务尚未启用模型，可手动输入或点右上角启用"
                                  : "输入模型 ID"
                              }
                              className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:font-ui placeholder:text-ink-muted focus:border-accent focus:outline-none"
                            />
                          )}
                        </div>

                        {/* Temperature */}
                        <div>
                          <label className="mb-1 block text-xs text-ink-muted">
                            创意度
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
                            className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-right text-xs text-ink-primary focus:border-accent focus:outline-none"
                          />
                        </div>

                        {/* Max Tokens */}
                        <div>
                          <label className="mb-1 block text-xs text-ink-muted">
                            输出上限
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
                            className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-right text-xs text-ink-primary focus:border-accent focus:outline-none"
                          />
                        </div>
                      </div>

                      {/* Optional Fallback Provider & Model */}
                      <div className="mt-2.5 grid grid-cols-1 gap-2.5 border-t border-border-subtle/70 pt-2.5 sm:grid-cols-2">
                        <div>
                          <label className="mb-1 block text-xs text-ink-muted">
                            备用服务（主模型限流或故障时自动切换）
                          </label>
                          <select
                            value={route.fallbackProviderId ?? ""}
                            onChange={(e) => {
                              const nextFbProvId = e.target.value
                                ? Number.parseInt(e.target.value, 10)
                                : undefined;
                              const nextFbProv = nextFbProvId
                                ? providerMap.get(nextFbProvId)
                                : undefined;
                              const nextFbModel = nextFbProvId
                                ? nextFbProv?.models?.[0] ?? route.fallbackModelName ?? ""
                                : "";
                              updateRouteField(stageDef.stage, {
                                fallbackProviderId: nextFbProvId,
                                fallbackModelName: nextFbModel,
                              });
                            }}
                            className="w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                          >
                            <option value="">不设备用</option>
                            {providers.map((p) => (
                              <option key={p.id} value={p.id}>
                                {p.name}
                              </option>
                            ))}
                          </select>
                        </div>

                        <div>
                          <label className="mb-1 block text-xs text-ink-muted">
                            备用模型
                          </label>
                          {(() => {
                            const fbProv = route.fallbackProviderId
                              ? providerMap.get(route.fallbackProviderId)
                              : undefined;
                            const fbModels = fbProv?.models ?? [];
                            if (fbModels.length > 0) {
                              return (
                                <select
                                  value={route.fallbackModelName ?? ""}
                                  onChange={(e) =>
                                    updateRouteField(stageDef.stage, {
                                      fallbackModelName: e.target.value,
                                    })
                                  }
                                  className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                                >
                                  <option value="">选择备用模型</option>
                                  {fbModels.map((m) => (
                                    <option key={m} value={m}>
                                      {m}
                                    </option>
                                  ))}
                                </select>
                              );
                            }
                            return (
                              <input
                                type="text"
                                disabled={!route.fallbackProviderId}
                                value={route.fallbackModelName ?? ""}
                                onChange={(e) =>
                                  updateRouteField(stageDef.stage, {
                                    fallbackModelName: e.target.value,
                                  })
                                }
                                placeholder={route.fallbackProviderId ? "输入备用模型 ID" : "请先选择备用服务"}
                                className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:font-ui placeholder:text-ink-muted focus:border-accent focus:outline-none disabled:opacity-50"
                              />
                            );
                          })()}
                        </div>
                      </div>
                    </div>
                  );
                })}

                {/* Custom Model Pricing Section */}
                <div className="mt-2 rounded-xl border border-border-subtle bg-surface/60 p-4">
                  <div className="mb-3 flex items-center justify-between gap-2">
                    <div>
                      <div className="text-sm font-medium text-ink-primary">模型单价设置（每百万 Token）</div>
                      <div className="text-xs text-ink-muted">按你使用的官方或中转价格填写，首页用量面板将自动换算花费</div>
                    </div>
                  </div>

                  <div className="grid grid-cols-1 gap-2 sm:grid-cols-[1fr_1fr_92px_92px_72px_auto]">
                    <select
                      value={pricingProvider}
                      onChange={(e) => {
                        setPricingProvider(e.target.value);
                        const p = providers.find((item) => item.name === e.target.value);
                        if (p && p.models.length > 0 && !pricingModel) {
                          setPricingModel(p.models[0] ?? "");
                        }
                      }}
                      className="rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                    >
                      <option value="">选择服务</option>
                      {providers.map((p) => (
                        <option key={p.id} value={p.name}>
                          {p.name}
                        </option>
                      ))}
                    </select>
                    <input
                      type="text"
                      value={pricingModel}
                      onChange={(e) => setPricingModel(e.target.value)}
                      placeholder="模型 ID"
                      className="nib-mono rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:font-ui placeholder:text-ink-muted focus:border-accent focus:outline-none"
                    />
                    <input
                      type="number"
                      min={0}
                      step={0.1}
                      value={pricingInput}
                      onChange={(e) => setPricingInput(e.target.value)}
                      placeholder="输入/1M"
                      className="nib-mono rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-right text-xs text-ink-primary placeholder:font-ui placeholder:text-ink-muted focus:border-accent focus:outline-none"
                    />
                    <input
                      type="number"
                      min={0}
                      step={0.1}
                      value={pricingOutput}
                      onChange={(e) => setPricingOutput(e.target.value)}
                      placeholder="输出/1M"
                      className="nib-mono rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-right text-xs text-ink-primary placeholder:font-ui placeholder:text-ink-muted focus:border-accent focus:outline-none"
                    />
                    <select
                      value={pricingCurrency}
                      onChange={(e) => setPricingCurrency(e.target.value === "USD" ? "USD" : "CNY")}
                      className="rounded-lg border border-border-subtle bg-canvas px-2 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                    >
                      <option value="CNY">CNY ¥</option>
                      <option value="USD">USD $</option>
                    </select>
                    <button
                      type="button"
                      disabled={savingPricing}
                      onClick={() => void handleSavePricing()}
                      className="rounded-lg bg-accent px-3 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
                    >
                      保存单价
                    </button>
                  </div>

                  {pricingList.length > 0 && (
                    <div className="mt-3 divide-y divide-border-subtle rounded-lg border border-border-subtle bg-canvas">
                      {pricingList.map((pr) => (
                        <div
                          key={`${pr.providerName}-${pr.modelName}`}
                          className="flex items-center justify-between gap-2 px-3 py-2 text-xs"
                        >
                          <div className="flex min-w-0 items-center gap-2">
                            <span className="font-medium text-ink-primary">{pr.providerName}</span>
                            <span className="nib-mono truncate text-ink-secondary">{pr.modelName}</span>
                          </div>
                          <div className="flex shrink-0 items-center gap-3">
                            <span className="nib-mono text-ink-secondary">
                              输入 {pr.currency === "USD" ? "$" : "¥"}{pr.inputPricePerM} / 输出 {pr.currency === "USD" ? "$" : "¥"}{pr.outputPricePerM}
                            </span>
                            <button
                              type="button"
                              onClick={() => void handleDeletePricing(pr.providerName, pr.modelName)}
                              className="rounded p-1 text-ink-muted hover:text-danger"
                            >
                              <Trash2 className="h-3.5 w-3.5" />
                            </button>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
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

                    {/* 字号 / 行距 / 栏宽自由调节 */}
                    <div className="grid grid-cols-1 gap-3 border-t border-border-subtle pt-3 sm:grid-cols-3">
                      <div>
                        <div className="mb-1 flex items-center justify-between text-xs">
                          <span className="text-ink-secondary">正文字号</span>
                          <span className="nib-mono text-ink-primary">{proseFontSize}px</span>
                        </div>
                        <input
                          type="range"
                          min={14}
                          max={24}
                          step={1}
                          value={proseFontSize}
                          onChange={(e) => setProseFontSize(Number(e.target.value))}
                          className="w-full accent-[var(--accent)]"
                        />
                      </div>

                      <div>
                        <div className="mb-1 flex items-center justify-between text-xs">
                          <span className="text-ink-secondary">行距倍数</span>
                          <span className="nib-mono text-ink-primary">{proseLineHeight.toFixed(2)}</span>
                        </div>
                        <input
                          type="range"
                          min={1.5}
                          max={2.3}
                          step={0.04}
                          value={proseLineHeight}
                          onChange={(e) => setProseLineHeight(Number(e.target.value))}
                          className="w-full accent-[var(--accent)]"
                        />
                      </div>

                      <div>
                        <div className="mb-1 flex items-center justify-between text-xs">
                          <span className="text-ink-secondary">正文栏宽</span>
                          <span className="nib-mono text-ink-primary">{proseMaxWidth}px</span>
                        </div>
                        <input
                          type="range"
                          min={580}
                          max={960}
                          step={20}
                          value={proseMaxWidth}
                          onChange={(e) => setProseMaxWidth(Number(e.target.value))}
                          className="w-full accent-[var(--accent)]"
                        />
                      </div>
                    </div>

                    <div className="flex items-center justify-end">
                      <button
                        type="button"
                        onClick={resetTypography}
                        className="text-[11px] text-ink-muted transition-colors hover:text-accent"
                      >
                        恢复默认排版参数
                      </button>
                    </div>

                    <div className="mt-1 rounded-md border border-border-subtle bg-canvas px-4 py-3">
                      <div className="nib-prose-column">
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
                        当前书籍写作规范与导出 · 《{activeBook.title}》
                      </span>
                    </div>

                    <div className="flex flex-col gap-3 rounded-lg border border-border-subtle bg-surface/50 p-3.5">
                      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                        <div>
                          <label className="mb-1 block text-xs text-ink-secondary">
                            叙事视角
                          </label>
                          <select
                            value={bookPovMode}
                            onChange={(e) => setBookPovMode(e.target.value)}
                            className="w-full rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                          >
                            <option value="third_limited">第三人称限知</option>
                            <option value="first_person">第一人称</option>
                            <option value="third_omniscient">第三人称全知</option>
                            <option value="multi_pov">多视角群像</option>
                          </select>
                        </div>

                        <div>
                          <label className="mb-1 block text-xs text-ink-secondary">
                            节奏风格
                          </label>
                          <input
                            type="text"
                            value={bookPacingStyle}
                            onChange={(e) => setBookPacingStyle(e.target.value)}
                            placeholder="如：番茄快节奏"
                            className="w-full rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                          />
                        </div>

                        <div>
                          <label className="mb-1 block text-xs text-ink-secondary">
                            日更目标（字）
                          </label>
                          <input
                            type="number"
                            min={100}
                            max={200000}
                            step={500}
                            value={bookDailyGoal}
                            onChange={(e) => setBookDailyGoal(e.target.value)}
                            className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                          />
                        </div>
                      </div>

                      <div>
                        <label className="mb-1 block text-xs text-ink-secondary">
                          固定写作指令（本书每次生成都会注入）
                        </label>
                        <textarea
                          rows={3}
                          maxLength={4000}
                          value={bookWritingPrompt}
                          onChange={(e) => setBookWritingPrompt(e.target.value)}
                          placeholder="如：每章至少一次信息差反转；主角不降智；对话占比不低于三成。"
                          className="w-full resize-y rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                        />
                      </div>

                      <div className="border-t border-border-subtle pt-3">
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

                    <div className="mt-3 flex flex-wrap items-center gap-2">
                      <button
                        type="button"
                        disabled={exportingFormat !== null}
                        onClick={() => void handleExportTXT()}
                        className="rounded-md border border-border-subtle bg-surface px-3 py-1.5 text-xs font-medium text-ink-primary transition-colors hover:border-border-strong disabled:opacity-40"
                      >
                        {exportingFormat === "txt" ? "导出中…" : "导出整本 TXT"}
                      </button>
                      <button
                        type="button"
                        disabled={exportingFormat !== null}
                        onClick={() => void handleExportEPUB()}
                        className="rounded-md border border-border-subtle bg-surface px-3 py-1.5 text-xs font-medium text-ink-primary transition-colors hover:border-border-strong disabled:opacity-40"
                      >
                        {exportingFormat === "epub" ? "打包中…" : "导出 EPUB 电子书"}
                      </button>
                      <button
                        type="button"
                        onClick={() => {
                          const targetBookId = activeBook.id;
                          closeSettings();
                          window.dispatchEvent(
                            new CustomEvent("nib:reverse-extract", { detail: { bookId: targetBookId } })
                          );
                        }}
                        className="rounded-md border border-accent/40 bg-accent-soft px-3 py-1.5 text-xs font-medium text-accent transition-colors hover:border-accent"
                      >
                        AI 反推旧稿设定
                      </button>
                    </div>
                  </section>
                )}

                {/* Database Backup */}
                <section className="border-t border-border-subtle pt-4">
                  <div className="mb-2 text-xs font-medium text-ink-secondary">
                    本地数据安全备份
                  </div>
                  <div className="flex items-center justify-between gap-3 rounded-lg border border-border-subtle bg-surface/50 p-3.5">
                    <div className="text-xs text-ink-secondary">
                      将所有书籍、章节正文、历史版本快照与设定记忆完整打包备份为一个本地 .db 文件
                    </div>
                    <button
                      type="button"
                      disabled={exportingFormat !== null}
                      onClick={() => void handleBackupDatabase()}
                      className="shrink-0 rounded-md border border-border-subtle bg-elevated px-3.5 py-1.5 text-xs font-medium text-ink-primary transition-colors hover:border-border-strong disabled:opacity-40"
                    >
                      {exportingFormat === "db" ? "备份中…" : "一键备份书库"}
                    </button>
                  </div>
                </section>

                {/* Auto Rolling Backups */}
                <section className="border-t border-border-subtle pt-4">
                  <div className="mb-2 flex items-center justify-between">
                    <span className="text-xs font-medium text-ink-secondary">
                      自动备份（最近 7 份）
                    </span>
                    <div className="flex items-center gap-3">
                      <button
                        type="button"
                        disabled={restoringBackupPath !== null}
                        onClick={() => void handleRestoreFromFile()}
                        className="text-xs text-ink-muted transition-colors hover:text-accent disabled:opacity-50"
                      >
                        从文件恢复
                      </button>
                      <button
                        type="button"
                        disabled={backupsLoading}
                        onClick={() => void loadAutoBackups()}
                        className="flex items-center gap-1 text-xs text-ink-muted transition-colors hover:text-accent disabled:opacity-50"
                      >
                        <RefreshCw className={`h-3 w-3 ${backupsLoading ? "animate-spin" : ""}`} />
                        刷新
                      </button>
                    </div>
                  </div>
                  <div className="overflow-hidden rounded-lg border border-border-subtle bg-surface/50">
                    {autoBackups.length === 0 ? (
                      <div className="px-4 py-3 text-xs text-ink-muted">
                        {backupsLoading ? "正在加载…" : "暂无自动备份，下次启动 Nib 会创建当日备份。"}
                      </div>
                    ) : (
                      <div className="divide-y divide-border-subtle">
                        {autoBackups.map((backup) => (
                          <div key={backup.path} className="flex items-center justify-between gap-3 px-4 py-2.5">
                            <div className="min-w-0">
                              <div className="nib-mono truncate text-xs text-ink-primary">{backup.name}</div>
                              <div className="mt-0.5 flex items-center gap-2 text-[11px] text-ink-muted">
                                <span className="nib-mono">{formatFileSize(backup.sizeBytes)}</span>
                                <span>{new Date(backup.modifiedAt).toLocaleString("zh-CN")}</span>
                              </div>
                            </div>
                            <button
                              type="button"
                              disabled={restoringBackupPath !== null}
                              onClick={() => void handleRestoreBackup(backup)}
                              className="shrink-0 rounded-md border border-border-subtle bg-elevated px-2.5 py-1 text-xs text-ink-primary transition-colors hover:border-border-strong disabled:opacity-40"
                            >
                              {restoringBackupPath === backup.path ? "恢复中…" : "恢复"}
                            </button>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                </section>

                {/* Trash */}
                <section className="border-t border-border-subtle pt-4">
                  <div className="mb-2 flex items-center justify-between">
                    <span className="text-xs font-medium text-ink-secondary">
                      回收站（保留 30 天）
                    </span>
                    {trashItems.length > 0 && (
                      <button
                        type="button"
                        disabled={clearingTrash}
                        onClick={() => void handleClearTrash()}
                        className="text-xs text-ink-muted transition-colors hover:text-danger disabled:opacity-50"
                      >
                        {clearingTrash ? "清空中…" : "清空回收站"}
                      </button>
                    )}
                  </div>
                  <div className="overflow-hidden rounded-lg border border-border-subtle bg-surface/50">
                    {trashItems.length === 0 ? (
                      <div className="px-4 py-3 text-xs text-ink-muted">
                        {trashLoading ? "正在加载…" : "回收站为空。删除书籍、分卷、章节或设定后会先进入这里。"}
                      </div>
                    ) : (
                      <div className="divide-y divide-border-subtle">
                        {trashItems.map((item) => (
                          <div key={item.id} className="flex items-center justify-between gap-3 px-4 py-2.5">
                            <div className="min-w-0">
                              <div className="flex items-center gap-2">
                                <span className="rounded bg-canvas px-1.5 py-0.5 text-[10px] text-ink-secondary">
                                  {item.itemType === "book"
                                    ? "书籍"
                                    : item.itemType === "volume"
                                      ? "分卷"
                                      : item.itemType === "chapter"
                                        ? "章节"
                                        : "设定"}
                                </span>
                                <span className="truncate text-xs font-medium text-ink-primary">{item.title}</span>
                              </div>
                              <div className="mt-0.5 text-[11px] text-ink-muted">
                                {item.summary ? `${item.summary} · ` : ""}
                                {new Date(item.deletedAt).toLocaleString("zh-CN")} 删除
                              </div>
                            </div>
                            <div className="flex shrink-0 items-center gap-1.5">
                              <button
                                type="button"
                                disabled={restoringTrashId === item.id}
                                onClick={() => void handleRestoreTrash(item)}
                                className="rounded-md border border-border-subtle bg-elevated px-2.5 py-1 text-xs text-ink-primary transition-colors hover:border-border-strong disabled:opacity-40"
                              >
                                {restoringTrashId === item.id ? "恢复中…" : "恢复"}
                              </button>
                              <button
                                type="button"
                                onClick={() => void handleDeleteTrash(item)}
                                className="rounded-md p-1 text-ink-muted transition-colors hover:text-danger"
                                title="永久删除"
                                aria-label="永久删除"
                              >
                                <Trash2 className="h-3.5 w-3.5" />
                              </button>
                            </div>
                          </div>
                        ))}
                      </div>
                    )}
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
