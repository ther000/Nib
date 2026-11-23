import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ChevronDown, ChevronRight, Save, Sparkles, Trash2 } from "lucide-react";
import {
  SettingsService,
  type ModelPricing,
  type ModelRoute,
  type ProviderPublic,
} from "../../mygo";
import { useApp } from "../../stores/AppContext";
import { PIPELINE_STAGES, formatErrorMessage } from "../../lib/constants";

function buildRouteDefaults(modelRoutes: ModelRoute[]): Record<string, ModelRoute> {
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
}

interface RoutesPricingTabProps {
  onGoToProviders: () => void;
  onSelectProvider: (provider: ProviderPublic) => void;
}

export function RoutesPricingTab({
  onGoToProviders,
  onSelectProvider,
}: RoutesPricingTabProps) {
  const { providers, modelRoutes, refreshSettingsData, notify } = useApp();

  const [routeDrafts, setRouteDrafts] = useState<Record<string, ModelRoute>>(() =>
    buildRouteDefaults(modelRoutes)
  );
  const [dirtyStages, setDirtyStages] = useState<Set<string>>(() => new Set());
  const dirtyStagesRef = useRef<Set<string>>(new Set());
  const [savingStage, setSavingStage] = useState<string | null>(null);
  const [savingAll, setSavingAll] = useState(false);
  const [customModelStages, setCustomModelStages] = useState<Set<string>>(() => new Set());
  const [customFallbackStages, setCustomFallbackStages] = useState<Set<string>>(() => new Set());
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [showPricing, setShowPricing] = useState(false);

  // Quick unified-model selector state
  const [quickProviderId, setQuickProviderId] = useState<number>(() => {
    const draftRoute = modelRoutes.find((r) => r.stage === "draft");
    if (draftRoute?.providerId) return draftRoute.providerId;
    return providers.find((p) => p.enabled)?.id ?? providers[0]?.id ?? 0;
  });
  const [quickModelName, setQuickModelName] = useState<string>(() => {
    const draftRoute = modelRoutes.find((r) => r.stage === "draft");
    if (draftRoute?.modelName) return draftRoute.modelName;
    const firstProv = providers.find((p) => p.enabled) ?? providers[0];
    return firstProv?.models?.[0] ?? "";
  });
  const [applyingAll, setApplyingAll] = useState(false);

  const [pricingList, setPricingList] = useState<ModelPricing[]>([]);
  const [pricingProvider, setPricingProvider] = useState("");
  const [pricingModel, setPricingModel] = useState("");
  const [pricingInput, setPricingInput] = useState("");
  const [pricingOutput, setPricingOutput] = useState("");
  const [pricingCurrency, setPricingCurrency] = useState<"CNY" | "USD">("CNY");
  const [savingPricing, setSavingPricing] = useState(false);

  const providerMap = useMemo(() => {
    const m = new Map<number, ProviderPublic>();
    for (const p of providers) m.set(p.id, p);
    return m;
  }, [providers]);

  useEffect(() => {
    const freshMap = buildRouteDefaults(modelRoutes);
    setRouteDrafts((prev) => {
      const merged: Record<string, ModelRoute> = { ...prev };
      for (const def of PIPELINE_STAGES) {
        if (!dirtyStagesRef.current.has(def.stage) && freshMap[def.stage]) {
          merged[def.stage] = freshMap[def.stage]!;
        }
      }
      return merged;
    });
  }, [modelRoutes]);

  useEffect(() => {
    if (quickProviderId > 0 && providerMap.has(quickProviderId)) return;
    const fallbackProv = providers.find((p) => p.enabled) ?? providers[0];
    if (fallbackProv) {
      setQuickProviderId(fallbackProv.id);
      setQuickModelName(fallbackProv.models[0] ?? "");
    }
  }, [providers, providerMap, quickProviderId]);

  const loadPricing = useCallback(async () => {
    try {
      const list = await SettingsService.listModelPricing();
      setPricingList(list);
    } catch {
      // ignore
    }
  }, []);

  useEffect(() => {
    void loadPricing();
  }, [loadPricing]);

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

  const validateRoute = (draft: ModelRoute): string | null => {
    if (!Number.isFinite(draft.temperature) || draft.temperature < 0 || draft.temperature > 2) {
      return "创意度需在 0.0 到 2.0 之间";
    }
    if (!Number.isFinite(draft.maxTokens) || draft.maxTokens < 256 || draft.maxTokens > 200000) {
      return "输出上限需在 256 到 200,000 之间";
    }
    if (draft.providerId && draft.providerId > 0) {
      const p = providerMap.get(draft.providerId);
      if (p && !p.enabled) {
        return `主模型服务「${p.name}」已停用，请先启用或更换服务`;
      }
    }
    const fbModel = (draft.fallbackModelName ?? "").trim();
    if (draft.fallbackProviderId && draft.fallbackProviderId > 0) {
      const fbp = providerMap.get(draft.fallbackProviderId);
      if (fbp && !fbp.enabled) {
        return `备用模型服务「${fbp.name}」已停用，请先启用或更换服务`;
      }
      if (!fbModel) {
        return "已选择备用服务，请同时指定备用模型";
      }
      if (
        draft.providerId === draft.fallbackProviderId &&
        draft.modelName.trim().toLowerCase() === fbModel.toLowerCase()
      ) {
        return "备用模型与主模型完全相同，无法起到故障切换作用，请更换备用模型";
      }
    }
    return null;
  };

  const persistSingleRoute = async (stage: string, draft: ModelRoute) => {
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
  };

  const handleSaveRoute = async (stage: string) => {
    const draft = routeDrafts[stage];
    if (!draft) return;
    const errMsg = validateRoute(draft);
    if (errMsg) {
      notify({ tone: "warn", message: errMsg });
      return;
    }
    setSavingStage(stage);
    try {
      await persistSingleRoute(stage, draft);
      await refreshSettingsData();
      const stageLabel = PIPELINE_STAGES.find((s) => s.stage === stage)?.label ?? stage;
      notify({ tone: "ok", message: `已保存「${stageLabel}」配置` });
    } catch (err) {
      notify({ tone: "danger", message: "保存模型分配失败", detail: formatErrorMessage(err) });
    } finally {
      setSavingStage(null);
    }
  };

  const handleSaveAllDirty = async () => {
    const stages = Array.from(dirtyStagesRef.current);
    if (stages.length === 0) return;
    for (const stage of stages) {
      const draft = routeDrafts[stage];
      if (!draft) continue;
      const errMsg = validateRoute(draft);
      if (errMsg) {
        notify({ tone: "warn", message: errMsg });
        return;
      }
    }
    setSavingAll(true);
    try {
      for (const stage of stages) {
        const draft = routeDrafts[stage];
        if (draft) {
          await persistSingleRoute(stage, draft);
        }
      }
      await refreshSettingsData();
      notify({ tone: "ok", message: `已保存 ${stages.length} 个环节的模型分配` });
    } catch (err) {
      notify({ tone: "danger", message: "保存模型分配失败", detail: formatErrorMessage(err) });
    } finally {
      setSavingAll(false);
    }
  };

  const handleApplyToAll = async () => {
    if (!quickProviderId || !quickModelName.trim()) {
      notify({ tone: "warn", message: "请先选择模型服务与模型" });
      return;
    }
    const qp = providerMap.get(quickProviderId);
    if (qp && !qp.enabled) {
      notify({ tone: "warn", message: `模型服务「${qp.name}」已停用，请先启用或更换服务` });
      return;
    }
    setApplyingAll(true);
    try {
      for (const def of PIPELINE_STAGES) {
        const current = routeDrafts[def.stage];
        const next: ModelRoute = {
          stage: def.stage,
          providerId: quickProviderId,
          modelName: quickModelName.trim(),
          fallbackProviderId: current?.fallbackProviderId,
          fallbackModelName: current?.fallbackModelName ?? "",
          temperature: current?.temperature ?? def.defaultTemp,
          maxTokens: current?.maxTokens ?? def.defaultMaxTokens,
          updatedAt: current?.updatedAt ?? "",
        };
        await persistSingleRoute(def.stage, next);
      }
      await refreshSettingsData();
      notify({ tone: "ok", message: `全部写作环节已统一设为 ${quickModelName.trim()}` });
    } catch (err) {
      notify({ tone: "danger", message: "批量应用失败", detail: formatErrorMessage(err) });
    } finally {
      setApplyingAll(false);
    }
  };

  const handleSavePricing = async () => {
    if (!pricingProvider.trim() || !pricingModel.trim()) {
      notify({ tone: "warn", message: "请填写模型服务与模型名称" });
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
      notify({ tone: "ok", message: "模型单价已保存" });
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

  const quickProvider = quickProviderId ? providerMap.get(quickProviderId) : undefined;
  const quickModels = quickProvider?.models ?? [];

  return (
    <div className="flex-1 overflow-y-auto px-5 py-4">
      {providers.length === 0 ? (
        <div className="mb-4 flex items-center justify-between rounded-lg border border-warn/35 bg-warn-soft px-3.5 py-2.5 text-xs text-warn">
          <span>请先添加模型服务，再为各写作环节指定模型。</span>
          <button
            type="button"
            onClick={onGoToProviders}
            className="font-medium underline underline-offset-2"
          >
            去添加
          </button>
        </div>
      ) : (
        /* Quick Unified Mode Bar */
        <div className="mb-4 rounded-xl border border-accent/25 bg-accent-soft/30 p-3.5">
          <div className="mb-2 flex items-center justify-between gap-2">
            <div className="flex items-center gap-1.5 text-xs font-medium text-ink-primary">
              <Sparkles className="h-3.5 w-3.5 text-accent" />
              <span>一键统一模型（新手推荐）</span>
            </div>
            <span className="text-xs text-ink-secondary">
              全流程共用一个主力模型，免去逐项配置
            </span>
          </div>
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-[170px_1fr_auto]">
            <select
              aria-label="统一模型服务"
              value={quickProviderId || ""}
              onChange={(e) => {
                const nextId = Number(e.target.value) || 0;
                setQuickProviderId(nextId);
                const nextProv = providerMap.get(nextId);
                setQuickModelName(nextProv?.models?.[0] ?? "");
              }}
              className="rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent"
            >
              <option value="">选择模型服务</option>
              {providers.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.enabled ? p.name : `${p.name}（已停用）`}
                </option>
              ))}
            </select>
            {quickModels.length > 0 ? (
              <select
                aria-label="统一模型名称"
                value={quickModelName}
                onChange={(e) => setQuickModelName(e.target.value)}
                className="nib-mono rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent"
              >
                {!quickModelName && <option value="">选择模型</option>}
                {quickModelName && !quickModels.includes(quickModelName) && (
                  <option value={quickModelName}>{quickModelName}</option>
                )}
                {quickModels.map((m) => (
                  <option key={m} value={m}>
                    {m}
                  </option>
                ))}
              </select>
            ) : (
              <input
                type="text"
                value={quickModelName}
                onChange={(e) => setQuickModelName(e.target.value)}
                placeholder="输入模型名称"
                className="nib-mono rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:font-ui placeholder:text-ink-muted focus:border-accent"
              />
            )}
            <button
              type="button"
              disabled={applyingAll || !quickProviderId || !quickModelName.trim()}
              onClick={() => void handleApplyToAll()}
              className="rounded-lg bg-accent px-3.5 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
            >
              {applyingAll ? "应用中…" : "应用到全部环节"}
            </button>
          </div>
        </div>
      )}

      {/* Per-Stage Controls Header */}
      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <div className="text-xs font-medium text-ink-secondary">按环节细调（可选）</div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setShowAdvanced((v) => !v)}
            className="flex items-center gap-1 rounded-md px-2 py-1 text-xs text-ink-secondary transition-colors hover:bg-surface hover:text-ink-primary"
          >
            {showAdvanced ? (
              <ChevronDown className="h-3.5 w-3.5" />
            ) : (
              <ChevronRight className="h-3.5 w-3.5" />
            )}
            <span>{showAdvanced ? "收起高级参数" : "展开创意度与备用模型"}</span>
          </button>
          {dirtyStages.size > 0 && (
            <button
              type="button"
              disabled={savingAll}
              onClick={() => void handleSaveAllDirty()}
              className="flex items-center gap-1 rounded-lg bg-accent px-3 py-1 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
            >
              <Save className="h-3 w-3" />
              <span>{savingAll ? "保存中…" : `保存全部修改 (${dirtyStages.size})`}</span>
            </button>
          )}
        </div>
      </div>

      <div className="divide-y divide-border-subtle rounded-xl border border-border-subtle bg-surface/40 px-4">
        {PIPELINE_STAGES.map((stageDef) => {
          const route = routeDrafts[stageDef.stage];
          if (!route) return null;
          const selectedProv = route.providerId
            ? providerMap.get(route.providerId)
            : undefined;
          const modelSuggestions = selectedProv?.models ?? [];
          const isDirty = dirtyStages.has(stageDef.stage);
          const isSaving = savingStage === stageDef.stage;
          const fbProv = route.fallbackProviderId
            ? providerMap.get(route.fallbackProviderId)
            : undefined;
          const hasValidFallback = Boolean(
            fbProv &&
              fbProv.enabled &&
              (route.fallbackModelName ?? "").trim() &&
              !(
                route.providerId === route.fallbackProviderId &&
                route.modelName.trim().toLowerCase() ===
                  (route.fallbackModelName ?? "").trim().toLowerCase()
              )
          );
          const isPrimaryDisabled = Boolean(selectedProv && !selectedProv.enabled);
          const isStageReady = Boolean(
            selectedProv && selectedProv.enabled && route.modelName.trim()
          );
          const isDuplicateFallback = Boolean(
            route.providerId &&
              route.fallbackProviderId &&
              route.providerId === route.fallbackProviderId &&
              route.modelName.trim() &&
              route.modelName.trim().toLowerCase() ===
                (route.fallbackModelName ?? "").trim().toLowerCase()
          );

          return (
            <div key={stageDef.stage} className="py-3.5">
              <div className="mb-2 flex items-center justify-between gap-2">
                <div className="flex flex-wrap items-center gap-2">
                  <span
                    className={`h-1.5 w-1.5 shrink-0 rounded-full ${
                      isStageReady
                        ? "bg-ok"
                        : isPrimaryDisabled
                          ? "bg-danger"
                          : "bg-warn"
                    }`}
                  />
                  <span className="text-[13px] font-medium text-ink-primary">
                    {stageDef.label}
                  </span>
                  <span className="text-xs text-ink-muted">{stageDef.desc}</span>
                  {isPrimaryDisabled && (
                    <span className="rounded bg-danger-soft px-1.5 py-0.5 text-[11px] text-danger">
                      服务已停用
                    </span>
                  )}
                  {!isStageReady && !isPrimaryDisabled && (
                    <span className="rounded bg-warn-soft px-1.5 py-0.5 text-[11px] text-warn">
                      未配置
                    </span>
                  )}
                  {hasValidFallback && (
                    <span className="rounded border border-border-subtle bg-canvas px-1.5 py-0.5 text-[11px] text-ink-secondary">
                      含备用
                    </span>
                  )}
                </div>

                {isDirty && (
                  <button
                    type="button"
                    disabled={isSaving}
                    onClick={() => void handleSaveRoute(stageDef.stage)}
                    className="flex items-center gap-1 rounded-lg border border-accent/35 bg-accent-soft px-2.5 py-1 text-xs font-medium text-accent transition-colors hover:border-accent disabled:opacity-40"
                  >
                    <Save className="h-3 w-3" />
                    <span>{isSaving ? "保存中…" : "保存"}</span>
                  </button>
                )}
              </div>

              <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-[180px_1fr]">
                {/* Provider Select */}
                <div>
                  <label className="mb-1 block text-xs text-ink-muted">模型服务</label>
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
                    className="w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent"
                  >
                    <option value="">未选择</option>
                    {providers.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.enabled ? p.name : `${p.name}（已停用）`}
                      </option>
                    ))}
                  </select>
                </div>

                {/* Model Selector */}
                <div>
                  <div className="mb-1 flex items-center justify-between gap-2">
                    <label className="block text-xs text-ink-muted">模型</label>
                    {selectedProv && modelSuggestions.length === 0 ? (
                      <button
                        type="button"
                        onClick={() => onSelectProvider(selectedProv)}
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
                        updateRouteField(stageDef.stage, { modelName: val });
                      }}
                      className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent"
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
                        updateRouteField(stageDef.stage, { modelName: e.target.value })
                      }
                      placeholder={
                        selectedProv && modelSuggestions.length === 0
                          ? "该服务尚未启用模型，可手动输入或点右上角启用"
                          : "输入模型名称"
                      }
                      className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:font-ui placeholder:text-ink-muted focus:border-accent"
                    />
                  )}
                </div>
              </div>

              {showAdvanced && (
                <div className="mt-2.5 grid grid-cols-1 gap-2.5 border-t border-border-subtle/70 pt-2.5 sm:grid-cols-[100px_112px_1fr_1fr]">
                  <div>
                    <label className="mb-1 block text-xs text-ink-muted">创意度</label>
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
                      className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-right text-xs text-ink-primary focus:border-accent"
                    />
                  </div>
                  <div>
                    <label className="mb-1 block text-xs text-ink-muted">输出上限</label>
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
                      className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-right text-xs text-ink-primary focus:border-accent"
                    />
                  </div>
                  <div>
                    <label className="mb-1 block text-xs text-ink-muted">备用服务</label>
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
                      className="w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent"
                    >
                      <option value="">不设备用</option>
                      {providers.map((p) => (
                        <option key={p.id} value={p.id}>
                          {p.enabled ? p.name : `${p.name}（已停用）`}
                        </option>
                      ))}
                    </select>
                  </div>
                  <div>
                    <div className="mb-1 flex items-center justify-between gap-2">
                      <label className="block text-xs text-ink-muted">备用模型</label>
                      {fbProv && (fbProv.models?.length ?? 0) > 0 && customFallbackStages.has(stageDef.stage) && (
                        <button
                          type="button"
                          onClick={() =>
                            setCustomFallbackStages((prev) => {
                              const next = new Set(prev);
                              next.delete(stageDef.stage);
                              return next;
                            })
                          }
                          className="text-xs text-accent transition-opacity hover:opacity-80"
                        >
                          从列表选择
                        </button>
                      )}
                    </div>
                    {(() => {
                      const fbModels = fbProv?.models ?? [];
                      const fbModelVal = route.fallbackModelName ?? "";
                      if (
                        route.fallbackProviderId &&
                        fbModels.length > 0 &&
                        !customFallbackStages.has(stageDef.stage)
                      ) {
                        return (
                          <select
                            value={fbModelVal}
                            onChange={(e) => {
                              const val = e.target.value;
                              if (val === "__custom__") {
                                setCustomFallbackStages((prev) => {
                                  const next = new Set(prev);
                                  next.add(stageDef.stage);
                                  return next;
                                });
                                return;
                              }
                              updateRouteField(stageDef.stage, {
                                fallbackModelName: val,
                              });
                            }}
                            className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent"
                          >
                            <option value="">选择备用模型</option>
                            {fbModelVal && !fbModels.includes(fbModelVal) && (
                              <option value={fbModelVal}>
                                {fbModelVal}（当前值）
                              </option>
                            )}
                            {fbModels.map((m) => (
                              <option key={m} value={m}>
                                {m}
                              </option>
                            ))}
                            <option value="__custom__">手动输入其他模型…</option>
                          </select>
                        );
                      }
                      return (
                        <input
                          type="text"
                          disabled={!route.fallbackProviderId}
                          value={fbModelVal}
                          onChange={(e) =>
                            updateRouteField(stageDef.stage, {
                              fallbackModelName: e.target.value,
                            })
                          }
                          placeholder={route.fallbackProviderId ? "输入备用模型" : "先选备用服务"}
                          className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:font-ui placeholder:text-ink-muted focus:border-accent disabled:opacity-50"
                        />
                      );
                    })()}
                    {isDuplicateFallback && (
                      <div className="mt-1 text-[11px] text-warn">
                        备用模型与主模型相同，无法生效
                      </div>
                    )}
                  </div>
                </div>
              )}
            </div>
          );
        })}
      </div>

      {/* Collapsible Optional Model Pricing */}
      <div className="mt-4 rounded-xl border border-border-subtle bg-surface/40">
        <button
          type="button"
          onClick={() => setShowPricing((v) => !v)}
          className="flex w-full items-center justify-between px-4 py-2.5 text-left text-xs font-medium text-ink-secondary transition-colors hover:text-ink-primary"
        >
          <span>自定义模型单价（可选，用于「用量统计」估算花费）</span>
          {showPricing ? (
            <ChevronDown className="h-3.5 w-3.5" />
          ) : (
            <ChevronRight className="h-3.5 w-3.5" />
          )}
        </button>

        {showPricing && (
          <div className="border-t border-border-subtle px-4 py-3.5">
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-[1fr_1fr_88px_88px_72px_auto]">
              <select
                value={pricingProvider}
                onChange={(e) => {
                  setPricingProvider(e.target.value);
                  const p = providers.find((item) => item.name === e.target.value);
                  if (p && p.models.length > 0 && !pricingModel) {
                    setPricingModel(p.models[0] ?? "");
                  }
                }}
                className="rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent"
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
                placeholder="模型名称"
                className="nib-mono rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:font-ui placeholder:text-ink-muted focus:border-accent"
              />
              <input
                type="number"
                min={0}
                step={0.1}
                value={pricingInput}
                onChange={(e) => setPricingInput(e.target.value)}
                placeholder="输入/百万"
                className="nib-mono rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-right text-xs text-ink-primary placeholder:font-ui placeholder:text-ink-muted focus:border-accent"
              />
              <input
                type="number"
                min={0}
                step={0.1}
                value={pricingOutput}
                onChange={(e) => setPricingOutput(e.target.value)}
                placeholder="输出/百万"
                className="nib-mono rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-right text-xs text-ink-primary placeholder:font-ui placeholder:text-ink-muted focus:border-accent"
              />
              <select
                value={pricingCurrency}
                onChange={(e) =>
                  setPricingCurrency(e.target.value === "USD" ? "USD" : "CNY")
                }
                className="rounded-lg border border-border-subtle bg-canvas px-2 py-1.5 text-xs text-ink-primary focus:border-accent"
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
                保存
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
                      <span className="font-medium text-ink-primary">
                        {pr.providerName}
                      </span>
                      <span className="nib-mono truncate text-ink-secondary">
                        {pr.modelName}
                      </span>
                    </div>
                    <div className="flex shrink-0 items-center gap-3">
                      <span className="nib-mono text-ink-secondary">
                        输入 {pr.currency === "USD" ? "$" : "¥"}
                        {pr.inputPricePerM} / 输出 {pr.currency === "USD" ? "$" : "¥"}
                        {pr.outputPricePerM}
                      </span>
                      <button
                        type="button"
                        onClick={() =>
                          void handleDeletePricing(pr.providerName, pr.modelName)
                        }
                        aria-label="删除单价"
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
        )}
      </div>
    </div>
  );
}
