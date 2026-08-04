package usage

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/auth"
	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/store"
	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/testsupport"
)

// fakeStore 的 recordedUsage 字段会被 AsyncUsageWriter 后台 goroutine 写入、
// 测试主 goroutine 读取，因此用 mutex 保护以避免数据竞争。
type fakeStore struct {
	testsupport.Store
	mu            sync.Mutex
	recordedUsage []store.UsageRecord
}

func (f *fakeStore) RecordUsage(_ context.Context, record store.UsageRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordedUsage = append(f.recordedUsage, record)
	return nil
}

func (f *fakeStore) CompleteSuccessCall(context.Context, store.SuccessQuotaReservation, bool) error {
	return nil
}

func testSuccessQuotaReservation(userID string) store.SuccessQuotaReservation {
	return store.SuccessQuotaReservation{ID: "reservation-" + userID, UserID: userID, Period: "2026-01"}
}

func (f *fakeStore) RecordedUsage() []store.UsageRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.UsageRecord(nil), f.recordedUsage...)
}

func TestMCPMiddlewareGatesUsageByToolCall(t *testing.T) {
	key := &store.APIKey{ID: "k1"}
	user := &auth.AuthenticatedUser{User: store.User{ID: "u1"}, SuccessLimit: 0}
	st := &fakeStore{}
	writer := store.NewAsyncUsageWriter(st, 8)
	defer writer.Close()
	h := MCPMiddleware(st, writer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","method":"initialize"}`))
	req = req.WithContext(auth.WithAPIKey(req.Context(), key))
	req = req.WithContext(auth.WithUser(req.Context(), user))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if recordedUsage := st.RecordedUsage(); len(recordedUsage) != 0 {
		t.Fatalf("initialize must not record usage, got records=%d", len(recordedUsage))
	}

	req2 := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"grok_web_search"}}`))
	req2 = req2.WithContext(auth.WithAPIKey(req2.Context(), key))
	req2 = req2.WithContext(auth.WithUser(req2.Context(), user))
	h.ServeHTTP(httptest.NewRecorder(), req2)
	deadline := time.Now().Add(2 * time.Second)
	for len(st.RecordedUsage()) < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	recordedUsage := st.RecordedUsage()
	if len(recordedUsage) != 1 {
		t.Fatalf("tools/call should enqueue one usage record, got %d", len(recordedUsage))
	}
	if recordedUsage[0].KeyID != "k1" || recordedUsage[0].ToolName != "grok_web_search" {
		t.Fatalf("unexpected usage record: key=%q tool=%q", recordedUsage[0].KeyID, recordedUsage[0].ToolName)
	}
}

func TestInspectJSONRPCRequestParsesToolNameAndRestoresBody(t *testing.T) {
	payload := `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"grok_x_search"}}`
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(payload))
	inspection := inspectJSONRPCRequest(r)
	if inspection.ToolName != "grok_x_search" {
		t.Fatalf("expected grok_x_search, got %q", inspection.ToolName)
	}
	rest, _ := io.ReadAll(r.Body)
	if string(rest) != payload {
		t.Fatalf("body not restored for downstream: got %q", rest)
	}
}

func TestExtractToolNameMiddlewareRejectsJSONRPCBatch(t *testing.T) {
	payload := `[{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"grok_web_search"}}]`
	called := false
	h := ExtractToolNameMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(payload))
	h.ServeHTTP(rec, req)

	if called {
		t.Fatal("JSON-RPC batch request must not reach downstream handler")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for JSON-RPC batch, got %d", rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("expected application/json content type, got %q", contentType)
	}
	if body := rec.Body.String(); !strings.Contains(body, "JSON-RPC batch requests are not supported") {
		t.Fatalf("expected batch rejection message, got %q", body)
	}
}

func TestExtractToolNameMiddlewareRejectsAmbiguousToolRoutingFields(t *testing.T) {
	testCases := []struct {
		name    string
		payload string
	}{
		{
			name:    "method alias after canonical field",
			payload: `{"jsonrpc":"2.0","id":1,"method":"tools/call","METHOD":"initialize","params":{"name":"grok_web_search"}}`,
		},
		{
			name:    "params alias after canonical field",
			payload: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"grok_web_search"},"PARAMS":{"name":""}}`,
		},
		{
			name:    "name alias after canonical field",
			payload: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"grok_web_search","NAME":""}}`,
		},
		{
			name:    "name alias before canonical field",
			payload: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"NAME":"","name":"grok_web_search"}}`,
		},
		{
			name:    "duplicate method field",
			payload: `{"jsonrpc":"2.0","id":1,"method":"tools/call","method":"initialize","params":{"name":"grok_web_search"}}`,
		},
		{
			name:    "duplicate params field with partial object",
			payload: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"grok_web_search"},"params":{"arguments":{"query":"ambiguous"}}}`,
		},
		{
			name:    "duplicate name field",
			payload: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"grok_web_search","name":""}}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			downstreamCalled := false
			handler := ExtractToolNameMiddleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				downstreamCalled = true
			}))

			responseRecorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(testCase.payload))
			handler.ServeHTTP(responseRecorder, request)

			if downstreamCalled {
				t.Fatal("ambiguous tool routing fields must not reach the downstream MCP SDK")
			}
			if responseRecorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", responseRecorder.Code, http.StatusBadRequest)
			}
			if contentType := responseRecorder.Header().Get("Content-Type"); contentType != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", contentType)
			}
			if responseBody := responseRecorder.Body.String(); !strings.Contains(responseBody, "Ambiguous JSON-RPC tool routing fields") {
				t.Fatalf("unexpected rejection body: %q", responseBody)
			}
		})
	}
}

func TestInspectJSONRPCRequestIgnoresNonToolCall(t *testing.T) {
	for _, payload := range []string{
		`{"jsonrpc":"2.0","method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"tools/list"}`,
		`not json at all`,
	} {
		r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(payload))
		if inspection := inspectJSONRPCRequest(r); inspection.ToolName != "" {
			t.Fatalf("expected empty tool name for %q, got %q", payload, inspection.ToolName)
		}
	}
}

