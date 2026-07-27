package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/auth"
	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/store"
	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/testsupport"
)

type inviteRedemptionListStore struct {
	testsupport.Store

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

func TestInviteCodeHTTPResponsesRevealRawCodeOnlyOnCreateAndExplicitReveal(t *testing.T) {
	sqliteStore, err := store.OpenSQLite(filepath.Join(t.TempDir(), "invite-responses.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqliteStore.Close()
	if err := sqliteStore.ConfigureAPIKeyEncryption("invite-response-encryption-secret-at-least-32-bytes"); err != nil {
		t.Fatal(err)
	}

	administrator, err := sqliteStore.CreateUser(t.Context(), "invite-response-admin", "hash", store.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	handler := &Handler{Store: sqliteStore}
	authenticatedAdministrator := &auth.AuthenticatedUser{User: *administrator}

	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/panel/v1/admin/invite-codes",
		bytes.NewBufferString(`{"registration_limit":3}`),
	)
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest = createRequest.WithContext(auth.WithUser(createRequest.Context(), authenticatedAdministrator))
	createResponseRecorder := httptest.NewRecorder()
	handler.adminCreateInviteCode(createResponseRecorder, createRequest)

	if createResponseRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d: %s", createResponseRecorder.Code, http.StatusCreated, createResponseRecorder.Body.String())
	}
	createResponseBody := createResponseRecorder.Body.String()
	var createResponse CreateInviteCodeResponse
	if err := json.Unmarshal([]byte(createResponseBody), &createResponse); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(createResponse.Code) == "" {
		t.Fatal("create response did not include the raw invite code")
	}
	if strings.Count(createResponseBody, createResponse.Code) != 1 {
		t.Fatalf("raw invite code appeared %d times in create response, want exactly once: %s", strings.Count(createResponseBody, createResponse.Code), createResponseBody)
	}
	assertJSONFieldAbsent(t, []byte(createResponseBody), []string{"invite_code", "code"})

	listRequest := httptest.NewRequest(http.MethodGet, "/panel/v1/admin/invite-codes", nil)
	listResponseRecorder := httptest.NewRecorder()
	handler.adminListInviteCodes(listResponseRecorder, listRequest)

	if listResponseRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d: %s", listResponseRecorder.Code, http.StatusOK, listResponseRecorder.Body.String())
	}
	assertRawInviteAbsent(t, listResponseRecorder.Body.Bytes(), createResponse.Code)
	assertJSONFieldAbsent(t, listResponseRecorder.Body.Bytes(), []string{"invite_codes", "0", "code"})

	updateRequest := httptest.NewRequest(
		http.MethodPatch,
		"/panel/v1/admin/invite-codes/"+createResponse.InviteCode.ID,
		bytes.NewBufferString(`{"enabled":false}`),
	)
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.SetPathValue("id", createResponse.InviteCode.ID)
	updateResponseRecorder := httptest.NewRecorder()
	handler.adminUpdateInviteCode(updateResponseRecorder, updateRequest)

	if updateResponseRecorder.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d: %s", updateResponseRecorder.Code, http.StatusOK, updateResponseRecorder.Body.String())
	}
	assertRawInviteAbsent(t, updateResponseRecorder.Body.Bytes(), createResponse.Code)
	assertJSONFieldAbsent(t, updateResponseRecorder.Body.Bytes(), []string{"code"})

	revealRequest := httptest.NewRequest(
		http.MethodPost,
		"/panel/v1/admin/invite-codes/"+createResponse.InviteCode.ID+"/reveal",
		nil,
	)
	revealRequest.SetPathValue("id", createResponse.InviteCode.ID)
	revealResponseRecorder := httptest.NewRecorder()
	handler.adminRevealInviteCode(revealResponseRecorder, revealRequest)

	if revealResponseRecorder.Code != http.StatusOK {
		t.Fatalf("reveal status = %d, want %d: %s", revealResponseRecorder.Code, http.StatusOK, revealResponseRecorder.Body.String())
	}
	if revealResponseRecorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("reveal Cache-Control = %q, want no-store", revealResponseRecorder.Header().Get("Cache-Control"))
	}
	var revealResponse RevealInviteCodeResponse
	if err := json.Unmarshal(revealResponseRecorder.Body.Bytes(), &revealResponse); err != nil {
		t.Fatal(err)
	}
	if revealResponse.Code != createResponse.Code {
		t.Fatalf("revealed invite code = %q, want created code", revealResponse.Code)
	}
}

type inviteCodeRevealStore struct {
	testsupport.Store
	rawInviteCode string
	err           error
}

func (testStore inviteCodeRevealStore) RevealInviteCode(context.Context, string) (string, error) {
	return testStore.rawInviteCode, testStore.err
}

func TestAdminRevealInviteCodeMapsUnavailableAndMissingSecrets(t *testing.T) {
	testCases := []struct {
		name               string
		storeError         error
		expectedStatusCode int
		expectedError      string
	}{
		{
			name:               "missing invite",
			storeError:         store.ErrInviteCodeNotFound,
			expectedStatusCode: http.StatusNotFound,
			expectedError:      "invite code not found",
		},
		{
			name:               "legacy hash-only invite",
			storeError:         store.ErrInviteCodeSecretUnavailable,
			expectedStatusCode: http.StatusConflict,
			expectedError:      "invite code secret is unavailable",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			handler := &Handler{Store: inviteCodeRevealStore{err: testCase.storeError}}
			request := httptest.NewRequest(http.MethodPost, "/panel/v1/admin/invite-codes/invite-one/reveal", nil)
			request.SetPathValue("id", "invite-one")
			responseRecorder := httptest.NewRecorder()

			handler.adminRevealInviteCode(responseRecorder, request)

			if responseRecorder.Code != testCase.expectedStatusCode {
				t.Fatalf("status = %d, want %d: %s", responseRecorder.Code, testCase.expectedStatusCode, responseRecorder.Body.String())
			}
			var errorResponse map[string]any
			if err := json.Unmarshal(responseRecorder.Body.Bytes(), &errorResponse); err != nil {
				t.Fatal(err)
			}
			if errorResponse["error"] != testCase.expectedError {
				t.Fatalf("error = %v, want %q", errorResponse["error"], testCase.expectedError)
			}
		})
	}
}

func assertRawInviteAbsent(t *testing.T, responseBody []byte, rawInviteCode string) {
	t.Helper()
	if bytes.Contains(responseBody, []byte(rawInviteCode)) {
		t.Fatalf("response disclosed raw invite code %q: %s", rawInviteCode, responseBody)
	}
}

func assertJSONFieldAbsent(t *testing.T, responseBody []byte, path []string) {
	t.Helper()
	var currentValue any
	if err := json.Unmarshal(responseBody, &currentValue); err != nil {
		t.Fatal(err)
	}
	for pathIndex, pathElement := range path {
		switch typedValue := currentValue.(type) {
		case map[string]any:
			nextValue, exists := typedValue[pathElement]
			if !exists {
				return
			}
			if pathIndex == len(path)-1 {
				t.Fatalf("response unexpectedly contains JSON field path %s: %s", strings.Join(path, "."), responseBody)
			}
			currentValue = nextValue
		case []any:
			if pathElement != "0" || len(typedValue) == 0 {
				return
			}
			currentValue = typedValue[0]
		default:
			return
		}
	}
}
