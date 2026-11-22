import { useEffect, useMemo, useState } from "react";
import {
  AlertTriangle,
  BookMarked,
  BookOpen,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Flag,
  ListChecks,
  Loader2,
  PanelLeftClose,
  Plus,
  RefreshCw,
  Search,
  Shield,
} from "lucide-react";
import {
  LibraryService,
  type Beat,
  type Chapter,
  type CriticFinding,
  type Entity,
  type EntitySnapshot,
  type Foreshadowing,
} from "../../mygo";
import {
  countWords,
  formatErrorMessage,
  formatNumber,
  formatSeq,
  getBeatStageMeta,
  getEntityKindLabel,
  getForeshadowingStatusMeta,
  getHookMeta,
  isForeshadowingApproaching,
  isForeshadowingOverdue,
  splitParagraphs,
} from "../../lib/constants";
import { alignBeatsToParagraphs, matchChapterEntities } from "../../lib/studioHelpers";

export type ReferenceTab = "history" | "beats" | "entities";

interface StudioReferenceSidebarProps {
  activeChapter: Chapter;
  chapters: Chapter[];
  currentContent: string;
  readingMode: boolean;
  generating: boolean;
  beats: Beat[];
  beatsLoading: boolean;
  entities: Entity[];
  snapshots: EntitySnapshot[];
  snapshotsLoading: boolean;
  includePending: boolean;
  foreshadowings: Foreshadowing[];
  criticFindings: CriticFinding[];
  activeTab: ReferenceTab;
  focusedEntityId: number | null;
  onChangeTab: (tab: ReferenceTab) => void;
  onChangeIncludePending: (next: boolean) => void;
  onRefreshSnapshots: () => void;
  onClose: () => void;
  onSwitchChapter: (chapterId: number) => void;
  onFocusParagraph: (paragraphIndex: number) => void;
  onInsertEntityName: (name: string) => void;
  onNotify: (input: {
    tone: "info" | "ok" | "warn" | "danger";
    message: string;
    detail?: string;
  }) => void;
}

