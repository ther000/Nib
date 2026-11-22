import { describe, expect, test } from "bun:test";
import type {
  Entity,
  EntityPresenceStat,
  EntityRelation,
  EntitySnapshot,
} from "../mygo";
import {
  buildRelationGraph,
  classifyRelationPolarity,
  computeGraphLayout,
  computeNeighborhood,
  extractEntityFaction,
  filterRelationsForGraph,
  isEntityAbsentAlert,
} from "./relationGraph";

function makeEntity(
  id: number,
  name: string,
  kind: Entity["kind"] = "character",
  attributes: Record<string, string> = {},
  isInvariant = false
): Entity {
  return {
    id,
    bookId: 1,
    kind,
    name,
    aliases: [],
    isInvariant,
    summary: "",
    attributes,
    createdAt: "",
    updatedAt: "",
  };
}

function makeRelation(
  id: number,
  sourceEntityId: number,
  targetEntityId: number,
  relationType: string,
  opts: Partial<EntityRelation> = {}
): EntityRelation {
  return {
    id,
    bookId: 1,
    sourceEntityId,
    targetEntityId,
    relationType,
    description: "",
    effectiveChapterSeq: 0,
    status: "committed",
    ...opts,
  };
}

function makePresence(
  entityId: number,
  overrides: Partial<EntityPresenceStat> = {}
): EntityPresenceStat {
  return {
    entityId,
    entityName: `E${entityId}`,
    entityKind: "character",
    isInvariant: false,
    appearanceChapters: 1,
    mentionCount: 1,
    beatAppearances: 0,
    firstSeenSeq: 1,
    lastSeenSeq: 1,
    lastSeenChapterId: 1,
    lastSeenChapterTitle: "第1章",
    absentChapters: 0,
    neverAppeared: false,
    appearedChapterSeqs: [1],
    ...overrides,
  };
}

