import { describe, expect, test } from "bun:test";
import type { Beat, CriticFinding, Entity, EntitySnapshot } from "../mygo";
import {
  alignBeatsToParagraphs,
  applyLintReplacementInContent,
  matchChapterEntities,
  parseSensitiveImportLines,
  resolveEntitySpans,
} from "./studioHelpers";

describe("parseSensitiveImportLines", () => {
  test("parses replacement mappings and comma-separated plain words with deduplication", () => {
    const raw = `
      违禁词甲=安全词甲
      违禁词乙 -> 安全词乙
      违禁词丙 => 安全词丙
      词丁：安全词丁
      词戊, 词己、词戊；词庚
    `;
    expect(parseSensitiveImportLines(raw)).toEqual([
      { pattern: "违禁词甲", replacement: "安全词甲" },
      { pattern: "违禁词乙", replacement: "安全词乙" },
      { pattern: "违禁词丙", replacement: "安全词丙" },
      { pattern: "词丁", replacement: "安全词丁" },
      { pattern: "词戊", replacement: "" },
      { pattern: "词己", replacement: "" },
      { pattern: "词庚", replacement: "" },
    ]);
  });
});

describe("applyLintReplacementInContent", () => {
  test("replaces exact rune span while preserving original paragraph spacing", () => {
    const content = "第一段正常。\n\n  林渊看见敏感词甲，立刻退后。  \n\n第三段。";
    const res = applyLintReplacementInContent(
      content,
      {
        paragraphIndex: 1,
        startRune: 4,
        endRune: 8,
        matchedText: "敏感词甲",
      },
      "合规词"
    );
    expect(res.replaced).toBe(true);
    expect(res.content).toBe("第一段正常。\n\n  林渊看见合规词，立刻退后。  \n\n第三段。");
  });

  test("falls back to matchedText search when rune offset has drifted", () => {
    const content = "第一段。\n\n前面多了字，这里还是有违禁词乙需要替换。";
    const res = applyLintReplacementInContent(
      content,
      {
        paragraphIndex: 1,
        startRune: 0,
        endRune: 2,
        matchedText: "违禁词乙",
      },
      "安全词"
    );
    expect(res.replaced).toBe(true);
    expect(res.content).toBe("第一段。\n\n前面多了字，这里还是有安全词需要替换。");
  });
});

describe("matchChapterEntities & resolveEntitySpans", () => {
  const snapshots: EntitySnapshot[] = [
    {
      entity: {
        id: 1,
        bookId: 1,
        kind: "character",
        name: "林渊",
        aliases: ["林师弟"],
        isInvariant: false,
        summary: "主角",
        attributes: { realm: "筑基初期" },
        createdAt: "",
        updatedAt: "",
      },
      effectiveAttributes: { realm: "筑基初期" },
      appliedEventCount: 1,
      pendingEventCount: 0,
    },
    {
      entity: {
        id: 2,
        bookId: 1,
        kind: "item",
        name: "青冥剑",
        aliases: ["古剑"],
        isInvariant: false,
        summary: "本命灵剑",
        attributes: {},
        createdAt: "",
        updatedAt: "",
      },
      effectiveAttributes: {},
      appliedEventCount: 0,
      pendingEventCount: 0,
    },
    {
      entity: {
        id: 3,
        bookId: 1,
        kind: "character",
        name: "王煞",
        aliases: [],
        isInvariant: false,
        summary: "外门宿敌",
        attributes: {},
        createdAt: "",
        updatedAt: "",
      },
      effectiveAttributes: {},
      appliedEventCount: 0,
      pendingEventCount: 0,
    },
  ];

  test("ranks entities mentioned in content and bound in beats first", () => {
    const beats: Beat[] = [
      {
        id: 1,
        chapterId: 1,
        seq: 1,
        stage: "burst",
        summary: "夺剑",
        conflictNote: "",
        entityIds: [2],
        targetWords: 500,
      },
    ];
    const items = matchChapterEntities(
      "林渊握紧铁器，林师弟冷眼看着王煞。",
      beats,
      snapshots
    );
    expect(items[0]?.snapshot.entity.name).toBe("林渊");
    expect(items[0]?.mentionCount).toBe(2);
    expect(items.find((i) => i.snapshot.entity.name === "青冥剑")?.inBeats).toBe(true);
  });

  test("resolves non-overlapping entity spans skipping blocked lint ranges", () => {
    const para = "林渊祭出青冥剑斩向王煞，嘴角勾起冷笑。";
    const runes = Array.from(para);
    // 假设 "王煞" 被某个 lint 区间覆盖（start: 9, end: 11）
    const spans = resolveEntitySpans(runes, snapshots, [{ start: 9, end: 11 }]);
    expect(spans.map((s) => s.matchedTerm)).toEqual(["林渊", "青冥剑"]);
  });
});

describe("alignBeatsToParagraphs", () => {
  test("maps beats to sequential paragraph spans and attaches critic beat findings", () => {
    const entities: Entity[] = [
      {
        id: 1,
        bookId: 1,
        kind: "character",
        name: "林渊",
        aliases: [],
        isInvariant: false,
        summary: "",
        attributes: {},
        createdAt: "",
        updatedAt: "",
      },
      {
        id: 2,
        bookId: 1,
        kind: "character",
        name: "王煞",
        aliases: [],
        isInvariant: false,
        summary: "",
        attributes: {},
        createdAt: "",
        updatedAt: "",
      },
    ];
    const beats: Beat[] = [
      {
        id: 10,
        chapterId: 1,
        seq: 1,
        stage: "setup",
        povEntityId: 1,
        summary: "林渊潜入药园搜寻灵草",
        conflictNote: "拿到赤血草",
        entityIds: [1],
        targetWords: 600,
      },
      {
        id: 11,
        chapterId: 1,
        seq: 2,
        stage: "burst",
        povEntityId: 1,
        summary: "王煞设伏拦截，双剑交锋",
        conflictNote: "击退王煞",
        entityIds: [1, 2],
        targetWords: 800,
      },
    ];
    const paragraphs = [
      "夜雨未歇，林渊贴着石墙潜入药园，一眼锁定了石缝间的赤血草。",
      "他刚把赤血草收进玉匣，身后阵纹骤亮。",
      "王煞手持阴雷珠现身堵住去路，双方立刻拔剑交锋。",
    ];
    const critic: CriticFinding[] = [
      {
        id: "cf-1",
        dimension: "beat_coverage",
        severity: "warn",
        paragraphIndex: 2,
        title: "第2拍打斗略仓促",
        detail: "王煞现身与交锋过程偏短",
        suggestion: "补充一回合试探",
      },
    ];

    const aligned = alignBeatsToParagraphs(beats, paragraphs, entities, critic);
    expect(aligned).toHaveLength(2);
    expect(aligned[0]?.status).toBe("covered");
    expect(aligned[0]?.startPara).toBe(0);
    expect(aligned[1]?.status).toBe("covered");
    expect(aligned[1]?.startPara).toBe(2);
    expect(aligned[1]?.criticHint).toContain("第2拍打斗略仓促");
  });
});
