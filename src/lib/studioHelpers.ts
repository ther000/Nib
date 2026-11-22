import type {
  Beat,
  CriticFinding,
  Entity,
  EntitySnapshot,
  LintIssue,
} from "../mygo";

export interface SensitiveImportEntry {
  pattern: string;
  replacement: string;
}

/**
 * 解析批量导入敏感词文本：
 * - 单行支持 `原词=替换词`、`原词->替换词`、`原词=>替换词`、`原词：替换词`
 * - 无替换分隔符时，支持按逗号/顿号/分号拆分多个敏感词
 */
export function parseSensitiveImportLines(raw: string): SensitiveImportEntry[] {
  if (!raw || !raw.trim()) return [];
  const seen = new Set<string>();
  const out: SensitiveImportEntry[] = [];

  const pushPattern = (pat: string, rep: string) => {
    const pattern = pat.trim();
    const replacement = rep.trim();
    if (!pattern || seen.has(pattern)) return;
    seen.add(pattern);
    out.push({ pattern, replacement });
  };

  for (const line of raw.split(/\r?\n+/)) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const sepMatch = /^(.+?)(?:=>|->|=|：)(.*)$/.exec(trimmed);
    if (sepMatch) {
      pushPattern(sepMatch[1] ?? "", sepMatch[2] ?? "");
      continue;
    }
    for (const part of trimmed.split(/[,，、;；]+/)) {
      pushPattern(part, "");
    }
  }
  return out;
}

/**
 * 将本地 Lint 告警在正文原位置替换（优先使用段落内 Rune 区间校验，漂移时回退 matchedText 首次匹配），
 * 保留原文段落布局与未命中区域的换行符。
 */
export function applyLintReplacementInContent(
  content: string,
  issue: Pick<LintIssue, "paragraphIndex" | "startRune" | "endRune" | "matchedText">,
  replacement: string
): { content: string; replaced: boolean } {
  if (!content || issue.paragraphIndex < 0) {
    return { content, replaced: false };
  }

  let nonEmptyIdx = 0;
  let pos = 0;
  while (pos <= content.length) {
    const nl = content.indexOf("\n", pos);
    const lineEnd = nl === -1 ? content.length : nl;
    let rawLineEnd = lineEnd;
    if (rawLineEnd > pos && content[rawLineEnd - 1] === "\r") {
      rawLineEnd--;
    }

    const line = content.slice(pos, rawLineEnd);
    const trimmed = line.trim();
    if (trimmed.length > 0) {
      if (nonEmptyIdx === issue.paragraphIndex) {
        const leadingLen = line.length - line.trimStart().length;
        const trimStart = pos + leadingLen;
        const trimEnd = trimStart + trimmed.length;
        const runes = Array.from(trimmed);

        let nextTrimmed: string | null = null;
        if (
          issue.startRune >= 0 &&
          issue.endRune <= runes.length &&
          issue.startRune < issue.endRune
        ) {
          const slice = runes.slice(issue.startRune, issue.endRune).join("");
          if (!issue.matchedText || slice === issue.matchedText) {
            nextTrimmed =
              runes.slice(0, issue.startRune).join("") +
              replacement +
              runes.slice(issue.endRune).join("");
          }
        }

        if (nextTrimmed === null && issue.matchedText) {
          const fallbackIdx = trimmed.indexOf(issue.matchedText);
          if (fallbackIdx >= 0) {
            nextTrimmed =
              trimmed.slice(0, fallbackIdx) +
              replacement +
              trimmed.slice(fallbackIdx + issue.matchedText.length);
          }
        }

        if (nextTrimmed === null) {
          return { content, replaced: false };
        }
        return {
          content:
            content.slice(0, trimStart) + nextTrimmed + content.slice(trimEnd),
          replaced: true,
        };
      }
      nonEmptyIdx++;
    }

    if (nl === -1) break;
    pos = nl + 1;
  }

  return { content, replaced: false };
}

export interface MatchedEntityItem {
  snapshot: EntitySnapshot;
  mentionCount: number;
  inBeats: boolean;
}

function collectEntityMatchTerms(
  ent: Pick<Entity, "name" | "aliases">
): string[] {
  const terms = new Set<string>();
  const mainName = ent.name.trim();
  if (mainName) terms.add(mainName);
  for (const alias of ent.aliases ?? []) {
    const a = alias.trim();
    if (Array.from(a).length >= 2) {
      terms.add(a);
    }
  }
  return Array.from(terms).sort((a, b) => b.length - a.length);
}

function countNonOverlappingOccurrences(text: string, term: string): number {
  if (!text || !term) return 0;
  let count = 0;
  let idx = 0;
  while ((idx = text.indexOf(term, idx)) !== -1) {
    count++;
    idx += term.length;
  }
  return count;
}

/**
 * 统计正文提及的设定实体与本章节拍卡绑定实体，按相关性与出现频次排序。
 */
