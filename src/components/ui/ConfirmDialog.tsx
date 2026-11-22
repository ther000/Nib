import { AlertTriangle } from "lucide-react";
import { ModalShell } from "./ModalShell";

export interface ConfirmDialogState {
  title: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
  onConfirm: () => void | Promise<void>;
}

interface ConfirmDialogProps {
  dialog: ConfirmDialogState | null;
  busy?: boolean;
  onClose: () => void;
}

/**
 * 统一非阻塞确认框，替代会阻塞 WebView2 线程且风格割裂的原生 window.confirm。
 */
export function ConfirmDialog({ dialog, busy = false, onClose }: ConfirmDialogProps) {
  if (!dialog) return null;

  const isDanger = Boolean(dialog.danger);

  return (
    <ModalShell
      open={Boolean(dialog)}
      onClose={onClose}
      closeDisabled={busy}
      title={dialog.title}
      icon={
        <AlertTriangle
          className={`h-4 w-4 shrink-0 ${isDanger ? "text-danger" : "text-warn"}`}
        />
      }
      maxWidthClass="max-w-[440px]"
      footer={
        <>
          <button
            type="button"
            disabled={busy}
            onClick={onClose}
            className="rounded-lg px-3.5 py-1.5 text-xs font-medium text-ink-secondary transition-colors hover:bg-surface hover:text-ink-primary disabled:opacity-40"
          >
            {dialog.cancelLabel ?? "取消"}
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={() => void dialog.onConfirm()}
            className={`rounded-lg px-4 py-1.5 text-xs font-semibold transition-opacity hover:opacity-90 disabled:opacity-50 ${
              isDanger
                ? "bg-danger text-canvas"
                : "bg-accent text-canvas"
            }`}
          >
            {busy ? "处理中…" : dialog.confirmLabel ?? "确认"}
          </button>
        </>
      }
    >
      <div className="px-5 py-4 text-[13px] leading-relaxed whitespace-pre-line text-ink-secondary">
        {dialog.message}
      </div>
    </ModalShell>
  );
}
