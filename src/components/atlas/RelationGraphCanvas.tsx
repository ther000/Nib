import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
} from "react";
import {
  AlertTriangle,
  ArrowRight,
  Crosshair,
  ExternalLink,
  Maximize2,
  Minus,
  Plus,
  RotateCcw,
  X,
} from "lucide-react";
import type { Chapter, EntityKind, GenreFieldSpec } from "../../mygo";
import { formatSeq, getEntityKindLabel } from "../../lib/constants";
import {
  computeGraphLayout,
  computeNeighborhood,
  type GraphEdge,
  type GraphNode,
  type GraphPoint,
  type RelationPolarity,
} from "../../lib/relationGraph";

interface RelationGraphCanvasProps {
  nodes: GraphNode[];
  edges: GraphEdge[];
  chapters: Chapter[];
  fieldLabelMap: Map<string, GenreFieldSpec>;
  selectedNodeId: number | null;
  onSelectNode: (id: number | null) => void;
  highlightHops: 1 | 2;
  timelineSeq: number;
  onJumpToEntity: (entityId: number) => void;
  onJumpToChapter: (chapterId: number) => void;
  onManageRelations?: (entityId: number) => void;
}

interface ViewportState {
  tx: number;
  ty: number;
  scale: number;
}

type DragState =
  | {
      kind: "pan";
      startClientX: number;
      startClientY: number;
      startTx: number;
      startTy: number;
      moved: boolean;
    }
  | {
      kind: "node";
      nodeId: number;
      startClientX: number;
      startClientY: number;
      startX: number;
      startY: number;
      moved: boolean;
    };

const KIND_ACCENT_STROKE: Record<EntityKind, string> = {
  character: "var(--accent)",
  faction: "var(--status-ok)",
  location: "var(--text-secondary)",
  item: "var(--status-warn)",
  system: "var(--accent-hover)",
  rule: "var(--status-danger)",
};

const KIND_FILL: Record<EntityKind, string> = {
  character: "var(--bg-elevated)",
  faction: "var(--bg-surface)",
  location: "var(--bg-surface)",
  item: "var(--bg-elevated)",
  system: "var(--bg-surface)",
  rule: "var(--bg-surface)",
};

function getPolarityStroke(polarity: RelationPolarity): string {
  switch (polarity) {
    case "hostile":
      return "var(--status-danger)";
    case "ally":
      return "var(--status-ok)";
    default:
      return "var(--border-strong)";
  }
}

function getPolarityMarkerSuffix(polarity: RelationPolarity, isHistorical: boolean): string {
  if (isHistorical) return "hist";
  return polarity;
}

function clampScale(s: number): number {
  return Math.max(0.35, Math.min(2.6, s));
}

function computeQuadraticEdgeGeometry(
  sx: number,
  sy: number,
  tx: number,
  ty: number,
  sourceRadius: number,
  targetRadius: number,
  curveIndex: number,
  curveTotal: number
) {
  const dx = tx - sx;
  const dy = ty - sy;
  const dist = Math.hypot(dx, dy) || 1;
  const ux = dx / dist;
  const uy = dy / dist;
  // 垂直法向量
  const nx = -uy;
  const ny = ux;

  // 多条平行边围绕中心对称展开弧度；单边保持轻微直线
  let curvature = 0;
  if (curveTotal > 1) {
    const offset = curveIndex - (curveTotal - 1) / 2;
    curvature = offset * Math.min(68, Math.max(36, dist * 0.18));
  }

  const midX = (sx + tx) / 2 + nx * curvature;
  const midY = (sy + ty) / 2 + ny * curvature;

  // 起止点从圆周外缘切出，避免箭头被节点遮挡
  const startGap = sourceRadius + 2;
  const endGap = targetRadius + 7;

  const sToMidX = midX - sx;
  const sToMidY = midY - sy;
  const sLen = Math.hypot(sToMidX, sToMidY) || 1;
  const startX = sx + (sToMidX / sLen) * Math.min(startGap, dist * 0.4);
  const startY = sy + (sToMidY / sLen) * Math.min(startGap, dist * 0.4);

  const midToTX = tx - midX;
  const midToTY = ty - midY;
  const eLen = Math.hypot(midToTX, midToTY) || 1;
  const endX = tx - (midToTX / eLen) * Math.min(endGap, dist * 0.45);
  const endY = ty - (midToTY / eLen) * Math.min(endGap, dist * 0.45);

  // 二次贝塞尔 t=0.5 处的真实中点：0.25*P0 + 0.5*C + 0.25*P1
  const labelX = 0.25 * startX + 0.5 * midX + 0.25 * endX;
  const labelY = 0.25 * startY + 0.5 * midY + 0.25 * endY;

  const path =
    Math.abs(curvature) < 0.5
      ? `M ${startX.toFixed(1)} ${startY.toFixed(1)} L ${endX.toFixed(1)} ${endY.toFixed(1)}`
      : `M ${startX.toFixed(1)} ${startY.toFixed(1)} Q ${midX.toFixed(1)} ${midY.toFixed(1)} ${endX.toFixed(1)} ${endY.toFixed(1)}`;

  return { path, labelX, labelY };
}