export function matchChapterEntities(
  content: string,
  beats: Beat[],
  snapshots: EntitySnapshot[]
): MatchedEntityItem[] {
  const beatEntityIDs = new Set<number>();
  for (const b of beats) {
    if (b.povEntityId && b.povEntityId > 0) {
      beatEntityIDs.add(b.povEntityId);
    }
    for (const id of b.entityIds ?? []) {
      beatEntityIDs.add(id);
    }
  }

  const items: MatchedEntityItem[] = snapshots.map((snapshot) => {
    const terms = collectEntityMatchTerms(snapshot.entity);
    let mentionCount = 0;
    for (const term of terms) {
      mentionCount += countNonOverlappingOccurrences(content, term);
    }
    return {
      snapshot,
      mentionCount,
      inBeats: beatEntityIDs.has(snapshot.entity.id),
    };
  });

  items.sort((a, b) => {
    const aActive = a.mentionCount > 0 || a.inBeats ? 1 : 0;
    const bActive = b.mentionCount > 0 || b.inBeats ? 1 : 0;
    if (aActive !== bActive) return bActive - aActive;
    if (a.mentionCount !== b.mentionCount) return b.mentionCount - a.mentionCount;
    if (a.inBeats !== b.inBeats) return a.inBeats ? -1 : 1;
    if (a.snapshot.entity.isInvariant !== b.snapshot.entity.isInvariant) {
      return a.snapshot.entity.isInvariant ? -1 : 1;
    }
    return a.snapshot.entity.id - b.snapshot.entity.id;
  });

  return items;
}

export interface EntityTextSpan {
  start: number;
  end: number;
  snapshot: EntitySnapshot;
  matchedTerm: string;
}

/**
 * 在校对态段落文本中标记实体提及（最长匹配优先，自动跳过已阻塞的 Lint 高亮区间）。
 */
export function resolveEntitySpans(
  runes: string[],
  entities: EntitySnapshot[],
  blockedRanges: Array<{ start: number; end: number }>
): EntityTextSpan[] {
  if (runes.length === 0 || entities.length === 0) return [];
  const text = runes.join("");

  const termEntries: Array<{ term: string; runeLen: number; snapshot: EntitySnapshot }> = [];
  for (const snap of entities) {
    for (const term of collectEntityMatchTerms(snap.entity)) {
      const runeLen = Array.from(term).length;
      if (runeLen < 2) continue;
      termEntries.push({ term, runeLen, snapshot: snap });
    }
  }
  termEntries.sort((a, b) => b.runeLen - a.runeLen);

  // 预先构建 UTF-16 code unit 到 rune 索引的映射，避免反复切片分配
  const charToRune = new Int32Array(text.length + 1);
  let rIdx = 0;
  for (let i = 0; i < runes.length; i++) {
    const unitLen = runes[i]!.length;
    for (let u = 0; u < unitLen; u++) {
      charToRune[rIdx + u] = i;
    }
    rIdx += unitLen;
  }
  charToRune[text.length] = runes.length;

  const isBlocked = (start: number, end: number, used: EntityTextSpan[]): boolean => {
    for (const b of blockedRanges) {
      if (start < b.end && end > b.start) return true;
    }
    for (const u of used) {
      if (start < u.end && end > u.start) return true;
    }
    return false;
  };

  const out: EntityTextSpan[] = [];
  for (const entry of termEntries) {
    let from = 0;
    while ((from = text.indexOf(entry.term, from)) !== -1) {
      const start = charToRune[from] ?? 0;
      const end = start + entry.runeLen;
      if (end <= runes.length && !isBlocked(start, end, out)) {
        out.push({
          start,
          end,
          snapshot: entry.snapshot,
          matchedTerm: entry.term,
        });
      }
      from += entry.term.length;
    }
  }

  out.sort((a, b) => a.start - b.start);
  return out;
}

export type BeatCoverageStatus = "covered" | "weak" | "missing";

export interface BeatAlignmentItem {
  beat: Beat;
  beatIndex: number;
  startPara: number | null;
  endPara: number | null;
  matchedTerms: string[];
  status: BeatCoverageStatus;
  criticHint: string;
}

const STOP_KEYWORDS = new Set([
  "主角",
  "众人",
  "自己",
  "对方",
  "什么",
  "怎么",
  "然后",
  "随后",
  "接着",
  "突然",
  "忽然",
  "已经",
  "开始",
  "决定",
  "准备",
  "进行",
  "发生",
  "发现",
  "出现",
  "离开",
  "返回",
  "进入",
  "通过",
  "利用",
  "因为",
  "所以",
]);

