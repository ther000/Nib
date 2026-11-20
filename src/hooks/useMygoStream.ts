import { useCallback, useEffect, useRef } from "react";
import { Channel } from "mygo-runtime";

/**
 * 统一管理 MyGo v0.10.0 Channel 流式调用的生命周期与安全取消。
 * 规则：严禁将带 Channel 参数的绑定方法与 .with({ signal }) 混用（会导致 pageCtx 失配提前关闭通道）；
 * 统一通过 ch.close() 触发 Go 侧取消上下文。
 */
export function useMygoChannel<T>() {
  const channelRef = useRef<Channel<T> | null>(null);
  const runIdRef = useRef(0);
  const stoppedRef = useRef(false);

  const stop = useCallback(() => {
    stoppedRef.current = true;
    const ch = channelRef.current;
    if (ch) {
      try {
        if (!ch.closed) ch.close();
      } catch {
        // ignore close error
      }
      channelRef.current = null;
    }
  }, []);

  const open = useCallback(
    (onChunk: (chunk: T, runId: number) => void): { channel: Channel<T>; runId: number } => {
      stop();
      const runId = ++runIdRef.current;
      stoppedRef.current = false;
      const ch = new Channel<T>((chunk) => {
        if (runIdRef.current !== runId) return;
        onChunk(chunk, runId);
      });
      channelRef.current = ch;
      return { channel: ch, runId };
    },
    [stop]
  );

  const release = useCallback((ch: Channel<T>) => {
    try {
      if (!ch.closed) ch.close();
    } catch {
      // ignore
    }
    if (channelRef.current === ch) {
      channelRef.current = null;
    }
  }, []);

  useEffect(() => {
    return () => {
      runIdRef.current += 1;
      stop();
    };
  }, [stop]);

  return {
    open,
    stop,
    release,
    isCurrent: (runId: number) => runIdRef.current === runId,
    wasStopped: () => stoppedRef.current,
  };
}
