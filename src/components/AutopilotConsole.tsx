import { useEffect, useMemo, useState } from "react";
import {
  AlertTriangle,
  Bot,
  CheckCircle2,
  Loader2,
  Play,
  Square,
  X,
} from "lucide-react";
import {
  AutopilotService,
  type AutopilotDecisionAction,
  type AutopilotState,
} from "../mygo";
import { useApp } from "../stores/AppContext";
import { formatCompactTokens, formatErrorMessage } from "../lib/constants";

const STEP_LABELS: Record<string, string> = {
  preparing: "准备中",
  outlining: "自动续写大纲",
  drafting: "写正文",
  linting: "本地扫描",
  critiquing: "AI 审校",
  polishing: "局部润色",
  extracting: "抽取记忆",
  committing: "同步设定",
  waiting_decision: "等待你的决定",
  done: "已完成",
};

export function AutopilotStatusBar() {
  const {
    activeBook,
    autopilotState,
    openAutopilot,
    setAutopilotState,
    jumpToChapter,
    notify,
  } = useApp();

  const [dismissedAt, setDismissedAt] = useState<string>("");
  const [resolving, setResolving] = useState(false);

  if (!activeBook || !autopilotState || autopilotState.bookId !== activeBook.id) {
    return null;
  }

  const isRunning = autopilotState.status === "running";
  const isWaiting = autopilotState.status === "waiting_decision";
  const isTerminal =
    autopilotState.status === "completed" ||
    autopilotState.status === "stopped" ||
    autopilotState.status === "failed";

  if (autopilotState.status === "idle") return null;
  if (isTerminal && dismissedAt === (autopilotState.updatedAt ?? "")) return null;

  const handleStop = async () => {
    try {
      const next = await AutopilotService.stopAutopilot(activeBook.id);
      setAutopilotState(next);
    } catch (err) {
      notify({ tone: "danger", message: "停止托管失败", detail: formatErrorMessage(err) });
    }
  };

  const handleResolve = async (action: AutopilotDecisionAction) => {
    setResolving(true);
    try {
      const next = await AutopilotService.resolveAutopilotGate(activeBook.id, action);
      setAutopilotState(next);
    } catch (err) {
      notify({ tone: "danger", message: "提交操作失败", detail: formatErrorMessage(err) });
    } finally {
      setResolving(false);
    }
  };

  return (
    <div
      className={`shrink-0 border-b border-border-subtle px-4 py-2 ${
        isWaiting ? "bg-warn-soft/50" : "bg-surface"
      }`}
    >
      <div className="mx-auto flex max-w-[1440px] flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2.5">
          {isRunning ? (
            <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-accent" />
          ) : isWaiting ? (
            <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-warn" />
          ) : autopilotState.status === "completed" ? (
            <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-ok" />
          ) : (
            <Bot className="h-3.5 w-3.5 shrink-0 text-ink-muted" />
          )}

          <span className="text-xs font-medium text-ink-primary">
            AI 托管 {autopilotState.completedChapters} / {autopilotState.targetChapters} 章
          </span>

          <span className="rounded bg-canvas px-1.5 py-0.5 text-xs text-ink-secondary">
            {STEP_LABELS[autopilotState.step] ?? autopilotState.step}
          </span>

          <span className="truncate text-xs text-ink-secondary">
            {autopilotState.message}
          </span>

          {autopilotState.tokensUsed > 0 && (
            <span className="nib-mono hidden text-xs text-ink-muted sm:inline">
              {formatCompactTokens(autopilotState.tokensUsed)} tokens
            </span>
          )}
        </div>

        <div className="flex shrink-0 items-center gap-2">
          {isWaiting && autopilotState.gate ? (
            <>
              {autopilotState.gate.options.map((opt) => (
                <button
                  key={opt.action}
                  type="button"
                  disabled={resolving}
                  title={opt.description}
                  onClick={() => void handleResolve(opt.action)}
                  className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors disabled:opacity-50 ${
                    opt.action === "retry_chapter" || opt.action === "continue"
                      ? "bg-accent text-white hover:bg-accent-hover"
                      : "border border-border-strong bg-elevated text-ink-primary hover:bg-canvas"
                  }`}
                >
                  {opt.label}
                </button>
              ))}
            </>
          ) : (
            <>
              {autopilotState.currentChapterId > 0 && (
                <button
                  type="button"
                  onClick={() => void jumpToChapter(autopilotState.currentChapterId)}
                  className="rounded-md px-2 py-1 text-xs text-ink-secondary transition-colors hover:bg-elevated hover:text-ink-primary"
                >
                  查看当前章
                </button>
              )}
              <button
                type="button"
                onClick={openAutopilot}
                className="rounded-md border border-border-subtle bg-elevated px-2.5 py-1 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary"
              >
                进度详情
              </button>
              {isRunning ? (
                <button
                  type="button"
                  onClick={() => void handleStop()}
                  className="flex items-center gap-1 rounded-md border border-border-strong bg-elevated px-2.5 py-1 text-xs font-medium text-ink-primary transition-colors hover:bg-canvas"
                >
                  <Square className="h-3 w-3" />
                  <span>停止</span>
                </button>
              ) : (
                <button
                  type="button"
                  onClick={() => setDismissedAt(autopilotState.updatedAt ?? "dismissed")}
                  aria-label="关闭状态条"
                  className="rounded-md p-1 text-ink-muted transition-colors hover:bg-elevated hover:text-ink-primary"
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              )}
            </>
          )}
        </div>
      </div>

      {isWaiting && autopilotState.gate?.detail && (
        <div className="mx-auto mt-1.5 max-w-[1440px] pl-6 text-xs text-ink-secondary">
          {autopilotState.gate.detail}
        </div>
      )}
    </div>
  );
}

export function AutopilotModal() {
  const {
    autopilotOpen,
    closeAutopilot,
    activeBook,
    volumes,
    providers,
    modelRoutes,
    autopilotState,
    setAutopilotState,
    openSettings,
    notify,
  } = useApp();

  const [targetChapters, setTargetChapters] = useState(3);
  const [volumeId, setVolumeId] = useState<number>(0);
  const [autoOutline, setAutoOutline] = useState(true);
  const [autoPolish, setAutoPolish] = useState(true);
  const [autoCommitMemory, setAutoCommitMemory] = useState(true);
  const [arcGoal, setArcGoal] = useState("");
  const [maxTokensBudget, setMaxTokensBudget] = useState("");
  const [starting, setStarting] = useState(false);

  const hasDraftRoute = useMemo(() => {
    const enabledProviderIds = new Set(
      providers.filter((p) => p.enabled && p.models.length > 0).map((p) => p.id),
    );
    return modelRoutes.some(
      (r) =>
        r.stage === "draft" &&
        r.providerId != null &&
        enabledProviderIds.has(r.providerId) &&
        r.modelName.trim().length > 0,
    );
  }, [providers, modelRoutes]);

  useEffect(() => {
    if (!autopilotOpen) return;
    const lastVol = volumes[volumes.length - 1];
    setVolumeId(lastVol ? lastVol.id : 0);
  }, [autopilotOpen, volumes]);

  useEffect(() => {
    if (!autopilotOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") closeAutopilot();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [autopilotOpen, closeAutopilot]);

  if (!autopilotOpen || !activeBook) return null;

  const activeState =
    autopilotState?.bookId === activeBook.id ? autopilotState : null;
  const isRunning =
    activeState != null &&
    (activeState.status === "running" || activeState.status === "waiting_decision");

  const handleStart = async () => {
    if (!hasDraftRoute) {
      const needProvider = !providers.some((p) => p.enabled && p.models.length > 0);
      notify({
        tone: "warn",
        message: needProvider ? "请先连接模型服务并启用模型" : "请先为「写作」分配模型",
        actionLabel: "去设置",
        onAction: () => {
          closeAutopilot();
          openSettings(needProvider ? "providers" : "routes");
        },
      });
      return;
    }

    const parsedChapters = Math.min(20, Math.max(1, targetChapters));
    const parsedBudget = maxTokensBudget.trim()
      ? Number.parseInt(maxTokensBudget.trim(), 10)
      : 0;
    if (!Number.isFinite(parsedBudget) || parsedBudget < 0) {
      notify({ tone: "warn", message: "Token 上限需为非负整数" });
      return;
    }

    setStarting(true);
    try {
      const state: AutopilotState = await AutopilotService.startAutopilot({
        bookId: activeBook.id,
        volumeId,
        targetChapters: parsedChapters,
        autoOutline,
        autoPolish,
        autoCommitMemory,
        maxTokensBudget: parsedBudget,
        arcGoal: arcGoal.trim(),
      });
      setAutopilotState(state);
      closeAutopilot();
      notify({
        tone: "ok",
        message: `已启动 AI 托管（目标 ${parsedChapters} 章）`,
      });
    } catch (err) {
      notify({ tone: "danger", message: "启动托管失败", detail: formatErrorMessage(err) });
    } finally {
      setStarting(false);
    }
  };

  const handleStop = async () => {
    try {
      const next = await AutopilotService.stopAutopilot(activeBook.id);
      setAutopilotState(next);
    } catch (err) {
      notify({ tone: "danger", message: "停止托管失败", detail: formatErrorMessage(err) });
    }
  };

  const logs = autopilotState?.bookId === activeBook.id ? autopilotState.logs : [];

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/55 p-4"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) closeAutopilot();
      }}
    >
      <div className="flex max-h-[88vh] w-full max-w-[560px] flex-col overflow-hidden rounded-xl border border-border-strong bg-elevated shadow-[var(--shadow-elevated)]">
        <div className="flex items-center justify-between border-b border-border-subtle px-5 py-3.5">
          <div className="flex items-center gap-2">
            <Bot className="h-4 w-4 text-accent" />
            <h2 className="text-base font-medium text-ink-primary">AI 自动托管</h2>
          </div>
          <button
            type="button"
            onClick={closeAutopilot}
            aria-label="关闭"
            className="rounded-lg p-1 text-ink-muted transition-colors hover:bg-surface hover:text-ink-primary"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
          {!isRunning ? (
            <>
              <div>
                <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                  本次自动推进章数
                </label>
                <div className="flex items-center gap-2">
                  {[1, 3, 5, 10].map((cnt) => (
                    <button
                      key={cnt}
                      type="button"
                      onClick={() => setTargetChapters(cnt)}
                      className={`flex-1 rounded-lg border py-1.5 text-[13px] font-medium transition-colors ${
                        targetChapters === cnt
                          ? "border-accent bg-accent-soft text-accent"
                          : "border-border-subtle bg-canvas text-ink-secondary hover:border-border-strong"
                      }`}
                    >
                      {cnt} 章
                    </button>
                  ))}
                  <input
                    type="number"
                    min={1}
                    max={20}
                    value={targetChapters}
                    onChange={(e) =>
                      setTargetChapters(
                        Math.min(20, Math.max(1, Number.parseInt(e.target.value, 10) || 1)),
                      )
                    }
                    className="nib-mono w-16 rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-center text-[13px] text-ink-primary focus:border-accent focus:outline-none"
                  />
                </div>
              </div>

              {volumes.length > 0 && (
                <div>
                  <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                    目标分卷
                  </label>
                  <select
                    value={volumeId}
                    onChange={(e) => setVolumeId(Number(e.target.value))}
                    className="w-full rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] text-ink-primary focus:border-accent focus:outline-none"
                  >
                    {volumes.map((v) => (
                      <option key={v.id} value={v.id}>
                        第 {v.seq} 卷 {v.title}
                      </option>
                    ))}
                  </select>
                </div>
              )}

              <div className="divide-y divide-border-subtle rounded-lg border border-border-subtle bg-canvas">
                <label className="flex cursor-pointer items-center justify-between gap-3 px-3.5 py-2.5">
                  <div>
                    <div className="text-[13px] font-medium text-ink-primary">
                      章纲不足时自动续写
                    </div>
                    <div className="text-xs text-ink-muted">
                      自动补齐后续章节大纲、节拍与出场角色
                    </div>
                  </div>
                  <input
                    type="checkbox"
                    checked={autoOutline}
                    onChange={(e) => setAutoOutline(e.target.checked)}
                    className="h-4 w-4 accent-[var(--accent)]"
                  />
                </label>

                <label className="flex cursor-pointer items-center justify-between gap-3 px-3.5 py-2.5">
                  <div>
                    <div className="text-[13px] font-medium text-ink-primary">
                      自动审校并局部润色
                    </div>
                    <div className="text-xs text-ink-muted">
                      写完自动检查并优化问题段落，改前自动存历史快照
                    </div>
                  </div>
                  <input
                    type="checkbox"
                    checked={autoPolish}
                    onChange={(e) => setAutoPolish(e.target.checked)}
                    className="h-4 w-4 accent-[var(--accent)]"
                  />
                </label>

                <label className="flex cursor-pointer items-center justify-between gap-3 px-3.5 py-2.5">
                  <div>
                    <div className="text-[13px] font-medium text-ink-primary">
                      自动同步设定与伏笔
                    </div>
                    <div className="text-xs text-ink-muted">
                      每章写完自动更新角色状态并喂给下一章，保留完整记录
                    </div>
                  </div>
                  <input
                    type="checkbox"
                    checked={autoCommitMemory}
                    onChange={(e) => setAutoCommitMemory(e.target.checked)}
                    className="h-4 w-4 accent-[var(--accent)]"
                  />
                </label>
              </div>

              <div className="grid grid-cols-1 gap-3 sm:grid-cols-[1fr_160px]">
                <div>
                  <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                    剧情方向提示（选填）
                  </label>
                  <input
                    type="text"
                    value={arcGoal}
                    onChange={(e) => setArcGoal(e.target.value)}
                    placeholder="例如：这几章安排外门大比并揭露反派阴谋"
                    className="w-full rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                  />
                </div>
                <div>
                  <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                    Token 上限（选填）
                  </label>
                  <input
                    type="number"
                    min={0}
                    step={10000}
                    value={maxTokensBudget}
                    onChange={(e) => setMaxTokensBudget(e.target.value)}
                    placeholder="留空不限"
                    className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-[13px] text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                  />
                </div>
              </div>
            </>
          ) : (
            <div className="rounded-lg border border-border-subtle bg-canvas px-3.5 py-3">
              <div className="flex items-center justify-between">
                <span className="text-[13px] font-medium text-ink-primary">
                  正在推进（已完成 {activeState?.completedChapters ?? 0} /{" "}
                  {activeState?.targetChapters ?? 0} 章）
                </span>
                {(activeState?.tokensUsed ?? 0) > 0 && (
                  <span className="nib-mono text-xs text-ink-muted">
                    已用 {formatCompactTokens(activeState?.tokensUsed ?? 0)} tokens
                  </span>
                )}
              </div>
              <div className="mt-1 text-xs text-ink-secondary">{activeState?.message}</div>
            </div>
          )}

          {logs.length > 0 && (
            <div>
              <div className="mb-1.5 text-xs font-medium text-ink-secondary">运行记录</div>
              <div className="max-h-48 space-y-1 overflow-y-auto rounded-lg border border-border-subtle bg-canvas p-3">
                {logs.map((entry, idx) => (
                  <div key={`${entry.time}-${idx}`} className="flex items-start gap-2 text-xs">
                    <span
                      className={`mt-1 h-1.5 w-1.5 shrink-0 rounded-full ${
                        entry.tone === "ok"
                          ? "bg-ok"
                          : entry.tone === "warn"
                            ? "bg-warn"
                            : entry.tone === "danger"
                              ? "bg-danger"
                              : "bg-ink-muted"
                      }`}
                    />
                    <span className="leading-relaxed text-ink-secondary">{entry.message}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>

        <div className="flex items-center justify-end gap-2 border-t border-border-subtle bg-surface/50 px-5 py-3">
          {isRunning ? (
            <>
              <button
                type="button"
                onClick={closeAutopilot}
                className="rounded-lg px-3.5 py-1.5 text-[13px] font-medium text-ink-secondary transition-colors hover:bg-surface hover:text-ink-primary"
              >
                后台继续
              </button>
              <button
                type="button"
                onClick={() => void handleStop()}
                className="flex items-center gap-1.5 rounded-lg border border-border-strong bg-surface px-4 py-1.5 text-[13px] font-medium text-ink-primary transition-colors hover:bg-canvas"
              >
                <Square className="h-3.5 w-3.5" />
                <span>停止托管</span>
              </button>
            </>
          ) : (
            <>
              <button
                type="button"
                onClick={closeAutopilot}
                className="rounded-lg px-3.5 py-1.5 text-[13px] font-medium text-ink-secondary transition-colors hover:bg-surface hover:text-ink-primary"
              >
                取消
              </button>
              <button
                type="button"
                disabled={starting}
                onClick={() => void handleStart()}
                className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-1.5 text-[13px] font-medium text-white transition-colors hover:bg-accent-hover disabled:opacity-50"
              >
                {starting ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Play className="h-3.5 w-3.5" />
                )}
                <span>{starting ? "启动中…" : "开始托管"}</span>
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
