import { isIMEComposing } from "../../lib/constants";
import { ModalShell } from "../ui/ModalShell";

export interface VolumeDialogState {
  mode: "create" | "edit";
  id: number;
  seq: number;
  title: string;
  goal: string;
  climaxPlan: string;
}

interface VolumeFormModalProps {
  dialog: VolumeDialogState | null;
  saving: boolean;
  onChange: (next: VolumeDialogState) => void;
  onClose: () => void;
  onSubmit: () => void;
}

export function VolumeFormModal({
  dialog,
  saving,
  onChange,
  onClose,
  onSubmit,
}: VolumeFormModalProps) {
  return (
    <ModalShell
      open={dialog !== null}
      onClose={onClose}
      closeDisabled={saving}
      title={dialog?.mode === "create" ? "新建卷" : "编辑卷"}
      maxWidthClass="max-w-[440px]"
      footer={
        <>
          <button
            type="button"
            disabled={saving}
            onClick={onClose}
            className="rounded-md px-3 py-1.5 text-xs text-ink-secondary hover:bg-surface hover:text-ink-primary disabled:opacity-40"
          >
            取消
          </button>
          <button
            type="button"
            disabled={saving}
            onClick={onSubmit}
            className="rounded-md bg-accent px-3.5 py-1.5 text-xs font-semibold text-canvas hover:opacity-90 disabled:opacity-50"
          >
            {saving ? "保存中…" : "保存"}
          </button>
        </>
      }
    >
      {dialog && (
        <div className="flex flex-col gap-3 p-4">
          <div>
            <label className="mb-1 block text-xs text-ink-secondary">卷名</label>
            <input
              type="text"
              autoFocus
              value={dialog.title}
              onChange={(e) => onChange({ ...dialog, title: e.target.value })}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !isIMEComposing(e)) {
                  e.preventDefault();
                  onSubmit();
                }
              }}
              placeholder="例如：第一卷 青云起"
              className="w-full rounded-md border border-border-subtle bg-canvas px-3 py-2 text-sm text-ink-primary focus:border-accent focus:outline-none"
            />
          </div>
          <div>
            <label className="mb-1 block text-xs text-ink-secondary">
              卷核心目标
            </label>
            <textarea
              rows={2}
              value={dialog.goal}
              onChange={(e) => onChange({ ...dialog, goal: e.target.value })}
              placeholder="本卷主角要达成的实力/地位/资源阶段目标"
              className="w-full resize-none rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary focus:border-accent focus:outline-none"
            />
          </div>
          <div>
            <label className="mb-1 block text-xs text-ink-secondary">
              卷末高潮规划
            </label>
            <textarea
              rows={2}
              value={dialog.climaxPlan}
              onChange={(e) => onChange({ ...dialog, climaxPlan: e.target.value })}
              placeholder="本卷收束大战、揭秘或换地图节点"
              className="w-full resize-none rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary focus:border-accent focus:outline-none"
            />
          </div>
        </div>
      )}
    </ModalShell>
  );
}