export function StudioReferenceSidebar({
  activeChapter,
  chapters,
  currentContent,
  readingMode,
  generating,
  beats,
  beatsLoading,
  entities,
  snapshots,
  snapshotsLoading,
  includePending,
  foreshadowings,
  criticFindings,
  activeTab,
  focusedEntityId,
  onChangeTab,
  onChangeIncludePending,
  onRefreshSnapshots,
  onClose,
  onSwitchChapter,
  onFocusParagraph,
  onInsertEntityName,
  onNotify,
}: StudioReferenceSidebarProps) {
  // 1. 历史章对照状态与缓存
  const [refChapterId, setRefChapterId] = useState<number>(0);
  const [refCache, setRefCache] = useState<Record<number, Chapter>>({});
  const [refLoading, setRefLoading] = useState(false);

  const otherChapters = useMemo(
    () => chapters.filter((c) => c.id !== activeChapter.id),
    [chapters, activeChapter.id]
  );

  // 切章时默认对准上一章
  useEffect(() => {
    const idx = chapters.findIndex((c) => c.id === activeChapter.id);
    if (idx > 0) {
      setRefChapterId(chapters[idx - 1]!.id);
    } else if (otherChapters.length > 0) {
      setRefChapterId(otherChapters[0]!.id);
    } else {
      setRefChapterId(0);
    }
  }, [activeChapter.id, chapters, otherChapters]);

  // 按需拉取参照章节全文并本地缓存
  useEffect(() => {
    if (activeTab !== "history" || refChapterId <= 0) return;
    if (refCache[refChapterId]) return;
    let cancelled = false;
    setRefLoading(true);
    LibraryService.getChapter(refChapterId)
      .then((ch) => {
        if (!cancelled) {
          setRefCache((prev) => ({ ...prev, [ch.id]: ch }));
        }
      })
      .catch((err) => {
        if (!cancelled) {
          onNotify({
            tone: "danger",
            message: "加载参照章节失败",
            detail: formatErrorMessage(err),
          });
        }
      })
      .finally(() => {
        if (!cancelled) setRefLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [activeTab, refChapterId, refCache, onNotify]);

  const refChapter = refChapterId > 0 ? refCache[refChapterId] ?? null : null;
  const refParagraphs = useMemo(
    () => (refChapter ? splitParagraphs(refChapter.content) : []),
    [refChapter]
  );

  // 2. 本章节拍本地进度对齐
  const currentParagraphs = useMemo(
    () => splitParagraphs(currentContent),
    [currentContent]
  );
  const beatAlignments = useMemo(
    () =>
      alignBeatsToParagraphs(
        beats,
        currentParagraphs,
        entities,
        criticFindings
      ),
    [beats, currentParagraphs, entities, criticFindings]
  );

  // 3. 实体与伏笔速查
  const [entityQuery, setEntityQuery] = useState("");
  const [expandedEntityIds, setExpandedEntityIds] = useState<Set<number>>(
    new Set()
  );

  useEffect(() => {
    if (focusedEntityId && focusedEntityId > 0) {
      setExpandedEntityIds((prev) => {
        const next = new Set(prev);
        next.add(focusedEntityId);
        return next;
      });
    }
  }, [focusedEntityId]);

  const matchedEntities = useMemo(
    () => matchChapterEntities(currentContent, beats, snapshots),
    [currentContent, beats, snapshots]
  );

  const filteredEntities = useMemo(() => {
    const q = entityQuery.trim().toLowerCase();
    if (!q) return matchedEntities;
    return matchedEntities.filter((item) => {
      const e = item.snapshot.entity;
      if (e.name.toLowerCase().includes(q)) return true;
      if (e.summary.toLowerCase().includes(q)) return true;
      return (e.aliases ?? []).some((a) => a.toLowerCase().includes(q));
    });
  }, [matchedEntities, entityQuery]);

  const activeMentionedCount = useMemo(
    () =>
      matchedEntities.filter((item) => item.mentionCount > 0 || item.inBeats)
        .length,
    [matchedEntities]
  );

  const relevantForeshadowings = useMemo(() => {
    const mentionedIds = new Set(
      matchedEntities
        .filter((item) => item.mentionCount > 0 || item.inBeats)
        .map((item) => item.snapshot.entity.id)
    );
    const activeFS = foreshadowings.filter(
      (fs) => fs.status !== "resolved" && fs.status !== "abandoned"
    );
    return activeFS
      .map((fs) => {
        const overdue = isForeshadowingOverdue(fs, activeChapter.globalSeq);
        const dueSoon = isForeshadowingApproaching(fs, activeChapter.globalSeq);
        const linked = (fs.involvedEntityIds ?? []).some((id) =>
          mentionedIds.has(id)
        );
        return { fs, overdue, dueSoon, linked };
      })
      .filter((x) => x.overdue || x.dueSoon || x.linked)
      .sort((a, b) => {
        if (a.overdue !== b.overdue) return a.overdue ? -1 : 1;
        if (a.dueSoon !== b.dueSoon) return a.dueSoon ? -1 : 1;
        if (a.linked !== b.linked) return a.linked ? -1 : 1;
        return a.fs.plantedChapterSeq - b.fs.plantedChapterSeq;
      });
  }, [foreshadowings, matchedEntities, activeChapter.globalSeq]);

  const toggleEntityExpand = (id: number) => {
    setExpandedEntityIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  return (
    <aside className="flex w-[320px] shrink-0 flex-col border-r border-border-subtle bg-surface/65">
      {/* Header Tabs */}
      <div className="flex h-10 shrink-0 items-center justify-between border-b border-border-subtle px-2.5">
        <div className="flex items-center gap-1">
          {(
            [
              { id: "history", label: "历史章", icon: BookOpen },
              { id: "beats", label: `节拍 (${beats.length})`, icon: ListChecks },
              {
                id: "entities",
                label: `速查 (${activeMentionedCount})`,
                icon: BookMarked,
              },
            ] as const
          ).map((tab) => {
            const Icon = tab.icon;
            const active = activeTab === tab.id;
            return (
              <button
                key={tab.id}
                type="button"
                onClick={() => onChangeTab(tab.id)}
                className={`flex items-center gap-1 rounded-md px-2 py-1 text-[11.5px] font-medium transition-colors ${
                  active
                    ? "bg-accent-soft text-accent"
                    : "text-ink-secondary hover:bg-elevated hover:text-ink-primary"
                }`}
              >
                <Icon className="h-3 w-3" />
                <span>{tab.label}</span>
              </button>
            );
          })}
        </div>

        <button
          type="button"
          onClick={onClose}
          title="收起左侧参照栏"
          className="rounded p-1 text-ink-muted transition-colors hover:bg-elevated hover:text-ink-primary"
        >
          <PanelLeftClose className="h-3.5 w-3.5" />
        </button>
      </div>

      {/* Tab 1: 历史章对照 */}
      {activeTab === "history" && (
        <div className="flex min-h-0 flex-1 flex-col">
          <div className="flex shrink-0 flex-col gap-2 border-b border-border-subtle bg-canvas/45 px-3 py-2.5">
            {otherChapters.length === 0 ? (
              <div className="text-xs text-ink-muted">
                本书暂无其他章节可对照。
              </div>
            ) : (
              <>
                <div className="flex items-center gap-1.5">
                  <select
                    value={refChapterId}
                    onChange={(e) => setRefChapterId(Number(e.target.value))}
                    className="min-w-0 flex-1 rounded-md border border-border-subtle bg-canvas px-2 py-1 text-xs text-ink-primary focus:border-accent focus:outline-none"
                  >
                    {otherChapters.map((ch) => (
                      <option key={ch.id} value={ch.id}>
                        第 {formatSeq(ch.globalSeq)} 章 · {ch.title}
                      </option>
                    ))}
                  </select>
                  {refChapter && (
                    <button
                      type="button"
                      disabled={generating}
                      onClick={() => onSwitchChapter(refChapter.id)}
                      title="跳转到此章进行编辑"
                      className="shrink-0 rounded-md border border-border-subtle bg-elevated px-2 py-1 text-[11px] text-ink-secondary transition-colors hover:border-accent/50 hover:text-accent disabled:opacity-40"
                    >
                      编辑此章
                    </button>
                  )}
                </div>

                {refChapter && (
                  <div className="flex flex-wrap items-center gap-2 text-[11px] text-ink-muted">
                    <span className="nib-mono">
                      {formatNumber(countWords(refChapter.content))} 字
                    </span>
                    {refChapter.hookType && (
                      <span className="rounded bg-surface px-1.5 py-0.5 text-[10px] text-ink-secondary">
                        钩子：{getHookMeta(refChapter.hookType).label}
                      </span>
                    )}
                  </div>
                )}
                {refChapter?.hookNote && (
                  <div className="rounded bg-surface/75 px-2 py-1 text-[11px] leading-relaxed text-ink-secondary">
                    {refChapter.hookNote}
                  </div>
                )}
              </>
            )}
          </div>

          <div className="flex-1 overflow-y-auto px-3.5 py-3">
            {refLoading ? (
              <div className="flex items-center justify-center gap-2 py-12 text-xs text-ink-muted">
                <Loader2 className="h-3.5 w-3.5 animate-spin text-accent" />
                <span>加载章节正文中…</span>
              </div>
            ) : !refChapter ? null : refParagraphs.length === 0 ? (
              <div className="py-10 text-center text-xs text-ink-muted">
                该章节尚无正文内容。
              </div>
            ) : (
              <div className="flex flex-col gap-3 font-prose text-[13.5px] leading-[1.85] text-ink-primary/90">
                {refParagraphs.map((para, idx) => (
                  <p key={idx} className="m-0 indent-[2em]">
                    {para}
                  </p>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Tab 2: 本章节拍进度对照 */}
      {activeTab === "beats" && (
        <div className="flex min-h-0 flex-1 flex-col">
          <div className="shrink-0 border-b border-border-subtle bg-canvas/45 px-3 py-2 text-[11px] text-ink-muted">
            0 成本本地按出场实体与关键词估算每拍在正文的覆盖段位；点击段号可定位核验。
          </div>

          <div className="flex-1 overflow-y-auto px-3 py-3">
            {beatsLoading ? (
              <div className="flex items-center justify-center gap-2 py-12 text-xs text-ink-muted">
                <Loader2 className="h-3.5 w-3.5 animate-spin text-accent" />
                <span>读取节拍卡…</span>
              </div>
            ) : beatAlignments.length === 0 ? (
              <div className="py-10 text-center text-xs text-ink-muted">
                本章暂未编排节拍卡，可先在编排台添加。
              </div>
            ) : (
              <div className="flex flex-col gap-2.5">
                {beatAlignments.map((item) => {
                  const statusBadge =
                    item.status === "covered"
                      ? {
                          label:
                            item.startPara !== null && item.endPara !== null
                              ? item.startPara === item.endPara
                                ? `P${item.startPara + 1}`
                                : `P${item.startPara + 1}–P${item.endPara + 1}`
                              : "已覆盖",
                          cls: "border-ok/35 bg-ok-soft text-ok",
                        }
                      : item.status === "weak"
                      ? {
                          label:
                            item.startPara !== null
                              ? `偏弱 P${item.startPara + 1}`
                              : "覆盖偏弱",
                          cls: "border-warn/35 bg-warn-soft text-warn",
                        }
                      : {
                          label: "待补写",
                          cls: "border-danger/35 bg-danger-soft text-danger",
                        };

                  return (
                    <div
                      key={item.beat.id || item.beatIndex}
                      className="rounded-lg border border-border-subtle bg-canvas/75 p-2.5 text-xs"
                    >
                      <div className="flex items-center justify-between gap-1.5">
                        <div className="flex items-center gap-1.5">
                          <span className="nib-mono rounded bg-surface px-1.5 py-0.5 text-[10px] font-semibold text-ink-secondary">
                            #{item.beatIndex + 1}
                          </span>
                          <span className="text-[11px] font-medium text-accent">
                            {getBeatStageMeta(item.beat.stage).label}
                          </span>
                        </div>

                        {item.startPara !== null ? (
                          <button
                            type="button"
                            onClick={() => onFocusParagraph(item.startPara!)}
                            title="点击定位到正文对应段落"
                            className={`nib-mono flex items-center gap-1 rounded border px-1.5 py-0.5 text-[10px] font-medium transition-opacity hover:opacity-80 ${statusBadge.cls}`}
                          >
                            <CheckCircle2 className="h-2.5 w-2.5" />
                            <span>{statusBadge.label}</span>
                          </button>
                        ) : (
                          <span
                            className={`rounded border px-1.5 py-0.5 text-[10px] font-medium ${statusBadge.cls}`}
                          >
                            {statusBadge.label}
                          </span>
                        )}
                      </div>

                      {item.beat.summary && (
                        <div className="mt-1.5 text-[11.5px] leading-relaxed text-ink-primary">
                          {item.beat.summary}
                        </div>
                      )}
                      {item.beat.conflictNote && (
                        <div className="mt-1 text-[11px] text-ink-muted">
                          冲突：{item.beat.conflictNote}
                        </div>
                      )}
                      {item.matchedTerms.length > 0 && (
                        <div className="mt-1.5 flex flex-wrap gap-1">
                          {item.matchedTerms.map((term) => (
                            <span
                              key={term}
                              className="rounded bg-surface px-1.5 py-0.5 text-[10px] text-ink-secondary"
                            >
                              {term}
                            </span>
                          ))}
                        </div>
                      )}
                      {item.criticHint && (
                        <div className="mt-1.5 flex items-start gap-1 rounded bg-warn-soft/50 px-2 py-1 text-[10.5px] text-warn">
                          <AlertTriangle className="mt-0.5 h-3 w-3 shrink-0" />
                          <span>{item.criticHint}</span>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Tab 3: 本章实体与伏笔速查 */}
      {activeTab === "entities" && (
        <div className="flex min-h-0 flex-1 flex-col">
          <div className="flex shrink-0 flex-col gap-2 border-b border-border-subtle bg-canvas/45 px-3 py-2.5">
            <div className="flex items-center gap-1.5">
              <div className="relative flex-1">
                <Search className="pointer-events-none absolute top-1/2 left-2 h-3 w-3 -translate-y-1/2 text-ink-muted" />
                <input
                  type="text"
                  value={entityQuery}
                  onChange={(e) => setEntityQuery(e.target.value)}
                  placeholder="过滤实体名称、别名或设定…"
                  className="w-full rounded-md border border-border-subtle bg-canvas py-1 pr-2 pl-6 text-xs text-ink-primary placeholder:text-ink-muted focus:border-accent focus:outline-none"
                />
              </div>
              <button
                type="button"
                onClick={onRefreshSnapshots}
                title="重新折算截至本章时间轴状态"
                className="rounded-md border border-border-subtle bg-elevated p-1.5 text-ink-secondary hover:text-ink-primary"
              >
                <RefreshCw
                  className={`h-3 w-3 ${snapshotsLoading ? "animate-spin text-accent" : ""}`}
                />
              </button>
            </div>

            <div className="flex items-center justify-between text-[11px] text-ink-muted">
              <span>截至第 {formatSeq(activeChapter.globalSeq)} 章状态快照</span>
              <label className="flex cursor-pointer items-center gap-1 text-ink-secondary select-none">
                <input
                  type="checkbox"
                  checked={includePending}
                  onChange={(e) => onChangeIncludePending(e.target.checked)}
                  className="h-3 w-3 accent-[var(--accent)]"
                />
                <span>含未审变更</span>
              </label>
            </div>
          </div>

          <div className="flex-1 overflow-y-auto px-3 py-3">
            {snapshotsLoading && snapshots.length === 0 ? (
              <div className="flex items-center justify-center gap-2 py-12 text-xs text-ink-muted">
                <Loader2 className="h-3.5 w-3.5 animate-spin text-accent" />
                <span>折算截至本章实体状态…</span>
              </div>
            ) : (
              <div className="flex flex-col gap-4">
                {/* 关联与待收伏笔预警 */}
                {relevantForeshadowings.length > 0 && (
                  <div className="flex flex-col gap-1.5">
                    <div className="flex items-center gap-1 text-[11px] font-medium text-ink-secondary">
                      <Flag className="h-3 w-3 text-warn" />
                      <span>待收 / 临近伏笔（{relevantForeshadowings.length}）</span>
                    </div>
                    {relevantForeshadowings.map(({ fs, overdue, dueSoon }) => {
                      const statusMeta = getForeshadowingStatusMeta(fs.status);
                      return (
                        <div
                          key={fs.id}
                          className={`rounded-lg border px-2.5 py-2 text-[11px] ${
                            overdue
                              ? "border-danger/45 bg-danger-soft/35"
                              : dueSoon
                              ? "border-warn/40 bg-warn-soft/30"
                              : "border-border-subtle bg-canvas/70"
                          }`}
                        >
                          <div className="flex items-center justify-between gap-1.5">
                            <span className="truncate font-medium text-ink-primary">
                              {fs.title}
                            </span>
                            <span
                              className={`shrink-0 rounded border px-1 py-0.5 text-[9.5px] ${statusMeta.badgeClass}`}
                            >
                              {overdue ? "已超期" : dueSoon ? "临近回收" : statusMeta.label}
                            </span>
                          </div>
                          {fs.description && (
                            <div className="mt-1 line-clamp-2 text-[10.5px] leading-relaxed text-ink-secondary">
                              {fs.description}
                            </div>
                          )}
                          <div className="nib-mono mt-1 text-[10px] text-ink-muted">
                            埋设 第{fs.plantedChapterSeq}章 · 计划第{" "}
                            {fs.targetResolveSeq || "?"} 章回收
                          </div>
                        </div>
                      );
                    })}
                  </div>
                )}

                {/* 实体快照清单 */}
                <div className="flex flex-col gap-1.5">
                  <div className="text-[11px] font-medium text-ink-secondary">
                    设定快照（{filteredEntities.length}）
                  </div>
                  {filteredEntities.length === 0 ? (
                    <div className="py-8 text-center text-xs text-ink-muted">
                      没有匹配的设定实体。
                    </div>
                  ) : (
                    filteredEntities.map((item) => {
                      const snap = item.snapshot;
                      const ent = snap.entity;
                      const expanded = expandedEntityIds.has(ent.id);
                      const isMentioned = item.mentionCount > 0 || item.inBeats;
                      const attrEntries = Object.entries(
                        snap.effectiveAttributes ?? {}
                      );

                      return (
                        <div
                          key={ent.id}
                          className={`rounded-lg border transition-colors ${
                            focusedEntityId === ent.id
                              ? "border-accent bg-accent-soft/25"
                              : isMentioned
                              ? "border-border-strong bg-canvas/85"
                              : "border-border-subtle bg-canvas/45 opacity-80"
                          }`}
                        >
                          <button
                            type="button"
                            onClick={() => toggleEntityExpand(ent.id)}
                            className="flex w-full items-center justify-between gap-2 px-2.5 py-2 text-left"
                          >
                            <div className="flex min-w-0 items-center gap-1.5">
                              {expanded ? (
                                <ChevronDown className="h-3 w-3 shrink-0 text-ink-muted" />
                              ) : (
                                <ChevronRight className="h-3 w-3 shrink-0 text-ink-muted" />
                              )}
                              {ent.isInvariant && (
                                <Shield className="h-3 w-3 shrink-0 text-accent" />
                              )}
                              <span className="truncate text-xs font-medium text-ink-primary">
                                {ent.name}
                              </span>
                              <span className="shrink-0 rounded bg-surface px-1 py-0.5 text-[9.5px] text-ink-muted">
                                {getEntityKindLabel(ent.kind)}
                              </span>
                            </div>

                            <div className="flex shrink-0 items-center gap-1">
                              {item.mentionCount > 0 && (
                                <span className="nib-mono rounded bg-accent-soft px-1 py-0.5 text-[9.5px] font-medium text-accent">
                                  ×{item.mentionCount}
                                </span>
                              )}
                              {item.inBeats && (
                                <span className="rounded bg-surface px-1 py-0.5 text-[9.5px] text-ink-secondary">
                                  拍
                                </span>
                              )}
                              {snap.pendingEventCount > 0 && (
                                <span className="rounded bg-warn-soft px-1 py-0.5 text-[9.5px] text-warn">
                                  待审
                                </span>
                              )}
                            </div>
                          </button>

                          {expanded && (
                            <div className="border-t border-border-subtle px-2.5 py-2 text-[11px]">
                              {ent.aliases && ent.aliases.length > 0 && (
                                <div className="mb-1 text-[10.5px] text-ink-muted">
                                  别名：{ent.aliases.join(" / ")}
                                </div>
                              )}
                              {ent.summary && (
                                <div className="leading-relaxed text-ink-secondary">
                                  {ent.summary}
                                </div>
                              )}
                              {attrEntries.length > 0 && (
                                <div className="mt-2 flex flex-wrap gap-1">
                                  {attrEntries.map(([k, v]) => (
                                    <span
                                      key={k}
                                      className="rounded border border-border-subtle bg-surface px-1.5 py-0.5 text-[10.5px] text-ink-primary"
                                    >
                                      <span className="text-ink-muted">{k}：</span>
                                      {String(v)}
                                    </span>
                                  ))}
                                </div>
                              )}
                              {!readingMode && !generating && (
                                <div className="mt-2 flex justify-end">
                                  <button
                                    type="button"
                                    onClick={() => onInsertEntityName(ent.name)}
                                    className="flex items-center gap-1 rounded border border-border-subtle bg-elevated px-2 py-0.5 text-[10.5px] text-ink-secondary hover:border-accent/50 hover:text-accent"
                                  >
                                    <Plus className="h-2.5 w-2.5" />
                                    <span>插入名称到光标</span>
                                  </button>
                                </div>
                              )}
                            </div>
                          )}
                        </div>
                      );
                    })
                  )}
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </aside>
  );
}
