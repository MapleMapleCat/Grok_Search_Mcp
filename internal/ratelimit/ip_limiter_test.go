package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
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

func TestIPLimiterRejectsEmptyOrMalformedForwardedClientIPHeaders(t *testing.T) {
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

	if responseRecorder := performRequest("", ""); responseRecorder.Code != http.StatusBadRequest {
		t.Fatalf("empty-header request status = %d, want %d", responseRecorder.Code, http.StatusBadRequest)
	}
	if responseRecorder := performRequest("not-an-ip", "unknown, invalid"); responseRecorder.Code != http.StatusBadRequest {
		t.Fatalf("malformed-header request status = %d, want %d", responseRecorder.Code, http.StatusBadRequest)
	}
	if allowedRequestCount != 0 {
		t.Fatalf("allowed request count = %d, want 0", allowedRequestCount)
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

func TestIPLimiterRequiresRealIPAndForwardedForToAgree(t *testing.T) {
	limiter := NewIPLimiter(1)
	defer limiter.Close()

	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	firstRequest := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	firstRequest.RemoteAddr = "203.0.113.10:10001"
	firstRequest.Header.Set("X-Real-IP", "198.51.100.10")
	firstRequest.Header.Set("X-Forwarded-For", "198.51.100.10, 203.0.113.20")
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
		t.Fatalf("X-Real-IP should reuse the first bucket, status = %d", thirdRecorder.Code)
	}

	conflictingRequest := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	conflictingRequest.Header.Set("X-Real-IP", "198.51.100.10")
	conflictingRequest.Header.Set("X-Forwarded-For", "198.51.100.20")
	conflictingRecorder := httptest.NewRecorder()
	handler.ServeHTTP(conflictingRecorder, conflictingRequest)
	if conflictingRecorder.Code != http.StatusBadRequest {
		t.Fatalf("conflicting forwarded headers status = %d, want %d", conflictingRecorder.Code, http.StatusBadRequest)
	}
}

func TestIPLimiterCanonicalizesEquivalentAddressesIntoOneBucket(t *testing.T) {
	limiter := NewIPLimiter(1)
	defer limiter.Close()

	handler := limiter.Middleware()(http.HandlerFunc(func(responseWriter http.ResponseWriter, _ *http.Request) {
		responseWriter.WriteHeader(http.StatusOK)
	}))

	firstRequest := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	firstRequest.Header.Set("X-Forwarded-For", "::ffff:198.51.100.10")
	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, firstRequest)
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want %d", firstRecorder.Code, http.StatusOK)
	}

	secondRequest := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	secondRequest.Header.Set("X-Forwarded-For", "198.51.100.10")
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, secondRequest)
	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("canonical equivalent request status = %d, want %d", secondRecorder.Code, http.StatusTooManyRequests)
	}
}

func TestIPLimiterIncrementallyCleansConfiguredShardBatch(t *testing.T) {
	limiter := NewIPLimiterWithConfig(IPLimiterConfig{
		RequestsPerMinute:     1,
		ShardCount:            4,
		EntryIdleTTL:          time.Minute,
		CleanupInterval:       time.Hour,
		CleanupShardBatchSize: 2,
	})
	defer limiter.Close()

	now := time.Now()
	expiredAt := now.Add(-2 * time.Minute)
	for shardIndex := range limiter.shards {
		address := netip.AddrFrom4([4]byte{198, 51, 100, byte(shardIndex + 1)})
		limiter.shards[shardIndex].entries[address] = &ipEntry{
			limiter:  limiter.newTokenBucket(),
			lastSeen: expiredAt,
		}
		limiter.shards[shardIndex].highWatermark = 1
	}

	limiter.cleanupNextShards(now)
	for shardIndex := range limiter.shards {
		entryCount := len(limiter.shards[shardIndex].entries)
		if shardIndex < 2 && entryCount != 0 {
			t.Fatalf("cleaned shard %d entry count = %d, want 0", shardIndex, entryCount)
		}
		if shardIndex >= 2 && entryCount != 1 {
			t.Fatalf("deferred shard %d entry count = %d, want 1", shardIndex, entryCount)
		}
	}
}

func TestIPLimiterRebuildsShardAfterHighWatermarkDrops(t *testing.T) {
	limiter := NewIPLimiterWithConfig(IPLimiterConfig{
		RequestsPerMinute: 1,
		ShardCount:        1,
		EntryIdleTTL:      time.Minute,
		CleanupInterval:   time.Hour,
	})
	defer limiter.Close()

	now := time.Now()
	expiredAt := now.Add(-2 * time.Minute)
	shard := &limiter.shards[0]
	for addressIndex := 0; addressIndex < minimumShardHighWatermarkForRebuild; addressIndex++ {
		address := netip.AddrFrom4([4]byte{10, byte(addressIndex >> 8), byte(addressIndex), 1})
		shard.entries[address] = &ipEntry{
			limiter:  limiter.newTokenBucket(),
			lastSeen: expiredAt,
		}
	}
	shard.highWatermark = len(shard.entries)

	limiter.cleanupExpiredEntries(now)
	if len(shard.entries) != 0 {
		t.Fatalf("entry count after cleanup = %d, want 0", len(shard.entries))
	}
	if shard.highWatermark != 0 {
		t.Fatalf("high watermark after rebuild = %d, want 0", shard.highWatermark)
	}
}
