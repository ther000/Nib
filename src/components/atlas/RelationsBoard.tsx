import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowRight,
  ChevronDown,
  ChevronUp,
  GitFork,
  List,
  Network,
  Pencil,
  Plus,
  Search,
  Trash2,
  X,
} from "lucide-react";
import {
  AtlasService,
  MemoryService,
  type EntityPresenceReport,
  type EntityRelation,
  type EntitySnapshot,
  type GenreFieldSpec,
} from "../../mygo";
import { useApp } from "../../stores/AppContext";
import {
  ENTITY_KINDS,
  formatErrorMessage,
  formatSeq,
  isIMEComposing,
} from "../../lib/constants";
import { buildRelationGraph, extractEntityFaction } from "../../lib/relationGraph";
import { ConfirmDialog, type ConfirmDialogState } from "../ui/ConfirmDialog";
import { RelationGraphCanvas } from "./RelationGraphCanvas";

const PRESET_RELATIONS = ["师徒", "死敌", "同盟", "隶属", "宿敌", "亲友", "道侣", "交易"];

export interface RelationsBoardProps {
  timelineSeq: number;
  latestChapterSeq: number;
  includePending: boolean;
  presenceReport: EntityPresenceReport | null;
  absentThreshold: number;
}

function formatRelationSpan(eff: number, end?: number): string {
  if (eff <= 0 && (end === undefined || end === null)) return "全程生效";
  const startText = eff > 0 ? `第 ${formatSeq(eff)} 章` : "初始";
  if (end !== undefined && end !== null) {
    return `${startText} – 第 ${formatSeq(end)} 章`;
  }
  return `${startText}起`;
}

function parseOptionalSeq(raw: string): { valid: boolean; value?: number } {
  const trimmed = raw.trim();
  if (!trimmed) return { valid: true, value: undefined };
  if (!/^\d+$/.test(trimmed)) return { valid: false };
  const n = Number.parseInt(trimmed, 10);
  if (!Number.isSafeInteger(n) || n < 0) return { valid: false };
  return { valid: true, value: n };
}

type RelationTimelineState = "active" | "ended" | "future";

function getRelationTimelineState(
  rel: EntityRelation,
  timelineSeq: number
): RelationTimelineState {
  if (rel.effectiveChapterSeq > timelineSeq) return "future";
  if (
    rel.endedChapterSeq !== undefined &&
    rel.endedChapterSeq !== null &&
    rel.endedChapterSeq <= timelineSeq
  ) {
    return "ended";
  }
  return "active";
}

