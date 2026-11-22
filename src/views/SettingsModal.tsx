import { useCallback, useEffect, useState } from "react";
import { BarChart3, GitBranch, KeyRound, Sliders, X } from "lucide-react";
import { useApp, type SettingsTab } from "../stores/AppContext";
import { ModalShell } from "../components/ui/ModalShell";
import { ProvidersTab } from "../components/settings/ProvidersTab";
import { RoutesPricingTab } from "../components/settings/RoutesPricingTab";
import { WorkspaceBackupTab } from "../components/settings/WorkspaceBackupTab";
import { UsageSettingsTab } from "../components/settings/UsageSettingsTab";

export function SettingsModal() {
  const { settingsOpen, settingsTab, closeSettings } = useApp();
  if (!settingsOpen) return null;
  return <SettingsModalBody initialTab={settingsTab} onClose={closeSettings} />;
}

const TAB_ITEMS: Array<{
  id: SettingsTab;
  label: string;
  title: string;
  icon: typeof KeyRound;
}> = [
  { id: "providers", label: "模型服务", title: "模型服务与密钥", icon: KeyRound },
  { id: "routes", label: "模型分配", title: "各环节模型分配", icon: GitBranch },
  { id: "workspace", label: "写作与备份", title: "写作偏好、阅读排版与本地备份", icon: Sliders },
  { id: "usage", label: "用量统计", title: "AI 调用与用量统计", icon: BarChart3 },
];

function SettingsModalBody({
  initialTab,
  onClose,
}: {
  initialTab: SettingsTab;
  onClose: () => void;
}) {
  const [tab, setTab] = useState<SettingsTab>(initialTab);
  const [focusProviderId, setFocusProviderId] = useState<number | null>(null);

  useEffect(() => {
    setTab(initialTab);
  }, [initialTab]);

  const handleConsumeFocus = useCallback(() => {
    setFocusProviderId(null);
  }, []);

  const currentTabMeta = TAB_ITEMS.find((item) => item.id === tab) ?? TAB_ITEMS[0]!;

  return (
    <ModalShell
      open
      onClose={onClose}
      hideHeader
      maxWidthClass="max-w-[920px]"
      maxHeightClass="h-[82vh] max-h-[720px]"
    >
      <div className="flex min-h-0 flex-1 overflow-hidden">
        {/* Left Navigation Rail */}
        <aside className="flex w-44 shrink-0 flex-col border-r border-border-subtle bg-surface p-3">
          <div className="mb-3 px-2.5 pt-1 text-[13px] font-medium text-ink-primary">
            设置
          </div>
          <nav className="flex flex-col gap-1">
            {TAB_ITEMS.map((item) => {
              const Icon = item.icon;
              const active = tab === item.id;
              return (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => setTab(item.id)}
                  className={`flex items-center gap-2 rounded-lg px-2.5 py-2 text-left text-[13px] font-medium transition-colors ${
                    active
                      ? "bg-accent-soft text-accent"
                      : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
                  }`}
                >
                  <Icon className="h-3.5 w-3.5" />
                  <span>{item.label}</span>
                </button>
              );
            })}
          </nav>
        </aside>

        {/* Right Panel */}
        <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
          <div className="flex shrink-0 items-center justify-between border-b border-border-subtle px-5 py-3">
            <h2 className="text-sm font-medium text-ink-primary">
              {currentTabMeta.title}
            </h2>
            <button
              type="button"
              onClick={onClose}
              aria-label="关闭设置"
              className="rounded-lg p-1 text-ink-muted transition-colors hover:bg-surface hover:text-ink-primary"
            >
              <X className="h-4 w-4" />
            </button>
          </div>

          {tab === "providers" && (
            <ProvidersTab
              focusProviderId={focusProviderId}
              onConsumeFocus={handleConsumeFocus}
            />
          )}
          {tab === "routes" && (
            <RoutesPricingTab
              onGoToProviders={() => setTab("providers")}
              onSelectProvider={(p) => {
                setFocusProviderId(p.id);
                setTab("providers");
              }}
            />
          )}
          {tab === "workspace" && <WorkspaceBackupTab />}
          {tab === "usage" && <UsageSettingsTab />}
        </div>
      </div>
    </ModalShell>
  );
}
