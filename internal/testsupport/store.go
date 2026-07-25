// Package testsupport contains reusable test doubles shared across package tests.
package testsupport

import (
	"context"
	"time"

	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/store"
)

// Store is an embeddable store.Store stub whose methods return neutral values.
// Tests override only the behavior relevant to each scenario.
type Store struct {
	store.Store
}

func (Store) Close() error { return nil }

func (Store) CreateUser(context.Context, string, string, store.UserRole) (*store.User, error) {
	return nil, nil
}
func (Store) RegisterUserWithCurrentMode(context.Context, string, string, string, store.RegistrationMode) (*store.User, error) {
	return nil, nil
}
func (Store) InviteCodeExists(context.Context, string) (bool, error) { return false, nil }
func (Store) GetUserByUsername(context.Context, string) (*store.User, error) {
	return nil, nil
}
func (Store) GetUserByID(context.Context, string) (*store.User, error) {
	return nil, store.ErrUserNotFound
}
func (Store) ListUsersPage(context.Context, *store.TimeIDCursor, int) (*store.UserPage, error) {
	return &store.UserPage{}, nil
}
func (Store) UpdateUser(context.Context, string, store.UserUpdates) (*store.User, error) {
	return nil, nil
}
func (Store) DeleteUser(context.Context, string) error          { return nil }
func (Store) CountUsers(context.Context) (int64, error)         { return 0, nil }
func (Store) CountEnabledAdmins(context.Context) (int64, error) { return 0, nil }
func (Store) ReserveSuccessCall(context.Context, string, int) (store.SuccessQuotaReservation, error) {
	return store.SuccessQuotaReservation{}, nil
}
func (Store) ReleaseSuccessCall(context.Context, store.SuccessQuotaReservation) error { return nil }

func (Store) GetTierByID(context.Context, string) (*store.Tier, error) {
	return nil, store.ErrTierNotFound
}
func (Store) GetTiersByIDs(context.Context, []string) (map[string]*store.Tier, error) {
	return map[string]*store.Tier{}, nil
}
func (Store) ListTiersPage(context.Context, *store.TierCursor, int) (*store.TierPage, error) {
	return &store.TierPage{}, nil
}
func (Store) CreateTier(context.Context, string, int, int, int) (*store.Tier, error) {
	return nil, nil
}
func (Store) UpdateTier(context.Context, string, store.TierUpdates) (*store.Tier, error) {
	return nil, nil
}
func (Store) DeleteTier(context.Context, string) error                { return nil }
func (Store) CountUsersByTier(context.Context, string) (int64, error) { return 0, nil }

func (Store) CreateKey(context.Context, string, string, int) (*store.APIKey, string, error) {
	return nil, "", nil
}
func (Store) ConfigureAPIKeyEncryption(string) error                      { return nil }
func (Store) RevealKey(context.Context, string) (string, error)           { return "", nil }
func (Store) GetKeyByHash(context.Context, string) (*store.APIKey, error) { return nil, nil }
func (Store) ListKeysByUserPage(context.Context, string, *store.TimeIDCursor, int) (*store.APIKeyPage, error) {
	return &store.APIKeyPage{}, nil
}
func (Store) GetKeyByID(context.Context, string) (*store.APIKey, error) { return nil, nil }
func (Store) UpdateKey(context.Context, string, store.KeyUpdates) (*store.APIKey, error) {
	return nil, nil
}
func (Store) DeleteKey(context.Context, string) error              { return nil }
func (Store) RecordUsage(context.Context, store.UsageRecord) error { return nil }
func (Store) GetUsageStats(context.Context, string, time.Time) (*store.UsageStats, error) {
	return nil, nil
}
func (Store) GetUserUsageStatsPage(context.Context, string, time.Time, *store.UsageRecordCursor, int) (*store.UsageStats, error) {
	return nil, nil
}
func (Store) ListUsageRecordsPage(context.Context, store.UsageRecordListScope, time.Time, *store.UsageRecordCursor, int) (*store.UsageRecordPage, error) {
	return &store.UsageRecordPage{}, nil
}
func (Store) GetUsageRecordDetail(context.Context, int64, store.UsageRecordScope) (*store.UsageRecord, error) {
	return nil, nil
}

func (Store) GetServerSettings(context.Context) (*store.ServerSettings, error) { return nil, nil }
func (Store) UpsertServerSettings(context.Context, store.ServerSettings) (*store.ServerSettings, error) {
	return nil, nil
}

func (Store) ListInviteCodesPage(context.Context, *store.TimeIDCursor, int) (*store.InviteCodePage, error) {
	return &store.InviteCodePage{}, nil
}
func (Store) ListInviteCodeRedemptionsPage(context.Context, string, *store.TimeIDCursor, int) (*store.InviteCodeRedemptionPage, error) {
	return &store.InviteCodeRedemptionPage{}, nil
}
func (Store) CreateInviteCode(context.Context, string, int) (*store.InviteCode, string, error) {
	return nil, "", nil
}
func (Store) UpdateInviteCode(context.Context, string, store.InviteCodeUpdates) (*store.InviteCode, error) {
	return nil, nil
}
func (Store) DeleteInviteCode(context.Context, string) error { return nil }
