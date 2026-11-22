import { useCallback, useEffect, useMemo, useState } from "react";
import { Lightbulb, Pin, Plus, Save, Search, Trash2, X } from "lucide-react";
import { LibraryService, type ScratchNote } from "../mygo";
import { useApp } from "../stores/AppContext";
import { formatErrorMessage } from "../lib/constants";

const EMPTY_DRAFT = { id: 0, title: "", content: "", tags: "", pinned: false };

export function ScratchNotesDrawer() {
  const { activeBook, notify } = useApp();
  const [open, setOpen] = useState(false);
  const [notes, setNotes] = useState<ScratchNote[]>([]);
  const [loading, setLoading] = useState(false);
  const [query, setQuery] = useState("");
  const [draft, setDraft] = useState(EMPTY_DRAFT);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    const toggle = () => setOpen((value) => !value);
    window.addEventListener("nib:toggle-notes", toggle);
    return () => window.removeEventListener("nib:toggle-notes", toggle);
  }, []);

  const loadNotes = useCallback(async () => {
    if (!activeBook) {
      setNotes([]);
      return;
    }
    setLoading(true);
    try {
      const list = await LibraryService.listScratchNotes(activeBook.id);
      setNotes(list);
    } catch (err) {
      notify({ tone: "danger", message: "加载灵感便签失败", detail: formatErrorMessage(err) });
    } finally {
      setLoading(false);
    }
  }, [activeBook, notify]);

  useEffect(() => {
    if (!open) return;
    void loadNotes();
    setDraft(EMPTY_DRAFT);
    setQuery("");
  }, [open, loadNotes]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        setOpen(false);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open]);

  const filtered = useMemo(() => {
    const keyword = query.trim().toLowerCase();
    if (!keyword) return notes;
    return notes.filter((note) => {
      const haystack = `${note.title}\n${note.content}\n${note.tags.join(" ")}`.toLowerCase();
      return haystack.includes(keyword);
    });
  }, [notes, query]);

  const startEdit = (note: ScratchNote) => {
    setDraft({
      id: note.id,
      title: note.title,
      content: note.content,
      tags: note.tags.join(", "),
      pinned: note.pinned,
    });
  };

  const handleSave = async () => {
    if (!activeBook) return;
    if (!draft.title.trim() && !draft.content.trim()) {
      notify({ tone: "warn", message: "标题和内容不能同时为空" });
      return;
    }
    setSaving(true);
    try {
      const tags = draft.tags
        .split(/[,，]/)
        .map((tag) => tag.trim())
        .filter(Boolean);
      await LibraryService.upsertScratchNote({
        id: draft.id,
        bookId: activeBook.id,
        title: draft.title.trim(),
        content: draft.content,
        tags,
        pinned: draft.pinned,
      });
      await loadNotes();
      setDraft(EMPTY_DRAFT);
      notify({ tone: "ok", message: draft.id ? "便签已更新" : "便签已保存" });
    } catch (err) {
      notify({ tone: "danger", message: "保存便签失败", detail: formatErrorMessage(err) });
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (note: ScratchNote) => {
    try {
      await LibraryService.deleteScratchNote(note.id);
      if (draft.id === note.id) setDraft(EMPTY_DRAFT);
      await loadNotes();
    } catch (err) {
      notify({ tone: "danger", message: "删除便签失败", detail: formatErrorMessage(err) });
    }
  };

  const handleTogglePin = async (note: ScratchNote) => {
    try {
      await LibraryService.upsertScratchNote({
        id: note.id,
        bookId: note.bookId,
        title: note.title,
        content: note.content,
        tags: note.tags,
        pinned: !note.pinned,
      });
      await loadNotes();
    } catch (err) {
      notify({ tone: "danger", message: "更新便签失败", detail: formatErrorMessage(err) });
    }
  };

  const handleInsert = (note: ScratchNote) => {
    window.dispatchEvent(new CustomEvent("nib:insert-text", { detail: { text: note.content } }));
    notify({ tone: "ok", message: "已插入到正文光标处" });
  };

  if (!open || !activeBook) return null;

  return (
    <aside className="fixed top-[var(--mygo-titlebar-height,var(--topbar-height))] right-0 bottom-0 z-40 flex w-[360px] flex-col border-l border-border-subtle bg-elevated shadow-[var(--shadow-elevated)]">
      <div className="flex items-center justify-between border-b border-border-subtle px-4 py-3">
        <div className="flex items-center gap-2">
          <Lightbulb className="h-4 w-4 text-accent" />
          <span className="text-sm font-medium text-ink-primary">灵感便签</span>
          <span className="nib-mono text-xs text-ink-muted">{notes.length}</span>
        </div>
        <div className="flex items-center gap-1">
          <button
            type="button"
            title="新建便签"
            aria-label="新建便签"
            onClick={() => setDraft(EMPTY_DRAFT)}
            className="rounded-md p-1 text-ink-secondary transition-colors hover:bg-surface hover:text-ink-primary"
          >
            <Plus className="h-4 w-4" />
          </button>
          <button
            type="button"
            title="关闭"
            aria-label="关闭灵感便签"
            onClick={() => setOpen(false)}
            className="rounded-md p-1 text-ink-muted transition-colors hover:bg-surface hover:text-ink-primary"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      </div>

      <div className="border-b border-border-subtle px-4 py-2.5">
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2 text-ink-muted" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="搜索标题、内容或标签"
            className="w-full rounded-md border border-border-subtle bg-canvas py-1.5 pr-3 pl-8 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
          />
        </div>
      </div>

      <div className="flex-1 overflow-y-auto px-4 py-3">
        {loading ? (
          <div className="py-6 text-center text-xs text-ink-muted">正在加载…</div>
        ) : filtered.length === 0 ? (
          <div className="rounded-lg border border-dashed border-border-subtle px-4 py-6 text-center text-xs text-ink-secondary">
            {notes.length === 0 ? "把突然想到的桥段、台词和废稿存这里。" : "没有匹配的便签。"}
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            {filtered.map((note) => (
              <div
                key={note.id}
                className={`rounded-lg border p-3 transition-colors ${
                  draft.id === note.id
                    ? "border-accent/50 bg-accent-soft/30"
                    : "border-border-subtle bg-surface/60 hover:border-border-strong"
                }`}
              >
                <div className="flex items-start justify-between gap-2">
                  <button
                    type="button"
                    onClick={() => startEdit(note)}
                    className="min-w-0 flex-1 text-left"
                  >
                    <div className="truncate text-[13px] font-medium text-ink-primary">
                      {note.title || "无标题便签"}
                    </div>
                    <p className="mt-1 line-clamp-3 whitespace-pre-wrap text-xs leading-relaxed text-ink-secondary">
                      {note.content}
                    </p>
                  </button>
                  <div className="flex shrink-0 items-center gap-0.5">
                    <button
                      type="button"
                      title={note.pinned ? "取消置顶" : "置顶"}
                      aria-label={note.pinned ? "取消置顶" : "置顶"}
                      onClick={() => void handleTogglePin(note)}
                      className={`rounded p-1 transition-colors ${
                        note.pinned ? "text-accent" : "text-ink-muted hover:text-ink-primary"
                      }`}
                    >
                      <Pin className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      title="删除便签"
                      aria-label="删除便签"
                      onClick={() => void handleDelete(note)}
                      className="rounded p-1 text-ink-muted transition-colors hover:text-danger"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                </div>
                <div className="mt-2 flex flex-wrap items-center gap-1.5">
                  {note.tags.map((tag) => (
                    <span key={tag} className="rounded bg-canvas px-1.5 py-0.5 text-[10px] text-ink-secondary">
                      {tag}
                    </span>
                  ))}
                  <button
                    type="button"
                    onClick={() => handleInsert(note)}
                    className="ml-auto text-[11px] text-accent transition-opacity hover:opacity-80"
                  >
                    插入正文
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="border-t border-border-subtle px-4 py-3">
        <div className="mb-2 flex items-center justify-between">
          <span className="text-xs font-medium text-ink-secondary">
            {draft.id ? "编辑便签" : "新建便签"}
          </span>
          {draft.id > 0 && (
            <button
              type="button"
              onClick={() => setDraft(EMPTY_DRAFT)}
              className="text-[11px] text-ink-muted hover:text-ink-primary"
            >
              取消编辑
            </button>
          )}
        </div>
        <input
          value={draft.title}
          onChange={(e) => setDraft((prev) => ({ ...prev, title: e.target.value }))}
          placeholder="标题"
          className="mb-2 w-full rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
        />
        <textarea
          rows={4}
          value={draft.content}
          onChange={(e) => setDraft((prev) => ({ ...prev, content: e.target.value }))}
          placeholder="记下这个灵感…"
          className="mb-2 w-full resize-y rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
        />
        <div className="mb-2 flex items-center gap-2">
          <input
            value={draft.tags}
            onChange={(e) => setDraft((prev) => ({ ...prev, tags: e.target.value }))}
            placeholder="标签，用逗号分隔"
            className="min-w-0 flex-1 rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
          />
          <label className="flex shrink-0 items-center gap-1.5 text-xs text-ink-secondary">
            <input
              type="checkbox"
              checked={draft.pinned}
              onChange={(e) => setDraft((prev) => ({ ...prev, pinned: e.target.checked }))}
              className="h-3.5 w-3.5 accent-[var(--accent)]"
            />
            置顶
          </label>
        </div>
        <button
          type="button"
          disabled={saving}
          onClick={() => void handleSave()}
          className="flex w-full items-center justify-center gap-1.5 rounded-md bg-accent px-3 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
        >
          <Save className="h-3.5 w-3.5" />
          <span>{saving ? "保存中…" : "保存便签"}</span>
        </button>
      </div>
    </aside>
  );
}
