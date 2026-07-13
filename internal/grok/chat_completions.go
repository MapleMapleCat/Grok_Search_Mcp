package grok

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type chatCompletionsRequest struct {
	Model            string               `json:"model"`
	Messages         []chatMessage        `json:"messages"`
	Stream           bool                 `json:"stream"`
	StreamOptions    chatStreamOptions    `json:"stream_options"`
	SearchParameters chatSearchParameters `json:"search_parameters"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatSearchParameters struct {
	Mode            string             `json:"mode"`
	ReturnCitations bool               `json:"return_citations"`
	Sources         []chatSearchSource `json:"sources"`
}

type chatSearchSource struct {
	Type             string   `json:"type"`
	AllowedWebsites  []string `json:"allowed_websites,omitempty"`
	ExcludedWebsites []string `json:"excluded_websites,omitempty"`
}

type chatCompletionsResponse struct {
	Choices   []chatChoice      `json:"choices"`
	Usage     chatUsage         `json:"usage"`
	Citations []json.RawMessage `json:"citations"`
}

type chatChoice struct {
	Delta   chatResponseMessage `json:"delta"`
	Message chatResponseMessage `json:"message"`
}

type chatResponseMessage struct {
	Content     string            `json:"content"`
	Annotations []chatAnnotation  `json:"annotations"`
	Citations   []json.RawMessage `json:"citations"`
}

type chatAnnotation struct {
	Type  string `json:"type"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (s clientSnapshot) searchChatCompletions(ctx context.Context, req SearchRequest) (*SearchResult, error) {
	model, body, err := buildChatCompletionsRequestBody(req, s.defaultModel)
	if err != nil {
		return nil, err
	}
	s.log.Debugf("SearchStream start protocol=%s model=%s tool=%s query=%q", s.protocol, model, req.ToolType, req.Query)

	response, err := s.postJSON(ctx, "/v1/chat/completions", body, false)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, s.httpError(response)
	}
	return parseChatCompletionsResponse(response.Body)
}

func buildChatCompletionsRequestBody(req SearchRequest, defaultModel string) (string, []byte, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = defaultModel
	}
	if err := validateModel(model); err != nil {
		return "", nil, err
	}

	searchSource := chatSearchSource{Type: "x"}
	if req.ToolType == ToolTypeWebSearch {
		searchSource = chatSearchSource{
			Type:             "web",
			AllowedWebsites:  req.AllowedDomains,
			ExcludedWebsites: req.ExcludedDomains,
		}
	}
	upstreamRequest := chatCompletionsRequest{
		Model:         model,
		Messages:      []chatMessage{{Role: "user", Content: req.Query}},
		Stream:        true,
		StreamOptions: chatStreamOptions{IncludeUsage: true},
		SearchParameters: chatSearchParameters{
			Mode:            "on",
			ReturnCitations: true,
			Sources:         []chatSearchSource{searchSource},
		},
	}
	body, err := json.Marshal(upstreamRequest)
	if err != nil {
		return "", nil, fmt.Errorf("marshal chat completions request: %w", err)
	}
	return model, body, nil
}

func parseChatCompletionsResponse(body io.Reader) (*SearchResult, error) {
	rawBody, err := io.ReadAll(io.LimitReader(body, 8*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read chat completions response: %w", err)
	}

	var answer strings.Builder
	collector := newCitationCollector()
	var normalizedUsage *Usage
	consumeResponse := func(response chatCompletionsResponse) {
		for _, choice := range response.Choices {
			message := choice.Delta
			if message.Content == "" {
				message = choice.Message
			}
			answer.WriteString(message.Content)
			collectChatMessageCitations(collector, message)
		}
		for _, rawCitation := range response.Citations {
			collector.addRaw(rawCitation)
		}
		if response.Usage.PromptTokens != 0 || response.Usage.CompletionTokens != 0 || response.Usage.TotalTokens != 0 {
			normalizedUsage = &Usage{
				InputTokens:  response.Usage.PromptTokens,
				OutputTokens: response.Usage.CompletionTokens,
				TotalTokens:  response.Usage.TotalTokens,
			}
		}
	}

	if bytes.Contains(rawBody, []byte("data:")) {
		err = forEachSSEEvent(bytes.NewReader(rawBody), func(payload string) error {
			var response chatCompletionsResponse
			if decodeErr := json.Unmarshal([]byte(payload), &response); decodeErr != nil {
				return fmt.Errorf("decode chat completions stream event: %w", decodeErr)
			}
			consumeResponse(response)
			return nil
		})
	} else {
		var response chatCompletionsResponse
		err = json.Unmarshal(rawBody, &response)
		if err == nil {
			consumeResponse(response)
		}
	}
	if err != nil {
		return nil, err
	}

	answerText := strings.TrimSpace(answer.String())
	if answerText == "" {
		return nil, fmt.Errorf("upstream response did not contain answer text")
	}
	return &SearchResult{
		Answer:      answerText,
		Citations:   collector.citations,
		Sources:     collector.sources,
		Usage:       normalizedUsage,
		RawResponse: json.RawMessage(rawBody),
	}, nil
}

func collectChatMessageCitations(collector *citationCollector, message chatResponseMessage) {
	for _, annotation := range message.Annotations {
		if annotation.Type == "url_citation" || annotation.URL != "" {
			collector.add(annotation.URL, annotation.Title)
		}
	}
	for _, rawCitation := range message.Citations {
		collector.addRaw(rawCitation)
	}
}
