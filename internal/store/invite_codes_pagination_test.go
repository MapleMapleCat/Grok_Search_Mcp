package store

import (
	"context"
	"testing"
	"time"
)

func TestListInviteCodeRedemptionsPageUsesStableTimeIDCursor(t *testing.T) {
	sqliteStore := openTestDB(t)
	contextValue := context.Background()
	olderTimestamp := time.Date(2026, time.July, 20, 10, 0, 0, 0, time.UTC)
	newerTimestamp := olderTimestamp.Add(time.Minute)

	testRedemptions := []struct {
		identifier   string
		inviteCodeID string
		userID       string
		username     string
		redeemedAt   time.Time
	}{
		{identifier: "redemption-newest", inviteCodeID: "invite-one", userID: "user-newest", username: "newest", redeemedAt: newerTimestamp},
		{identifier: "redemption-b", inviteCodeID: "invite-one", userID: "user-b", username: "second", redeemedAt: olderTimestamp},
		{identifier: "redemption-a", inviteCodeID: "invite-one", userID: "user-a", username: "third", redeemedAt: olderTimestamp},
		{identifier: "other-invite", inviteCodeID: "invite-two", userID: "user-other", username: "other", redeemedAt: newerTimestamp},
	}
	for _, redemption := range testRedemptions {
		_, err := sqliteStore.db.ExecContext(
			contextValue,
			`INSERT INTO invite_code_redemptions (
				id, invite_code_id, invite_code_prefix, user_id, username, redeemed_at
			) VALUES (?, ?, ?, ?, ?, ?)`,
			redemption.identifier,
			redemption.inviteCodeID,
			"invite-prefix",
			redemption.userID,
			redemption.username,
			formatTime(redemption.redeemedAt),
		)
		if err != nil {
			t.Fatalf("insert redemption %q: %v", redemption.identifier, err)
		}
	}

	firstPage, err := sqliteStore.ListInviteCodeRedemptionsPage(contextValue, "invite-one", nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Redemptions) != 2 {
		t.Fatalf("first page count = %d, want 2", len(firstPage.Redemptions))
	}
	if firstPage.Redemptions[0].ID != "redemption-newest" || firstPage.Redemptions[1].ID != "redemption-b" {
		t.Fatalf("unexpected first page order: %q, %q", firstPage.Redemptions[0].ID, firstPage.Redemptions[1].ID)
	}
	if !firstPage.HasMore || firstPage.NextCursor == nil {
		t.Fatalf("first page pagination = has_more %t, cursor %+v", firstPage.HasMore, firstPage.NextCursor)
	}
	if firstPage.NextCursor.ID != "redemption-b" || !firstPage.NextCursor.Timestamp.Equal(olderTimestamp) {
		t.Fatalf("first page cursor = %+v, want redemption-b boundary", firstPage.NextCursor)
	}

	secondPage, err := sqliteStore.ListInviteCodeRedemptionsPage(
		contextValue,
		"invite-one",
		firstPage.NextCursor,
		2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage.Redemptions) != 1 || secondPage.Redemptions[0].ID != "redemption-a" {
		t.Fatalf("unexpected second page: %+v", secondPage.Redemptions)
	}
	if secondPage.HasMore || secondPage.NextCursor != nil {
		t.Fatalf("second page pagination = has_more %t, cursor %+v", secondPage.HasMore, secondPage.NextCursor)
	}
}