func TestInspectJSONRPCRequestRestoresOversizedBody(t *testing.T) {
	big := strings.Repeat("x", maxParseBody+10)
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(big))
	if inspection := inspectJSONRPCRequest(r); inspection.ToolName != "" {
		t.Fatalf("expected empty for oversized body, got %q", inspection.ToolName)
	}
	rest, _ := io.ReadAll(r.Body)
	if len(rest) != len(big) {
		t.Fatalf("oversized body must be fully restored downstream: got %d want %d", len(rest), len(big))
	}
}

func TestResponseOutcomeInspectorDetectsToolErrors(t *testing.T) {
	testCases := []struct {
		name         string
		responseBody string
		expectsError bool
	}{
		{
			name:         "successful result",
			responseBody: `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}`,
		},
		{
			name:         "successful result containing error text",
			responseBody: `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"the word error is not a JSON-RPC error"}]}}`,
		},
		{
			name:         "tool result isError",
			responseBody: `{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[{"type":"text","text":"fail"}]}}`,
			expectsError: true,
		},
		{
			name:         "unknown tool JSON-RPC error",
			responseBody: `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"unknown tool"}}`,
			expectsError: true,
		},
		{
			name:         "invalid parameters JSON-RPC error",
			responseBody: `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"Invalid params"}}`,
			expectsError: true,
		},
		{
			name:         "null JSON-RPC error",
			responseBody: `{"jsonrpc":"2.0","id":1,"error":null,"result":{"content":[]}}`,
		},
		{
			name:         "JSON-RPC error in SSE payload",
			responseBody: "event: message\r\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"error\":{\"code\":-32602,\"message\":\"Invalid params\"}}\r\n\r\n",
			expectsError: true,
		},
		{
			name:         "JSON-RPC error in batch payload",
			responseBody: `[{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}},{"jsonrpc":"2.0","id":2,"error":{"code":-32602,"message":"unknown tool"}}]`,
			expectsError: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			inspector := &responseOutcomeInspector{}
			inspector.inspect([]byte(testCase.responseBody))
			actualError := inspector.toolError()
			if actualError != testCase.expectsError {
				t.Fatalf("toolError() = %t, want %t", actualError, testCase.expectsError)
			}
		})
	}
}

