package compiler

import (
	"math"
	"strings"
	"unicode"
)

// EstimateTokens 使用面向中文网文的确定性启发式算法估算文本 Token 数。
// 基准对齐文档设定：12,000 tokens ≈ 18,000~20,000 汉字（单汉字约 0.65 token）。
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	var weight float64
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			weight += 0.25
		case isCJK(r):
			weight += 0.65
		case r <= unicode.MaxASCII:
			weight += 0.28
		default:
			weight += 0.75
		}
	}
	est := int(math.Ceil(weight))
	if est < 1 {
		return 1
	}
	return est
}

func isCJK(r rune) bool {
	if unicode.Is(unicode.Han, r) {
		return true
	}
	// CJK 标点符号区（U+3000..U+303F）与全角符号区（U+FF00..U+FFEF）。
	return (r >= 0x3000 && r <= 0x303F) || (r >= 0xFF00 && r <= 0xFFEF)
}

// tailRunes 截取文本末尾最多 maxRunes 个 Unicode 字符，并返回是否发生截断。
func tailRunes(text string, maxRunes int) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || maxRunes <= 0 {
		return "", trimmed != ""
	}
	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed, false
	}
	return string(runes[len(runes)-maxRunes:]), true
}

// fitTailWithinTokens 在给定 token 上限内二分查找可保留的最长文本尾部，确保前文衔接平滑。
func fitTailWithinTokens(prefix, text string, maxTokens int) (string, int) {
	if maxTokens <= 0 {
		return "", 0
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return "", 0
	}

	full := prefix + string(runes)
	if EstimateTokens(full) <= maxTokens {
		return full, len(runes)
	}
	if EstimateTokens(prefix) >= maxTokens {
		return "", 0
	}

	// 二分查找可保留的最大尾部字符数。
	lo, hi := 1, len(runes)
	bestLen := 0
	for lo <= hi {
		mid := lo + (hi-lo)/2
		candidate := prefix + "……" + string(runes[len(runes)-mid:])
		if EstimateTokens(candidate) <= maxTokens {
			bestLen = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if bestLen == 0 {
		return "", 0
	}
	return prefix + "……" + string(runes[len(runes)-bestLen:]), bestLen
}

// truncateHeadWithinTokens 按 Token 上限截断超长文本头部。
func truncateHeadWithinTokens(text string, maxTokens int) string {
	if maxTokens <= 0 || text == "" {
		return ""
	}
	if EstimateTokens(text) <= maxTokens {
		return text
	}
	runes := []rune(text)
	lo, hi := 1, len(runes)
	bestLen := 0
	for lo <= hi {
		mid := lo + (hi-lo)/2
		candidate := string(runes[:mid]) + "……"
		if EstimateTokens(candidate) <= maxTokens {
			bestLen = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if bestLen == 0 {
		return ""
	}
	return string(runes[:bestLen]) + "……"
}
