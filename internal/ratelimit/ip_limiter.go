package ratelimit

import (
	"net/http"
	"net/netip"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	defaultIPLimiterShardCount          = 64
	defaultIPLimiterEntryIdleTTL        = 5 * time.Minute
	defaultIPLimiterCleanupInterval     = 10 * time.Second
	defaultIPLimiterCleanupShardBatch   = 4
	minimumShardHighWatermarkForRebuild = 256
	shardHighWatermarkRebuildDivisor    = 4
)

type ipEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type ipLimiterShard struct {
	mu            sync.Mutex
	entries       map[netip.Addr]*ipEntry
	highWatermark int
}

// IPLimiterConfig controls the in-memory IP limiter. Zero values use bounded,
// production-oriented defaults.
type IPLimiterConfig struct {
	RequestsPerMinute     int
	ClientIPResolver      *ClientIPResolver
	ShardCount            int
	EntryIdleTTL          time.Duration
	CleanupInterval       time.Duration
	CleanupShardBatchSize int
}

// IPLimiter 对能够解析出有效反向代理客户端 IP 的请求按来源 IP 共享令牌桶。
// 无有效 X-Real-IP 或 X-Forwarded-For 的请求不会启用 IP 限流。
// 部署必须确保应用仅能由会清理并覆盖这些 Header 的可信反向代理访问。
type IPLimiter struct {
	requestsPerMinute     int
	clientIPResolver      *ClientIPResolver
	shards                []ipLimiterShard
	entryIdleTTL          time.Duration
	cleanupInterval       time.Duration
	cleanupShardBatchSize int
	cleanupCursor         int
	closeOnce             sync.Once
	stop                  chan struct{}
	workerDone            chan struct{}
}

// NewIPLimiter 创建来源 IP 限流器。
func NewIPLimiter(requestsPerMinute int) *IPLimiter {
	return NewIPLimiterWithConfig(IPLimiterConfig{RequestsPerMinute: requestsPerMinute})
}

// NewIPLimiterWithConfig creates a sharded limiter with incremental cleanup.
func NewIPLimiterWithConfig(config IPLimiterConfig) *IPLimiter {
	if config.RequestsPerMinute <= 0 {
		config.RequestsPerMinute = 60
	}
	if config.ClientIPResolver == nil {
		config.ClientIPResolver = NewClientIPResolver()
	}
	if config.ShardCount <= 0 {
		config.ShardCount = defaultIPLimiterShardCount
	}
	if config.EntryIdleTTL <= 0 {
		config.EntryIdleTTL = defaultIPLimiterEntryIdleTTL
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = defaultIPLimiterCleanupInterval
	}
	if config.CleanupShardBatchSize <= 0 {
		config.CleanupShardBatchSize = defaultIPLimiterCleanupShardBatch
	}
	if config.CleanupShardBatchSize > config.ShardCount {
		config.CleanupShardBatchSize = config.ShardCount
	}

	limiter := &IPLimiter{
		requestsPerMinute:     config.RequestsPerMinute,
		clientIPResolver:      config.ClientIPResolver,
		shards:                make([]ipLimiterShard, config.ShardCount),
		entryIdleTTL:          config.EntryIdleTTL,
		cleanupInterval:       config.CleanupInterval,
		cleanupShardBatchSize: config.CleanupShardBatchSize,
		stop:                  make(chan struct{}),
		workerDone:            make(chan struct{}),
	}
	for shardIndex := range limiter.shards {
		limiter.shards[shardIndex].entries = make(map[netip.Addr]*ipEntry)
	}
	go limiter.cleanupLoop()
	return limiter
}

func (limiter *IPLimiter) allow(clientAddress netip.Addr) bool {
	return limiter.allowAt(clientAddress, time.Now())
}

