// Package mcpserver 将 grok 搜索能力注册为 MCP 工具（grok_web_search、grok_x_search）。
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/grok"
	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/logx"
	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/usage"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	webSearchToolName        = "grok_web_search"
	webSearchToolTitle       = "Grok Web Search"
	webSearchToolDescription = "Search the current public web with Grok."

	xSearchToolName        = "grok_x_search"
	xSearchToolTitle       = "Grok X Search"
	xSearchToolDescription = "Search current public X posts with Grok."

	listModelsToolName        = "grok_list_models"
	listModelsToolTitle       = "Grok List Models"
	listModelsToolDescription = "List available Grok model IDs for optional model overrides."
)

// IsSearchToolName reports whether a tool executes a streaming upstream search.
// Model listing is intentionally excluded because it does not hold an SSE search open.
func IsSearchToolName(toolName string) bool {
	return toolName == webSearchToolName || toolName == xSearchToolName
}

// WebSearchInput 为 grok_web_search 的 JSON 入参；工具注册时提供显式 schema。
type WebSearchInput struct {
	Query                    string   `json:"query"`
	Model                    string   `json:"model,omitempty"`
	AllowedDomains           []string `json:"allowed_domains,omitempty"`
	ExcludedDomains          []string `json:"excluded_domains,omitempty"`
	EnableImageUnderstanding *bool    `json:"enable_image_understanding,omitempty"`
	EnableImageSearch        *bool    `json:"enable_image_search,omitempty"`
}

// XSearchInput 为 grok_x_search 的 JSON 入参。X 搜索不暴露 web_search 专属的域名或图片选项。
type XSearchInput struct {
	Query string `json:"query"`
	Model string `json:"model,omitempty"`
}

// ListModelsInput 为 grok_list_models 的 JSON 入参；该工具不需要参数。
type ListModelsInput struct{}

// SearchOutput 为工具成功时的结构化返回，会序列化为 MCP 工具结果 JSON。
// 失败时返回零值 SearchOutput（各字段为零值），由 toolError 单独承载错误文案。
type SearchOutput struct {
	Answer    string        `json:"answer,omitempty" jsonschema:"Generated answer."`
	Citations []string      `json:"citations,omitempty" jsonschema:"Cited source URLs."`
	Sources   []grok.Source `json:"sources,omitempty" jsonschema:"Source metadata."`
	Usage     *grok.Usage   `json:"usage,omitempty" jsonschema:"Upstream token usage."`
}

// ListModelsOutput 为模型列表工具成功时的结构化返回。
type ListModelsOutput struct {
	Models []grok.Model `json:"models,omitempty" jsonschema:"Available Grok models."`
}

// RegisterToolsWithLogger 使用可动态配置的日志器注册 MCP 工具。
func RegisterToolsWithLogger(server *mcp.Server, client *grok.Client, log *logx.Logger) {
	registerListModelsTool(server, client, log)
	registerWebSearchTool(server, client, log)
	registerXSearchTool(server, client, log)
}

func registerListModelsTool(server *mcp.Server, client *grok.Client, log *logx.Logger) {
	mcp.AddTool(server, newListModelsTool(), func(ctx context.Context, _ *mcp.CallToolRequest, _ ListModelsInput) (*mcp.CallToolResult, ListModelsOutput, error) {
		return runListModels(ctx, client, log)
	})
}

func registerWebSearchTool(server *mcp.Server, client *grok.Client, log *logx.Logger) {
	mcp.AddTool(server, newWebSearchTool(), func(ctx context.Context, req *mcp.CallToolRequest, input WebSearchInput) (*mcp.CallToolResult, SearchOutput, error) {
		searchReq := grok.SearchRequest{
			Query:                    input.Query,
			Model:                    input.Model,
			ToolType:                 grok.ToolTypeWebSearch,
			AllowedDomains:           input.AllowedDomains,
			ExcludedDomains:          input.ExcludedDomains,
			EnableImageUnderstanding: input.EnableImageUnderstanding,
			EnableImageSearch:        input.EnableImageSearch,
		}
		return runSearch(ctx, req, client, log, searchReq)
	})
}

func registerXSearchTool(server *mcp.Server, client *grok.Client, log *logx.Logger) {
	mcp.AddTool(server, newXSearchTool(), func(ctx context.Context, req *mcp.CallToolRequest, input XSearchInput) (*mcp.CallToolResult, SearchOutput, error) {
		searchReq := grok.SearchRequest{
			Query:    input.Query,
			Model:    input.Model,
			ToolType: grok.ToolTypeXSearch,
		}
		return runSearch(ctx, req, client, log, searchReq)
	})
}

