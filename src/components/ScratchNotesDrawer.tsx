import { useCallback, useEffect, useMemo, useState } from "react";
import { Lightbulb, Pin, Plus, Save, Search, Trash2, X } from "lucide-react";
import { LibraryService, type ScratchNote } from "../mygo";
import { useApp } from "../stores/AppContext";
import { formatErrorMessage } from "../lib/constants";
import { ConfirmDialog, type ConfirmDialogState } from "./ui/ConfirmDialog";

const EMPTY_DRAFT = { id: 0, title: "", content: "", tags: "", pinned: false };

export function ScratchNotesDrawer() {
  const { activeBook, activeTab, activeChapter, setActiveTab, notesOpen, closeNotes, notify } = useApp();
  const [notes, setNotes] = useState<ScratchNote[]>([]);
  const [loading, setLoading] = useState(false);
  const [query, setQuery] = useState("");
  const [composerOpen, setComposerOpen] = useState(false);
  const [draft, setDraft] = useState(EMPTY_DRAFT);
  const [saving, setSaving] = useState(false);
  const [confirmDialog, setConfirmDialog] = useState<ConfirmDialogState | null>(null);
  const [deleting, setDeleting] = useState(false);

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
    if (!notesOpen) return;
    void loadNotes();
    setDraft(EMPTY_DRAFT);
    setComposerOpen(false);
    setQuery("");
  }, [notesOpen, loadNotes]);

  const filtered = useMemo(() => {
    const keyword = query.trim().toLowerCase();
    if (!keyword) return notes;
    return notes.filter((note) => {
      const haystack = `${note.title}\n${note.content}\n${note.tags.join(" ")}`.toLowerCase();
      return haystack.includes(keyword);
    });
  }, [notes, query]);

  const startCreate = () => {
    setDraft(EMPTY_DRAFT);
    setComposerOpen(true);
  };

  const startEdit = (note: ScratchNote) => {
    setDraft({
      id: note.id,
      title: note.title,
      content: note.content,
      tags: note.tags.join(", "),
      pinned: note.pinned,
    });
    setComposerOpen(true);
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
      setComposerOpen(false);
      notify({ tone: "ok", message: draft.id ? "便签已更新" : "便签已保存" });
    } catch (err) {
      notify({ tone: "danger", message: "保存便签失败", detail: formatErrorMessage(err) });
    } finally {
      setSaving(false);
    }
  };

  const requestDelete = (note: ScratchNote) => {
    setConfirmDialog({
      title: "删除灵感便签",
      message: `确定永久删除「${note.title || "无标题便签"}」吗？此操作无法撤销。`,
      confirmLabel: "删除",
      danger: true,
      onConfirm: async () => {
        setDeleting(true);
        try {
          await LibraryService.deleteScratchNote(note.id);
          if (draft.id === note.id) {
            setDraft(EMPTY_DRAFT);
            setComposerOpen(false);
          }
          setConfirmDialog(null);
          await loadNotes();
          notify({ tone: "ok", message: "已删除便签" });
        } catch (err) {
          notify({ tone: "danger", message: "删除便签失败", detail: formatErrorMessage(err) });
        } finally {
          setDeleting(false);
        }
      },
    });
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

  const handleInsert = async (note: ScratchNote) => {
    if (!note.content.trim()) {
      notify({ tone: "warn", message: "便签正文为空" });
      return;
    }
    if (!activeChapter) {
      try {
        await navigator.clipboard.writeText(note.content);
        notify({ tone: "ok", message: "当前未打开章节，已复制便签内容到剪贴板" });
      } catch {
        notify({ tone: "warn", message: "请先在写作页打开一个章节" });
      }
      return;
    }
    if (activeTab !== "studio") {
      setActiveTab("studio");
      window.setTimeout(() => {
        window.dispatchEvent(new CustomEvent("nib:insert-text", { detail: { text: note.content } }));
      }, 120);
      notify({ tone: "ok", message: "已切回写作页并插入便签内容" });
      return;
    }
    window.dispatchEvent(new CustomEvent("nib:insert-text", { detail: { text: note.content } }));
    notify({ tone: "ok", message: "已插入到正文光标处" });
  };

  if (!notesOpen || !activeBook) return null;

  return (
    <aside className="nib-glass-panel m-2 mt-0 ml-0 flex w-[348px] shrink-0 flex-col overflow-hidden">
      <div className="flex h-11 shrink-0 items-center justify-between border-b border-border-subtle px-4">
        <div className="flex items-center gap-2">
          <Lightbulb className="h-4 w-4 text-accent" />
          <span className="text-[13px] font-medium text-ink-primary">灵感便签</span>
          <span className="nib-mono text-xs text-ink-muted">{notes.length}</span>
        </div>
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={startCreate}
            className="flex items-center gap-1 rounded-md bg-accent-soft px-2 py-1 text-xs font-medium text-accent transition-colors hover:bg-accent/20"
          >
            <Plus className="h-3.5 w-3.5" />
            <span>新建</span>
          </button>
          <button
            type="button"
            title="收起灵感便签"
            aria-label="收起灵感便签"
            onClick={closeNotes}
            className="rounded-md p-1 text-ink-muted transition-colors hover:bg-elevated hover:text-ink-primary"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      </div>

      {notes.length > 0 && (
        <div className="border-b border-border-subtle px-4 py-2.5">
          <div className="relative">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2 text-ink-muted" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="搜索标题、内容或标签"
              className="w-full rounded-lg border border-border-subtle bg-canvas py-1.5 pr-3 pl-8 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent"
            />
          </div>
        </div>
      )}

      {composerOpen && (
        <div className="border-b border-border-subtle bg-canvas/50 px-4 py-3">
          <div className="mb-2 flex items-center justify-between">
            <span className="text-xs font-medium text-ink-primary">
              {draft.id ? "编辑便签" : "记一条灵感"}
            </span>
            <button
              type="button"
              onClick={() => {
                setComposerOpen(false);
                setDraft(EMPTY_DRAFT);
              }}
              className="text-xs text-ink-muted hover:text-ink-primary"
            >
              取消
            </button>
          </div>
          <input
            autoFocus
            value={draft.title}
            onChange={(e) => setDraft((prev) => ({ ...prev, title: e.target.value }))}
            placeholder="标题（可选）"
            className="mb-2 w-full rounded-lg border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent"
          />
          <textarea
            rows={4}
            value={draft.content}
            onChange={(e) => setDraft((prev) => ({ ...prev, content: e.target.value }))}
            placeholder="记下桥段、金句或临时废稿…"
            className="mb-2 w-full resize-y rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent"
          />
          <div className="mb-2.5 flex items-center gap-2">
            <input
              value={draft.tags}
              onChange={(e) => setDraft((prev) => ({ ...prev, tags: e.target.value }))}
              placeholder="标签，用逗号分隔"
              className="min-w-0 flex-1 rounded-lg border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent"
            />
            <label className="flex shrink-0 cursor-pointer items-center gap-1.5 text-xs text-ink-secondary">
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
            className="flex w-full items-center justify-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
          >
            <Save className="h-3.5 w-3.5" />
            <span>{saving ? "保存中…" : "保存便签"}</span>
          </button>
        </div>
      )}

      <div className="flex-1 overflow-y-auto px-4 py-3">
        {loading ? (
          <div className="py-6 text-center text-xs text-ink-muted">正在加载…</div>
        ) : filtered.length === 0 ? (
          <div className="flex flex-col items-center gap-2.5 rounded-xl border border-dashed border-border-subtle px-4 py-8 text-center">
            <p className="text-xs leading-relaxed text-ink-secondary">
              {notes.length === 0 ? "把突然想到的桥段、台词和废稿随手存在这里。" : "没有匹配的便签。"}
            </p>
            {notes.length === 0 && !composerOpen && (
              <button
                type="button"
                onClick={startCreate}
                className="rounded-lg bg-accent-soft px-3 py-1.5 text-xs font-medium text-accent transition-colors hover:bg-accent/20"
              >
                写第一条便签
              </button>
            )}
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            {filtered.map((note) => (
              <div
                key={note.id}
                className={`rounded-lg border p-3 transition-colors ${
                  composerOpen && draft.id === note.id
                    ? "border-accent/50 bg-accent-soft/30"
                    : "border-border-subtle bg-canvas/70 hover:border-border-strong"
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
                      onClick={() => requestDelete(note)}
                      className="rounded p-1 text-ink-muted transition-colors hover:text-danger"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                </div>
                <div className="mt-2 flex flex-wrap items-center gap-1.5">
                  {note.tags.map((tag) => (
                    <span key={tag} className="rounded bg-surface px-1.5 py-0.5 text-xs text-ink-secondary">
                      {tag}
                    </span>
                  ))}
                  <button
                    type="button"
                    onClick={() => void handleInsert(note)}
                    className="ml-auto text-xs font-medium text-accent transition-opacity hover:opacity-80"
                  >
                    插入正文
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <ConfirmDialog
        dialog={confirmDialog}
        busy={deleting}
        onClose={() => setConfirmDialog(null)}
      />
    </aside>
  );
}