func (limiter *IPLimiter) allowAt(clientAddress netip.Addr, now time.Time) bool {
	shard := limiter.shardFor(clientAddress)
	shard.mu.Lock()
	entry, ok := shard.entries[clientAddress]
	if !ok {
		entry = &ipEntry{limiter: limiter.newTokenBucket()}
		shard.entries[clientAddress] = entry
		if len(shard.entries) > shard.highWatermark {
			shard.highWatermark = len(shard.entries)
		}
	}
	entry.lastSeen = now
	shard.mu.Unlock()

	return entry.limiter.AllowN(now, 1)
}

func (limiter *IPLimiter) newTokenBucket() *rate.Limiter {
	return rate.NewLimiter(rate.Every(time.Minute/time.Duration(limiter.requestsPerMinute)), limiter.requestsPerMinute)
}

func (limiter *IPLimiter) cleanupLoop() {
	defer close(limiter.workerDone)
	ticker := time.NewTicker(limiter.cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-limiter.stop:
			return
		case now := <-ticker.C:
			limiter.cleanupNextShards(now)
		}
	}
}

func (limiter *IPLimiter) cleanupNextShards(now time.Time) {
	for cleanedShardCount := 0; cleanedShardCount < limiter.cleanupShardBatchSize; cleanedShardCount++ {
		limiter.cleanupShard(limiter.cleanupCursor, now)
		limiter.cleanupCursor = (limiter.cleanupCursor + 1) % len(limiter.shards)
	}
}

func (limiter *IPLimiter) cleanupExpiredEntries(now time.Time) {
	for shardIndex := range limiter.shards {
		limiter.cleanupShard(shardIndex, now)
	}
}

func (limiter *IPLimiter) cleanupShard(shardIndex int, now time.Time) {
	shard := &limiter.shards[shardIndex]
	cutoff := now.Add(-limiter.entryIdleTTL)

	shard.mu.Lock()
	defer shard.mu.Unlock()
	for clientAddress, entry := range shard.entries {
		if entry.lastSeen.Before(cutoff) {
			delete(shard.entries, clientAddress)
		}
	}

	shouldRebuild := shard.highWatermark >= minimumShardHighWatermarkForRebuild &&
		len(shard.entries) <= shard.highWatermark/shardHighWatermarkRebuildDivisor
	if !shouldRebuild {
		return
	}
	rebuiltEntries := make(map[netip.Addr]*ipEntry, len(shard.entries))
	for clientAddress, entry := range shard.entries {
		rebuiltEntries[clientAddress] = entry
	}
	shard.entries = rebuiltEntries
	shard.highWatermark = len(rebuiltEntries)
}

func (limiter *IPLimiter) shardFor(clientAddress netip.Addr) *ipLimiterShard {
	return &limiter.shards[limiter.shardIndexFor(clientAddress)]
}

func (limiter *IPLimiter) shardIndexFor(clientAddress netip.Addr) int {
	const (
		fnvOffsetBasis uint64 = 1469598103934665603
		fnvPrime       uint64 = 1099511628211
	)
	addressBytes := clientAddress.As16()
	hash := fnvOffsetBasis
	for _, addressByte := range addressBytes {
		hash ^= uint64(addressByte)
		hash *= fnvPrime
	}
	return int(hash % uint64(len(limiter.shards)))
}

// Close 停止后台清理。
func (limiter *IPLimiter) Close() {
	limiter.closeOnce.Do(func() {
		close(limiter.stop)
		<-limiter.workerDone
	})
}

// Middleware 仅对能够从 X-Real-IP 或 X-Forwarded-For 解析出有效 IP 的请求限流。
func (limiter *IPLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientAddress, err := limiter.clientIPResolver.ResolveAddress(r)
			if err != nil {
				http.Error(w, ErrInvalidForwardedClientIPHeaders.Error(), http.StatusBadRequest)
				return
			}
			if !clientAddress.IsValid() {
				next.ServeHTTP(w, r)
				return
			}
			if !limiter.allow(clientAddress) {
				w.Header().Set("Retry-After", "60")
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (limiter *IPLimiter) clientIP(r *http.Request) string {
	return limiter.clientIPResolver.Resolve(r)
}
