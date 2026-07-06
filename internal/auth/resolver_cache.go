package auth

import (
	"context"
	"sync"
	"time"

	"github.com/grok-mcp/internal/store"
)

const defaultAuthCacheTTL = 30 * time.Second

type cacheEntry struct {
	key   *store.APIKey
	user  *store.User
	until time.Time
}

// CachedAPIKeyResolver 缓存 MCP 鉴权链上的 key + 带 tier 限额的用户，减少热路径 DB 查询。
type CachedAPIKeyResolver struct {
	st     store.Store
	ttl    time.Duration
	mu     sync.Mutex
	byHash map[string]cacheEntry
}

// NewCachedAPIKeyResolver 创建鉴权解析缓存；ttl<=0 时使用默认 30s。
func NewCachedAPIKeyResolver(st store.Store, ttl time.Duration) *CachedAPIKeyResolver {
	if ttl <= 0 {
		ttl = defaultAuthCacheTTL
	}
	return &CachedAPIKeyResolver{
		st:     st,
		ttl:    ttl,
		byHash: make(map[string]cacheEntry),
	}
}

// Resolve 按 API Key 哈希加载密钥与启用用户（含 tier 限额）。
func (c *CachedAPIKeyResolver) Resolve(ctx context.Context, keyHash string) (*store.APIKey, *store.User, error) {
	now := time.Now()
	c.mu.Lock()
	if e, ok := c.byHash[keyHash]; ok && now.Before(e.until) {
		key, user := e.key, e.user
		c.mu.Unlock()
		return cloneAPIKey(key), cloneUser(user), nil
	}
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
	c.byHash[keyHash] = cacheEntry{
		key:   key,
		user:  user,
		until: now.Add(c.ttl),
	}
	c.mu.Unlock()
	return cloneAPIKey(key), cloneUser(user), nil
}

// InvalidateAll 清空缓存（管理员变更 tier/用户/密钥后调用）。
func (c *CachedAPIKeyResolver) InvalidateAll() {
	c.mu.Lock()
	c.byHash = make(map[string]cacheEntry)
	c.mu.Unlock()
}

func cloneAPIKey(k *store.APIKey) *store.APIKey {
	if k == nil {
		return nil
	}
	cp := *k
	return &cp
}

func cloneUser(u *store.User) *store.User {
	if u == nil {
		return nil
	}
	cp := *u
	return &cp
}
