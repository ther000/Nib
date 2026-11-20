import type {
  ArcSummaryLevel,
  BeatStage,
  ChapterStatus,
  CriticDimension,
  DeaiCategory,
  DeltaForeshadowAction,
  EmotionTag,
  EntityKind,
  Foreshadowing,
  ForeshadowingStatus,
  HookType,
  LintSeverity,
  PipelineStage,
  PovRule,
  ProviderProtocol,
  SceneType,
  StateEventStatus,
  VersionSource,
} from "../mygo";

export type WorkspaceTab = "architect" | "studio" | "atlas";
export type AtlasSubTab = "entities" | "foreshadowings" | "summaries" | "style";

export interface OptionMeta<T extends string = string> {
  value: T;
  label: string;
  shortLabel?: string;
  hint?: string;
}

export const EMOTION_TAGS: OptionMeta<EmotionTag>[] = [
  { value: "setup", label: "铺垫建场", shortLabel: "铺垫", hint: "引入新地图、人物或规则" },
  { value: "suppress", label: "蓄压受阻", shortLabel: "蓄压", hint: "遇强敌、受限或信息劣势" },
  { value: "turn", label: "反转破局", shortLabel: "反转", hint: "信息差或底牌逆转局势" },
  { value: "burst", label: "高潮爆发", shortLabel: "爆发", hint: "正面交锋与情绪爽感峰值" },
  { value: "harvest", label: "落袋留悬", shortLabel: "收割", hint: "清点收获并抛出新隐患" },
];

export const HOOK_TYPES: OptionMeta<HookType>[] = [
  { value: "mystery", label: "悬念揭露", hint: "末段抛出反常识线索或异象" },
  { value: "crisis", label: "突发危机", hint: "强敌降临、期限逼近或后院起火" },
  { value: "counterattack_eve", label: "反击前夜", hint: "刚布好局或拿到关键道具待清算" },
  { value: "reward_pending", label: "利益待拆", hint: "刚获秘宝权限，收获留待下章拆解" },
  { value: "new_arc", label: "转场开篇", hint: "旧矛盾收束，切入新地图或邀约" },
];

export const BEAT_STAGES: OptionMeta<BeatStage>[] = [
  { value: "setup", label: "起手建场", shortLabel: "起手" },
  { value: "target", label: "立靶冲突", shortLabel: "立靶" },
  { value: "suppress", label: "压制试探", shortLabel: "压制" },
  { value: "turn", label: "转折破局", shortLabel: "转折" },
  { value: "burst", label: "动作爆发", shortLabel: "爆发" },
  { value: "harvest", label: "收割结算", shortLabel: "收割" },
  { value: "hook", label: "章末留钩", shortLabel: "留钩" },
];

export const ENTITY_KINDS: OptionMeta<EntityKind>[] = [
  { value: "character", label: "角色" },
  { value: "faction", label: "势力" },
  { value: "location", label: "地点" },
  { value: "item", label: "道具" },
  { value: "system", label: "体系" },
  { value: "rule", label: "规则" },
];

export const CHAPTER_STATUS_META: Record<
  string,
  { label: string; dotClass: string; badgeClass: string }
> = {
  planned: {
    label: "待写",
    dotClass: "bg-ink-muted",
    badgeClass: "bg-elevated text-ink-secondary border-border-subtle",
  },
  outlined: {
    label: "已有大纲",
    dotClass: "bg-ink-secondary",
    badgeClass: "bg-elevated text-ink-primary border-border-subtle",
  },
  drafting: {
    label: "写作中",
    dotClass: "bg-accent animate-pulse",
    badgeClass: "bg-accent-soft text-accent border-accent/30",
  },
  partial: {
    label: "未写完",
    dotClass: "bg-warn",
    badgeClass: "bg-warn-soft text-warn border-warn/30",
  },
  reviewing: {
    label: "待定稿",
    dotClass: "bg-accent",
    badgeClass: "bg-accent-soft text-accent border-accent/30",
  },
  finalized: {
    label: "已定稿",
    dotClass: "bg-ok",
    badgeClass: "bg-ok-soft text-ok border-ok/30",
  },
  upstream_dirty: {
    label: "前文有变动",
    dotClass: "bg-warn",
    badgeClass: "bg-warn-soft text-warn border-warn/40 font-medium",
  },
};

export const FORESHADOWING_STATUSES: Array<
  OptionMeta<ForeshadowingStatus> & { badgeClass: string; dotClass: string }
