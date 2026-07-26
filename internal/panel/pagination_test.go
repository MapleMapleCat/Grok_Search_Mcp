package panel

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/store"
)

func TestTierCursorRoundTripsCreationBoundary(t *testing.T) {
	createdAt := time.Date(2026, time.July, 26, 12, 34, 56, 0, time.UTC)
	encodedCursor := encodeTierCursor(&store.TierCursor{
		CreatedAt: createdAt,
		ID:        "tier-boundary",
	})
	request := httptest.NewRequest("GET", "/panel/v1/admin/tiers?cursor="+encodedCursor, nil)

	decodedCursor, err := parseTierCursor(request)
	if err != nil {
		t.Fatal(err)
	}
	if decodedCursor.ID != "tier-boundary" || !decodedCursor.CreatedAt.Equal(createdAt) {
		t.Fatalf("decoded tier cursor = %+v, want created_at=%s id=tier-boundary", decodedCursor, createdAt)
	}
}
