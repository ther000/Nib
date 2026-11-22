import type {
  Entity,
  EntityKind,
  EntityPresenceStat,
  EntityRelation,
  EntitySnapshot,
} from "../mygo";

export type RelationPolarity = "ally" | "hostile" | "neutral";

export interface GraphNode {
  id: number;
  entity: Entity;
  snapshot?: EntitySnapshot;
  presence?: EntityPresenceStat;
  factionLabel: string;
  degree: number;
  radius: number;
  isAbsentAlert: boolean;
}

export interface GraphEdge {
  id: number;
  relation: EntityRelation;
  sourceId: number;
  targetId: number;
  polarity: RelationPolarity;
  isHistorical: boolean;
  curveIndex: number;
  curveTotal: number;
}

export interface GraphPoint {
  x: number;
  y: number;
}

export interface GraphBounds {
  minX: number;
  maxX: number;
  minY: number;
  maxY: number;
  width: number;
  height: number;
}

export interface GraphLayoutResult {
  positions: Record<number, GraphPoint>;
  bounds: GraphBounds;
}

export interface NeighborhoodResult {
  hop0: Set<number>;
  hop1: Set<number>;
  hop2: Set<number>;
  edgeHops: Map<number, 1 | 2>;
}

export interface BuildRelationGraphParams {
  entities: Entity[];
  snapshots: EntitySnapshot[];
  presenceStats: EntityPresenceStat[];
  allRelations: EntityRelation[];
  activeRelations: EntityRelation[];
  timelineSeq: number;
  includePending: boolean;
  showEnded: boolean;
  kindFilter: string; // "all" | EntityKind
  factionFilter: string; // "all" | factionLabel
  relationTypeFilter: string; // "all" | relationType
  onlyConnected: boolean;
  absentThreshold: number;
}

export interface BuiltRelationGraph {
  nodes: GraphNode[];
  edges: GraphEdge[];
  availableFactions: string[];
  availableRelationTypes: string[];
}

const HOSTILE_KEYWORDS = [
  "死敌",
  "宿敌",
  "仇敌",
  "追杀",
  "通缉",
  "叛徒",
  "敌对",
  "对立",
  "背叛",
  "仇杀",
  "敌视",
];

const ALLY_KEYWORDS = [
  "师徒",
  "同盟",
  "同门",
  "亲友",
  "道侣",
  "效忠",
  "归顺",
  "合作",
  "盟友",
  "隶属",
  "师兄",
  "师姐",
  "师弟",
  "师妹",
  "家人",
  "上下级",
];

const FACTION_ATTR_KEYS = [
  "affiliation",
  "faction",
  "sect",
  "camp",
  "阵营",
  "势力",
  "宗门",
  "所属势力",
  "家族",
];

export function classifyRelationPolarity(relationType: string): RelationPolarity {
  const normalized = relationType.trim().toLowerCase();
  if (!normalized) return "neutral";
  for (const kw of HOSTILE_KEYWORDS) {
    if (normalized.includes(kw)) return "hostile";
  }
  for (const kw of ALLY_KEYWORDS) {
    if (normalized.includes(kw)) return "ally";
  }
  return "neutral";
}

export function extractEntityFaction(snap: EntitySnapshot | undefined): string {
  if (!snap) return "";
  if (snap.entity.kind === "faction") {
    return snap.entity.name.trim();
  }
  const attrs = snap.effectiveAttributes ?? snap.entity.attributes ?? {};
  // 精确匹配优先，再大小写不敏感匹配
  for (const key of FACTION_ATTR_KEYS) {
    const val = attrs[key]?.trim();
    if (val) return val;
  }
  const lowerEntries = Object.entries(attrs).map(([k, v]) => [
    k.trim().toLowerCase(),
    v.trim(),
  ] as const);
  for (const key of FACTION_ATTR_KEYS) {
    const lowerKey = key.toLowerCase();
    const hit = lowerEntries.find(([k, v]) => k === lowerKey && v.length > 0);
    if (hit) return hit[1];
  }
  return "";
}

export function isEntityAbsentAlert(
  entity: Pick<Entity, "isInvariant">,
  presence: Pick<EntityPresenceStat, "neverAppeared" | "absentChapters"> | undefined,
  absentThreshold: number
): boolean {
  if (!presence || entity.isInvariant) return false;
  if (presence.neverAppeared) return false;
  if (absentThreshold <= 0) return false;
  return presence.absentChapters >= absentThreshold;
}

