package grok

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

const (
	maxUpstreamResponseBytes   int64 = 8 * 1024 * 1024
	maxSSEEventBytes                 = 8 * 1024 * 1024
	maxSSEEventCount                 = 4096
	maxSearchRoundCount              = 64
	maxAggregatedAnswerBytes         = 2 * 1024 * 1024
	maxCitationCount                 = 256
	maxAggregatedCitationBytes       = 256 * 1024
)

// forEachSSEEvent 按 SSE 规范解析响应体：空行分隔事件，多行 data: 拼接为一条 payload。
// 忽略 "[DONE]" 哨兵；对每条有效 JSON payload 调用 onEvent。
func forEachSSEEvent(r io.Reader, onEvent func(payload string) error) error {
	limitedReader := &io.LimitedReader{R: r, N: maxUpstreamResponseBytes + 1}
	scanner := bufio.NewScanner(limitedReader)
	// 上游可能推送较大的 completed 事件，放宽 Scanner 缓冲区上限。
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSEEventBytes+1)

	var dataLines []string
	eventBytes := 0
	eventCount := 0

	flushEvent := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = nil
		eventBytes = 0
		eventCount++
		if eventCount > maxSSEEventCount {
			return fmt.Errorf("upstream stream exceeded event limit of %d", maxSSEEventCount)
		}

		if payload == "[DONE]" {
			return nil
		}

		return onEvent(payload)
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flushEvent(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLine := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			additionalBytes := len(dataLine)
			if len(dataLines) > 0 {
				additionalBytes++
			}
			if eventBytes > maxSSEEventBytes-additionalBytes {
				return fmt.Errorf("upstream stream event exceeded byte limit of %d", maxSSEEventBytes)
			}
			eventBytes += additionalBytes
			dataLines = append(dataLines, dataLine)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stream: %w", err)
	}
	if limitedReader.N == 0 {
		return fmt.Errorf("upstream stream exceeded total byte limit of %d", maxUpstreamResponseBytes)
	}
	return flushEvent()
}

func readAllUpstreamResponse(reader io.Reader) ([]byte, error) {
	limitedReader := &io.LimitedReader{R: reader, N: maxUpstreamResponseBytes + 1}
	responseBody, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}
	if int64(len(responseBody)) > maxUpstreamResponseBytes {
		return nil, fmt.Errorf("upstream response exceeded total byte limit of %d", maxUpstreamResponseBytes)
	}
	return responseBody, nil
}

func appendAnswerText(answer *strings.Builder, textParts ...string) error {
	additionalBytes := 0
	for _, textPart := range textParts {
		if additionalBytes > maxAggregatedAnswerBytes-len(textPart) {
			return fmt.Errorf("upstream response exceeded aggregated answer byte limit of %d", maxAggregatedAnswerBytes)
		}
		additionalBytes += len(textPart)
	}
	if answer.Len() > maxAggregatedAnswerBytes-additionalBytes {
		return fmt.Errorf("upstream response exceeded aggregated answer byte limit of %d", maxAggregatedAnswerBytes)
	}
	for _, textPart := range textParts {
		answer.WriteString(textPart)
	}
	return nil
}