function truncateNodeLabel(name: string, maxChars = 6): string {
  const trimmed = name.trim();
  if (trimmed.length <= maxChars) return trimmed;
  return `${trimmed.slice(0, maxChars - 1)}…`;
}

export function RelationGraphCanvas({
  nodes,
  edges,
  chapters,
  fieldLabelMap,
  selectedNodeId,
  onSelectNode,
  highlightHops,
  timelineSeq,
  onJumpToEntity,
  onJumpToChapter,
  onManageRelations,
}: RelationGraphCanvasProps) {
  const uid = useId().replace(/:/g, "");
  const containerRef = useRef<HTMLDivElement | null>(null);
  const dragRef = useRef<DragState | null>(null);
  const [canvasSize, setCanvasSize] = useState({ width: 800, height: 520 });
  const [viewport, setViewport] = useState<ViewportState>({ tx: 400, ty: 260, scale: 1 });
  const [manualPositions, setManualPositions] = useState<Record<number, GraphPoint>>({});
  const [hoveredNodeId, setHoveredNodeId] = useState<number | null>(null);
  const [draggingKind, setDraggingKind] = useState<"pan" | "node" | null>(null);

  const layout = useMemo(() => computeGraphLayout(nodes, edges), [nodes, edges]);

  // 节点集合或关系拓扑大幅变化时，仅保留仍存在的节点手动微调坐标
  useEffect(() => {
    setManualPositions((prev) => {
      const nodeIdSet = new Set(nodes.map((n) => n.id));
      let changed = false;
      const next: Record<number, GraphPoint> = {};
      for (const [rawId, pt] of Object.entries(prev)) {
        const id = Number(rawId);
        if (nodeIdSet.has(id)) {
          next[id] = pt;
        } else {
          changed = true;
        }
      }
      return changed ? next : prev;
    });
  }, [nodes]);

  const effectivePositions = useMemo(() => {
    const out: Record<number, GraphPoint> = {};
    for (const n of nodes) {
      out[n.id] = manualPositions[n.id] ?? layout.positions[n.id] ?? { x: 0, y: 0 };
    }
    return out;
  }, [nodes, manualPositions, layout.positions]);

  const fitViewport = useCallback(
    (w = canvasSize.width, h = canvasSize.height) => {
      if (nodes.length === 0) {
        setViewport({ tx: w / 2, ty: h / 2, scale: 1 });
        return;
      }
      let minX = Infinity;
      let maxX = -Infinity;
      let minY = Infinity;
      let maxY = -Infinity;
      for (const n of nodes) {
        const pt = effectivePositions[n.id] ?? { x: 0, y: 0 };
        minX = Math.min(minX, pt.x - n.radius - 28);
        maxX = Math.max(maxX, pt.x + n.radius + 28);
        minY = Math.min(minY, pt.y - n.radius - 24);
        maxY = Math.max(maxY, pt.y + n.radius + 28);
      }
      const boxW = Math.max(180, maxX - minX);
      const boxH = Math.max(160, maxY - minY);
      const pad = 48;
      const fitScale = clampScale(
        Math.min((w - pad * 2) / boxW, (h - pad * 2) / boxH, 1.18)
      );
      const cx = (minX + maxX) / 2;
      const cy = (minY + maxY) / 2;
      setViewport({
        tx: w / 2 - cx * fitScale,
        ty: h / 2 - cy * fitScale,
        scale: fitScale,
      });
    },
    [canvasSize.width, canvasSize.height, nodes, effectivePositions]
  );

  // 观测画布尺寸并在首次挂载 / 图谱关键维度变化时自适应居中
  const fittedSigRef = useRef<string>("");
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const measure = () => {
      const rect = el.getBoundingClientRect();
      if (rect.width > 0 && rect.height > 0) {
        setCanvasSize({ width: rect.width, height: rect.height });
      }
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  useEffect(() => {
    const sig = `${nodes.map((n) => n.id).join(",")}|${edges.length}`;
    if (fittedSigRef.current !== sig && canvasSize.width > 0 && canvasSize.height > 0) {
      fittedSigRef.current = sig;
      fitViewport(canvasSize.width, canvasSize.height);
    }
  }, [nodes, edges.length, canvasSize.width, canvasSize.height, fitViewport]);

  // 以鼠标所在屏幕点为锚点缩放画布（non-passive wheel listener 防止外层容器滚动）
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const rect = el.getBoundingClientRect();
      const px = e.clientX - rect.left;
      const py = e.clientY - rect.top;
      const factor = e.deltaY < 0 ? 1.12 : 0.89;
      setViewport((prev) => {
        const nextScale = clampScale(prev.scale * factor);
        const worldX = (px - prev.tx) / prev.scale;
        const worldY = (py - prev.ty) / prev.scale;
        return {
          scale: nextScale,
          tx: px - worldX * nextScale,
          ty: py - worldY * nextScale,
        };
      });
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, []);

  // 全局 mousemove / mouseup 处理平移与节点拖拽
  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      const d = dragRef.current;
      if (!d) return;
      const dx = e.clientX - d.startClientX;
      const dy = e.clientY - d.startClientY;
      if (Math.hypot(dx, dy) >= 4) {
        d.moved = true;
      }
      if (d.kind === "pan") {
        setViewport((prev) => ({
          ...prev,
          tx: d.startTx + dx,
          ty: d.startTy + dy,
        }));
      } else {
        setManualPositions((prev) => ({
          ...prev,
          [d.nodeId]: {
            x: d.startX + dx / viewport.scale,
            y: d.startY + dy / viewport.scale,
          },
        }));
      }
    };
    const onUp = () => {
      const d = dragRef.current;
      if (!d) return;
      if (d.kind === "node" && !d.moved) {
        onSelectNode(selectedNodeId === d.nodeId ? null : d.nodeId);
      } else if (d.kind === "pan" && !d.moved) {
        onSelectNode(null);
      }
      dragRef.current = null;
      setDraggingKind(null);
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    return () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
  }, [viewport.scale, onSelectNode, selectedNodeId]);

  const zoomAtCenter = (factor: number) => {
    const cx = canvasSize.width / 2;
    const cy = canvasSize.height / 2;
    setViewport((prev) => {
      const nextScale = clampScale(prev.scale * factor);
      const wx = (cx - prev.tx) / prev.scale;
      const wy = (cy - prev.ty) / prev.scale;
      return {
        scale: nextScale,
        tx: cx - wx * nextScale,
        ty: cy - wy * nextScale,
      };
    });
  };

  const focusId = selectedNodeId ?? hoveredNodeId;
  const neighborhood = useMemo(
    () => computeNeighborhood(focusId, edges, highlightHops),
    [focusId, edges, highlightHops]
  );

  const nodeMap = useMemo(() => {
    const map = new Map<number, GraphNode>();
    for (const n of nodes) map.set(n.id, n);
    return map;
  }, [nodes]);

  const chapterBySeq = useMemo(() => {
    const map = new Map<number, Chapter>();
    for (const ch of chapters) {
      map.set(ch.globalSeq, ch);
    }
    return map;
  }, [chapters]);

  const selectedNode = useMemo(
    () => (selectedNodeId ? nodeMap.get(selectedNodeId) ?? null : null),
    [nodeMap, selectedNodeId]
  );

  const selectedNodeEdges = useMemo(() => {
    if (!selectedNode) return [];
    return edges.filter(
      (e) => e.sourceId === selectedNode.id || e.targetId === selectedNode.id
    );
  }, [edges, selectedNode]);

  const handleCanvasMouseDown = (e: ReactMouseEvent<SVGSVGElement>) => {
    if (e.button !== 0) return;
    dragRef.current = {
      kind: "pan",
      startClientX: e.clientX,
      startClientY: e.clientY,
      startTx: viewport.tx,
      startTy: viewport.ty,
      moved: false,
    };
    setDraggingKind("pan");
  };

  const handleNodeMouseDown = (e: ReactMouseEvent<SVGGElement>, node: GraphNode) => {
    if (e.button !== 0) return;
    e.stopPropagation();
    const pt = effectivePositions[node.id] ?? { x: 0, y: 0 };
    dragRef.current = {
      kind: "node",
      nodeId: node.id,
      startClientX: e.clientX,
      startClientY: e.clientY,
      startX: pt.x,
      startY: pt.y,
      moved: false,
    };
    setDraggingKind("node");
  };

  return (
    <div className="relative flex min-h-[540px] w-full flex-1 overflow-hidden rounded-xl border border-border-subtle bg-canvas">
      {/* Main SVG Viewport */}
      <div
        ref={containerRef}
        className={`relative flex-1 overflow-hidden select-none ${
          draggingKind ? "cursor-grabbing" : "cursor-grab"
        }`}
      >
        <svg
          width="100%"
          height="100%"
          className="block h-full w-full"
          onMouseDown={handleCanvasMouseDown}
        >
          <defs>
            <pattern
              id={`${uid}-dots`}
              width={24}
              height={24}
              patternUnits="userSpaceOnUse"
            >
              <circle
                cx={2}
                cy={2}
                r={1}
                fill="var(--border-strong)"
                fillOpacity={0.35}
              />
            </pattern>
            {(
              [
                ["ally", "var(--status-ok)", 0.9],
                ["hostile", "var(--status-danger)", 0.9],
                ["neutral", "var(--border-strong)", 0.9],
                ["hist", "var(--text-muted)", 0.55],
              ] as const
            ).map(([suffix, fill, opacity]) => (
              <marker
                key={suffix}
                id={`${uid}-arrow-${suffix}`}
                viewBox="0 0 10 10"
                refX="8.5"
                refY="5"
                markerWidth="6.5"
                markerHeight="6.5"
                orient="auto-start-reverse"
              >
                <path d="M 0 1.2 L 9 5 L 0 8.8 z" fill={fill} fillOpacity={opacity} />
              </marker>
            ))}
          </defs>

          {/* Background Dot Grid */}
          <rect width="100%" height="100%" fill={`url(#${uid}-dots)`} />

          {/* Graph Layer */}
          <g
            transform={`translate(${viewport.tx.toFixed(2)}, ${viewport.ty.toFixed(
              2
            )}) scale(${viewport.scale.toFixed(4)})`}
          >
            {/* Edges */}
            {edges.map((edge) => {
              const sNode = nodeMap.get(edge.sourceId);
              const tNode = nodeMap.get(edge.targetId);
              const sPt = effectivePositions[edge.sourceId];
              const tPt = effectivePositions[edge.targetId];
              if (!sNode || !tNode || !sPt || !tPt) return null;

              const hop = neighborhood.edgeHops.get(edge.id);
              const dimmed = focusId !== null && !hop;
              const strokeColor = edge.isHistorical
                ? "var(--text-muted)"
                : getPolarityStroke(edge.polarity);
              const markerId = `${uid}-arrow-${getPolarityMarkerSuffix(
                edge.polarity,
                edge.isHistorical
              )}`;
              const geom = computeQuadraticEdgeGeometry(
                sPt.x,
                sPt.y,
                tPt.x,
                tPt.y,
                sNode.radius,
                tNode.radius,
                edge.curveIndex,
                edge.curveTotal
              );

              const baseOpacity = edge.isHistorical ? 0.48 : 0.86;
              const opacity = dimmed ? 0.14 : hop === 1 ? 1 : baseOpacity;

              return (
                <g key={edge.id} style={{ opacity, transition: "opacity 120ms ease" }}>
                  <path
                    d={geom.path}
                    fill="none"
                    stroke={strokeColor}
                    strokeWidth={hop === 1 ? 2.2 : 1.5}
                    strokeDasharray={edge.isHistorical ? "5 4" : undefined}
                    markerEnd={`url(#${markerId})`}
                  />
                  {/* Relation Label Pill */}
                  <g
                    transform={`translate(${geom.labelX.toFixed(1)}, ${geom.labelY.toFixed(
                      1
                    )})`}
                  >
                    <rect
                      x={-Math.max(20, edge.relation.relationType.length * 6.5 + 8)}
                      y={-9}
                      width={Math.max(40, edge.relation.relationType.length * 13 + 16)}
                      height={18}
                      rx={9}
                      fill="var(--bg-surface)"
                      stroke={strokeColor}
                      strokeWidth={0.9}
                      strokeOpacity={edge.isHistorical ? 0.45 : 0.65}
                    />
                    <text
                      textAnchor="middle"
                      dominantBaseline="central"
                      fontSize={10.5}
                      fontWeight={500}
                      fill={
                        edge.isHistorical ? "var(--text-muted)" : "var(--text-primary)"
                      }
                    >
                      {edge.relation.relationType}
                    </text>
                  </g>
                </g>
              );
            })}

            {/* Nodes */}
            {nodes.map((node) => {
              const pt = effectivePositions[node.id] ?? { x: 0, y: 0 };
              const isSelected = selectedNodeId === node.id;
              const isHop0 = neighborhood.hop0.has(node.id);
              const isHop1 = neighborhood.hop1.has(node.id);
              const isHop2 = neighborhood.hop2.has(node.id);
              const isDimmed = focusId !== null && !isHop0 && !isHop1 && !isHop2;

              const strokeColor = isSelected
                ? "var(--accent)"
                : node.isAbsentAlert
                ? "var(--status-danger)"
                : KIND_ACCENT_STROKE[node.entity.kind] ?? "var(--border-strong)";

              const absentCount = node.presence?.absentChapters ?? 0;
              const appCount = node.presence?.appearanceChapters ?? 0;

              return (
                <g
                  key={node.id}
                  transform={`translate(${pt.x.toFixed(1)}, ${pt.y.toFixed(1)})`}
                  onMouseDown={(e) => handleNodeMouseDown(e, node)}
                  onMouseEnter={() => setHoveredNodeId(node.id)}
                  onMouseLeave={() =>
                    setHoveredNodeId((prev) => (prev === node.id ? null : prev))
                  }
                  style={{
                    cursor: "pointer",
                    opacity: isDimmed ? 0.16 : isHop2 ? 0.85 : 1,
                    transition: "opacity 120ms ease",
                  }}
                >
                  {/* Selection / Hover halo */}
                  {(isSelected || isHop0) && (
                    <circle
                      r={node.radius + 7}
                      fill="none"
                      stroke="var(--accent)"
                      strokeWidth={1.6}
                      strokeOpacity={0.65}
                    />
                  )}
                  {node.isAbsentAlert && !isSelected && (
                    <circle
                      r={node.radius + 4}
                      fill="none"
                      stroke="var(--status-danger)"
                      strokeWidth={1.3}
                      strokeDasharray="3 3"
                      strokeOpacity={0.85}
                    />
                  )}

                  {/* Main Circle */}
                  <circle
                    r={node.radius}
                    fill={KIND_FILL[node.entity.kind] ?? "var(--bg-surface)"}
                    stroke={strokeColor}
                    strokeWidth={isSelected ? 2.6 : 1.9}
                  />

                  {/* Entity Name inside Circle */}
                  <text
                    y={-2}
                    textAnchor="middle"
                    dominantBaseline="central"
                    fontSize={node.entity.name.trim().length > 5 ? 10.5 : 12}
                    fontWeight={600}
                    fill="var(--text-primary)"
                  >
                    {truncateNodeLabel(node.entity.name, 8)}
                  </text>

                  {/* Kind sub-label */}
                  <text
                    y={12}
                    textAnchor="middle"
                    dominantBaseline="central"
                    fontSize={9.5}
                    fill="var(--text-muted)"
                  >
                    {getEntityKindLabel(node.entity.kind)}
                  </text>

                  {/* Faction pill below node */}
                  {node.factionLabel && node.entity.kind !== "faction" && (
                    <text
                      y={node.radius + 14}
                      textAnchor="middle"
                      fontSize={10}
                      fill="var(--text-secondary)"
                    >
                      {truncateNodeLabel(node.factionLabel, 7)}
                    </text>
                  )}

                  {/* Presence / Absent Alert Badge on top-right */}
                  {node.isAbsentAlert ? (
                    <g transform={`translate(${node.radius * 0.72}, ${-node.radius * 0.78})`}>
                      <rect
                        x={-24}
                        y={-9}
                        width={48}
                        height={18}
                        rx={9}
                        fill="var(--status-danger)"
                      />
                      <text
                        textAnchor="middle"
                        dominantBaseline="central"
                        fontSize={9.5}
                        fontWeight={600}
                        fill="#ffffff"
                      >
                        掉线{absentCount}
                      </text>
                    </g>
                  ) : (
                    appCount > 0 && (
                      <g
                        transform={`translate(${node.radius * 0.75}, ${-node.radius * 0.75})`}
                      >
                        <circle
                          r={9.5}
                          fill="var(--bg-elevated)"
                          stroke="var(--border-strong)"
                          strokeWidth={1}
                        />
                        <text
                          textAnchor="middle"
                          dominantBaseline="central"
                          fontSize={9.5}
                          fontWeight={600}
                          fill="var(--accent)"
                        >
                          {appCount}
                        </text>
                      </g>
                    )
                  )}
                </g>
              );
            })}
          </g>
        </svg>

        {/* Empty Node Guard */}
        {nodes.length === 0 && (
          <div className="pointer-events-none absolute inset-0 flex items-center justify-center px-6 text-center text-xs text-ink-muted">
            当前筛选条件下暂无符合条件的节点，可放宽筛选或勾选「显示无连线设定」
          </div>
        )}

        {/* Top-Left Legend */}
        <div className="pointer-events-none absolute top-3 left-3 flex flex-wrap items-center gap-2.5 rounded-lg border border-border-subtle bg-surface/90 px-2.5 py-1.5 text-[11px] text-ink-secondary backdrop-blur-xs">
          <span className="inline-flex items-center gap-1">
            <span className="h-2 w-2 rounded-full bg-ok" />
            同门 / 盟友
          </span>
          <span className="inline-flex items-center gap-1">
            <span className="h-2 w-2 rounded-full bg-danger" />
            死敌 / 对立
          </span>
          <span className="inline-flex items-center gap-1">
            <span className="h-2 w-2 rounded-full border border-dashed border-ink-muted" />
            历史关系
          </span>
          <span className="text-ink-muted">右上角为出场章数 / 久未出场提醒</span>
        </div>

        {/* Top-Right Zoom / Fit Controls */}
        <div className="absolute top-3 right-3 flex items-center gap-1 rounded-lg border border-border-subtle bg-surface/95 p-1 shadow-sm">
          <button
            type="button"
            onClick={() => zoomAtCenter(1.18)}
            title="放大"
            className="rounded p-1.5 text-ink-secondary hover:bg-elevated hover:text-ink-primary"
          >
            <Plus className="h-3.5 w-3.5" />
          </button>
          <button
            type="button"
            onClick={() => zoomAtCenter(0.84)}
            title="缩小"
            className="rounded p-1.5 text-ink-secondary hover:bg-elevated hover:text-ink-primary"
          >
            <Minus className="h-3.5 w-3.5" />
          </button>
          <button
            type="button"
            onClick={() => fitViewport()}
            title="适应画布"
            className="rounded p-1.5 text-ink-secondary hover:bg-elevated hover:text-ink-primary"
          >
            <Maximize2 className="h-3.5 w-3.5" />
          </button>
          {Object.keys(manualPositions).length > 0 && (
            <button
              type="button"
              onClick={() => setManualPositions({})}
              title="重置节点拖拽位置"
              className="rounded p-1.5 text-ink-secondary hover:bg-elevated hover:text-ink-primary"
            >
              <RotateCcw className="h-3.5 w-3.5" />
            </button>
          )}
          <span className="nib-mono px-1.5 text-[10.5px] text-ink-muted">
            {Math.round(viewport.scale * 100)}%
          </span>
        </div>

        {/* Bottom-Left Tip */}
        <div className="pointer-events-none absolute bottom-3 left-3 rounded-md bg-surface/85 px-2.5 py-1 text-[11px] text-ink-muted">
          拖拽空白平移 · 滚轮缩放 · 点击节点高亮{highlightHops === 1 ? "直接" : "二度"}关系 · 拖动节点可微调位置
        </div>
      </div>

      {/* Right Side Inspector Drawer when a node is selected */}
      {selectedNode && (
        <aside className="flex w-[320px] shrink-0 flex-col border-l border-border-subtle bg-surface">
          <div className="flex items-start justify-between gap-2 border-b border-border-subtle px-4 py-3">
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="truncate text-sm font-semibold text-ink-primary">
                  {selectedNode.entity.name}
                </span>
                <span className="rounded border border-border-subtle bg-canvas px-1.5 py-0.5 text-[10px] text-ink-secondary">
                  {getEntityKindLabel(selectedNode.entity.kind)}
                </span>
                {selectedNode.factionLabel && (
                  <span className="rounded bg-accent-soft px-1.5 py-0.5 text-[10px] text-accent">
                    {selectedNode.factionLabel}
                  </span>
                )}
              </div>
              {selectedNode.entity.aliases.length > 0 && (
                <div className="mt-1 truncate text-[11px] text-ink-muted">
                  别名：{selectedNode.entity.aliases.join(" / ")}
                </div>
              )}
            </div>
            <button
              type="button"
              onClick={() => onSelectNode(null)}
              className="rounded p-1 text-ink-muted hover:bg-elevated hover:text-ink-primary"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>

          <div className="flex flex-1 flex-col gap-4 overflow-y-auto px-4 py-3.5 text-xs">
            {/* Presence Radar Block */}
            <section className="rounded-lg border border-border-subtle bg-canvas/70 p-3">
              <div className="flex items-center justify-between">
                <span className="font-medium text-ink-primary">
                  出场雷达（截至第 {formatSeq(timelineSeq)} 章）
                </span>
                {selectedNode.isAbsentAlert && selectedNode.presence && (
                  <span className="inline-flex items-center gap-1 rounded bg-danger-soft px-1.5 py-0.5 text-[10.5px] font-medium text-danger">
                    <AlertTriangle className="h-3 w-3" />
                    已掉线 {selectedNode.presence.absentChapters} 章
                  </span>
                )}
              </div>
              {selectedNode.presence && !selectedNode.presence.neverAppeared ? (
                <>
                  <div className="mt-2 grid grid-cols-3 gap-2 text-center">
                    <div className="rounded bg-surface px-2 py-1.5">
                      <div className="nib-mono text-sm font-semibold text-ink-primary">
                        {selectedNode.presence.appearanceChapters}
                      </div>
                      <div className="text-[10px] text-ink-muted">出场章数</div>
                    </div>
                    <div className="rounded bg-surface px-2 py-1.5">
                      <div className="nib-mono text-sm font-semibold text-ink-primary">
                        {selectedNode.presence.mentionCount}
                      </div>
                      <div className="text-[10px] text-ink-muted">正文提及</div>
                    </div>
                    <div className="rounded bg-surface px-2 py-1.5">
                      <div className="nib-mono text-sm font-semibold text-ink-primary">
                        {selectedNode.presence.absentChapters}
                      </div>
                      <div className="text-[10px] text-ink-muted">掉线章数</div>
                    </div>
                  </div>
                  <div className="mt-2.5 flex flex-wrap gap-1">
                    {selectedNode.presence.appearedChapterSeqs.slice(-16).map((seq) => {
                      const ch = chapterBySeq.get(seq);
                      return (
                        <button
                          key={seq}
                          type="button"
                          disabled={!ch}
                          onClick={() => ch && onJumpToChapter(ch.id)}
                          title={ch ? `跳转到第 ${formatSeq(seq)} 章：${ch.title}` : `第 ${seq} 章`}
                          className="nib-mono rounded border border-border-subtle bg-surface px-1.5 py-0.5 text-[10.5px] text-ink-secondary hover:border-accent hover:text-accent disabled:opacity-50"
                        >
                          {formatSeq(seq)}
                        </button>
                      );
                    })}
                    {selectedNode.presence.appearedChapterSeqs.length > 16 && (
                      <span className="self-center text-[10px] text-ink-muted">
                        共 {selectedNode.presence.appearedChapterSeqs.length} 章
                      </span>
                    )}
                  </div>
                </>
              ) : (
                <div className="mt-1.5 text-[11px] text-ink-muted">
                  截至当前时间点尚无正文或节拍出场记录
                </div>
              )}
            </section>

            {/* Effective Attributes Snapshot */}
            <section className="flex flex-col gap-1.5">
              <div className="font-medium text-ink-primary">此刻核心属性</div>
              {Object.keys(
                selectedNode.snapshot?.effectiveAttributes ??
                  selectedNode.entity.attributes ??
                  {}
              ).length === 0 ? (
                <div className="text-[11px] text-ink-muted">暂无属性记录</div>
              ) : (
                <div className="flex flex-wrap gap-1.5">
                  {Object.entries(
                    selectedNode.snapshot?.effectiveAttributes ??
                      selectedNode.entity.attributes ??
                      {}
                  ).map(([k, v]) => {
                    const spec = fieldLabelMap.get(k);
                    const changed = (selectedNode.entity.attributes?.[k] ?? "") !== v;
                    return (
                      <span
                        key={k}
                        className={`inline-flex max-w-full items-center gap-1 rounded px-2 py-0.5 text-[11px] ${
                          changed
                            ? "border border-accent/40 bg-accent-soft text-ink-primary"
                            : "bg-canvas text-ink-secondary"
                        }`}
                      >
                        <span className="text-ink-muted">{spec?.label ?? k}:</span>
                        <span className="truncate">{v}</span>
                      </span>
                    );
                  })}
                </div>
              )}
              {selectedNode.entity.summary && (
                <p className="mt-1 text-[11.5px] leading-relaxed text-ink-secondary">
                  {selectedNode.entity.summary}
                </p>
              )}
            </section>

            {/* Direct 1-hop Relations */}
            <section className="flex flex-col gap-1.5">
              <div className="flex items-center justify-between">
                <span className="font-medium text-ink-primary">
                  直接关系（{selectedNodeEdges.length}）
                </span>
                <span className="text-[10.5px] text-ink-muted">点击对方切换焦点</span>
              </div>
              {selectedNodeEdges.length === 0 ? (
                <div className="text-[11px] text-ink-muted">当前时间点暂无关系记录</div>
              ) : (
                <div className="flex flex-col gap-1.5">
                  {selectedNodeEdges.map((edge) => {
                    const isOutgoing = edge.sourceId === selectedNode.id;
                    const peerId = isOutgoing ? edge.targetId : edge.sourceId;
                    const peerName = isOutgoing
                      ? edge.relation.targetName || `#${peerId}`
                      : edge.relation.sourceName || `#${peerId}`;
                    return (
                      <button
                        key={edge.id}
                        type="button"
                        onClick={() => onSelectNode(peerId)}
                        className={`flex flex-col gap-1 rounded-lg border px-2.5 py-2 text-left transition-colors hover:border-accent/60 ${
                          edge.isHistorical
                            ? "border-dashed border-border-subtle bg-canvas/40 opacity-75"
                            : "border-border-subtle bg-canvas/75"
                        }`}
                      >
                        <div className="flex items-center justify-between gap-2">
                          <div className="flex items-center gap-1 text-[11.5px] font-medium text-ink-primary">
                            {isOutgoing ? (
                              <>
                                <span className="text-accent">
                                  {edge.relation.relationType}
                                </span>
                                <ArrowRight className="h-3 w-3 text-ink-muted" />
                                <span>{peerName}</span>
                              </>
                            ) : (
                              <>
                                <span>{peerName}</span>
                                <ArrowRight className="h-3 w-3 text-ink-muted" />
                                <span className="text-accent">
                                  {edge.relation.relationType}
                                </span>
                              </>
                            )}
                          </div>
                          {edge.isHistorical && (
                            <span className="rounded bg-surface px-1 py-0.5 text-[10px] text-ink-muted">
                              已终止
                            </span>
                          )}
                        </div>
                        {edge.relation.description && (
                          <div className="line-clamp-2 text-[11px] text-ink-secondary">
                            {edge.relation.description}
                          </div>
                        )}
                      </button>
                    );
                  })}
                </div>
              )}
            </section>
          </div>

          <div className="flex items-center justify-between gap-2 border-t border-border-subtle bg-surface/70 px-4 py-2.5">
            <button
              type="button"
              onClick={() => onSelectNode(null)}
              className="flex items-center gap-1 rounded-md px-2 py-1 text-xs text-ink-secondary hover:bg-elevated hover:text-ink-primary"
            >
              <Crosshair className="h-3.5 w-3.5" />
              <span>取消</span>
            </button>
            <div className="flex items-center gap-1.5">
              {onManageRelations && (
                <button
                  type="button"
                  onClick={() => onManageRelations(selectedNode.id)}
                  className="rounded-md border border-border-subtle bg-canvas px-2.5 py-1.5 text-xs font-medium text-ink-primary transition-colors hover:border-border-strong"
                >
                  管理关系
                </button>
              )}
              <button
                type="button"
                onClick={() => onJumpToEntity(selectedNode.id)}
                className="flex items-center gap-1.5 rounded-md bg-accent px-3 py-1.5 text-xs font-semibold text-canvas hover:opacity-90"
              >
                <ExternalLink className="h-3.5 w-3.5" />
                <span>编辑档案</span>
              </button>
            </div>
          </div>
        </aside>
      )}
    </div>
  );
}
