import { useCallback, useEffect, useRef, useState, type RefObject } from "react";
import {
  StudioService,
  type Chapter,
  type GenChunk,
  type ProviderPublic,
  type TokenUsage,
} from "../mygo";
import { useMygoChannel } from "./useMygoStream";
import { formatErrorMessage, formatNumber, parseKeywordList } from "../lib/constants";
import { hasAnyEnabledModel } from "../lib/modelReady";
import type { SettingsTab } from "../stores/AppContext";
import type { NotifyInput } from "../stores/ToastContext";

interface DraftRouteInfo {
  configured: boolean;
  label: string;
}

interface UseChapterGenerationOptions {
  activeChapter: Chapter | null;
  activeChapterId: number | null;
  providers: ProviderPublic[];
  draftRouteInfo: DraftRouteInfo;
  content: string;
  dirty: boolean;
  extraInstruction: string;
  searchKeywordsText: string;
  includePending: boolean;
  pinnedEntityIds: Set<number>;
  excludedEntityIds: Set<number>;
  canvasScrollRef: RefObject<HTMLDivElement | null>;
  saveDraftSilent: () => Promise<boolean>;
  updateChapterInCache: (chap: Chapter) => void;
  refreshActiveChapter: (chapId?: number | null) => Promise<Chapter | null>;
  setTitle: (title: string) => void;
  setContent: (content: string) => void;
  setDirty: (dirty: boolean) => void;
  openSettings: (tab?: SettingsTab) => void;
  notify: (input: NotifyInput) => void;
  onGenerationSettled: (chapterId: number) => void;
}