export function filterRelationsForGraph(
  allRelations: EntityRelation[],
  activeRelations: EntityRelation[],
  timelineSeq: number,
  includePending: boolean,
  showEnded: boolean
): Array<{ relation: EntityRelation; isHistorical: boolean }> {
  const out: Array<{ relation: EntityRelation; isHistorical: boolean }> = [];
  const seenIds = new Set<number>();

  for (const rel of activeRelations) {
    if (rel.effectiveChapterSeq > timelineSeq) continue;
    if (!includePending && rel.status !== "committed") continue;
    if (seenIds.has(rel.id)) continue;
    seenIds.add(rel.id);
    out.push({ relation: rel, isHistorical: false });
  }

  if (!showEnded) {
    return out;
  }

  for (const rel of allRelations) {
    if (seenIds.has(rel.id)) continue;
    if (rel.effectiveChapterSeq > timelineSeq) continue;
    if (rel.endedChapterSeq === undefined || rel.endedChapterSeq === null) continue;
    if (rel.endedChapterSeq > timelineSeq) continue;
    if (!includePending && rel.status !== "committed") continue;
    seenIds.add(rel.id);
    out.push({ relation: rel, isHistorical: true });
  }

  return out;
}

export function buildRelationGraph(params: BuildRelationGraphParams): BuiltRelationGraph {
  const {
    entities,
    snapshots,
    presenceStats,
    allRelations,
    activeRelations,
    timelineSeq,
    includePending,
    showEnded,
    kindFilter,
    factionFilter,
    relationTypeFilter,
    onlyConnected,
    absentThreshold,
  } = params;

  const snapMap = new Map<number, EntitySnapshot>();
  for (const s of snapshots) {
    snapMap.set(s.entity.id, s);
  }

  const presenceMap = new Map<number, EntityPresenceStat>();
  for (const p of presenceStats) {
    presenceMap.set(p.entityId, p);
  }

  const nonInvariantEntities = entities.filter((e) => !e.isInvariant);
  const entityMap = new Map<number, Entity>();
  const factionByEntityId = new Map<number, string>();
  const factionSet = new Set<string>();

  for (const ent of nonInvariantEntities) {
    entityMap.set(ent.id, ent);
    const snap = snapMap.get(ent.id) ?? {
      entity: ent,
      effectiveAttributes: ent.attributes ?? {},
      appliedEventCount: 0,
      pendingEventCount: 0,
    };
    const faction = extractEntityFaction(snap);
    factionByEntityId.set(ent.id, faction);
    if (faction) factionSet.add(faction);
  }

  const slicedRelations = filterRelationsForGraph(
    allRelations,
    activeRelations,
    timelineSeq,
    includePending,
    showEnded
  );

  const relationTypeSet = new Set<string>();
  for (const item of slicedRelations) {
    const rt = item.relation.relationType.trim();
    if (rt) relationTypeSet.add(rt);
  }

  // 先基于实体分类与势力筛选出候选节点集合
  const candidateNodeIds = new Set<number>();
  for (const ent of nonInvariantEntities) {
    if (kindFilter !== "all" && ent.kind !== (kindFilter as EntityKind)) {
      continue;
    }
    const faction = factionByEntityId.get(ent.id) ?? "";
    if (factionFilter !== "all" && faction !== factionFilter) {
      continue;
    }
    candidateNodeIds.add(ent.id);
  }

  // 过滤双方都在候选集合且满足关系类型筛选的边
  const rawEdges = slicedRelations.filter(({ relation }) => {
    if (!candidateNodeIds.has(relation.sourceEntityId)) return false;
    if (!candidateNodeIds.has(relation.targetEntityId)) return false;
    if (relation.sourceEntityId === relation.targetEntityId) return false;
    if (
      relationTypeFilter !== "all" &&
      relation.relationType.trim() !== relationTypeFilter
    ) {
      return false;
    }
    return true;
  });

  // 为每对无序端点统计平行边序号与总数
  const pairGroups = new Map<string, number[]>();
  rawEdges.forEach((item, idx) => {
    const a = Math.min(item.relation.sourceEntityId, item.relation.targetEntityId);
    const b = Math.max(item.relation.sourceEntityId, item.relation.targetEntityId);
    const key = `${a}:${b}`;
    const arr = pairGroups.get(key);
    if (arr) {
      arr.push(idx);
    } else {
      pairGroups.set(key, [idx]);
    }
  });

  const degreeMap = new Map<number, number>();
  const edges: GraphEdge[] = rawEdges.map((item, idx) => {
    const rel = item.relation;
    degreeMap.set(rel.sourceEntityId, (degreeMap.get(rel.sourceEntityId) ?? 0) + 1);
    degreeMap.set(rel.targetEntityId, (degreeMap.get(rel.targetEntityId) ?? 0) + 1);
    const a = Math.min(rel.sourceEntityId, rel.targetEntityId);
    const b = Math.max(rel.sourceEntityId, rel.targetEntityId);
    const group = pairGroups.get(`${a}:${b}`) ?? [idx];
    const curveIndex = group.indexOf(idx);
    return {
      id: rel.id,
      relation: rel,
      sourceId: rel.sourceEntityId,
      targetId: rel.targetEntityId,
      polarity: classifyRelationPolarity(rel.relationType),
      isHistorical: item.isHistorical,
      curveIndex: curveIndex >= 0 ? curveIndex : 0,
      curveTotal: group.length,
    };
  });

  const nodes: GraphNode[] = [];
  for (const ent of nonInvariantEntities) {
    if (!candidateNodeIds.has(ent.id)) continue;
    const degree = degreeMap.get(ent.id) ?? 0;
    if (onlyConnected && degree === 0) continue;
    const presence = presenceMap.get(ent.id);
    const appearanceScore = presence?.appearanceChapters ?? 0;
    const rawRadius = 22 + Math.min(12, degree * 1.8 + Math.sqrt(appearanceScore) * 1.4);
    const radius = Math.round(Math.max(22, Math.min(34, rawRadius)));

    nodes.push({
      id: ent.id,
      entity: ent,
      snapshot: snapMap.get(ent.id),
      presence,
      factionLabel: factionByEntityId.get(ent.id) ?? "",
      degree,
      radius,
      isAbsentAlert: isEntityAbsentAlert(ent, presence, absentThreshold),
    });
  }

  return {
    nodes,
    edges,
    availableFactions: Array.from(factionSet).sort((a, b) => a.localeCompare(b, "zh-CN")),
    availableRelationTypes: Array.from(relationTypeSet).sort((a, b) =>
      a.localeCompare(b, "zh-CN")
    ),
  };
}

