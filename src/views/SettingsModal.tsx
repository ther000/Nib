import { useCallback, useEffect, useRef, useState } from "react";
import { GitBranch, KeyRound, Sliders, X } from "lucide-react";
import { useApp, type SettingsTab } from "../stores/AppContext";
import { ProvidersTab } from "../components/settings/ProvidersTab";
import { RoutesPricingTab } from "../components/settings/RoutesPricingTab";
import { WorkspaceBackupTab } from "../components/settings/WorkspaceBackupTab";

export function SettingsModal() {
  const { settingsOpen, settingsTab, closeSettings } = useApp();
  if (!settingsOpen) return null;
  return <SettingsModalBody initialTab={settingsTab} onClose={closeSettings} />;
}

function SettingsModalBody({
  initialTab,
  onClose,
}: {
  initialTab: SettingsTab;
  onClose: () => void;
}) {
  const [tab, setTab] = useState<SettingsTab>(initialTab);
  const [focusProviderId, setFocusProviderId] = useState<number | null>(null);
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setTab(initialTab);
  }, [initialTab]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      const modals = document.querySelectorAll('[data-nib-modal="true"]');
      if (modals.length > 0 && modals[modals.length - 1] !== rootRef.current) {
        return;
      }
      onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const handleConsumeFocus = useCallback(() => {
    setFocusProviderId(null);
  }, []);

  return (
    <div
      ref={rootRef}
      data-nib-modal="true"
      role="dialog"
      aria-modal="true"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/55 p-4"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
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
        </div>
      </div>
    </div>
  );
}
