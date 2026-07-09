package panel

import (
	"testing"

	"github.com/grok-mcp/internal/store"
)

func TestToUserResponseWithTierMarksUnavailableWhenTierMissing(t *testing.T) {
	user := &store.User{ID: "u1", Username: "alice", Role: store.RoleUser, Enabled: true, TierID: "gone", SuccessCalls: 3}
	resp := toUserResponseWithTier(user, nil)
	if !resp.LimitsUnavailable {
		t.Fatal("expected limits_unavailable when tier is nil")
	}
	if resp.RPM != 0 || resp.SuccessLimit != 0 {
		t.Fatalf("rpm/success must stay 0 when unavailable, got rpm=%d success=%d", resp.RPM, resp.SuccessLimit)
	}
	if resp.SuccessCalls != 3 {
		t.Fatalf("success_calls should still surface, got %d", resp.SuccessCalls)
	}
}

func TestToUserResponseWithTierUsesTierLimits(t *testing.T) {
	user := &store.User{ID: "u1", Username: "alice", Role: store.RoleUser, Enabled: true, TierID: "t1"}
	tier := &store.Tier{ID: "t1", Name: "custom", Level: 9, RPM: 12, SuccessLimit: 34}
	resp := toUserResponseWithTier(user, tier)
	if resp.LimitsUnavailable {
		t.Fatal("limits must be available when tier is present")
	}
	if resp.RPM != 12 || resp.SuccessLimit != 34 || resp.TierName != "custom" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}
