import { useEffect } from "react";
import { AlertCircle, Loader2 } from "lucide-react";
import { AppProvider, useApp } from "./stores/AppContext";
import { TopBar } from "./components/TopBar";
import { EmptyShelf } from "./components/EmptyShelf";
import { CreateBookModal } from "./components/CreateBookModal";
import { ToastHost } from "./components/ToastHost";
import { ArchitectView } from "./views/ArchitectView";
import { StudioView } from "./views/StudioView";
import { AtlasView } from "./views/AtlasView";
import { SettingsModal } from "./views/SettingsModal";

function NibShell() {
  const {
    bootstrapping,
    runtimeError,
    activeBook,
    activeTab,
    setActiveTab,
    toggleStudioDrawer,
    triggerSave,
    triggerGenerate,
    settingsOpen,
    createBookOpen,
  } = useApp();

  // Global keyboard shortcuts:
  // Ctrl+1/2/3 -> switch 编排 / 审改 / 账本
  // Ctrl+\     -> toggle Studio right drawer
  // Ctrl+S     -> save active chapter draft (or beats/meta in Architect)
  // Ctrl+Enter -> start/stop chapter generation in Studio
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey;
      if (!mod) return;

      const modalOpen = settingsOpen || createBookOpen;

      if ((e.key === "1" || e.code === "Digit1") && !modalOpen && activeBook) {
        e.preventDefault();
        setActiveTab("architect");
        return;
      }
      if ((e.key === "2" || e.code === "Digit2") && !modalOpen && activeBook) {
        e.preventDefault();
        setActiveTab("studio");
        return;
      }
      if ((e.key === "3" || e.code === "Digit3") && !modalOpen && activeBook) {
        e.preventDefault();
        setActiveTab("atlas");
        return;
      }
      if ((e.key === "\\" || e.code === "Backslash") && !modalOpen && activeBook) {
        e.preventDefault();
        toggleStudioDrawer();
        return;
      }
      if (
        (e.key.toLowerCase() === "s" || e.code === "KeyS") &&
        !e.shiftKey &&
        !modalOpen &&
        activeBook
      ) {
        e.preventDefault();
        triggerSave();
        return;
      }
      if (e.key === "Enter" && !modalOpen && activeBook && activeTab === "studio") {
        e.preventDefault();
        triggerGenerate();
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [
    activeBook,
    activeTab,
    settingsOpen,
    createBookOpen,
    setActiveTab,
    toggleStudioDrawer,
    triggerSave,
    triggerGenerate,
  ]);

  return (
    <div className="flex h-full flex-col overflow-hidden bg-canvas text-ink-primary">
      <TopBar />

      {bootstrapping ? (
        <main className="flex flex-1 items-center justify-center">
          <div className="flex items-center gap-2 text-xs text-ink-muted">
            <Loader2 className="h-4 w-4 animate-spin text-accent" />
            <span>正在载入工作台…</span>
          </div>
        </main>
      ) : runtimeError ? (
        <main className="flex flex-1 items-center justify-center px-6">
          <div className="flex max-w-md items-start gap-3 rounded-xl border border-danger/35 bg-surface p-5">
            <AlertCircle className="mt-0.5 h-5 w-5 shrink-0 text-danger" />
            <div>
              <div className="text-sm font-medium text-ink-primary">
                无法连接 Nib 桌面引擎
              </div>
              <div className="mt-1 text-xs leading-relaxed text-ink-secondary">
                {runtimeError}
              </div>
            </div>
          </div>
        </main>
      ) : !activeBook ? (
        <EmptyShelf />
      ) : activeTab === "architect" ? (
        <ArchitectView />
      ) : activeTab === "studio" ? (
        <StudioView />
      ) : (
        <AtlasView />
      )}

      <CreateBookModal />
      <SettingsModal />
      <ToastHost />
    </div>
  );
}

export function App() {
  return (
    <AppProvider>
      <NibShell />
    </AppProvider>
  );
}
