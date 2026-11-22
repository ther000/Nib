import { LibraryService } from "../mygo";
import { formatErrorMessage } from "./constants";
import type { NotifyInput } from "../stores/ToastContext";

export type BookExportFormat = "txt" | "epub";

/**
 * 统一封装整本书导出（TXT / EPUB）及 Toast 结果提示，消除多处视图重复实现。
 */
export async function exportBookWithNotify(
  bookId: number,
  bookTitle: string,
  format: BookExportFormat,
  notify: (input: NotifyInput) => void
): Promise<string> {
  const label = format === "txt" ? "TXT" : "EPUB";
  try {
    const outPath =
      format === "txt"
        ? await LibraryService.exportBookTXT(bookId)
        : await LibraryService.exportBookEPUB(bookId);
    if (outPath) {
      notify({
        tone: "ok",
        message: `已导出《${bookTitle}》${label}`,
        detail: outPath,
      });
    }
    return outPath;
  } catch (err) {
    notify({
      tone: "danger",
      message: `导出 ${label} 失败`,
      detail: formatErrorMessage(err),
    });
    return "";
  }
}
