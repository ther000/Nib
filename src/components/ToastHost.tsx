import { AlertCircle, CheckCircle2, Info, AlertTriangle, X } from "lucide-react";
import { useApp, type ToastTone } from "../stores/AppContext";

const TONE_STYLES: Record<ToastTone, { border: string; iconClass: string; Icon: typeof Info }> = {
  info: { border: "border-l-accent", iconClass: "text-accent", Icon: Info },
  ok: { border: "border-l-ok", iconClass: "text-ok", Icon: CheckCircle2 },
  warn: { border: "border-l-warn", iconClass: "text-warn", Icon: AlertTriangle },
  danger: { border: "border-l-danger", iconClass: "text-danger", Icon: AlertCircle },
};

export function ToastHost() {
  const { toasts, dismissToast } = useApp();

  if (toasts.length === 0) return null;

  return (
    <div
      className="pointer-events-none fixed right-4 bottom-12 z-50 flex w-[340px] max-w-[calc(100vw-2rem)] flex-col gap-2"
      role="status"
      aria-live="polite"
    >
      {toasts.map((toast) => {
        const style = TONE_STYLES[toast.tone];
        const Icon = style.Icon;
        return (
          <div
            key={toast.id}
            className={`pointer-events-auto flex items-start gap-2.5 rounded-md border border-border-strong border-l-2 ${style.border} bg-elevated px-3.5 py-2.5 shadow-[var(--shadow-elevated)]`}
          >
            <Icon className={`mt-0.5 h-4 w-4 shrink-0 ${style.iconClass}`} />
            <div className="min-w-0 flex-1">
              <div className="text-[13px] leading-snug font-medium text-ink-primary">
                {toast.message}
              </div>
              {toast.detail && (
                <div className="mt-0.5 text-xs leading-relaxed text-ink-secondary break-words">
                  {toast.detail}
                </div>
              )}
              {toast.actionLabel && toast.onAction && (
                <button
                  type="button"
                  onClick={() => {
                    toast.onAction?.();
                    dismissToast(toast.id);
                  }}
                  className="mt-1.5 text-xs font-medium text-accent transition-opacity hover:opacity-80"
                >
                  {toast.actionLabel}
                </button>
              )}
            </div>
            <button
              type="button"
              onClick={() => dismissToast(toast.id)}
              aria-label="关闭通知"
              className="rounded p-0.5 text-ink-muted transition-colors hover:bg-surface hover:text-ink-primary"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
        );
      })}
    </div>
  );
}
