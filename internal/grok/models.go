package grok

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxModelsResponseBytes int64 = 1 << 20

type upstreamModelsResponse struct {
	Data []upstreamModel `json:"data"`
}

type upstreamModel struct {
	ID string `json:"id"`
}

// ListModels fetches the upstream /v1/models list and returns only Grok models.
// The Grok keyword filter is applied here before any model list leaves the
// upstream client, and callers should defensively reapply FilterGrokModels at
// their own boundary before exposing the list further downstream.
func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	snapshot := c.snapshot()
	resp, err := snapshot.getModels(ctx)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, snapshot.httpError(resp)
	}

	models, err := decodeModelsResponse(resp.Body)
	if err != nil {
		return nil, err
	}
	return FilterGrokModels(models), nil
}

func (s clientSnapshot) getModels(ctx context.Context) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create models request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("models request failed: %w", err)
	}
	return resp, nil
}

func decodeModelsResponse(body io.Reader) ([]Model, error) {
	var upstreamResponse upstreamModelsResponse
	decoder := json.NewDecoder(io.LimitReader(body, maxModelsResponseBytes))
	if err := decoder.Decode(&upstreamResponse); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}

	models := make([]Model, 0, len(upstreamResponse.Data))
	for _, upstreamModel := range upstreamResponse.Data {
		models = append(models, Model{ID: upstreamModel.ID})
	}
	return models, nil
}

// FilterGrokModels normalizes, deduplicates, and keeps only model IDs that
// contain the "grok" keyword, case-insensitively.
func FilterGrokModels(models []Model) []Model {
	filteredModels := make([]Model, 0, len(models))
	seenModelIDs := make(map[string]struct{}, len(models))

	for _, model := range models {
		modelID := strings.TrimSpace(model.ID)
		if modelID == "" || !strings.Contains(strings.ToLower(modelID), "grok") {
			continue
		}
		if _, alreadySeen := seenModelIDs[modelID]; alreadySeen {
			continue
		}

		seenModelIDs[modelID] = struct{}{}
		filteredModels = append(filteredModels, Model{ID: modelID})
	}

	return filteredModels
}
