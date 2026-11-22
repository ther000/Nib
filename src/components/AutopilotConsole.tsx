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
import { useAutopilotActions, useAutopilotState } from "../stores/AutopilotContext";
import { hasReadyModelRoute } from "../lib/modelReady";
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
    jumpToChapter,
    openDirector,
    notify,
  } = useApp();
  const { autopilotState } = useAutopilotState();
  const { setAutopilotState } = useAutopilotActions();

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
  const canResume =
    (autopilotState.status === "stopped" || autopilotState.status === "failed") &&
    autopilotState.completedChapters < autopilotState.targetChapters;
  const statusKey =
    autopilotState.updatedAt || `${autopilotState.status}:${autopilotState.completedChapters}`;

  if (autopilotState.status === "idle") return null;
  if (isTerminal && dismissedAt === statusKey) return null;

  const handleResume = async () => {
    setResolving(true);
    try {
      const next = await AutopilotService.resumeAutopilot(activeBook.id);
      setAutopilotState(next);
      notify({ tone: "ok", message: "已从断点继续托管" });
    } catch (err) {
      notify({ tone: "danger", message: "继续托管失败", detail: formatErrorMessage(err) });
    } finally {
      setResolving(false);
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
      className={`mx-2 mb-2 shrink-0 rounded-xl border border-border-subtle px-4 py-2 ${
        isWaiting ? "bg-warn-soft/70" : "nib-glass-panel"
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

        <div className="flex shrink-0 flex-wrap items-center gap-2">
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
                      ? "bg-accent text-canvas hover:bg-accent-hover"
                      : "border border-border-strong bg-elevated text-ink-primary hover:bg-canvas"
                  }`}
                >
                  {opt.label}
                </button>
              ))}
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
                onClick={() => openDirector("autopilot")}
                className="rounded-md border border-border-subtle bg-elevated px-2.5 py-1 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary"
              >
                进度详情
              </button>
              <button
                type="button"
                onClick={() => void handleStop()}
                className="flex items-center gap-1 rounded-md border border-border-strong bg-elevated px-2.5 py-1 text-xs font-medium text-ink-primary transition-colors hover:bg-canvas"
              >
                <Square className="h-3 w-3" />
                <span>停止</span>
              </button>
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
                onClick={() => openDirector("autopilot")}
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
                <>
                  {canResume && (
                    <button
                      type="button"
                      disabled={resolving}
                      onClick={() => void handleResume()}
                      className="flex items-center gap-1 rounded-md bg-accent px-2.5 py-1 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
                    >
                      <Play className="h-3 w-3 fill-current" />
                      <span>继续剩余 {autopilotState.targetChapters - autopilotState.completedChapters} 章</span>
                    </button>
                  )}
                  <button
                    type="button"
                    onClick={() => setDismissedAt(statusKey)}
                    aria-label="关闭状态条"
                    className="rounded-md p-1 text-ink-muted transition-colors hover:bg-elevated hover:text-ink-primary"
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                </>
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

