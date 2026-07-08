package auth

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/grok-mcp/internal/store"
)

// openAuthStore 打开一个临时 SQLite 库；迁移已预置 tier0（rpm=10, success=800）。
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
	if loaded.RPM != tier0.RPM || loaded.SuccessLimit != tier0.SuccessLimit {
		t.Fatalf("limits must mirror tier0: got rpm=%d success=%d",
			loaded.RPM, loaded.SuccessLimit)
	}
}

type tierResolvingStore struct {
	store.TestStore
	user  *store.User
	tiers map[string]*store.Tier
}

func (s tierResolvingStore) GetUserByID(context.Context, string) (*store.User, error) {
	if s.user == nil {
		return nil, store.ErrUserNotFound
	}
	userCopy := *s.user
	return &userCopy, nil
}

func (s tierResolvingStore) GetTierByID(_ context.Context, id string) (*store.Tier, error) {
	if tier, ok := s.tiers[id]; ok {
		tierCopy := *tier
		return &tierCopy, nil
	}
	return nil, store.ErrTierNotFound
}

func (s tierResolvingStore) GetTierByName(_ context.Context, name string) (*store.Tier, error) {
	for _, tier := range s.tiers {
		if tier.Name == name {
			tierCopy := *tier
			return &tierCopy, nil
		}
	}
	return nil, nil
}

// TestLoadUserWithTierLimitsFallsBackToTier0 锁定缺省用户的核心语义：
// tier_id 为空时生效限额回退到 tier0，绝不退化为“不限”或历史残留值。
func TestLoadUserWithTierLimitsFallsBackToTier0(t *testing.T) {
	ctx := context.Background()
	tier0 := &store.Tier{ID: "tier0-id", Name: "tier0", RPM: 10, SuccessLimit: 800}
	st := tierResolvingStore{
		user:  &store.User{ID: "user-without-tier"},
		tiers: map[string]*store.Tier{tier0.ID: tier0},
	}

	loaded, err := LoadUserWithTierLimits(ctx, st, "user-without-tier")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RPM != tier0.RPM || loaded.SuccessLimit != tier0.SuccessLimit {
		t.Fatalf("missing tier must fall back to tier0, got rpm=%d success=%d",
			loaded.RPM, loaded.SuccessLimit)
	}
}

func TestLoadUserWithTierLimitsFailsClosedWhenAssignedTierIsMissing(t *testing.T) {
	st := tierResolvingStore{
		user:  &store.User{ID: "user-with-missing-tier", TierID: "missing-tier"},
		tiers: map[string]*store.Tier{"tier0-id": {ID: "tier0-id", Name: "tier0", RPM: 10, SuccessLimit: 800}},
	}

	if _, err := LoadUserWithTierLimits(context.Background(), st, "user-with-missing-tier"); err == nil {
		t.Fatal("missing assigned tier must fail closed")
	}
}

func TestLoadUserWithTierLimitsFailsClosedWhenDefaultTierIsMissing(t *testing.T) {
	st := tierResolvingStore{
		user:  &store.User{ID: "user-without-tier"},
		tiers: map[string]*store.Tier{},
	}

	if _, err := LoadUserWithTierLimits(context.Background(), st, "user-without-tier"); err == nil {
		t.Fatal("missing default tier0 must fail closed")
	}
}
