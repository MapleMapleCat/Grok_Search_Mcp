package auth

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/grok-mcp/internal/store"
)

// openAuthStore 打开一个临时 SQLite 库；迁移已预置 tier0（rpm=10,total=1000,success=800）。
func openAuthStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// TestLoadUserWithTierLimitsResolvesFromTier 断言限额来自 tier，而非任何用户自身字段。
func TestLoadUserWithTierLimitsResolvesFromTier(t *testing.T) {
	st := openAuthStore(t)
	ctx := context.Background()

	tier0, err := st.GetTierByName(ctx, "tier0")
	if err != nil || tier0 == nil {
		t.Fatalf("tier0 should be seeded by migration: %v", err)
	}

	u, err := st.CreateUser(ctx, "u", "h", store.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadUserWithTierLimits(ctx, st, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RPM != tier0.RPM || loaded.TotalLimit != tier0.TotalLimit || loaded.SuccessLimit != tier0.SuccessLimit {
		t.Fatalf("limits must mirror tier0: got rpm=%d total=%d success=%d",
			loaded.RPM, loaded.TotalLimit, loaded.SuccessLimit)
	}
}

// TestLoadUserWithTierLimitsFallsBackToTier0 锁定本次修复的核心语义：
// 即使 tier_id 被清空，生效限额也回退到 tier0，绝不退化为“不限”或历史残留值。
func TestLoadUserWithTierLimitsFallsBackToTier0(t *testing.T) {
	st := openAuthStore(t)
	ctx := context.Background()

	u, err := st.CreateUser(ctx, "u", "h", store.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	tier0, _ := st.GetTierByName(ctx, "tier0")

	empty := ""
	if _, err := st.UpdateUser(ctx, u.ID, store.UserUpdates{TierID: &empty}); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadUserWithTierLimits(ctx, st, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tier0 == nil {
		t.Fatal("tier0 not seeded")
	}
	if loaded.RPM != tier0.RPM || loaded.TotalLimit != tier0.TotalLimit || loaded.SuccessLimit != tier0.SuccessLimit {
		t.Fatalf("missing tier must fall back to tier0, got rpm=%d total=%d success=%d",
			loaded.RPM, loaded.TotalLimit, loaded.SuccessLimit)
	}
}