func newListModelsTool() *mcp.Tool {
	openWorldHint := false
	return &mcp.Tool{
		Name:        listModelsToolName,
		Title:       listModelsToolTitle,
		Description: listModelsToolDescription,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:  true,
			OpenWorldHint: &openWorldHint,
		},
	}
}

func newWebSearchTool() *mcp.Tool {
	return newSearchTool(webSearchToolName, webSearchToolTitle, webSearchToolDescription, newWebSearchInputSchema())
}

func newXSearchTool() *mcp.Tool {
	return newSearchTool(xSearchToolName, xSearchToolTitle, xSearchToolDescription, newXSearchInputSchema())
}

func newSearchTool(name, title, description string, inputSchema *jsonschema.Schema) *mcp.Tool {
	openWorldHint := true
	return &mcp.Tool{
		Name:        name,
		Title:       title,
		Description: description,
		InputSchema: inputSchema,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:  true,
			OpenWorldHint: &openWorldHint,
		},
	}
}

func newWebSearchInputSchema() *jsonschema.Schema {
	maximumDomainCount := 5
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"query": newSearchQuerySchema(),
			"model": newSearchModelSchema(),
			"allowed_domains": {
				Type:        "array",
				Description: "Plain hostnames to include, such as example.com. No URLs, paths, ports, wildcards, IPs, or local hosts. Max 5; do not use with excluded_domains.",
				Items:       &jsonschema.Schema{Type: "string"},
				MaxItems:    &maximumDomainCount,
			},
			"excluded_domains": {
				Type:        "array",
				Description: "Plain hostnames to exclude, such as example.com. No URLs, paths, ports, wildcards, IPs, or local hosts. Max 5; do not use with allowed_domains.",
				Items:       &jsonschema.Schema{Type: "string"},
				MaxItems:    &maximumDomainCount,
			},
			"enable_image_understanding": {
				Type:        "boolean",
				Description: "Enable image understanding.",
			},
			"enable_image_search": {
				Type:        "boolean",
				Description: "Include image search results.",
			},
		},
		Required:             []string{"query"},
		Not:                  &jsonschema.Schema{Required: []string{"allowed_domains", "excluded_domains"}},
		AdditionalProperties: closedObjectAdditionalPropertiesSchema(),
		PropertyOrder: []string{
			"query",
			"model",
			"allowed_domains",
			"excluded_domains",
			"enable_image_understanding",
			"enable_image_search",
		},
	}
}

func newXSearchInputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"query": newSearchQuerySchema(),
			"model": newSearchModelSchema(),
		},
		Required:             []string{"query"},
		AdditionalProperties: closedObjectAdditionalPropertiesSchema(),
		PropertyOrder:        []string{"query", "model"},
	}
}

func newSearchQuerySchema() *jsonschema.Schema {
	minimumQueryLength := 1
	return &jsonschema.Schema{
		Type:        "string",
		Description: "Non-empty search query.",
		MinLength:   &minimumQueryLength,
	}
}

func newSearchModelSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:        "string",
		Description: `Optional model ID containing "grok"; omit to use the server default.`,
		Pattern:     `[gG][rR][oO][kK]`,
	}
}

func closedObjectAdditionalPropertiesSchema() *jsonschema.Schema {
	return &jsonschema.Schema{Not: &jsonschema.Schema{}}
}

// runListModels 调用上游模型列表接口，并在向 MCP 客户端返回前再次应用 Grok 关键词过滤。
func runListModels(ctx context.Context, client *grok.Client, log *logx.Logger) (*mcp.CallToolResult, ListModelsOutput, error) {
	if client == nil {
		usage.MarkToolOutcome(ctx, false)
		return toolError("model listing is not configured"), ListModelsOutput{}, nil
	}

	log.Debugf("list models start")
	models, err := client.ListModels(ctx)
	if err != nil {
		log.Debugf("list models failed category=%s", safeErrorCategory(err))
		usage.MarkToolOutcome(ctx, false)
		return toolError(classifyListModelsError(err)), ListModelsOutput{}, nil
	}

	filteredModels := grok.FilterGrokModels(models)
	log.Debugf("list models done models=%d", len(filteredModels))
	usage.MarkToolOutcome(ctx, true)
	return nil, ListModelsOutput{Models: filteredModels}, nil
}