> = [
  {
    value: "pending_plant",
    label: "待埋设",
    badgeClass: "bg-elevated text-ink-secondary border-border-subtle",
    dotClass: "bg-ink-muted",
  },
  {
    value: "planted",
    label: "已埋设",
    badgeClass: "bg-accent-soft text-accent border-accent/30",
    dotClass: "bg-accent",
  },
  {
    value: "hinted",
    label: "已暗示",
    badgeClass: "bg-warn-soft text-warn border-warn/30",
    dotClass: "bg-warn",
  },
  {
    value: "resolved",
    label: "已回收",
    badgeClass: "bg-ok-soft text-ok border-ok/30",
    dotClass: "bg-ok",
  },
  {
    value: "abandoned",
    label: "已废弃",
    badgeClass: "bg-elevated text-ink-muted border-border-subtle",
    dotClass: "bg-ink-muted/60",
  },
];

export const ARC_SUMMARY_LEVELS: OptionMeta<ArcSummaryLevel>[] = [
  { value: "arc_10", label: "10 章弧线摘要", shortLabel: "10章弧线" },
  { value: "volume", label: "卷级摘要", shortLabel: "卷摘要" },
];

export const DELTA_FORESHADOW_ACTION_META: Record<
  DeltaForeshadowAction,
  { label: string; badgeClass: string }
> = {
  plant: { label: "埋设新伏笔", badgeClass: "bg-accent-soft text-accent border-accent/30" },
  hint: { label: "推进暗示", badgeClass: "bg-warn-soft text-warn border-warn/30" },
  resolve: { label: "回收关闭", badgeClass: "bg-ok-soft text-ok border-ok/30" },
  abandon: { label: "废弃关闭", badgeClass: "bg-elevated text-ink-muted border-border-subtle" },
};

export const STATE_EVENT_STATUS_LABELS: Record<StateEventStatus, string> = {
  pending: "待审",
  committed: "已入库",
  reverted: "已撤回",
};

export const SCENE_TYPES: OptionMeta<SceneType>[] = [
  { value: "combat", label: "战斗交锋", shortLabel: "战斗" },
  { value: "dialogue", label: "博弈对话", shortLabel: "对话" },
  { value: "daily", label: "过渡日常", shortLabel: "日常" },
  { value: "suspense", label: "探秘悬念", shortLabel: "悬念" },
];

export const DEAI_CATEGORIES: OptionMeta<DeaiCategory>[] = [
  { value: "cliche", label: "高频套话", shortLabel: "套话" },
  { value: "structure", label: "句式结构病", shortLabel: "结构" },
  { value: "formatting", label: "排版与推进病", shortLabel: "排版" },
];

export const LINT_SEVERITIES: Array<
  OptionMeta<LintSeverity> & { badgeClass: string }
> = [
  {
    value: "error",
    label: "严重",
    badgeClass: "bg-danger-soft text-danger border-danger/35",
  },
  {
    value: "warn",
    label: "警告",
    badgeClass: "bg-warn-soft text-warn border-warn/35",
  },
  {
    value: "info",
    label: "提示",
    badgeClass: "bg-elevated text-ink-secondary border-border-subtle",
  },
];

export const POV_RULES: OptionMeta<PovRule>[] = [
  { value: "third_limited", label: "第三人称限知" },
  { value: "first_person", label: "第一人称" },
  { value: "third_omniscient", label: "第三人称全知" },
];

export const CRITIC_DIMENSIONS: Array<
  OptionMeta<CriticDimension> & { badgeClass: string }
> = [
  {
    value: "consistency",
    label: "防吃书一致性",
    shortLabel: "防吃书",
    badgeClass: "bg-danger-soft text-danger border-danger/35",
  },
  {
    value: "poison_point",
    label: "男频毒点排查",
    shortLabel: "防毒",
    badgeClass: "bg-warn-soft text-warn border-warn/35",
  },
  {
    value: "beat_coverage",
    label: "节拍落点核验",
    shortLabel: "节拍",
    badgeClass: "bg-accent-soft text-accent border-accent/35",
  },
  {
    value: "hook",
    label: "章末钩子强检",
    shortLabel: "钩子",
    badgeClass: "bg-ok-soft text-ok border-ok/35",
  },
];

export const PIPELINE_STAGES: Array<{
  stage: PipelineStage;
  label: string;
  desc: string;
  defaultTemp: number;
  defaultMaxTokens: number;
}> = [
  { stage: "outline", label: "大纲构思", desc: "生成章节大纲与剧情节拍", defaultTemp: 0.65, defaultMaxTokens: 4096 },
  { stage: "draft", label: "正文写作", desc: "按大纲撰写章节正文", defaultTemp: 0.8, defaultMaxTokens: 4096 },
  { stage: "critic", label: "审校检查", desc: "检查设定冲突、毒点与结尾悬念", defaultTemp: 0.2, defaultMaxTokens: 2048 },
  { stage: "polish", label: "局部润色", desc: "改写生硬段落，保持上下文连贯", defaultTemp: 0.55, defaultMaxTokens: 2048 },
  { stage: "extract", label: "设定提取", desc: "完章后自动整理人物变化与伏笔", defaultTemp: 0.1, defaultMaxTokens: 2048 },
];