function extractBeatTerms(beat: Beat, entityById: Map<number, Entity>): {
  entityTerms: string[];
  keywords: string[];
} {
  const entityTerms: string[] = [];
  const allIDs = new Set<number>(beat.entityIds ?? []);
  if (beat.povEntityId && beat.povEntityId > 0) {
    allIDs.add(beat.povEntityId);
  }
  for (const id of allIDs) {
    const ent = entityById.get(id);
    if (!ent) continue;
    if (ent.name.trim()) entityTerms.push(ent.name.trim());
    for (const al of ent.aliases ?? []) {
      const a = al.trim();
      if (Array.from(a).length >= 2) entityTerms.push(a);
    }
  }

  const kwSet = new Set<string>();
  const text = `${beat.summary ?? ""} ${beat.conflictNote ?? ""}`;
  const segments = text.match(/[\u4e00-\u9fff]{2,}/g) ?? [];
  for (const seg of segments) {
    if (Array.from(seg).length <= 4 && !STOP_KEYWORDS.has(seg)) {
      kwSet.add(seg);
      continue;
    }
    const chars = Array.from(seg);
    for (let i = 0; i <= chars.length - 2; i++) {
      const bigram = chars.slice(i, i + 2).join("");
      if (!STOP_KEYWORDS.has(bigram)) kwSet.add(bigram);
      if (i <= chars.length - 3) {
        const trigram = chars.slice(i, i + 3).join("");
        if (!STOP_KEYWORDS.has(trigram)) kwSet.add(trigram);
      }
    }
  }

  return {
    entityTerms: Array.from(new Set(entityTerms)),
    keywords: Array.from(kwSet).slice(0, 24),
  };
}

/**
 * 0 成本本地节拍进度对齐：按节拍先后顺序、出场实体与关键词估算每拍在正文中的覆盖区间，
 * 若存在 Critic 的 beat_coverage 意见则一并挂载提醒。
 */
export function alignBeatsToParagraphs(
  beats: Beat[],
  paragraphs: string[],
  entities: Entity[],
  criticFindings: CriticFinding[]
): BeatAlignmentItem[] {
  if (beats.length === 0) return [];
  const entityById = new Map(entities.map((e) => [e.id, e]));
  const beatFindings = criticFindings.filter((f) => f.dimension === "beat_coverage");

  const matchCriticHint = (beat: Beat, idx: number): string => {
    const seqLabel = `第${idx + 1}拍`;
    const summaryPrefix = (beat.summary || "").trim().slice(0, 6);
    const hit = beatFindings.find((f) => {
      const hay = `${f.title} ${f.detail} ${f.suggestion}`;
      return (
        hay.includes(seqLabel) ||
        hay.includes(`节拍${idx + 1}`) ||
        hay.includes(`#${idx + 1}`) ||
        (summaryPrefix.length >= 2 && hay.includes(summaryPrefix))
      );
    });
    return hit ? `${hit.title}${hit.detail ? `：${hit.detail}` : ""}` : "";
  };

  if (paragraphs.length === 0) {
    return beats.map((beat, idx) => ({
      beat,
      beatIndex: idx,
      startPara: null,
      endPara: null,
      matchedTerms: [],
      status: "missing",
      criticHint: matchCriticHint(beat, idx),
    }));
  }

  let cursor = 0;
  return beats.map((beat, idx) => {
    const { entityTerms, keywords } = extractBeatTerms(beat, entityById);
    const remainingBeats = beats.length - idx;
    const expectedWindowEnd = Math.min(
      paragraphs.length - 1,
      Math.max(
        cursor,
        Math.ceil(((idx + 1) / beats.length) * paragraphs.length)
      )
    );

    let bestPara = -1;
    let bestScore = 0;
    let bestMatched: string[] = [];
    const hitParas: number[] = [];

    const searchEnd = Math.min(paragraphs.length - 1, expectedWindowEnd + 2);
    for (let p = cursor; p <= searchEnd; p++) {
      const para = paragraphs[p] ?? "";
      let score = 0;
      const matched: string[] = [];
      for (const et of entityTerms) {
        if (para.includes(et)) {
          score += 3;
          matched.push(et);
        }
      }
      for (const kw of keywords) {
        if (para.includes(kw)) {
          score += 1;
          if (matched.length < 6 && !matched.includes(kw)) matched.push(kw);
        }
      }
      if (score >= 2) {
        hitParas.push(p);
      }
      if (score > bestScore) {
        bestScore = score;
        bestPara = p;
        bestMatched = matched;
      }
    }

    const criticHint = matchCriticHint(beat, idx);
    if (bestScore === 0 || bestPara < 0) {
      return {
        beat,
        beatIndex: idx,
        startPara: null,
        endPara: null,
        matchedTerms: [],
        status: "missing",
        criticHint,
      };
    }

    const startPara = hitParas.length > 0 ? Math.min(...hitParas) : bestPara;
    const endPara = hitParas.length > 0 ? Math.max(...hitParas) : bestPara;
    cursor = Math.min(paragraphs.length - Math.max(1, remainingBeats - 1), endPara + 1);

    let status: BeatCoverageStatus = bestScore >= 3 ? "covered" : "weak";
    if (criticHint && (criticHint.includes("遗漏") || criticHint.includes("未写") || criticHint.includes("缺少"))) {
      status = "missing";
    }

    return {
      beat,
      beatIndex: idx,
      startPara,
      endPara,
      matchedTerms: bestMatched.slice(0, 4),
      status,
      criticHint,
    };
  });
}