export function RelationsBoard({
  timelineSeq,
  latestChapterSeq,
  includePending,
  presenceReport,
  absentThreshold,
}: RelationsBoardProps) {
  const {
    activeBook,
    activeGenrePreset,
    chapters,
    entities,
    memoryRevision,
    jumpToChapter,
    jumpToEntity,
    notify,
  } = useApp();

  const [relations, setRelations] = useState<EntityRelation[]>([]);
  const [activeRelations, setActiveRelations] = useState<EntityRelation[]>([]);
  const [snapshots, setSnapshots] = useState<EntitySnapshot[]>([]);
  const [loading, setLoading] = useState(false);

  // 视图与图谱交互状态
  const [viewMode, setViewMode] = useState<"graph" | "list">("graph");
  const userToggledViewRef = useRef(false);
  const lastBookIdRef = useRef<number | null>(activeBook?.id ?? null);
  const [addFormOpen, setAddFormOpen] = useState(false);

  const [kindFilter, setKindFilter] = useState<string>("all");
  const [factionFilter, setFactionFilter] = useState<string>("all");
  const [relationTypeFilter, setRelationTypeFilter] = useState<string>("all");
  const [listSearch, setListSearch] = useState<string>("");
  const [highlightHops, setHighlightHops] = useState<1 | 2>(1);
  const [showEndedEdges, setShowEndedEdges] = useState<boolean>(false);
  const [onlyConnected, setOnlyConnected] = useState<boolean>(true);
  const [selectedNodeId, setSelectedNodeId] = useState<number | null>(null);
  const [confirmDialog, setConfirmDialog] = useState<ConfirmDialogState | null>(null);
  const [confirmBusy, setConfirmBusy] = useState(false);

  // 新增关系表单状态
  const [sourceId, setSourceId] = useState<string>("");
  const [targetId, setTargetId] = useState<string>("");
  const [relType, setRelType] = useState("");
  const [description, setDescription] = useState("");
  const [effectiveSeq, setEffectiveSeq] = useState("");
  const [endedSeq, setEndedSeq] = useState("");
  const [saving, setSaving] = useState(false);

  // 行内编辑状态
  const [editingId, setEditingId] = useState<number | null>(null);
  const [editRelType, setEditRelType] = useState("");
  const [editDesc, setEditDesc] = useState("");
  const [editEffSeq, setEditEffSeq] = useState("");
  const [editEndSeq, setEditEndSeq] = useState("");
  const [savingEdit, setSavingEdit] = useState(false);

  const sliceReqRef = useRef(0);

  // 切书时重置筛选与视图偏好
  useEffect(() => {
    const nextBookId = activeBook?.id ?? null;
    if (lastBookIdRef.current !== nextBookId) {
      lastBookIdRef.current = nextBookId;
      userToggledViewRef.current = false;
      setKindFilter("all");
      setFactionFilter("all");
      setRelationTypeFilter("all");
      setListSearch("");
      setSelectedNodeId(null);
      setEditingId(null);
    }
  }, [activeBook?.id]);

  const loadAllRelations = useCallback(async () => {
    if (!activeBook) {
      setRelations([]);
      return;
    }
    setLoading(true);
    try {
      const list = await AtlasService.listEntityRelations(activeBook.id);
      setRelations(list);
      if (!userToggledViewRef.current) {
        setViewMode(list.length > 0 ? "graph" : "list");
      }
    } catch (err) {
      notify({
        tone: "danger",
        message: "加载关系网失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setLoading(false);
    }
  }, [activeBook, notify]);

  useEffect(() => {
    void loadAllRelations();
  }, [loadAllRelations, memoryRevision]);

  const loadTimelineSlice = useCallback(async () => {
    if (!activeBook) {
      setActiveRelations([]);
      setSnapshots([]);
      return;
    }
    const reqId = ++sliceReqRef.current;
    try {
      const [slicedRels, snaps] = await Promise.all([
        AtlasService.listEntityRelationsAt(activeBook.id, timelineSeq, includePending),
        MemoryService.snapshotAt(activeBook.id, timelineSeq, includePending),
      ]);
      if (sliceReqRef.current !== reqId) return;
      setActiveRelations(slicedRels);
      setSnapshots(snaps);
    } catch {
      if (sliceReqRef.current !== reqId) return;
      setActiveRelations([]);
    }
  }, [activeBook, timelineSeq, includePending]);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void loadTimelineSlice();
    }, 100);
    return () => window.clearTimeout(timer);
  }, [loadTimelineSlice, relations, memoryRevision]);

  const refreshAll = async () => {
    await Promise.all([loadAllRelations(), loadTimelineSlice()]);
  };

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
    const parsedEff = parseOptionalSeq(effectiveSeq);
    const parsedEnd = parseOptionalSeq(endedSeq);
    if (!parsedEff.valid || !parsedEnd.valid) {
      notify({ tone: "warn", message: "生效/结束章号必须为非负整数" });
      return;
    }
    const effVal = parsedEff.value ?? 0;
    if (parsedEnd.value !== undefined && parsedEnd.value < effVal) {
      notify({ tone: "warn", message: "结束章号不能早于生效章号" });
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
        effectiveChapterSeq: effVal,
        endedChapterSeq: parsedEnd.value,
      });
      setRelType("");
      setDescription("");
      setEffectiveSeq("");
      setEndedSeq("");
      await refreshAll();
      notify({ tone: "ok", message: "已添加关系" });
    } catch (err) {
      notify({
        tone: "danger",
        message: "保存关系失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setSaving(false);
    }
  };

  const openEditSpan = (rel: EntityRelation) => {
    setEditingId(rel.id);
    setEditRelType(rel.relationType);
    setEditDesc(rel.description);
    setEditEffSeq(rel.effectiveChapterSeq > 0 ? String(rel.effectiveChapterSeq) : "");
    setEditEndSeq(
      rel.endedChapterSeq !== undefined && rel.endedChapterSeq !== null
        ? String(rel.endedChapterSeq)
        : ""
    );
  };

  const handleSaveEdit = async (rel: EntityRelation) => {
    if (!editRelType.trim()) {
      notify({ tone: "warn", message: "关系类型不能为空" });
      return;
    }
    const parsedEff = parseOptionalSeq(editEffSeq);
    const parsedEnd = parseOptionalSeq(editEndSeq);
    if (!parsedEff.valid || !parsedEnd.valid) {
      notify({ tone: "warn", message: "生效/结束章号必须为非负整数" });
      return;
    }
    const effVal = parsedEff.value ?? 0;
    if (parsedEnd.value !== undefined && parsedEnd.value < effVal) {
      notify({ tone: "warn", message: "结束章号不能早于生效章号" });
      return;
    }
    setSavingEdit(true);
    try {
      await AtlasService.upsertEntityRelation({
        id: rel.id,
        bookId: rel.bookId,
        sourceEntityId: rel.sourceEntityId,
        targetEntityId: rel.targetEntityId,
        relationType: editRelType.trim(),
        description: editDesc.trim(),
        effectiveChapterSeq: effVal,
        endedChapterSeq: parsedEnd.value,
        status: rel.status,
      });
      setEditingId(null);
      await refreshAll();
      notify({ tone: "ok", message: "已更新关系阶段与描述" });
    } catch (err) {
      notify({
        tone: "danger",
        message: "更新关系失败",
        detail: formatErrorMessage(err),
      });
    } finally {
      setSavingEdit(false);
    }
  };

  const requestDelete = (rel: EntityRelation) => {
    setConfirmDialog({
      title: "删除关系",
      message: `确定删除「${rel.sourceName} — ${rel.relationType} → ${rel.targetName}」？删除后无法恢复。`,
      confirmLabel: "删除",
      danger: true,
      onConfirm: async () => {
        setConfirmBusy(true);
        try {
          await AtlasService.deleteEntityRelation(rel.id);
          setConfirmDialog(null);
          await refreshAll();
          notify({ tone: "ok", message: "已删除关系" });
        } catch (err) {
          notify({
            tone: "danger",
            message: "删除关系失败",
            detail: formatErrorMessage(err),
          });
        } finally {
          setConfirmBusy(false);
        }
      },
    });
  };

  const handleManageNodeRelations = useCallback(
    (entityId: number) => {
      const target = entities.find((e) => e.id === entityId);
      userToggledViewRef.current = true;
      setViewMode("list");
      setSourceId(String(entityId));
      setListSearch(target?.name ?? "");
    },
    [entities]
  );

  const connectableEntities = useMemo(
    () => entities.filter((e) => !e.isInvariant),
    [entities]
  );

  const fieldLabelMap = useMemo(() => {
    const map = new Map<string, GenreFieldSpec>();
    for (const spec of activeGenrePreset?.entityFields ?? []) {
      map.set(spec.key, spec);
    }
    return map;
  }, [activeGenrePreset]);

  const builtGraph = useMemo(
    () =>
      buildRelationGraph({
        entities,
        snapshots,
        presenceStats: presenceReport?.stats ?? [],
        allRelations: relations,
        activeRelations,
        timelineSeq,
        includePending,
        showEnded: showEndedEdges,
        kindFilter,
        factionFilter,
        relationTypeFilter,
        onlyConnected,
        absentThreshold,
      }),
    [
      entities,
      snapshots,
      presenceReport?.stats,
      relations,
      activeRelations,
      timelineSeq,
      includePending,
      showEndedEdges,
      kindFilter,
      factionFilter,
      relationTypeFilter,
      onlyConnected,
      absentThreshold,
    ]
  );

  // 当前选中节点如果被筛选条件移除，自动取消选中避免右侧残留空壳
  useEffect(() => {
    if (
      selectedNodeId !== null &&
      !builtGraph.nodes.some((n) => n.id === selectedNodeId)
    ) {
      setSelectedNodeId(null);
    }
  }, [builtGraph.nodes, selectedNodeId]);

  const absentAlertCount = useMemo(
    () => builtGraph.nodes.filter((n) => n.isAbsentAlert).length,
    [builtGraph.nodes]
  );

  const entityById = useMemo(() => {
    const map = new Map<number, (typeof entities)[number]>();
    for (const e of entities) map.set(e.id, e);
    return map;
  }, [entities]);

  const factionByEntityId = useMemo(() => {
    const map = new Map<number, string>();
    for (const snap of snapshots) {
      map.set(snap.entity.id, extractEntityFaction(snap));
    }
    return map;
  }, [snapshots]);

  const filteredListRelations = useMemo(() => {
    const q = listSearch.trim().toLowerCase();
    return relations.filter((rel) => {
      const stateAtTimeline = getRelationTimelineState(rel, timelineSeq);
      if (!showEndedEdges && stateAtTimeline === "ended") return false;
      if (relationTypeFilter !== "all" && rel.relationType !== relationTypeFilter) {
        return false;
      }
      if (kindFilter !== "all") {
        const sKind = entityById.get(rel.sourceEntityId)?.kind;
        const tKind = entityById.get(rel.targetEntityId)?.kind;
        if (sKind !== kindFilter && tKind !== kindFilter) return false;
      }
      if (factionFilter !== "all") {
        const sFac = factionByEntityId.get(rel.sourceEntityId);
        const tFac = factionByEntityId.get(rel.targetEntityId);
        if (sFac !== factionFilter && tFac !== factionFilter) return false;
      }
      if (q) {
        const haystack =
          `${rel.sourceName} ${rel.targetName} ${rel.relationType} ${rel.description}`.toLowerCase();
        if (!haystack.includes(q)) return false;
      }
      return true;
    });
  }, [
    relations,
    timelineSeq,
    showEndedEdges,
    relationTypeFilter,
    kindFilter,
    factionFilter,
    listSearch,
    entityById,
    factionByEntityId,
  ]);

  const quickAddSection = (
    <section className="flex flex-col gap-3 rounded-xl border border-border-subtle bg-surface p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2 text-sm font-medium text-ink-primary">
          <GitFork className="h-4 w-4 text-accent" />
          <span>新增角色与势力关系</span>
        </div>
        <span className="text-xs text-ink-muted">
          出场角色的关系会在写正文时自动带给 AI，避免阵营写乱
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

      <div className="grid grid-cols-1 items-end gap-2 sm:grid-cols-[1fr_112px_112px_auto]">
        <div>
          <label className="mb-1 block text-xs text-ink-secondary">原因或现状（可选）</label>
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
            placeholder="例如：第 12 章黑市夺丹结仇"
            className="w-full rounded-lg border border-border-subtle bg-canvas px-3 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
          />
        </div>
        <div>
          <label className="mb-1 block text-xs text-ink-secondary">生效章</label>
          <input
            type="number"
            min={0}
            value={effectiveSeq}
            onChange={(e) => setEffectiveSeq(e.target.value)}
            placeholder="0=全书"
            title="从第几章起生效（留空或 0 表示从开篇生效）"
            className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
          />
        </div>
        <div>
          <label className="mb-1 block text-xs text-ink-secondary">结束章</label>
          <input
            type="number"
            min={0}
            value={endedSeq}
            onChange={(e) => setEndedSeq(e.target.value)}
            placeholder="留空持续"
            title="到第几章结束（不含该章，留空表示持续生效）"
            className="nib-mono w-full rounded-lg border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
          />
        </div>
        <button
          type="button"
          disabled={saving || connectableEntities.length < 2}
          onClick={() => void handleAdd()}
          className="flex shrink-0 items-center justify-center gap-1.5 rounded-lg bg-accent px-3.5 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
        >
          <Plus className="h-3.5 w-3.5" />
          <span>添加关系</span>
        </button>
      </div>
    </section>
  );

  return (
    <div className="flex-1 overflow-y-auto px-6 py-5">
      <div className="mx-auto flex w-full max-w-[1160px] flex-col gap-4">
        {/* View Switcher & Compact Summary Toolbar */}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <div className="inline-flex rounded-lg border border-border-subtle bg-surface p-0.5">
              <button
                type="button"
                onClick={() => {
                  userToggledViewRef.current = true;
                  setViewMode("graph");
                }}
                className={`flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
                  viewMode === "graph"
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:text-ink-primary"
                }`}
              >
                <Network className="h-3.5 w-3.5" />
                <span>关系图谱</span>
              </button>
              <button
                type="button"
                onClick={() => {
                  userToggledViewRef.current = true;
                  setViewMode("list");
                }}
                className={`flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
                  viewMode === "list"
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:text-ink-primary"
                }`}
              >
                <List className="h-3.5 w-3.5" />
                <span>关系列表</span>
                <span className="nib-mono text-[11px] opacity-75">
                  ({relations.length})
                </span>
              </button>
            </div>

            <span className="text-xs text-ink-muted">
              当前生效 {activeRelations.length} 条 · 节点 {builtGraph.nodes.length}
              {absentAlertCount > 0 && (
                <span className="ml-1.5 text-danger">
                  · {absentAlertCount} 个角色久未出场
                </span>
              )}
            </span>
          </div>

          {viewMode === "graph" && (
            <button
              type="button"
              onClick={() => setAddFormOpen((v) => !v)}
              className="flex items-center gap-1.5 rounded-md border border-border-subtle bg-surface px-3 py-1.5 text-xs font-medium text-ink-primary transition-colors hover:border-border-strong"
            >
              <Plus className="h-3.5 w-3.5 text-accent" />
              <span>新增关系</span>
              {addFormOpen ? (
                <ChevronUp className="h-3.5 w-3.5 text-ink-muted" />
              ) : (
                <ChevronDown className="h-3.5 w-3.5 text-ink-muted" />
              )}
            </button>
          )}
        </div>

        {/* Shared Filters Strip */}
        <div className="flex flex-wrap items-center gap-2 rounded-xl border border-border-subtle bg-surface px-3.5 py-2.5 text-xs">
          <select
            value={kindFilter}
            onChange={(e) => setKindFilter(e.target.value)}
            className="rounded-md border border-border-subtle bg-canvas px-2 py-1 text-xs text-ink-primary focus:border-accent focus:outline-none"
          >
            <option value="all">全部分类</option>
            {ENTITY_KINDS.filter((k) => k.value !== "rule").map((k) => (
              <option key={k.value} value={k.value}>
                {k.label}
              </option>
            ))}
          </select>

          <select
            value={factionFilter}
            onChange={(e) => setFactionFilter(e.target.value)}
            className="rounded-md border border-border-subtle bg-canvas px-2 py-1 text-xs text-ink-primary focus:border-accent focus:outline-none"
          >
            <option value="all">全部势力/阵营</option>
            {builtGraph.availableFactions.map((fac) => (
              <option key={fac} value={fac}>
                {fac}
              </option>
            ))}
          </select>

          <select
            value={relationTypeFilter}
            onChange={(e) => setRelationTypeFilter(e.target.value)}
            className="rounded-md border border-border-subtle bg-canvas px-2 py-1 text-xs text-ink-primary focus:border-accent focus:outline-none"
          >
            <option value="all">全部关系类型</option>
            {builtGraph.availableRelationTypes.map((rt) => (
              <option key={rt} value={rt}>
                {rt}
              </option>
            ))}
          </select>

          {viewMode === "graph" && (
            <div className="inline-flex items-center rounded-md border border-border-subtle bg-canvas p-0.5">
              {(
                [
                  [1, "直接关系"],
                  [2, "二度关系"],
                ] as const
              ).map(([h, label]) => (
                <button
                  key={h}
                  type="button"
                  onClick={() => setHighlightHops(h)}
                  className={`rounded px-2 py-0.5 text-xs transition-colors ${
                    highlightHops === h
                      ? "bg-accent-soft font-medium text-accent"
                      : "text-ink-muted hover:text-ink-primary"
                  }`}
                >
                  {label}
                </button>
              ))}
            </div>
          )}

          <label className="flex cursor-pointer items-center gap-1.5 text-ink-secondary select-none">
            <input
              type="checkbox"
              checked={showEndedEdges}
              onChange={(e) => setShowEndedEdges(e.target.checked)}
              className="h-3.5 w-3.5 accent-[var(--accent)]"
            />
            <span>含历史关系</span>
          </label>

          {viewMode === "graph" ? (
            <label className="flex cursor-pointer items-center gap-1.5 text-ink-secondary select-none">
              <input
                type="checkbox"
                checked={!onlyConnected}
                onChange={(e) => setOnlyConnected(!e.target.checked)}
                className="h-3.5 w-3.5 accent-[var(--accent)]"
              />
              <span>显示无连线设定</span>
            </label>
          ) : (
            <div className="relative ml-auto min-w-[180px]">
              <Search className="pointer-events-none absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2 text-ink-muted" />
              <input
                type="text"
                value={listSearch}
                onChange={(e) => setListSearch(e.target.value)}
                placeholder="搜索角色、关系或备注…"
                className="w-full rounded-md border border-border-subtle bg-canvas py-1 pr-7 pl-8 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
              />
              {listSearch && (
                <button
                  type="button"
                  onClick={() => setListSearch("")}
                  aria-label="清空搜索"
                  className="absolute top-1/2 right-2 -translate-y-1/2 text-ink-muted hover:text-ink-primary"
                >
                  <X className="h-3 w-3" />
                </button>
              )}
            </div>
          )}
        </div>

        {/* Graph Mode */}
        {viewMode === "graph" && (
          <>
            {addFormOpen && quickAddSection}

            <RelationGraphCanvas
              nodes={builtGraph.nodes}
              edges={builtGraph.edges}
              chapters={chapters}
              fieldLabelMap={fieldLabelMap}
              selectedNodeId={selectedNodeId}
              onSelectNode={setSelectedNodeId}
              highlightHops={highlightHops}
              timelineSeq={timelineSeq}
              onJumpToEntity={jumpToEntity}
              onJumpToChapter={(chId) => void jumpToChapter(chId)}
              onManageRelations={handleManageNodeRelations}
            />
          </>
        )}

        {/* List Mode */}
        {viewMode === "list" && (
          <>
            {quickAddSection}

            {loading && relations.length === 0 ? (
              <div className="py-16 text-center text-xs text-ink-muted">
                正在加载关系网…
              </div>
            ) : relations.length === 0 ? (
              <div className="rounded-xl border border-dashed border-border-strong bg-surface/35 py-16 text-center text-xs text-ink-secondary">
                暂无记录的角色或势力关系（也可直接让 AI 导演帮你梳理并记录全书关系）
              </div>
            ) : filteredListRelations.length === 0 ? (
              <div className="rounded-xl border border-dashed border-border-strong bg-surface/35 py-12 text-center text-xs text-ink-secondary">
                当前筛选条件下暂无匹配的关系，可放宽上方筛选条件或勾选「含历史关系」
              </div>
            ) : (
              <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                {filteredListRelations.map((rel) => {
                  const isEditing = editingId === rel.id;
                  const stateAtTimeline = getRelationTimelineState(rel, timelineSeq);
                  const isPending = rel.status === "pending";
                  const spanText = formatRelationSpan(
                    rel.effectiveChapterSeq ?? 0,
                    rel.endedChapterSeq
                  );

                  return (
                    <div
                      key={rel.id}
                      className={`flex flex-col gap-2.5 rounded-xl border border-border-subtle bg-surface p-4 ${
                        stateAtTimeline === "ended" || stateAtTimeline === "future"
                          ? "opacity-70"
                          : ""
                      }`}
                    >
                      <div className="flex items-start justify-between gap-3">
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-2 text-sm font-medium text-ink-primary">
                            <span>{rel.sourceName}</span>
                            <span className="inline-flex items-center gap-1 rounded-full border border-accent/35 bg-accent-soft px-2 py-0.5 text-xs font-medium text-accent">
                              <span>{rel.relationType}</span>
                              <ArrowRight className="h-3 w-3" />
                            </span>
                            <span>{rel.targetName}</span>
                            <span
                              className={`nib-mono rounded px-1.5 py-0.5 text-[11px] ${
                                stateAtTimeline === "ended"
                                  ? "bg-canvas text-ink-muted"
                                  : "border border-border-subtle bg-canvas/80 text-ink-secondary"
                              }`}
                            >
                              {spanText}
                            </span>
                            <span
                              className={`rounded px-1.5 py-0.5 text-[11px] ${
                                stateAtTimeline === "active"
                                  ? "bg-ok-soft text-ok"
                                  : stateAtTimeline === "ended"
                                  ? "bg-canvas text-ink-muted"
                                  : "bg-elevated text-ink-muted"
                              }`}
                            >
                              {stateAtTimeline === "active"
                                ? "当前生效"
                                : stateAtTimeline === "ended"
                                ? "已结束"
                                : "尚未生效"}
                            </span>
                            {isPending && (
                              <span className="rounded bg-warn-soft px-1.5 py-0.5 text-[11px] text-warn">
                                待审
                              </span>
                            )}
                          </div>
                          {rel.description && !isEditing && (
                            <p className="mt-1.5 text-xs leading-relaxed text-ink-secondary">
                              {rel.description}
                            </p>
                          )}
                        </div>
                        <div className="flex shrink-0 items-center gap-0.5">
                          <button
                            type="button"
                            onClick={() =>
                              isEditing ? setEditingId(null) : openEditSpan(rel)
                            }
                            title="调整生效章节区间或说明"
                            className="rounded p-1 text-ink-muted transition-colors hover:bg-elevated hover:text-ink-primary"
                          >
                            <Pencil className="h-3.5 w-3.5" />
                          </button>
                          <button
                            type="button"
                            onClick={() => requestDelete(rel)}
                            title="删除此关系"
                            className="rounded p-1 text-ink-muted transition-colors hover:bg-elevated hover:text-danger"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </button>
                        </div>
                      </div>

                      {isEditing && (
                        <div className="flex flex-col gap-2 rounded-lg border border-border-subtle bg-canvas p-2.5">
                          <div className="grid grid-cols-3 gap-2">
                            <div>
                              <label className="mb-0.5 block text-[11px] text-ink-muted">关系类型</label>
                              <input
                                type="text"
                                value={editRelType}
                                onChange={(e) => setEditRelType(e.target.value)}
                                placeholder="关系类型"
                                className="w-full rounded border border-border-subtle bg-surface px-2 py-1 text-xs text-ink-primary focus:border-accent focus:outline-none"
                              />
                            </div>
                            <div>
                              <label className="mb-0.5 block text-[11px] text-ink-muted">生效章</label>
                              <input
                                type="number"
                                min={0}
                                value={editEffSeq}
                                onChange={(e) => setEditEffSeq(e.target.value)}
                                placeholder="0=全书"
                                className="nib-mono w-full rounded border border-border-subtle bg-surface px-2 py-1 text-xs text-ink-primary focus:border-accent focus:outline-none"
                              />
                            </div>
                            <div>
                              <label className="mb-0.5 block text-[11px] text-ink-muted">结束章</label>
                              <input
                                type="number"
                                min={0}
                                value={editEndSeq}
                                onChange={(e) => setEditEndSeq(e.target.value)}
                                placeholder="留空持续"
                                className="nib-mono w-full rounded border border-border-subtle bg-surface px-2 py-1 text-xs text-ink-primary focus:border-accent focus:outline-none"
                              />
                            </div>
                          </div>
                          <input
                            type="text"
                            value={editDesc}
                            onChange={(e) => setEditDesc(e.target.value)}
                            placeholder="补充原因或阶段说明"
                            className="rounded border border-border-subtle bg-surface px-2 py-1 text-xs text-ink-primary focus:border-accent focus:outline-none"
                          />
                          <div className="flex flex-wrap items-center justify-between gap-2">
                            {latestChapterSeq > 0 ? (
                              <button
                                type="button"
                                onClick={() => setEditEndSeq(String(latestChapterSeq))}
                                className="rounded border border-border-subtle bg-surface px-2 py-0.5 text-[11px] text-ink-secondary hover:border-border-strong hover:text-ink-primary"
                              >
                                截至第 {latestChapterSeq} 章结束
                              </button>
                            ) : (
                              <div />
                            )}
                            <div className="flex items-center gap-1.5">
                              <button
                                type="button"
                                onClick={() => setEditingId(null)}
                                className="rounded px-2 py-1 text-xs text-ink-secondary hover:bg-surface"
                              >
                                取消
                              </button>
                              <button
                                type="button"
                                disabled={savingEdit}
                                onClick={() => void handleSaveEdit(rel)}
                                className="rounded bg-accent px-2.5 py-1 text-xs font-semibold text-canvas disabled:opacity-50"
                              >
                                {savingEdit ? "保存中…" : "保存"}
                              </button>
                            </div>
                          </div>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </>
        )}
      </div>

      <ConfirmDialog
        dialog={confirmDialog}
        busy={confirmBusy}
        onClose={() => setConfirmDialog(null)}
      />
    </div>
  );
}
