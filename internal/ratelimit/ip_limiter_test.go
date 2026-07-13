package ratelimit

import (
	"net"
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

func TestIPLimiterTreatsPresentEmptyForwardedHeaderAsProtectionSignal(t *testing.T) {
	limiter := NewIPLimiter(1)
	defer limiter.Close()

	handler := limiter.Middleware()(http.HandlerFunc(func(responseWriter http.ResponseWriter, _ *http.Request) {
		responseWriter.WriteHeader(http.StatusOK)
	}))

	performRequest := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		request.RemoteAddr = "198.51.100.10:10001"
		request.Header["X-Real-Ip"] = []string{""}
		responseRecorder := httptest.NewRecorder()
		handler.ServeHTTP(responseRecorder, request)
		return responseRecorder
	}

	if responseRecorder := performRequest(); responseRecorder.Code != http.StatusOK {
		t.Fatalf("first empty-header request status = %d, want %d", responseRecorder.Code, http.StatusOK)
	}
	if responseRecorder := performRequest(); responseRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second empty-header request status = %d, want %d", responseRecorder.Code, http.StatusTooManyRequests)
	}
}

func TestIPLimiterIgnoresForwardedHeadersWithoutTrustedProxy(t *testing.T) {
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
	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("untrusted forwarded header must not split buckets, status = %d", secondRecorder.Code)
	}
}

func TestIPLimiterUsesForwardedHeadersFromTrustedProxy(t *testing.T) {
	limiter := NewIPLimiter(1)
	defer limiter.Close()
	limiter.SetTrustedProxies([]*net.IPNet{mustParseCIDR(t, "203.0.113.0/24")})

	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	firstRequest := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	firstRequest.RemoteAddr = "203.0.113.10:10001"
	firstRequest.Header.Set("X-Forwarded-For", "198.51.100.10")
	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, firstRequest)
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first forwarded request status = %d, want %d", firstRecorder.Code, http.StatusOK)
	}

	secondRequest := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	secondRequest.RemoteAddr = "203.0.113.10:10002"
	secondRequest.Header.Set("X-Forwarded-For", "198.51.100.11")
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, secondRequest)
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("different forwarded client should get a separate bucket, status = %d", secondRecorder.Code)
	}

	thirdRequest := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	thirdRequest.RemoteAddr = "203.0.113.10:10003"
	thirdRequest.Header.Set("X-Real-IP", "198.51.100.10")
	thirdRecorder := httptest.NewRecorder()
	handler.ServeHTTP(thirdRecorder, thirdRequest)
	if thirdRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("same trusted forwarded client should share bucket, status = %d", thirdRecorder.Code)
	}
}

func mustParseCIDR(t *testing.T, rawCIDR string) *net.IPNet {
	t.Helper()
	_, network, err := net.ParseCIDR(rawCIDR)
	if err != nil {
		t.Fatalf("parse CIDR %q: %v", rawCIDR, err)
	}
	return network
}