// runSearch 调用上游流式搜索，并在客户端提供 progressToken 时推送每轮搜索进度通知。
func runSearch(ctx context.Context, req *mcp.CallToolRequest, client *grok.Client, log *logx.Logger, searchReq grok.SearchRequest) (*mcp.CallToolResult, SearchOutput, error) {
	query := strings.TrimSpace(searchReq.Query)
	if query == "" {
		usage.MarkToolOutcome(ctx, false)
		return toolError("query must be non-empty"), SearchOutput{}, nil
	}
	searchReq.Query = query
	searchReq.Model = strings.TrimSpace(searchReq.Model)
	toolType := searchReq.ToolType

	var progress float64
	var token any
	if req != nil {
		token = req.Params.GetProgressToken()
	}

	log.Debugf(
		"search start tool=%s query_bytes=%d model_override=%t allowed_domains=%d excluded_domains=%d",
		toolType,
		len(query),
		searchReq.Model != "",
		len(searchReq.AllowedDomains),
		len(searchReq.ExcludedDomains),
	)
	result, err := client.SearchStream(ctx, searchReq, func(round grok.SearchRound) {
		if token == nil || req == nil {
			return
		}
		progress++
		_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
			ProgressToken: token,
			Progress:      progress,
			// Grok search has no fixed total round count. Set Total to the current
			// progress so clients show "x/x" instead of an unknown-total "x/?".
			Total:   progress,
			Message: formatSearchRoundMessage(round),
		})
	})
	if err != nil {
		log.Debugf("search failed tool=%s category=%s", toolType, safeErrorCategory(err))
		usage.MarkToolOutcome(ctx, false)
		return toolError(classifySearchError(err)), SearchOutput{}, nil
	}

	log.Debugf("search done tool=%s citations=%d sources=%d", toolType, len(result.Citations), len(result.Sources))
	output := SearchOutput{
		Answer:    result.Answer,
		Citations: result.Citations,
		Sources:   result.Sources,
		Usage:     result.Usage,
	}
	usage.MarkToolOutcome(ctx, true)
	return nil, output, nil
}

// formatSearchRoundMessage keeps protocol-facing progress messages in English,
// matching the language used by MCP tool errors and schema descriptions.
func formatSearchRoundMessage(round grok.SearchRound) string {
	if q := strings.TrimSpace(round.Query); q != "" {
		return fmt.Sprintf("Search round %d: querying \"%s\"", round.Round, q)
	}
	if u := strings.TrimSpace(round.URL); u != "" {
		return fmt.Sprintf("Search round %d: reading %s", round.Round, u)
	}
	return fmt.Sprintf("Search round %d: searching", round.Round)
}

// toolError 构造 MCP 约定的 IsError 工具结果（不向 Go error 传播，避免断开会话）。
func toolError(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: message}},
	}
}

// classifySearchError 把上游/网络错误映射为面向 MCP 客户端的简短文案，
// 不泄露上游 body 细节（可能含 CPA 内部信息），但区分类别便于客户端处理。
func classifySearchError(err error) string {
	if err == nil {
		return "search request failed"
	}
	if validationMessage, ok := grok.SearchRequestErrorMessage(err); ok {
		return validationMessage
	}
	if upstreamError, ok := grok.UpstreamErrorMetadata(err); ok {
		switch upstreamError.Category {
		case grok.UpstreamErrorCategoryHTTPStatus:
			return fmt.Sprintf("upstream returned HTTP %d", upstreamError.StatusCode)
		case grok.UpstreamErrorCategoryStream:
			return "upstream stream error"
		case grok.UpstreamErrorCategoryTransport:
			if isTimeoutError(err) {
				return "upstream search timed out"
			}
			return "search request failed"
		default:
			return "search request failed"
		}
	}
	msg := err.Error()
	switch {
	case isTimeoutError(err):
		return "upstream search timed out"
	case strings.Contains(msg, "did not contain answer text"):
		return "upstream returned empty answer"
	case strings.Contains(msg, "ended without response.completed"):
		return "upstream stream ended prematurely"
	case strings.Contains(msg, "upstream stream error"):
		return "upstream stream error"
	default:
		return "search request failed"
	}
}

func classifyListModelsError(err error) string {
	if err == nil {
		return "model list request failed"
	}
	if upstreamError, ok := grok.UpstreamErrorMetadata(err); ok {
		if upstreamError.Category == grok.UpstreamErrorCategoryHTTPStatus {
			return fmt.Sprintf("upstream returned HTTP %d", upstreamError.StatusCode)
		}
	}
	switch {
	case isTimeoutError(err):
		return "upstream model list timed out"
	default:
		return "model list request failed"
	}
}

func safeErrorCategory(err error) string {
	if _, ok := grok.SearchRequestErrorMessage(err); ok {
		return "validation"
	}
	if upstreamError, ok := grok.UpstreamErrorMetadata(err); ok {
		return string(upstreamError.Category)
	}
	if isTimeoutError(err) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return "request"
}

func isTimeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}