export function computeGraphLayout(
  nodes: GraphNode[],
  edges: GraphEdge[]
): GraphLayoutResult {
  if (nodes.length === 0) {
    return {
      positions: {},
      bounds: { minX: 0, maxX: 0, minY: 0, maxY: 0, width: 0, height: 0 },
    };
  }
  if (nodes.length === 1 && nodes[0]) {
    const only = nodes[0];
    const r = only.radius;
    return {
      positions: { [only.id]: { x: 0, y: 0 } },
      bounds: { minX: -r, maxX: r, minY: -r, maxY: r, width: r * 2, height: r * 2 },
    };
  }

  // 按 factionLabel || kind 分组，确保初始位置完全确定且同类聚合
  const groupKeys = new Map<number, string>();
  const groupBuckets = new Map<string, GraphNode[]>();
  for (const node of nodes) {
    const key = node.factionLabel ? `f:${node.factionLabel}` : `k:${node.entity.kind}`;
    groupKeys.set(node.id, key);
    const bucket = groupBuckets.get(key);
    if (bucket) {
      bucket.push(node);
    } else {
      groupBuckets.set(key, [node]);
    }
  }

  const orderedGroups = Array.from(groupBuckets.entries()).sort(([ka, ba], [kb, bb]) => {
    if (ba.length !== bb.length) return bb.length - ba.length;
    return ka.localeCompare(kb, "zh-CN");
  });

  const posX = new Map<number, number>();
  const posY = new Map<number, number>();
  const baseSpread = Math.max(200, Math.sqrt(nodes.length) * 115);

  orderedGroups.forEach(([, bucket], gIdx) => {
    const sorted = [...bucket].sort((a, b) => {
      if (a.degree !== b.degree) return b.degree - a.degree;
      const appA = a.presence?.appearanceChapters ?? 0;
      const appB = b.presence?.appearanceChapters ?? 0;
      if (appA !== appB) return appB - appA;
      return a.id - b.id;
    });

    const sectorAngle =
      orderedGroups.length === 1
        ? 0
        : (gIdx / orderedGroups.length) * Math.PI * 2 - Math.PI / 2;
    const groupCenterDist = orderedGroups.length === 1 ? 0 : baseSpread * 0.42;
    const gx = Math.cos(sectorAngle) * groupCenterDist;
    const gy = Math.sin(sectorAngle) * groupCenterDist;

    sorted.forEach((node, idx) => {
      if (orderedGroups.length === 1 && idx === 0) {
        posX.set(node.id, 0);
        posY.set(node.id, 0);
        return;
      }
      const ring = Math.floor(idx / 7) + 1;
      const posInRing = idx % 7;
      const ringCount = Math.min(7, sorted.length - (ring - 1) * 7);
      const localAngle =
        sectorAngle + (posInRing / Math.max(1, ringCount)) * Math.PI * 2 + ring * 0.35;
      const localR = (orderedGroups.length === 1 ? 110 : 62) * ring + idx * 4;
      posX.set(node.id, gx + Math.cos(localAngle) * localR);
      posY.set(node.id, gy + Math.sin(localAngle) * localR);
    });
  });

  // 72 轮确定性 Fruchterman-Reingold 力导向迭代
  const iterations = 72;
  const k = Math.max(118, Math.sqrt((960 * 680) / Math.max(1, nodes.length)) * 0.72);
  const k2 = k * k;
  const dispX = new Map<number, number>();
  const dispY = new Map<number, number>();

  for (let iter = 0; iter < iterations; iter++) {
    for (const n of nodes) {
      dispX.set(n.id, 0);
      dispY.set(n.id, 0);
    }

    // 1) 节点两两排斥 + 轻微同阵营聚合
    for (let i = 0; i < nodes.length; i++) {
      const u = nodes[i];
      if (!u) continue;
      const ux = posX.get(u.id) ?? 0;
      const uy = posY.get(u.id) ?? 0;
      const ug = groupKeys.get(u.id);

      for (let j = i + 1; j < nodes.length; j++) {
        const v = nodes[j];
        if (!v) continue;
        const vx = posX.get(v.id) ?? 0;
        const vy = posY.get(v.id) ?? 0;

        let dx = ux - vx;
        let dy = uy - vy;
        let dist2 = dx * dx + dy * dy;
        if (dist2 < 0.01) {
          // 确定性微小偏置打破完全重合
          const fallbackAngle = ((u.id * 37 + v.id * 17) % 360) * (Math.PI / 180);
          dx = Math.cos(fallbackAngle);
          dy = Math.sin(fallbackAngle);
          dist2 = 1;
        }
        const dist = Math.sqrt(dist2);
        const minSpacing = u.radius + v.radius + 36;
        let rep = k2 / dist;
        if (dist < minSpacing) {
          rep *= 1.8;
        }
        let fx = (dx / dist) * rep;
        let fy = (dy / dist) * rep;

        // 同势力/分类微聚合力
        if (ug && ug === groupKeys.get(v.id) && dist > minSpacing * 1.4) {
          const groupPull = dist * 0.035;
          fx -= (dx / dist) * groupPull;
          fy -= (dy / dist) * groupPull;
        }

        dispX.set(u.id, (dispX.get(u.id) ?? 0) + fx);
        dispY.set(u.id, (dispY.get(u.id) ?? 0) + fy);
        dispX.set(v.id, (dispX.get(v.id) ?? 0) - fx);
        dispY.set(v.id, (dispY.get(v.id) ?? 0) - fy);
      }
    }

    // 2) 连边引力（历史关系降权，多平行边只按第一条主边施力防过度拉近）
    const seenEdgePair = new Set<string>();
    for (const edge of edges) {
      const a = Math.min(edge.sourceId, edge.targetId);
      const b = Math.max(edge.sourceId, edge.targetId);
      const pairKey = `${a}:${b}`;
      if (seenEdgePair.has(pairKey)) continue;
      seenEdgePair.add(pairKey);

      const ux = posX.get(edge.sourceId);
      const uy = posY.get(edge.sourceId);
      const vx = posX.get(edge.targetId);
      const vy = posY.get(edge.targetId);
      if (ux === undefined || uy === undefined || vx === undefined || vy === undefined) {
        continue;
      }

      const dx = ux - vx;
      const dy = uy - vy;
      const dist = Math.max(1, Math.hypot(dx, dy));
      const weight = edge.isHistorical ? 0.25 : 1.0;
      const attr = ((dist * dist) / k) * weight * 0.55;
      const fx = (dx / dist) * attr;
      const fy = (dy / dist) * attr;

      dispX.set(edge.sourceId, (dispX.get(edge.sourceId) ?? 0) - fx);
      dispY.set(edge.sourceId, (dispY.get(edge.sourceId) ?? 0) - fy);
      dispX.set(edge.targetId, (dispX.get(edge.targetId) ?? 0) + fx);
      dispY.set(edge.targetId, (dispY.get(edge.targetId) ?? 0) + fy);
    }

    // 3) 轻度向心力 + 降温步长钳制
    const temp = Math.max(6, 56 * (1 - iter / iterations));
    for (const n of nodes) {
      const x = posX.get(n.id) ?? 0;
      const y = posY.get(n.id) ?? 0;
      const gravityFactor = n.degree === 0 ? 0.08 : 0.04;
      const gx = -x * gravityFactor;
      const gy = -y * gravityFactor;

      const fx = (dispX.get(n.id) ?? 0) + gx;
      const fy = (dispY.get(n.id) ?? 0) + gy;
      const len = Math.hypot(fx, fy);
      if (len > 0) {
        const step = Math.min(len, temp);
        posX.set(n.id, x + (fx / len) * step);
        posY.set(n.id, y + (fy / len) * step);
      }
    }
  }

  // 平移使整体包围盒中心归零
  let rawMinX = Infinity;
  let rawMaxX = -Infinity;
  let rawMinY = Infinity;
  let rawMaxY = -Infinity;
  for (const n of nodes) {
    const x = posX.get(n.id) ?? 0;
    const y = posY.get(n.id) ?? 0;
    if (x - n.radius < rawMinX) rawMinX = x - n.radius;
    if (x + n.radius > rawMaxX) rawMaxX = x + n.radius;
    if (y - n.radius < rawMinY) rawMinY = y - n.radius;
    if (y + n.radius > rawMaxY) rawMaxY = y + n.radius;
  }

  const cx = (rawMinX + rawMaxX) / 2;
  const cy = (rawMinY + rawMaxY) / 2;

  const positions: Record<number, GraphPoint> = {};
  let minX = Infinity;
  let maxX = -Infinity;
  let minY = Infinity;
  let maxY = -Infinity;

  for (const n of nodes) {
    const x = Math.round(((posX.get(n.id) ?? 0) - cx) * 100) / 100;
    const y = Math.round(((posY.get(n.id) ?? 0) - cy) * 100) / 100;
    positions[n.id] = { x, y };
    if (x - n.radius < minX) minX = x - n.radius;
    if (x + n.radius > maxX) maxX = x + n.radius;
    if (y - n.radius < minY) minY = y - n.radius;
    if (y + n.radius > maxY) maxY = y + n.radius;
  }

  return {
    positions,
    bounds: {
      minX,
      maxX,
      minY,
      maxY,
      width: Math.max(0, maxX - minX),
      height: Math.max(0, maxY - minY),
    },
  };
}

