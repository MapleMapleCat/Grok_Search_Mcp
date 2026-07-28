package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/config"
	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/grok"
	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/logx"
	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/usage"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServerInstructionsDocumentSearchToolUsage(t *testing.T) {
	wantedSnippets := []string{
		webSearchToolName,
		xSearchToolName,
		listModelsToolName,
		"Omit model to use the server default",
		"Tool errors are recoverable MCP results",
	}

	for _, wantedSnippet := range wantedSnippets {
		if !strings.Contains(ServerInstructions, wantedSnippet) {
			t.Fatalf("ServerInstructions missing %q", wantedSnippet)
		}
	}
}

func TestNewSearchToolMetadata(t *testing.T) {
	testCases := []struct {
		name            string
		title           string
		description     string
		constructTool   func() *mcp.Tool
		expectedFields  []string
		forbiddenFields []string
	}{
		{
			name:           webSearchToolName,
			title:          webSearchToolTitle,
			description:    webSearchToolDescription,
			constructTool:  newWebSearchTool,
			expectedFields: []string{"query", "model", "allowed_domains", "excluded_domains", "enable_image_understanding", "enable_image_search"},
		},
		{
			name:            xSearchToolName,
			title:           xSearchToolTitle,
			description:     xSearchToolDescription,
			constructTool:   newXSearchTool,
			expectedFields:  []string{"query", "model"},
			forbiddenFields: []string{"allowed_domains", "excluded_domains", "enable_image_understanding", "enable_image_search"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tool := testCase.constructTool()
			if tool.Name != testCase.name {
				t.Fatalf("Name = %q, want %q", tool.Name, testCase.name)
			}
			if tool.Title != testCase.title {
				t.Fatalf("Title = %q, want %q", tool.Title, testCase.title)
			}
			if tool.Description != testCase.description {
				t.Fatalf("Description = %q, want %q", tool.Description, testCase.description)
			}
			schema, ok := tool.InputSchema.(*jsonschema.Schema)
			if !ok {
				t.Fatalf("InputSchema type = %T, want *jsonschema.Schema", tool.InputSchema)
			}
			for _, propertyName := range testCase.expectedFields {
				if schema.Properties[propertyName] == nil {
					t.Fatalf("input schema missing %q", propertyName)
				}
			}
			for _, propertyName := range testCase.forbiddenFields {
				if schema.Properties[propertyName] != nil {
					t.Fatalf("input schema must not expose %q", propertyName)
				}
			}
			if tool.Annotations == nil {
				t.Fatalf("Annotations must be set")
			}
			if !tool.Annotations.ReadOnlyHint {
				t.Fatalf("ReadOnlyHint must be true")
			}
			if tool.Annotations.OpenWorldHint == nil || !*tool.Annotations.OpenWorldHint {
				t.Fatalf("OpenWorldHint must be true")
			}
		})
	}
}

func TestNewListModelsToolMetadata(t *testing.T) {
	tool := newListModelsTool()
	if tool.Name != listModelsToolName {
		t.Fatalf("Name = %q, want %q", tool.Name, listModelsToolName)
	}
	if tool.Title != listModelsToolTitle {
		t.Fatalf("Title = %q, want %q", tool.Title, listModelsToolTitle)
	}
	if tool.Description != listModelsToolDescription {
		t.Fatalf("Description = %q, want %q", tool.Description, listModelsToolDescription)
	}
	if tool.Annotations == nil {
		t.Fatalf("Annotations must be set")
	}
	if !tool.Annotations.ReadOnlyHint {
		t.Fatalf("ReadOnlyHint must be true")
	}
	if tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
		t.Fatalf("OpenWorldHint must be false for model listing")
	}
}

