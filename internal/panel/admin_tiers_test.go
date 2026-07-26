package panel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/store"
	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/testsupport"
)

type adminTierStore struct {
	testsupport.Store

	tierPage       *store.TierPage
	defaultTier    *store.Tier
	createdDefault bool
	updatedTierID  string
	updatedFields  store.TierUpdates
}

func (testStore *adminTierStore) ListTiersPage(context.Context, *store.TierCursor, int) (*store.TierPage, error) {
	return testStore.tierPage, nil
}

func (testStore *adminTierStore) GetDefaultTier(context.Context) (*store.Tier, error) {
	if testStore.defaultTier == nil {
		return nil, store.ErrTierNotFound
	}
	return testStore.defaultTier, nil
}

func (testStore *adminTierStore) CountUsersByTier(_ context.Context, tierID string) (int64, error) {
	if testStore.defaultTier != nil && tierID == testStore.defaultTier.ID {
		return 7, nil
	}
	return 2, nil
}

func (testStore *adminTierStore) CountUsers(context.Context) (int64, error) {
	return 9, nil
}

func (testStore *adminTierStore) CreateTier(
	_ context.Context,
	name string,
	level int,
	rpm int,
	successLimit int,
	isDefault bool,
) (*store.Tier, error) {
	testStore.createdDefault = isDefault
	return &store.Tier{
		ID:           "created-tier",
		Name:         name,
		Level:        level,
		RPM:          rpm,
		SuccessLimit: successLimit,
		IsDefault:    isDefault,
	}, nil
}

func (testStore *adminTierStore) UpdateTier(_ context.Context, tierID string, updates store.TierUpdates) (*store.Tier, error) {
	testStore.updatedTierID = tierID
	testStore.updatedFields = updates
	return &store.Tier{ID: tierID, Name: "updated", IsDefault: updates.IsDefault != nil && *updates.IsDefault}, nil
}

func TestAdminListTiersReturnsExplicitDefaultOutsideCurrentPage(t *testing.T) {
	visibleTier := &store.Tier{ID: "visible-tier", Name: "Visible", Level: 1}
	defaultTier := &store.Tier{ID: "default-tier", Name: "Default", Level: 99, IsDefault: true}
	testStore := &adminTierStore{
		tierPage: &store.TierPage{
			Tiers:      []*store.Tier{visibleTier},
			TotalCount: 2,
		},
		defaultTier: defaultTier,
	}
	handler := &Handler{Store: testStore}
	request := httptest.NewRequest(http.MethodGet, "/panel/v1/admin/tiers?limit=1", nil)
	responseRecorder := httptest.NewRecorder()

	handler.adminListTiers(responseRecorder, request)

	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("admin list tiers returned status %d: %s", responseRecorder.Code, responseRecorder.Body.String())
	}
	var response TiersResponse
	if err := json.NewDecoder(responseRecorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.DefaultTier == nil || response.DefaultTier.ID != defaultTier.ID || !response.DefaultTier.IsDefault {
		t.Fatalf("default tier response = %+v, want %+v", response.DefaultTier, defaultTier)
	}
	if response.DefaultTier.UserCount != 7 || response.AssignedUserCount != 9 {
		t.Fatalf("unexpected tier counts: default=%d assigned=%d", response.DefaultTier.UserCount, response.AssignedUserCount)
	}
}

func TestAdminTierMutationsForwardExplicitDefaultSelection(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		testStore := &adminTierStore{}
		handler := &Handler{Store: testStore}
		request := httptest.NewRequest(
			http.MethodPost,
			"/panel/v1/admin/tiers",
			strings.NewReader(`{"name":"Starter","level":1,"rpm":10,"success_limit":100,"is_default":true}`),
		)
		responseRecorder := httptest.NewRecorder()

		handler.adminCreateTier(responseRecorder, request)

		if responseRecorder.Code != http.StatusCreated {
			t.Fatalf("admin create tier returned status %d: %s", responseRecorder.Code, responseRecorder.Body.String())
		}
		if !testStore.createdDefault {
			t.Fatal("create request did not forward is_default=true")
		}
	})

	t.Run("update", func(t *testing.T) {
		testStore := &adminTierStore{}
		handler := &Handler{Store: testStore}
		request := httptest.NewRequest(
			http.MethodPatch,
			"/panel/v1/admin/tiers/tier-to-promote",
			strings.NewReader(`{"is_default":true}`),
		)
		request.SetPathValue("id", "tier-to-promote")
		responseRecorder := httptest.NewRecorder()

		handler.adminUpdateTier(responseRecorder, request)

		if responseRecorder.Code != http.StatusOK {
			t.Fatalf("admin update tier returned status %d: %s", responseRecorder.Code, responseRecorder.Body.String())
		}
		if testStore.updatedTierID != "tier-to-promote" || testStore.updatedFields.IsDefault == nil || !*testStore.updatedFields.IsDefault {
			t.Fatalf("update request did not forward default selection: id=%q updates=%+v", testStore.updatedTierID, testStore.updatedFields)
		}
	})
}