export function computeNeighborhood(
  selectedId: number | null,
  edges: GraphEdge[],
  maxHops: 1 | 2
): NeighborhoodResult {
  const hop0 = new Set<number>();
  const hop1 = new Set<number>();
  const hop2 = new Set<number>();
  const edgeHops = new Map<number, 1 | 2>();

  if (!selectedId || selectedId <= 0) {
    return { hop0, hop1, hop2, edgeHops };
  }

  hop0.add(selectedId);

  const adj = new Map<number, Array<{ neighborId: number; edgeId: number }>>();
  for (const edge of edges) {
    const aList = adj.get(edge.sourceId);
    if (aList) aList.push({ neighborId: edge.targetId, edgeId: edge.id });
    else adj.set(edge.sourceId, [{ neighborId: edge.targetId, edgeId: edge.id }]);

    const bList = adj.get(edge.targetId);
    if (bList) bList.push({ neighborId: edge.sourceId, edgeId: edge.id });
    else adj.set(edge.targetId, [{ neighborId: edge.sourceId, edgeId: edge.id }]);
  }

  for (const link of adj.get(selectedId) ?? []) {
    edgeHops.set(link.edgeId, 1);
    if (!hop0.has(link.neighborId)) {
      hop1.add(link.neighborId);
    }
  }

  if (maxHops === 2) {
    for (const h1 of hop1) {
      for (const link of adj.get(h1) ?? []) {
        if (!edgeHops.has(link.edgeId)) {
          edgeHops.set(link.edgeId, 2);
        }
        if (!hop0.has(link.neighborId) && !hop1.has(link.neighborId)) {
          hop2.add(link.neighborId);
        }
      }
    }
  }

  return { hop0, hop1, hop2, edgeHops };
}
