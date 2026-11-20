import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Plus,
  Search,
  ShieldAlert,
  Trash2,
  X,
} from "lucide-react";
import { AtlasService, type Entity, type EntityKind, type GenreFieldSpec } from "../mygo";
import { useApp } from "../stores/AppContext";
import {
  ENTITY_KINDS,
  formatErrorMessage,
  getEntityKindLabel,
} from "../lib/constants";

interface CustomAttrRow {
  localKey: string;
  key: string;
  value: string;
}

interface EntityEditorState {
  id: number;
  kind: EntityKind;
  name: string;
  aliases: string[];
  isInvariant: boolean;
  summary: string;
  presetValues: Record<string, string>;
  customAttrs: CustomAttrRow[];
}

let attrSeq = 1;
function nextAttrKey(): string {
  return `attr-${attrSeq++}`;
}

export function AtlasView() {
  const {
    activeBook,
    activeGenrePreset,
    entities: allEntities,
    refreshEntities,
    notify,
  } = useApp();

  const [kindFilter, setKindFilter] = useState<string>("all");
  const [searchQuery, setSearchQuery] = useState("");
  const [filteredEntities, setFilteredEntities] = useState<Entity[]>([]);
  const [loading, setLoading] = useState(false);

  // Slide-over editor state
  const [editor, setEditor] = useState<EntityEditorState | null>(null);
  const [aliasDraft, setAliasDraft] = useState("");
  const [saving, setSaving] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  // All invariants pinned at top
  const invariants = useMemo(
    () => allEntities.filter((e) => e.isInvariant),
    [allEntities]
  );

  // Spec map by key for labeling cards nicely
  const fieldLabelMap = useMemo(() => {
    const map = new Map<string, GenreFieldSpec>();
    for (const spec of activeGenrePreset?.entityFields ?? []) {
      map.set(spec.key, spec);
    }
    return map;
  }, [activeGenrePreset]);

  // Query backend via AtlasService.listEntities (supports FTS5 + LIKE fallback)
  const fetchFiltered = useCallback(async () => {
    if (!activeBook) {
      setFilteredEntities([]);
      return;
    }
    setLoading(true);
    try {
      const list = await AtlasService.listEntities(activeBook.id, {
        kind: kindFilter === "all" ? undefined : kindFilter,
        query: searchQuery.trim() || undefined,
      });
      setFilteredEntities(list);
    } catch (err) {
      notify({ tone: "danger", message: "检索设定账本失败", detail: formatErrorMessage(err) });
    } finally {
      setLoading(false);
    }
  }, [activeBook, kindFilter, searchQuery, notify]);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void fetchFiltered();
    }, 150);
    return () => window.clearTimeout(timer);
  }, [fetchFiltered, allEntities]);

  const getSpecsForKind = useCallback(
    (kind: EntityKind): GenreFieldSpec[] =>
      (activeGenrePreset?.entityFields ?? []).filter((f) => f.entityKind === kind),
    [activeGenrePreset]
  );

  const openNewEntity = (presetKind: EntityKind = "character", isInvariant = false) => {
    const specs = getSpecsForKind(presetKind);
    const presetValues: Record<string, string> = {};
    for (const spec of specs) {
      presetValues[spec.key] = "";
    }
    setEditor({
      id: 0,
      kind: presetKind,
      name: "",
      aliases: [],
      isInvariant,
      summary: "",
      presetValues,
      customAttrs: [],
    });
    setAliasDraft("");
    setConfirmDelete(false);
  };

  const openEditEntity = (ent: Entity) => {
    const specs = getSpecsForKind(ent.kind);
    const specKeySet = new Set(specs.map((s) => s.key));
    const presetValues: Record<string, string> = {};
    for (const spec of specs) {
      presetValues[spec.key] = ent.attributes?.[spec.key] ?? "";
    }
    const customAttrs: CustomAttrRow[] = [];
    for (const [k, v] of Object.entries(ent.attributes ?? {})) {
      if (!specKeySet.has(k)) {
        customAttrs.push({ localKey: nextAttrKey(), key: k, value: v });
      }
    }
    setEditor({
      id: ent.id,
      kind: ent.kind,
      name: ent.name,
      aliases: Array.isArray(ent.aliases) ? [...ent.aliases] : [],
      isInvariant: ent.isInvariant,
      summary: ent.summary,
      presetValues,
      customAttrs,
    });
    setAliasDraft("");
    setConfirmDelete(false);
  };

  // When changing kind inside editor, re-seed preset fields while keeping entered values
  const handleKindChange = (nextKind: EntityKind) => {
    if (!editor) return;
    const allCurrent: Record<string, string> = { ...editor.presetValues };
    for (const row of editor.customAttrs) {
      const k = row.key.trim();
      if (k && row.value.trim()) allCurrent[k] = row.value;
    }

    const nextSpecs = getSpecsForKind(nextKind);
    const nextSpecKeys = new Set(nextSpecs.map((s) => s.key));
    const nextPresetValues: Record<string, string> = {};
    for (const spec of nextSpecs) {
      nextPresetValues[spec.key] = allCurrent[spec.key] ?? "";
    }
    const nextCustomAttrs: CustomAttrRow[] = [];
    for (const [k, v] of Object.entries(allCurrent)) {
      if (!nextSpecKeys.has(k) && v.trim()) {
        nextCustomAttrs.push({ localKey: nextAttrKey(), key: k, value: v });
      }
    }

    setEditor({
      ...editor,
      kind: nextKind,
      presetValues: nextPresetValues,
      customAttrs: nextCustomAttrs,
    });
  };

  const addAlias = () => {
    if (!editor) return;
    const parts = aliasDraft
      .split(/[,，\n]+/)
      .map((s) => s.trim())
      .filter(Boolean);
    if (parts.length === 0) return;
    const next = Array.from(new Set([...editor.aliases, ...parts]));
    setEditor({ ...editor, aliases: next });
    setAliasDraft("");
  };

  const removeAlias = (alias: string) => {
    if (!editor) return;
    setEditor({
      ...editor,
      aliases: editor.aliases.filter((a) => a !== alias),
    });
  };

  const handleSaveEntity = async () => {
    if (!activeBook || !editor) return;
    const trimmedName = editor.name.trim();
    if (!trimmedName) {
      notify({ tone: "warn", message: "实体名称不能为空" });
      return;
    }

    const attributes: Record<string, string> = {};
    for (const [k, v] of Object.entries(editor.presetValues)) {
      const trimmedVal = v.trim();
      if (trimmedVal) attributes[k] = trimmedVal;
    }
    for (const row of editor.customAttrs) {
      const k = row.key.trim();
      const v = row.value.trim();
      if (k && v) attributes[k] = v;
    }

    // Flush pending alias text if user forgot to hit Enter
    const finalAliases = [...editor.aliases];
    const extraAlias = aliasDraft.trim();
    if (extraAlias && !finalAliases.includes(extraAlias)) {
      finalAliases.push(extraAlias);
    }

    setSaving(true);
    try {
      await AtlasService.upsertEntity({
        id: editor.id,
        bookId: activeBook.id,
        kind: editor.kind,
        name: trimmedName,
        aliases: finalAliases,
        isInvariant: editor.isInvariant,
        summary: editor.summary.trim(),
        attributes,
      });
      await refreshEntities(activeBook.id);
      setEditor(null);
      notify({
        tone: "ok",
        message: editor.id > 0 ? `已更新「${trimmedName}」` : `已新建「${trimmedName}」`,
      });
    } catch (err) {
      notify({ tone: "danger", message: "保存实体失败", detail: formatErrorMessage(err) });
    } finally {
      setSaving(false);
    }
  };

  const handleDeleteEntity = async () => {
    if (!activeBook || !editor || editor.id <= 0) return;
    setSaving(true);
    try {
      await AtlasService.deleteEntity(editor.id);
      await refreshEntities(activeBook.id);
      setEditor(null);
      notify({ tone: "ok", message: `已删除「${editor.name}」` });
    } catch (err) {
      notify({ tone: "danger", message: "删除实体失败", detail: formatErrorMessage(err) });
    } finally {
      setSaving(false);
    }
  };

  // Close slide-over on Escape
  useEffect(() => {
    if (!editor) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setEditor(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [editor]);

  const editorSpecs = useMemo(
    () => (editor ? getSpecsForKind(editor.kind) : []),
    [editor, getSpecsForKind]
  );

  return (
    <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden bg-canvas">
      {/* Pinned Top Strip: 世界铁律 (isInvariant: true) */}
      <section className="shrink-0 border-b border-border-subtle bg-surface/85 px-6 py-3">
        <div className="mx-auto flex w-full max-w-[1160px] flex-wrap items-center gap-2.5">
          <div className="flex shrink-0 items-center gap-1.5">
            <ShieldAlert className="h-4 w-4 text-danger" />
            <span className="text-xs font-semibold text-ink-primary">世界铁律</span>
            <span className="nib-mono rounded bg-danger-soft px-1.5 py-0.5 text-[10px] font-medium text-danger">
              P0 强制注入 · {invariants.length}
            </span>
          </div>

          <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
            {invariants.map((inv) => (
              <button
                key={inv.id}
                type="button"
                onClick={() => openEditEntity(inv)}
                title={inv.summary || inv.name}
                className="flex max-w-[260px] items-center gap-1.5 rounded-full border border-danger/30 bg-danger-soft/70 px-2.5 py-1 text-xs text-ink-primary transition-colors hover:border-danger/60"
              >
                <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-danger" />
                <span className="truncate font-medium">{inv.name}</span>
              </button>
            ))}
            <button
              type="button"
              onClick={() => openNewEntity("rule", true)}
              className="flex items-center gap-1 rounded-full border border-dashed border-accent/50 px-2.5 py-1 text-xs text-accent transition-colors hover:border-accent hover:bg-accent-soft"
            >
              <Plus className="h-3 w-3" />
              <span>新增铁律</span>
            </button>
          </div>
        </div>
      </section>

      {/* Filter Toolbar + Entity Grid */}
      <div className="flex-1 overflow-y-auto px-6 py-5">
        <div className="mx-auto flex w-full max-w-[1160px] flex-col gap-5">
          {/* Toolbar */}
          <div className="flex flex-wrap items-center justify-between gap-3">
            {/* Kind Filter */}
            <div className="flex flex-wrap items-center gap-1 rounded-lg border border-border-subtle bg-surface p-1">
              <button
                type="button"
                onClick={() => setKindFilter("all")}
                className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                  kindFilter === "all"
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:text-ink-primary"
                }`}
              >
                全部
              </button>
              {ENTITY_KINDS.map((k) => (
                <button
                  key={k.value}
                  type="button"
                  onClick={() => setKindFilter(k.value)}
                  className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                    kindFilter === k.value
                      ? "bg-accent-soft text-accent"
                      : "text-ink-secondary hover:text-ink-primary"
                  }`}
                >
                  {k.label}
                </button>
              ))}
            </div>

            {/* Search + Add */}
            <div className="flex items-center gap-2">
              <div className="relative">
                <Search className="pointer-events-none absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2 text-ink-muted" />
                <input
                  type="text"
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  placeholder="检索实体名、别名或设定…"
                  className="w-56 rounded-md border border-border-subtle bg-surface py-1.5 pr-7 pl-8 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                />
                {searchQuery && (
                  <button
                    type="button"
                    onClick={() => setSearchQuery("")}
                    className="absolute top-1/2 right-2 -translate-y-1/2 text-ink-muted hover:text-ink-primary"
                  >
                    <X className="h-3 w-3" />
                  </button>
                )}
              </div>

              <button
                type="button"
                onClick={() =>
                  openNewEntity(
                    kindFilter === "all" ? "character" : kindFilter,
                    false
                  )
                }
                className="flex items-center gap-1.5 rounded-md bg-accent px-3 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90"
              >
                <Plus className="h-3.5 w-3.5" />
                <span>新建实体</span>
              </button>
            </div>
          </div>

          {/* Card Grid */}
          {loading && filteredEntities.length === 0 ? (
            <div className="py-16 text-center text-xs text-ink-muted">
              正在加载设定账本…
            </div>
          ) : filteredEntities.length === 0 ? (
            <div className="flex flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-border-strong bg-surface/35 py-16 text-center">
              <div className="text-xs text-ink-secondary">
                {searchQuery
                  ? `未找到与「${searchQuery}」匹配的实体`
                  : "当前分类下暂无设定实体"}
              </div>
              {!searchQuery && (
                <button
                  type="button"
                  onClick={() =>
                    openNewEntity(
                      kindFilter === "all" ? "character" : kindFilter,
                      false
                    )
                  }
                  className="rounded-md border border-accent/40 bg-accent-soft px-3.5 py-1.5 text-xs font-medium text-accent hover:border-accent"
                >
                  新建实体档案
                </button>
              )}
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-3.5 sm:grid-cols-2 lg:grid-cols-3">
              {filteredEntities.map((ent) => {
                const attrEntries = Object.entries(ent.attributes ?? {}).slice(0, 4);
                return (
                  <button
                    key={ent.id}
                    type="button"
                    onClick={() => openEditEntity(ent)}
                    className="group flex flex-col rounded-xl border border-border-subtle bg-surface p-4 text-left transition-colors hover:border-border-strong hover:bg-elevated/60"
                  >
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <span className="truncate text-[14.5px] font-medium text-ink-primary">
                            {ent.name}
                          </span>
                          {ent.isInvariant && (
                            <span className="shrink-0 rounded bg-danger-soft px-1.5 py-0.5 text-[10px] font-medium text-danger">
                              铁律
                            </span>
                          )}
                        </div>
                        {ent.aliases && ent.aliases.length > 0 && (
                          <div className="mt-1 truncate text-[11px] text-ink-muted">
                            别名：{ent.aliases.join(" / ")}
                          </div>
                        )}
                      </div>
                      <span className="shrink-0 rounded-md border border-border-subtle bg-canvas/70 px-2 py-0.5 text-[11px] text-ink-secondary">
                        {getEntityKindLabel(ent.kind)}
                      </span>
                    </div>

                    {ent.summary && (
                      <p className="mt-2.5 line-clamp-2 text-xs leading-relaxed text-ink-secondary">
                        {ent.summary}
                      </p>
                    )}

                    {attrEntries.length > 0 && (
                      <div className="mt-3 flex flex-wrap gap-1.5 border-t border-border-subtle pt-2.5">
                        {attrEntries.map(([k, v]) => {
                          const spec = fieldLabelMap.get(k);
                          return (
                            <span
                              key={k}
                              className={`inline-flex max-w-full items-center gap-1 rounded px-1.5 py-0.5 text-[11px] ${
                                spec?.highRisk
                                  ? "bg-accent-soft text-ink-primary"
                                  : "bg-canvas/80 text-ink-secondary"
                              }`}
                            >
                              <span className="shrink-0 text-ink-muted">
                                {spec?.label ?? k}:
                              </span>
                              <span className="truncate">{v}</span>
                            </span>
                          );
                        })}
                      </div>
                    )}
                  </button>
                );
              })}
            </div>
          )}
        </div>
      </div>

      {/* Slide-Over Detail Editor */}
      {editor && (
        <div
          className="fixed inset-0 z-40 flex justify-end bg-black/50 backdrop-blur-[1px]"
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) setEditor(null);
          }}
        >
          <div className="flex h-full w-full max-w-[460px] flex-col border-l border-border-strong bg-elevated shadow-[var(--shadow-elevated)]">
            {/* Header */}
            <div className="flex items-center justify-between border-b border-border-subtle px-5 py-3.5">
              <h2 className="text-sm font-medium text-ink-primary">
                {editor.id > 0 ? "编辑设定实体" : "新建设定实体"}
              </h2>
              <button
                type="button"
                onClick={() => setEditor(null)}
                aria-label="关闭"
                className="rounded p-1 text-ink-muted hover:bg-surface hover:text-ink-primary"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            {/* Body */}
            <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
              {/* Name + Kind */}
              <div className="grid grid-cols-[1fr_110px] gap-2.5">
                <div>
                  <label className="mb-1 block text-xs text-ink-secondary">名称</label>
                  <input
                    type="text"
                    autoFocus
                    value={editor.name}
                    onChange={(e) => setEditor({ ...editor, name: e.target.value })}
                    placeholder="角色 / 道具 / 宗门 / 铁律名称"
                    className="w-full rounded-md border border-border-subtle bg-canvas px-3 py-2 text-sm text-ink-primary focus:border-accent focus:outline-none"
                  />
                </div>
                <div>
                  <label className="mb-1 block text-xs text-ink-secondary">分类</label>
                  <select
                    value={editor.kind}
                    onChange={(e) => handleKindChange(e.target.value)}
                    className="w-full rounded-md border border-border-subtle bg-canvas px-2.5 py-2 text-xs text-ink-primary focus:border-accent focus:outline-none"
                  >
                    {ENTITY_KINDS.map((k) => (
                      <option key={k.value} value={k.value}>
                        {k.label}
                      </option>
                    ))}
                  </select>
                </div>
              </div>

              {/* Invariant Toggle */}
              <label className="flex cursor-pointer items-center justify-between rounded-lg border border-border-subtle bg-canvas/70 px-3.5 py-2.5">
                <div className="flex items-center gap-2">
                  <ShieldAlert
                    className={`h-4 w-4 ${
                      editor.isInvariant ? "text-danger" : "text-ink-muted"
                    }`}
                  />
                  <div>
                    <div className="text-xs font-medium text-ink-primary">
                      设为世界铁律
                    </div>
                    <div className="text-[11px] text-ink-muted">
                      每章出稿强制注入 P0 槽位，不可被上下文配额裁剪
                    </div>
                  </div>
                </div>
                <input
                  type="checkbox"
                  checked={editor.isInvariant}
                  onChange={(e) =>
                    setEditor({ ...editor, isInvariant: e.target.checked })
                  }
                  className="h-4 w-4 accent-[var(--accent)]"
                />
              </label>

              {/* Aliases */}
              <div>
                <label className="mb-1 block text-xs text-ink-secondary">
                  别名 / 称号
                </label>
                <div className="flex flex-wrap items-center gap-1.5 rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5">
                  {editor.aliases.map((alias) => (
                    <span
                      key={alias}
                      className="inline-flex items-center gap-1 rounded-full bg-elevated px-2 py-0.5 text-xs text-ink-primary"
                    >
                      <span>{alias}</span>
                      <button
                        type="button"
                        onClick={() => removeAlias(alias)}
                        className="text-ink-muted hover:text-danger"
                      >
                        <X className="h-2.5 w-2.5" />
                      </button>
                    </span>
                  ))}
                  <input
                    type="text"
                    value={aliasDraft}
                    onChange={(e) => setAliasDraft(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === ",") {
                        e.preventDefault();
                        addAlias();
                      }
                    }}
                    onBlur={addAlias}
                    placeholder="输入后回车添加…"
                    className="min-w-[110px] flex-1 bg-transparent py-0.5 text-xs text-ink-primary placeholder:text-ink-muted focus:outline-none"
                  />
                </div>
              </div>

              {/* Summary */}
              <div>
                <label className="mb-1 block text-xs text-ink-secondary">
                  核心设定摘要
                </label>
                <textarea
                  rows={3}
                  value={editor.summary}
                  onChange={(e) => setEditor({ ...editor, summary: e.target.value })}
                  placeholder="外貌性格、核心立场、触发规则或不可违背的铁律正文"
                  className="w-full resize-y rounded-md border border-border-subtle bg-canvas px-3 py-2 text-xs leading-relaxed text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                />
              </div>

              {/* Genre-Preseeded Structured Attributes */}
              {editorSpecs.length > 0 && (
                <div className="flex flex-col gap-2.5">
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-medium text-ink-secondary">
                      {activeGenrePreset?.name ?? "题材"}快捷属性
                    </span>
                    <span className="text-[11px] text-ink-muted">
                      高危字段重点防吃书
                    </span>
                  </div>
                  <div className="flex flex-col gap-2">
                    {editorSpecs.map((spec) => (
                      <div key={spec.key}>
                        <div className="mb-1 flex items-center gap-1.5">
                          <label className="text-[11.5px] text-ink-secondary">
                            {spec.label}
                          </label>
                          {spec.highRisk && (
                            <span className="rounded bg-accent-soft px-1 text-[10px] text-accent">
                              关键状态
                            </span>
                          )}
                        </div>
                        <input
                          type="text"
                          value={editor.presetValues[spec.key] ?? ""}
                          onChange={(e) =>
                            setEditor({
                              ...editor,
                              presetValues: {
                                ...editor.presetValues,
                                [spec.key]: e.target.value,
                              },
                            })
                          }
                          placeholder={spec.placeholder}
                          className="w-full rounded-md border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                        />
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* Custom Key-Value Attributes */}
              <div className="flex flex-col gap-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-medium text-ink-secondary">
                    自定义属性
                  </span>
                  <button
                    type="button"
                    onClick={() =>
                      setEditor({
                        ...editor,
                        customAttrs: [
                          ...editor.customAttrs,
                          { localKey: nextAttrKey(), key: "", value: "" },
                        ],
                      })
                    }
                    className="flex items-center gap-1 text-xs text-accent hover:opacity-80"
                  >
                    <Plus className="h-3 w-3" />
                    <span>添加属性</span>
                  </button>
                </div>

                {editor.customAttrs.map((row) => (
                  <div key={row.localKey} className="flex items-center gap-1.5">
                    <input
                      type="text"
                      value={row.key}
                      onChange={(e) =>
                        setEditor({
                          ...editor,
                          customAttrs: editor.customAttrs.map((r) =>
                            r.localKey === row.localKey
                              ? { ...r, key: e.target.value }
                              : r
                          ),
                        })
                      }
                      placeholder="字段名"
                      className="w-28 shrink-0 rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                    />
                    <input
                      type="text"
                      value={row.value}
                      onChange={(e) =>
                        setEditor({
                          ...editor,
                          customAttrs: editor.customAttrs.map((r) =>
                            r.localKey === row.localKey
                              ? { ...r, value: e.target.value }
                              : r
                          ),
                        })
                      }
                      placeholder="属性值"
                      className="min-w-0 flex-1 rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                    />
                    <button
                      type="button"
                      onClick={() =>
                        setEditor({
                          ...editor,
                          customAttrs: editor.customAttrs.filter(
                            (r) => r.localKey !== row.localKey
                          ),
                        })
                      }
                      className="rounded p-1 text-ink-muted hover:bg-danger-soft hover:text-danger"
                    >
                      <X className="h-3.5 w-3.5" />
                    </button>
                  </div>
                ))}
              </div>
            </div>

            {/* Footer */}
            <div className="flex items-center justify-between border-t border-border-subtle bg-surface/60 px-5 py-3">
              {editor.id > 0 ? (
                confirmDelete ? (
                  <div className="flex items-center gap-1.5">
                    <button
                      type="button"
                      disabled={saving}
                      onClick={() => void handleDeleteEntity()}
                      className="rounded-md bg-danger px-2.5 py-1 text-xs font-medium text-white"
                    >
                      确认删除
                    </button>
                    <button
                      type="button"
                      onClick={() => setConfirmDelete(false)}
                      className="text-xs text-ink-secondary hover:text-ink-primary"
                    >
                      取消
                    </button>
                  </div>
                ) : (
                  <button
                    type="button"
                    onClick={() => setConfirmDelete(true)}
                    className="flex items-center gap-1 rounded-md px-2 py-1 text-xs text-ink-muted transition-colors hover:bg-danger-soft hover:text-danger"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                    <span>删除</span>
                  </button>
                )
              ) : (
                <div />
              )}

              <div className="flex items-center gap-2">
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
                  onClick={() => void handleSaveEntity()}
                  className="rounded-md bg-accent px-4 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
                >
                  {saving ? "保存中…" : "保存"}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
