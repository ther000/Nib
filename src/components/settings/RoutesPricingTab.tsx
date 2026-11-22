import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Save, Trash2 } from "lucide-react";
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
  const [customModelStages, setCustomModelStages] = useState<Set<string>>(() => new Set());

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
      const stageLabel = PIPELINE_STAGES.find((s) => s.stage === stage)?.label ?? stage;
      notify({ tone: "ok", message: `已保存「${stageLabel}」配置` });
    } catch (err) {
      notify({ tone: "danger", message: "保存模型分配失败", detail: formatErrorMessage(err) });
    } finally {
      setSavingStage(null);
    }
  };

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

  return (
    <div className="flex-1 overflow-y-auto px-5 py-4">
      {providers.length === 0 && (
        <div className="mb-4 flex items-center justify-between rounded-lg border border-warn/35 bg-warn-soft px-3.5 py-2.5 text-xs text-warn">
          <span>请先添加模型服务，再为各写作环节指定模型。</span>
          <button
            type="button"
            onClick={onGoToProviders}
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
            <div key={stageDef.stage} className="py-4 first:pt-1 last:pb-1">
              <div className="mb-2.5 flex items-center justify-between gap-2">
                <div className="flex items-baseline gap-2.5">
                  <span className="text-[13px] font-medium text-ink-primary">
                    {stageDef.label}
                  </span>
                  <span className="text-xs text-ink-muted">{stageDef.desc}</span>
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
                        placeholder={
                          route.fallbackProviderId
                            ? "输入备用模型 ID"
                            : "请先选择备用服务"
                        }
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
              <div className="text-sm font-medium text-ink-primary">
                模型单价设置（每百万 Token）
              </div>
              <div className="text-xs text-ink-muted">
                按你使用的官方或中转价格填写，首页用量面板将自动换算花费
              </div>
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
              onChange={(e) =>
                setPricingCurrency(e.target.value === "USD" ? "USD" : "CNY")
              }
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
  );
}