export const PROVIDER_PROTOCOLS: Array<{
  value: ProviderProtocol;
  label: string;
  defaultBaseUrl: string;
  sampleModels: string;
}> = [
  {
    value: "openai_compat",
    label: "OpenAI 兼容接口",
    defaultBaseUrl: "https://api.deepseek.com/v1",
    sampleModels: "deepseek-chat\ndeepseek-reasoner",
  },
  {
    value: "anthropic",
    label: "Anthropic (Claude)",
    defaultBaseUrl: "https://api.anthropic.com",
    sampleModels: "claude-sonnet-4-20250514\nclaude-3-7-sonnet-latest",
  },
  {
    value: "gemini",
    label: "Google Gemini",
    defaultBaseUrl: "https://generativelanguage.googleapis.com",
    sampleModels: "gemini-2.5-pro\ngemini-2.5-flash",
  },
];

export const VERSION_SOURCE_LABELS: Record<VersionSource, string> = {
  ai_draft: "AI 生成",
  manual_backup: "手动保存",
  surgical_polish: "局部润色",
  restore_backup: "恢复前备份",
};

export function getEmotionMeta(tag: EmotionTag): OptionMeta<EmotionTag> {
  return EMOTION_TAGS.find((item) => item.value === tag) ?? { value: tag, label: tag || "铺垫建场", shortLabel: tag || "铺垫" };
}

export function getHookMeta(hook: HookType): OptionMeta<HookType> {
  return HOOK_TYPES.find((item) => item.value === hook) ?? { value: hook, label: hook || "悬念揭露" };
}

export function getBeatStageMeta(stage: BeatStage): OptionMeta<BeatStage> {
  return BEAT_STAGES.find((item) => item.value === stage) ?? { value: stage, label: stage || "节拍", shortLabel: stage || "节拍" };
}

export function getEntityKindLabel(kind: EntityKind): string {
  return ENTITY_KINDS.find((item) => item.value === kind)?.label ?? kind;
}

export function getChapterStatusMeta(status: ChapterStatus) {
  return (
    CHAPTER_STATUS_META[status] ?? {
      label: status || "规划中",
      dotClass: "bg-ink-muted",
      badgeClass: "bg-elevated text-ink-secondary border-border-subtle",
    }
  );
}

export function getVersionSourceLabel(source: VersionSource): string {
  return VERSION_SOURCE_LABELS[source] ?? source;
}

export function getForeshadowingStatusMeta(status: ForeshadowingStatus) {
  return (
    FORESHADOWING_STATUSES.find((s) => s.value === status) ?? {
      value: status,
      label: status || "已埋设",
      badgeClass: "bg-elevated text-ink-secondary border-border-subtle",
      dotClass: "bg-ink-muted",
    }
  );
}

export function getDeltaForeshadowActionMeta(action: DeltaForeshadowAction) {
  return (
    DELTA_FORESHADOW_ACTION_META[action] ?? {
      label: action || "伏笔变更",
      badgeClass: "bg-elevated text-ink-secondary border-border-subtle",
    }
  );
}

export function getArcSummaryLevelLabel(level: ArcSummaryLevel): string {
  return ARC_SUMMARY_LEVELS.find((l) => l.value === level)?.label ?? level;
}

export function isForeshadowingOpen(status: ForeshadowingStatus): boolean {
  return status === "pending_plant" || status === "planted" || status === "hinted";
}

export function isForeshadowingOverdue(item: Foreshadowing, currentSeq: number): boolean {
  return (
    isForeshadowingOpen(item.status) &&
    item.targetResolveSeq > 0 &&
    currentSeq > item.targetResolveSeq
  );
}

export function isForeshadowingApproaching(item: Foreshadowing, currentSeq: number): boolean {
  return (
    isForeshadowingOpen(item.status) &&
    item.targetResolveSeq > 0 &&
    currentSeq > 0 &&
    currentSeq <= item.targetResolveSeq &&
    item.targetResolveSeq - currentSeq <= 3
  );
}

export function parseKeywordList(raw: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const part of raw.split(/[\n,，、;；]+/)) {
    const trimmed = part.trim();
    if (trimmed && !seen.has(trimmed)) {
      seen.add(trimmed);
      out.push(trimmed);
    }
  }
  return out;
}

export function getSceneTypeLabel(scene: SceneType): string {
  return SCENE_TYPES.find((s) => s.value === scene)?.label ?? scene;
}

