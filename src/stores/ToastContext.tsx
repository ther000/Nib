import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";

export type ToastTone = "info" | "ok" | "warn" | "danger";

export interface ToastItem {
  id: number;
  tone: ToastTone;
  message: string;
  detail?: string;
  actionLabel?: string;
  onAction?: () => void;
}

export type NotifyInput = Omit<ToastItem, "id"> & { durationMs?: number };

interface ToastActionsValue {
  notify: (input: NotifyInput) => void;
  dismissToast: (id: number) => void;
}

const ToastStateContext = createContext<ToastItem[] | null>(null);
const ToastActionsContext = createContext<ToastActionsValue | null>(null);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastItem[]>([]);
  const toastSeqRef = useRef(1);
  const toastTimersRef = useRef<Map<number, number>>(new Map());

  const dismissToast = useCallback((id: number) => {
    const timer = toastTimersRef.current.get(id);
    if (timer !== undefined) {
      window.clearTimeout(timer);
      toastTimersRef.current.delete(id);
    }
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const notify = useCallback((input: NotifyInput) => {
    const id = toastSeqRef.current++;
    const duration = input.durationMs ?? (input.tone === "danger" ? 6500 : 3800);
    const item: ToastItem = {
      id,
      tone: input.tone,
      message: input.message,
      detail: input.detail,
      actionLabel: input.actionLabel,
      onAction: input.onAction,
    };
    setToasts((prev) => [...prev.slice(-3), item]);
    if (duration > 0) {
      const timer = window.setTimeout(() => {
        toastTimersRef.current.delete(id);
        setToasts((prev) => prev.filter((t) => t.id !== id));
      }, duration);
      toastTimersRef.current.set(id, timer);
    }
  }, []);

  useEffect(() => {
    const timers = toastTimersRef.current;
    return () => {
      for (const timer of timers.values()) {
        window.clearTimeout(timer);
      }
      timers.clear();
    };
  }, []);

  const actions = useMemo<ToastActionsValue>(
    () => ({ notify, dismissToast }),
    [notify, dismissToast]
  );

  return (
    <ToastActionsContext.Provider value={actions}>
      <ToastStateContext.Provider value={toasts}>{children}</ToastStateContext.Provider>
    </ToastActionsContext.Provider>
  );
}

export function useToastState(): ToastItem[] {
  const ctx = useContext(ToastStateContext);
  if (!ctx) {
    throw new Error("useToastState must be used inside ToastProvider");
  }
  return ctx;
}

export function useToastActions(): ToastActionsValue {
  const ctx = useContext(ToastActionsContext);
  if (!ctx) {
    throw new Error("useToastActions must be used inside ToastProvider");
  }
  return ctx;
}
