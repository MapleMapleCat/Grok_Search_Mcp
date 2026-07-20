package panel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/store"
)

type inviteRedemptionListStore struct {
	store.TestStore

	page                    *store.InviteCodeRedemptionPage
	requestedInviteCodeID   string
	requestedCursor         *store.TimeIDCursor
	requestedLimit          int
	redemptionListCallCount int
}

func (testStore *inviteRedemptionListStore) ListInviteCodeRedemptionsPage(
	_ context.Context,
	inviteCodeID string,
	cursor *store.TimeIDCursor,
	limit int,
) (*store.InviteCodeRedemptionPage, error) {
	testStore.redemptionListCallCount++
	testStore.requestedInviteCodeID = inviteCodeID
	testStore.requestedCursor = cursor
	testStore.requestedLimit = limit
	return testStore.page, nil
}

func TestAdminListInviteCodeRedemptionsPropagatesKeysetPagination(t *testing.T) {
	requestBoundary := time.Date(2026, time.July, 20, 10, 0, 0, 0, time.UTC)
	responseBoundary := requestBoundary.Add(-time.Minute)
	testStore := &inviteRedemptionListStore{
		page: &store.InviteCodeRedemptionPage{
			Redemptions: []*store.InviteCodeRedemption{
				{
					ID:               "redemption-one",
					InviteCodeID:     "invite-one",
					InviteCodePrefix: "invite-prefix",
					UserID:           "user-one",
					Username:         "registered-user",
					RedeemedAt:       responseBoundary,
				},
			},
			HasMore: true,
			NextCursor: &store.TimeIDCursor{
				Timestamp: responseBoundary,
				ID:        "redemption-one",
			},
		},
	}
	handler := &Handler{Store: testStore}
	requestCursor := encodeTimeIDCursor(cursorKindInviteRedemptions, &store.TimeIDCursor{
		Timestamp: requestBoundary,
		ID:        "request-boundary",
	})
	request := httptest.NewRequest(
		http.MethodGet,
		"/panel/v1/admin/invite-codes/invite-one/redemptions?limit=25&cursor="+requestCursor,
		nil,
	)
	request.SetPathValue("id", "invite-one")
	responseRecorder := httptest.NewRecorder()

	handler.adminListInviteCodeRedemptions(responseRecorder, request)

	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", responseRecorder.Code, http.StatusOK, responseRecorder.Body.String())
	}
	if testStore.redemptionListCallCount != 1 {
		t.Fatalf("store call count = %d, want 1", testStore.redemptionListCallCount)
	}
	if testStore.requestedInviteCodeID != "invite-one" || testStore.requestedLimit != 25 {
		t.Fatalf("store request = invite %q, limit %d", testStore.requestedInviteCodeID, testStore.requestedLimit)
	}
	if testStore.requestedCursor == nil ||
		testStore.requestedCursor.ID != "request-boundary" ||
		!testStore.requestedCursor.Timestamp.Equal(requestBoundary) {
		t.Fatalf("store cursor = %+v, want request boundary", testStore.requestedCursor)
	}

	var response InviteCodeRedemptionsResponse
	if err := json.NewDecoder(responseRecorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response.Redemptions) != 1 || response.Redemptions[0].ID != "redemption-one" {
		t.Fatalf("unexpected response redemptions: %+v", response.Redemptions)
	}
	expectedNextCursor := encodeTimeIDCursor(cursorKindInviteRedemptions, testStore.page.NextCursor)
	if !response.HasMore || response.NextCursor != expectedNextCursor {
		t.Fatalf("response pagination = has_more %t, cursor %q", response.HasMore, response.NextCursor)
	}
}

func TestAdminListInviteCodeRedemptionsRejectsCursorFromAnotherCollection(t *testing.T) {
	testStore := &inviteRedemptionListStore{page: &store.InviteCodeRedemptionPage{}}
	handler := &Handler{Store: testStore}
	wrongCursor := encodeTimeIDCursor(cursorKindInvites, &store.TimeIDCursor{
		Timestamp: time.Date(2026, time.July, 20, 10, 0, 0, 0, time.UTC),
		ID:        "invite-boundary",
	})
	request := httptest.NewRequest(
		http.MethodGet,
		"/panel/v1/admin/invite-codes/invite-one/redemptions?cursor="+wrongCursor,
		nil,
	)
	request.SetPathValue("id", "invite-one")
	responseRecorder := httptest.NewRecorder()

	handler.adminListInviteCodeRedemptions(responseRecorder, request)

	if responseRecorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", responseRecorder.Code, http.StatusBadRequest, responseRecorder.Body.String())
	}
	if testStore.redemptionListCallCount != 0 {
		t.Fatalf("store call count = %d, want 0", testStore.redemptionListCallCount)
	}
}