func TestResponseOutcomeInspectorHandlesFragmentedSSEAndLatchesError(t *testing.T) {
	inspector := &responseOutcomeInspector{}
	fragments := []string{
		"event: message\r\nda",
		"ta: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"isErr",
		"or\":true}}\r\n\r\n",
		"data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[]}}\n\n",
	}
	for _, fragment := range fragments {
		inspector.inspect([]byte(fragment))
	}
	if !inspector.toolError() {
		t.Fatal("fragmented SSE error must remain latched after a later success event")
	}
}

func TestResponseOutcomeInspectorEnforcesIndependentCap(t *testing.T) {
	inspector := &responseOutcomeInspector{}
	inspector.inspect(bytes.Repeat([]byte("x"), maxOutcomeInspectionBytes+1024))
	inspector.inspect([]byte("\ndata: {\"jsonrpc\":\"2.0\",\"error\":{\"code\":-32603}}\n\n"))

	if inspector.inspectedBytes != maxOutcomeInspectionBytes {
		t.Fatalf("inspected bytes = %d, want cap %d", inspector.inspectedBytes, maxOutcomeInspectionBytes)
	}
	if len(inspector.jsonCapture) > maxOutcomeInspectionBytes || len(inspector.sseLineBuffer) > maxOutcomeInspectionBytes {
		t.Fatalf("inspection buffers exceeded cap: json=%d line=%d", len(inspector.jsonCapture), len(inspector.sseLineBuffer))
	}
	if inspector.toolError() {
		t.Fatal("error arriving after the inspection cap must not be parsed")
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

type quotaCompletion struct {
	reservation store.SuccessQuotaReservation
	succeeded   bool
}

// completionCountingStore 记录 CompleteSuccessCall 调用，用于断言完成结果。
type completionCountingStore struct {
	testsupport.Store
	completions []quotaCompletion
}

func (completionStore *completionCountingStore) CompleteSuccessCall(_ context.Context, reservation store.SuccessQuotaReservation, succeeded bool) error {
	completionStore.completions = append(completionStore.completions, quotaCompletion{reservation: reservation, succeeded: succeeded})
	return nil
}

type completionContextRecordingStore struct {
	testsupport.Store
	completions          []quotaCompletion
	completionContextErr error
}

type panickingQuotaCompleter struct {
	testsupport.Store
	completionAttempts int
}

func (completer *panickingQuotaCompleter) CompleteSuccessCall(context.Context, store.SuccessQuotaReservation, bool) error {
	completer.completionAttempts++
	panic("quota completion failed unexpectedly")
}

func (completionStore *completionContextRecordingStore) CompleteSuccessCall(ctx context.Context, reservation store.SuccessQuotaReservation, succeeded bool) error {
	completionStore.completions = append(completionStore.completions, quotaCompletion{reservation: reservation, succeeded: succeeded})
	completionStore.completionContextErr = ctx.Err()
	return nil
}

type failureRecordingStore struct {
	testsupport.Store
	completions   []quotaCompletion
	recordedUsage []store.UsageRecord
}

type debugCaptureRecordingStore struct {
	testsupport.Store
	mu                  sync.Mutex
	recordedUsage       []store.UsageRecord
	requestBody         []byte
	responseBody        []byte
	requestPermissions  os.FileMode
	responsePermissions os.FileMode
}

func (s *debugCaptureRecordingStore) Enabled() bool {
	return true
}

func (s *debugCaptureRecordingStore) RecordUsage(_ context.Context, record store.UsageRecord) error {
	requestBody, err := os.ReadFile(record.DebugRequestBodyPath)
	if err != nil {
		return err
	}
	responseBody, err := os.ReadFile(record.DebugResponseBodyPath)
	if err != nil {
		return err
	}
	requestInfo, err := os.Stat(record.DebugRequestBodyPath)
	if err != nil {
		return err
	}
	responseInfo, err := os.Stat(record.DebugResponseBodyPath)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordedUsage = append(s.recordedUsage, record)
	s.requestBody = requestBody
	s.responseBody = responseBody
	s.requestPermissions = requestInfo.Mode().Perm()
	s.responsePermissions = responseInfo.Mode().Perm()
	return nil
}

func (s *debugCaptureRecordingStore) snapshot() (store.UsageRecord, []byte, []byte, os.FileMode, os.FileMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recordedUsage[0], s.requestBody, s.responseBody, s.requestPermissions, s.responsePermissions
}

func TestMCPMiddlewareBoundsDebugBodiesWithoutChangingForwardedContent(t *testing.T) {
	requestBody := strings.Repeat("request-body-segment|", 120000)
	responseBody := strings.Repeat("response-body-segment|", 550000)
	debugStore := &debugCaptureRecordingStore{}
	writer := store.NewAsyncUsageWriter(debugStore, 4)

	handler := MCPMiddleware(debugStore, writer, debugStore)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwardedRequestBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		if !bytes.Equal(forwardedRequestBody, []byte(requestBody)) {
			t.Errorf("forwarded request length = %d, want %d", len(forwardedRequestBody), len(requestBody))
		}
		MarkToolOutcome(r.Context(), true)
		_, _ = io.WriteString(w, responseBody)
	}))

	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(requestBody))
	requestContext := auth.WithAPIKey(request.Context(), &store.APIKey{ID: "debug-key", KeyPrefix: "grok_dbg"})
	requestContext = WithToolName(requestContext, "grok_web_search")
	request = request.WithContext(requestContext)
	responseRecorder := httptest.NewRecorder()
	handler.ServeHTTP(responseRecorder, request)
	writer.Close()

	if len(debugStore.recordedUsage) != 1 {
		t.Fatalf("recorded usage count = %d, want 1", len(debugStore.recordedUsage))
	}
	record, capturedRequest, capturedResponse, requestPermissions, responsePermissions := debugStore.snapshot()
	if record.DebugRequestBody != "" || record.DebugResponseBody != "" {
		t.Fatalf("queued usage record retained body strings: request=%d response=%d", len(record.DebugRequestBody), len(record.DebugResponseBody))
	}
	if len(record.DebugJSON) > 16<<10 {
		t.Fatalf("debug metadata length = %d, want compact metadata", len(record.DebugJSON))
	}
	if strings.Contains(record.DebugJSON, "request-body-segment") || strings.Contains(record.DebugJSON, "response-body-segment") {
		t.Fatal("debug metadata must not embed request or response bodies")
	}
	if !bytes.Equal(capturedRequest, []byte(requestBody[:maxDebugCapturedBodyBytes])) {
		t.Fatalf("captured request length = %d, want bounded prefix %d", len(capturedRequest), maxDebugCapturedBodyBytes)
	}
	if !bytes.Equal(capturedResponse, []byte(responseBody[:maxDebugCapturedBodyBytes])) {
		t.Fatalf("captured response length = %d, want bounded prefix %d", len(capturedResponse), maxDebugCapturedBodyBytes)
	}
	if record.DebugRequestObservedBytes != int64(len(requestBody)) || record.DebugResponseObservedBytes != int64(len(responseBody)) {
		t.Fatalf("observed bytes request=%d response=%d, want %d and %d", record.DebugRequestObservedBytes, record.DebugResponseObservedBytes, len(requestBody), len(responseBody))
	}
	if !record.DebugRequestTruncated || !record.DebugResponseTruncated {
		t.Fatalf("truncation flags request=%v response=%v, want true", record.DebugRequestTruncated, record.DebugResponseTruncated)
	}
	if responseRecorder.Body.String() != responseBody {
		t.Fatalf("forwarded response length = %d, want %d", responseRecorder.Body.Len(), len(responseBody))
	}
	if requestPermissions != 0o600 || responsePermissions != 0o600 {
		t.Fatalf("spool permissions request=%#o response=%#o, want 0600", requestPermissions, responsePermissions)
	}
	if _, err := os.Stat(record.DebugRequestBodyPath); !os.IsNotExist(err) {
		t.Fatalf("request spool was not removed after persistence: %v", err)
	}
	if _, err := os.Stat(record.DebugResponseBodyPath); !os.IsNotExist(err) {
		t.Fatalf("response spool was not removed after persistence: %v", err)
	}
}