func TestWebSearchInputSchema(t *testing.T) {
	schema := newWebSearchInputSchema()

	required := false
	for _, r := range schema.Required {
		if r == "query" {
			required = true
		}
	}
	if !required {
		t.Fatalf("query must be required; required=%v", schema.Required)
	}

	queryProperty := schema.Properties["query"]
	if queryProperty == nil {
		t.Fatalf("query property missing from schema")
	}
	if queryProperty.MinLength == nil || *queryProperty.MinLength != 1 {
		t.Fatalf("query minLength = %v, want 1", queryProperty.MinLength)
	}

	modelProperty := schema.Properties["model"]
	if modelProperty == nil || modelProperty.Pattern != `[gG][rR][oO][kK]` {
		t.Fatalf("model pattern = %q, want case-insensitive grok pattern", modelProperty.Pattern)
	}

	webSearchProperties := []string{
		"allowed_domains",
		"excluded_domains",
		"enable_image_understanding",
		"enable_image_search",
	}
	for _, propertyName := range webSearchProperties {
		property := schema.Properties[propertyName]
		if property == nil {
			t.Fatalf("web search schema missing %q", propertyName)
		}
	}

	for _, propertyName := range []string{"allowed_domains", "excluded_domains"} {
		property := schema.Properties[propertyName]
		if property.MaxItems == nil || *property.MaxItems != 5 {
			t.Fatalf("%s maxItems = %v, want 5", propertyName, property.MaxItems)
		}
	}
	if schema.Not == nil || len(schema.Not.Required) != 2 || schema.Not.Required[0] != "allowed_domains" || schema.Not.Required[1] != "excluded_domains" {
		t.Fatalf("schema must reject simultaneous domain filters; not=%+v", schema.Not)
	}
}

func TestXSearchInputSchemaOmitsWebOnlyFields(t *testing.T) {
	schema := newXSearchInputSchema()

	required := false
	for _, requiredPropertyName := range schema.Required {
		if requiredPropertyName == "query" {
			required = true
		}
	}
	if !required {
		t.Fatalf("query must be required; required=%v", schema.Required)
	}

	if schema.Properties["query"] == nil {
		t.Fatalf("query property missing from x search schema")
	}
	if schema.Properties["model"] == nil {
		t.Fatalf("model property missing from x search schema")
	}

	webOnlyProperties := []string{
		"allowed_domains",
		"excluded_domains",
		"enable_image_understanding",
		"enable_image_search",
	}
	for _, propertyName := range webOnlyProperties {
		if schema.Properties[propertyName] != nil {
			t.Fatalf("x search schema must not expose %q", propertyName)
		}
	}
}

