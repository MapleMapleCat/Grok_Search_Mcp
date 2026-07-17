package grok

import (
	"encoding/json"
	"strings"
	"testing"
)

var benchmarkSearchResult *SearchResult

func BenchmarkForEachSSEEvent(b *testing.B) {
	benchmarkCases := []struct {
		name   string
		stream string
	}{
		{
			name:   "single-large-event",
			stream: buildBenchmarkSingleLineSSEEvent(1024*1024 - 64),
		},
		{
			name:   "multiline-large-event",
			stream: buildBenchmarkMultilineSSEEvent(1024*1024-64, 16*1024),
		},
		{
			name:   "many-small-events",
			stream: strings.Repeat("data: {\"type\":\"ping\"}\n\n", 4096),
		},
		{
			name:   "near-total-limit",
			stream: buildBenchmarkRepeatedSSEEvents(7*1024*1024, 256*1024),
		},
	}

	for _, benchmarkCase := range benchmarkCases {
		b.Run(benchmarkCase.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(benchmarkCase.stream)))
			for iteration := 0; iteration < b.N; iteration++ {
				eventCount := 0
				err := forEachSSEEvent(strings.NewReader(benchmarkCase.stream), func([]byte) error {
					eventCount++
					return nil
				})
				if err != nil {
					b.Fatalf("parse benchmark SSE stream: %v", err)
				}
				if eventCount == 0 {
					b.Fatal("expected at least one SSE event")
				}
			}
		})
	}
}

func BenchmarkParseChatCompletionsResponse(b *testing.B) {
	content := strings.Repeat("a", 768*1024)
	payload, err := json.Marshal(chatCompletionsResponse{
		Choices: []chatChoice{{
			Delta: chatResponseMessage{Content: content},
		}},
		Usage: chatUsage{
			PromptTokens:     100,
			CompletionTokens: 200,
			TotalTokens:      300,
		},
	})
	if err != nil {
		b.Fatalf("marshal chat completions benchmark payload: %v", err)
	}
	stream := "data: " + string(payload) + "\n\ndata: [DONE]\n\n"

	b.ReportAllocs()
	b.SetBytes(int64(len(stream)))
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		result, parseErr := parseChatCompletionsResponse(strings.NewReader(stream), nil, nil)
		if parseErr != nil {
			b.Fatalf("parse chat completions benchmark response: %v", parseErr)
		}
		benchmarkSearchResult = result
	}
}

func buildBenchmarkSingleLineSSEEvent(payloadBytes int) string {
	return "data: " + strings.Repeat("a", payloadBytes) + "\n\n"
}

func buildBenchmarkMultilineSSEEvent(payloadBytes int, lineBytes int) string {
	var stream strings.Builder
	remainingBytes := payloadBytes
	for remainingBytes > 0 {
		currentLineBytes := min(remainingBytes, lineBytes)
		stream.WriteString("data: ")
		stream.WriteString(strings.Repeat("a", currentLineBytes))
		stream.WriteByte('\n')
		remainingBytes -= currentLineBytes
	}
	stream.WriteByte('\n')
	return stream.String()
}

func buildBenchmarkRepeatedSSEEvents(totalBytes int, eventPayloadBytes int) string {
	var stream strings.Builder
	event := buildBenchmarkSingleLineSSEEvent(eventPayloadBytes)
	for stream.Len()+len(event) <= totalBytes {
		stream.WriteString(event)
	}
	return stream.String()
}
