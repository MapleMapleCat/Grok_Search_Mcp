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

type integrationEnv struct {
	ts      *httptest.Server
	st      *store.SQLiteStore
	writer  *store.AsyncUsageWriter
	userLim *ratelimit.UserLimiter
	created panel.CreateKeyResponse
}

func bootIntegrationEnv(t *testing.T, cpa *httptest.Server) *integrationEnv {
	t.Helper()
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "guards.db"))
	if err != nil {
		t.Fatal(err)
	}
	writer := store.NewAsyncUsageWriter(st, 64)
	cfg := &config.Config{
		CPABaseURL:              cpa.URL,
		CPAAPIKey:               "cpa-mock-key",
		Model:                   "grok-4.3",
		JWTSecret:               "jwt-secret-must-be-at-least-32-bytes!",
		DefaultUserRPM:          1000,
		DefaultUserTotalLimit:   0,
		DefaultUserSuccessLimit: 0,
		Timeout:                 30 * time.Second,
	}
	client := grok.NewClient(cfg)
	server := mcp.NewServer(&mcp.Implementation{Name: "grok-mcp", Version: version.Version}, nil)
	mcpserver.RegisterTools(server, client, false)
	userLim := ratelimit.NewUserLimiter(cfg.DefaultUserRPM)
	authResolver := auth.NewCachedAPIKeyResolver(st, 30*time.Second)
	mcpHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{Stateless: true})
	var mcpChain http.Handler = mcpHandler
	mcpChain = panel.MaxBodyMiddleware(panel.MaxPanelBodyBytes())(mcpChain)
	mcpChain = usage.MCPMiddleware(st, writer)(mcpChain)
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
	t.Cleanup(func() {
		ts.Close()
		userLim.Close()
		writer.Close()
		st.Close()
	})

	regReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/register", bytes.NewBufferString(`{"username":"guarduser","password":"password123"}`))
	regResp, err := http.DefaultClient.Do(regReq)
	if err != nil {
		t.Fatal(err)
	}
	regResp.Body.Close()

	loginReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/login", bytes.NewBufferString(`{"username":"guarduser","password":"password123"}`))
	loginResp, err := http.DefaultClient.Do(loginReq)
	if err != nil {
		t.Fatal(err)
	}
	defer loginResp.Body.Close()
	var login panel.LoginResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&login); err != nil {
		t.Fatal(err)
	}

	keyReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/keys", bytes.NewBufferString(`{"name":"guard-key"}`))
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
		t.Fatalf("create key %d", keyResp.StatusCode)
	}
	return &integrationEnv{ts: ts, st: st, writer: writer, userLim: userLim, created: created}
}

func TestHTTPPanelKeysRequireJWT(t *testing.T) {
	cpa := cpaMockSSE(t)
	defer cpa.Close()
	env := bootIntegrationEnv(t, cpa)

	req, _ := http.NewRequest(http.MethodGet, env.ts.URL+"/panel/v1/keys", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without JWT, got %d", resp.StatusCode)
	}
}

func TestHTTPMCPDisabledAPIKeyForbidden(t *testing.T) {
	cpa := cpaMockSSE(t)
	defer cpa.Close()
	env := bootIntegrationEnv(t, cpa)
	ctx := context.Background()
	dis := false
	if _, err := env.st.UpdateKey(ctx, env.created.Key.ID, store.KeyUpdates{Enabled: &dis}); err != nil {
		t.Fatal(err)
	}

	toolPayload := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"grok_web_search","arguments":{"query":"x"}}}`
	req, _ := http.NewRequest(http.MethodPost, env.ts.URL+"/mcp", bytes.NewBufferString(toolPayload))
	setMCPHeaders(req, env.created.APIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403 disabled key, got %d body=%s", resp.StatusCode, truncate(string(body), 256))
	}
}

func TestHTTPMCPDisabledUserForbidden(t *testing.T) {
	cpa := cpaMockSSE(t)
	defer cpa.Close()
	env := bootIntegrationEnv(t, cpa)
	ctx := context.Background()
	key, err := env.st.GetKeyByID(ctx, env.created.Key.ID)
	if err != nil {
		t.Fatal(err)
	}
	dis := false
	if _, err := env.st.UpdateUser(ctx, key.UserID, store.UserUpdates{Enabled: &dis}); err != nil {
		t.Fatal(err)
	}

	toolPayload := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"grok_web_search","arguments":{"query":"x"}}}`
	req, _ := http.NewRequest(http.MethodPost, env.ts.URL+"/mcp", bytes.NewBufferString(toolPayload))
	setMCPHeaders(req, env.created.APIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403 disabled user, got %d body=%s", resp.StatusCode, truncate(string(body), 256))
	}
}

func TestHTTPToolCallUpstreamFailureRecordsUnsuccessfulUsage(t *testing.T) {
	cpa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("bad"))
	}))
	defer cpa.Close()
	env := bootIntegrationEnv(t, cpa)
	keyID := env.created.Key.ID

	toolPayload := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"grok_web_search","arguments":{"query":"fail upstream"}}}`
	req, _ := http.NewRequest(http.MethodPost, env.ts.URL+"/mcp", bytes.NewBufferString(toolPayload))
	setMCPHeaders(req, env.created.APIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tools/call HTTP %d body=%s", resp.StatusCode, truncate(string(body), 512))
	}
	if !strings.Contains(string(body), `"isError":true`) {
		t.Fatalf("expected MCP isError tool result, got %s", truncate(string(body), 512))
	}

	env.writer.Close()
	since := time.Now().UTC().Add(-time.Hour)
	stats, err := env.st.GetUsageStats(context.Background(), keyID, since)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalCalls != 1 {
		t.Fatalf("expected usage row, got %+v", stats)
	}
	if stats.SuccessCalls != 0 {
		t.Fatalf("expected unsuccessful usage, success=%d", stats.SuccessCalls)
	}
}