func TestMCPMiddlewarePrefersAuthoritativeSemanticOutcome(t *testing.T) {
	testCases := []struct {
		name                   string
		semanticSuccess        bool
		responseBody           string
		expectedSuccess        bool
		expectedQuotaSucceeded bool
	}{
		{
			name:                   "handler success overrides fallback error payload",
			semanticSuccess:        true,
			responseBody:           `{"jsonrpc":"2.0","id":1,"result":{"isError":true}}`,
			expectedSuccess:        true,
			expectedQuotaSucceeded: true,
		},
		{
			name:                   "handler error overrides fallback success payload",
			semanticSuccess:        false,
			responseBody:           `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`,
			expectedSuccess:        false,
			expectedQuotaSucceeded: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			expectedReservation := testSuccessQuotaReservation("u1")
			usageStore := &failureRecordingStore{}
			writer := store.NewAsyncUsageWriter(usageStore, 4)
			handler := MCPMiddleware(usageStore, writer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				MarkToolOutcome(r.Context(), testCase.semanticSuccess)
				_, _ = io.WriteString(w, testCase.responseBody)
			}))

			request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
			requestContext := auth.WithAPIKey(request.Context(), &store.APIKey{ID: "k1"})
			requestContext = auth.WithUser(requestContext, &auth.AuthenticatedUser{User: store.User{ID: "u1"}})
			requestContext = WithToolName(requestContext, "grok_web_search")
			requestContext = WithSuccessQuotaReservation(requestContext, expectedReservation)
			request = request.WithContext(requestContext)
			handler.ServeHTTP(httptest.NewRecorder(), request)
			writer.Close()

			if len(usageStore.recordedUsage) != 1 {
				t.Fatalf("recorded usage count = %d, want 1", len(usageStore.recordedUsage))
			}
			if usageStore.recordedUsage[0].Success != testCase.expectedSuccess {
				t.Fatalf("recorded success = %t, want %t", usageStore.recordedUsage[0].Success, testCase.expectedSuccess)
			}
			if len(usageStore.completions) != 1 {
				t.Fatalf("quota completions = %+v, want exactly one", usageStore.completions)
			}
			completion := usageStore.completions[0]
			if completion.reservation != expectedReservation || completion.succeeded != testCase.expectedQuotaSucceeded {
				t.Fatalf("quota completion = %+v, want reservation=%+v succeeded=%t", completion, expectedReservation, testCase.expectedQuotaSucceeded)
			}
		})
	}
}

