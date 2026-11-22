import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { Clipboard, Copy, Scissors, TextSelect } from "lucide-react";

export type ContextMenuItem =
  | { type: "separator" }
  | { type: "label"; label: string }
  | {
      type?: "item";
      label: string;
      icon?: ReactNode;
      shortcut?: string;
      danger?: boolean;
      disabled?: boolean;
      onSelect: () => void | Promise<void>;
    };

interface ContextMenuState {
  x: number;
  y: number;
  items: ContextMenuItem[];
}

interface ContextMenuContextValue {
  openContextMenu: (
    e: ReactMouseEvent | MouseEvent,
    items: ContextMenuItem[]
  ) => void;
  closeContextMenu: () => void;
}

const ContextMenuContext = createContext<ContextMenuContextValue | null>(null);

export function useContextMenu(): ContextMenuContextValue {
  const ctx = useContext(ContextMenuContext);
  if (!ctx) {
    throw new Error("useContextMenu must be used within ContextMenuProvider");
  }
  return ctx;
}

type EditableTextElement = HTMLInputElement | HTMLTextAreaElement;

function isEditableTextElement(el: EventTarget | null): el is EditableTextElement {
  if (!el || !(el instanceof HTMLElement)) return false;
  if (el instanceof HTMLTextAreaElement) return !el.disabled;
  if (el instanceof HTMLInputElement) {
    const textTypes = new Set([
      "text",
      "search",
      "url",
      "tel",
      "password",
    ]);
    return textTypes.has(el.type || "text") && !el.disabled;
  }
  return false;
}

function setNativeEditableValue(el: EditableTextElement, nextValue: string) {
  const proto =
    el instanceof HTMLTextAreaElement
      ? HTMLTextAreaElement.prototype
      : HTMLInputElement.prototype;
  const setter = Object.getOwnPropertyDescriptor(proto, "value")?.set;
  if (setter) {
    setter.call(el, nextValue);
  } else {
    el.value = nextValue;
  }
  el.dispatchEvent(new Event("input", { bubbles: true }));
}

function buildEditableFallbackItems(el: EditableTextElement): ContextMenuItem[] {
  const start = el.selectionStart ?? 0;
  const end = el.selectionEnd ?? 0;
  const hasSelection = end > start;
  const selectedText = hasSelection ? el.value.slice(start, end) : "";
  const isReadOnly = el.readOnly;
  const isPassword = el instanceof HTMLInputElement && el.type === "password";

  const items: ContextMenuItem[] = [
    {
      label: "剪切",
      icon: <Scissors className="h-3.5 w-3.5" />,
      shortcut: "Ctrl+X",
      disabled: !hasSelection || isReadOnly || isPassword,
      onSelect: async () => {
        if (!selectedText || isReadOnly || isPassword) return;
        try {
          await navigator.clipboard.writeText(selectedText);
        } catch {
          // ignore clipboard errors
        }
        el.focus();
        const next = el.value.slice(0, start) + el.value.slice(end);
        setNativeEditableValue(el, next);
        requestAnimationFrame(() => {
          el.setSelectionRange(start, start);
        });
      },
    },
    {
      label: "复制",
      icon: <Copy className="h-3.5 w-3.5" />,
      shortcut: "Ctrl+C",
      disabled: !hasSelection || isPassword,
      onSelect: async () => {
        if (!selectedText || isPassword) return;
        try {
          await navigator.clipboard.writeText(selectedText);
        } catch {
          // ignore
        }
      },
    },
    {
      label: "粘贴",
      icon: <Clipboard className="h-3.5 w-3.5" />,
      shortcut: "Ctrl+V",
      disabled: isReadOnly,
      onSelect: async () => {
        if (isReadOnly) return;
        let pasted = "";
        try {
          pasted = await navigator.clipboard.readText();
        } catch {
          return;
        }
        if (!pasted) return;
        el.focus();
        const curStart = el.selectionStart ?? start;
        const curEnd = el.selectionEnd ?? end;
        const next = el.value.slice(0, curStart) + pasted + el.value.slice(curEnd);
        setNativeEditableValue(el, next);
        const caret = curStart + pasted.length;
        requestAnimationFrame(() => {
          el.setSelectionRange(caret, caret);
        });
      },
    },
    { type: "separator" },
    {
      label: "全选",
      icon: <TextSelect className="h-3.5 w-3.5" />,
      shortcut: "Ctrl+A",
      disabled: el.value.length === 0,
      onSelect: () => {
        el.focus();
        el.select();
      },
    },
  ];
  return items;
}

