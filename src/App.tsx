import { useEffect } from "react";
import { AlertCircle, Loader2 } from "lucide-react";
import { AppProvider, useApp } from "./stores/AppContext";
import { ContextMenuProvider } from "./components/ContextMenu";
import { TopBar } from "./components/TopBar";
import { AutopilotModal, AutopilotStatusBar } from "./components/AutopilotConsole";
import { DirectorDrawer } from "./components/DirectorDrawer";
import { CreateBookModal } from "./components/CreateBookModal";
import { CommandPalette } from "./components/CommandPalette";
import { MemorySearchModal } from "./components/MemorySearchModal";
import { ToastHost } from "./components/ToastHost";
import { HomeView } from "./views/HomeView";
import { ArchitectView } from "./views/ArchitectView";
import { StudioView } from "./views/StudioView";
import { AtlasView } from "./views/AtlasView";
import { SettingsModal } from "./views/SettingsModal";

function NibShell() {
  const {
    bootstrapping,
    runtimeError,
    viewMode,
    activeBook,
    activeTab,
    setActiveTab,
    toggleStudioDrawer,
    triggerSave,
    triggerGenerate,
    settingsOpen,
    createBookOpen,
    memorySearchOpen,
    toggleMemorySearch,
    toggleDirector,
    focusMode,
    setFocusMode,
    toggleFocusMode,
    commandPaletteOpen,
    toggleCommandPalette,
  } = useApp();

  const inWorkspace = viewMode === "workspace" && Boolean(activeBook);
  const effectiveFocusMode = focusMode && inWorkspace && activeTab === "studio";

  // 离开写作台时自动退出专注模式
  useEffect(() => {
    if (focusMode && (!inWorkspace || activeTab !== "studio")) {
      setFocusMode(false);
    }
  }, [focusMode, inWorkspace, activeTab, setFocusMode]);

  // Global keyboard shortcuts:
  // Ctrl+Shift+P   -> toggle Command Palette & Shortcuts
  // F11 / Ctrl+Shift+Z -> toggle Focus Mode in Studio
  // Ctrl+1/2/3     -> switch 编排 / 审改 / 账本
  // Ctrl+\         -> toggle Studio right drawer
  // Ctrl+S         -> save active chapter draft (or beats/meta in Architect)
  // Ctrl+Enter     -> start/stop chapter generation in Studio
  // Ctrl+Shift+F   -> toggle Global Memory Search modal
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const blockingModalOpen = settingsOpen || createBookOpen;

      if (e.key === "F11" && !blockingModalOpen && inWorkspace && activeTab === "studio") {
        e.preventDefault();
        toggleFocusMode();
        return;
      }

      if (
        e.key === "Escape" &&
        effectiveFocusMode &&
        !blockingModalOpen &&
        !memorySearchOpen &&
        !commandPaletteOpen
      ) {
        setFocusMode(false);
        return;
      }

      const mod = e.ctrlKey || e.metaKey;
      if (!mod) return;

      if ((e.key.toLowerCase() === "p" || e.code === "KeyP") && e.shiftKey && !blockingModalOpen) {
        e.preventDefault();
        toggleCommandPalette();
        return;
      }

      if (
        (e.key.toLowerCase() === "z" || e.code === "KeyZ") &&
        e.shiftKey &&
        !blockingModalOpen &&
        inWorkspace &&
        activeTab === "studio"
      ) {
        e.preventDefault();
        toggleFocusMode();
        return;
      }

      if (
        (e.key.toLowerCase() === "f" || e.code === "KeyF") &&
        e.shiftKey &&
        !blockingModalOpen &&
        inWorkspace
      ) {
        e.preventDefault();
        toggleMemorySearch();
        return;
      }

      if (
        (e.key.toLowerCase() === "k" || e.code === "KeyK") &&
        !e.shiftKey &&
        !blockingModalOpen &&
        inWorkspace
      ) {
        e.preventDefault();
        toggleDirector();
        return;
      }

      const modalOpen = blockingModalOpen || memorySearchOpen;

      if ((e.key === "1" || e.code === "Digit1") && !modalOpen && inWorkspace) {
        e.preventDefault();
        setActiveTab("architect");
        return;
      }
      if ((e.key === "2" || e.code === "Digit2") && !modalOpen && inWorkspace) {
        e.preventDefault();
        setActiveTab("studio");
        return;
      }
      if ((e.key === "3" || e.code === "Digit3") && !modalOpen && inWorkspace) {
        e.preventDefault();
        setActiveTab("atlas");
        return;
      }
      if ((e.key === "\\" || e.code === "Backslash") && !modalOpen && inWorkspace) {
        e.preventDefault();
        toggleStudioDrawer();
        return;
      }
      if (
        (e.key.toLowerCase() === "s" || e.code === "KeyS") &&
        !e.shiftKey &&
        !modalOpen &&
        inWorkspace
      ) {
        e.preventDefault();
        triggerSave();
        return;
      }
      if (e.key === "Enter" && !modalOpen && inWorkspace && activeTab === "studio") {
        e.preventDefault();
        triggerGenerate();
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [
    inWorkspace,
    activeTab,
    settingsOpen,
    createBookOpen,
    memorySearchOpen,
    setActiveTab,
    toggleStudioDrawer,
    toggleMemorySearch,
    toggleDirector,
    effectiveFocusMode,
    commandPaletteOpen,
    setFocusMode,
    toggleFocusMode,
    toggleCommandPalette,
    triggerSave,
    triggerGenerate,
  ]);

  return (
    <div className="flex h-full flex-col overflow-hidden bg-canvas text-ink-primary">
      {!effectiveFocusMode && <TopBar />}
      {!effectiveFocusMode && inWorkspace && <AutopilotStatusBar />}

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
      ) : !inWorkspace ? (
        <HomeView />
      ) : (
        <div className="flex min-h-0 flex-1 overflow-hidden">
          <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
            {activeTab === "architect" ? (
              <ArchitectView />
            ) : activeTab === "studio" ? (
              <StudioView />
            ) : (
              <AtlasView />
            )}
          </div>
          {!effectiveFocusMode && <DirectorDrawer />}
        </div>
      )}

      <CreateBookModal />
      <AutopilotModal />
      <MemorySearchModal />
      <CommandPalette />
      <SettingsModal />
      <ToastHost />
    </div>
  );
}

export function App() {
  return (
    <AppProvider>
      <ContextMenuProvider>
        <NibShell />
      </ContextMenuProvider>
    </AppProvider>
  );
}