export function getDeaiCategoryLabel(cat: DeaiCategory): string {
  return DEAI_CATEGORIES.find((c) => c.value === cat)?.label ?? cat;
}

export function getLintSeverityMeta(sev: LintSeverity) {
  return (
    LINT_SEVERITIES.find((s) => s.value === sev) ?? {
      value: sev,
      label: sev || "警告",
      badgeClass: "bg-warn-soft text-warn border-warn/35",
    }
  );
}

export function getCriticDimensionMeta(dim: CriticDimension) {
  return (
    CRITIC_DIMENSIONS.find((d) => d.value === dim) ?? {
      value: dim,
      label: dim || "审校",
      shortLabel: dim || "审校",
      badgeClass: "bg-elevated text-ink-secondary border-border-subtle",
    }
  );
}

/**
 * Splits chapter prose into non-empty trimmed paragraphs [P0..PN],
 * matching Go's linter.SplitParagraphs 100%.
 */
export function splitParagraphs(text: string): string[] {
  if (!text) return [];
  return text
    .split(/\r?\n+/)
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
}

export function getEmotionColorClass(tag: EmotionTag): string {
  switch (tag) {
    case "burst":
      return "text-accent bg-accent-soft border-accent/30";
    case "turn":
      return "text-ok bg-ok-soft border-ok/30";
    case "suppress":
      return "text-warn bg-warn-soft border-warn/30";
    case "harvest":
      return "text-ink-primary bg-elevated border-border-strong";
    default:
      return "text-ink-secondary bg-elevated border-border-subtle";
  }
}

export function getBeatAccentClass(stage: BeatStage): string {
  switch (stage) {
    case "burst":
      return "border-l-accent";
    case "turn":
      return "border-l-ok";
    case "suppress":
      return "border-l-warn";
    case "hook":
      return "border-l-danger";
    default:
      return "border-l-border-strong";
  }
}

export function countWords(text: string): number {
  if (!text) return 0;
  const compact = text.replace(/\s+/g, "");
  return Array.from(compact).length;
}

export function formatNumber(n: number): string {
  if (!Number.isFinite(n)) return "0";
  return Math.round(n).toLocaleString("zh-CN");
}

export function formatCompactTokens(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "0";
  if (n >= 100_000_000) {
    return `${(n / 100_000_000).toFixed(2)} 亿`;
  }
  if (n >= 10_000) {
    return `${(n / 10_000).toFixed(n >= 100_000 ? 1 : 2)} 万`;
  }
  return Math.round(n).toLocaleString("zh-CN");
}

export function formatLatency(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return "—";
  if (ms >= 1000) {
    return `${(ms / 1000).toFixed(1)} 秒`;
  }
  return `${Math.round(ms)} 毫秒`;
}

export function formatSeq(seq: number): string {
  return String(seq).padStart(3, "0");
}

export function formatTimestamp(iso: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const mm = String(d.getMonth() + 1).padStart(2, "0");
  const dd = String(d.getDate()).padStart(2, "0");
  const hh = String(d.getHours()).padStart(2, "0");
  const mi = String(d.getMinutes()).padStart(2, "0");
  return `${mm}-${dd} ${hh}:${mi}`;
}

export function parseModelList(raw: string): string[] {
  const seen = new Set<string>();
  const list: string[] = [];
  for (const part of raw.split(/[\n,，]+/)) {
    const trimmed = part.trim();
    if (trimmed && !seen.has(trimmed)) {
      seen.add(trimmed);
      list.push(trimmed);
    }
  }
  return list;
}

export function formatErrorMessage(err: unknown): string {
  if (!err) return "操作未成功，请稍后重试";
  const raw = err instanceof Error ? err.message : String(err);
  const lower = raw.toLowerCase();

  if (lower.includes("401") || lower.includes("invalid api key") || lower.includes("unauthorized")) {
    return "密钥验证未通过，请检查模型服务中的密钥是否正确";
  }
  if (lower.includes("429") || lower.includes("rate limit")) {
    return "请求过于频繁或额度不足，请稍后再试或更换模型";
  }
  if (lower.includes("model route not configured") || lower.includes("provider not configured")) {
    return "尚未指定写作模型，请先在设置中配置模型服务并启用模型";
  }
  if (lower.includes("export directory not configured")) {
    return "还没设置本地备份文件夹，请先在设置中选择导出位置";
  }
  if (lower.includes("context window exceeded")) {
    return "参考资料超出模型上限，请在右侧「参考资料」中排除部分设定后重试";
  }
  if (lower.includes("runtime not found")) {
    return "请在 Nib 桌面客户端内运行";
  }
  const validationPrefix = /^validation failed(?: on [^:]+)?:\s*/i;
  if (validationPrefix.test(raw)) {
    return raw.replace(validationPrefix, "");
  }
  return raw;
}
