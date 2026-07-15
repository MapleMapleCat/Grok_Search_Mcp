package auth

import (
	"context"
	"testing"
	"time"

	"github.com/grok-mcp/internal/store"
)

type cachedResolverStore struct {
	store.TestStore
	key          *store.APIKey
	keys         map[string]*store.APIKey
	user         *store.User
	tier         *store.Tier
	getKeyCalls  int
	getUserCalls int
	getTierCalls int
}

func (s *cachedResolverStore) GetKeyByHash(_ context.Context, keyHash string) (*store.APIKey, error) {
	s.getKeyCalls++
	key := s.key
	if s.keys != nil {
		key = s.keys[keyHash]
	}
	if key == nil {
		return nil, nil
	}
	keyCopy := *key
	return &keyCopy, nil
}

func (s *cachedResolverStore) GetUserByID(context.Context, string) (*store.User, error) {
	s.getUserCalls++
	userCopy := *s.user
	return &userCopy, nil
}

func (s *cachedResolverStore) GetTierByID(context.Context, string) (*store.Tier, error) {
	s.getTierCalls++
	tierCopy := *s.tier
	return &tierCopy, nil
}

func TestCachedAPIKeyResolverCachesCompleteAuthenticationSnapshot(t *testing.T) {
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

	firstUser.SuccessLimit = 99
	tier.SuccessLimit = 7
	_, secondUser, err := resolver.Resolve(context.Background(), "hashed-key")
	if err != nil {
		t.Fatal(err)
	}
	if secondUser.SuccessLimit != 1 {
		t.Fatalf("cached authentication snapshot success limit = %d, want 1", secondUser.SuccessLimit)
	}
	if fakeStore.getKeyCalls != 1 {
		t.Fatalf("key lookup should still be cached, got %d lookups", fakeStore.getKeyCalls)
	}
	if fakeStore.getUserCalls != 1 {
		t.Fatalf("user lookup should be cached, got %d lookups", fakeStore.getUserCalls)
	}
	if fakeStore.getTierCalls != 1 {
		t.Fatalf("tier lookup should be cached, got %d lookups", fakeStore.getTierCalls)
	}
}

func TestCachedAPIKeyResolverRefreshesAuthenticationSnapshotAfterInvalidation(t *testing.T) {
	tier := &store.Tier{ID: "tier0-id", Name: "tier0", RPM: 10, SuccessLimit: 1}
	fakeStore := &cachedResolverStore{
		key:  &store.APIKey{ID: "key-id", UserID: "user-id", Enabled: true},
		user: &store.User{ID: "user-id", Enabled: true, TierID: tier.ID},
		tier: tier,
	}
	resolver := NewCachedAPIKeyResolver(fakeStore, time.Hour)

	if _, _, err := resolver.Resolve(context.Background(), "hashed-key"); err != nil {
		t.Fatal(err)
	}
	tier.SuccessLimit = 7
	resolver.InvalidateAll()

	_, refreshedUser, err := resolver.Resolve(context.Background(), "hashed-key")
	if err != nil {
		t.Fatal(err)
	}
	if refreshedUser.SuccessLimit != 7 {
		t.Fatalf("refreshed success limit = %d, want 7", refreshedUser.SuccessLimit)
	}
	if fakeStore.getKeyCalls != 2 || fakeStore.getUserCalls != 2 || fakeStore.getTierCalls != 2 {
		t.Fatalf("lookups after invalidation = key:%d user:%d tier:%d, want 2 each", fakeStore.getKeyCalls, fakeStore.getUserCalls, fakeStore.getTierCalls)
	}
}

func TestCachedAPIKeyResolverReclaimsExpiredEntriesAtBoundedIntervals(t *testing.T) {
	currentTime := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	tier := &store.Tier{ID: "tier-id", Name: "tier", RPM: 10, SuccessLimit: 10}
	fakeStore := &cachedResolverStore{
		keys: map[string]*store.APIKey{
			"first-hash":  {ID: "first-key", UserID: "user-id", Enabled: true},
			"second-hash": {ID: "second-key", UserID: "user-id", Enabled: true},
			"third-hash":  {ID: "third-key", UserID: "user-id", Enabled: true},
		},
		user: &store.User{ID: "user-id", Enabled: true, TierID: tier.ID},
		tier: tier,
	}
	resolver := NewCachedAPIKeyResolver(fakeStore, 10*time.Second)
	resolver.now = func() time.Time { return currentTime }

	if _, _, err := resolver.Resolve(context.Background(), "first-hash"); err != nil {
		t.Fatal(err)
	}
	firstCleanupAt := currentTime.Add(authCacheCleanupInterval)
	if !resolver.nextCleanupAt.Equal(firstCleanupAt) {
		t.Fatalf("next cleanup = %s, want %s", resolver.nextCleanupAt, firstCleanupAt)
	}

	currentTime = currentTime.Add(11 * time.Second)
	if _, _, err := resolver.Resolve(context.Background(), "second-hash"); err != nil {
		t.Fatal(err)
	}
	if _, exists := resolver.byHash["first-hash"]; !exists {
		t.Fatal("cleanup ran before the bounded cleanup interval")
	}
	if !resolver.nextCleanupAt.Equal(firstCleanupAt) {
		t.Fatalf("early resolve changed next cleanup to %s, want %s", resolver.nextCleanupAt, firstCleanupAt)
	}

	currentTime = firstCleanupAt
	if _, _, err := resolver.Resolve(context.Background(), "third-hash"); err != nil {
		t.Fatal(err)
	}
	if _, exists := resolver.byHash["first-hash"]; exists {
		t.Fatal("expired first entry was not reclaimed")
	}
	if _, exists := resolver.byHash["second-hash"]; exists {
		t.Fatal("expired second entry was not reclaimed")
	}
	if _, exists := resolver.byHash["third-hash"]; !exists {
		t.Fatal("current entry was not cached after cleanup")
	}
	wantedNextCleanupAt := currentTime.Add(authCacheCleanupInterval)
	if !resolver.nextCleanupAt.Equal(wantedNextCleanupAt) {
		t.Fatalf("next cleanup = %s, want %s", resolver.nextCleanupAt, wantedNextCleanupAt)
	}
}
