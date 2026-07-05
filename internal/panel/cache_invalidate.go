package panel

// AuthCacheInvalidator 在管理员变更 tier/用户/密钥后清空 MCP 鉴权缓存。
type AuthCacheInvalidator interface {
	InvalidateAll()
}

func (h *Handler) invalidateAuthCache() {
	if h.AuthCache != nil {
		h.AuthCache.InvalidateAll()
	}
}