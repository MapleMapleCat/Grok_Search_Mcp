package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/grok-mcp/internal/store"
)

const defaultTierName = "tier0"

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

// LoadUserWithTierLimits 加载用户，并把生效限额（rpm/success_limit）合并进返回的
// user 对象。限额以 tier 为唯一来源：用户自身不再持久化任何限额字段，因此不存在“历史残留值”。
// tier_id 缺失时回退到默认 tier0；若用户指定的 tier 或默认 tier0 不存在，则返回错误并 fail-closed。
func LoadUserWithTierLimits(ctx context.Context, st store.Store, userID string) (*store.User, error) {
	user, err := st.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err := applyTierLimits(ctx, st, user); err != nil {
		return nil, err
	}
	return user, nil
}

// resolveTier 返回用户的生效 tier：优先 user.TierID，缺失则回退到默认 tier0。
// 指定 tier 或默认 tier0 找不到时返回错误，避免因零值限额变成不限。
func resolveTier(ctx context.Context, st store.Store, user *store.User) (*store.Tier, error) {
	if user == nil {
		return nil, fmt.Errorf("user is required")
	}
	id := strings.TrimSpace(user.TierID)
	if id != "" {
		tier, err := st.GetTierByID(ctx, id)
		if err != nil {
			if errors.Is(err, store.ErrTierNotFound) {
				return nil, fmt.Errorf("assigned tier %q not found: %w", id, err)
			}
			return nil, err
		}
		if tier == nil {
			return nil, fmt.Errorf("assigned tier %q not found: %w", id, store.ErrTierNotFound)
		}
		return tier, nil
	}
	tier, err := st.GetTierByName(ctx, defaultTierName)
	if err != nil {
		return nil, err
	}
	if tier == nil {
		return nil, fmt.Errorf("default tier %q not found: %w", defaultTierName, store.ErrTierNotFound)
	}
	return tier, nil
}

// applyTierLimits 把生效 tier 的限额就地写进 user；tier 缺失时返回错误，杜绝无限额来源。
func applyTierLimits(ctx context.Context, st store.Store, user *store.User) error {
	if user == nil {
		return nil
	}
	tier, err := resolveTier(ctx, st, user)
	if err != nil {
		return err
	}
	user.RPM = tier.RPM
	user.SuccessLimit = tier.SuccessLimit
	return nil
}