func TestMCPMiddlewareCleansDebugSpoolsOnPanic(t *testing.T) {
	temporaryDirectory := t.TempDir()
	t.Setenv("TMPDIR", temporaryDirectory)
	debugStore := &debugCaptureRecordingStore{}
	handler := MCPMiddleware(debugStore, nil, debugStore)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("handler failed")
	}))

	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("panic request body"))
	requestContext := auth.WithAPIKey(request.Context(), &store.APIKey{ID: "panic-key"})
	requestContext = WithToolName(requestContext, "grok_web_search")
	request = request.WithContext(requestContext)

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected downstream panic")
			}
		}()
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}()

	entries, err := os.ReadDir(temporaryDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("debug spool files remained after panic: %+v", entries)
	}
}

func (f *failureRecordingStore) CompleteSuccessCall(_ context.Context, reservation store.SuccessQuotaReservation, succeeded bool) error {
	f.completions = append(f.completions, quotaCompletion{reservation: reservation, succeeded: succeeded})
	return nil
}

func (f *failureRecordingStore) RecordUsage(_ context.Context, record store.UsageRecord) error {
	f.recordedUsage = append(f.recordedUsage, record)
	return nil
}

func TestMCPMiddlewareCompletesAndRecordsFailureOnToolErrorAndHTTPError(t *testing.T) {
	testCases := []struct {
		name    string
		handler http.Handler
	}{
		{
			name: "mcp tool isError",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[{"type":"text","text":"failed"}]}}`))
			}),
		},
		{
			name: "JSON-RPC top-level error",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"Invalid params"}}`))
			}),
		},
		{
			name: "http failure status",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "upstream unavailable", http.StatusBadGateway)
			}),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			key := &store.APIKey{ID: "k1"}
			user := &auth.AuthenticatedUser{User: store.User{ID: "u1"}}
			expectedReservation := testSuccessQuotaReservation(user.ID)
			st := &failureRecordingStore{}
			writer := store.NewAsyncUsageWriter(st, 8)
			h := MCPMiddleware(st, writer)(testCase.handler)

			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
			ctx := auth.WithAPIKey(req.Context(), key)
			ctx = auth.WithUser(ctx, user)
			ctx = WithToolName(ctx, "grok_web_search")
			ctx = WithSuccessQuotaReservation(ctx, expectedReservation)
			req = req.WithContext(ctx)

			h.ServeHTTP(httptest.NewRecorder(), req)
			writer.Close()

			if len(st.completions) != 1 || st.completions[0].reservation != expectedReservation || st.completions[0].succeeded {
				t.Fatalf("quota completions = %+v, want one failed completion for %+v", st.completions, expectedReservation)
			}
			if len(st.recordedUsage) != 1 {
				t.Fatalf("expected one usage record, got %+v", st.recordedUsage)
			}
			if st.recordedUsage[0].Success {
				t.Fatalf("expected unsuccessful usage record, got %+v", st.recordedUsage[0])
			}
			if st.recordedUsage[0].ToolName != "grok_web_search" {
				t.Fatalf("unexpected tool name in usage record: %+v", st.recordedUsage[0])
			}
		})
	}
}

