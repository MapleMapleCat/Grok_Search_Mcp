package auth

import (
	"context"
	"sync"
	"time"

	"github.com/grok-mcp/internal/store"
)

const (
	defaultAuthCacheTTL      = 30 * time.Second
	authCacheCleanupInterval = time.Minute
)

type cacheEntry struct {
	key   *store.APIKey
	user  *AuthenticatedUser
	until time.Time
}

// CachedAPIKeyResolver 缓存 MCP 鉴权链上的完整认证快照，避免热路径重复查询
// API key、用户与 tier。管理员修改鉴权数据后应调用 InvalidateAll。
type CachedAPIKeyResolver struct {
	st            APIKeyStore
	ttl           time.Duration
	now           func() time.Time
	mu            sync.Mutex
	byHash        map[string]cacheEntry
	nextCleanupAt time.Time
	generation    uint64
}

// NewCachedAPIKeyResolver 创建鉴权解析缓存；ttl<=0 时使用默认 30s。
func NewCachedAPIKeyResolver(st APIKeyStore, ttl time.Duration) *CachedAPIKeyResolver {
	if ttl <= 0 {
		ttl = defaultAuthCacheTTL
	}
	return &CachedAPIKeyResolver{
		st:     st,
		ttl:    ttl,
		now:    time.Now,
		byHash: make(map[string]cacheEntry),
	}
}

// Resolve 按 API Key 哈希加载密钥与启用用户（含 tier 限额）。
func (c *CachedAPIKeyResolver) Resolve(ctx context.Context, keyHash string) (*store.APIKey, *AuthenticatedUser, error) {
	now := c.now()
	c.mu.Lock()
	c.removeExpiredEntries(now)
	if entry, ok := c.byHash[keyHash]; ok && now.Before(entry.until) {
		key := cloneAPIKey(entry.key)
		user := cloneAuthenticatedUser(entry.user)
		c.mu.Unlock()
		return key, user, nil
	}
	loadGeneration := c.generation
	c.mu.Unlock()

	key, err := c.st.GetKeyByHash(ctx, keyHash)
	if err != nil {
		return nil, nil, err
	}
	if key == nil {
		return nil, nil, nil
	}
	user, err := LoadUserWithTierLimits(ctx, c.st, key.UserID)
	if err != nil {
		return nil, nil, err
	}

	c.mu.Lock()
	if c.generation == loadGeneration {
		c.byHash[keyHash] = cacheEntry{
			key:   cloneAPIKey(key),
			user:  cloneAuthenticatedUser(user),
			until: now.Add(c.ttl),
		}
	}
	c.mu.Unlock()
	return cloneAPIKey(key), cloneAuthenticatedUser(user), nil
}

func (c *CachedAPIKeyResolver) removeExpiredEntries(now time.Time) {
	if now.Before(c.nextCleanupAt) {
		return
	}
	for keyHash, entry := range c.byHash {
		if !now.Before(entry.until) {
			delete(c.byHash, keyHash)
		}
	}
	c.nextCleanupAt = now.Add(authCacheCleanupInterval)
}

// InvalidateAll 清空缓存（管理员变更 tier/用户/密钥后调用）。
func (c *CachedAPIKeyResolver) InvalidateAll() {
	c.mu.Lock()
	c.byHash = make(map[string]cacheEntry)
	c.nextCleanupAt = time.Time{}
	c.generation++
	c.mu.Unlock()
}

func cloneAPIKey(key *store.APIKey) *store.APIKey {
	if key == nil {
		return nil
	}
	keyCopy := *key
	return &keyCopy
}

func cloneAuthenticatedUser(user *AuthenticatedUser) *AuthenticatedUser {
	if user == nil {
		return nil
	}
	userCopy := *user
	return &userCopy
}
