import { useCallback, useEffect, useState } from "react";
import { ArrowRight, GitFork, Plus, Trash2 } from "lucide-react";
import { AtlasService, type EntityRelation } from "../../mygo";
import { useApp } from "../../stores/AppContext";
import { formatErrorMessage, isIMEComposing } from "../../lib/constants";

const PRESET_RELATIONS = ["师徒", "死敌", "同盟", "隶属", "宿敌", "亲友", "道侣", "交易"];

export function RelationsBoard() {
  const { activeBook, entities, memoryRevision, notify } = useApp();
  const [relations, setRelations] = useState<EntityRelation[]>([]);
  const [loading, setLoading] = useState(false);
  const [sourceId, setSourceId] = useState<string>("");
  const [targetId, setTargetId] = useState<string>("");
  const [relType, setRelType] = useState("");
  const [description, setDescription] = useState("");
  const [saving, setSaving] = useState(false);

  const loadRelations = useCallback(async () => {
    if (!activeBook) {
      setRelations([]);
      return;
    }
    setLoading(true);
    try {
      const list = await AtlasService.listEntityRelations(activeBook.id);
      setRelations(list);
    } catch (err) {
      notify({ tone: "danger", message: "加载关系网失败", detail: formatErrorMessage(err) });
    } finally {
      setLoading(false);
    }
  }, [activeBook, notify]);

  useEffect(() => {
    void loadRelations();
  }, [loadRelations, memoryRevision]);

  const handleAdd = async () => {
    if (!activeBook) return;
    const src = Number.parseInt(sourceId, 10);
    const dst = Number.parseInt(targetId, 10);
    if (!src || !dst) {
      notify({ tone: "warn", message: "请选择关系双方实体" });
      return;
    }
    if (src === dst) {
      notify({ tone: "warn", message: "关系双方不能是同一个实体" });
      return;
    }
    if (!relType.trim()) {
      notify({ tone: "warn", message: "请输入关系类型（如 师徒、死敌）" });
      return;
    }
    setSaving(true);
    try {
      await AtlasService.upsertEntityRelation({
        id: 0,
        bookId: activeBook.id,
        sourceEntityId: src,
        targetEntityId: dst,
        relationType: relType.trim(),
        description: description.trim(),
      });
      setRelType("");
      setDescription("");
      await loadRelations();
      notify({ tone: "ok", message: "已添加关系并自动参与写作上下文注入" });
    } catch (err) {
      notify({ tone: "danger", message: "保存关系失败", detail: formatErrorMessage(err) });
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (id: number) => {
    try {
      await AtlasService.deleteEntityRelation(id);
      await loadRelations();
    } catch (err) {
      notify({ tone: "danger", message: "删除关系失败", detail: formatErrorMessage(err) });
    }
  };

  const connectableEntities = entities.filter((e) => !e.isInvariant);

  return (
    <div className="flex-1 overflow-y-auto px-6 py-5">
      <div className="mx-auto flex w-full max-w-[1160px] flex-col gap-5">
        {/* Quick Add Relation Form */}
        <section className="flex flex-col gap-3 rounded-xl border border-border-subtle bg-surface p-4">
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2 text-sm font-medium text-ink-primary">
              <GitFork className="h-4 w-4 text-accent" />
              <span>新增角色与势力关系</span>
            </div>
            <span className="text-xs text-ink-muted">
              出场角色的关系会自动注入写作简报 P2，防止长篇阵营错乱
            </span>
          </div>

          <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-[1fr_140px_1fr]">
            <select
              value={sourceId}
              onChange={(e) => setSourceId(e.target.value)}
              className="rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-xs text-ink-primary focus:border-accent focus:outline-none"
            >
              <option value="">选择一方（如 主角）</option>
              {connectableEntities.map((e) => (
                <option key={e.id} value={e.id}>
                  {e.name}
                </option>
              ))}
            </select>

            <input
              type="text"
              value={relType}
              onChange={(e) => setRelType(e.target.value)}
              placeholder="关系（如 死敌）"
              className="rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
            />

            <select
              value={targetId}
              onChange={(e) => setTargetId(e.target.value)}
              className="rounded-lg border border-border-subtle bg-canvas px-3 py-2 text-xs text-ink-primary focus:border-accent focus:outline-none"
            >
              <option value="">选择另一方（如 宗门/反派）</option>
              {connectableEntities.map((e) => (
                <option key={e.id} value={e.id}>
                  {e.name}
                </option>
              ))}
            </select>
          </div>

          <div className="flex flex-wrap items-center gap-1.5">
            {PRESET_RELATIONS.map((preset) => (
              <button
                key={preset}
                type="button"
                onClick={() => setRelType(preset)}
                className={`rounded-full border px-2.5 py-0.5 text-xs transition-colors ${
                  relType === preset
                    ? "border-accent bg-accent-soft text-accent"
                    : "border-border-subtle bg-canvas text-ink-secondary hover:border-border-strong"
                }`}
              >
                {preset}
              </button>
            ))}
          </div>

          <div className="flex items-center gap-2">
            <input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !isIMEComposing(e)) {
                  e.preventDefault();
                  void handleAdd();
                }
              }}
              placeholder="补充原因或现状（可选，例如：第 12 章黑市夺丹结仇）"
              className="min-w-0 flex-1 rounded-lg border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
            />
            <button
              type="button"
              disabled={saving || connectableEntities.length < 2}
              onClick={() => void handleAdd()}
              className="flex shrink-0 items-center gap-1.5 rounded-lg bg-accent px-3.5 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
            >
              <Plus className="h-3.5 w-3.5" />
              <span>添加关系</span>
            </button>
          </div>
        </section>

        {/* Relation List */}
        {loading && relations.length === 0 ? (
          <div className="py-16 text-center text-xs text-ink-muted">正在加载关系网…</div>
        ) : relations.length === 0 ? (
          <div className="rounded-xl border border-dashed border-border-strong bg-surface/35 py-16 text-center text-xs text-ink-secondary">
            暂无记录的角色或势力关系（也可直接让 AI 导演帮你梳理并记录全书关系）
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            {relations.map((rel) => (
              <div
                key={rel.id}
                className="flex items-start justify-between gap-3 rounded-xl border border-border-subtle bg-surface p-4"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2 text-sm font-medium text-ink-primary">
                    <span>{rel.sourceName}</span>
                    <span className="inline-flex items-center gap-1 rounded-full border border-accent/35 bg-accent-soft px-2 py-0.5 text-xs font-medium text-accent">
                      <span>{rel.relationType}</span>
                      <ArrowRight className="h-3 w-3" />
                    </span>
                    <span>{rel.targetName}</span>
                  </div>
                  {rel.description && (
                    <p className="mt-1.5 text-xs leading-relaxed text-ink-secondary">
                      {rel.description}
                    </p>
                  )}
                </div>
                <button
                  type="button"
                  onClick={() => void handleDelete(rel.id)}
                  title="删除此关系"
                  className="rounded p-1 text-ink-muted transition-colors hover:bg-elevated hover:text-danger"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
