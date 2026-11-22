import { useEffect, useRef, useState, type ReactNode } from "react";
import {
  BookOpen,
  CornerDownLeft,
  Loader2,
  Search,
  Sparkles,
  X,
} from "lucide-react";
import { MemoryService, type MemorySearchHit } from "../mygo";
import { useApp } from "../stores/AppContext";
import { formatErrorMessage, formatSeq, isIMEComposing } from "../lib/constants";

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * Safely renders snippet text with highlighted matches (zero dangerouslySetInnerHTML).
 * Handles both FTS5 markers (`<mark>...</mark>` / `【...】`) and plain query terms.
 */
function renderHighlighted(text: string, query: string): ReactNode {
  if (!text) return null;

  // Handle explicit <mark>...</mark> or SQLite FTS5 `【...】` snippet markers
  if (text.includes("<mark>") && text.includes("</mark>")) {
    const segments = text.split(/(<mark>[\s\S]*?<\/mark>)/g);
    return segments.map((seg, i) => {
      if (seg.startsWith("<mark>") && seg.endsWith("</mark>")) {
        return (
          <mark
            key={i}
            className="rounded-sm bg-accent-soft px-0.5 text-accent font-medium"
          >
            {seg.slice(6, -7)}
          </mark>
        );
      }
      return <span key={i}>{seg}</span>;
    });
  }

  if (text.includes("【") && text.includes("】")) {
    const segments = text.split(/(【[^【】]+】)/g);
    return segments.map((seg, i) => {
      if (seg.startsWith("【") && seg.endsWith("】")) {
        return (
          <mark
            key={i}
            className="rounded-sm bg-accent-soft px-0.5 text-accent font-medium"
          >
            {seg.slice(1, -1)}
          </mark>
        );
      }
      return <span key={i}>{seg}</span>;
    });
  }

  const trimmed = query.trim();
  if (!trimmed) return text;
  const tokens = trimmed
    .split(/\s+/)
    .map((t) => t.trim())
    .filter((t) => t.length > 0);
  if (tokens.length === 0) return text;

  const pattern = new RegExp(`(${tokens.map(escapeRegExp).join("|")})`, "gi");
  const parts = text.split(pattern);
  const lowerTokens = new Set(tokens.map((t) => t.toLowerCase()));

  return parts.map((part, idx) =>
    lowerTokens.has(part.toLowerCase()) ? (
      <mark
        key={idx}
        className="rounded-sm bg-accent-soft px-0.5 text-accent font-medium"
      >
        {part}
      </mark>
    ) : (
      <span key={idx}>{part}</span>
    )
  );
}

