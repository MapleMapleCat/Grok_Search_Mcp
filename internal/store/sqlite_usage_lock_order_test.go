package store

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryUsageStatsReleasesPrimaryConnectionBeforeLoadingDebug(t *testing.T) {
	testContext, cancelTest := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelTest()

	sqliteStore, err := OpenSQLite(":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	if err := sqliteStore.ConfigureAPIKeyEncryption("test-api-key-encryption-secret-at-least-32-bytes"); err != nil {
		t.Fatalf("ConfigureAPIKeyEncryption: %v", err)
	}

	user, err := sqliteStore.CreateUser(testContext, "usage-lock-order-user", "hash", RoleUser)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	apiKey, _, err := sqliteStore.CreateKey(testContext, user.ID, "usage-lock-order-key", 20)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	initialUsageTimestamp := time.Now().UTC().Truncate(time.Second)
	if err := sqliteStore.RecordUsage(testContext, UsageRecord{
		KeyID:     apiKey.ID,
		ToolName:  "grok_web_search",
		Timestamp: initialUsageTimestamp,
		Success:   true,
		DebugJSON: `{"phase":"initial"}`,
	}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}

	// Keep debugDB's only connection inside an active sidecar write. Stats must
	// finish and commit its primary read transaction before waiting for it.
	heldDebugWrite, err := sqliteStore.debugDB.BeginTx(testContext, nil)
	if err != nil {
		t.Fatalf("begin held debug write: %v", err)
	}
	heldDebugWriteReleased := false
	t.Cleanup(func() {
		if !heldDebugWriteReleased {
			_ = heldDebugWrite.Rollback()
		}
	})
	if _, err := heldDebugWrite.ExecContext(testContext,
		`UPDATE usage_debug SET created_at = created_at`); err != nil {
		t.Fatalf("hold debug sidecar write: %v", err)
	}

	type usageStatsResult struct {
		stats *UsageStats
		err   error
	}
	initialDebugWaitCount := sqliteStore.debugDB.Stats().WaitCount
	statsResultChannel := make(chan usageStatsResult, 1)
	go func() {
		stats, statsErr := sqliteStore.GetUsageStats(testContext, apiKey.ID, time.Time{})
		statsResultChannel <- usageStatsResult{stats: stats, err: statsErr}
	}()

	waitPollTicker := time.NewTicker(time.Millisecond)
	defer waitPollTicker.Stop()
	for sqliteStore.debugDB.Stats().WaitCount == initialDebugWaitCount {
		select {
		case statsResult := <-statsResultChannel:
			t.Fatalf("GetUsageStats returned before waiting for debugDB: %v", statsResult.err)
		case <-testContext.Done():
			t.Fatalf("GetUsageStats did not wait for the held debugDB connection: %v", testContext.Err())
		case <-waitPollTicker.C:
		}
	}

	select {
	case statsResult := <-statsResultChannel:
		t.Fatalf("GetUsageStats returned while the debug sidecar write was held: %v", statsResult.err)
	default:
	}

	primaryWriteContext, cancelPrimaryWrite := context.WithTimeout(testContext, time.Second)
	defer cancelPrimaryWrite()
	if err := sqliteStore.RecordUsage(primaryWriteContext, UsageRecord{
		KeyID:     apiKey.ID,
		ToolName:  "grok_x_search",
		Timestamp: initialUsageTimestamp.Add(time.Second),
		Success:   true,
	}); err != nil {
		t.Fatalf("primary usage write waited behind stats while stats waited for debugDB: %v", err)
	}

	select {
	case statsResult := <-statsResultChannel:
		t.Fatalf("GetUsageStats returned before releasing the debug sidecar write: %v", statsResult.err)
	default:
	}

	if err := heldDebugWrite.Commit(); err != nil {
		t.Fatalf("release debug sidecar write: %v", err)
	}
	heldDebugWriteReleased = true

	select {
	case statsResult := <-statsResultChannel:
		if statsResult.err != nil {
			t.Fatalf("GetUsageStats after releasing debugDB: %v", statsResult.err)
		}
		if statsResult.stats.TotalCalls != 1 {
			t.Fatalf("stats total calls = %d, want snapshot total 1", statsResult.stats.TotalCalls)
		}
		if len(statsResult.stats.Records) != 1 {
			t.Fatalf("stats records = %d, want snapshot record count 1", len(statsResult.stats.Records))
		}
		if statsResult.stats.Records[0].DebugJSON != `{"phase":"initial"}` {
			t.Fatalf("stats debug JSON = %q, want initial debug summary", statsResult.stats.Records[0].DebugJSON)
		}
	case <-testContext.Done():
		t.Fatalf("GetUsageStats did not finish after releasing debugDB: %v", testContext.Err())
	}
}
