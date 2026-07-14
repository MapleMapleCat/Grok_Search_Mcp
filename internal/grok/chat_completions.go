package grok

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/grok-mcp/internal/logx"
)

const maxChatContinuationAttempts = 2

const chatFinalAnswerInstruction = "Complete the requested research and return the final answer now. Do not only describe that you are searching, researching, checking, or preparing an answer."

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
	Choices       []chatChoice      `json:"choices"`
	Usage         chatUsage         `json:"usage"`
	Citations     []json.RawMessage `json:"citations"`
	Sources       []json.RawMessage `json:"sources"`
	Annotations   []json.RawMessage `json:"annotations"`
	SearchResults []json.RawMessage `json:"search_results"`
}

type chatChoice struct {
	Delta       chatResponseMessage `json:"delta"`
	Message     chatResponseMessage `json:"message"`
	Citations   []json.RawMessage   `json:"citations"`
	Sources     []json.RawMessage   `json:"sources"`
	Annotations []json.RawMessage   `json:"annotations"`
}

type chatResponseMessage struct {
	Content     string            `json:"content"`
	Annotations []json.RawMessage `json:"annotations"`
	Citations   []json.RawMessage `json:"citations"`
	Sources     []json.RawMessage `json:"sources"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (s clientSnapshot) searchChatCompletions(ctx context.Context, req SearchRequest, onRound func(SearchRound)) (*SearchResult, error) {
	model, upstreamRequest, err := buildChatCompletionsRequest(req, s.defaultModel)
	if err != nil {
		return nil, err
	}
	s.log.Debugf("SearchStream start protocol=%s model=%s tool=%s query=%q", s.protocol, model, req.ToolType, req.Query)

	var accumulatedUsage Usage
	var hasAccumulatedUsage bool
	for attempt := 0; attempt <= maxChatContinuationAttempts; attempt++ {
		body, marshalErr := json.Marshal(upstreamRequest)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal chat completions request: %w", marshalErr)
		}

		response, requestErr := s.postJSON(ctx, "/v1/chat/completions", body, false)
		if requestErr != nil {
			return nil, requestErr
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			requestErr = s.httpError(response)
			_ = response.Body.Close()
			return nil, requestErr
		}
		result, parseErr := parseChatCompletionsResponse(response.Body, onRound, s.log)
		_ = response.Body.Close()
		if parseErr != nil {
			return nil, parseErr
		}
		if result.Usage != nil {
			accumulatedUsage.InputTokens += result.Usage.InputTokens
			accumulatedUsage.OutputTokens += result.Usage.OutputTokens
			accumulatedUsage.TotalTokens += result.Usage.TotalTokens
			accumulatedUsage.ReasoningTokens += result.Usage.ReasoningTokens
			hasAccumulatedUsage = true
		}

		if !isChatIntermediateAnswer(result.Answer) {
			if hasAccumulatedUsage {
				result.Usage = &accumulatedUsage
			}
			return result, nil
		}
		if attempt == maxChatContinuationAttempts {
			return nil, fmt.Errorf("chat completions did not return a final answer after %d continuation attempts", maxChatContinuationAttempts)
		}

		s.log.Debugf("Chat Completions returned an intermediate answer; requesting continuation attempt=%d", attempt+1)
		upstreamRequest.Messages = append(upstreamRequest.Messages,
			chatMessage{Role: "assistant", Content: result.Answer},
			chatMessage{Role: "user", Content: chatFinalAnswerInstruction},
		)
	}

	return nil, fmt.Errorf("chat completions continuation exhausted unexpectedly")
}

func buildChatCompletionsRequestBody(req SearchRequest, defaultModel string) (string, []byte, error) {
	model, upstreamRequest, err := buildChatCompletionsRequest(req, defaultModel)
	if err != nil {
		return "", nil, err
	}
	body, err := json.Marshal(upstreamRequest)
	if err != nil {
		return "", nil, fmt.Errorf("marshal chat completions request: %w", err)
	}
	return model, body, nil
}

func buildChatCompletionsRequest(req SearchRequest, defaultModel string) (string, chatCompletionsRequest, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = defaultModel
	}
	if err := validateModel(model); err != nil {
		return "", chatCompletionsRequest{}, err
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
	return model, upstreamRequest, nil
}

func isChatIntermediateAnswer(answer string) bool {
	normalizedAnswer := strings.ToLower(strings.TrimSpace(answer))
	if normalizedAnswer == "" || len([]rune(normalizedAnswer)) > 320 {
		return false
	}

	intermediatePhrases := []string{
		"正在检索",
		"正在搜索",
		"正在查询",
		"正在查找",
		"正在查阅",
		"正在阅读",
		"正在浏览",
		"正在核验",
		"正在收集",
		"接下来我会",
		"以便交叉核验",
		"let me search",
		"i will search",
		"i'll search",
		"i am searching",
		"i'm searching",
		"searching for",
		"researching the",
		"i will research",
		"i'll research",
		"checking the official",
		"gathering information",
	}
	for _, phrase := range intermediatePhrases {
		if strings.Contains(normalizedAnswer, phrase) {
			return true
		}
	}
	return false
}

func parseChatCompletionsResponse(body io.Reader, onRound func(SearchRound), log *logx.Logger) (*SearchResult, error) {
	limitedBody := &io.LimitedReader{R: body, N: maxUpstreamResponseBytes + 1}
	bufferedBody := bufio.NewReader(limitedBody)
	responseReader, isSSE, err := identifyChatCompletionsResponse(bufferedBody)
	if err != nil {
		if limitedBody.N == 0 {
			return nil, fmt.Errorf("upstream response exceeded total byte limit of %d", maxUpstreamResponseBytes)
		}
		return nil, err
	}

	var rawBody bytes.Buffer
	capturedBody := io.TeeReader(responseReader, &rawBody)

	var answer strings.Builder
	collector := newCitationCollector()
	var normalizedUsage *Usage
	consumeResponse := func(response chatCompletionsResponse) error {
		for _, choice := range response.Choices {
			if err := appendAnswerText(&answer, choice.Delta.Content); err != nil {
				return err
			}
			collectChatMessageCitations(collector, choice.Delta)
			if choice.Delta.Content == "" {
				if err := appendAnswerText(&answer, choice.Message.Content); err != nil {
					return err
				}
			}
			collectChatMessageCitations(collector, choice.Message)
			collectRawCitations(collector, choice.Citations)
			collectRawCitations(collector, choice.Sources)
			collectRawCitations(collector, choice.Annotations)
		}
		collectRawCitations(collector, response.Citations)
		collectRawCitations(collector, response.Sources)
		collectRawCitations(collector, response.Annotations)
		collectRawCitations(collector, response.SearchResults)
		if response.Usage.PromptTokens != 0 || response.Usage.CompletionTokens != 0 || response.Usage.TotalTokens != 0 {
			normalizedUsage = &Usage{
				InputTokens:  response.Usage.PromptTokens,
				OutputTokens: response.Usage.CompletionTokens,
				TotalTokens:  response.Usage.TotalTokens,
			}
		}
		return collector.err
	}

	if isSSE {
		searchRounds := newSearchRoundTracker()
		err = forEachSSEEvent(capturedBody, func(payload string) error {
			if searchErr := searchRounds.emitCompatibleSearchRound(payload, onRound, log); searchErr != nil {
				return searchErr
			}
			var response chatCompletionsResponse
			if decodeErr := json.Unmarshal([]byte(payload), &response); decodeErr != nil {
				return fmt.Errorf("decode chat completions stream event: %w", decodeErr)
			}
			return consumeResponse(response)
		})
	} else {
		responseBody, readErr := io.ReadAll(capturedBody)
		if readErr != nil {
			return nil, fmt.Errorf("read chat completions response: %w", readErr)
		}
		var response chatCompletionsResponse
		err = json.Unmarshal(responseBody, &response)
		if err == nil {
			err = consumeResponse(response)
		}
	}
	if limitedBody.N == 0 {
		return nil, fmt.Errorf("upstream response exceeded total byte limit of %d", maxUpstreamResponseBytes)
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
		RawResponse: json.RawMessage(rawBody.Bytes()),
	}, nil
}

func collectChatMessageCitations(collector *citationCollector, message chatResponseMessage) {
	collectRawCitations(collector, message.Annotations)
	collectRawCitations(collector, message.Citations)
	collectRawCitations(collector, message.Sources)
}

func collectRawCitations(collector *citationCollector, rawCitations []json.RawMessage) {
	for _, rawCitation := range rawCitations {
		collector.addRaw(rawCitation)
	}
}

func identifyChatCompletionsResponse(bufferedBody *bufio.Reader) (io.Reader, bool, error) {
	var inspectedPrefix bytes.Buffer
	for {
		line, readErr := bufferedBody.ReadString('\n')
		inspectedPrefix.WriteString(line)

		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "data:") || strings.HasPrefix(trimmedLine, "event:") {
			return io.MultiReader(bytes.NewReader(inspectedPrefix.Bytes()), bufferedBody), true, nil
		}
		if strings.HasPrefix(trimmedLine, "{") || strings.HasPrefix(trimmedLine, "[") {
			return io.MultiReader(bytes.NewReader(inspectedPrefix.Bytes()), bufferedBody), false, nil
		}

		if readErr == io.EOF {
			return bytes.NewReader(inspectedPrefix.Bytes()), false, nil
		}
		if readErr != nil {
			return nil, false, fmt.Errorf("inspect chat completions response: %w", readErr)
		}
	}
}
