import { describe, expect, test } from "bun:test";
import { diffInlineTokens, diffParagraphs, formatWebNovelProse } from "./textDiff";

describe("diffParagraphs", () => {
  test("computes added, removed, and unchanged blocks accurately", () => {
    const oldText = "第一段。\n\n旧第二段。\n\n第三段。";
    const newText = "第一段。\n\n新第二段。\n\n补充段。\n\n第三段。";
    const summary = diffParagraphs(oldText, newText);

    expect(summary.unchanged).toBe(2);
    expect(summary.removed).toBe(1);
    expect(summary.added).toBe(2);
    expect(summary.blocks.map((b) => [b.kind, b.text])).toEqual([
      ["equal", "第一段。"],
      ["removed", "旧第二段。"],
      ["added", "新第二段。"],
      ["added", "补充段。"],
      ["equal", "第三段。"],
    ]);
  });

  test("degrades gracefully when paragraph product exceeds DP cap", () => {
    const a = Array.from({ length: 510 }, (_, i) => `A-${i}`).join("\n\n");
    const b = Array.from({ length: 510 }, (_, i) => `B-${i}`).join("\n\n");
    const summary = diffParagraphs(a, b);
    expect(summary.removed).toBe(510);
    expect(summary.added).toBe(510);
    expect(summary.unchanged).toBe(0);
  });
});

describe("formatWebNovelProse", () => {
  test("removes repeated spaces between 3+ CJK chars and normalizes punctuation & quotes", () => {
    const raw = '　　林 渊 拔 出 长 剑,"你找死?!"...  \r\n  "试试看!" ';
    const { text, changed } = formatWebNovelProse(raw);
    expect(changed).toBe(true);
    expect(text).toBe("林渊拔出长剑，“你找死？！”……\n\n“试试看！”");
  });

  test("returns unchanged=false for already clean prose", () => {
    const clean = "剑光骤起。\n\n“退后！”";
    const res = formatWebNovelProse(clean);
    expect(res.changed).toBe(false);
    expect(res.text).toBe(clean);
  });
});

describe("diffInlineTokens", () => {
  test("handles empty and identical texts", () => {
    expect(diffInlineTokens("", "")).toEqual([]);
    expect(diffInlineTokens("拔剑出鞘", "拔剑出鞘")).toEqual([
      { kind: "equal", text: "拔剑出鞘" },
    ]);
    expect(diffInlineTokens("", "新增句")).toEqual([{ kind: "added", text: "新增句" }]);
    expect(diffInlineTokens("旧句", "")).toEqual([{ kind: "removed", text: "旧句" }]);
  });

  test("highlights inline CJK edits, punctuation changes, and multi-paragraph edits", () => {
    const segs = diffInlineTokens(
      "林渊嘴角勾起，拔出铁剑。\n\n他缓缓说道：“走。”",
      "林渊神色微冷，抽剑出鞘！\n\n他沉声喝道：“走。”"
    );
    const reconstructedOld = segs
      .filter((s) => s.kind !== "added")
      .map((s) => s.text)
      .join("");
    const reconstructedNew = segs
      .filter((s) => s.kind !== "removed")
      .map((s) => s.text)
      .join("");
    expect(reconstructedOld).toBe("林渊嘴角勾起，拔出铁剑。\n\n他缓缓说道：“走。”");
    expect(reconstructedNew).toBe("林渊神色微冷，抽剑出鞘！\n\n他沉声喝道：“走。”");
    expect(segs.some((s) => s.kind === "removed" && s.text.includes("嘴角勾起"))).toBe(true);
    expect(segs.some((s) => s.kind === "added" && s.text.includes("神色微冷"))).toBe(true);
  });
});
