package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type ipEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// IPLimiter 对携带反向代理客户端 IP Header 的请求按来源 IP 共享令牌桶。
// 不携带 X-Real-IP 或 X-Forwarded-For 的普通请求不会启用 IP 限流。
// Header 仅负责启用限流；只有 RemoteAddr 命中可信反代 CIDR 时才会采用 Header 中的 IP。
type IPLimiter struct {
	requestsPerMinute int
	clientIPResolver  *ClientIPResolver
	mu                sync.Mutex
	entries           map[string]*ipEntry
	closeOnce         sync.Once
	stop              chan struct{}
}

// NewIPLimiter 创建来源 IP 限流器。
func NewIPLimiter(requestsPerMinute int) *IPLimiter {
	if requestsPerMinute <= 0 {
		requestsPerMinute = 60
	}
	limiter := &IPLimiter{
		requestsPerMinute: requestsPerMinute,
		clientIPResolver:  NewClientIPResolver(nil),
		entries:           make(map[string]*ipEntry),
		stop:              make(chan struct{}),
	}
	go limiter.cleanupLoop()
	return limiter
}

// SetTrustedProxies 设置可信反向代理网段；nil/空表示永不信任转发头。
func (limiter *IPLimiter) SetTrustedProxies(networks []*net.IPNet) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.clientIPResolver = NewClientIPResolver(networks)
}

func (limiter *IPLimiter) allow(clientAddress string) bool {
	now := time.Now()
	clientAddress = strings.TrimSpace(clientAddress)
	if clientAddress == "" {
		clientAddress = "unknown"
	}

	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	entry, ok := limiter.entries[clientAddress]
	if !ok {
		entry = &ipEntry{limiter: limiter.newTokenBucket()}
		limiter.entries[clientAddress] = entry
	}
	entry.lastSeen = now
	return entry.limiter.Allow()
}

func (limiter *IPLimiter) newTokenBucket() *rate.Limiter {
	return rate.NewLimiter(rate.Every(time.Minute/time.Duration(limiter.requestsPerMinute)), limiter.requestsPerMinute)
}

func (limiter *IPLimiter) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-limiter.stop:
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-30 * time.Minute)
			limiter.mu.Lock()
			for clientAddress, entry := range limiter.entries {
				if entry.lastSeen.Before(cutoff) {
					delete(limiter.entries, clientAddress)
				}
			}
			limiter.mu.Unlock()
		}
	}
}

// Close 停止后台清理。
func (limiter *IPLimiter) Close() {
	limiter.closeOnce.Do(func() { close(limiter.stop) })
}

// Middleware 仅对携带 X-Real-IP 或 X-Forwarded-For 的请求按来源 IP 限流。
// 直连对端不可信时仍以 RemoteAddr 为桶键，避免伪造 Header 绕过已启用的限流。
func (limiter *IPLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !HasForwardedClientIPHeader(r) {
				next.ServeHTTP(w, r)
				return
			}
			if !limiter.allow(limiter.clientIP(r)) {
				w.Header().Set("Retry-After", "60")
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (limiter *IPLimiter) clientIP(r *http.Request) string {
	limiter.mu.Lock()
	resolver := limiter.clientIPResolver
	limiter.mu.Unlock()
	return resolver.Resolve(r)
}
