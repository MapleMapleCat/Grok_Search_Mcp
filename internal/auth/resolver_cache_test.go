package auth

import (
	"context"
	"testing"
	"time"

	"github.com/grok-mcp/internal/store"
)

type cachedResolverStore struct {
	store.TestStore
	key         *store.APIKey
	user        *store.User
	tier        *store.Tier
	getKeyCalls int
}

func (s *cachedResolverStore) GetKeyByHash(context.Context, string) (*store.APIKey, error) {
	s.getKeyCalls++
	keyCopy := *s.key
	return &keyCopy, nil
}

func (s *cachedResolverStore) GetUserByID(context.Context, string) (*store.User, error) {
	userCopy := *s.user
	return &userCopy, nil
}

func (s *cachedResolverStore) GetTierByID(context.Context, string) (*store.Tier, error) {
	tierCopy := *s.tier
	return &tierCopy, nil
}

func TestCachedAPIKeyResolverReloadsTierLimitsForCachedKey(t *testing.T) {
	tier := &store.Tier{ID: "tier0-id", Name: "tier0", RPM: 10, SuccessLimit: 1}
	fakeStore := &cachedResolverStore{
		key:  &store.APIKey{ID: "key-id", UserID: "user-id", Enabled: true},
		user: &store.User{ID: "user-id", Enabled: true, TierID: tier.ID},
		tier: tier,
	}
	resolver := NewCachedAPIKeyResolver(fakeStore, time.Hour)

	_, firstUser, err := resolver.Resolve(context.Background(), "hashed-key")
	if err != nil {
		t.Fatal(err)
	}
	if firstUser.SuccessLimit != 1 {
		t.Fatalf("first resolve success limit = %d, want 1", firstUser.SuccessLimit)
	}

	tier.SuccessLimit = 7
	_, secondUser, err := resolver.Resolve(context.Background(), "hashed-key")
	if err != nil {
		t.Fatal(err)
	}
	if secondUser.SuccessLimit != 7 {
		t.Fatalf("cached key must reload tier limits, got %d", secondUser.SuccessLimit)
	}
	if fakeStore.getKeyCalls != 1 {
		t.Fatalf("key lookup should still be cached, got %d lookups", fakeStore.getKeyCalls)
	}
}
