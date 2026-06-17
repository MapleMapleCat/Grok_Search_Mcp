package grok

import (
	"encoding/json"
	"fmt"
	"strings"
)

func validateSearchRequest(req SearchRequest) error {
	if strings.TrimSpace(req.Query) == "" {
		return fmt.Errorf("query must not be empty")
	}
	if req.ToolType != ToolTypeWebSearch && req.ToolType != ToolTypeXSearch {
		return fmt.Errorf("unsupported tool type: %q", req.ToolType)
	}
	if len(req.AllowedDomains) > 0 && len(req.ExcludedDomains) > 0 {
		return fmt.Errorf("allowed_domains and excluded_domains cannot be used together")
	}
	if len(req.AllowedDomains) > 5 {
		return fmt.Errorf("allowed_domains supports at most 5 entries")
	}
	if len(req.ExcludedDomains) > 5 {
		return fmt.Errorf("excluded_domains supports at most 5 entries")
	}
	return nil
}

func (c *Client) buildToolDef(req SearchRequest) toolDef {
	tool := toolDef{Type: string(req.ToolType)}

	if req.ToolType == ToolTypeWebSearch {
		tool.AllowedDomains = req.AllowedDomains
		tool.ExcludedDomains = req.ExcludedDomains
		tool.EnableImageUnderstanding = req.EnableImageUnderstanding
		tool.EnableImageSearch = req.EnableImageSearch
	}

	return tool
}

func (c *Client) buildSearchRequestBody(req SearchRequest) (string, []byte, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = c.defaultModel
	}

	upstreamReq := responsesRequest{
		Model:  model,
		Input:  []inputMessage{{Role: "user", Content: req.Query}},
		Tools:  []toolDef{c.buildToolDef(req)},
		Stream: true,
	}

	body, err := json.Marshal(upstreamReq)
	if err != nil {
		return "", nil, fmt.Errorf("marshal request: %w", err)
	}
	return model, body, nil
}