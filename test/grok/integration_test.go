package grok_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/grok-mcp/internal/config"
	"github.com/grok-mcp/internal/grok"
)

func TestIntegrationSearchLiveCPA(t *testing.T) {
	if os.Getenv("GROK_INTEGRATION_TEST") != "1" {
		t.Skip("set GROK_INTEGRATION_TEST=1 to run live CPA integration tests")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load failed: %v", err)
	}

	client := grok.NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	var webRounds []grok.SearchRound
	webResult, err := client.SearchStream(ctx, grok.SearchRequest{
		Query:    "What is the capital of France?",
		ToolType: grok.ToolTypeWebSearch,
	}, func(round grok.SearchRound) {
		webRounds = append(webRounds, round)
		t.Logf("web search round %d: query=%q url=%q", round.Round, round.Query, round.URL)
	})
	if err != nil {
		t.Fatalf("web search stream failed: %v", err)
	}
	if strings.TrimSpace(webResult.Answer) == "" {
		t.Fatalf("web search returned empty answer")
	}
	if len(webResult.Citations) == 0 {
		t.Fatalf("web search returned no citations; raw response: %s", string(webResult.RawResponse))
	}
	t.Logf("web search rounds (%d): %+v", len(webRounds), webRounds)
	t.Logf("web search sources (%d): %+v", len(webResult.Sources), webResult.Sources)
	t.Logf("web search raw response (stream): %s", prettyJSON(webResult.RawResponse))

	var xRounds []grok.SearchRound
	xResult, err := client.SearchStream(ctx, grok.SearchRequest{
		Query:    "What did Elon Musk post about SpaceX recently?",
		ToolType: grok.ToolTypeXSearch,
	}, func(round grok.SearchRound) {
		xRounds = append(xRounds, round)
		t.Logf("x search round %d: query=%q url=%q", round.Round, round.Query, round.URL)
	})
	if err != nil {
		t.Fatalf("x search stream failed: %v", err)
	}
	if strings.TrimSpace(xResult.Answer) == "" {
		t.Fatalf("x search returned empty answer")
	}
	if len(xResult.Citations) == 0 {
		t.Fatalf("x search returned no citations; raw response: %s", string(xResult.RawResponse))
	}
	t.Logf("x search rounds (%d): %+v", len(xRounds), xRounds)
	t.Logf("x search sources (%d): %+v", len(xResult.Sources), xResult.Sources)
}

func prettyJSON(raw json.RawMessage) string {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return string(raw)
	}
	return out.String()
}