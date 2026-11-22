package llm

import (
	"encoding/json"
	"errors"
	"strings"
)

// ExtractJSONObject 从 LLM 响应（可能含 Markdown 代码围栏或前后说明文字）中提取首个完整合法的 JSON 对象（{...}）。
// 全程基于引号/转义感知的括号平衡扫描，避免非贪婪正则在 JSON 字符串内嵌 ``` 或前置非 JSON 代码块时截断失败。
func ExtractJSONObject(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("empty LLM response")
	}
	if strings.HasPrefix(trimmed, "{") && json.Valid([]byte(trimmed)) {
		return trimmed, nil
	}

	spans, sawOpen, sawMalformed := scanBalancedJSONSpans(trimmed, '{', '}')
	if len(spans) > 0 {
		return spans[0], nil
	}
	if !sawOpen {
		return "", errors.New("no JSON object found in response")
	}
	if sawMalformed {
		return "", errors.New("extracted JSON object is malformed")
	}
	return "", errors.New("unterminated JSON object in response")
}

// ExtractAllJSONObjects 提取响应文本中所有相互独立的合法顶层 JSON 对象（{...}）。
func ExtractAllJSONObjects(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "{") && json.Valid([]byte(trimmed)) {
		return []string{trimmed}
	}
	spans, _, _ := scanBalancedJSONSpans(trimmed, '{', '}')
	return spans
}

// ExtractJSONArray 从 LLM 响应中提取首个完整合法的 JSON 数组（[...]）。
func ExtractJSONArray(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("empty LLM response")
	}
	if strings.HasPrefix(trimmed, "[") && json.Valid([]byte(trimmed)) {
		return trimmed, nil
	}

	spans, sawOpen, sawMalformed := scanBalancedJSONSpans(trimmed, '[', ']')
	if len(spans) > 0 {
		return spans[0], nil
	}
	if !sawOpen {
		return "", errors.New("no JSON array found in response")
	}
	if sawMalformed {
		return "", errors.New("extracted JSON array is malformed")
	}
	return "", errors.New("unterminated JSON array in response")
}

func scanBalancedJSONSpans(src string, openCh, closeCh byte) (valid []string, sawOpen bool, sawMalformed bool) {
	for start := 0; start < len(src); start++ {
		if src[start] != openCh {
			continue
		}
		sawOpen = true
		depth := 0
		inString := false
		escaped := false
		matchedEnd := -1

		for i := start; i < len(src); i++ {
			ch := src[i]
			if escaped {
				escaped = false
				continue
			}
			if inString {
				if ch == '\\' {
					escaped = true
				} else if ch == '"' {
					inString = false
				}
				continue
			}
			switch ch {
			case '"':
				inString = true
			case openCh:
				depth++
			case closeCh:
				depth--
				if depth == 0 {
					matchedEnd = i
				}
			}
			if matchedEnd >= 0 {
				break
			}
		}

		if matchedEnd < 0 {
			// 未闭合时继续向右尝试下一个起点（防止前面闲聊里孤立的 '{' 掩盖后方合法 JSON）
			continue
		}

		candidate := src[start : matchedEnd+1]
		if json.Valid([]byte(candidate)) {
			valid = append(valid, candidate)
			start = matchedEnd
		} else {
			sawMalformed = true
		}
	}
	return valid, sawOpen, sawMalformed
}
