import { X } from "lucide-react";

interface FindReplaceBarProps {
  findScope: "chapter" | "book";
  findQuery: string;
  replaceText: string;
  chapterFindMatches: number;
  bookReplaceStats: { affectedChapters: number; totalMatches: number } | null;
  replacingBook: boolean;
  onChangeScope: (scope: "chapter" | "book") => void;
  onChangeQuery: (value: string) => void;
  onChangeReplace: (value: string) => void;
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
  bookReplaceStats,
  replacingBook,
  onChangeScope,
  onChangeQuery,
  onChangeReplace,
  onReplaceInChapter,
  onPreviewBookReplace,
  onExecuteBookReplace,
  onClose,
}: FindReplaceBarProps) {
  return (
    <div className="mb-5 flex flex-col gap-2.5 rounded-xl border border-border-subtle bg-surface p-3 text-xs">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-1 rounded-lg border border-border-subtle bg-canvas p-0.5">
          <button
            type="button"
            onClick={() => onChangeScope("chapter")}
            className={`rounded-md px-2.5 py-1 text-xs font-medium ${
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
            className={`rounded-md px-2.5 py-1 text-xs font-medium ${
              findScope === "book"
                ? "bg-accent-soft text-accent"
                : "text-ink-secondary hover:text-ink-primary"
            }`}
          >
            全书改名/替换
          </button>
        </div>
        <button
          type="button"
          onClick={onClose}
          className="rounded p-1 text-ink-muted hover:text-ink-primary"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <input
          type="text"
          value={findQuery}
          onChange={(e) => onChangeQuery(e.target.value)}
          placeholder="查找内容（如旧人名/招式名）"
          className="min-w-[160px] flex-1 rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
        />
        <input
          type="text"
          value={replaceText}
          onChange={(e) => onChangeReplace(e.target.value)}
          placeholder="替换为…"
          className="min-w-[160px] flex-1 rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
        />

        {findScope === "chapter" ? (
          <div className="flex items-center gap-2">
            {findQuery && (
              <span className="nib-mono text-ink-muted">
                {chapterFindMatches} 处匹配
              </span>
            )}
            <button
              type="button"
              disabled={!findQuery || chapterFindMatches === 0}
              onClick={onReplaceInChapter}
              className="rounded-lg bg-accent px-3 py-1.5 font-medium text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
            >
              全部替换
            </button>
          </div>
        ) : (
          <div className="flex items-center gap-2">
            <button
              type="button"
              disabled={!findQuery || replacingBook}
              onClick={onPreviewBookReplace}
              className="rounded-lg border border-border-subtle bg-elevated px-3 py-1.5 text-ink-primary hover:border-border-strong disabled:opacity-40"
            >
              {replacingBook ? "统计中…" : "预检影响范围"}
            </button>
            {bookReplaceStats && bookReplaceStats.totalMatches > 0 && (
              <button
                type="button"
                disabled={replacingBook}
                onClick={onExecuteBookReplace}
                className="rounded-lg bg-accent px-3 py-1.5 font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
              >
                确认替换（{bookReplaceStats.affectedChapters} 章{" "}
                {bookReplaceStats.totalMatches} 处）
              </button>
            )}
          </div>
        )}
      </div>
      {findScope === "book" &&
        bookReplaceStats &&
        bookReplaceStats.totalMatches === 0 && (
          <div className="text-ink-muted">
            全书正文、标题与章纲中均未找到「{findQuery}」
          </div>
        )}
    </div>
  );
}
