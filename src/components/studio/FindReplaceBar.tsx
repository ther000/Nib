import { useEffect, useRef } from "react";
import { ChevronDown, ChevronUp, X } from "lucide-react";
import { isIMEComposing } from "../../lib/constants";

interface FindReplaceBarProps {
  findScope: "chapter" | "book";
  findQuery: string;
  replaceText: string;
  chapterFindMatches: number;
  currentMatchIndex: number;
  bookReplaceStats: { affectedChapters: number; totalMatches: number } | null;
  replacingBook: boolean;
  onChangeScope: (scope: "chapter" | "book") => void;
  onChangeQuery: (value: string) => void;
  onChangeReplace: (value: string) => void;
  onJumpMatch: (delta: -1 | 1) => void;
  onReplaceInChapter: () => void;
  onPreviewBookReplace: () => void;
  onExecuteBookReplace: () => void;
  onClose: () => void;
}

export function FindReplaceBar({
  findScope,
  findQuery,
  replaceText,
  chapterFindMatches,
  currentMatchIndex,
  bookReplaceStats,
  replacingBook,
  onChangeScope,
  onChangeQuery,
  onChangeReplace,
  onJumpMatch,
  onReplaceInChapter,
  onPreviewBookReplace,
  onExecuteBookReplace,
  onClose,
}: FindReplaceBarProps) {
  const queryInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    requestAnimationFrame(() => {
      queryInputRef.current?.focus();
      queryInputRef.current?.select();
    });
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        if (document.querySelector('[data-nib-modal="true"]')) return;
        e.preventDefault();
        onClose();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div className="pointer-events-auto nib-floating-dock w-full max-w-[540px] rounded-2xl p-3 text-xs">
      <div className="mb-2 flex items-center justify-between gap-2">
        <div className="flex items-center gap-1 rounded-lg border border-border-subtle bg-canvas p-0.5">
          <button
            type="button"
            onClick={() => onChangeScope("chapter")}
            className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
              findScope === "chapter"
                ? "bg-accent-soft text-accent"
                : "text-ink-secondary hover:text-ink-primary"
            }`}
          >
            当前章节
          </button>
          <button
            type="button"
            onClick={() => onChangeScope("book")}
            className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
              findScope === "book"
                ? "bg-accent-soft text-accent"
                : "text-ink-secondary hover:text-ink-primary"
            }`}
          >
            全书替换
          </button>
        </div>
        <button
          type="button"
          onClick={onClose}
          title="关闭查找栏 (Esc)"
          aria-label="关闭查找栏"
          className="rounded p-1 text-ink-muted transition-colors hover:bg-surface hover:text-ink-primary"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-[170px] flex-1">
          <input
            ref={queryInputRef}
            type="text"
            value={findQuery}
            onChange={(e) => onChangeQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !isIMEComposing(e) && findScope === "chapter" && chapterFindMatches > 0) {
                e.preventDefault();
                onJumpMatch(e.shiftKey ? -1 : 1);
              }
            }}
            placeholder="查找内容（回车跳转下一处）"
            className="w-full rounded-lg border border-border-subtle bg-canvas py-1.5 pr-16 pl-2.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent"
          />
          {findScope === "chapter" && findQuery && (
            <span className="nib-mono pointer-events-none absolute top-1/2 right-2.5 -translate-y-1/2 text-xs text-ink-muted">
              {chapterFindMatches > 0
                ? `${Math.min(currentMatchIndex + 1, chapterFindMatches)}/${chapterFindMatches}`
                : "0/0"}
            </span>
          )}
        </div>

        {findScope === "chapter" && (
          <div className="flex items-center gap-0.5">
            <button
              type="button"
              disabled={!findQuery || chapterFindMatches === 0}
              onClick={() => onJumpMatch(-1)}
              title="上一处 (Shift+Enter)"
              aria-label="上一处匹配"
              className="rounded-md border border-border-subtle bg-surface p-1.5 text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary disabled:opacity-35"
            >
              <ChevronUp className="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              disabled={!findQuery || chapterFindMatches === 0}
              onClick={() => onJumpMatch(1)}
              title="下一处 (Enter)"
              aria-label="下一处匹配"
              className="rounded-md border border-border-subtle bg-surface p-1.5 text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary disabled:opacity-35"
            >
              <ChevronDown className="h-3.5 w-3.5" />
            </button>
          </div>
        )}
      </div>

      <div className="mt-2 flex flex-wrap items-center gap-2">
        <input
          type="text"
          value={replaceText}
          onChange={(e) => onChangeReplace(e.target.value)}
          placeholder="替换为…"
          className="min-w-[170px] flex-1 rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent"
        />

        {findScope === "chapter" ? (
          <button
            type="button"
            disabled={!findQuery || chapterFindMatches === 0}
            onClick={onReplaceInChapter}
            className="rounded-lg bg-accent px-3 py-1.5 font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
          >
            全部替换
          </button>
        ) : (
          <div className="flex items-center gap-2">
            <button
              type="button"
              disabled={!findQuery || replacingBook}
              onClick={onPreviewBookReplace}
              className="rounded-lg border border-border-subtle bg-surface px-3 py-1.5 text-ink-primary transition-colors hover:border-border-strong disabled:opacity-40"
            >
              {replacingBook ? "统计中…" : "预检范围"}
            </button>
            {bookReplaceStats && bookReplaceStats.totalMatches > 0 && (
              <button
                type="button"
                disabled={replacingBook}
                onClick={onExecuteBookReplace}
                className="rounded-lg bg-accent px-3 py-1.5 font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
              >
                替换 {bookReplaceStats.affectedChapters} 章 ({bookReplaceStats.totalMatches} 处)
              </button>
            )}
          </div>
        )}
      </div>

      {findScope === "book" && bookReplaceStats && bookReplaceStats.totalMatches === 0 && (
        <div className="mt-2 text-xs text-ink-muted">
          全书正文、标题与章纲中均未找到「{findQuery}」
        </div>
      )}
    </div>
  );
}
