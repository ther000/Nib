import { useEffect, useState, type FormEvent } from "react";
import { FolderOpen, X } from "lucide-react";
import { LibraryService, SettingsService } from "../mygo";
import { useApp } from "../stores/AppContext";
import { formatErrorMessage } from "../lib/constants";

export function CreateBookModal() {
  const {
    createBookOpen,
    closeCreateBook,
    genrePresets,
    refreshBooks,
    setActiveTab,
    notify,
  } = useApp();

  const [title, setTitle] = useState("");
  const [genre, setGenre] = useState("xuanhuan");
  const [premise, setPremise] = useState("");
  const [goldenFinger, setGoldenFinger] = useState("");
  const [targetWords, setTargetWords] = useState("2500");
  const [exportDir, setExportDir] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!createBookOpen) return;
    setTitle("");
    setGenre(genrePresets[0]?.id ?? "xuanhuan");
    setPremise("");
    setGoldenFinger("");
    setTargetWords("2500");
    setExportDir("");
    setSubmitting(false);
  }, [createBookOpen, genrePresets]);

  useEffect(() => {
    if (!createBookOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") closeCreateBook();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [createBookOpen, closeCreateBook]);

  if (!createBookOpen) return null;

  const handlePickExportDir = async () => {
    try {
      const dir = await SettingsService.pickExportDir();
      if (dir) setExportDir(dir);
    } catch (err) {
      notify({ tone: "danger", message: "选择导出目录失败", detail: formatErrorMessage(err) });
    }
  };

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    const trimmedTitle = title.trim();
    if (!trimmedTitle) {
      notify({ tone: "warn", message: "请输入书名" });
      return;
    }
    const parsedWords = Number.parseInt(targetWords, 10);
    const finalWords = Number.isFinite(parsedWords) && parsedWords > 0 ? parsedWords : 2500;

    setSubmitting(true);
    try {
      const created = await LibraryService.createBook({
        title: trimmedTitle,
        genre,
        premise: premise.trim(),
        goldenFinger: goldenFinger.trim(),
        targetChapterWords: finalWords,
        exportDir: exportDir.trim(),
      });
      await refreshBooks(created.id);
      setActiveTab("architect");
      closeCreateBook();
      notify({ tone: "ok", message: `已创建《${created.title}》` });
    } catch (err) {
      notify({ tone: "danger", message: "创建书籍失败", detail: formatErrorMessage(err) });
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-[2px]"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) closeCreateBook();
      }}
    >
      <div className="flex max-h-[88vh] w-full max-w-[580px] flex-col overflow-hidden rounded-xl border border-border-strong bg-elevated shadow-[var(--shadow-elevated)]">
        <div className="flex items-center justify-between border-b border-border-subtle px-5 py-3.5">
          <h2 className="text-[15px] font-medium text-ink-primary">新建书籍</h2>
          <button
            type="button"
            onClick={closeCreateBook}
            aria-label="关闭"
            className="rounded-md p-1 text-ink-muted transition-colors hover:bg-surface hover:text-ink-primary"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="flex flex-1 flex-col overflow-y-auto">
          <div className="flex flex-col gap-4 px-5 py-4">
            {/* Title */}
            <div>
              <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                书名
              </label>
              <input
                type="text"
                autoFocus
                required
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="例如：太初剑渊"
                className="w-full rounded-md border border-border-subtle bg-canvas px-3 py-2 text-sm text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
              />
            </div>

            {/* Genre Pack */}
            <div>
              <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                题材包
              </label>
              <div className="grid grid-cols-2 gap-2">
                {genrePresets.map((preset) => {
                  const selected = preset.id === genre;
                  return (
                    <button
                      key={preset.id}
                      type="button"
                      onClick={() => setGenre(preset.id)}
                      className={`rounded-lg border px-3 py-2.5 text-left transition-colors ${
                        selected
                          ? "border-accent bg-accent-soft"
                          : "border-border-subtle bg-canvas/60 hover:border-border-strong"
                      }`}
                    >
                      <div className="flex items-center justify-between gap-2">
                        <span
                          className={`text-[13px] font-medium ${
                            selected ? "text-accent" : "text-ink-primary"
                          }`}
                        >
                          {preset.name}
                        </span>
                        <span className="text-[11px] text-ink-muted">
                          {preset.loopCycleChapters}
                        </span>
                      </div>
                      <div className="mt-1 line-clamp-1 text-[11px] text-ink-secondary">
                        {preset.tagline}
                      </div>
                    </button>
                  );
                })}
              </div>
            </div>

            {/* Premise */}
            <div>
              <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                核心设定 / 简介
              </label>
              <textarea
                rows={2}
                value={premise}
                onChange={(e) => setPremise(e.target.value)}
                placeholder="世界观核心矛盾、主角开局处境与长期目标"
                className="w-full resize-none rounded-md border border-border-subtle bg-canvas px-3 py-2 text-[13px] leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
              />
            </div>

            {/* Golden Finger */}
            <div>
              <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                金手指 / 核心依仗
              </label>
              <textarea
                rows={2}
                value={goldenFinger}
                onChange={(e) => setGoldenFinger(e.target.value)}
                placeholder="外挂机制、触发条件与使用代价"
                className="w-full resize-none rounded-md border border-border-subtle bg-canvas px-3 py-2 text-[13px] leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
              />
            </div>

            {/* Target Words + Export Dir */}
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-[140px_1fr]">
              <div>
                <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                  单章目标字数
                </label>
                <input
                  type="number"
                  min={300}
                  max={50000}
                  step={100}
                  value={targetWords}
                  onChange={(e) => setTargetWords(e.target.value)}
                  className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-3 py-2 text-[13px] text-ink-primary focus:border-accent focus:outline-none"
                />
              </div>

              <div>
                <label className="mb-1.5 block text-xs font-medium text-ink-secondary">
                  本地 Markdown 导出目录（可选）
                </label>
                <div className="flex gap-1.5">
                  <input
                    type="text"
                    value={exportDir}
                    onChange={(e) => setExportDir(e.target.value)}
                    placeholder="留空可稍后在设置中指定"
                    className="min-w-0 flex-1 rounded-md border border-border-subtle bg-canvas px-3 py-2 text-[13px] text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                  />
                  <button
                    type="button"
                    onClick={() => void handlePickExportDir()}
                    className="flex shrink-0 items-center gap-1.5 rounded-md border border-border-subtle bg-surface px-2.5 text-xs text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary"
                  >
                    <FolderOpen className="h-3.5 w-3.5" />
                    <span>选择</span>
                  </button>
                </div>
              </div>
            </div>
          </div>

          <div className="flex items-center justify-end gap-2 border-t border-border-subtle bg-surface/50 px-5 py-3">
            <button
              type="button"
              onClick={closeCreateBook}
              className="rounded-md px-3.5 py-1.5 text-xs font-medium text-ink-secondary transition-colors hover:bg-surface hover:text-ink-primary"
            >
              取消
            </button>
            <button
              type="submit"
              disabled={submitting}
              className="rounded-md bg-accent px-4 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
            >
              {submitting ? "创建中…" : "创建书籍"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
