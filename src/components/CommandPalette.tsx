import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  BookMarked,
  BookOpen,
  Bot,
  Command,
  Download,
  FileText,
  Home,
  Keyboard,
  Maximize2,
  Minimize2,
  Moon,
  PenLine,
  Plus,
  Search,
  Settings,
  Sparkles,
  Sun,
  X,
} from "lucide-react";
import { useApp } from "../stores/AppContext";
import { useAutopilotActions } from "../stores/AutopilotContext";
import { exportBookWithNotify } from "../lib/bookExport";
import { formatSeq, isIMEComposing } from "../lib/constants";

interface PaletteCommand {
  id: string;
  group: string;
  label: string;
  keywords?: string;
  shortcut?: string;
  icon: ReactNode;
  run: () => void | Promise<void>;
}

const SHORTCUT_ITEMS: Array<{ keys: string; desc: string; scope: string }> = [
  { keys: "Ctrl + Shift + P", desc: "打开命令面板 / 快捷键速查", scope: "全局" },
  { keys: "Ctrl + 1 / 2 / 3", desc: "切换 大纲 / 写作 / 设定", scope: "工作区" },
  { keys: "Ctrl + S", desc: "保存当前章节正文或大纲节拍", scope: "工作区" },
  { keys: "Ctrl + Enter", desc: "开始或停止 AI 流式生成本章", scope: "写作台" },
  { keys: "Ctrl + H", desc: "打开本章 / 全书查找与替换", scope: "写作台" },
  { keys: "F11 / Ctrl+Shift+Z", desc: "进入或退出无干扰专注写作模式", scope: "写作台" },
  { keys: "Ctrl + \\", desc: "折叠或展开写作台右侧抽屉", scope: "写作台" },
  { keys: "Alt + [ / ]", desc: "跳转上一处 / 下一处审校告警段落", scope: "写作台" },
  { keys: "Ctrl + K", desc: "打开或收起 AI 导演总控抽屉", scope: "工作区" },
  { keys: "Ctrl + Shift + F", desc: "打开全局记忆与设定混合检索", scope: "工作区" },
];

export function CommandPalette() {
  const { commandPaletteOpen } = useApp();
  if (!commandPaletteOpen) return null;
  return <CommandPaletteBody />;
}

