// Package quota 的中间件测试覆盖成功请求额度预留的关键路径：
//   - success 预留成功后放行 handler
//   - success 超限直接拒绝
//   - success 存储错误返回 500
//   - 非 tools/call 请求不触发预留
//   - 未鉴权用户不触发预留
package quota

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/auth"
	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/store"
	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/usage"
)

// recordingStore 记录 Reserve/Release 调用顺序与次数，用于断言回滚逻辑。
type recordingStore struct {
	store.TestStore

	reserveSuccessCalls int
	lastUserID          string
	lastSuccessLimit    int
	reserveReservation  store.SuccessQuotaReservation

	// 控制返回的错误
	reserveSuccessErr error
}

func (r *recordingStore) ReserveSuccessCall(_ context.Context, userID string, successLimit int) (store.SuccessQuotaReservation, error) {
	r.reserveSuccessCalls++
	r.lastUserID = userID
	r.lastSuccessLimit = successLimit
	if r.reserveSuccessErr != nil {
		return store.SuccessQuotaReservation{}, r.reserveSuccessErr
	}
	if r.reserveReservation.UserID == "" {
		r.reserveReservation = store.SuccessQuotaReservation{UserID: userID, Period: "2026-01"}
	}
	return r.reserveReservation, nil
}

// newToolCallRequest 构造一个 tools/call 请求并预先把工具名写入 context，
// 模拟链路中 ExtractToolNameMiddleware 已运行的情况。
func newToolCallRequest(name string) *http.Request {
	body := `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"` + name + `"}}`
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	r = r.WithContext(usage.WithToolName(r.Context(), name))
	return r
}

// newNoToolCallRequest 构造 initialize 请求；ExtractToolNameMiddleware 会写入空名。
func newNoToolCallRequest() *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","method":"initialize"}`))
	r = r.WithContext(usage.WithToolName(r.Context(), ""))
	return r
}

func TestNonToolsCallSkipsReserve(t *testing.T) {
	st := &recordingStore{}
	called := false
	h := MCPMiddleware(st)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	h.ServeHTTP(httptest.NewRecorder(), newNoToolCallRequest())

	if !called {
		t.Fatal("non tools/call should pass through to handler")
	}
	if st.reserveSuccessCalls != 0 {
		t.Fatalf("non tools/call must not reserve success calls, got %d", st.reserveSuccessCalls)
	}
}

func TestNoUserSkipsReserve(t *testing.T) {
	st := &recordingStore{}
	h := MCPMiddleware(st)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := newToolCallRequest("grok_web_search")
	// 故意不带 user
	h.ServeHTTP(httptest.NewRecorder(), req)

	if st.reserveSuccessCalls != 0 {
		t.Fatalf("unauthenticated request must not reserve success calls, got %d", st.reserveSuccessCalls)
	}
}

func TestReserveSuccessAndForward(t *testing.T) {
	expectedReservation := store.SuccessQuotaReservation{UserID: "u1", Period: "2026-01"}
	st := &recordingStore{reserveReservation: expectedReservation}
	called := false
	var forwardedReservation store.SuccessQuotaReservation
	var hasForwardedReservation bool
	h := MCPMiddleware(st)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		forwardedReservation, hasForwardedReservation = usage.SuccessQuotaReservationFromContext(r.Context())
	}))

	req := newToolCallRequest("grok_web_search")
	req = req.WithContext(auth.WithUser(req.Context(), &auth.AuthenticatedUser{User: store.User{ID: "u1"}}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("handler should be called when quota reserve succeeds")
	}
	if st.reserveSuccessCalls != 1 {
		t.Fatalf("want 1 reserveSuccessCall, got %d", st.reserveSuccessCalls)
	}
	if !hasForwardedReservation || forwardedReservation != expectedReservation {
		t.Fatalf("forwarded reservation = %+v present=%t, want %+v", forwardedReservation, hasForwardedReservation, expectedReservation)
	}
}