function normalizeItems(items: ContextMenuItem[]): ContextMenuItem[] {
  const result: ContextMenuItem[] = [];
  for (const item of items) {
    if (item.type === "separator") {
      if (result.length === 0 || result[result.length - 1]?.type === "separator") {
        continue;
      }
      result.push(item);
    } else {
      result.push(item);
    }
  }
  while (result.length > 0 && result[result.length - 1]?.type === "separator") {
    result.pop();
  }
  return result;
}

export function ContextMenuProvider({ children }: { children: ReactNode }) {
  const [menu, setMenu] = useState<ContextMenuState | null>(null);
  const [position, setPosition] = useState<{ left: number; top: number }>({
    left: -9999,
    top: -9999,
  });
  const [activeIndex, setActiveIndex] = useState<number>(-1);
  const menuRef = useRef<HTMLDivElement>(null);

  const closeContextMenu = useCallback(() => {
    setMenu(null);
    setActiveIndex(-1);
  }, []);

  const openContextMenu = useCallback(
    (e: ReactMouseEvent | MouseEvent, rawItems: ContextMenuItem[]) => {
      e.preventDefault();
      e.stopPropagation();
      const items = normalizeItems(rawItems);
      if (items.length === 0) {
        setMenu(null);
        return;
      }
      setMenu({ x: e.clientX, y: e.clientY, items });
      setPosition({ left: e.clientX, top: e.clientY });
      setActiveIndex(-1);
    },
    []
  );

  // 全局拦截 WebView2 默认右键菜单：输入框与选中文本自动提供桌面级基础菜单，其余空白处静默拦截
  useEffect(() => {
    const handleGlobalContextMenu = (e: MouseEvent) => {
      if (e.defaultPrevented) return;
      const target = e.target as HTMLElement | null;

      if (menuRef.current && target && menuRef.current.contains(target)) {
        e.preventDefault();
        return;
      }

      if (isEditableTextElement(target)) {
        e.preventDefault();
        const items = normalizeItems(buildEditableFallbackItems(target));
        setMenu({ x: e.clientX, y: e.clientY, items });
        setPosition({ left: e.clientX, top: e.clientY });
        setActiveIndex(-1);
        return;
      }

      const selectedText = window.getSelection()?.toString() ?? "";
      if (selectedText.trim().length > 0) {
        e.preventDefault();
        setMenu({
          x: e.clientX,
          y: e.clientY,
          items: [
            {
              label: "复制选中文本",
              icon: <Copy className="h-3.5 w-3.5" />,
              shortcut: "Ctrl+C",
              onSelect: async () => {
                try {
                  await navigator.clipboard.writeText(selectedText);
                } catch {
                  // ignore
                }
              },
            },
          ],
        });
        setPosition({ left: e.clientX, top: e.clientY });
        setActiveIndex(-1);
        return;
      }

      // 其余桌面空白区域抑制 WebView2 原生浏览器菜单
      e.preventDefault();
      setMenu(null);
    };

    window.addEventListener("contextmenu", handleGlobalContextMenu);
    return () => window.removeEventListener("contextmenu", handleGlobalContextMenu);
  }, []);

  // 根据视口尺寸修正弹出位置，避免溢出右下边界
  useLayoutEffect(() => {
    if (!menu || !menuRef.current) return;
    const rect = menuRef.current.getBoundingClientRect();
    const pad = 8;
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    let left = menu.x;
    let top = menu.y;
    if (left + rect.width + pad > vw) {
      left = Math.max(pad, menu.x - rect.width);
    }
    if (top + rect.height + pad > vh) {
      top = Math.max(pad, menu.y - rect.height);
    }
    setPosition({ left, top });
  }, [menu]);

  // 点击外部、失焦、滚动、键盘导航
  useEffect(() => {
    if (!menu) return;

    const selectableIndices: number[] = [];
    menu.items.forEach((item, idx) => {
      if ((item.type === undefined || item.type === "item") && !item.disabled) {
        selectableIndices.push(idx);
      }
    });

    const onMouseDown = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        closeContextMenu();
      }
    };

    const onScroll = () => {
      closeContextMenu();
    };

    const onBlur = () => {
      closeContextMenu();
    };

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        closeContextMenu();
        return;
      }
      if (selectableIndices.length === 0) return;

      if (e.key === "ArrowDown") {
        e.preventDefault();
        setActiveIndex((prev) => {
          const pos = selectableIndices.indexOf(prev);
          const next = pos === -1 ? 0 : (pos + 1) % selectableIndices.length;
          return selectableIndices[next] ?? -1;
        });
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setActiveIndex((prev) => {
          const pos = selectableIndices.indexOf(prev);
          const next =
            pos === -1
              ? selectableIndices.length - 1
              : (pos - 1 + selectableIndices.length) % selectableIndices.length;
          return selectableIndices[next] ?? -1;
        });
      } else if (e.key === "Enter" && activeIndex >= 0) {
        const target = menu.items[activeIndex];
        if (target && (target.type === undefined || target.type === "item") && !target.disabled) {
          e.preventDefault();
          closeContextMenu();
          void target.onSelect();
        }
      }
    };

    document.addEventListener("mousedown", onMouseDown, true);
    window.addEventListener("scroll", onScroll, true);
    window.addEventListener("resize", onBlur);
    window.addEventListener("blur", onBlur);
    window.addEventListener("keydown", onKeyDown, true);
    return () => {
      document.removeEventListener("mousedown", onMouseDown, true);
      window.removeEventListener("scroll", onScroll, true);
      window.removeEventListener("resize", onBlur);
      window.removeEventListener("blur", onBlur);
      window.removeEventListener("keydown", onKeyDown, true);
    };
  }, [menu, activeIndex, closeContextMenu]);

  const contextValue = useMemo(
    () => ({ openContextMenu, closeContextMenu }),
    [openContextMenu, closeContextMenu]
  );

  return (
    <ContextMenuContext.Provider value={contextValue}>
      {children}
      {menu &&
        createPortal(
          <div
            ref={menuRef}
            role="menu"
            style={{ left: `${position.left}px`, top: `${position.top}px` }}
            onContextMenu={(e) => e.preventDefault()}
            className="fixed z-[100] min-w-[196px] max-w-[260px] rounded-xl border border-border-strong bg-elevated/95 py-1.5 text-[12.5px] text-ink-primary shadow-[var(--shadow-elevated)] backdrop-blur-md select-none"
          >
            {menu.items.map((item, idx) => {
              if (item.type === "separator") {
                return (
                  <div
                    key={`sep-${idx}`}
                    role="separator"
                    className="my-1 h-px bg-border-subtle"
                  />
                );
              }
              if (item.type === "label") {
                return (
                  <div
                    key={`lbl-${idx}`}
                    className="px-3 py-1 text-[11px] font-medium text-ink-muted"
                  >
                    {item.label}
                  </div>
                );
              }
              const isHighlighted = activeIndex === idx;
              return (
                <button
                  key={`item-${idx}-${item.label}`}
                  type="button"
                  role="menuitem"
                  disabled={item.disabled}
                  onMouseEnter={() => {
                    if (!item.disabled) setActiveIndex(idx);
                  }}
                  onMouseLeave={() => {
                    if (activeIndex === idx) setActiveIndex(-1);
                  }}
                  onClick={() => {
                    if (item.disabled) return;
                    closeContextMenu();
                    void item.onSelect();
                  }}
                  className={`mx-1 flex w-[calc(100%-8px)] items-center justify-between gap-3 rounded-lg px-2.5 py-1.5 text-left transition-colors disabled:pointer-events-none disabled:opacity-35 ${
                    item.danger
                      ? isHighlighted
                        ? "bg-danger-soft text-danger"
                        : "text-danger hover:bg-danger-soft"
                      : isHighlighted
                        ? "bg-accent-soft text-ink-primary"
                        : "text-ink-primary hover:bg-accent-soft"
                  }`}
                >
                  <span className="flex min-w-0 items-center gap-2">
                    {item.icon && (
                      <span
                        className={`shrink-0 ${
                          item.danger ? "text-danger" : "text-ink-secondary"
                        }`}
                      >
                        {item.icon}
                      </span>
                    )}
                    <span className="truncate">{item.label}</span>
                  </span>
                  {item.shortcut && (
                    <span className="nib-mono shrink-0 text-[10.5px] text-ink-muted">
                      {item.shortcut}
                    </span>
                  )}
                </button>
              );
            })}
          </div>,
          document.body
        )}
    </ContextMenuContext.Provider>
  );
}
