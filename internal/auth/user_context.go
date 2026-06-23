package auth

import (
	"context"

	"github.com/grok-mcp/internal/store"
)

type userCtxKey struct{}

// WithUser 将所属用户写入 context（MCP 鉴权链在加载 API Key 后设置）。
func WithUser(ctx context.Context, user *store.User) context.Context {
	return context.WithValue(ctx, userCtxKey{}, user)
}

// UserFromContext 返回 MCP 或面板 JWT 链注入的用户。
func UserFromContext(ctx context.Context) (*store.User, bool) {
	u, ok := ctx.Value(userCtxKey{}).(*store.User)
	return u, ok
}