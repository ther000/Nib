import { useEffect, useMemo, useRef, useState } from "react";
import { Check, ChevronUp, Loader2, Search, Settings } from "lucide-react";
import {
  SettingsService,
  type ModelRoute,
  type ProviderPublic,
  type TokenUsage,
} from "../../mygo";
import { useApp } from "../../stores/AppContext";
import {
  PIPELINE_STAGES,
  formatCompactTokens,
  formatErrorMessage,
} from "../../lib/constants";

interface QuickModelPickerProps {
  generating: boolean;
  genModelName: string;
  lastUsage: TokenUsage | null;
  draftRouteInfo: {
    configured: boolean;
    label: string;
    providerName?: string;
  };
}

interface ProviderModelGroup {
  provider: ProviderPublic;
  models: string[];
}

export function QuickModelPicker({
  generating,
  genModelName,
  lastUsage,
  draftRouteInfo,
}: QuickModelPickerProps) {
  const { providers, modelRoutes, refreshSettingsData, openSettings, notify } =
    useApp();

  const [open, setOpen] = useState(false);
  const [savingKey, setSavingKey] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const containerRef = useRef<HTMLDivElement>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);

  const draftRoute = useMemo(
    () => modelRoutes.find((r) => r.stage === "draft"),
    [modelRoutes]
  );

  // Close on outside click or Escape
  useEffect(() => {
    if (!open) return;
    const handlePointerDown = (e: MouseEvent) => {
      if (
        containerRef.current &&
        !containerRef.current.contains(e.target as Node)
      ) {
        setOpen(false);
      }
    };
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", handlePointerDown);
    document.addEventListener("keydown", handleKey);
    return () => {
      document.removeEventListener("mousedown", handlePointerDown);
      document.removeEventListener("keydown", handleKey);
    };
  }, [open]);

  // Reset search when closed; auto-focus when opened with many models
  const groups = useMemo<ProviderModelGroup[]>(() => {
    const result: ProviderModelGroup[] = [];
    const sortedProviders = [...providers].sort((a, b) => {
      if (a.enabled !== b.enabled) return a.enabled ? -1 : 1;
      return a.id - b.id;
    });

    for (const prov of sortedProviders) {
      const isCurrentProv = draftRoute?.providerId === prov.id;
      if (!prov.enabled && !isCurrentProv) continue;

      const modelList = [...prov.models];
      if (
        isCurrentProv &&
        draftRoute?.modelName &&
        !modelList.includes(draftRoute.modelName)
      ) {
        modelList.unshift(draftRoute.modelName);
      }
      if (modelList.length > 0) {
        result.push({ provider: prov, models: modelList });
      }
    }
    return result;
  }, [providers, draftRoute]);

  const totalModels = useMemo(
    () => groups.reduce((acc, g) => acc + g.models.length, 0),
    [groups]
  );

  useEffect(() => {
    if (!open) {
      setQuery("");
      return;
    }
    if (totalModels > 8) {
      requestAnimationFrame(() => searchInputRef.current?.focus());
    }
  }, [open, totalModels]);

  const filteredGroups = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return groups;
    return groups
      .map((g) => {
        const provMatch = g.provider.name.toLowerCase().includes(q);
        const matched = provMatch
          ? g.models
          : g.models.filter((m) => m.toLowerCase().includes(q));
        return { ...g, models: matched };
      })
      .filter((g) => g.models.length > 0);
  }, [groups, query]);

  const handleSelectModel = async (providerId: number, modelName: string) => {
    if (savingKey || generating) return;
    if (
      draftRoute?.providerId === providerId &&
      draftRoute?.modelName === modelName
    ) {
      setOpen(false);
      return;
    }

    const key = `${providerId}:${modelName}`;
    setSavingKey(key);
    try {
      const draftDef = PIPELINE_STAGES.find((s) => s.stage === "draft");
      const nextRoute: ModelRoute = {
        stage: "draft",
        providerId,
        modelName,
        fallbackProviderId:
          draftRoute?.fallbackProviderId && draftRoute.fallbackProviderId > 0
            ? draftRoute.fallbackProviderId
            : undefined,
        fallbackModelName: (draftRoute?.fallbackModelName ?? "").trim(),
        temperature: draftRoute?.temperature ?? draftDef?.defaultTemp ?? 0.8,
        maxTokens: draftRoute?.maxTokens ?? draftDef?.defaultMaxTokens ?? 4096,
        updatedAt: draftRoute?.updatedAt ?? "",
      };
      await SettingsService.saveModelRoute(nextRoute);
      await refreshSettingsData();
      setOpen(false);
      notify({ tone: "ok", message: `正文模型已切换为 ${modelName}` });
    } catch (err) {
      notify({
        tone: "danger",
        message: "切换正文模型失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setSavingKey(null);
    }
  };

  const displayLabel = generating
    ? genModelName || draftRouteInfo.label
    : draftRouteInfo.label;

  return (
    <div ref={containerRef} className="relative hidden sm:inline-flex">
      <button
        type="button"
        disabled={generating}
        onClick={() => setOpen((v) => !v)}
        title={
          lastUsage
            ? `快速切换正文模型（上次调用：输入 ${formatCompactTokens(lastUsage.promptTokens)} / 输出 ${formatCompactTokens(lastUsage.completionTokens)} Token）`
            : "快速切换正文写作模型"
        }
        aria-haspopup="listbox"
        aria-expanded={open}
        className={`inline-flex max-w-[164px] items-center gap-1 rounded-full px-2 py-0.5 text-[11px] transition-colors disabled:opacity-60 ${
          open
            ? "bg-[var(--bg-hover)] text-ink-primary"
            : draftRouteInfo.configured
              ? "text-ink-muted hover:bg-[var(--bg-hover)] hover:text-ink-primary"
              : "text-warn hover:bg-warn-soft"
        }`}
      >
        <span
          className={`truncate ${draftRouteInfo.configured ? "nib-mono" : ""}`}
        >
          {displayLabel}
        </span>
        <ChevronUp
          className={`h-3 w-3 shrink-0 opacity-65 transition-transform duration-150 ${
            open ? "rotate-180" : ""
          }`}
        />
      </button>

      {open && (
        <div
          role="listbox"
          aria-label="选择正文写作模型"
          className="nib-floating-dock absolute right-0 bottom-[calc(100%+10px)] z-40 w-72 rounded-xl py-1.5 text-left"
        >
          <div className="flex items-center justify-between px-3 py-1">
            <span className="text-[11px] font-medium text-ink-secondary">
              正文写作模型
            </span>
            {draftRouteInfo.providerName && (
              <span className="truncate pl-2 text-[10px] text-ink-muted">
                {draftRouteInfo.providerName}
              </span>
            )}
          </div>

          {totalModels > 8 && (
            <div className="px-2 pb-1.5">
              <div className="flex items-center gap-1.5 rounded-lg border border-border-subtle bg-canvas px-2 py-1">
                <Search className="h-3 w-3 shrink-0 text-ink-muted" />
                <input
                  ref={searchInputRef}
                  type="text"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder="筛选模型…"
                  className="w-full bg-transparent text-xs text-ink-primary placeholder:text-ink-muted focus:outline-none"
                />
              </div>
            </div>
          )}

          {groups.length === 0 ? (
            <div className="px-3 py-4 text-center text-xs text-ink-muted">
              暂未启用任何模型
            </div>
          ) : filteredGroups.length === 0 ? (
            <div className="px-3 py-4 text-center text-xs text-ink-muted">
              没有匹配的模型
            </div>
          ) : (
            <div className="max-h-64 overflow-y-auto px-1.5">
              {filteredGroups.map((group, idx) => (
                <div
                  key={group.provider.id}
                  className={idx > 0 ? "mt-1.5 border-t border-border-subtle pt-1.5" : ""}
                >
                  <div className="px-2 py-0.5 text-[10px] font-medium text-ink-muted">
                    {group.provider.name}
                    {!group.provider.enabled && "（已停用）"}
                  </div>
                  <div className="flex flex-col gap-0.5">
                    {group.models.map((modelName) => {
                      const key = `${group.provider.id}:${modelName}`;
                      const selected =
                        draftRoute?.providerId === group.provider.id &&
                        draftRoute?.modelName === modelName;
                      const isSaving = savingKey === key;
                      return (
                        <button
                          key={key}
                          type="button"
                          role="option"
                          aria-selected={selected}
                          disabled={Boolean(savingKey)}
                          onClick={() =>
                            void handleSelectModel(group.provider.id, modelName)
                          }
                          className={`flex w-full items-center justify-between gap-2 rounded-lg px-2 py-1.5 text-left text-xs transition-colors disabled:opacity-60 ${
                            selected
                              ? "bg-accent-soft font-medium text-accent"
                              : "text-ink-primary hover:bg-[var(--bg-hover)]"
                          }`}
                        >
                          <span className="nib-mono truncate">{modelName}</span>
                          {isSaving ? (
                            <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-accent" />
                          ) : selected ? (
                            <Check className="h-3.5 w-3.5 shrink-0 text-accent" />
                          ) : null}
                        </button>
                      );
                    })}
                  </div>
                </div>
              ))}
            </div>
          )}

          <div className="my-1.5 border-t border-border-subtle" />

          <div className="px-1.5">
            <button
              type="button"
              onClick={() => {
                setOpen(false);
                openSettings(draftRouteInfo.configured ? "routes" : "providers");
              }}
              className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-xs text-ink-secondary transition-colors hover:bg-[var(--bg-hover)] hover:text-ink-primary"
            >
              <Settings className="h-3.5 w-3.5 text-ink-muted" />
              <span>
                {groups.length === 0 ? "前往配置模型服务…" : "模型与路由设置…"}
              </span>
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