function CommandPaletteBody() {
  const {
    closeCommandPalette,
    viewMode,
    activeBook,
    chapters,
    theme,
    toggleTheme,
    focusMode,
    toggleFocusMode,
    openHome,
    setActiveTab,
    openAtlasSubTab,
    jumpToChapter,
    openCreateBook,
    openSettings,
    openMemorySearch,
    toggleDirector,
    toggleStudioDrawer,
    triggerSave,
    notify,
  } = useApp();
  const { openAutopilot } = useAutopilotActions();

  const [mode, setMode] = useState<"commands" | "shortcuts">("commands");
  const [query, setQuery] = useState("");
  const [selectedIdx, setSelectedIdx] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([]);

  const inWorkspace = viewMode === "workspace" && Boolean(activeBook);

  useEffect(() => {
    setQuery("");
    setSelectedIdx(0);
    setMode("commands");
    requestAnimationFrame(() => inputRef.current?.focus());
  }, []);

  const commands = useMemo<PaletteCommand[]>(() => {
    const list: PaletteCommand[] = [];

    if (inWorkspace && activeBook) {
      list.push(
        {
          id: "nav-architect",
          group: "视图切换",
          label: "切换到大纲编排台",
          keywords: "dagang bianpai architect",
          shortcut: "Ctrl+1",
          icon: <BookOpen className="h-3.5 w-3.5" />,
          run: () => setActiveTab("architect"),
        },
        {
          id: "nav-studio",
          group: "视图切换",
          label: "切换到正文写作台",
          keywords: "xiezuo studio zhengwen",
          shortcut: "Ctrl+2",
          icon: <PenLine className="h-3.5 w-3.5" />,
          run: () => setActiveTab("studio"),
        },
        {
          id: "nav-atlas",
          group: "视图切换",
          label: "切换到设定账本",
          keywords: "sheding atlas renwu",
          shortcut: "Ctrl+3",
          icon: <BookMarked className="h-3.5 w-3.5" />,
          run: () => openAtlasSubTab("entities"),
        },
        {
          id: "nav-style",
          group: "视图切换",
          label: "打开文风与去 AI 味 / 敏感词规则",
          keywords: "wenfeng deai minganci style",
          icon: <Sparkles className="h-3.5 w-3.5" />,
          run: () => openAtlasSubTab("style"),
        },
        {
          id: "toggle-focus",
          group: "写作操作",
          label: focusMode ? "退出专注写作模式" : "进入专注写作模式",
          keywords: "zhuanzhu focus zen fullscreen",
          shortcut: "F11",
          icon: focusMode ? <Minimize2 className="h-3.5 w-3.5" /> : <Maximize2 className="h-3.5 w-3.5" />,
          run: () => {
            setActiveTab("studio");
            toggleFocusMode();
          },
        },
        {
          id: "action-save",
          group: "写作操作",
          label: "保存当前章节 / 编排",
          keywords: "baocun save",
          shortcut: "Ctrl+S",
          icon: <FileText className="h-3.5 w-3.5" />,
          run: () => triggerSave(),
        },
        {
          id: "toggle-drawer",
          group: "写作操作",
          label: "折叠 / 展开写作台右侧抽屉",
          keywords: "chouti drawer",
          shortcut: "Ctrl+\\",
          icon: <BookOpen className="h-3.5 w-3.5" />,
          run: () => toggleStudioDrawer(),
        },
        {
          id: "open-director",
          group: "AI 助手",
          label: "打开 / 收起 AI 导演总控",
          keywords: "daoyan director ai",
          shortcut: "Ctrl+K",
          icon: <Sparkles className="h-3.5 w-3.5" />,
          run: () => toggleDirector(),
        },
        {
          id: "open-autopilot",
          group: "AI 助手",
          label: "打开 AI 自动托管写作",
          keywords: "tuoguan autopilot",
          icon: <Bot className="h-3.5 w-3.5" />,
          run: () => openAutopilot(),
        },
        {
          id: "open-memory-search",
          group: "AI 助手",
          label: "全局记忆与设定混合检索",
          keywords: "jiyi sousuo search memory",
          shortcut: "Ctrl+Shift+F",
          icon: <Search className="h-3.5 w-3.5" />,
          run: () => openMemorySearch(),
        },
        {
          id: "export-txt",
          group: "导出当前书籍",
          label: `导出《${activeBook.title}》整本 TXT`,
          keywords: "daochu txt export",
          icon: <FileText className="h-3.5 w-3.5" />,
          run: () => void exportBookWithNotify(activeBook.id, activeBook.title, "txt", notify),
        },
        {
          id: "export-epub",
          group: "导出当前书籍",
          label: `导出《${activeBook.title}》EPUB 电子书`,
          keywords: "daochu epub export",
          icon: <Download className="h-3.5 w-3.5" />,
          run: () => void exportBookWithNotify(activeBook.id, activeBook.title, "epub", notify),
        },
        {
          id: "nav-home",
          group: "全局操作",
          label: "返回首页书架",
          keywords: "shouye shujia home",
          icon: <Home className="h-3.5 w-3.5" />,
          run: () => openHome(),
        }
      );
    }

    list.push(
      {
        id: "new-book",
        group: "全局操作",
        label: "新建书籍",
        keywords: "xinjian book create",
        icon: <Plus className="h-3.5 w-3.5" />,
        run: () => openCreateBook(),
      },
      {
        id: "toggle-theme",
        group: "全局操作",
        label: theme === "dark" ? "切换到暖纸浅色主题" : "切换到深色护眼主题",
        keywords: "zhuti theme dark light",
        icon: theme === "dark" ? <Sun className="h-3.5 w-3.5" /> : <Moon className="h-3.5 w-3.5" />,
        run: () => toggleTheme(),
      },
      {
        id: "open-settings-workspace",
        group: "全局操作",
        label: "打开写作排版与备份设置",
        keywords: "shezhi paiban zihao beifen settings",
        icon: <Settings className="h-3.5 w-3.5" />,
        run: () => openSettings("workspace"),
      },
      {
        id: "open-settings-providers",
        group: "全局操作",
        label: "打开模型服务与路由设置",
        keywords: "moxing api settings",
        icon: <Settings className="h-3.5 w-3.5" />,
        run: () => openSettings("providers"),
      }
    );

    // 章节快速跳转（最多展示前 40 章或搜索命中章）
    if (inWorkspace && chapters.length > 0) {
      for (const chap of chapters) {
        list.push({
          id: `chap-${chap.id}`,
          group: "跳转章节",
          label: `第 ${formatSeq(chap.globalSeq)} 章 · ${chap.title}`,
          keywords: `${chap.globalSeq} ${chap.title}`,
          icon: <PenLine className="h-3.5 w-3.5" />,
          run: () => void jumpToChapter(chap.id),
        });
      }
    }

    return list;
  }, [
    inWorkspace,
    activeBook,
    chapters,
    theme,
    focusMode,
    setActiveTab,
    openAtlasSubTab,
    toggleFocusMode,
    triggerSave,
    toggleStudioDrawer,
    toggleDirector,
    openAutopilot,
    openMemorySearch,
    openHome,
    openCreateBook,
    toggleTheme,
    openSettings,
    jumpToChapter,
    notify,
  ]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) {
      // 未输入关键词时，章节只显示前 8 条以免淹没操作指令
      let chapCount = 0;
      return commands.filter((cmd) => {
        if (cmd.group === "跳转章节") {
          chapCount++;
          return chapCount <= 8;
        }
        return true;
      });
    }
    return commands.filter(
      (cmd) =>
        cmd.label.toLowerCase().includes(q) ||
        cmd.group.toLowerCase().includes(q) ||
        (cmd.keywords?.toLowerCase().includes(q) ?? false)
    );
  }, [commands, query]);

  useEffect(() => {
    setSelectedIdx(0);
  }, [query, mode]);

  useEffect(() => {
    itemRefs.current[selectedIdx]?.scrollIntoView({ block: "nearest" });
  }, [selectedIdx]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        closeCommandPalette();
        return;
      }
      if (mode !== "commands" || filtered.length === 0) return;
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setSelectedIdx((i) => (i + 1) % filtered.length);
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setSelectedIdx((i) => (i - 1 + filtered.length) % filtered.length);
      } else if (e.key === "Enter" && !isIMEComposing(e)) {
        e.preventDefault();
        const target = filtered[selectedIdx];
        if (target) {
          closeCommandPalette();
          void target.run();
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [mode, filtered, selectedIdx, closeCommandPalette]);

  return (
    <div
      data-nib-modal="true"
      className="fixed inset-0 z-50 flex items-start justify-center bg-black/55 px-4 pt-[11vh] backdrop-blur-[2px]"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) closeCommandPalette();
      }}
    >
      <div className="flex max-h-[72vh] w-full max-w-[560px] flex-col overflow-hidden rounded-xl border border-border-strong bg-elevated shadow-[var(--shadow-elevated)]">
        {/* Header / Mode Switch */}
        <div className="flex items-center gap-2 border-b border-border-subtle px-3.5 py-2.5">
          {mode === "commands" ? (
            <>
              <Command className="h-4 w-4 shrink-0 text-accent" />
              <input
                ref={inputRef}
                type="text"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="输入命令、视图或章节标题快速跳转…"
                className="min-w-0 flex-1 bg-transparent text-[13px] text-ink-primary placeholder:text-ink-muted focus:outline-none"
              />
            </>
          ) : (
            <div className="flex flex-1 items-center gap-2 text-[13px] font-medium text-ink-primary">
              <Keyboard className="h-4 w-4 text-accent" />
              <span>快捷键速查</span>
            </div>
          )}

          <div className="flex shrink-0 items-center gap-1 rounded-lg border border-border-subtle bg-canvas p-0.5 text-[11px]">
            <button
              type="button"
              onClick={() => setMode("commands")}
              className={`rounded px-2 py-0.5 transition-colors ${
                mode === "commands"
                  ? "bg-accent-soft font-medium text-accent"
                  : "text-ink-secondary hover:text-ink-primary"
              }`}
            >
              命令
            </button>
            <button
              type="button"
              onClick={() => setMode("shortcuts")}
              className={`rounded px-2 py-0.5 transition-colors ${
                mode === "shortcuts"
                  ? "bg-accent-soft font-medium text-accent"
                  : "text-ink-secondary hover:text-ink-primary"
              }`}
            >
              快捷键
            </button>
          </div>

          <button
            type="button"
            onClick={closeCommandPalette}
            className="rounded p-1 text-ink-muted hover:bg-surface hover:text-ink-primary"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        </div>

        {/* Body */}
        {mode === "commands" ? (
          <div className="flex-1 overflow-y-auto p-1.5">
            {filtered.length === 0 ? (
              <div className="py-10 text-center text-xs text-ink-muted">
                未找到匹配的命令或章节
              </div>
            ) : (
              filtered.map((cmd, idx) => {
                const active = idx === selectedIdx;
                const prevGroup = idx > 0 ? filtered[idx - 1]?.group : null;
                return (
                  <div key={cmd.id}>
                    {cmd.group !== prevGroup && (
                      <div className="px-2.5 pt-2 pb-1 text-[11px] font-medium text-ink-muted">
                        {cmd.group}
                      </div>
                    )}
                    <button
                      ref={(el) => {
                        itemRefs.current[idx] = el;
                      }}
                      type="button"
                      onMouseEnter={() => setSelectedIdx(idx)}
                      onClick={() => {
                        closeCommandPalette();
                        void cmd.run();
                      }}
                      className={`flex w-full items-center justify-between gap-3 rounded-lg px-2.5 py-2 text-left text-xs transition-colors ${
                        active
                          ? "bg-accent-soft text-ink-primary"
                          : "text-ink-secondary hover:bg-surface hover:text-ink-primary"
                      }`}
                    >
                      <span className="flex min-w-0 items-center gap-2.5">
                        <span className={active ? "text-accent" : "text-ink-muted"}>
                          {cmd.icon}
                        </span>
                        <span className="truncate text-[12.5px] text-ink-primary">
                          {cmd.label}
                        </span>
                      </span>
                      {cmd.shortcut && (
                        <span className="nib-mono shrink-0 rounded border border-border-subtle bg-canvas px-1.5 py-0.5 text-[10.5px] text-ink-muted">
                          {cmd.shortcut}
                        </span>
                      )}
                    </button>
                  </div>
                );
              })
            )}
          </div>
        ) : (
          <div className="flex-1 divide-y divide-border-subtle overflow-y-auto px-4 py-2">
            {SHORTCUT_ITEMS.map((item) => (
              <div
                key={item.keys}
                className="flex items-center justify-between gap-3 py-2.5 text-xs"
              >
                <div className="flex items-center gap-2">
                  <span className="rounded bg-canvas px-1.5 py-0.5 text-[10.5px] text-ink-muted">
                    {item.scope}
                  </span>
                  <span className="text-ink-primary">{item.desc}</span>
                </div>
                <kbd className="nib-mono shrink-0 rounded border border-border-strong bg-canvas px-2 py-0.5 text-[11px] text-accent">
                  {item.keys}
                </kbd>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
