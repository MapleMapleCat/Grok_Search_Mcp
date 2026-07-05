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

// LoadUserWithTierLimits 加载用户，并把生效限额（rpm/total_limit/success_limit）合并进返回的
// user 对象。限额以 tier 为唯一来源：用户自身不再持久化任何限额字段，因此不存在“历史残留值”。
// tier_id 缺失时回退到默认 tier0，确保任何情况下都有明确限额来源。
func LoadUserWithTierLimits(ctx context.Context, st store.Store, userID string) (*store.User, error) {
	user, err := st.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	applyTierLimits(ctx, st, user)
	return user, nil
}

// resolveTier 返回用户的生效 tier：优先 user.TierID，缺失则回退到默认 tier0。
// tier0 也找不到（理论上迁移已预置）时返回 nil，调用方据此保持零值（=不限）。
func resolveTier(ctx context.Context, st store.Store, user *store.User) *store.Tier {
	id := user.TierID
	if id != "" {
		if tier, err := st.GetTierByID(ctx, id); err == nil && tier != nil {
			return tier
		}
	}
	if tier, err := st.GetTierByName(ctx, "tier0"); err == nil && tier != nil {
		return tier
	}
	return nil
}

// applyTierLimits 把生效 tier 的限额就地写进 user；tier 缺失时回退 tier0，杜绝无限额来源。
func applyTierLimits(ctx context.Context, st store.Store, user *store.User) {
	if user == nil {
		return
	}
	if tier := resolveTier(ctx, st, user); tier != nil {
		user.RPM = tier.RPM
		user.TotalLimit = tier.TotalLimit
		user.SuccessLimit = tier.SuccessLimit
	}
}
