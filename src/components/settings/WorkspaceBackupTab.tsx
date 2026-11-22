import { useCallback, useEffect, useState } from "react";
import { Moon, RefreshCw, Sun, Trash2 } from "lucide-react";
import {
  SettingsService,
  type AutoBackupInfo,
  type TrashItem,
} from "../../mygo";
import { useApp } from "../../stores/AppContext";
import { formatErrorMessage } from "../../lib/constants";
import { ConfirmDialog, type ConfirmDialogState } from "../ui/ConfirmDialog";

function formatFileSize(bytes: number) {
  if (bytes >= 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`;
  if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${bytes} B`;
}

export function WorkspaceBackupTab() {
  const {
    theme,
    setTheme,
    proseFont,
    setProseFont,
    proseIndent,
    setProseIndent,
    proseFontSize,
    setProseFontSize,
    proseLineHeight,
    setProseLineHeight,
    proseMaxWidth,
    setProseMaxWidth,
    resetTypography,
    refreshBooks,
    notify,
  } = useApp();

  const [backingUpDb, setBackingUpDb] = useState(false);

  const [autoBackups, setAutoBackups] = useState<AutoBackupInfo[]>([]);
  const [backupsLoading, setBackupsLoading] = useState(false);
  const [restoringBackupPath, setRestoringBackupPath] = useState<string | null>(null);
  const [trashItems, setTrashItems] = useState<TrashItem[]>([]);
  const [trashLoading, setTrashLoading] = useState(false);
  const [restoringTrashId, setRestoringTrashId] = useState<number | null>(null);
  const [clearingTrash, setClearingTrash] = useState(false);

  const [confirmDialog, setConfirmDialog] = useState<ConfirmDialogState | null>(null);
  const [confirmBusy, setConfirmBusy] = useState(false);

  const loadAutoBackups = useCallback(async () => {
    setBackupsLoading(true);
    try {
      setAutoBackups(await SettingsService.listAutoBackups());
    } catch (err) {
      notify({ tone: "danger", message: "加载自动备份失败", detail: formatErrorMessage(err) });
    } finally {
      setBackupsLoading(false);
    }
  }, [notify]);

  const loadTrash = useCallback(async () => {
    setTrashLoading(true);
    try {
      setTrashItems(await SettingsService.listTrash());
    } catch (err) {
      notify({ tone: "danger", message: "加载回收站失败", detail: formatErrorMessage(err) });
    } finally {
      setTrashLoading(false);
    }
  }, [notify]);

  useEffect(() => {
    void loadAutoBackups();
    void loadTrash();
  }, [loadAutoBackups, loadTrash]);

  const handleBackupDatabase = async () => {
    if (backingUpDb) return;
    setBackingUpDb(true);
    try {
      const savedPath = await SettingsService.backupDatabase();
      if (savedPath) {
        notify({ tone: "ok", message: "已安全备份全部书库数据", detail: savedPath });
      }
    } catch (err) {
      notify({ tone: "danger", message: "备份数据库失败", detail: formatErrorMessage(err) });
    } finally {
      setBackingUpDb(false);
    }
  };

  const performRestoreFromPath = async (backupPath: string) => {
    setRestoringBackupPath(backupPath);
    setConfirmBusy(true);
    try {
      const preRestore = await SettingsService.restoreDatabaseFromFile(backupPath);
      setConfirmDialog(null);
      notify({ tone: "ok", message: "书库已恢复，正在重新载入…", detail: preRestore });
      setTimeout(() => window.location.reload(), 500);
    } catch (err) {
      notify({ tone: "danger", message: "恢复书库失败", detail: formatErrorMessage(err) });
    } finally {
      setRestoringBackupPath(null);
      setConfirmBusy(false);
    }
  };

  const handleRestoreBackup = (backup: AutoBackupInfo) => {
    setConfirmDialog({
      title: "恢复自动备份",
      message: `恢复后会覆盖当前书库的全部数据。系统会先自动备份当前数据库，确认恢复「${backup.name}」吗？`,
      confirmLabel: "确认恢复",
      danger: true,
      onConfirm: () => performRestoreFromPath(backup.path),
    });
  };

  const handleRestoreFromFile = async () => {
    try {
      const path = await SettingsService.chooseBackupFile();
      if (!path) return;
      setConfirmDialog({
        title: "从文件恢复书库",
        message: "恢复后会覆盖当前书库的全部数据，系统会先自动备份当前数据库。确认继续吗？",
        confirmLabel: "确认恢复",
        danger: true,
        onConfirm: () => performRestoreFromPath(path),
      });
    } catch (err) {
      notify({ tone: "danger", message: "选择备份文件失败", detail: formatErrorMessage(err) });
    }
  };

  const handleRestoreTrash = async (item: TrashItem) => {
    setRestoringTrashId(item.id);
    try {
      const result = await SettingsService.restoreTrash(item.id);
      await refreshBooks();
      await loadTrash();
      notify({ tone: "ok", message: result.message || `已恢复「${item.title}」` });
    } catch (err) {
      notify({ tone: "danger", message: "恢复失败", detail: formatErrorMessage(err) });
    } finally {
      setRestoringTrashId(null);
    }
  };

  const handleDeleteTrash = async (item: TrashItem) => {
    try {
      await SettingsService.deleteTrash(item.id);
      await loadTrash();
    } catch (err) {
      notify({ tone: "danger", message: "删除回收站条目失败", detail: formatErrorMessage(err) });
    }
  };

  const handleClearTrash = () => {
    if (trashItems.length === 0) return;
    setConfirmDialog({
      title: "清空回收站",
      message: "清空后回收站内的内容将无法再恢复，确认清空吗？",
      confirmLabel: "清空回收站",
      danger: true,
      onConfirm: async () => {
        setClearingTrash(true);
        setConfirmBusy(true);
        try {
          const count = await SettingsService.clearTrash();
          setConfirmDialog(null);
          await loadTrash();
          notify({ tone: "ok", message: `已清空回收站（${count} 项）` });
        } catch (err) {
          notify({ tone: "danger", message: "清空回收站失败", detail: formatErrorMessage(err) });
        } finally {
          setClearingTrash(false);
          setConfirmBusy(false);
        }
      },
    });
  };

  return (
    <>
      <div className="flex-1 overflow-y-auto px-5 py-4">
        <div className="flex flex-col gap-5">
          {/* Theme Selector */}
          <section>
            <div className="mb-2 text-xs font-medium text-ink-secondary">
              界面主题
            </div>
            <div className="grid grid-cols-2 gap-3">
              <button
                type="button"
                onClick={() => setTheme("dark")}
                className={`flex items-center gap-3 rounded-lg border p-3 text-left transition-colors ${
                  theme === "dark"
                    ? "border-accent bg-accent-soft"
                    : "border-border-subtle bg-canvas/60 hover:border-border-strong"
                }`}
              >
                <span
                  className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-white/15"
                  style={{ backgroundColor: "#141416", color: "#D4A95E" }}
                >
                  <Moon className="h-4 w-4" />
                </span>
                <div>
                  <div className="text-xs font-medium text-ink-primary">
                    深色护眼
                  </div>
                  <div className="nib-mono text-[10.5px] text-ink-muted">
                    #141416
                  </div>
                </div>
              </button>

              <button
                type="button"
                onClick={() => setTheme("light")}
                className={`flex items-center gap-3 rounded-lg border p-3 text-left transition-colors ${
                  theme === "light"
                    ? "border-accent bg-accent-soft"
                    : "border-border-subtle bg-canvas/60 hover:border-border-strong"
                }`}
              >
                <span
                  className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-black/15"
                  style={{ backgroundColor: "#F6F3EC", color: "#9C6D24" }}
                >
                  <Sun className="h-4 w-4" />
                </span>
                <div>
                  <div className="text-xs font-medium text-ink-primary">
                    暖纸浅色
                  </div>
                  <div className="nib-mono text-[10.5px] text-ink-muted">
                    #F6F3EC
                  </div>
                </div>
              </button>
            </div>
          </section>

          {/* Prose Typography */}
          <section>
            <div className="mb-2 text-xs font-medium text-ink-secondary">
              正文阅读排版
            </div>
            <div className="flex flex-col gap-2.5 rounded-lg border border-border-subtle bg-surface/50 p-3.5">
              <div className="flex items-center justify-between gap-3">
                <span className="text-xs text-ink-primary">正文字体</span>
                <div className="flex items-center gap-1 rounded-md border border-border-subtle bg-canvas p-0.5">
                  <button
                    type="button"
                    onClick={() => setProseFont("serif")}
                    className={`rounded px-2.5 py-1 text-xs transition-colors ${
                      proseFont === "serif"
                        ? "bg-accent-soft font-medium text-accent"
                        : "text-ink-secondary hover:text-ink-primary"
                    }`}
                  >
                    衬线阅读体
                  </button>
                  <button
                    type="button"
                    onClick={() => setProseFont("sans")}
                    className={`rounded px-2.5 py-1 text-xs transition-colors ${
                      proseFont === "sans"
                        ? "bg-accent-soft font-medium text-accent"
                        : "text-ink-secondary hover:text-ink-primary"
                    }`}
                  >
                    无衬线码字体
                  </button>
                </div>
              </div>

              <label className="flex cursor-pointer items-center justify-between border-t border-border-subtle pt-2.5">
                <span className="text-xs text-ink-primary">
                  段落首行缩进两字符（2em）
                </span>
                <input
                  type="checkbox"
                  checked={proseIndent}
                  onChange={(e) => setProseIndent(e.target.checked)}
                  className="h-4 w-4 accent-[var(--accent)]"
                />
              </label>

              {/* 字号 / 行距 / 栏宽自由调节 */}
              <div className="grid grid-cols-1 gap-3 border-t border-border-subtle pt-3 sm:grid-cols-3">
                <div>
                  <div className="mb-1 flex items-center justify-between text-xs">
                    <span className="text-ink-secondary">正文字号</span>
                    <span className="nib-mono text-ink-primary">{proseFontSize}px</span>
                  </div>
                  <input
                    type="range"
                    min={14}
                    max={24}
                    step={1}
                    value={proseFontSize}
                    onChange={(e) => setProseFontSize(Number(e.target.value))}
                    className="w-full accent-[var(--accent)]"
                  />
                </div>

                <div>
                  <div className="mb-1 flex items-center justify-between text-xs">
                    <span className="text-ink-secondary">行距倍数</span>
                    <span className="nib-mono text-ink-primary">
                      {proseLineHeight.toFixed(2)}
                    </span>
                  </div>
                  <input
                    type="range"
                    min={1.5}
                    max={2.3}
                    step={0.04}
                    value={proseLineHeight}
                    onChange={(e) => setProseLineHeight(Number(e.target.value))}
                    className="w-full accent-[var(--accent)]"
                  />
                </div>

                <div>
                  <div className="mb-1 flex items-center justify-between text-xs">
                    <span className="text-ink-secondary">正文栏宽</span>
                    <span className="nib-mono text-ink-primary">{proseMaxWidth}px</span>
                  </div>
                  <input
                    type="range"
                    min={580}
                    max={960}
                    step={20}
                    value={proseMaxWidth}
                    onChange={(e) => setProseMaxWidth(Number(e.target.value))}
                    className="w-full accent-[var(--accent)]"
                  />
                </div>
              </div>

              <div className="flex items-center justify-end">
                <button
                  type="button"
                  onClick={resetTypography}
                  className="text-[11px] text-ink-muted transition-colors hover:text-accent"
                >
                  恢复默认排版参数
                </button>
              </div>

              <div className="mt-1 rounded-md border border-border-subtle bg-canvas px-4 py-3">
                <div className="nib-prose-column">
                  <p>
                    残阵灵光骤灭，焦糊味混着血腥气在石室散开。他俯身扣紧那枚微温玉瓶，转身朝暗门掠去。
                  </p>
                </div>
              </div>
            </div>
          </section>

          {/* Database Backup */}
          <section className="border-t border-border-subtle pt-4">
            <div className="mb-2 text-xs font-medium text-ink-secondary">
              本地数据安全备份
            </div>
            <div className="flex items-center justify-between gap-3 rounded-lg border border-border-subtle bg-surface/50 p-3.5">
              <div className="text-xs text-ink-secondary">
                将所有书籍、章节正文、历史版本快照与设定记忆完整打包备份为一个本地 .db 文件
              </div>
              <button
                type="button"
                disabled={backingUpDb}
                onClick={() => void handleBackupDatabase()}
                className="shrink-0 rounded-md border border-border-subtle bg-elevated px-3.5 py-1.5 text-xs font-medium text-ink-primary transition-colors hover:border-border-strong disabled:opacity-40"
              >
                {backingUpDb ? "备份中…" : "一键备份书库"}
              </button>
            </div>
          </section>

          {/* Auto Rolling Backups */}
          <section className="border-t border-border-subtle pt-4">
            <div className="mb-2 flex items-center justify-between">
              <span className="text-xs font-medium text-ink-secondary">
                自动备份（最近 7 份）
              </span>
              <div className="flex items-center gap-3">
                <button
                  type="button"
                  disabled={restoringBackupPath !== null}
                  onClick={() => void handleRestoreFromFile()}
                  className="text-xs text-ink-muted transition-colors hover:text-accent disabled:opacity-50"
                >
                  从文件恢复
                </button>
                <button
                  type="button"
                  disabled={backupsLoading}
                  onClick={() => void loadAutoBackups()}
                  className="flex items-center gap-1 text-xs text-ink-muted transition-colors hover:text-accent disabled:opacity-50"
                >
                  <RefreshCw className={`h-3 w-3 ${backupsLoading ? "animate-spin" : ""}`} />
                  刷新
                </button>
              </div>
            </div>
            <div className="overflow-hidden rounded-lg border border-border-subtle bg-surface/50">
              {autoBackups.length === 0 ? (
                <div className="px-4 py-3 text-xs text-ink-muted">
                  {backupsLoading ? "正在加载…" : "暂无自动备份，下次启动 Nib 会创建当日备份。"}
                </div>
              ) : (
                <div className="divide-y divide-border-subtle">
                  {autoBackups.map((backup) => (
                    <div
                      key={backup.path}
                      className="flex items-center justify-between gap-3 px-4 py-2.5"
                    >
                      <div className="min-w-0">
                        <div className="nib-mono truncate text-xs text-ink-primary">
                          {backup.name}
                        </div>
                        <div className="mt-0.5 flex items-center gap-2 text-[11px] text-ink-muted">
                          <span className="nib-mono">{formatFileSize(backup.sizeBytes)}</span>
                          <span>{new Date(backup.modifiedAt).toLocaleString("zh-CN")}</span>
                        </div>
                      </div>
                      <button
                        type="button"
                        disabled={restoringBackupPath !== null}
                        onClick={() => handleRestoreBackup(backup)}
                        className="shrink-0 rounded-md border border-border-subtle bg-elevated px-2.5 py-1 text-xs text-ink-primary transition-colors hover:border-border-strong disabled:opacity-40"
                      >
                        {restoringBackupPath === backup.path ? "恢复中…" : "恢复"}
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </section>

          {/* Trash */}
          <section className="border-t border-border-subtle pt-4">
            <div className="mb-2 flex items-center justify-between">
              <span className="text-xs font-medium text-ink-secondary">
                回收站（保留 30 天）
              </span>
              {trashItems.length > 0 && (
                <button
                  type="button"
                  disabled={clearingTrash}
                  onClick={handleClearTrash}
                  className="text-xs text-ink-muted transition-colors hover:text-danger disabled:opacity-50"
                >
                  {clearingTrash ? "清空中…" : "清空回收站"}
                </button>
              )}
            </div>
            <div className="overflow-hidden rounded-lg border border-border-subtle bg-surface/50">
              {trashItems.length === 0 ? (
                <div className="px-4 py-3 text-xs text-ink-muted">
                  {trashLoading ? "正在加载…" : "回收站为空。删除书籍、分卷、章节或设定后会先进入这里。"}
                </div>
              ) : (
                <div className="divide-y divide-border-subtle">
                  {trashItems.map((item) => (
                    <div
                      key={item.id}
                      className="flex items-center justify-between gap-3 px-4 py-2.5"
                    >
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="rounded bg-canvas px-1.5 py-0.5 text-[10px] text-ink-secondary">
                            {item.itemType === "book"
                              ? "书籍"
                              : item.itemType === "volume"
                              ? "分卷"
                              : item.itemType === "chapter"
                              ? "章节"
                              : "设定"}
                          </span>
                          <span className="truncate text-xs font-medium text-ink-primary">
                            {item.title}
                          </span>
                        </div>
                        <div className="mt-0.5 text-[11px] text-ink-muted">
                          {item.summary ? `${item.summary} · ` : ""}
                          {new Date(item.deletedAt).toLocaleString("zh-CN")} 删除
                        </div>
                      </div>
                      <div className="flex shrink-0 items-center gap-1.5">
                        <button
                          type="button"
                          disabled={restoringTrashId === item.id}
                          onClick={() => void handleRestoreTrash(item)}
                          className="rounded-md border border-border-subtle bg-elevated px-2.5 py-1 text-xs text-ink-primary transition-colors hover:border-border-strong disabled:opacity-40"
                        >
                          {restoringTrashId === item.id ? "恢复中…" : "恢复"}
                        </button>
                        <button
                          type="button"
                          onClick={() => void handleDeleteTrash(item)}
                          className="rounded-md p-1 text-ink-muted transition-colors hover:text-danger"
                          title="永久删除"
                          aria-label="永久删除"
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </section>
        </div>
      </div>

      <ConfirmDialog
        dialog={confirmDialog}
        busy={confirmBusy}
        onClose={() => setConfirmDialog(null)}
      />
    </>
  );
}
