import { describe, expect, test } from "bun:test";
import {
  countWords,
  formatCompactTokens,
  formatErrorMessage,
  formatSeq,
  isIMEComposing,
  parseKeywordList,
  parseModelList,
} from "./constants";

describe("countWords", () => {
  test("counts CJK, ASCII, and surrogate pairs excluding all whitespace", () => {
    expect(countWords("")).toBe(0);
    expect(countWords("  \n\t　")).toBe(0);
    expect(countWords("残阵 灵光\n\n123 𠀀")).toBe(8);
  });
});

describe("isIMEComposing", () => {
  test("detects both direct and nativeEvent IME composition states", () => {
    expect(isIMEComposing({ isComposing: true })).toBe(true);
    expect(isIMEComposing({ keyCode: 229 })).toBe(true);
    expect(isIMEComposing({ nativeEvent: { isComposing: true } })).toBe(true);
    expect(isIMEComposing({ nativeEvent: { keyCode: 229 } })).toBe(true);
    expect(isIMEComposing({ isComposing: false, keyCode: 13 })).toBe(false);
  });
});

describe("formatters & parsers", () => {
  test("formatSeq and formatCompactTokens", () => {
    expect(formatSeq(7)).toBe("007");
    expect(formatCompactTokens(0)).toBe("0");
    expect(formatCompactTokens(12500)).toBe("1.25 万");
    expect(formatCompactTokens(150_000_000)).toBe("1.50 亿");
  });

  test("parseKeywordList & parseModelList deduplicate cleanly", () => {
    expect(parseKeywordList("林渊, 玄铁剑， 林渊；黑碑")).toEqual([
      "林渊",
      "玄铁剑",
      "黑碑",
    ]);
    expect(parseModelList("gpt-4o\ndeepseek-chat, gpt-4o")).toEqual([
      "gpt-4o",
      "deepseek-chat",
    ]);
  });

  test("formatErrorMessage maps common runtime errors", () => {
    expect(formatErrorMessage(new Error("401 unauthorized"))).toContain("密钥验证未通过");
    expect(formatErrorMessage(new Error("429 rate limit"))).toContain("请求过于频繁");
    expect(formatErrorMessage(new Error("validation failed on title: 书名不能为空"))).toBe(
      "书名不能为空"
    );
  });
});
