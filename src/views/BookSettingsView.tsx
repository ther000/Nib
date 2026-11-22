import { useCallback, useEffect, useState } from "react";
import {
  BookOpen,
  Download,
  FolderOpen,
  Save,
  Sparkles,
  Trash2,
} from "lucide-react";
import { LibraryService, SettingsService } from "../mygo";
import { useApp } from "../stores/AppContext";
import { formatErrorMessage, formatNumber } from "../lib/constants";
import { exportBookWithNotify } from "../lib/bookExport";
import { ConfirmDialog, type ConfirmDialogState } from "../components/ui/ConfirmDialog";

const POV_OPTIONS = [
  { value: "third_limited", label: "第三人称限知" },
  { value: "first_person", label: "第一人称" },
  { value: "third_omniscient", label: "第三人称全知" },
  { value: "multi_pov", label: "多视角群像" },
];

export function BookSettingsView() {
  const {
    activeBook,
    genrePresets,
    refreshBooks,
    deleteBook,
    openAtlasSubTab,
    registerSaveHandler,
    notify,
  } = useApp();

  const [title, setTitle] = useState("");
  const [genre, setGenre] = useState("");
  const [premise, setPremise] = useState("");
  const [goldenFinger, setGoldenFinger] = useState("");
  const [povMode, setPovMode] = useState("third_limited");
  const [pacingStyle, setPacingStyle] = useState("");
  const [targetChapterWords, setTargetChapterWords] = useState("2500");
  const [dailyWordGoal, setDailyWordGoal] = useState("4000");
  const [writingPrompt, setWritingPrompt] = useState("");
  const [exportDir, setExportDir] = useState("");

  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [exporting, setExporting] = useState<"txt" | "epub" | "md" | null>(null);
  const [confirmDialog, setConfirmDialog] = useState<ConfirmDialogState | null>(null);
  const [deleting, setDeleting] = useState(false);

  useEffect(() => {
    if (!activeBook) return;
    setTitle(activeBook.title ?? "");
    setGenre(activeBook.genre ?? "");
    setPremise(activeBook.premise ?? "");
    setGoldenFinger(activeBook.goldenFinger ?? "");
    setPovMode(activeBook.povMode || "third_limited");
    setPacingStyle(activeBook.pacingStyle ?? "");
    setTargetChapterWords(String(activeBook.targetChapterWords || 2500));
    setDailyWordGoal(String(activeBook.dailyWordGoal || 4000));
    setWritingPrompt(activeBook.writingPrompt ?? "");
    setExportDir(activeBook.exportDir ?? "");
    setDirty(false);
  }, [activeBook]);

  const handleSave = useCallback(async () => {
    if (!activeBook || saving) return;
    const trimmedTitle = title.trim();
    if (!trimmedTitle) {
      notify({ tone: "warn", message: "书名不能为空" });
      return;
    }
    const parsedWords = Number.parseInt(targetChapterWords, 10);
    if (!Number.isFinite(parsedWords) || parsedWords < 300 || parsedWords > 50000) {
      notify({ tone: "warn", message: "单章目标字数需在 300 到 50,000 字之间" });
      return;
    }
    const parsedDailyGoal = Number.parseInt(dailyWordGoal, 10);
    if (!Number.isFinite(parsedDailyGoal) || parsedDailyGoal < 100 || parsedDailyGoal > 200000) {
      notify({ tone: "warn", message: "日更目标需在 100 到 200,000 字之间" });
      return;
    }
    if (writingPrompt.length > 4000) {
      notify({ tone: "warn", message: "固定写作指令不能超过 4,000 字" });
      return;
    }

    setSaving(true);
    try {
      await LibraryService.updateBook({
        id: activeBook.id,
        title: trimmedTitle,
        genre: genre || activeBook.genre,
        premise: premise.trim(),
        goldenFinger: goldenFinger.trim(),
        targetChapterWords: parsedWords,
        exportDir: exportDir.trim(),
        povMode,
        pacingStyle: pacingStyle.trim(),
        writingPrompt: writingPrompt.trim(),
        dailyWordGoal: parsedDailyGoal,
      });
      await refreshBooks(activeBook.id);
      setDirty(false);
      notify({ tone: "ok", message: "本书设置已保存" });
    } catch (err) {
      notify({ tone: "danger", message: "保存本书设置失败", detail: formatErrorMessage(err) });
    } finally {
      setSaving(false);
    }
  }, [
    activeBook,
    saving,
    title,
    genre,
    premise,
    goldenFinger,
    targetChapterWords,
    dailyWordGoal,
    writingPrompt,
    exportDir,
    povMode,
    pacingStyle,
    refreshBooks,
    notify,
  ]);

  useEffect(() => {
    return registerSaveHandler(handleSave);
  }, [registerSaveHandler, handleSave]);

  const handlePickExportDir = async () => {
    try {
      const dir = await SettingsService.pickExportDir();
      if (dir) {
        setExportDir(dir);
        setDirty(true);
      }
    } catch (err) {
      notify({ tone: "danger", message: "选择导出目录失败", detail: formatErrorMessage(err) });
    }
  };

  const handleExportTXT = async () => {
    if (!activeBook || exporting) return;
    setExporting("txt");
    try {
      await exportBookWithNotify(activeBook.id, activeBook.title, "txt", notify);
    } finally {
      setExporting(null);
    }
  };

  const handleExportEPUB = async () => {
    if (!activeBook || exporting) return;
    setExporting("epub");
    try {
      await exportBookWithNotify(activeBook.id, activeBook.title, "epub", notify);
    } finally {
      setExporting(null);
    }
  };

  const handleExportMarkdown = async () => {
    if (!activeBook || exporting) return;
    const trimmedDir = exportDir.trim();
    if (!trimmedDir) {
      notify({ tone: "warn", message: "请先选择本地 Markdown 镜像目录并保存" });
      return;
    }
    setExporting("md");
    try {
      if (dirty) {
        await handleSave();
      }
      const res = await LibraryService.exportBookMarkdown(activeBook.id);
      notify({
        tone: "ok",
        message: `已导出全书 Markdown（${res.chapterCount} 章）`,
        detail: res.bookDir || res.exportDir,
      });
    } catch (err) {
      notify({ tone: "danger", message: "导出 Markdown 失败", detail: formatErrorMessage(err) });
    } finally {
      setExporting(null);
    }
  };

  const handleRequestDeleteBook = () => {
    if (!activeBook) return;
    setConfirmDialog({
      title: "删除本书",
      message: `确定删除《${activeBook.title}》？删除后作品会先移入全局设置的回收站，30 天内可随时恢复。`,
      confirmLabel: "删除作品",
      danger: true,
      onConfirm: async () => {
        setDeleting(true);
        try {
          await deleteBook(activeBook.id);
          setConfirmDialog(null);
        } finally {
          setDeleting(false);
        }
      },
    });
  };

  if (!activeBook) return null;

  return (
    <div className="nib-paper-sheet nib-scroll-stable m-2 mt-0 flex-1 overflow-y-auto px-8 py-7">
      <div className="mx-auto flex w-full max-w-[820px] flex-col gap-5 pb-12">
        {/* Page Header */}
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border-subtle pb-4">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <BookOpen className="h-4 w-4 shrink-0 text-accent" />
              <h1 className="truncate text-base font-medium text-ink-primary">
                《{activeBook.title}》设置
              </h1>
              {dirty && (
                <span className="rounded bg-warn-soft px-1.5 py-0.5 text-[11px] font-medium text-warn">
                  未保存
                </span>
              )}
            </div>
            <div className="nib-mono mt-1 flex flex-wrap items-center gap-2 text-xs text-ink-muted">
              <span>{formatNumber(activeBook.chapterCount)} 章</span>
              <span>·</span>
              <span>{formatNumber(activeBook.totalWords)} 字</span>
              <span>·</span>
              <span>仅作用于当前作品</span>
            </div>
          </div>

          <button
            type="button"
            disabled={saving || !dirty}
            onClick={() => void handleSave()}
            className="flex shrink-0 items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
          >
            <Save className="h-3.5 w-3.5" />
            <span>{saving ? "保存中…" : "保存设置"}</span>
          </button>
        </div>

        {/* 1. 作品信息 */}
        <section className="flex flex-col gap-3.5 rounded-xl border border-border-subtle bg-surface p-5">
          <h2 className="text-sm font-medium text-ink-primary">作品信息</h2>

          <div className="grid grid-cols-1 gap-3.5 sm:grid-cols-[1fr_200px]">
            <div>
              <label className="mb-1 block text-xs text-ink-secondary">书名</label>
              <input
                type="text"
                value={title}
                onChange={(e) => {
                  setTitle(e.target.value);
                  setDirty(true);
                }}
                placeholder="输入作品书名"
                className="w-full rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
              />
            </div>

            <div>
              <label className="mb-1 block text-xs text-ink-secondary">题材预设</label>
              <select
                value={genre}
                onChange={(e) => {
                  setGenre(e.target.value);
                  setDirty(true);
                }}
                className="w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-2 text-xs text-ink-primary focus:border-accent focus:outline-none"
              >
                {genrePresets.map((g) => (
                  <option key={g.id} value={g.id}>
                    {g.name}
                  </option>
                ))}
              </select>
            </div>
          </div>

          <div>
            <label className="mb-1 block text-xs text-ink-secondary">
              核心简介 / 故事主线
            </label>
            <textarea
              rows={2}
              value={premise}
              onChange={(e) => {
                setPremise(e.target.value);
                setDirty(true);
              }}
              placeholder="一句话卖点、主角处境与核心驱动力"
              className="w-full resize-y rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
            />
          </div>

          <div>
            <label className="mb-1 block text-xs text-ink-secondary">
              核心金手指 / 世界机制
            </label>
            <textarea
              rows={2}
              value={goldenFinger}
              onChange={(e) => {
                setGoldenFinger(e.target.value);
                setDirty(true);
              }}
              placeholder="主角核心依仗、升级条件、限制与代价"
              className="w-full resize-y rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
            />
          </div>
        </section>

        {/* 2. 写作规范与字数目标 */}
        <section className="flex flex-col gap-3.5 rounded-xl border border-border-subtle bg-surface p-5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h2 className="text-sm font-medium text-ink-primary">写作规范与目标</h2>
            <button
              type="button"
              onClick={() => openAtlasSubTab("style")}
              className="text-xs text-accent hover:opacity-80"
            >
              前往管理文风样章与禁用词 →
            </button>
          </div>

          <div className="grid grid-cols-1 gap-3.5 sm:grid-cols-2 lg:grid-cols-4">
            <div>
              <label className="mb-1 block text-xs text-ink-secondary">叙事视角</label>
              <select
                value={povMode}
                onChange={(e) => {
                  setPovMode(e.target.value);
                  setDirty(true);
                }}
                className="w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-2 text-xs text-ink-primary focus:border-accent focus:outline-none"
              >
                {POV_OPTIONS.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {opt.label}
                  </option>
                ))}
              </select>
            </div>

            <div>
              <label className="mb-1 block text-xs text-ink-secondary">节奏风格</label>
              <input
                type="text"
                value={pacingStyle}
                onChange={(e) => {
                  setPacingStyle(e.target.value);
                  setDirty(true);
                }}
                placeholder="如：番茄快节奏"
                className="w-full rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
              />
            </div>

            <div>
              <label className="mb-1 block text-xs text-ink-secondary">单章目标字数</label>
              <input
                type="number"
                min={300}
                max={50000}
                step={100}
                value={targetChapterWords}
                onChange={(e) => {
                  setTargetChapterWords(e.target.value);
                  setDirty(true);
                }}
                className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs text-ink-primary focus:border-accent focus:outline-none"
              />
            </div>

            <div>
              <label className="mb-1 block text-xs text-ink-secondary">日更目标（字）</label>
              <input
                type="number"
                min={100}
                max={200000}
                step={500}
                value={dailyWordGoal}
                onChange={(e) => {
                  setDailyWordGoal(e.target.value);
                  setDirty(true);
                }}
                className="nib-mono w-full rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs text-ink-primary focus:border-accent focus:outline-none"
              />
            </div>
          </div>

          <div>
            <label className="mb-1 block text-xs text-ink-secondary">
              固定写作指令（本书每次生成都会自动带给 AI）
            </label>
            <textarea
              rows={3}
              maxLength={4000}
              value={writingPrompt}
              onChange={(e) => {
                setWritingPrompt(e.target.value);
                setDirty(true);
              }}
              placeholder="如：每章至少一次信息差反转；主角不降智；对话占比不低于三成。"
              className="w-full resize-y rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
            />
          </div>
        </section>

        {/* 3. 导出、镜像与旧稿工具 */}
        <section className="flex flex-col gap-3.5 rounded-xl border border-border-subtle bg-surface p-5">
          <h2 className="text-sm font-medium text-ink-primary">导出与本地镜像</h2>

          <div>
            <label className="mb-1 block text-xs text-ink-secondary">
              本地 Markdown 单向镜像目录
            </label>
            <div className="flex gap-2">
              <input
                type="text"
                value={exportDir}
                onChange={(e) => {
                  setExportDir(e.target.value);
                  setDirty(true);
                }}
                placeholder="未配置导出目录"
                className="min-w-0 flex-1 rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
              />
              <button
                type="button"
                onClick={() => void handlePickExportDir()}
                className="flex shrink-0 items-center gap-1.5 rounded-md border border-border-subtle bg-elevated px-3 py-2 text-xs text-ink-primary transition-colors hover:border-border-strong"
              >
                <FolderOpen className="h-3.5 w-3.5 text-accent" />
                <span>选择目录</span>
              </button>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2 pt-1">
            <button
              type="button"
              disabled={exporting !== null}
              onClick={() => void handleExportTXT()}
              className="flex items-center gap-1.5 rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs font-medium text-ink-primary transition-colors hover:border-border-strong disabled:opacity-40"
            >
              <Download className="h-3.5 w-3.5 text-ink-muted" />
              <span>{exporting === "txt" ? "导出中…" : "导出整本 TXT"}</span>
            </button>
            <button
              type="button"
              disabled={exporting !== null}
              onClick={() => void handleExportEPUB()}
              className="flex items-center gap-1.5 rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs font-medium text-ink-primary transition-colors hover:border-border-strong disabled:opacity-40"
            >
              <Download className="h-3.5 w-3.5 text-ink-muted" />
              <span>{exporting === "epub" ? "打包中…" : "导出 EPUB 电子书"}</span>
            </button>
            <button
              type="button"
              disabled={exporting !== null || !exportDir.trim()}
              onClick={() => void handleExportMarkdown()}
              title={exportDir.trim() ? "将全书单向导出为 Markdown 副本" : "请先配置镜像目录"}
              className="flex items-center gap-1.5 rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs font-medium text-ink-primary transition-colors hover:border-border-strong disabled:opacity-40"
            >
              <FolderOpen className="h-3.5 w-3.5 text-ink-muted" />
              <span>{exporting === "md" ? "导出中…" : "立即同步 Markdown"}</span>
            </button>
            <button
              type="button"
              onClick={() => {
                window.dispatchEvent(
                  new CustomEvent("nib:reverse-extract", {
                    detail: { bookId: activeBook.id },
                  })
                );
              }}
              className="flex items-center gap-1.5 rounded-md border border-accent/40 bg-accent-soft px-3 py-1.5 text-xs font-medium text-accent transition-colors hover:border-accent"
            >
              <Sparkles className="h-3.5 w-3.5" />
              <span>AI 反推旧稿设定</span>
            </button>
          </div>
        </section>

        {/* 4. 危险操作 */}
        <section className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-danger/30 bg-danger-soft/15 px-5 py-4">
          <div>
            <div className="text-sm font-medium text-ink-primary">删除本书</div>
            <div className="mt-0.5 text-xs text-ink-secondary">
              作品会先移入全局设置的回收站，30 天内可随时恢复
            </div>
          </div>
          <button
            type="button"
            onClick={handleRequestDeleteBook}
            className="flex items-center gap-1.5 rounded-md border border-danger/45 bg-danger-soft px-3.5 py-1.5 text-xs font-medium text-danger transition-colors hover:border-danger"
          >
            <Trash2 className="h-3.5 w-3.5" />
            <span>删除《{activeBook.title}》</span>
          </button>
        </section>
      </div>

      <ConfirmDialog
        dialog={confirmDialog}
        busy={deleting}
        onClose={() => setConfirmDialog(null)}
      />
    </div>
  );
}
