package grok

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/grok-mcp/internal/config"
	"github.com/grok-mcp/internal/logx"
)

type Client struct {
	baseURL      string
	apiKey       string
	defaultModel string
	httpClient   *http.Client
	log          *logx.Logger
}

func NewClient(cfg *config.Config) *Client {
	return &Client{
		baseURL:      cfg.CPABaseURL,
		apiKey:       cfg.CPAAPIKey,
		defaultModel: cfg.Model,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		log: logx.New("grok", cfg.Debug),
	}
}

// SearchStream performs a streaming search and invokes onRound for each upstream
// web_search_call action before returning the final parsed result.
func (c *Client) SearchStream(ctx context.Context, req SearchRequest, onRound func(SearchRound)) (*SearchResult, error) {
	if err := validateSearchRequest(req); err != nil {
		return nil, err
	}

	model, body, err := c.buildSearchRequestBody(req)
	if err != nil {
		return nil, err
	}
	c.log.Debugf("SearchStream start model=%s tool=%s query=%q", model, req.ToolType, logx.Truncate(req.Query, 80))

	resp, err := c.post(ctx, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.httpError(resp)
	}

	result, err := parseSearchStream(resp.Body, onRound, c.log)
	if err != nil {
		return nil, err
	}
	if result.Usage != nil {
		c.log.Debugf("SearchStream done tokens=%d", result.Usage.TotalTokens)
	}
	return result, nil
}

func (c *Client) post(ctx context.Context, body []byte) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	return resp, nil
}

func (c *Client) httpError(resp *http.Response) error {
	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		c.log.Debugf("upstream HTTP %d read body error: %v", resp.StatusCode, readErr)
		return fmt.Errorf("upstream returned HTTP %d: read body: %w", resp.StatusCode, readErr)
	}
	c.log.Debugf("upstream HTTP %d: %s", resp.StatusCode, logx.Truncate(string(respBody), 256))
	return fmt.Errorf("upstream returned HTTP %d: %s", resp.StatusCode, logx.Truncate(string(respBody), 1024))
}