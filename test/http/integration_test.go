package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grok-mcp/internal/auth"
	"github.com/grok-mcp/internal/config"
	"github.com/grok-mcp/internal/grok"
	mcpserver "github.com/grok-mcp/internal/mcp"
	"github.com/grok-mcp/internal/panel"
	"github.com/grok-mcp/internal/quota"
	"github.com/grok-mcp/internal/ratelimit"
	"github.com/grok-mcp/internal/store"
	"github.com/grok-mcp/internal/usage"
	"github.com/grok-mcp/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpAccept = "application/json, text/event-stream"

func setMCPHeaders(req *http.Request, apiKey string) {
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", mcpAccept)
}

func cpaMockSSE(t *testing.T) *httptest.Server {
	t.Helper()
	responseJSON := `{"output":[{"role":"assistant","content":[{"type":"output_text","text":"mock integration answer"}]}]}`
	completed := `{"type":"response.completed","response":` + strings.TrimSpace(responseJSON) + `}`
	body := "data: " + completed + "\n\n"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
}

func TestHTTPPanelAndMCPFlow(t *testing.T) {
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "int.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cpa := cpaMockSSE(t)
	defer cpa.Close()

	usageWriter := store.NewAsyncUsageWriter(st, 64)

	cfg := &config.Config{
		CPABaseURL:     cpa.URL,
		CPAAPIKey:      "cpa-mock-key",
		Model:          "grok-4.3",
		JWTSecret:      "jwt-secret-must-be-at-least-32-bytes!",
		DefaultUserRPM: 1000,
		Timeout:        30 * time.Second,
	}
	client := grok.NewClient(cfg)
	server := mcp.NewServer(&mcp.Implementation{Name: "grok-mcp", Version: version.Version}, nil)
	mcpserver.RegisterTools(server, client, false)

	userLim := ratelimit.NewUserLimiter(cfg.DefaultUserRPM)
	defer userLim.Close()

	authResolver := auth.NewCachedAPIKeyResolver(st, 30*time.Second)

	mcpHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{Stateless: true})

	var mcpChain http.Handler = mcpHandler
	mcpChain = panel.MaxBodyMiddleware(panel.MaxPanelBodyBytes())(mcpChain)
	mcpChain = usage.MCPMiddleware(st, usageWriter)(mcpChain)
	mcpChain = quota.MCPMiddleware(st)(mcpChain)
	mcpChain = usage.ExtractToolNameMiddleware()(mcpChain)
	mcpChain = userLim.UserMiddleware()(mcpChain)
	mcpChain = auth.APIKeyMiddleware(authResolver)(mcpChain)

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpChain)

	ph := &panel.Handler{Store: st, Config: cfg, AuthCache: authResolver}
	pm := panel.NewMux(ph)
	skip := map[string]struct{}{
		"/panel/v1/auth/register": {},
		"/panel/v1/auth/login":    {},
	}
	var panelChain http.Handler = pm
	panelChain = panel.MaxBodyMiddleware(panel.MaxPanelBodyBytes())(panelChain)
	panelChain = auth.JWTMiddleware(cfg.JWTSecret, st, skip)(panelChain)
	mux.Handle("/panel/", panelChain)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	regBody := `{"username":"intuser","password":"password123"}`
	regReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/register", bytes.NewBufferString(regBody))
	regResp, err := http.DefaultClient.Do(regReq)
	if err != nil {
		t.Fatal(err)
	}
	regResp.Body.Close()
	if regResp.StatusCode != http.StatusCreated {
		t.Fatalf("register %d", regResp.StatusCode)
	}

	loginBody := `{"username":"intuser","password":"password123"}`
	loginReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/login", bytes.NewBufferString(loginBody))
	loginResp, err := http.DefaultClient.Do(loginReq)
	if err != nil {
		t.Fatal(err)
	}
	defer loginResp.Body.Close()
	var login panel.LoginResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&login); err != nil {
		t.Fatal(err)
	}

	keyBody := `{"name":"integration"}`
	keyReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/keys", bytes.NewBufferString(keyBody))
	keyReq.Header.Set("Authorization", "Bearer "+login.Token)
	keyResp, err := http.DefaultClient.Do(keyReq)
	if err != nil {
		t.Fatal(err)
	}
	defer keyResp.Body.Close()
	var created panel.CreateKeyResponse
	if err := json.NewDecoder(keyResp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if keyResp.StatusCode != http.StatusCreated {
		t.Fatalf("create key status %d", keyResp.StatusCode)
	}
	keyID := created.Key.ID

	bad, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewBufferString(`{}`))
	badResp, err := http.DefaultClient.Do(bad)
	if err != nil {
		t.Fatal(err)
	}
	badResp.Body.Close()
	if badResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without key, got %d", badResp.StatusCode)
	}

	initPayload := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`
	initReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewBufferString(initPayload))
	setMCPHeaders(initReq, created.APIKey)
	initResp, err := http.DefaultClient.Do(initReq)
	if err != nil {
		t.Fatal(err)
	}
	initBody, _ := io.ReadAll(initResp.Body)
	initResp.Body.Close()
	if initResp.StatusCode != http.StatusOK {
		t.Fatalf("initialize status %d body=%s", initResp.StatusCode, truncate(string(initBody), 512))
	}

	toolPayload := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"grok_web_search","arguments":{"query":"integration test"}}}`
	toolReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewBufferString(toolPayload))
	setMCPHeaders(toolReq, created.APIKey)
	toolResp, err := http.DefaultClient.Do(toolReq)
	if err != nil {
		t.Fatal(err)
	}
	toolBody, _ := io.ReadAll(toolResp.Body)
	toolResp.Body.Close()
	if toolResp.StatusCode != http.StatusOK {
		t.Fatalf("tools/call status %d body=%s", toolResp.StatusCode, truncate(string(toolBody), 1024))
	}
	if !strings.Contains(string(toolBody), "mock integration answer") {
		t.Fatalf("tools/call response missing mock answer: %s", truncate(string(toolBody), 1024))
	}

	usageWriter.Close()

	since := time.Now().UTC().Add(-time.Hour)
	stats, err := st.GetUsageStats(context.Background(), keyID, since)
	if err != nil {
		t.Fatalf("GetUsageStats: %v", err)
	}
	if stats.TotalCalls != 1 {
		t.Fatalf("expected 1 usage_log row for tools/call, got total=%d stats=%+v", stats.TotalCalls, stats)
	}
	if stats.ByTool["grok_web_search"] != 1 {
		t.Fatalf("expected grok_web_search in usage stats, got %+v", stats.ByTool)
	}
	if stats.SuccessCalls != 1 {
		t.Fatalf("expected successful tools/call recorded, success=%d", stats.SuccessCalls)
	}

	k, err := st.GetKeyByID(context.Background(), keyID)
	if err != nil {
		t.Fatal(err)
	}
	if k.TotalCalls < 1 {
		t.Fatalf("expected key total_calls incremented, got %d", k.TotalCalls)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