export function MemorySearchModal() {
  const {
    activeBook,
    activeChapter,
    memorySearchOpen,
    closeMemorySearch,
    jumpToChapter,
    jumpToEntity,
  } = useApp();

  const [query, setQuery] = useState("");
  const [beforeCurrentOnly, setBeforeCurrentOnly] = useState(false);
  const [hits, setHits] = useState<MemorySearchHit[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [activeIndex, setActiveIndex] = useState(0);

  const inputRef = useRef<HTMLInputElement>(null);
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const reqIdRef = useRef(0);

  useEffect(() => {
    if (!memorySearchOpen) return;
    setError(null);
    setActiveIndex(0);
    requestAnimationFrame(() => {
      inputRef.current?.focus();
      inputRef.current?.select();
    });
  }, [memorySearchOpen]);

  // Reset search results when active book changes
  useEffect(() => {
    setQuery("");
    setHits([]);
    setError(null);
    setActiveIndex(0);
  }, [activeBook?.id]);

  // Debounced live search against MemoryService.searchMemory
  useEffect(() => {
    if (!memorySearchOpen || !activeBook) return;
    const trimmed = query.trim();
    if (!trimmed) {
      reqIdRef.current += 1;
      setHits([]);
      setLoading(false);
      setError(null);
      setActiveIndex(0);
      return;
    }

    const reqId = ++reqIdRef.current;
    const timer = window.setTimeout(async () => {
      setLoading(true);
      setError(null);
      try {
        const capBeforeSeq =
          beforeCurrentOnly && activeChapter ? activeChapter.globalSeq - 1 : 0;
        const maxSeqParam = capBeforeSeq > 0 ? capBeforeSeq : 0;
        const rawHits = await MemoryService.searchMemory(
          activeBook.id,
          trimmed,
          maxSeqParam
        );
        if (reqIdRef.current !== reqId) return;

        // Edge case: when activeChapter is ch 1 and "仅搜当前章之前" is checked,
        // backend treats maxChapterSeq=0 as unlimited, so filter chapter hits >= 1 cleanly.
        const filtered =
          beforeCurrentOnly && activeChapter && activeChapter.globalSeq <= 1
            ? rawHits.filter((h) => h.source !== "chapter")
            : rawHits;

        setHits(filtered);
        setActiveIndex(0);
      } catch (err) {
        if (reqIdRef.current !== reqId) return;
        setError(formatErrorMessage(err));
        setHits([]);
      } finally {
        if (reqIdRef.current === reqId) {
          setLoading(false);
        }
      }
    }, 160);

    return () => window.clearTimeout(timer);
  }, [memorySearchOpen, activeBook, activeChapter, query, beforeCurrentOnly]);

  const handleSelectHit = async (hit: MemorySearchHit) => {
    closeMemorySearch();
    if (hit.source === "chapter" && hit.chapterId > 0) {
      await jumpToChapter(hit.chapterId);
    } else if (hit.source === "entity" && hit.entityId > 0) {
      jumpToEntity(hit.entityId);
    }
  };

  useEffect(() => {
    if (!memorySearchOpen) return;
    itemRefs.current[activeIndex]?.scrollIntoView({ block: "nearest" });
  }, [memorySearchOpen, activeIndex]);

  // Keyboard navigation inside modal
  useEffect(() => {
    if (!memorySearchOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        closeMemorySearch();
        return;
      }
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setActiveIndex((prev) => (hits.length === 0 ? 0 : (prev + 1) % hits.length));
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setActiveIndex((prev) =>
          hits.length === 0 ? 0 : (prev - 1 + hits.length) % hits.length
        );
        return;
      }
      if (e.key === "Enter" && !isIMEComposing(e) && hits.length > 0) {
        const chosen = hits[activeIndex] ?? hits[0];
        if (chosen) {
          e.preventDefault();
          void handleSelectHit(chosen);
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [memorySearchOpen, hits, activeIndex, closeMemorySearch]);

  if (!memorySearchOpen || !activeBook) return null;

  return (
    <div
      data-nib-modal="true"
      className="fixed inset-0 z-50 flex items-start justify-center bg-black/55 px-4 pt-[13vh]"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) closeMemorySearch();
      }}
    >
      <div className="flex max-h-[70vh] w-full max-w-[620px] flex-col overflow-hidden rounded-xl border border-border-strong bg-elevated shadow-[var(--shadow-elevated)]">
        {/* Search Input Row */}
        <div className="flex items-center gap-2.5 border-b border-border-subtle px-4 py-3">
          {loading ? (
            <Loader2 className="h-4 w-4 shrink-0 animate-spin text-accent" />
          ) : (
            <Search className="h-4 w-4 shrink-0 text-ink-muted" />
          )}
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="检索前文原文、双字人名、招式、道具或设定…"
            className="min-w-0 flex-1 bg-transparent text-sm text-ink-primary placeholder:text-ink-muted focus:outline-none"
          />
          {query && (
            <button
              type="button"
              onClick={() => setQuery("")}
              aria-label="清空"
              className="rounded p-1 text-ink-muted hover:bg-surface hover:text-ink-primary"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          )}
        </div>

        {/* Spoiler Cap & Hint Bar */}
        <div className="flex items-center justify-between border-b border-border-subtle bg-surface/60 px-4 py-2 text-[11px] text-ink-secondary">
          <label
            className={`flex items-center gap-1.5 select-none ${
              activeChapter ? "cursor-pointer" : "opacity-45"
            }`}
          >
            <input
              type="checkbox"
              disabled={!activeChapter}
              checked={beforeCurrentOnly}
              onChange={(e) => setBeforeCurrentOnly(e.target.checked)}
              className="h-3.5 w-3.5 accent-[var(--accent)]"
            />
            <span>
              仅搜当前章之前
              {activeChapter ? (
                <span className="nib-mono ml-1 text-ink-muted">
                  (&lt; 第 {formatSeq(activeChapter.globalSeq)} 章)
                </span>
              ) : null}
            </span>
          </label>

          <div className="hidden items-center gap-2 text-ink-muted sm:flex">
            <span>↑↓ 选择</span>
            <span>·</span>
            <span>Enter 跳转</span>
            <span>·</span>
            <span>Esc 关闭</span>
          </div>
        </div>

        {/* Results */}
        <div className="flex-1 overflow-y-auto p-2">
          {error ? (
            <div className="px-3 py-6 text-center text-xs text-danger">{error}</div>
          ) : !query.trim() ? (
            <div className="px-4 py-10 text-center text-xs text-ink-muted">
              输入关键词实时检索《{activeBook.title}》的章节正文与设定档案（支持双字人名）
            </div>
          ) : !loading && hits.length === 0 ? (
            <div className="px-4 py-10 text-center text-xs text-ink-muted">
              未找到与「{query.trim()}」匹配的章节或实体
            </div>
          ) : (
            <div className="flex flex-col gap-1">
              {hits.map((hit, idx) => {
                const isSelected = idx === activeIndex;
                const isChapter = hit.source === "chapter";
                return (
                  <button
                    ref={(el) => {
                      itemRefs.current[idx] = el;
                    }}
                    key={`${hit.source}-${hit.chapterId}-${hit.entityId}-${idx}`}
                    type="button"
                    onMouseEnter={() => setActiveIndex(idx)}
                    onClick={() => void handleSelectHit(hit)}
                    className={`flex w-full items-start gap-3 rounded-lg px-3 py-2.5 text-left transition-colors ${
                      isSelected ? "bg-accent-soft" : "hover:bg-surface"
                    }`}
                  >
                    <div
                      className={`mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-md ${
                        isChapter
                          ? "bg-canvas text-accent"
                          : "bg-canvas text-ink-secondary"
                      }`}
                    >
                      {isChapter ? (
                        <BookOpen className="h-3.5 w-3.5" />
                      ) : (
                        <Sparkles className="h-3.5 w-3.5" />
                      )}
                    </div>

                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span
                          className={`shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium ${
                            isChapter
                              ? "nib-mono bg-canvas text-accent"
                              : "bg-canvas text-ink-secondary"
                          }`}
                        >
                          {isChapter ? `第 ${formatSeq(hit.chapterSeq)} 章` : "实体"}
                        </span>
                        <span className="truncate text-[13px] font-medium text-ink-primary">
                          {renderHighlighted(hit.title, query)}
                        </span>
                      </div>
                      {hit.snippet && (
                        <p className="mt-1 line-clamp-2 text-xs leading-relaxed text-ink-secondary">
                          {renderHighlighted(hit.snippet, query)}
                        </p>
                      )}
                    </div>

                    {isSelected && (
                      <CornerDownLeft className="mt-1 h-3.5 w-3.5 shrink-0 text-accent" />
                    )}
                  </button>
                );
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
