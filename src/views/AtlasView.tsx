import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from "react";
import {
  AlertTriangle,
  ArrowRight,
  BookMarked,
  Clock,
  Copy,
  Flag,
  GitFork,
  History,
  Merge,
  Pencil,
  Plus,
  Radio,
  Search,
  Shield,
  ShieldAlert,
  ShieldCheck,
  Sparkles,
  Trash2,
  Users,
  X,
} from "lucide-react";
import {
  AtlasService,
  MemoryService,
  type Entity,
  type EntityKind,
  type EntityPresenceReport,
  type EntityPresenceStat,
  type EntitySnapshot,
  type EntityStateEvent,
  type GenreFieldSpec,
} from "../mygo";
import { useApp } from "../stores/AppContext";
import { useContextMenu } from "../components/ContextMenu";
import {
  ENTITY_KINDS,
  STATE_EVENT_STATUS_LABELS,
  formatErrorMessage,
  formatSeq,
  getEntityKindLabel,
  isForeshadowingOverdue,
  isIMEComposing,
  type AtlasSubTab,
} from "../lib/constants";
import { isEntityAbsentAlert } from "../lib/relationGraph";
import { AtlasTimelineBar } from "../components/atlas/AtlasTimelineBar";
import { ForeshadowingBoard } from "../components/atlas/ForeshadowingBoard";
import { ArcSummaryBoard } from "../components/atlas/ArcSummaryBoard";
import { StyleRulesBoard } from "../components/atlas/StyleRulesBoard";
import { RelationsBoard } from "../components/atlas/RelationsBoard";
import { ConsistencyAuditBoard } from "../components/atlas/ConsistencyAuditBoard";
import { EntitySafetyToolsModal } from "../components/atlas/EntitySafetyToolsModal";

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
    chapters,
    activeChapter,
    entities: allEntities,
    foreshadowings,
    atlasSubTab,
    setAtlasSubTab,
    atlasFocusEntityId,
    clearAtlasFocusEntity,
    memoryRevision,
    bumpMemoryRevision,
    refreshEntities,
    refreshBookData,
    refreshActiveChapter,
    jumpToChapter,
    notify,
  } = useApp();
  const { openContextMenu } = useContextMenu();

  const latestChapterSeq = useMemo(
    () => chapters.reduce((max, c) => (c.globalSeq > max ? c.globalSeq : max), 0),
    [chapters]
  );

  const overdueForeshadowCount = useMemo(
    () =>
      foreshadowings.filter((fs) => isForeshadowingOverdue(fs, latestChapterSeq))
        .length,
    [foreshadowings, latestChapterSeq]
  );

  const dirtyChapterCount = useMemo(
    () => chapters.filter((c) => c.status === "upstream_dirty").length,
    [chapters]
  );

  // Timeline state for 实体档案
  const [timelineSeq, setTimelineSeq] = useState<number>(latestChapterSeq);
  const [includePending, setIncludePending] = useState<boolean>(false);
  const userAdjustedTimelineRef = useRef(false);
  const lastBookIdRef = useRef<number | null>(activeBook?.id ?? null);

  // Reset timeline to latest chapter when book switches or chapters finish initial load
  useEffect(() => {
    const bookChanged = lastBookIdRef.current !== (activeBook?.id ?? null);
    if (bookChanged) {
      lastBookIdRef.current = activeBook?.id ?? null;
      userAdjustedTimelineRef.current = false;
      setTimelineSeq(latestChapterSeq);
      return;
    }
    if (!userAdjustedTimelineRef.current) {
      setTimelineSeq(latestChapterSeq);
    } else if (timelineSeq > latestChapterSeq) {
      setTimelineSeq(latestChapterSeq);
    }
  }, [activeBook?.id, latestChapterSeq, timelineSeq]);

  // Entity list, snapshot & presence radar state
  const [kindFilter, setKindFilter] = useState<string>("all");
  const [searchQuery, setSearchQuery] = useState("");
  const [snapshots, setSnapshots] = useState<EntitySnapshot[]>([]);
  const [searchMatchedIds, setSearchMatchedIds] = useState<Set<number> | null>(null);
  const [loading, setLoading] = useState(false);
  const snapReqRef = useRef(0);

  const [presenceReport, setPresenceReport] = useState<EntityPresenceReport | null>(null);
  const [absentThreshold, setAbsentThreshold] = useState<number>(15);
  const [absentOnly, setAbsentOnly] = useState<boolean>(false);
  const presenceReqRef = useRef(0);

  const handleTimelineSeqChange = useCallback((seq: number, isManual: boolean) => {
    userAdjustedTimelineRef.current = isManual;
    setTimelineSeq(seq);
  }, []);

  // Slide-over editor state
  const [editor, setEditor] = useState<EntityEditorState | null>(null);
  const [aliasDraft, setAliasDraft] = useState("");
  const [saving, setSaving] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  // Chronological state events for currently open entity
  const [entityEvents, setEntityEvents] = useState<EntityStateEvent[]>([]);
  const [eventsLoading, setEventsLoading] = useState(false);
  const [showAllEntityEvents, setShowAllEntityEvents] = useState(false);
  const [safetyToolsOpen, setSafetyToolsOpen] = useState(false);

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

  // Fetch timeline snapshots via MemoryService.snapshotAt
  const fetchSnapshots = useCallback(async () => {
    if (!activeBook) {
      setSnapshots([]);
      return;
    }
    const reqId = ++snapReqRef.current;
    setLoading(true);
    try {
      const snaps = await MemoryService.snapshotAt(
        activeBook.id,
        timelineSeq,
        includePending
      );
      if (snapReqRef.current !== reqId) return;
      setSnapshots(snaps);
    } catch {
      if (snapReqRef.current !== reqId) return;
      // Fallback cleanly to static entities if memory store isn't wired yet
      setSnapshots(
        allEntities.map((entity) => ({
          entity,
          effectiveAttributes: entity.attributes ?? {},
          appliedEventCount: 0,
          pendingEventCount: 0,
        }))
      );
    } finally {
      if (snapReqRef.current === reqId) setLoading(false);
    }
  }, [activeBook, timelineSeq, includePending, allEntities]);

  useEffect(() => {
    if (atlasSubTab !== "entities") return;
    const timer = window.setTimeout(() => {
      void fetchSnapshots();
    }, 100);
    return () => window.clearTimeout(timer);
  }, [atlasSubTab, fetchSnapshots, memoryRevision]);

  // Fetch entity presence & absence radar when entities or relations sub-tab is active
  const fetchPresence = useCallback(async () => {
    if (!activeBook) {
      setPresenceReport(null);
      return;
    }
    if (latestChapterSeq === 0) {
      setPresenceReport({
        bookId: activeBook.id,
        referenceSeq: 0,
        latestChapterSeq: 0,
        stats: [],
      });
      return;
    }
    const reqId = ++presenceReqRef.current;
    try {
      const rep = await AtlasService.getEntityPresence(activeBook.id, timelineSeq);
      if (presenceReqRef.current !== reqId) return;
      setPresenceReport(rep);
    } catch {
      if (presenceReqRef.current !== reqId) return;
      setPresenceReport(null);
    }
  }, [activeBook, latestChapterSeq, timelineSeq]);

  useEffect(() => {
    if (atlasSubTab !== "entities" && atlasSubTab !== "relations") return;
    const timer = window.setTimeout(() => {
      void fetchPresence();
    }, 100);
    return () => window.clearTimeout(timer);
  }, [atlasSubTab, fetchPresence, memoryRevision]);

  const presenceByEntityId = useMemo(() => {
    const map = new Map<number, EntityPresenceStat>();
    for (const stat of presenceReport?.stats ?? []) {
      map.set(stat.entityId, stat);
    }
    return map;
  }, [presenceReport]);

  const absentAlertCount = useMemo(() => {
    let count = 0;
    for (const ent of allEntities) {
      if (isEntityAbsentAlert(ent, presenceByEntityId.get(ent.id), absentThreshold)) {
        count++;
      }
    }
    return count;
  }, [allEntities, presenceByEntityId, absentThreshold]);

  // Backend search filter when searchQuery is non-empty
  useEffect(() => {
    if (atlasSubTab !== "entities" || !activeBook) return;
    const trimmed = searchQuery.trim();
    if (!trimmed) {
      setSearchMatchedIds(null);
      return;
    }
    let cancelled = false;
    const timer = window.setTimeout(async () => {
      try {
        const list = await AtlasService.listEntities(activeBook.id, {
          kind: kindFilter === "all" ? undefined : kindFilter,
          query: trimmed,
        });
        if (!cancelled) {
          setSearchMatchedIds(new Set(list.map((e) => e.id)));
        }
      } catch (err) {
        if (!cancelled) {
          notify({
            tone: "danger",
            message: "检索设定账本失败",
            detail: formatErrorMessage(err),
          });
        }
      }
    }, 150);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [atlasSubTab, activeBook, kindFilter, searchQuery, allEntities, notify]);

  const displayedSnapshots = useMemo(() => {
    return snapshots.filter((snap) => {
      if (kindFilter !== "all" && snap.entity.kind !== kindFilter) {
        return false;
      }
      if (searchMatchedIds !== null && !searchMatchedIds.has(snap.entity.id)) {
        return false;
      }
      if (
        absentOnly &&
        !isEntityAbsentAlert(
          snap.entity,
          presenceByEntityId.get(snap.entity.id),
          absentThreshold
        )
      ) {
        return false;
      }
      return true;
    });
  }, [
    snapshots,
    kindFilter,
    searchMatchedIds,
    absentOnly,
    presenceByEntityId,
    absentThreshold,
  ]);

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
    setEntityEvents([]);
    setShowAllEntityEvents(false);
  };

  const openEditEntity = useCallback(
    (ent: Entity) => {
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
      setShowAllEntityEvents(false);
    },
    [getSpecsForKind]
  );

  // Handle cross-view jump from MemorySearchModal
  useEffect(() => {
    if (!atlasFocusEntityId) return;
    const target = allEntities.find((e) => e.id === atlasFocusEntityId);
    if (target) {
      setAtlasSubTab("entities");
      openEditEntity(target);
      clearAtlasFocusEntity();
    }
  }, [atlasFocusEntityId, allEntities, setAtlasSubTab, openEditEntity, clearAtlasFocusEntity]);

  // Load chronological state events for the open entity
  useEffect(() => {
    if (!activeBook || !editor || editor.id <= 0) {
      setEntityEvents([]);
      return;
    }
    // Note: backend treats upToSeq=0 as "unlimited". When user sits at chapter 0
    // and hasn't asked to show all history, show 0 active events to stay spoiler-free.
    if (!showAllEntityEvents && timelineSeq === 0) {
      setEntityEvents([]);
      setEventsLoading(false);
      return;
    }
    let cancelled = false;
    setEventsLoading(true);
    const upToSeq = showAllEntityEvents ? 0 : timelineSeq;
    MemoryService.listStateEvents(activeBook.id, editor.id, upToSeq, includePending)
      .then((list) => {
        if (!cancelled) setEntityEvents(list);
      })
      .catch(() => {
        if (!cancelled) setEntityEvents([]);
      })
      .finally(() => {
        if (!cancelled) setEventsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [activeBook, editor?.id, timelineSeq, includePending, showAllEntityEvents, memoryRevision]);

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

  const handleToggleEntityInvariant = useCallback(
    async (ent: Entity) => {
      if (!activeBook) return;
      try {
        await AtlasService.upsertEntity({
          id: ent.id,
          bookId: activeBook.id,
          kind: ent.kind,
          name: ent.name,
          aliases: Array.isArray(ent.aliases) ? ent.aliases : [],
          isInvariant: !ent.isInvariant,
          summary: ent.summary,
          attributes: ent.attributes ?? {},
        });
        await refreshEntities(activeBook.id);
        notify({
          tone: "ok",
          message: !ent.isInvariant
            ? `已将「${ent.name}」设为世界铁律`
            : `已取消「${ent.name}」的世界铁律标记`,
        });
      } catch (err) {
        notify({ tone: "danger", message: "更新实体状态失败", detail: formatErrorMessage(err) });
      }
    },
    [activeBook, refreshEntities, notify]
  );

  const openEntityContextMenu = useCallback(
    (e: ReactMouseEvent, ent: Entity) => {
      openContextMenu(e, [
        {
          label: "编辑档案",
          icon: <Pencil className="h-3.5 w-3.5" />,
          onSelect: () => openEditEntity(ent),
        },
        {
          label: "复制名称",
          icon: <Copy className="h-3.5 w-3.5" />,
          onSelect: async () => {
            try {
              await navigator.clipboard.writeText(ent.name);
              notify({ tone: "ok", message: `已复制「${ent.name}」` });
            } catch {
              // ignore
            }
          },
        },
        {
          label: ent.isInvariant ? "取消世界铁律标记" : "设为世界铁律 (P0)",
          icon: <Shield className="h-3.5 w-3.5" />,
          onSelect: () => void handleToggleEntityInvariant(ent),
        },
        { type: "separator" },
        {
          label: "删除实体…",
          icon: <Trash2 className="h-3.5 w-3.5" />,
          danger: true,
          onSelect: () => {
            openEditEntity(ent);
            setConfirmDelete(true);
          },
        },
      ]);
    },
    [openContextMenu, openEditEntity, notify, handleToggleEntityInvariant]
  );

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

  const subTabs: Array<{
    id: AtlasSubTab;
    label: string;
    Icon: typeof Users;
    badge?: number;
    badgeTone?: "danger" | "muted";
    badgeText?: string;
  }> = [
    { id: "entities", label: "实体档案", Icon: Users, badge: allEntities.length, badgeTone: "muted" },
    {
      id: "relations",
      label: "关系网",
      Icon: GitFork,
    },
    {
      id: "foreshadowings",
      label: "伏笔看板",
      Icon: Flag,
      badge: overdueForeshadowCount > 0 ? overdueForeshadowCount : foreshadowings.length,
      badgeTone: overdueForeshadowCount > 0 ? "danger" : "muted",
      badgeText: overdueForeshadowCount > 0 ? `${overdueForeshadowCount} 超期` : undefined,
    },
    { id: "summaries", label: "弧线摘要", Icon: BookMarked },
    { id: "style", label: "文风与去AI味规则", Icon: Sparkles },
    {
      id: "audit",
      label: "全书体检",
      Icon: ShieldCheck,
      badge: dirtyChapterCount,
      badgeTone: dirtyChapterCount > 0 ? "danger" : "muted",
      badgeText: dirtyChapterCount > 0 ? `${dirtyChapterCount} 脏标` : undefined,
    },
  ];

  return (
    <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden bg-canvas">
      {/* Atlas Sub-Navigation Bar */}
      <div className="shrink-0 border-b border-border-subtle bg-surface px-6">
        <div className="mx-auto flex h-10 w-full max-w-[1160px] items-center justify-between gap-4">
          <div className="flex items-center gap-1">
            {subTabs.map(({ id, label, Icon, badge, badgeTone, badgeText }) => {
              const active = atlasSubTab === id;
              return (
                <button
                  key={id}
                  type="button"
                  onClick={() => setAtlasSubTab(id)}
                  className={`flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
                    active
                      ? "bg-accent-soft text-accent"
                      : "text-ink-secondary hover:bg-elevated/60 hover:text-ink-primary"
                  }`}
                >
                  <Icon className="h-3.5 w-3.5" />
                  <span>{label}</span>
                  {badge !== undefined && badge > 0 && (
                    <span
                      className={`nib-mono rounded-full px-1.5 py-0.5 text-[10px] ${
                        badgeTone === "danger"
                          ? "bg-danger-soft text-danger font-semibold"
                          : "bg-canvas/70 text-ink-muted"
                      }`}
                    >
                      {badgeText ?? badge}
                    </span>
                  )}
                </button>
              );
            })}
          </div>

          {(atlasSubTab === "entities" || atlasSubTab === "relations") && (
            <div className="hidden items-center gap-2 text-[11px] text-ink-muted sm:flex">
              <Clock className="h-3.5 w-3.5 text-accent" />
              <span>
                {timelineSeq === 0
                  ? "初始静态设定"
                  : `时间轴：截至第 ${formatSeq(timelineSeq)} 章`}
              </span>
            </div>
          )}
        </div>
      </div>

      {/* Sub-tab 1: 实体档案 (with Pinned Invariants + Chapter Timeline Slider) */}
      {atlasSubTab === "entities" && (
        <>
          {/* Pinned Top Strip: 世界铁律 (isInvariant: true) */}
          <section className="shrink-0 border-b border-border-subtle bg-surface/65 px-6 py-2.5">
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
                    onContextMenu={(e) => openEntityContextMenu(e, inv)}
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

          {/* Main Content */}
          <div className="flex-1 overflow-y-auto px-6 py-5">
            <div className="mx-auto flex w-full max-w-[1160px] flex-col gap-4">
              {/* Shared Chapter Timeline Slider Strip */}
              <AtlasTimelineBar
                timelineSeq={timelineSeq}
                latestChapterSeq={latestChapterSeq}
                chapters={chapters}
                includePending={includePending}
                onChangeTimelineSeq={handleTimelineSeqChange}
                onChangeIncludePending={setIncludePending}
              />

              {/* Compact Presence & Absence Radar Strip */}
              {latestChapterSeq > 0 && (
                <div className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-border-subtle bg-surface/75 px-3.5 py-2 text-xs">
                  <div className="flex flex-wrap items-center gap-2">
                    <Radio className="h-3.5 w-3.5 text-accent" />
                    <span className="font-medium text-ink-primary">出场与掉线雷达</span>
                    <span className="text-ink-muted">
                      （统计截至第 {formatSeq(timelineSeq)} 章的正文提及与节拍出场）
                    </span>
                    {absentAlertCount > 0 ? (
                      <span className="inline-flex items-center gap-1 rounded-full bg-danger-soft px-2 py-0.5 text-[11px] font-medium text-danger">
                        <AlertTriangle className="h-3 w-3" />
                        {absentAlertCount} 个非铁律实体已掉线 ≥ {absentThreshold} 章
                      </span>
                    ) : (
                      <span className="rounded-full bg-ok-soft px-2 py-0.5 text-[11px] text-ok">
                        当前无掉线超过 {absentThreshold} 章的已出场实体
                      </span>
                    )}
                  </div>

                  <div className="flex flex-wrap items-center gap-3">
                    <label className="flex items-center gap-1.5 text-ink-secondary">
                      <span>掉线阈值</span>
                      <select
                        value={absentThreshold}
                        onChange={(e) => setAbsentThreshold(Number(e.target.value))}
                        className="nib-mono rounded border border-border-subtle bg-canvas px-1.5 py-0.5 text-[11px] text-ink-primary focus:border-accent focus:outline-none"
                      >
                        {[10, 15, 20, 30].map((n) => (
                          <option key={n} value={n}>
                            {n} 章
                          </option>
                        ))}
                      </select>
                    </label>

                    <label className="flex cursor-pointer items-center gap-1.5 text-ink-secondary select-none">
                      <input
                        type="checkbox"
                        checked={absentOnly}
                        onChange={(e) => setAbsentOnly(e.target.checked)}
                        className="h-3.5 w-3.5 accent-[var(--accent)]"
                      />
                      <span>仅看掉线实体</span>
                    </label>
                  </div>
                </div>
              )}

              {/* Filter Toolbar */}
              <div className="flex flex-wrap items-center justify-between gap-3">
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
                    onClick={() => setSafetyToolsOpen(true)}
                    disabled={allEntities.length === 0}
                    title="合并重复实体或全书安全改名（带预览与撤销点）"
                    className="flex items-center gap-1.5 rounded-md border border-border-subtle bg-surface px-2.5 py-1.5 text-xs font-medium text-ink-secondary transition-colors hover:border-border-strong hover:text-ink-primary disabled:opacity-40"
                  >
                    <Merge className="h-3.5 w-3.5 text-accent" />
                    <span>合并 / 改名</span>
                  </button>

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

              {/* Snapshot Card Grid */}
              {loading && displayedSnapshots.length === 0 ? (
                <div className="py-16 text-center text-xs text-ink-muted">
                  正在折算时间轴快照…
                </div>
              ) : displayedSnapshots.length === 0 ? (
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
                <div className="grid grid-cols-1 gap-3.5 pb-8 sm:grid-cols-2 lg:grid-cols-3">
                  {displayedSnapshots.map((snap) => {
                    const ent = snap.entity;
                    const effectiveAttrs = snap.effectiveAttributes ?? ent.attributes ?? {};
                    const attrEntries = Object.entries(effectiveAttrs).slice(0, 4);
                    const presence = presenceByEntityId.get(ent.id);
                    const absentAlert = isEntityAbsentAlert(
                      ent,
                      presence,
                      absentThreshold
                    );
                    return (
                      <button
                        key={ent.id}
                        type="button"
                        onClick={() => openEditEntity(ent)}
                        onContextMenu={(e) => openEntityContextMenu(e, ent)}
                        className={`group flex flex-col rounded-xl border bg-surface p-4 text-left transition-colors hover:border-border-strong hover:bg-elevated/60 ${
                          absentAlert ? "border-danger/45" : "border-border-subtle"
                        }`}
                      >
                        <div className="flex items-start justify-between gap-2">
                          <div className="min-w-0 flex-1">
                            <div className="flex flex-wrap items-center gap-1.5">
                              <span className="truncate text-[14.5px] font-medium text-ink-primary">
                                {ent.name}
                              </span>
                              {ent.isInvariant && (
                                <span className="shrink-0 rounded bg-danger-soft px-1.5 py-0.5 text-[10px] font-medium text-danger">
                                  铁律
                                </span>
                              )}
                              {snap.appliedEventCount > 0 && (
                                <span className="nib-mono shrink-0 rounded bg-accent-soft px-1.5 py-0.5 text-[10px] text-accent">
                                  {snap.appliedEventCount} 演变
                                </span>
                              )}
                              {snap.pendingEventCount > 0 && (
                                <span className="nib-mono shrink-0 rounded bg-warn-soft px-1.5 py-0.5 text-[10px] text-warn">
                                  {snap.pendingEventCount} 待审
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
                              const changedFromBase = (ent.attributes?.[k] ?? "") !== v;
                              return (
                                <span
                                  key={k}
                                  className={`inline-flex max-w-full items-center gap-1 rounded px-1.5 py-0.5 text-[11px] ${
                                    changedFromBase
                                      ? "border border-accent/35 bg-accent-soft text-ink-primary"
                                      : spec?.highRisk
                                      ? "bg-accent-soft/60 text-ink-primary"
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

                        {presence && latestChapterSeq > 0 && timelineSeq > 0 && (
                          <div className="mt-2.5 flex flex-wrap items-center justify-between gap-1.5 border-t border-border-subtle/70 pt-2 text-[11px]">
                            {presence.neverAppeared ? (
                              <span className="text-ink-muted">截至此刻尚未出场</span>
                            ) : (
                              <>
                                <span className="nib-mono text-ink-secondary">
                                  出场 {presence.appearanceChapters} 章 · 末见第{" "}
                                  {formatSeq(presence.lastSeenSeq)} 章
                                </span>
                                {absentAlert && (
                                  <span className="nib-mono inline-flex items-center gap-1 rounded bg-danger-soft px-1.5 py-0.5 text-[10px] font-medium text-danger">
                                    已掉线 {presence.absentChapters} 章
                                  </span>
                                )}
                              </>
                            )}
                          </div>
                        )}
                      </button>
                    );
                  })}
                </div>
              )}
            </div>
          </div>
        </>
      )}

      {/* Sub-tab 1.5: 角色/势力关系网 */}
      {atlasSubTab === "relations" && (
        <RelationsBoard
          timelineSeq={timelineSeq}
          latestChapterSeq={latestChapterSeq}
          includePending={includePending}
          onChangeTimelineSeq={handleTimelineSeqChange}
          onChangeIncludePending={setIncludePending}
          presenceReport={presenceReport}
          absentThreshold={absentThreshold}
          onChangeAbsentThreshold={setAbsentThreshold}
        />
      )}

      {/* Sub-tab 2: 伏笔看板 */}
      {atlasSubTab === "foreshadowings" && (
        <ForeshadowingBoard latestChapterSeq={latestChapterSeq} />
      )}

      {/* Sub-tab 3: 弧线摘要 */}
      {atlasSubTab === "summaries" && (
        <ArcSummaryBoard latestChapterSeq={latestChapterSeq} />
      )}

      {/* Sub-tab 4: 文风与去AI味规则 */}
      {atlasSubTab === "style" && <StyleRulesBoard />}

      {/* Sub-tab 5: 全书双层一致性体检与脏标修复队列 */}
      {atlasSubTab === "audit" && <ConsistencyAuditBoard />}

      {/* Slide-Over Detail Editor for Entities (with Chronological State Events History) */}
      {editor && (
        <div
          className="fixed inset-0 z-40 flex justify-end bg-black/50"
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) setEditor(null);
          }}
        >
          <div className="flex h-full w-full max-w-[480px] flex-col border-l border-border-strong bg-elevated shadow-[var(--shadow-elevated)]">
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
                      if ((e.key === "Enter" || e.key === ",") && !isIMEComposing(e)) {
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

              {/* Genre-Preseeded Static Base Attributes */}
              {editorSpecs.length > 0 && (
                <div className="flex flex-col gap-2.5">
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-medium text-ink-secondary">
                      {activeGenrePreset?.name ?? "题材"}初始属性底座
                    </span>
                    <span className="text-[11px] text-ink-muted">
                      后续章节变化记录在状态事件账本中
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

              {/* Presence Radar Summary (for existing entities) */}
              {editor.id > 0 && latestChapterSeq > 0 && (() => {
                const ep = presenceByEntityId.get(editor.id);
                if (!ep) return null;
                const isAlert = isEntityAbsentAlert(
                  { isInvariant: editor.isInvariant },
                  ep,
                  absentThreshold
                );
                return (
                  <div className="flex flex-col gap-2 rounded-lg border border-border-subtle bg-canvas/70 p-3">
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex items-center gap-1.5 text-xs font-medium text-ink-primary">
                        <Radio className="h-3.5 w-3.5 text-accent" />
                        <span>出场雷达（截至第 {formatSeq(timelineSeq)} 章）</span>
                      </div>
                      {isAlert && (
                        <span className="nib-mono rounded bg-danger-soft px-1.5 py-0.5 text-[10px] font-medium text-danger">
                          已掉线 {ep.absentChapters} 章
                        </span>
                      )}
                    </div>
                    {ep.neverAppeared ? (
                      <div className="text-[11px] text-ink-muted">
                        截至当前时间点尚无正文或节拍出场记录
                      </div>
                    ) : (
                      <div className="flex flex-wrap items-center justify-between gap-2 text-[11px] text-ink-secondary">
                        <span className="nib-mono">
                          出场 {ep.appearanceChapters} 章 · 正文 {ep.mentionCount} 次 · 节拍{" "}
                          {ep.beatAppearances} 次 · 末见第 {formatSeq(ep.lastSeenSeq)} 章
                        </span>
                        {ep.lastSeenChapterId > 0 && (
                          <button
                            type="button"
                            onClick={() => {
                              setEditor(null);
                              void jumpToChapter(ep.lastSeenChapterId);
                            }}
                            className="text-accent hover:opacity-80"
                          >
                            跳转末见章
                          </button>
                        )}
                      </div>
                    )}
                  </div>
                );
              })()}

              {/* Chronological State Events Ledger (for existing entities) */}
              {editor.id > 0 && (
                <div className="flex flex-col gap-2 border-t border-border-subtle pt-4">
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex items-center gap-1.5 text-xs font-medium text-ink-primary">
                      <History className="h-3.5 w-3.5 text-accent" />
                      <span>状态演变流水</span>
                      <span className="nib-mono text-[11px] text-ink-muted">
                        ({entityEvents.length})
                      </span>
                    </div>
                    <button
                      type="button"
                      onClick={() => setShowAllEntityEvents((v) => !v)}
                      className="text-[11px] text-accent hover:opacity-80"
                    >
                      {showAllEntityEvents
                        ? `仅看截至第 ${formatSeq(timelineSeq)} 章`
                        : "查看全书演变"}
                    </button>
                  </div>

                  {eventsLoading ? (
                    <div className="py-4 text-center text-xs text-ink-muted">
                      正在读取状态事件…
                    </div>
                  ) : entityEvents.length === 0 ? (
                    <div className="rounded-lg border border-dashed border-border-subtle px-3 py-4 text-center text-[11.5px] text-ink-muted">
                      {!showAllEntityEvents && timelineSeq === 0
                        ? "当前处于第 0 章初始态，尚无章节变更事件"
                        : "截至当前时间点暂无状态演变记录"}
                    </div>
                  ) : (
                    <div className="flex flex-col gap-2">
                      {entityEvents.map((ev) => {
                        const isHighRisk = ev.changeSeverity === "high_risk";
                        const spec = fieldLabelMap.get(ev.attrKey);
                        return (
                          <div
                            key={ev.id}
                            className={`rounded-lg border px-3 py-2 text-xs ${
                              isHighRisk
                                ? "border-danger/35 bg-danger-soft/30"
                                : "border-border-subtle bg-canvas/70"
                            }`}
                          >
                            <div className="flex items-center justify-between gap-2">
                              <div className="flex items-center gap-1.5">
                                <span className="nib-mono rounded bg-surface px-1.5 py-0.5 text-[10.5px] text-accent">
                                  第 {formatSeq(ev.chapterSeq)} 章
                                </span>
                                <span className="font-medium text-ink-primary">
                                  {spec?.label ?? ev.attrKey}
                                </span>
                                {isHighRisk && (
                                  <span className="inline-flex items-center gap-0.5 rounded bg-danger-soft px-1 text-[10px] font-medium text-danger">
                                    <AlertTriangle className="h-2.5 w-2.5" />
                                    高危
                                  </span>
                                )}
                              </div>
                              <span
                                className={`rounded px-1.5 py-0.5 text-[10px] ${
                                  ev.status === "pending"
                                    ? "bg-warn-soft text-warn"
                                    : "bg-surface text-ink-muted"
                                }`}
                              >
                                {STATE_EVENT_STATUS_LABELS[ev.status] ?? ev.status}
                              </span>
                            </div>

                            <div className="mt-1.5 flex flex-wrap items-center gap-1.5 text-[11.5px]">
                              <span className="text-ink-muted line-through">
                                {ev.prevValue || "无"}
                              </span>
                              <ArrowRight className="h-3 w-3 shrink-0 text-ink-muted" />
                              <span className="font-medium text-ink-primary">
                                {ev.newValue || "清空"}
                              </span>
                            </div>

                            {ev.reason && (
                              <div className="mt-1 text-[11px] text-ink-secondary">
                                {ev.reason}
                              </div>
                            )}
                          </div>
                        );
                      })}
                    </div>
                  )}
                </div>
              )}
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

      {activeBook && (
        <EntitySafetyToolsModal
          open={safetyToolsOpen}
          bookId={activeBook.id}
          entities={allEntities}
          onClose={() => setSafetyToolsOpen(false)}
          onCompleted={async () => {
            setEditor(null);
            await refreshBookData(activeBook.id, activeChapter?.id ?? null);
            if (activeChapter?.id && activeChapter.id > 0) {
              await refreshActiveChapter(activeChapter.id);
            }
            bumpMemoryRevision();
          }}
          onNotify={notify}
        />
      )}
    </div>
  );
}