export function useChapterGeneration({
  activeChapter,
  activeChapterId,
  providers,
  draftRouteInfo,
  content,
  dirty,
  extraInstruction,
  searchKeywordsText,
  includePending,
  pinnedEntityIds,
  excludedEntityIds,
  canvasScrollRef,
  saveDraftSilent,
  updateChapterInCache,
  refreshActiveChapter,
  setTitle,
  setContent,
  setDirty,
  openSettings,
  notify,
  onGenerationSettled,
}: UseChapterGenerationOptions) {
  const [generating, setGenerating] = useState(false);
  const [genStageText, setGenStageText] = useState("");
  const [genModelName, setGenModelName] = useState("");
  const [genError, setGenError] = useState<string | null>(null);
  const [lastUsage, setLastUsage] = useState<TokenUsage | null>(null);

  const genChannel = useMygoChannel<GenChunk>();
  const mountedRef = useRef(true);
  const activeChapterIdRef = useRef<number | null>(activeChapterId);
  const streamTextRef = useRef("");
  const receivedDeltaRef = useRef(false);
  activeChapterIdRef.current = activeChapterId;

  const cancelGeneration = useCallback(() => {
    genChannel.stop();
    setGenerating(false);
    setGenStageText("");
  }, [genChannel]);

  const stopGeneration = useCallback(() => {
    setGenStageText("正在停止并保存已生成内容…");
    genChannel.stop();
  }, [genChannel]);

  useEffect(() => {
    cancelGeneration();
    setGenError(null);
    if (!activeChapter) {
      setLastUsage(null);
    }
  }, [activeChapter?.id, cancelGeneration]);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      genChannel.stop();
    };
  }, [genChannel]);

  const startGeneration = useCallback(
    async (mode: "rewrite" | "continue" = "rewrite") => {
      if (!activeChapter || generating) return;

      if (!draftRouteInfo.configured) {
        const hasEnabled = hasAnyEnabledModel(providers);
        notify({
          tone: "warn",
          message: "尚未设置写作模型",
          detail: hasEnabled
            ? "请先在「模型分配」中指定正文写作模型"
            : "请先在「模型服务」中连接并启用模型",
        });
        openSettings(hasEnabled ? "routes" : "providers");
        return;
      }

      // 续写模式若编辑器有未保存修改，先保存最新正文让后端读取到准确结尾。
      const effectiveMode: "rewrite" | "continue" =
        mode === "continue" && content.trim().length > 0 ? "continue" : "rewrite";
      if (effectiveMode === "continue" && dirty) {
        await saveDraftSilent();
      }

      const genChapterId = activeChapter.id;
      const continueBase = effectiveMode === "continue" ? content.trimEnd() : "";
      streamTextRef.current = continueBase;
      receivedDeltaRef.current = false;

      setGenerating(true);
      setGenError(null);
      setGenStageText(
        effectiveMode === "continue" ? "正在衔接前文准备续写…" : "正在整理参考资料…"
      );
      setGenModelName(draftRouteInfo.configured ? draftRouteInfo.label : "");
      setLastUsage(null);

      const { channel, runId } = genChannel.open((chunk) => {
        if (
          !mountedRef.current ||
          !genChannel.isCurrent(runId) ||
          activeChapterIdRef.current !== genChapterId
        ) {
          return;
        }
        switch (chunk.kind) {
          case "started": {
            const model = chunk.modelName || draftRouteInfo.label;
            setGenModelName(model);
            setGenStageText(
              `${effectiveMode === "continue" ? "正在续写" : "正在写作"}（${model}）`
            );
            break;
          }
          case "thinking": {
            const model = chunk.modelName || draftRouteInfo.label;
            setGenStageText(`模型思考中（${model}）…`);
            break;
          }
          case "delta": {
            if (chunk.delta) {
              if (!receivedDeltaRef.current) {
                receivedDeltaRef.current = true;
                if (effectiveMode === "continue" && continueBase) {
                  const sep = /[。！？”」』…!?]$/.test(continueBase) ? "\n\n" : "";
                  streamTextRef.current =
                    continueBase + sep + chunk.delta.replace(/^[\r\n]+/, "");
                } else {
                  streamTextRef.current = chunk.delta;
                }
              } else {
                streamTextRef.current += chunk.delta;
              }
              setContent(streamTextRef.current);
              setDirty(false);
              const scroller = canvasScrollRef.current;
              if (scroller) {
                const dist =
                  scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight;
                if (dist < 180) {
                  requestAnimationFrame(() => {
                    scroller.scrollTop = scroller.scrollHeight;
                  });
                }
              }
            }
            break;
          }
          case "usage": {
            if (chunk.usage) setLastUsage(chunk.usage);
            break;
          }
          case "completed": {
            if (chunk.usage) setLastUsage(chunk.usage);
            if (chunk.modelName) setGenModelName(chunk.modelName);
            if (chunk.error) {
              notify({ tone: "warn", message: chunk.error });
            }
            setGenStageText("");
            break;
          }
          case "partial": {
            setGenStageText("");
            break;
          }
          case "error": {
            const msg = chunk.error
              ? formatErrorMessage(chunk.error)
              : "生成过程发生错误";
            if (chunk.retryable && chunk.retryAttempt) {
              setGenStageText(`${msg}（重试第 ${chunk.retryAttempt} 次…）`);
            } else {
              setGenError(msg);
            }
            break;
          }
          default:
            break;
        }
      });

      const trimmedExtra = extraInstruction.trim();
      const keywords = parseKeywordList(searchKeywordsText);
      const opts = {
        mode: effectiveMode,
        briefing: {
          pinnedEntityIds: Array.from(pinnedEntityIds),
          excludedEntityIds: Array.from(excludedEntityIds),
          extraInstruction: trimmedExtra || undefined,
          includePending,
          searchKeywords: keywords.length > 0 ? keywords : undefined,
        },
        extraInstruction: trimmedExtra || undefined,
      };

      try {
        const updated = await StudioService.generateChapter(genChapterId, opts, channel);

        if (!mountedRef.current || !genChannel.isCurrent(runId)) {
          return;
        }

        updateChapterInCache(updated);
        if (activeChapterIdRef.current === genChapterId) {
          setTitle(updated.title);
          setContent(updated.content);
          setDirty(false);
        }

        if (updated.status === "partial") {
          notify({
            tone: "warn",
            message: `已停在 ${formatNumber(updated.wordCount)} 字`,
            detail: "已保留已生成文本并归档快照，可继续编辑或重新生成",
          });
        } else {
          notify({
            tone: "ok",
            message: `本章生成完成（${formatNumber(updated.wordCount)} 字）`,
          });
        }
      } catch (err) {
        if (!mountedRef.current || !genChannel.isCurrent(runId)) {
          return;
        }

        const isSameChapter = activeChapterIdRef.current === genChapterId;
        if (genChannel.wasStopped()) {
          if (isSameChapter) {
            const fresh = await refreshActiveChapter(genChapterId);
            if (
              fresh &&
              mountedRef.current &&
              genChannel.isCurrent(runId) &&
              activeChapterIdRef.current === genChapterId
            ) {
              setTitle(fresh.title);
              setContent(fresh.content);
              setDirty(false);
            }
          }
        } else {
          if (receivedDeltaRef.current && isSameChapter) {
            const fresh = await refreshActiveChapter(genChapterId);
            if (
              fresh &&
              mountedRef.current &&
              genChannel.isCurrent(runId) &&
              activeChapterIdRef.current === genChapterId
            ) {
              setTitle(fresh.title);
              setContent(fresh.content);
              setDirty(false);
            }
          }
          const msg = formatErrorMessage(err);
          if (isSameChapter) {
            setGenError(msg);
          }
          notify({
            tone: "danger",
            message: "生成失败",
            detail: msg,
            actionLabel: msg.includes("设置") ? "打开设置" : undefined,
            onAction: msg.includes("设置")
              ? () => openSettings(msg.includes("路由") ? "routes" : "providers")
              : undefined,
          });
        }
      } finally {
        genChannel.release(channel);
        if (mountedRef.current && genChannel.isCurrent(runId)) {
          setGenerating(false);
          setGenStageText("");
          if (activeChapterIdRef.current === genChapterId) {
            onGenerationSettled(genChapterId);
          }
        }
      }
    },
    [
      activeChapter,
      generating,
      draftRouteInfo,
      providers,
      content,
      dirty,
      saveDraftSilent,
      genChannel,
      extraInstruction,
      searchKeywordsText,
      pinnedEntityIds,
      excludedEntityIds,
      includePending,
      canvasScrollRef,
      updateChapterInCache,
      refreshActiveChapter,
      setTitle,
      setContent,
      setDirty,
      openSettings,
      notify,
      onGenerationSettled,
    ]
  );

  return {
    generating,
    genStageText,
    genModelName,
    genError,
    setGenError,
    lastUsage,
    startGeneration,
    stopGeneration,
  };
}
