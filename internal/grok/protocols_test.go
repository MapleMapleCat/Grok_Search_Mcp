package grok

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grok-mcp/internal/config"
)

func TestBuildChatCompletionsRequestBodyMapsSearchSources(t *testing.T) {
	_, body, err := buildChatCompletionsRequestBody(SearchRequest{
		Query:           "latest news",
		ToolType:        ToolTypeWebSearch,
		AllowedDomains:  []string{"example.com"},
		ExcludedDomains: nil,
	}, "grok-4.3")
	if err != nil {
		t.Fatalf("build chat completions request: %v", err)
	}

	var request chatCompletionsRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("decode chat completions request: %v", err)
	}
	if len(request.Messages) != 1 || request.Messages[0].Content != "latest news" {
		t.Fatalf("unexpected messages: %+v", request.Messages)
	}
	if !request.Stream || !request.StreamOptions.IncludeUsage {
		t.Fatalf("streaming usage must be enabled: %+v", request)
	}
	if len(request.SearchParameters.Sources) != 1 {
		t.Fatalf("expected one search source: %+v", request.SearchParameters)
	}
	source := request.SearchParameters.Sources[0]
	if source.Type != "web" || len(source.AllowedWebsites) != 1 || source.AllowedWebsites[0] != "example.com" {
		t.Fatalf("unexpected web source: %+v", source)
	}

	_, xBody, err := buildChatCompletionsRequestBody(SearchRequest{
		Query:    "recent posts",
		ToolType: ToolTypeXSearch,
	}, "grok-4.3")
	if err != nil {
		t.Fatalf("build X chat completions request: %v", err)
	}
	if err := json.Unmarshal(xBody, &request); err != nil {
		t.Fatalf("decode X chat completions request: %v", err)
	}
	if request.SearchParameters.Sources[0].Type != "x" {
		t.Fatalf("expected X search source, got %+v", request.SearchParameters.Sources[0])
	}
}

func TestParseChatCompletionsResponseAggregatesTextCitationsAndUsage(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Hello "}}]}`,
		"",
		`data: {"choices":[{"delta":{"content":"world"}}],"citations":[{"url":"https://example.com","title":"Example"}]}`,
		"",
		`data: {"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	result, err := parseChatCompletionsResponse(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("parse chat completions response: %v", err)
	}
	if result.Answer != "Hello world" {
		t.Fatalf("unexpected answer: %q", result.Answer)
	}
	if len(result.Sources) != 1 || result.Sources[0].URL != "https://example.com" {
		t.Fatalf("unexpected sources: %+v", result.Sources)
	}
	if result.Usage == nil || result.Usage.TotalTokens != 5 {
		t.Fatalf("unexpected usage: %+v", result.Usage)
	}
}

func TestBuildAnthropicMessagesRequestBodyMapsProtocolTools(t *testing.T) {
	_, body, err := buildAnthropicMessagesRequestBody(SearchRequest{
		Query:           "latest news",
		ToolType:        ToolTypeWebSearch,
		AllowedDomains:  []string{"example.com"},
		ExcludedDomains: []string{"blocked.example"},
	}, "grok-4.3")
	if err != nil {
		t.Fatalf("build anthropic request: %v", err)
	}

	var request anthropicMessagesRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("decode anthropic request: %v", err)
	}
	if request.MaxTokens != anthropicDefaultMaxTokens || !request.Stream {
		t.Fatalf("unexpected messages settings: %+v", request)
	}
	if len(request.Tools) != 1 || request.Tools[0].Type != "web_search_20250305" {
		t.Fatalf("unexpected web search tool: %+v", request.Tools)
	}
	if len(request.Tools[0].BlockedDomains) != 1 || request.Tools[0].BlockedDomains[0] != "blocked.example" {
		t.Fatalf("unexpected blocked domains: %+v", request.Tools[0])
	}

	_, xBody, err := buildAnthropicMessagesRequestBody(SearchRequest{
		Query:    "recent posts",
		ToolType: ToolTypeXSearch,
	}, "grok-4.3")
	if err != nil {
		t.Fatalf("build anthropic X request: %v", err)
	}
	if err := json.Unmarshal(xBody, &request); err != nil {
		t.Fatalf("decode anthropic X request: %v", err)
	}
	if request.Tools[0].Type != "x_search" || request.Tools[0].Name != "x_search" {
		t.Fatalf("unexpected X search tool: %+v", request.Tools[0])
	}
}

func TestParseAnthropicMessagesResponseAggregatesTextCitationsAndUsage(t *testing.T) {
	stream := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"usage":{"input_tokens":4}}}`,
		"",
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Answer"}}`,
		"",
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","delta":{"type":"citations_delta","citation":{"type":"web_search_result_location","url":"https://example.com","title":"Example"}}}`,
		"",
		`event: message_delta`,
		`data: {"type":"message_delta","usage":{"output_tokens":6}}`,
		"",
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")

	result, err := parseAnthropicMessagesResponse(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("parse anthropic response: %v", err)
	}
	if result.Answer != "Answer" {
		t.Fatalf("unexpected answer: %q", result.Answer)
	}
	if len(result.Sources) != 1 || result.Sources[0].Title != "Example" {
		t.Fatalf("unexpected sources: %+v", result.Sources)
	}
	if result.Usage == nil || result.Usage.InputTokens != 4 || result.Usage.TotalTokens != 10 {
		t.Fatalf("unexpected usage: %+v", result.Usage)
	}
}

func TestClientUsesSelectedProtocolEndpointAndHeaders(t *testing.T) {
	testCases := []struct {
		name           string
		protocol       config.UpstreamProtocol
		expectedPath   string
		expectedAPIKey string
		responseBody   string
	}{
		{
			name:         "chat completions",
			protocol:     config.UpstreamProtocolChatCompletions,
			expectedPath: "/v1/chat/completions",
			responseBody: "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n",
		},
		{
			name:           "anthropic messages",
			protocol:       config.UpstreamProtocolAnthropicMessages,
			expectedPath:   "/v1/messages",
			expectedAPIKey: "test-key",
			responseBody:   "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
				if request.URL.Path != testCase.expectedPath {
					t.Errorf("expected path %q, got %q", testCase.expectedPath, request.URL.Path)
				}
				if request.Header.Get("Authorization") != "Bearer test-key" {
					t.Errorf("missing CPA bearer authorization")
				}
				if testCase.expectedAPIKey != "" && request.Header.Get("x-api-key") != testCase.expectedAPIKey {
					t.Errorf("expected Anthropic x-api-key header")
				}
				responseWriter.Header().Set("Content-Type", "text/event-stream")
				_, _ = responseWriter.Write([]byte(testCase.responseBody))
			}))
			defer server.Close()

			client := NewClient(&config.Config{
				CPABaseURL:       server.URL,
				CPAAPIKey:        "test-key",
				UpstreamProtocol: testCase.protocol,
				Model:            "grok-4.3",
				Timeout:          5 * time.Second,
			})
			result, err := client.SearchStream(context.Background(), SearchRequest{
				Query:    "test",
				ToolType: ToolTypeWebSearch,
			}, nil)
			if err != nil {
				t.Fatalf("search selected protocol: %v", err)
			}
			if result.Answer != "ok" {
				t.Fatalf("unexpected answer: %q", result.Answer)
			}
		})
	}
}
