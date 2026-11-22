export type DiffKind = "equal" | "added" | "removed";

export interface DiffBlock {
  kind: DiffKind;
  text: string;
  oldIndex?: number;
  newIndex?: number;
}

export interface DiffSummary {
  blocks: DiffBlock[];
  added: number;
  removed: number;
  unchanged: number;
}

function splitNonEmptyLines(text: string): string[] {
  if (!text) return [];
  return text
    .replace(/\r\n/g, "\n")
    .split(/\n+/)
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
}

/**
 * 段级 LCS Diff：用于对比“历史版本快照(oldText)”与“当前正文(newText)”。
 * 当两份文本段落数乘积过大时自动降级为首尾公共前后缀对比，确保前端渲染绝不卡顿。
 */
export function diffParagraphs(oldText: string, newText: string): DiffSummary {
  const a = splitNonEmptyLines(oldText);
  const b = splitNonEmptyLines(newText);
  const n = a.length;
  const m = b.length;

  if (n === 0 && m === 0) {
    return { blocks: [], added: 0, removed: 0, unchanged: 0 };
  }

  // 裁剪首尾完全一致的段落以缩小 DP 规模
  let prefix = 0;
  while (prefix < n && prefix < m && a[prefix] === b[prefix]) {
    prefix++;
  }
  let suffix = 0;
  while (
    suffix < n - prefix &&
    suffix < m - prefix &&
    a[n - 1 - suffix] === b[m - 1 - suffix]
  ) {
    suffix++;
  }

  const blocks: DiffBlock[] = [];
  for (let i = 0; i < prefix; i++) {
    blocks.push({ kind: "equal", text: a[i]!, oldIndex: i, newIndex: i });
  }

  const midA = a.slice(prefix, n - suffix);
  const midB = b.slice(prefix, m - suffix);
  const na = midA.length;
  const mb = midB.length;

  if (na > 0 || mb > 0) {
    if (na * mb > 250_000) {
      // 超大文本安全兜底
      for (let i = 0; i < na; i++) {
        blocks.push({ kind: "removed", text: midA[i]!, oldIndex: prefix + i });
      }
      for (let j = 0; j < mb; j++) {
        blocks.push({ kind: "added", text: midB[j]!, newIndex: prefix + j });
      }
    } else {
      const dp = new Uint32Array((na + 1) * (mb + 1));
      const stride = mb + 1;
      for (let i = na - 1; i >= 0; i--) {
        for (let j = mb - 1; j >= 0; j--) {
          if (midA[i] === midB[j]) {
            dp[i * stride + j] = (dp[(i + 1) * stride + (j + 1)] ?? 0) + 1;
          } else {
            const down = dp[(i + 1) * stride + j] ?? 0;
            const right = dp[i * stride + (j + 1)] ?? 0;
            dp[i * stride + j] = down >= right ? down : right;
          }
        }
      }

      let i = 0;
      let j = 0;
      while (i < na && j < mb) {
        if (midA[i] === midB[j]) {
          blocks.push({
            kind: "equal",
            text: midA[i]!,
            oldIndex: prefix + i,
            newIndex: prefix + j,
          });
          i++;
          j++;
        } else if ((dp[(i + 1) * stride + j] ?? 0) >= (dp[i * stride + (j + 1)] ?? 0)) {
          blocks.push({ kind: "removed", text: midA[i]!, oldIndex: prefix + i });
          i++;
        } else {
          blocks.push({ kind: "added", text: midB[j]!, newIndex: prefix + j });
          j++;
        }
      }
      while (i < na) {
        blocks.push({ kind: "removed", text: midA[i]!, oldIndex: prefix + i });
        i++;
      }
      while (j < mb) {
        blocks.push({ kind: "added", text: midB[j]!, newIndex: prefix + j });
        j++;
      }
    }
  }

  for (let k = 0; k < suffix; k++) {
    const oi = n - suffix + k;
    const ni = m - suffix + k;
    blocks.push({ kind: "equal", text: a[oi]!, oldIndex: oi, newIndex: ni });
  }

  let added = 0;
  let removed = 0;
  let unchanged = 0;
  for (const blk of blocks) {
    if (blk.kind === "added") added++;
    else if (blk.kind === "removed") removed++;
    else unchanged++;
  }

  return { blocks, added, removed, unchanged };
}

/**
 * 一键中文网文排版整理：
 * 1. 统一换行符、去除每段首尾全半角空白与多余空格
 * 2. 将中文语境中的英文半角标点（, ? ! ; :）转为全角标点
 * 3. 将成对英文直引号 "..." 规范为中文弯引号 “...”
 * 4. 将半角省略号 ... 规范为中文省略号 ……
 * 5. 按网文标准段落间单空行（\n\n）重新拼接
 */
