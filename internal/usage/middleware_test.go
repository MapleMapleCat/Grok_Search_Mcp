package usage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grok-mcp/internal/auth"
	"github.com/grok-mcp/internal/store"
)

type fakeStore struct {
	store.TestStore
	touched int
	lastID  string
}

func (f *fakeStore) TouchKeyUsage(_ context.Context, keyID string) error {
	f.touched++
	f.lastID = keyID
	return nil
}

func (f *fakeStore) ReleaseSuccessCall(context.Context, string) error { return nil }
func (f *fakeStore) ReleaseTotalCall(context.Context, string) error   { return nil }

func (f *fakeStore) TryIncrementUserSuccessCalls(context.Context, string, int) error {
	return nil
}

func TestMCPMiddlewareGatesUsageByToolCall(t *testing.T) {
	key := &store.APIKey{ID: "k1"}
	user := &store.User{ID: "u1", SuccessLimit: 0}
	st := &fakeStore{}
	h := MCPMiddleware(st, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","method":"initialize"}`))
	req = req.WithContext(auth.WithAPIKey(req.Context(), key))
	req = req.WithContext(auth.WithUser(req.Context(), user))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if st.touched != 0 {
		t.Fatalf("initialize must not touch usage, got touched=%d", st.touched)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"grok_web_search"}}`))
	req2 = req2.WithContext(auth.WithAPIKey(req2.Context(), key))
	req2 = req2.WithContext(auth.WithUser(req2.Context(), user))
	h.ServeHTTP(httptest.NewRecorder(), req2)
	if st.touched != 1 || st.lastID != "k1" {
		t.Fatalf("tools/call should touch usage once for k1, got touched=%d id=%q", st.touched, st.lastID)
	}
}

func TestExtractToolNameParsesAndRestoresBody(t *testing.T) {
	payload := `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"grok_x_search"}}`
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(payload))
	if name := extractToolName(r); name != "grok_x_search" {
		t.Fatalf("expected grok_x_search, got %q", name)
	}
	rest, _ := io.ReadAll(r.Body)
	if string(rest) != payload {
		t.Fatalf("body not restored for downstream: got %q", rest)
	}
}

func TestExtractToolNameIgnoresNonToolCall(t *testing.T) {
	for _, payload := range []string{
		`{"jsonrpc":"2.0","method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"tools/list"}`,
		`not json at all`,
	} {
		r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(payload))
		if name := extractToolName(r); name != "" {
			t.Fatalf("expected empty tool name for %q, got %q", payload, name)
		}
	}
}

func TestExtractToolNameOversizedBodyStillRestored(t *testing.T) {
	big := strings.Repeat("x", maxParseBody+10)
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(big))
	if name := extractToolName(r); name != "" {
		t.Fatalf("expected empty for oversized body, got %q", name)
	}
	rest, _ := io.ReadAll(r.Body)
	if len(rest) != len(big) {
		t.Fatalf("oversized body must be fully restored downstream: got %d want %d", len(rest), len(big))
	}
}

func TestMCPToolResultIsError(t *testing.T) {
	okBody := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`
	if mcpToolResultIsError([]byte(okBody)) {
		t.Fatal("expected success result")
	}
	errBody := `{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[{"type":"text","text":"fail"}]}}`
	if !mcpToolResultIsError([]byte(errBody)) {
		t.Fatal("expected tool error")
	}
	sse := "event: message\r\ndata: " + errBody + "\r\n\r\n"
	if !mcpToolResultIsError([]byte(sse)) {
		t.Fatal("expected tool error in SSE payload")
	}
}

func TestResponseRecorderFlushDelegates(t *testing.T) {
	var flushed bool
	inner := &flushRecorder{flushed: &flushed}
	rec := &responseRecorder{ResponseWriter: inner}
	rec.Flush()
	if !flushed {
		t.Fatal("expected Flush to delegate to underlying ResponseWriter")
	}
}

type flushRecorder struct {
	http.ResponseWriter
	flushed *bool
}

func (f *flushRecorder) Flush() {
	*f.flushed = true
}