func TestMCPMiddlewareSkipsCompletionWithoutUsableReservation(t *testing.T) {
	testCases := []struct {
		name        string
		reservation store.SuccessQuotaReservation
	}{
		{name: "missing reservation"},
		{name: "missing reservation ID", reservation: store.SuccessQuotaReservation{UserID: "u1", Period: "2026-01"}},
		{name: "missing period", reservation: store.SuccessQuotaReservation{ID: "reservation-1", UserID: "u1"}},
		{name: "missing user", reservation: store.SuccessQuotaReservation{ID: "reservation-1", Period: "2026-01"}},
		{name: "malformed period", reservation: store.SuccessQuotaReservation{ID: "reservation-1", UserID: "u1", Period: "January"}},
		{name: "different user", reservation: store.SuccessQuotaReservation{ID: "reservation-1", UserID: "other-user", Period: "2026-01"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			usageStore := &failureRecordingStore{}
			handler := MCPMiddleware(usageStore, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "upstream unavailable", http.StatusBadGateway)
			}))

			request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
			requestContext := auth.WithAPIKey(request.Context(), &store.APIKey{ID: "k1"})
			requestContext = auth.WithUser(requestContext, &auth.AuthenticatedUser{User: store.User{ID: "unrelated-user"}})
			requestContext = WithToolName(requestContext, "grok_web_search")
			if testCase.reservation != (store.SuccessQuotaReservation{}) {
				requestContext = WithSuccessQuotaReservation(requestContext, testCase.reservation)
			}
			request = request.WithContext(requestContext)

			handler.ServeHTTP(httptest.NewRecorder(), request)

			if len(usageStore.completions) != 0 {
				t.Fatalf("missing or invalid reservation completed quota: %+v", usageStore.completions)
			}
		})
	}
}

func TestMCPMiddlewareCompletesWithLiveContextAfterRequestCancel(t *testing.T) {
	key := &store.APIKey{ID: "k1"}
	user := &auth.AuthenticatedUser{User: store.User{ID: "u1"}}
	st := &completionContextRecordingStore{}
	h := MCPMiddleware(st, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}))

	baseContext, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`)).WithContext(baseContext)
	ctx := auth.WithAPIKey(req.Context(), key)
	ctx = auth.WithUser(ctx, user)
	ctx = WithToolName(ctx, "grok_web_search")
	expectedReservation := testSuccessQuotaReservation(user.ID)
	ctx = WithSuccessQuotaReservation(ctx, expectedReservation)
	req = req.WithContext(ctx)

	h.ServeHTTP(httptest.NewRecorder(), req)

	if len(st.completions) != 1 || st.completions[0].reservation != expectedReservation || st.completions[0].succeeded {
		t.Fatalf("quota completions = %+v, want one failed completion for %+v", st.completions, expectedReservation)
	}
	if st.completionContextErr != nil {
		t.Fatalf("quota completion must detach from canceled request context, got context err %v", st.completionContextErr)
	}
}

func TestHeaderSnapshotRedactsSensitiveHeadersCaseInsensitively(t *testing.T) {
	snapshot := headerSnapshot(http.Header{
		"Authorization":       {"Bearer private-token"},
		"x-api-key":           {"private-api-key"},
		"COOKIE":              {"private-cookie"},
		"X-Custom-Diagnostic": {"safe-value"},
	})

	for _, headerName := range []string{"Authorization", "x-api-key", "COOKIE"} {
		if got := snapshot[headerName][0]; got != "[redacted]" {
			t.Fatalf("header %s snapshot = %q, want redacted", headerName, got)
		}
	}
	if got := snapshot["X-Custom-Diagnostic"][0]; got != "safe-value" {
		t.Fatalf("ordinary header snapshot = %q, want original value", got)
	}
}

// TestMCPMiddlewareCompletesFailureOnPanic 验证 handler panic 时，usage 中间件
// 通过 defer/recover 按失败完成预留，然后重新 panic 让上层处理。
func TestMCPMiddlewareCompletesFailureOnPanic(t *testing.T) {
	key := &store.APIKey{ID: "k1"}
	user := &auth.AuthenticatedUser{User: store.User{ID: "u1"}}
	st := &completionCountingStore{}
	h := MCPMiddleware(st, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"grok_web_search"}}`))
	req = req.WithContext(auth.WithAPIKey(req.Context(), key))
	req = req.WithContext(auth.WithUser(req.Context(), user))
	expectedReservation := testSuccessQuotaReservation(user.ID)
	req = req.WithContext(WithSuccessQuotaReservation(req.Context(), expectedReservation))

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic to propagate")
		}
		if len(st.completions) != 1 || st.completions[0].reservation != expectedReservation || st.completions[0].succeeded {
			t.Fatalf("quota completions = %+v, want one failed completion for %+v", st.completions, expectedReservation)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), req)
}