export function formatWebNovelProse(raw: string): { text: string; changed: boolean } {
  if (!raw || !raw.trim()) {
    return { text: raw, changed: false };
  }

  const lines = raw
    .replace(/\r\n/g, "\n")
    .replace(/\r/g, "\n")
    .split(/\n+/)
    .map((line) => line.replace(/^[\s\u3000]+|[\s\u3000]+$/g, ""))
    .filter((line) => line.length > 0);

  const formattedLines = lines.map((line) => {
    let s = line;
    // 清理中文字符之间多余空格（循环直到全部消除，避免连续三个汉字间空格漏处理）
    let prev: string;
    do {
      prev = s;
      s = s.replace(
        /([\u4e00-\u9fff\u3000-\u303f\uff00-\uffef])[ \t]+([\u4e00-\u9fff\u3000-\u303f\uff00-\uffef])/g,
        "$1$2"
      );
    } while (s !== prev);
    // 英文省略号转中文省略号
    s = s.replace(/\.{3,}/g, "……");
    // 中文及中文标点后紧跟的半角标点转全角
    s = s.replace(/([\u4e00-\u9fff”’》）】？！，。；：])[,]/g, "$1，");
    s = s.replace(/([\u4e00-\u9fff”’》）】？！，。；：])[?]/g, "$1？");
    s = s.replace(/([\u4e00-\u9fff”’》）】？！，。；：])[!]/g, "$1！");
    s = s.replace(/([\u4e00-\u9fff”’》）】？！，。；：])[;]/g, "$1；");
    s = s.replace(/([\u4e00-\u9fff”’》）】？！，。；：])[:]/g, "$1：");
    // 配对英文双引号转中文弯引号
    let openQuote = true;
    s = s.replace(/"/g, () => {
      const q = openQuote ? "“" : "”";
      openQuote = !openQuote;
      return q;
    });
    return s;
  });

  const formatted = formattedLines.join("\n\n");
  return {
    text: formatted,
    changed: formatted !== raw,
  };
}

export interface InlineDiffSegment {
  kind: DiffKind;
  text: string;
}

function tokenizeInline(text: string): string[] {
  if (!text) return [];
  const tokens: string[] = [];
  // 连续英文/数字合并为 1 token，单个空白或单个汉字/标点各自成 token（兼容 UTF-16 代理对）
  const re = /[A-Za-z0-9_]+|[\s]|[\uD800-\uDBFF][\uDC00-\uDFFF]|./g;
  let match: RegExpExecArray | null;
  while ((match = re.exec(text)) !== null) {
    tokens.push(match[0]);
  }
  return tokens;
}

function pushMergedInline(out: InlineDiffSegment[], kind: DiffKind, text: string) {
  if (!text) return;
  const last = out[out.length - 1];
  if (last && last.kind === kind) {
    last.text += text;
  } else {
    out.push({ kind, text });
  }
}

/**
 * 词/字级 LCS Diff：用于划词改写预览与局部替换高亮对比。
 * 当 token 乘积超过上限时降级为首尾公共前后缀 + 整段增删对比。
 */
export function diffInlineTokens(oldText: string, newText: string): InlineDiffSegment[] {
  if (!oldText && !newText) return [];
  if (oldText === newText) return [{ kind: "equal", text: newText }];
  if (!oldText) return [{ kind: "added", text: newText }];
  if (!newText) return [{ kind: "removed", text: oldText }];

  const a = tokenizeInline(oldText);
  const b = tokenizeInline(newText);
  const n = a.length;
  const m = b.length;

  let prefix = 0;
  while (prefix < n && prefix < m && a[prefix] === b[prefix]) {
    prefix++;
  }
  let suffix = 0;
  while (
    suffix < n - prefix &&
    suffix < m - prefix &&
    a[n - 1 - suffix] === b[m - 1 - suffix]
  ) {
    suffix++;
  }

  const out: InlineDiffSegment[] = [];
  for (let i = 0; i < prefix; i++) {
    pushMergedInline(out, "equal", a[i]!);
  }

  const midA = a.slice(prefix, n - suffix);
  const midB = b.slice(prefix, m - suffix);
  const na = midA.length;
  const mb = midB.length;

  if (na > 0 || mb > 0) {
    if (na * mb > 160_000) {
      if (na > 0) pushMergedInline(out, "removed", midA.join(""));
      if (mb > 0) pushMergedInline(out, "added", midB.join(""));
    } else {
      const dp = new Uint32Array((na + 1) * (mb + 1));
      const stride = mb + 1;
      for (let i = na - 1; i >= 0; i--) {
        for (let j = mb - 1; j >= 0; j--) {
          if (midA[i] === midB[j]) {
            dp[i * stride + j] = (dp[(i + 1) * stride + (j + 1)] ?? 0) + 1;
          } else {
            const down = dp[(i + 1) * stride + j] ?? 0;
            const right = dp[i * stride + (j + 1)] ?? 0;
            dp[i * stride + j] = down >= right ? down : right;
          }
        }
      }

      let i = 0;
      let j = 0;
      while (i < na && j < mb) {
        if (midA[i] === midB[j]) {
          pushMergedInline(out, "equal", midA[i]!);
          i++;
          j++;
        } else if ((dp[(i + 1) * stride + j] ?? 0) >= (dp[i * stride + (j + 1)] ?? 0)) {
          pushMergedInline(out, "removed", midA[i]!);
          i++;
        } else {
          pushMergedInline(out, "added", midB[j]!);
          j++;
        }
      }
      while (i < na) {
        pushMergedInline(out, "removed", midA[i]!);
        i++;
      }
      while (j < mb) {
        pushMergedInline(out, "added", midB[j]!);
        j++;
      }
    }
  }

  for (let k = 0; k < suffix; k++) {
    pushMergedInline(out, "equal", a[n - suffix + k]!);
  }
  return out;
}