func TestInvalidOrForeignReservationFailsClosed(t *testing.T) {
	testCases := []struct {
		name        string
		reservation store.SuccessQuotaReservation
	}{
		{
			name:        "malformed period",
			reservation: store.SuccessQuotaReservation{UserID: "u1", Period: "January"},
		},
		{
			name:        "different user",
			reservation: store.SuccessQuotaReservation{UserID: "other-user", Period: "2026-01"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			quotaStore := &recordingStore{reserveReservation: testCase.reservation}
			downstreamCalled := false
			handler := MCPMiddleware(quotaStore)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				downstreamCalled = true
			}))

			request := newToolCallRequest("grok_web_search")
			request = request.WithContext(auth.WithUser(request.Context(), &auth.AuthenticatedUser{User: store.User{ID: "u1"}}))
			responseRecorder := httptest.NewRecorder()
			handler.ServeHTTP(responseRecorder, request)

			if downstreamCalled {
				t.Fatal("invalid reservation must not reach downstream handler")
			}
			if responseRecorder.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", responseRecorder.Code, http.StatusInternalServerError)
			}
			if quotaStore.reserveSuccessCalls != 1 {
				t.Fatalf("reserve calls = %d, want 1", quotaStore.reserveSuccessCalls)
			}
		})
	}
}

func TestReserveUsesAuthenticatedUserLimit(t *testing.T) {
	st := &recordingStore{}
	h := MCPMiddleware(st)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	user := &auth.AuthenticatedUser{User: store.User{ID: "paid-user"}, SuccessLimit: 123}
	req := newToolCallRequest("grok_x_search")
	req = req.WithContext(auth.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if st.lastUserID != user.ID || st.lastSuccessLimit != user.SuccessLimit {
		t.Fatalf("reserve got user=%q limit=%d, want user=%q limit=%d", st.lastUserID, st.lastSuccessLimit, user.ID, user.SuccessLimit)
	}
}

// TestSuccessLimitExceeded 验证 success_calls 达到上限时返回 429 且不调用 handler。
func TestSuccessLimitExceeded(t *testing.T) {
	st := &recordingStore{reserveSuccessErr: store.ErrQuotaSuccess}
	called := false
	h := MCPMiddleware(st)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := newToolCallRequest("grok_web_search")
	req = req.WithContext(auth.WithUser(req.Context(), &auth.AuthenticatedUser{User: store.User{ID: "u1"}}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if called {
		t.Fatal("handler must not be called when success quota exceeded")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", rec.Code)
	}
	if st.reserveSuccessCalls != 1 {
		t.Fatalf("want 1 reserveSuccessCall, got %d", st.reserveSuccessCalls)
	}
}

// TestReserveSuccessInternalError 验证 success reserve 非额度错误返回 500。
func TestReserveSuccessInternalError(t *testing.T) {
	st := &recordingStore{reserveSuccessErr: errors.New("db down")}
	h := MCPMiddleware(st)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := newToolCallRequest("grok_web_search")
	req = req.WithContext(auth.WithUser(req.Context(), &auth.AuthenticatedUser{User: store.User{ID: "u1"}}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 on store error, got %d", rec.Code)
	}
}

func TestReserveUserNotFoundReturnsForbidden(t *testing.T) {
	st := &recordingStore{reserveSuccessErr: store.ErrUserNotFound}
	called := false
	h := MCPMiddleware(st)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := newToolCallRequest("grok_web_search")
	req = req.WithContext(auth.WithUser(req.Context(), &auth.AuthenticatedUser{User: store.User{ID: "deleted-user"}}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if called {
		t.Fatal("handler must not be called when the authenticated user disappeared")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403 for deleted user race, got %d", rec.Code)
	}
}

func TestMissingExtractedToolNameSkipsReserve(t *testing.T) {
	st := &recordingStore{}
	called := false
	h := MCPMiddleware(st)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"grok_web_search"}}`))
	req = req.WithContext(auth.WithUser(req.Context(), &auth.AuthenticatedUser{User: store.User{ID: "u1"}}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("request without extracted tool name should pass through")
	}
	if st.reserveSuccessCalls != 0 {
		t.Fatalf("request without extracted tool name must not reserve success, got %d", st.reserveSuccessCalls)
	}
}
