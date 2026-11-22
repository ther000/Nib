import { useEffect, useMemo, useRef, useState } from "react";
import {
  Bot,
  CheckCircle2,
  Loader2,
  RotateCcw,
  Send,
  Sparkles,
  Square,
  Trash2,
  X,
  XCircle,
} from "lucide-react";
import {
  DirectorService,
  StudioService,
  type DirectorEvent,
  type DirectorMessage,
  type DirectorToolCall,
} from "../mygo";
import { useApp } from "../stores/AppContext";
import { useAutopilotState } from "../stores/AutopilotContext";
import { useMygoChannel } from "../hooks/useMygoStream";
import { hasAnyEnabledModel, hasReadyModelRoute } from "../lib/modelReady";
import { formatErrorMessage, isIMEComposing } from "../lib/constants";
import { AutopilotPanel } from "./AutopilotConsole";

const QUICK_PROMPTS = [
  "检查当前书的设定和章纲，帮我补全不足之处",
  "帮我规划接下来 3 章的大纲与节拍",
  "对当前章做一次质检并自动润色问题段落",
  "帮我托管自动续写 3 章并同步设定更新",
];

export function DirectorDrawer() {
  const {
    directorOpen,
    directorTab,
    setDirectorTab,
    closeDirector,
    activeBook,
    activeChapter,
    providers,
    modelRoutes,
    openSettings,
    refreshBookData,
    refreshActiveChapter,
    bumpMemoryRevision,
    notify,
  } = useApp();
  const { autopilotState } = useAutopilotState();

  const [messages, setMessages] = useState<DirectorMessage[]>([]);
  const [input, setInput] = useState("");
  const [running, setRunning] = useState(false);
  const [liveStatus, setLiveStatus] = useState("");
  const [liveToolCalls, setLiveToolCalls] = useState<DirectorToolCall[]>([]);
  const [undoingVersionId, setUndoingVersionId] = useState<number | null>(null);
  const [undoingSnapshotId, setUndoingSnapshotId] = useState<number | null>(null);

  const {
    open: openStream,
    stop: stopStream,
    release: releaseStream,
    isCurrent: isCurrentRun,
    wasStopped: wasStreamStopped,
  } = useMygoChannel<DirectorEvent>();
  const scrollRef = useRef<HTMLDivElement>(null);
  const prevBookIdRef = useRef<number | null>(null);
  const activeBookIdRef = useRef<number | null>(activeBook?.id ?? null);
  activeBookIdRef.current = activeBook?.id ?? null;

  const hasReadyModel = useMemo(
    () => hasReadyModelRoute(providers, modelRoutes, ["outline", "draft"]),
    [providers, modelRoutes]
  );

  // 切换书籍时取消旧流；仅在抽屉打开时从 SQLite 加载该书持久化的 AI 导演对话记录
  useEffect(() => {
    const bookId = activeBook?.id ?? null;
    if (bookId !== prevBookIdRef.current) {
      prevBookIdRef.current = bookId;
      stopStream();
      setMessages([]);
      setLiveStatus("");
      setLiveToolCalls([]);
      setRunning(false);
    }
    if (!directorOpen || !bookId) return;
    let active = true;
    void DirectorService.listMessages(bookId)
      .then((list) => {
        if (active) setMessages(list);
      })
      .catch(() => {
        // ignore load error
      });
    return () => {
      active = false;
    };
  }, [activeBook?.id, directorOpen, stopStream]);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [messages, liveToolCalls, liveStatus]);

  if (!directorOpen || !activeBook) return null;

  const handleStop = () => {
    stopStream();
  };

  const handleClearHistory = async () => {
    if (!activeBook || running) return;
    try {
      await DirectorService.clearMessages(activeBook.id);
      setMessages([]);
      notify({ tone: "ok", message: "已清空本书 AI 导演对话记录" });
    } catch (err) {
      notify({ tone: "danger", message: "清空对话失败", detail: formatErrorMessage(err) });
    }
  };

  const handleUndoProseEdit = async (versionId: number, chapterId?: number) => {
    if (!activeBook || undoingVersionId !== null) return;
    setUndoingVersionId(versionId);
    try {
      await StudioService.restoreVersion(versionId);
      await refreshBookData(activeBook.id, chapterId ?? activeChapter?.id ?? null);
      if (chapterId && chapterId > 0) {
        await refreshActiveChapter(chapterId);
      }
      notify({ tone: "ok", message: "已撤回该次正文修改" });
    } catch (err) {
      notify({ tone: "danger", message: "撤回正文修改失败", detail: formatErrorMessage(err) });
    } finally {
      setUndoingVersionId(null);
    }
  };

  const handleUndoDirectorTurn = async (snapshotId: number) => {
    if (!activeBook || running || undoingSnapshotId !== null || undoingVersionId !== null) return;
    setUndoingSnapshotId(snapshotId);
    try {
      await DirectorService.undoDirectorTurn(snapshotId);
      await refreshBookData(activeBook.id, activeChapter?.id ?? null);
      if (activeChapter?.id && activeChapter.id > 0) {
        await refreshActiveChapter(activeChapter.id);
      }
      bumpMemoryRevision();
      const refreshedMsgs = await DirectorService.listMessages(activeBook.id);
      setMessages(refreshedMsgs);
      notify({ tone: "ok", message: "已撤销本轮 AI 导演的所有书库改动" });
    } catch (err) {
      notify({ tone: "danger", message: "撤销本轮改动失败", detail: formatErrorMessage(err) });
    } finally {
      setUndoingSnapshotId(null);
    }
  };

  const handleSend = async (overrideText?: string) => {
    const text = (overrideText ?? input).trim();
    if (!text || running) return;

    if (!hasReadyModel) {
      const needProvider = !hasAnyEnabledModel(providers);
      notify({
        tone: "warn",
        message: needProvider ? "请先连接模型服务并启用模型" : "请先为大纲或写作分配模型",
        actionLabel: "去设置",
        onAction: () => openSettings(needProvider ? "providers" : "routes"),
      });
      return;
    }

    const userMsg: DirectorMessage = {
      role: "user",
      content: text,
      createdAt: new Date().toISOString(),
    };
    const nextHistory = [...messages, userMsg];
    setMessages(nextHistory);
    if (!overrideText) setInput("");

    setRunning(true);
    setLiveStatus("正在分析你的指令…");
    setLiveToolCalls([]);

    const targetBookId = activeBook.id;
    const { channel: ch, runId } = openStream((ev) => {
      if (!isCurrentRun(runId) || activeBookIdRef.current !== targetBookId) return;
      if (ev.kind === "started" || ev.kind === "thinking") {
        if (ev.message) setLiveStatus(ev.message);
      } else if (ev.kind === "tool_start" && ev.toolCall) {
        setLiveStatus(ev.message || `${ev.toolCall.label}…`);
        setLiveToolCalls((prev) => [...prev, ev.toolCall!]);
      } else if (ev.kind === "tool_result" && ev.toolCall) {
        setLiveToolCalls((prev) =>
          prev.map((tc) => (tc.id === ev.toolCall!.id ? ev.toolCall! : tc)),
        );
      } else if (ev.kind === "error" && ev.error) {
        setLiveStatus(formatErrorMessage(ev.error));
      }
    });

    try {
      const reply = await DirectorService.sendInstruction(
        {
          bookId: targetBookId,
          activeChapterId: activeChapter?.id ?? 0,
          instruction: text,
          history: messages,
        },
        ch,
      );
      if (!isCurrentRun(runId) || activeBookIdRef.current !== targetBookId) return;

      setMessages((prev) => [
        ...prev,
        {
          role: "assistant",
          content: reply.reply,
          toolCalls: reply.toolCalls,
          undoSnapshotId: reply.undoSnapshotId,
          createdAt: new Date().toISOString(),
        },
      ]);

      if (reply.mutated) {
        await refreshBookData(targetBookId, activeChapter?.id ?? null);
        if (activeChapter?.id && activeBookIdRef.current === targetBookId) {
          await refreshActiveChapter(activeChapter.id);
        }
        bumpMemoryRevision();
      }
    } catch (err) {
      if (!isCurrentRun(runId) || activeBookIdRef.current !== targetBookId) return;
      if (!wasStreamStopped()) {
        notify({
          tone: "danger",
          message: "AI 导演执行失败",
          detail: formatErrorMessage(err),
        });
        setMessages((prev) => [
          ...prev,
          {
            role: "assistant",
            content: `执行遇到问题：${formatErrorMessage(err)}`,
            createdAt: new Date().toISOString(),
          },
        ]);
      } else {
        setMessages((prev) => [
          ...prev,
          {
            role: "assistant",
            content: "已停止当前指令。",
            createdAt: new Date().toISOString(),
          },
        ]);
      }
    } finally {
      releaseStream(ch);
      if (isCurrentRun(runId) && activeBookIdRef.current === targetBookId) {
        setRunning(false);
        setLiveStatus("");
        setLiveToolCalls([]);
      }
    }
  };

  const bookAutopilot =
    autopilotState?.bookId === activeBook.id ? autopilotState : null;
  const autopilotRunning = bookAutopilot?.status === "running";
  const autopilotWaiting = bookAutopilot?.status === "waiting_decision";

  return (
    <aside className="flex h-full w-[360px] shrink-0 flex-col border-l border-border-subtle bg-surface">
      {/* Header */}
      <div className="flex h-11 shrink-0 items-center justify-between gap-2 border-b border-border-subtle px-3">
        <div className="flex items-center gap-0.5 rounded-lg border border-border-subtle bg-canvas/80 p-0.5">
          <button
            type="button"
            onClick={() => setDirectorTab("chat")}
            className={`flex items-center gap-1.5 rounded-[6px] px-2.5 py-1 text-xs font-medium transition-colors ${
              directorTab === "chat"
                ? "bg-accent-soft text-accent"
                : "text-ink-secondary hover:text-ink-primary"
            }`}
          >
            <Sparkles className="h-3 w-3" />
            <span>AI 导演</span>
          </button>
          <button
            type="button"
            onClick={() => setDirectorTab("autopilot")}
            className={`flex items-center gap-1.5 rounded-[6px] px-2.5 py-1 text-xs font-medium transition-colors ${
              directorTab === "autopilot"
                ? "bg-accent-soft text-accent"
                : "text-ink-secondary hover:text-ink-primary"
            }`}
          >
            {autopilotRunning ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <Bot className="h-3 w-3" />
            )}
            <span>
              {autopilotRunning
                ? `托管 ${bookAutopilot.completedChapters}/${bookAutopilot.targetChapters}`
                : "自动托管"}
            </span>
            {autopilotWaiting && (
              <span className="h-1.5 w-1.5 rounded-full bg-warn" />
            )}
          </button>
        </div>

        <div className="flex items-center gap-1">
          {directorTab === "chat" && activeChapter && (
            <span className="max-w-[72px] truncate rounded bg-canvas px-1.5 py-0.5 text-[11px] text-ink-muted">
              第 {activeChapter.globalSeq} 章
            </span>
          )}
          {directorTab === "chat" && messages.length > 0 && !running && (
            <button
              type="button"
              onClick={() => void handleClearHistory()}
              title="清空本书对话记录"
              aria-label="清空对话"
              className="rounded-md p-1 text-ink-muted transition-colors hover:bg-elevated hover:text-ink-primary"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </button>
          )}
          <button
            type="button"
            onClick={closeDirector}
            title="收起 AI 导演 (Ctrl+K)"
            aria-label="收起 AI 导演"
            className="rounded-md p-1 text-ink-muted transition-colors hover:bg-elevated hover:text-ink-primary"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      {directorTab === "autopilot" ? (
        <AutopilotPanel />
      ) : (
        <>
          {/* Messages */}
          <div ref={scrollRef} className="flex-1 space-y-3 overflow-y-auto px-4 py-4">
        {messages.length === 0 && !running ? (
          <div className="flex flex-col gap-3">
            <div className="rounded-lg border border-border-subtle bg-canvas p-3.5">
              <div className="text-[13px] font-medium text-ink-primary">
                直接告诉我你想怎么改或怎么写
              </div>
              <div className="mt-1 text-xs leading-relaxed text-ink-secondary">
                我可以帮你调整角色与世界铁律、规划分卷和章纲、重写或润色正文，也可以直接启动托管连续写稿。
              </div>
            </div>

            <div className="flex flex-col gap-1.5">
              <div className="text-xs font-medium text-ink-muted">试试这些指令</div>
              {QUICK_PROMPTS.map((prompt) => (
                <button
                  key={prompt}
                  type="button"
                  onClick={() => void handleSend(prompt)}
                  className="rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-left text-xs leading-relaxed text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary"
                >
                  {prompt}
                </button>
              ))}
            </div>
          </div>
        ) : (
          <>
            {messages.map((msg, idx) => (
              <div
                key={`${msg.role}-${idx}`}
                className={`flex flex-col gap-1.5 ${
                  msg.role === "user" ? "items-end" : "items-start"
                }`}
              >
                {msg.role === "assistant" && msg.toolCalls && msg.toolCalls.length > 0 && (
                  <div className="w-full space-y-1 rounded-lg border border-border-subtle bg-canvas px-3 py-2">
                    {msg.toolCalls.map((tc) => (
                      <div key={tc.id} className="flex items-start gap-2 text-xs">
                        {tc.status === "ok" ? (
                          <CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0 text-ok" />
                        ) : (
                          <XCircle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-danger" />
                        )}
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center justify-between gap-2">
                            <span className="font-medium text-ink-primary">{tc.label}</span>
                            {tc.status === "ok" && tc.undoVersionId && tc.undoVersionId > 0 && (
                              <button
                                type="button"
                                disabled={undoingVersionId !== null}
                                onClick={() => void handleUndoProseEdit(tc.undoVersionId!, tc.undoChapterId)}
                                className="flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-xs text-accent transition-colors hover:bg-accent-soft disabled:opacity-40"
                              >
                                <RotateCcw className="h-3 w-3" />
                                <span>{undoingVersionId === tc.undoVersionId ? "撤回中…" : "撤回正文"}</span>
                              </button>
                            )}
                          </div>
                          {tc.resultSummary && (
                            <div className="mt-0.5 text-ink-muted">{tc.resultSummary}</div>
                          )}
                        </div>
                      </div>
                    ))}
                  </div>
                )}

                <div
                  className={`max-w-[92%] rounded-lg px-3 py-2 text-[13px] leading-relaxed whitespace-pre-wrap ${
                    msg.role === "user"
                      ? "bg-accent text-white"
                      : "border border-border-subtle bg-canvas text-ink-primary"
                  }`}
                >
                  {msg.content}
                </div>

                {msg.role === "assistant" && msg.undoSnapshotId && msg.undoSnapshotId > 0 && (
                  <button
                    type="button"
                    disabled={
                      running || undoingSnapshotId !== null || undoingVersionId !== null
                    }
                    onClick={() => void handleUndoDirectorTurn(msg.undoSnapshotId!)}
                    className="inline-flex items-center gap-1 rounded-md border border-border-subtle bg-canvas px-2 py-0.5 text-[11px] text-ink-secondary transition-colors hover:border-accent/50 hover:text-accent disabled:opacity-45"
                  >
                    <RotateCcw className="h-3 w-3" />
                    <span>
                      {undoingSnapshotId === msg.undoSnapshotId
                        ? "正在撤销…"
                        : "撤销本轮改动"}
                    </span>
                  </button>
                )}
              </div>
            ))}

            {running && (
              <div className="space-y-2 rounded-lg border border-border-subtle bg-canvas px-3 py-2.5">
                {liveToolCalls.map((tc) => (
                  <div key={tc.id} className="flex items-start gap-2 text-xs">
                    {tc.status === "running" ? (
                      <Loader2 className="mt-0.5 h-3.5 w-3.5 shrink-0 animate-spin text-accent" />
                    ) : tc.status === "ok" ? (
                      <CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0 text-ok" />
                    ) : (
                      <XCircle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-danger" />
                    )}
                    <div className="min-w-0 flex-1">
                      <span className="font-medium text-ink-primary">{tc.label}</span>
                      {tc.resultSummary && (
                        <div className="mt-0.5 text-ink-muted">{tc.resultSummary}</div>
                      )}
                    </div>
                  </div>
                ))}
                <div className="flex items-center gap-2 text-xs text-ink-secondary">
                  <Loader2 className="h-3.5 w-3.5 animate-spin text-accent" />
                  <span>{liveStatus || "处理中…"}</span>
                </div>
              </div>
            )}
          </>
        )}
      </div>

      {/* Input */}
      <div className="shrink-0 border-t border-border-subtle p-3">
        <div className="flex flex-col gap-2 rounded-lg border border-border-subtle bg-canvas p-2 focus-within:border-accent">
          <textarea
            rows={2}
            disabled={running}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey && !isIMEComposing(e)) {
                e.preventDefault();
                void handleSend();
              }
            }}
            placeholder="输入指令，例如：把反派修为调高一阶，顺手规划下两章…"
            className="w-full resize-none bg-transparent text-[13px] leading-relaxed text-ink-primary placeholder:text-ink-muted focus:outline-none disabled:opacity-60"
          />
          <div className="flex items-center justify-between">
            <span className="text-xs text-ink-muted">Enter 发送，Shift+Enter 换行</span>
            {running ? (
              <button
                type="button"
                onClick={handleStop}
                className="flex items-center gap-1 rounded-[6px] border border-border-strong bg-surface px-2.5 py-1 text-xs font-medium text-ink-primary transition-colors hover:bg-elevated"
              >
                <Square className="h-3 w-3" />
                <span>停止</span>
              </button>
            ) : (
              <button
                type="button"
                disabled={!input.trim()}
                onClick={() => void handleSend()}
                className="flex items-center gap-1 rounded-[6px] bg-accent px-3 py-1 text-xs font-medium text-canvas transition-colors hover:bg-accent-hover disabled:opacity-40"
              >
                <Send className="h-3 w-3" />
                <span>执行</span>
              </button>
            )}
          </div>
        </div>
      </div>
        </>
      )}
    </aside>
  );
}
