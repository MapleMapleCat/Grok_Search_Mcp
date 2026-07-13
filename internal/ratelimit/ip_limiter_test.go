package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIPLimiterBypassesRequestsWithoutForwardedClientIPHeaders(t *testing.T) {
	limiter := NewIPLimiter(1)
	defer limiter.Close()

	allowedRequests := 0
	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		allowedRequests++
		w.WriteHeader(http.StatusOK)
	}))

	firstRequest := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	firstRequest.RemoteAddr = "198.51.100.10:10001"
	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, firstRequest)
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want %d", firstRecorder.Code, http.StatusOK)
	}

	secondRequest := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	secondRequest.RemoteAddr = "198.51.100.10:10002"
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, secondRequest)
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("second headerless request status = %d, want %d", secondRecorder.Code, http.StatusOK)
	}

	if allowedRequests != 2 {
		t.Fatalf("allowed request count = %d, want %d", allowedRequests, 2)
	}
}

func TestIPLimiterBypassesEmptyOrMalformedForwardedClientIPHeaders(t *testing.T) {
	limiter := NewIPLimiter(1)
	defer limiter.Close()

	allowedRequestCount := 0
	handler := limiter.Middleware()(http.HandlerFunc(func(responseWriter http.ResponseWriter, _ *http.Request) {
		allowedRequestCount++
		responseWriter.WriteHeader(http.StatusOK)
	}))

	performRequest := func(realIP, forwardedFor string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		request.Header["X-Real-Ip"] = []string{realIP}
		request.Header["X-Forwarded-For"] = []string{forwardedFor}
		responseRecorder := httptest.NewRecorder()
		handler.ServeHTTP(responseRecorder, request)
		return responseRecorder
	}

	if responseRecorder := performRequest("", ""); responseRecorder.Code != http.StatusOK {
		t.Fatalf("first empty-header request status = %d, want %d", responseRecorder.Code, http.StatusOK)
	}
	if responseRecorder := performRequest("not-an-ip", "unknown, invalid"); responseRecorder.Code != http.StatusOK {
		t.Fatalf("malformed-header request status = %d, want %d", responseRecorder.Code, http.StatusOK)
	}
	if allowedRequestCount != 2 {
		t.Fatalf("allowed request count = %d, want %d", allowedRequestCount, 2)
	}
}

func TestIPLimiterUsesForwardedClientIPWithoutProxyConfiguration(t *testing.T) {
	limiter := NewIPLimiter(1)
	defer limiter.Close()

	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	firstRequest := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	firstRequest.RemoteAddr = "203.0.113.10:10001"
	firstRequest.Header.Set("X-Forwarded-For", "198.51.100.10")
	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, firstRequest)
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want %d", firstRecorder.Code, http.StatusOK)
	}

	secondRequest := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	secondRequest.RemoteAddr = "203.0.113.10:10002"
	secondRequest.Header.Set("X-Forwarded-For", "198.51.100.11")
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, secondRequest)
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("different forwarded clients should use separate buckets, status = %d", secondRecorder.Code)
	}
}

func TestIPLimiterPrioritizesRealIPOverForwardedFor(t *testing.T) {
	limiter := NewIPLimiter(1)
	defer limiter.Close()

	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	firstRequest := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	firstRequest.RemoteAddr = "203.0.113.10:10001"
	firstRequest.Header.Set("X-Real-IP", "198.51.100.10")
	firstRequest.Header.Set("X-Forwarded-For", "198.51.100.20, 203.0.113.20")
	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, firstRequest)
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first forwarded request status = %d, want %d", firstRecorder.Code, http.StatusOK)
	}

	secondRequest := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	secondRequest.RemoteAddr = "203.0.113.10:10002"
	secondRequest.Header.Set("X-Forwarded-For", "198.51.100.20, 203.0.113.20")
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, secondRequest)
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("X-Forwarded-For client should get a separate bucket, status = %d", secondRecorder.Code)
	}

	thirdRequest := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	thirdRequest.RemoteAddr = "203.0.113.10:10003"
	thirdRequest.Header.Set("X-Real-IP", "198.51.100.10")
	thirdRecorder := httptest.NewRecorder()
	handler.ServeHTTP(thirdRecorder, thirdRequest)
	if thirdRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("X-Real-IP should take precedence and reuse the first bucket, status = %d", thirdRecorder.Code)
	}
}
