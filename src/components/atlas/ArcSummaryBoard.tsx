import { useCallback, useEffect, useMemo, useState } from "react";
import { BookMarked, Plus, X } from "lucide-react";
import {
  MemoryService,
  type ArcSummary,
  type ArcSummaryLevel,
} from "../../mygo";
import { useApp } from "../../stores/AppContext";
import {
  ARC_SUMMARY_LEVELS,
  formatErrorMessage,
  formatSeq,
  getArcSummaryLevelLabel,
} from "../../lib/constants";

interface ArcSummaryEditorState {
  id: number;
  level: ArcSummaryLevel;
  volumeId: string;
  startSeq: string;
  endSeq: string;
  summary: string;
}

export function ArcSummaryBoard({ latestChapterSeq }: { latestChapterSeq: number }) {
  const { activeBook, volumes, memoryRevision, notify } = useApp();

  const [levelFilter, setLevelFilter] = useState<string>("all");
  const [items, setItems] = useState<ArcSummary[]>([]);
  const [loading, setLoading] = useState(false);

  const [editor, setEditor] = useState<ArcSummaryEditorState | null>(null);
  const [saving, setSaving] = useState(false);

  const volumeMap = useMemo(() => {
    const map = new Map<number, (typeof volumes)[number]>();
    for (const v of volumes) map.set(v.id, v);
    return map;
  }, [volumes]);

  const fetchSummaries = useCallback(async () => {
    if (!activeBook) {
      setItems([]);
      return;
    }
    setLoading(true);
    try {
      const list = await MemoryService.listArcSummaries(
        activeBook.id,
        levelFilter === "all" ? "" : levelFilter
      );
      setItems(list);
    } catch (err) {
      notify({ tone: "danger", message: "加载弧线摘要失败", detail: formatErrorMessage(err) });
    } finally {
      setLoading(false);
    }
  }, [activeBook, levelFilter, notify]);

  useEffect(() => {
    void fetchSummaries();
  }, [fetchSummaries, memoryRevision]);

  useEffect(() => {
    if (!editor) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setEditor(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [editor]);

  const openCreate = () => {
    const defaultLevel: ArcSummaryLevel =
      levelFilter === "volume" ? "volume" : "arc_10";
    const endDefault = latestChapterSeq > 0 ? latestChapterSeq : 10;
    const startDefault = Math.max(1, endDefault - 9);
    setEditor({
      id: 0,
      level: defaultLevel,
      volumeId: volumes[0] ? String(volumes[0].id) : "",
      startSeq: String(startDefault),
      endSeq: String(endDefault),
      summary: "",
    });
  };

  const openEdit = (item: ArcSummary) => {
    setEditor({
      id: item.id,
      level: item.level,
      volumeId: item.volumeId ? String(item.volumeId) : "",
      startSeq: String(item.startSeq),
      endSeq: String(item.endSeq),
      summary: item.summary,
    });
  };

  const handleSave = async () => {
    if (!activeBook || !editor) return;
    const startSeq = Number.parseInt(editor.startSeq, 10);
    const endSeq = Number.parseInt(editor.endSeq, 10);
    if (!Number.isFinite(startSeq) || startSeq <= 0) {
      notify({ tone: "warn", message: "起始章号必须大于 0" });
      return;
    }
    if (!Number.isFinite(endSeq) || endSeq < startSeq) {
      notify({ tone: "warn", message: "结束章号不能小于起始章号" });
      return;
    }
    const trimmed = editor.summary.trim();
    if (!trimmed) {
      notify({ tone: "warn", message: "摘要内容不能为空" });
      return;
    }
    const volId =
      editor.level === "volume" && editor.volumeId
        ? Number.parseInt(editor.volumeId, 10)
        : undefined;

    setSaving(true);
    try {
      await MemoryService.upsertArcSummary({
        id: editor.id,
        bookId: activeBook.id,
        level: editor.level,
        volumeId: volId && volId > 0 ? volId : undefined,
        startSeq,
        endSeq,
        summary: trimmed,
      });
      await fetchSummaries();
      setEditor(null);
      notify({ tone: "ok", message: editor.id > 0 ? "摘要已更新" : "已新建摘要" });
    } catch (err) {
      notify({ tone: "danger", message: "保存摘要失败", detail: formatErrorMessage(err) });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex-1 overflow-y-auto px-6 py-5">
      <div className="mx-auto flex w-full max-w-[1160px] flex-col gap-5">
        {/* Toolbar */}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-1 rounded-lg border border-border-subtle bg-surface p-1">
            <button
              type="button"
              onClick={() => setLevelFilter("all")}
              className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                levelFilter === "all"
                  ? "bg-accent-soft text-accent"
                  : "text-ink-secondary hover:text-ink-primary"
              }`}
            >
              全部层级
            </button>
            {ARC_SUMMARY_LEVELS.map((lvl) => (
              <button
                key={lvl.value}
                type="button"
                onClick={() => setLevelFilter(lvl.value)}
                className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                  levelFilter === lvl.value
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:text-ink-primary"
                }`}
              >
                {lvl.label}
              </button>
            ))}
          </div>

          <button
            type="button"
            onClick={openCreate}
            className="flex items-center gap-1.5 rounded-md bg-accent px-3 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90"
          >
            <Plus className="h-3.5 w-3.5" />
            <span>新建摘要</span>
          </button>
        </div>

        {/* Summary Cards */}
        {loading && items.length === 0 ? (
          <div className="py-16 text-center text-xs text-ink-muted">
            正在加载弧线摘要…
          </div>
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-border-strong bg-surface/35 py-16 text-center">
            <div className="text-xs text-ink-secondary">
              暂无 10 章弧线摘要或卷级摘要（用于长篇 P5 记忆金字塔压缩注入）
            </div>
            <button
              type="button"
              onClick={openCreate}
              className="rounded-md border border-accent/40 bg-accent-soft px-3.5 py-1.5 text-xs font-medium text-accent hover:border-accent"
            >
              新建第一条弧线摘要
            </button>
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-3.5 md:grid-cols-2">
            {items.map((item) => {
              const vol = item.volumeId ? volumeMap.get(item.volumeId) : undefined;
              const isVolume = item.level === "volume";
              return (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => openEdit(item)}
                  className="flex flex-col rounded-xl border border-border-subtle bg-surface p-4 text-left transition-colors hover:border-border-strong hover:bg-elevated/60"
                >
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                      <BookMarked
                        className={`h-3.5 w-3.5 ${
                          isVolume ? "text-accent" : "text-ink-secondary"
                        }`}
                      />
                      <span
                        className={`rounded border px-1.5 py-0.5 text-[10.5px] font-medium ${
                          isVolume
                            ? "border-accent/30 bg-accent-soft text-accent"
                            : "border-border-subtle bg-canvas text-ink-secondary"
                        }`}
                      >
                        {getArcSummaryLevelLabel(item.level)}
                      </span>
                      {vol && (
                        <span className="truncate text-xs font-medium text-ink-primary">
                          {vol.title}
                        </span>
                      )}
                    </div>
                    <span className="nib-mono text-xs text-ink-secondary">
                      第 {formatSeq(item.startSeq)} – {formatSeq(item.endSeq)} 章
                    </span>
                  </div>

                  <p className="mt-3 line-clamp-4 text-[13px] leading-relaxed text-ink-secondary whitespace-pre-wrap">
                    {item.summary}
                  </p>
                </button>
              );
            })}
          </div>
        )}
      </div>

      {/* Editor Modal */}
      {editor && (
        <div
          className="fixed inset-0 z-40 flex items-center justify-center bg-black/55 p-4 backdrop-blur-[2px]"
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) setEditor(null);
          }}
        >
          <div className="w-full max-w-[520px] overflow-hidden rounded-xl border border-border-strong bg-elevated shadow-[var(--shadow-elevated)]">
            <div className="flex items-center justify-between border-b border-border-subtle px-5 py-3.5">
              <h3 className="text-sm font-medium text-ink-primary">
                {editor.id > 0 ? "编辑弧线 / 卷摘要" : "新建弧线 / 卷摘要"}
              </h3>
              <button
                type="button"
                onClick={() => setEditor(null)}
                className="rounded p-1 text-ink-muted hover:bg-surface hover:text-ink-primary"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            <div className="flex flex-col gap-3.5 p-5">
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div>
                  <label className="mb-1 block text-xs text-ink-secondary">
                    摘要层级
                  </label>
                  <select
                    value={editor.level}
                    onChange={(e) => setEditor({ ...editor, level: e.target.value })}
                    className="w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                  >
                    {ARC_SUMMARY_LEVELS.map((lvl) => (
                      <option key={lvl.value} value={lvl.value}>
                        {lvl.label}
                      </option>
                    ))}
                  </select>
                </div>

                {editor.level === "volume" && (
                  <div>
                    <label className="mb-1 block text-xs text-ink-secondary">
                      对应卷（可选）
                    </label>
                    <select
                      value={editor.volumeId}
                      onChange={(e) =>
                        setEditor({ ...editor, volumeId: e.target.value })
                      }
                      className="w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                    >
                      <option value="">不指定</option>
                      {volumes.map((v) => (
                        <option key={v.id} value={v.id}>
                          {v.title}
                        </option>
                      ))}
                    </select>
                  </div>
                )}
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="mb-1 block text-xs text-ink-secondary">
                    起始章号
                  </label>
                  <input
                    type="number"
                    min={1}
                    value={editor.startSeq}
                    onChange={(e) => setEditor({ ...editor, startSeq: e.target.value })}
                    className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                  />
                </div>
                <div>
                  <label className="mb-1 block text-xs text-ink-secondary">
                    结束章号
                  </label>
                  <input
                    type="number"
                    min={1}
                    value={editor.endSeq}
                    onChange={(e) => setEditor({ ...editor, endSeq: e.target.value })}
                    className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary focus:border-accent focus:outline-none"
                  />
                </div>
              </div>

              <div>
                <label className="mb-1 block text-xs text-ink-secondary">
                  阶段因果摘要
                </label>
                <textarea
                  rows={5}
                  value={editor.summary}
                  onChange={(e) => setEditor({ ...editor, summary: e.target.value })}
                  placeholder="提炼本阶段主线因果推进、关键人物境界/资产变动与重大伏笔收束"
                  className="w-full resize-y rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                />
              </div>
            </div>

            <div className="flex items-center justify-end gap-2 border-t border-border-subtle bg-surface/50 px-5 py-3">
              <button
                type="button"
                onClick={() => setEditor(null)}
                className="rounded-md px-3 py-1.5 text-xs text-ink-secondary hover:bg-surface hover:text-ink-primary"
              >
                取消
              </button>
              <button
                type="button"
                disabled={saving}
                onClick={() => void handleSave()}
                className="rounded-md bg-accent px-4 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
              >
                {saving ? "保存中…" : "保存"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