export function AutopilotPanel() {
  const {
    activeBook,
    volumes,
    providers,
    modelRoutes,
    jumpToChapter,
    openSettings,
    notify,
  } = useApp();
  const { autopilotState } = useAutopilotState();
  const { setAutopilotState } = useAutopilotActions();

  const [targetChapters, setTargetChapters] = useState(3);
  const [volumeId, setVolumeId] = useState<number>(0);
  const [autoOutline, setAutoOutline] = useState(true);
  const [autoPolish, setAutoPolish] = useState(true);
  const [autoCommitMemory, setAutoCommitMemory] = useState(true);
  const [arcGoal, setArcGoal] = useState("");
  const [maxTokensBudget, setMaxTokensBudget] = useState("");
  const [starting, setStarting] = useState(false);
  const [resolving, setResolving] = useState(false);

  const hasDraftRoute = useMemo(
    () => hasReadyModelRoute(providers, modelRoutes, ["draft"]),
    [providers, modelRoutes]
  );

  useEffect(() => {
    const lastVol = volumes[volumes.length - 1];
    setVolumeId(lastVol ? lastVol.id : 0);
  }, [activeBook?.id, volumes]);

  if (!activeBook) return null;

  const activeState =
    autopilotState?.bookId === activeBook.id ? autopilotState : null;
  const isRunning =
    activeState != null &&
    (activeState.status === "running" || activeState.status === "waiting_decision");
  const isWaiting = activeState?.status === "waiting_decision";
  const canResume =
    activeState != null &&
    (activeState.status === "stopped" || activeState.status === "failed") &&
    activeState.completedChapters < activeState.targetChapters;

  const handleStart = async () => {
    if (!hasDraftRoute) {
      const needProvider = !providers.some((p) => p.enabled && p.models.length > 0);
      notify({
        tone: "warn",
        message: needProvider ? "请先连接模型服务并启用模型" : "请先为「写作」分配模型",
        actionLabel: "去设置",
        onAction: () => openSettings(needProvider ? "providers" : "routes"),
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

  const handleResume = async () => {
    setResolving(true);
    try {
      const next = await AutopilotService.resumeAutopilot(activeBook.id);
      setAutopilotState(next);
      notify({ tone: "ok", message: "已从断点继续托管" });
    } catch (err) {
      notify({ tone: "danger", message: "继续托管失败", detail: formatErrorMessage(err) });
    } finally {
      setResolving(false);
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

  const logs = activeState?.logs ?? [];
  const progressPct =
    activeState && activeState.targetChapters > 0
      ? Math.min(100, Math.round((activeState.completedChapters / activeState.targetChapters) * 100))
      : 0;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-1 flex-col gap-3.5 overflow-y-auto px-4 py-4">
        {!isRunning ? (
          <>
            {canResume && activeState && (
              <div className="rounded-lg border border-warn/35 bg-warn-soft/50 p-3">
                <div className="flex items-center justify-between gap-2 text-xs">
                  <span className="font-medium text-ink-primary">
                    上次中断在 {activeState.completedChapters} / {activeState.targetChapters} 章
                  </span>
                  <span className="text-ink-muted">
                    {activeState.status === "failed" ? "异常暂停" : "已手动停止"}
                  </span>
                </div>
                {activeState.message && (
                  <div className="mt-1 text-xs leading-relaxed text-ink-secondary">
                    {activeState.message}
                  </div>
                )}
                <button
                  type="button"
                  disabled={resolving}
                  onClick={() => void handleResume()}
                  className="mt-2.5 flex w-full items-center justify-center gap-1.5 rounded-md bg-accent px-3 py-1.5 text-xs font-medium text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
                >
                  {resolving ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Play className="h-3 w-3 fill-current" />
                  )}
                  <span>
                    继续剩余 {activeState.targetChapters - activeState.completedChapters} 章
                  </span>
                </button>
              </div>
            )}

            <div>
              <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                本次自动推进章数
              </label>
              <div className="flex items-center gap-1.5">
                {[1, 3, 5, 10].map((cnt) => (
                  <button
                    key={cnt}
                    type="button"
                    onClick={() => setTargetChapters(cnt)}
                    className={`flex-1 rounded-lg border py-1.5 text-xs font-medium transition-colors ${
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
                  className="nib-mono w-14 rounded-lg border border-border-subtle bg-canvas px-2 py-1.5 text-center text-xs text-ink-primary focus:border-accent focus:outline-none"
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
                  className="w-full rounded-lg border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
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
              <label className="flex cursor-pointer items-center justify-between gap-3 px-3 py-2.5">
                <div>
                  <div className="text-xs font-medium text-ink-primary">
                    章纲不足时自动续写
                  </div>
                  <div className="mt-0.5 text-[11px] text-ink-muted">
                    自动补齐后续章节大纲、节拍与出场角色
                  </div>
                </div>
                <input
                  type="checkbox"
                  checked={autoOutline}
                  onChange={(e) => setAutoOutline(e.target.checked)}
                  className="h-4 w-4 shrink-0 accent-[var(--accent)]"
                />
              </label>

              <label className="flex cursor-pointer items-center justify-between gap-3 px-3 py-2.5">
                <div>
                  <div className="text-xs font-medium text-ink-primary">
                    自动审校并局部润色
                  </div>
                  <div className="mt-0.5 text-[11px] text-ink-muted">
                    写完自动检查并优化问题段落，改前自动存快照
                  </div>
                </div>
                <input
                  type="checkbox"
                  checked={autoPolish}
                  onChange={(e) => setAutoPolish(e.target.checked)}
                  className="h-4 w-4 shrink-0 accent-[var(--accent)]"
                />
              </label>

              <label className="flex cursor-pointer items-center justify-between gap-3 px-3 py-2.5">
                <div>
                  <div className="text-xs font-medium text-ink-primary">
                    自动同步设定与伏笔
                  </div>
                  <div className="mt-0.5 text-[11px] text-ink-muted">
                    每章写完自动更新角色状态并喂给下一章
                  </div>
                </div>
                <input
                  type="checkbox"
                  checked={autoCommitMemory}
                  onChange={(e) => setAutoCommitMemory(e.target.checked)}
                  className="h-4 w-4 shrink-0 accent-[var(--accent)]"
                />
              </label>
            </div>

            <div>
              <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                剧情方向提示（选填）
              </label>
              <input
                type="text"
                value={arcGoal}
                onChange={(e) => setArcGoal(e.target.value)}
                placeholder="例如：这几章安排外门大比并揭露反派阴谋"
                className="w-full rounded-lg border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
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
                className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
              />
            </div>
          </>
        ) : (
          <div
            className={`rounded-lg border p-3.5 ${
              isWaiting
                ? "border-warn/40 bg-warn-soft/50"
                : "border-border-subtle bg-canvas"
            }`}
          >
            <div className="flex items-center justify-between gap-2">
              <div className="flex items-center gap-2 text-xs font-medium text-ink-primary">
                {isWaiting ? (
                  <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-warn" />
                ) : (
                  <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-accent" />
                )}
                <span>
                  已完成 {activeState?.completedChapters ?? 0} /{" "}
                  {activeState?.targetChapters ?? 0} 章
                </span>
              </div>
              <span className="rounded bg-surface px-1.5 py-0.5 text-[11px] text-ink-secondary">
                {STEP_LABELS[activeState?.step ?? ""] ?? activeState?.step}
              </span>
            </div>

            <div className="mt-2.5 h-1.5 overflow-hidden rounded-full bg-surface">
              <div
                className={`h-full rounded-full transition-all ${
                  isWaiting ? "bg-warn" : "bg-accent"
                }`}
                style={{ width: `${progressPct}%` }}
              />
            </div>

            <div className="mt-2 text-xs leading-relaxed text-ink-secondary">
              {activeState?.message}
            </div>

            <div className="mt-2 flex items-center justify-between gap-2 text-[11px] text-ink-muted">
              {(activeState?.tokensUsed ?? 0) > 0 ? (
                <span className="nib-mono">
                  已用 {formatCompactTokens(activeState?.tokensUsed ?? 0)} tokens
                </span>
              ) : (
                <span />
              )}
              {(activeState?.currentChapterId ?? 0) > 0 && (
                <button
                  type="button"
                  onClick={() => void jumpToChapter(activeState!.currentChapterId)}
                  className="text-accent hover:underline"
                >
                  查看当前章
                </button>
              )}
            </div>

            {isWaiting && activeState?.gate && (
              <div className="mt-3 border-t border-warn/25 pt-2.5">
                {activeState.gate.detail && (
                  <div className="mb-2 text-xs leading-relaxed text-ink-secondary">
                    {activeState.gate.detail}
                  </div>
                )}
                <div className="flex flex-wrap gap-1.5">
                  {activeState.gate.options.map((opt) => (
                    <button
                      key={opt.action}
                      type="button"
                      disabled={resolving}
                      title={opt.description}
                      onClick={() => void handleResolve(opt.action)}
                      className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors disabled:opacity-50 ${
                        opt.action === "retry_chapter" || opt.action === "continue"
                          ? "bg-accent text-canvas hover:bg-accent-hover"
                          : "border border-border-strong bg-elevated text-ink-primary hover:bg-canvas"
                      }`}
                    >
                      {opt.label}
                    </button>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}

        {logs.length > 0 && (
          <div className="flex min-h-0 flex-col">
            <div className="mb-1.5 text-xs font-medium text-ink-secondary">运行记录</div>
            <div className="max-h-56 space-y-1.5 overflow-y-auto rounded-lg border border-border-subtle bg-canvas p-3">
              {logs.map((entry, idx) => (
                <div key={`${entry.time}-${idx}`} className="flex items-start gap-2 text-xs">
                  <span
                    className={`mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full ${
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

      <div className="shrink-0 border-t border-border-subtle p-3">
        {isRunning ? (
          <button
            type="button"
            onClick={() => void handleStop()}
            className="flex w-full items-center justify-center gap-1.5 rounded-lg border border-border-strong bg-canvas px-4 py-2 text-xs font-medium text-ink-primary transition-colors hover:bg-elevated"
          >
            <Square className="h-3.5 w-3.5" />
            <span>停止托管</span>
          </button>
        ) : (
          <button
            type="button"
            disabled={starting}
            onClick={() => void handleStart()}
            className="flex w-full items-center justify-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-xs font-medium text-canvas transition-colors hover:bg-accent-hover disabled:opacity-50"
          >
            {starting ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Play className="h-3.5 w-3.5 fill-current" />
            )}
            <span>{starting ? "启动中…" : "开始托管"}</span>
          </button>
        )}
      </div>
    </div>
  );
}
