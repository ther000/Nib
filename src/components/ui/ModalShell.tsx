import { useEffect, useRef, type ReactNode } from "react";
import { X } from "lucide-react";

interface ModalShellProps {
  open: boolean;
  onClose: () => void;
  closeDisabled?: boolean;
  title?: ReactNode;
  icon?: ReactNode;
  maxWidthClass?: string;
  maxHeightClass?: string;
  align?: "center" | "top";
  hideHeader?: boolean;
  headerExtra?: ReactNode;
  footer?: ReactNode;
  children: ReactNode;
}

/**
 * 统一桌面弹窗外壳：提供一致的 backdrop、data-nib-modal 快捷键屏蔽标记、
 * ARIA dialog 语义与顶层弹窗优先的 Escape 安全关闭逻辑。
 */
export function ModalShell({
  open,
  onClose,
  closeDisabled = false,
  title,
  icon,
  maxWidthClass = "max-w-[540px]",
  maxHeightClass = "max-h-[88vh]",
  align = "center",
  hideHeader = false,
  headerExtra,
  footer,
  children,
}: ModalShellProps) {
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || closeDisabled) return;
      const modals = document.querySelectorAll('[data-nib-modal="true"]');
      if (modals.length > 0 && modals[modals.length - 1] !== rootRef.current) {
        return;
      }
      e.preventDefault();
      onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, closeDisabled, onClose]);

  if (!open) return null;

  const alignClass =
    align === "top"
      ? "items-start justify-center px-4 pt-[11vh]"
      : "items-center justify-center p-4";

  return (
    <div
      ref={rootRef}
      data-nib-modal="true"
      role="dialog"
      aria-modal="true"
      className={`fixed inset-0 z-50 flex bg-black/55 backdrop-blur-[2px] ${alignClass}`}
      onMouseDown={(e) => {
        if (e.target === e.currentTarget && !closeDisabled) {
          onClose();
        }
      }}
    >
      <div
        className={`flex w-full flex-col overflow-hidden rounded-xl border border-border-strong bg-elevated shadow-[var(--shadow-elevated)] ${maxWidthClass} ${maxHeightClass}`}
      >
        {!hideHeader && (
          <div className="flex shrink-0 items-center justify-between gap-2 border-b border-border-subtle px-5 py-3.5">
            <div className="flex min-w-0 items-center gap-2.5">
              {icon}
              {title !== undefined && (
                <h2 className="truncate text-sm font-medium text-ink-primary">{title}</h2>
              )}
            </div>
            <div className="flex shrink-0 items-center gap-2">
              {headerExtra}
              <button
                type="button"
                disabled={closeDisabled}
                onClick={onClose}
                aria-label="关闭弹窗"
                className="rounded-lg p-1 text-ink-muted transition-colors hover:bg-surface hover:text-ink-primary disabled:opacity-40"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
          </div>
        )}

        <div className="flex min-h-0 flex-1 flex-col overflow-hidden">{children}</div>

        {footer && (
          <div className="flex shrink-0 items-center justify-end gap-2 border-t border-border-subtle bg-surface/50 px-5 py-3">
            {footer}
          </div>
        )}
      </div>
    </div>
  );
}