describe("relationGraph helpers", () => {
  test("classifies relation polarity correctly", () => {
    expect(classifyRelationPolarity("死敌")).toBe("hostile");
    expect(classifyRelationPolarity("宗门叛徒")).toBe("hostile");
    expect(classifyRelationPolarity("同门师兄")).toBe("ally");
    expect(classifyRelationPolarity("利益同盟")).toBe("ally");
    expect(classifyRelationPolarity("陌生路人")).toBe("neutral");
  });

  test("extracts entity faction from kind=faction or attributes", () => {
    const sect = makeEntity(1, "青云宗", "faction");
    const disciple = makeEntity(2, "林渊", "character", { 所属势力: "青云宗" });
    const snapSect: EntitySnapshot = {
      entity: sect,
      effectiveAttributes: {},
      appliedEventCount: 0,
      pendingEventCount: 0,
    };
    const snapDisciple: EntitySnapshot = {
      entity: disciple,
      effectiveAttributes: { 宗门: "青云宗" },
      appliedEventCount: 1,
      pendingEventCount: 0,
    };

    expect(extractEntityFaction(snapSect)).toBe("青云宗");
    expect(extractEntityFaction(snapDisciple)).toBe("青云宗");
  });

  test("filters active, historical, future, and pending relations across timeline", () => {
    const rAlly = makeRelation(1, 1, 2, "同门", {
      effectiveChapterSeq: 1,
      endedChapterSeq: 80,
    });
    const rEnemy = makeRelation(2, 1, 2, "死敌", {
      effectiveChapterSeq: 80,
    });
    const rPending = makeRelation(3, 1, 3, "同盟", {
      effectiveChapterSeq: 40,
      status: "pending",
    });
    const all = [rAlly, rEnemy, rPending];

    // 第 50 章：不含未来死敌；includePending=false 不含 pending；showEnded=true 也不该显示未结束的同门
    const at50Committed = filterRelationsForGraph(all, [rAlly], 50, false, true);
    expect(at50Committed.map((x) => [x.relation.id, x.isHistorical])).toEqual([
      [1, false],
    ]);

    // 第 50 章：includePending=true 时显示 pending 边
    const at50Pending = filterRelationsForGraph(
      all,
      [rAlly, rPending],
      50,
      true,
      false
    );
    expect(at50Pending.map((x) => x.relation.id)).toEqual([1, 3]);

    // 第 80 章：showEnded=false 只见死敌
    const at80ActiveOnly = filterRelationsForGraph(all, [rEnemy], 80, false, false);
    expect(at80ActiveOnly.map((x) => [x.relation.id, x.isHistorical])).toEqual([
      [2, false],
    ]);

    // 第 80 章：showEnded=true 同时返回已终止同门（historical=true）与死敌（historical=false）
    const at80WithHistory = filterRelationsForGraph(all, [rEnemy], 80, false, true);
    expect(at80WithHistory.map((x) => [x.relation.id, x.isHistorical])).toEqual([
      [2, false],
      [1, true],
    ]);
  });

  test("numbers parallel edges and computes 1-hop / 2-hop neighborhoods", () => {
    const entities = [
      makeEntity(1, "林渊"),
      makeEntity(2, "周明"),
      makeEntity(3, "苏清浅"),
      makeEntity(4, "魔尊"),
      makeEntity(5, "世界铁律", "rule", {}, true),
    ];
    const r1 = makeRelation(10, 1, 2, "同门", { effectiveChapterSeq: 1, endedChapterSeq: 80 });
    const r2 = makeRelation(11, 1, 2, "死敌", { effectiveChapterSeq: 80 });
    const r3 = makeRelation(12, 2, 3, "师兄妹", { effectiveChapterSeq: 1 });
    const r4 = makeRelation(13, 3, 4, "宿敌", { effectiveChapterSeq: 1 });

    const built = buildRelationGraph({
      entities,
      snapshots: [],
      presenceStats: [],
      allRelations: [r1, r2, r3, r4],
      activeRelations: [r2, r3, r4],
      timelineSeq: 80,
      includePending: false,
      showEnded: true,
      kindFilter: "all",
      factionFilter: "all",
      relationTypeFilter: "all",
      onlyConnected: false,
      absentThreshold: 15,
    });

    // 铁律实体被排除，保留 4 个普通实体
    expect(built.nodes.map((n) => n.id)).toEqual([1, 2, 3, 4]);
    const parallelEdges = built.edges.filter(
      (e) =>
        (e.sourceId === 1 && e.targetId === 2) ||
        (e.sourceId === 2 && e.targetId === 1)
    );
    expect(parallelEdges.length).toBe(2);
    expect(parallelEdges.map((e) => e.curveIndex)).toEqual([0, 1]);
    expect(parallelEdges.map((e) => e.curveTotal)).toEqual([2, 2]);

    const nh1 = computeNeighborhood(1, built.edges, 1);
    expect(Array.from(nh1.hop0)).toEqual([1]);
    expect(Array.from(nh1.hop1)).toEqual([2]);
    expect(Array.from(nh1.hop2)).toEqual([]);
    expect(nh1.edgeHops.get(10)).toBe(1);
    expect(nh1.edgeHops.get(11)).toBe(1);
    expect(nh1.edgeHops.has(12)).toBe(false);

    const nh2 = computeNeighborhood(1, built.edges, 2);
    expect(Array.from(nh2.hop1)).toEqual([2]);
    expect(Array.from(nh2.hop2)).toEqual([3]);
    expect(nh2.hop2.has(4)).toBe(false);
    expect(nh2.edgeHops.get(12)).toBe(2);
    expect(nh2.edgeHops.has(13)).toBe(false);
  });

  test("computeGraphLayout is deterministic, centered, and handles 0/1/N nodes cleanly", () => {
    expect(computeGraphLayout([], []).positions).toEqual({});

    const single = buildRelationGraph({
      entities: [makeEntity(1, "林渊")],
      snapshots: [],
      presenceStats: [],
      allRelations: [],
      activeRelations: [],
      timelineSeq: 1,
      includePending: false,
      showEnded: false,
      kindFilter: "all",
      factionFilter: "all",
      relationTypeFilter: "all",
      onlyConnected: false,
      absentThreshold: 15,
    });
    const singleLayout = computeGraphLayout(single.nodes, single.edges);
    expect(singleLayout.positions[1]).toEqual({ x: 0, y: 0 });

    const multi = buildRelationGraph({
      entities: [
        makeEntity(1, "林渊", "character", { 宗门: "青云宗" }),
        makeEntity(2, "周明", "character", { 宗门: "青云宗" }),
        makeEntity(3, "血煞老魔", "character", { 宗门: "血煞教" }),
        makeEntity(4, "青云宗", "faction"),
      ],
      snapshots: [],
      presenceStats: [],
      allRelations: [],
      activeRelations: [
        makeRelation(1, 1, 4, "隶属"),
        makeRelation(2, 2, 4, "隶属"),
        makeRelation(3, 1, 3, "死敌"),
      ],
      timelineSeq: 10,
      includePending: false,
      showEnded: false,
      kindFilter: "all",
      factionFilter: "all",
      relationTypeFilter: "all",
      onlyConnected: false,
      absentThreshold: 15,
    });

    const l1 = computeGraphLayout(multi.nodes, multi.edges);
    const l2 = computeGraphLayout(multi.nodes, multi.edges);
    expect(l1).toEqual(l2);

    const coords = Object.values(l1.positions);
    expect(coords.length).toBe(4);
    for (const pt of coords) {
      expect(Number.isFinite(pt.x)).toBe(true);
      expect(Number.isFinite(pt.y)).toBe(true);
    }
    // 验证所有节点之间不存在重合
    for (let i = 0; i < coords.length; i++) {
      const a = coords[i];
      if (!a) continue;
      for (let j = i + 1; j < coords.length; j++) {
        const b = coords[j];
        if (!b) continue;
        const dist = Math.hypot(a.x - b.x, a.y - b.y);
        expect(dist).toBeGreaterThan(20);
      }
    }
  });

  test("absent alert excludes invariants and never-appeared entities", () => {
    expect(
      isEntityAbsentAlert(
        { isInvariant: true },
        makePresence(1, { absentChapters: 40, neverAppeared: false }),
        15
      )
    ).toBe(false);
    expect(
      isEntityAbsentAlert(
        { isInvariant: false },
        makePresence(2, { absentChapters: 0, neverAppeared: true }),
        15
      )
    ).toBe(false);
    expect(
      isEntityAbsentAlert(
        { isInvariant: false },
        makePresence(3, { absentChapters: 14, neverAppeared: false }),
        15
      )
    ).toBe(false);
    expect(
      isEntityAbsentAlert(
        { isInvariant: false },
        makePresence(4, { absentChapters: 15, neverAppeared: false }),
        15
      )
    ).toBe(true);
  });
});