func TestFormatSearchRoundMessageSearch(t *testing.T) {
	got := formatSearchRoundMessage(grok.SearchRound{Round: 1, Query: "capital of France"})
	want := `Search round 1: querying "capital of France"`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatSearchRoundMessageFetch(t *testing.T) {
	got := formatSearchRoundMessage(grok.SearchRound{Round: 2, URL: "https://example.com/france"})
	want := "Search round 2: reading https://example.com/france"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatSearchRoundMessageEmpty(t *testing.T) {
	got := formatSearchRoundMessage(grok.SearchRound{Round: 3})
	want := "Search round 3: searching"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRunSearchReturnsStructuredOutputFromUpstream(t *testing.T) {
	var capturedRequest struct {
		Input []struct {
			Content string `json:"content"`
		} `json:"input"`
		Tools []struct {
			Type           string   `json:"type"`
			AllowedDomains []string `json:"allowed_domains"`
		} `json:"tools"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("request path = %q, want %q", r.URL.Path, "/v1/responses")
		}
		if authorization := r.Header.Get("Authorization"); authorization != "Bearer test-cpa-key" {
			t.Fatalf("Authorization = %q, want %q", authorization, "Bearer test-cpa-key")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if err := json.Unmarshal(body, &capturedRequest); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		responseJSON := `{"output":[{"role":"assistant","content":[{"type":"output_text","text":"structured answer","annotations":[{"type":"url_citation","url":"https://example.com/source","title":"Example Source"}]}]}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + completedEventForMCPTest(responseJSON) + "\n\n"))
	}))
	defer server.Close()

	toolResult, output, err := runSearch(context.Background(), nil, newMCPTestClient(t, server.URL), logx.NewWithDebugState("mcp-test", logx.NewDebugState(false)), grok.SearchRequest{
		Query:          "  structured query  ",
		ToolType:       grok.ToolTypeWebSearch,
		AllowedDomains: []string{"example.com"},
	})
	if err != nil {
		t.Fatalf("runSearch returned Go error: %v", err)
	}
	if toolResult != nil {
		t.Fatalf("runSearch returned unexpected tool error: %+v", toolResult)
	}
	if output.Answer != "structured answer" {
		t.Fatalf("Answer = %q, want %q", output.Answer, "structured answer")
	}
	if len(output.Citations) != 1 || output.Citations[0] != "https://example.com/source" {
		t.Fatalf("unexpected citations: %+v", output.Citations)
	}
	if output.Usage == nil || output.Usage.TotalTokens != 3 {
		t.Fatalf("unexpected usage: %+v", output.Usage)
	}
	if len(capturedRequest.Input) != 1 || capturedRequest.Input[0].Content != "structured query" {
		t.Fatalf("expected trimmed query in upstream request, got %+v", capturedRequest.Input)
	}
	if len(capturedRequest.Tools) != 1 || capturedRequest.Tools[0].Type != "web_search" || len(capturedRequest.Tools[0].AllowedDomains) != 1 {
		t.Fatalf("unexpected upstream tools request: %+v", capturedRequest.Tools)
	}
}

func TestRunSearchMarksAuthoritativeSemanticOutcome(t *testing.T) {
	t.Run("validation error", func(t *testing.T) {
		ctx, marker := usage.WithToolOutcomeMarker(context.Background())
		toolResult, _, err := runSearch(ctx, nil, nil, logx.NewWithDebugState("mcp-test", logx.NewDebugState(false)), grok.SearchRequest{})
		if err != nil || toolResult == nil || !toolResult.IsError {
			t.Fatalf("unexpected validation result: toolResult=%+v err=%v", toolResult, err)
		}
		semanticSuccess, known := marker.Outcome()
		if !known || semanticSuccess {
			t.Fatalf("semantic outcome = (%t, %t), want known error", semanticSuccess, known)
		}
	})

	t.Run("successful search", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			responseJSON := `{"output":[{"role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: " + completedEventForMCPTest(responseJSON) + "\n\n"))
		}))
		defer server.Close()

		ctx, marker := usage.WithToolOutcomeMarker(context.Background())
		toolResult, output, err := runSearch(ctx, nil, newMCPTestClient(t, server.URL), logx.NewWithDebugState("mcp-test", logx.NewDebugState(false)), grok.SearchRequest{
			Query:    "semantic outcome",
			ToolType: grok.ToolTypeWebSearch,
		})
		if err != nil || toolResult != nil || output.Answer != "ok" {
			t.Fatalf("unexpected success result: toolResult=%+v output=%+v err=%v", toolResult, output, err)
		}
		semanticSuccess, known := marker.Outcome()
		if !known || !semanticSuccess {
			t.Fatalf("semantic outcome = (%t, %t), want known success", semanticSuccess, known)
		}
	})
}

func TestRunSearchUsesRuntimeDebugState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		responseJSON := `{"output":[{"role":"assistant","content":[{"type":"output_text","text":"runtime debug answer"}]}]}`
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + completedEventForMCPTest(responseJSON) + "\n\n"))
	}))
	defer server.Close()

	configuration := &config.Config{
		CPABaseURL:       server.URL,
		CPAAPIKey:        "test-cpa-key",
		UpstreamProtocol: config.UpstreamProtocolResponses,
		Model:            "grok-4.3",
		Timeout:          5 * time.Second,
		RegistrationMode: "free",
		Debug:            false,
	}
	debugState := logx.NewDebugState(false)
	client, err := grok.NewClientWithServerSettings(configuration.ServerSettings(), debugState)
	if err != nil {
		t.Fatalf("NewClientWithServerSettings failed: %v", err)
	}
	mcpLogger := logx.NewWithDebugState("mcp-test", debugState)

	var logBuffer bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	log.SetOutput(&logBuffer)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})

	runSearchForDebugTest := func(query string) {
		t.Helper()
		toolResult, output, err := runSearch(context.Background(), nil, client, mcpLogger, grok.SearchRequest{
			Query:    query,
			ToolType: grok.ToolTypeWebSearch,
		})
		if err != nil {
			t.Fatalf("runSearch returned Go error: %v", err)
		}
		if toolResult != nil || output.Answer != "runtime debug answer" {
			t.Fatalf("unexpected runSearch result: toolResult=%+v output=%+v", toolResult, output)
		}
	}

	runSearchForDebugTest("disabled query")
	if logBuffer.Len() != 0 {
		t.Fatalf("expected no debug logs before runtime enable, got %q", logBuffer.String())
	}

	settings := configuration.ServerSettings()
	settings.Debug = true
	if err := client.ApplyServerSettings(settings); err != nil {
		t.Fatalf("enable runtime debug: %v", err)
	}
	privateQueryMarker := "private-query-marker"
	runSearchForDebugTest(privateQueryMarker)
	debugLogOutput := logBuffer.String()
	if !strings.Contains(debugLogOutput, "[mcp-test] search start tool=web_search") {
		t.Fatalf("expected retained MCP logger to observe runtime enable, got %q", debugLogOutput)
	}
	if strings.Contains(debugLogOutput, privateQueryMarker) {
		t.Fatalf("debug log disclosed private query marker: %q", debugLogOutput)
	}

	logBuffer.Reset()
	settings.Debug = false
	if err := client.ApplyServerSettings(settings); err != nil {
		t.Fatalf("disable runtime debug: %v", err)
	}
	runSearchForDebugTest("disabled again")
	if logBuffer.Len() != 0 {
		t.Fatalf("expected retained MCP logger to observe runtime disable, got %q", logBuffer.String())
	}
}

func TestRunSearchMapsValidationAndUpstreamErrorsToMCPToolErrors(t *testing.T) {
	t.Run("missing query", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			t.Fatalf("missing-query validation should not call upstream, got %s %s", r.Method, r.URL.Path)
		}))
		defer server.Close()

		toolResult, output, err := runSearch(context.Background(), nil, newMCPTestClient(t, server.URL), logx.NewWithDebugState("mcp-test", logx.NewDebugState(false)), grok.SearchRequest{
			Query:    "   ",
			ToolType: grok.ToolTypeWebSearch,
		})
		if err != nil {
			t.Fatalf("runSearch returned Go error: %v", err)
		}
		if output.Answer != "" {
			t.Fatalf("expected empty output on validation error, got %+v", output)
		}
		if got := toolErrorText(t, toolResult); got != "query must be non-empty" {
			t.Fatalf("tool error text = %q, want %q", got, "query must be non-empty")
		}
	})

	validationCases := []struct {
		name            string
		request         grok.SearchRequest
		expectedMessage string
	}{
		{
			name: "mutually exclusive domain filters",
			request: grok.SearchRequest{
				Query:           "domain conflict",
				ToolType:        grok.ToolTypeWebSearch,
				AllowedDomains:  []string{"example.com"},
				ExcludedDomains: []string{"other.example"},
			},
			expectedMessage: "allowed_domains and excluded_domains cannot be used together",
		},
		{
			name: "domain URL",
			request: grok.SearchRequest{
				Query:          "domain URL",
				ToolType:       grok.ToolTypeWebSearch,
				AllowedDomains: []string{"https://example.com"},
			},
			expectedMessage: "allowed_domains entry 0 is invalid: scheme is not allowed",
		},
		{
			name: "unsupported model",
			request: grok.SearchRequest{
				Query:    "model override",
				ToolType: grok.ToolTypeWebSearch,
				Model:    "gpt-4",
			},
			expectedMessage: "unsupported model (must contain 'grok'); omit model to use the server default",
		},
	}

	for _, validationCase := range validationCases {
		t.Run(validationCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
				t.Fatalf("parameter validation should not call upstream, got %s %s", request.Method, request.URL.Path)
			}))
			defer server.Close()

			toolResult, output, err := runSearch(
				context.Background(),
				nil,
				newMCPTestClient(t, server.URL),
				logx.NewWithDebugState("mcp-test", logx.NewDebugState(false)),
				validationCase.request,
			)
			if err != nil {
				t.Fatalf("runSearch returned Go error: %v", err)
			}
			if output.Answer != "" {
				t.Fatalf("expected empty output on validation error, got %+v", output)
			}
			if got := toolErrorText(t, toolResult); got != validationCase.expectedMessage {
				t.Fatalf("tool error text = %q, want %q", got, validationCase.expectedMessage)
			}
		})
	}

	t.Run("upstream HTTP status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("sensitive upstream details"))
		}))
		defer server.Close()

		toolResult, output, err := runSearch(context.Background(), nil, newMCPTestClient(t, server.URL), logx.NewWithDebugState("mcp-test", logx.NewDebugState(false)), grok.SearchRequest{
			Query:    "upstream failure",
			ToolType: grok.ToolTypeWebSearch,
		})
		if err != nil {
			t.Fatalf("runSearch returned Go error: %v", err)
		}
		if output.Answer != "" {
			t.Fatalf("expected empty output on upstream error, got %+v", output)
		}
		if got := toolErrorText(t, toolResult); got != "upstream returned HTTP 502" {
			t.Fatalf("tool error text = %q, want %q", got, "upstream returned HTTP 502")
		}
		if strings.Contains(toolErrorText(t, toolResult), "sensitive upstream details") {
			t.Fatalf("tool error leaked upstream body: %q", toolErrorText(t, toolResult))
		}
	})
}

func TestRunSearchSendsXSearchToolTypeWithoutWebOnlyFields(t *testing.T) {
	var capturedRequest map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if err := json.Unmarshal(body, &capturedRequest); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		responseJSON := `{"output":[{"role":"assistant","content":[{"type":"output_text","text":"x answer"}]}]}`
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + completedEventForMCPTest(responseJSON) + "\n\n"))
	}))
	defer server.Close()

	toolResult, output, err := runSearch(context.Background(), nil, newMCPTestClient(t, server.URL), logx.NewWithDebugState("mcp-test", logx.NewDebugState(false)), grok.SearchRequest{
		Query:                    "x query",
		ToolType:                 grok.ToolTypeXSearch,
		AllowedDomains:           []string{"ignored.example"},
		EnableImageUnderstanding: boolPointerForMCPTest(true),
	})
	if err != nil {
		t.Fatalf("runSearch returned Go error: %v", err)
	}
	if toolResult != nil || output.Answer != "x answer" {
		t.Fatalf("unexpected runSearch result: toolResult=%+v output=%+v", toolResult, output)
	}

	tools, ok := capturedRequest["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("unexpected tools payload: %+v", capturedRequest["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected tool payload: %+v", tools[0])
	}
	if tool["type"] != "x_search" {
		t.Fatalf("tool type = %v, want x_search", tool["type"])
	}
	for _, webOnlyField := range []string{"allowed_domains", "excluded_domains", "enable_image_understanding", "enable_image_search"} {
		if _, exists := tool[webOnlyField]; exists {
			t.Fatalf("x_search request must not include %q: %+v", webOnlyField, tool)
		}
	}
}

func TestRunListModelsReturnsOnlyFilteredGrokModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("request path = %q, want %q", r.URL.Path, "/v1/models")
		}
		if authorization := r.Header.Get("Authorization"); authorization != "Bearer test-cpa-key" {
			t.Fatalf("Authorization = %q, want %q", authorization, "Bearer test-cpa-key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"grok-4.3"},{"id":"gpt-4"},{"id":" Grok-Beta "},{"id":"grok-imagine-image"},{"id":"grok-imagine-video"},{"id":"grok-video-preview"},{"id":"grok-4.3"}]}`))
	}))
	defer server.Close()

	toolResult, output, err := runListModels(context.Background(), newMCPTestClient(t, server.URL), logx.NewWithDebugState("mcp-test", logx.NewDebugState(false)))
	if err != nil {
		t.Fatalf("runListModels returned Go error: %v", err)
	}
	if toolResult != nil {
		t.Fatalf("runListModels returned unexpected tool error: %+v", toolResult)
	}

	modelIDs := make([]string, 0, len(output.Models))
	for _, model := range output.Models {
		modelIDs = append(modelIDs, model.ID)
	}
	wantedModelIDs := []string{"grok-4.3", "Grok-Beta"}
	if len(modelIDs) != len(wantedModelIDs) {
		t.Fatalf("model IDs = %+v, want %+v", modelIDs, wantedModelIDs)
	}
	for index, wantedModelID := range wantedModelIDs {
		if modelIDs[index] != wantedModelID {
			t.Fatalf("model IDs = %+v, want %+v", modelIDs, wantedModelIDs)
		}
	}
}

func newMCPTestClient(t *testing.T, baseURL string) *grok.Client {
	t.Helper()
	configuration := &config.Config{
		CPABaseURL:       baseURL,
		CPAAPIKey:        "test-cpa-key",
		UpstreamProtocol: config.UpstreamProtocolResponses,
		Model:            "grok-4.3",
		Timeout:          5 * time.Second,
		RegistrationMode: "free",
	}
	client, err := grok.NewClientWithServerSettings(configuration.ServerSettings(), nil)
	if err != nil {
		t.Fatalf("NewClientWithServerSettings failed: %v", err)
	}
	return client
}

func completedEventForMCPTest(responseJSON string) string {
	return `{"type":"response.completed","response":` + strings.TrimSpace(responseJSON) + `}`
}

func toolErrorText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil {
		t.Fatal("expected MCP tool error result, got nil")
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true, got %+v", result)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected exactly one error content item, got %+v", result.Content)
	}
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected text error content, got %T", result.Content[0])
	}
	return textContent.Text
}

func boolPointerForMCPTest(value bool) *bool {
	return &value
}
