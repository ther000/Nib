package llm

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

type sseEvent struct {
	Event string
	Data  string
}

// readSSE 容错解析 Server-Sent Events 流。
// 使用 bufio.Reader 规避 bufio.Scanner 64KB 单行上限，兼容 \r\n、: keepalive 注释、
// 空行分隔与部分中转站省略空行的连续 data: 行。
func readSSE(r io.Reader, handle func(sseEvent) error) error {
	br := bufio.NewReaderSize(r, 64*1024)
	var eventName string
	var dataLines []string

	flush := func() error {
		if len(dataLines) == 0 {
			eventName = ""
			return nil
		}
		ev := sseEvent{
			Event: eventName,
			Data:  strings.Join(dataLines, "\n"),
		}
		eventName = ""
		dataLines = dataLines[:0]
		return handle(ev)
	}

	for {
		line, readErr := br.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}

		// 若最后一行无内容且已到达 EOF，则刷新缓冲后正常结束。
		if line == "" && errors.Is(readErr, io.EOF) {
			return flush()
		}

		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			if err := flush(); err != nil {
				return err
			}
		case strings.HasPrefix(line, ":"):
			// SSE 注释 / 心跳保活（如 ": keepalive"），直接忽略。
		case strings.HasPrefix(line, "event:"):
			// 若已有未刷新的完整数据，先派发以免串联两条事件。
			if len(dataLines) > 0 {
				if err := flush(); err != nil {
					return err
				}
			}
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			payload := strings.TrimPrefix(line, "data:")
			if strings.HasPrefix(payload, " ") {
				payload = payload[1:]
			}
			// 兼容少数中转站省略事件间空行直接连续输出 data: {...} 的情况。
			if len(dataLines) > 0 {
				prev := strings.TrimSpace(strings.Join(dataLines, "\n"))
				if prev == "[DONE]" || json.Valid([]byte(prev)) {
					if err := flush(); err != nil {
						return err
					}
				}
			}
			dataLines = append(dataLines, payload)
		default:
			// 忽略 id: / retry: 或非标准元数据行。
		}

		if errors.Is(readErr, io.EOF) {
			return flush()
		}
	}
}
