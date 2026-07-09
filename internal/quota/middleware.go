// Package quota 在 MCP tools/call 前按用户成功请求额度原子预留 success_calls。
package quota

import (
	"context"
	"errors"
	"net/http"

	"github.com/grok-mcp/internal/auth"
	"github.com/grok-mcp/internal/store"
	"github.com/grok-mcp/internal/usage"
)

// SuccessQuotaReserver is the minimal store surface needed by quota middleware.
// Defined at the consumer side so quota does not require the full store.Store.
type SuccessQuotaReserver interface {
	ReserveSuccessCall(ctx context.Context, userID string, successLimit int) error
}

// MCPMiddleware 仅对 tools/call 在 handler 前原子预留用户 success_calls。
func MCPMiddleware(reserver SuccessQuotaReserver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 优先复用链路前置 ExtractToolNameMiddleware 写入的工具名；
			// 兼容未挂载该中间件的旧用法，回退到一次解析。
			toolName, ok := usage.ToolNameFromContext(r.Context())
			if !ok {
				toolName = usage.PeekToolName(r)
			}
			if toolName == "" {
				next.ServeHTTP(w, r)
				return
			}
			user, ok := auth.UserFromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			if err := reserver.ReserveSuccessCall(r.Context(), user.ID, user.SuccessLimit); err != nil {
				if errors.Is(err, store.ErrQuotaSuccess) {
					http.Error(w, "success request limit exceeded", http.StatusTooManyRequests)
					return
				}
				if errors.Is(err, store.ErrUserNotFound) {
					// 鉴权后用户被删除等竞态：返回 403，避免误报 429 额度耗尽。
					http.Error(w, "user not found", http.StatusForbidden)
					return
				}
				http.Error(w, "quota check failed", http.StatusInternalServerError)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