func TestMCPMiddlewareDoesNotRetryPanickingQuotaCompletion(t *testing.T) {
	user := &auth.AuthenticatedUser{User: store.User{ID: "u1"}}
	quotaCompleter := &panickingQuotaCompleter{}
	handler := MCPMiddleware(quotaCompleter, nil)(http.HandlerFunc(func(responseWriter http.ResponseWriter, _ *http.Request) {
		http.Error(responseWriter, "upstream unavailable", http.StatusBadGateway)
	}))

	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	requestContext := auth.WithAPIKey(request.Context(), &store.APIKey{ID: "k1"})
	requestContext = auth.WithUser(requestContext, user)
	requestContext = WithToolName(requestContext, "grok_web_search")
	requestContext = WithSuccessQuotaReservation(requestContext, testSuccessQuotaReservation(user.ID))
	request = request.WithContext(requestContext)

	defer func() {
		if recover() == nil {
			t.Fatal("expected quota completion panic to propagate")
		}
		if quotaCompleter.completionAttempts != 1 {
			t.Fatalf("quota completion attempts = %d, want 1", quotaCompleter.completionAttempts)
		}
	}()
	handler.ServeHTTP(httptest.NewRecorder(), request)
}

// TestExtractToolNameMiddlewareWritesContext 验证提取中间件把工具名写入 context，
// 后续中间件无需重复解析请求体。
func TestExtractToolNameMiddlewareWritesContext(t *testing.T) {
	var gotName string
	var hasName bool
	h := ExtractToolNameMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotName, hasName = ToolNameFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"grok_x_search"}}`))
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !hasName {
		t.Fatal("expected tool name in context")
	}
	if gotName != "grok_x_search" {
		t.Fatalf("want grok_x_search, got %q", gotName)
	}
}

// TestMCPMiddlewareUsesContextToolName 验证当 context 已有工具名时不再重复解析 body：
// 提供一个一读就出错的 body，若中间件读取它会触发错误并跳过用量记录。
type errReader struct{}

func (errReader) Read(p []byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestMCPMiddlewareUsesContextToolName(t *testing.T) {
	key := &store.APIKey{ID: "k1"}
	user := &auth.AuthenticatedUser{User: store.User{ID: "u1"}}
	st := &fakeStore{}
	writer := store.NewAsyncUsageWriter(st, 8)
	defer writer.Close()
	h := MCPMiddleware(st, writer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", errReader{})
	req.Body = io.NopCloser(errReader{})
	ctx := auth.WithAPIKey(req.Context(), key)
	ctx = auth.WithUser(ctx, user)
	ctx = WithToolName(ctx, "grok_web_search")
	req = req.WithContext(ctx)

	h.ServeHTTP(httptest.NewRecorder(), req)
	deadline := time.Now().Add(2 * time.Second)
	for len(st.RecordedUsage()) < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if recordedUsage := st.RecordedUsage(); len(recordedUsage) != 1 {
		t.Fatalf("expected one usage record via context tool name, got %d", len(recordedUsage))
	}
}
