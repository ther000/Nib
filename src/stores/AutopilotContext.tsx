import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { isMyGo } from "mygo-runtime";
import { AutopilotService, events, type AutopilotState } from "../mygo";

interface AutopilotStateValue {
  autopilotState: AutopilotState | null;
}

interface AutopilotActionsValue {
  setAutopilotState: (state: AutopilotState | null) => void;
}

const AutopilotStateContext = createContext<AutopilotStateValue | null>(null);
const AutopilotActionsContext = createContext<AutopilotActionsValue | null>(null);

interface AutopilotProviderProps {
  activeBookId: number | null;
  onProgressRefresh: (bookId: number) => void;
  children: ReactNode;
}

export function AutopilotProvider({
  activeBookId,
  onProgressRefresh,
  children,
}: AutopilotProviderProps) {
  const [autopilotState, setAutopilotState] = useState<AutopilotState | null>(null);

  const activeBookIdRef = useRef(activeBookId);
  activeBookIdRef.current = activeBookId;
  const onProgressRefreshRef = useRef(onProgressRefresh);
  onProgressRefreshRef.current = onProgressRefresh;

  useEffect(() => {
    if (!activeBookId) {
      setAutopilotState(null);
      return;
    }
    let cancelled = false;
    void AutopilotService.getAutopilotState(activeBookId)
      .then((state) => {
        if (!cancelled) setAutopilotState(state);
      })
      .catch(() => {
        if (!cancelled) setAutopilotState(null);
      });
    return () => {
      cancelled = true;
    };
  }, [activeBookId]);

  useEffect(() => {
    if (!isMyGo()) return;
    let lastCompleted = -1;
    let lastStatus = "";
    let unsub: (() => void) | null = null;
    try {
      unsub = events.autopilotProgress.on((prog) => {
        const curBookId = activeBookIdRef.current;
        if (curBookId && prog.bookId !== curBookId) return;
        setAutopilotState(prog);
        const progressed = prog.completedChapters !== lastCompleted;
        const finished =
          (prog.status === "completed" ||
            prog.status === "stopped" ||
            prog.status === "failed") &&
          prog.status !== lastStatus;
        lastCompleted = prog.completedChapters;
        lastStatus = prog.status;
        if (progressed || finished) {
          onProgressRefreshRef.current(prog.bookId);
        }
      });
    } catch {
      // ignore outside mygo runtime
    }
    return () => {
      unsub?.();
    };
  }, []);

  const stateValue = useMemo<AutopilotStateValue>(
    () => ({ autopilotState }),
    [autopilotState]
  );
  const actionsValue = useMemo<AutopilotActionsValue>(
    () => ({ setAutopilotState }),
    []
  );

  return (
    <AutopilotActionsContext.Provider value={actionsValue}>
      <AutopilotStateContext.Provider value={stateValue}>{children}</AutopilotStateContext.Provider>
    </AutopilotActionsContext.Provider>
  );
}

export function useAutopilotState(): AutopilotStateValue {
  const ctx = useContext(AutopilotStateContext);
  if (!ctx) {
    throw new Error("useAutopilotState must be used inside AutopilotProvider");
  }
  return ctx;
}

export function useAutopilotActions(): AutopilotActionsValue {
  const ctx = useContext(AutopilotActionsContext);
  if (!ctx) {
    throw new Error("useAutopilotActions must be used inside AutopilotProvider");
  }
  return ctx;
}
