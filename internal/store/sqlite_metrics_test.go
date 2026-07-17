package store

import (
	"context"
	"reflect"
	"testing"
)

func TestSQLiteMetricsAreDisabledByDefault(t *testing.T) {
	sqliteStore := openTestDB(t)
	userID := testUserID(t, sqliteStore)

	if err := sqliteStore.ReserveSuccessCall(context.Background(), userID, 1); err != nil {
		t.Fatal(err)
	}

	metrics := sqliteStore.SQLiteMetrics()
	if !reflect.DeepEqual(metrics, SQLiteMetricsSnapshot{}) {
		t.Fatalf("metrics collected while disabled: %+v", metrics)
	}
}

func TestSQLiteMetricsCollectOnlyWhileEnabled(t *testing.T) {
	sqliteStore := openTestDB(t)
	userID := testUserID(t, sqliteStore)
	requestContext := context.Background()

	sqliteStore.SetMetricsEnabled(true)
	if err := sqliteStore.ReserveSuccessCall(requestContext, userID, 1); err != nil {
		t.Fatal(err)
	}
	enabledMetrics := sqliteStore.SQLiteMetrics()
	if enabledMetrics.QuotaReserve.Attempts != 1 {
		t.Fatalf("quota reserve attempts = %d, want 1", enabledMetrics.QuotaReserve.Attempts)
	}
	if enabledMetrics.PrimaryWritePool.MaximumOpenConnections != 1 {
		t.Fatalf("primary write pool max connections = %d, want 1", enabledMetrics.PrimaryWritePool.MaximumOpenConnections)
	}

	sqliteStore.SetMetricsEnabled(false)
	if err := sqliteStore.ReleaseSuccessCall(requestContext, userID); err != nil {
		t.Fatal(err)
	}
	if metrics := sqliteStore.SQLiteMetrics(); !reflect.DeepEqual(metrics, SQLiteMetricsSnapshot{}) {
		t.Fatalf("disabled metrics snapshot was not empty: %+v", metrics)
	}

	sqliteStore.SetMetricsEnabled(true)
	reenabledMetrics := sqliteStore.SQLiteMetrics()
	if reenabledMetrics.QuotaRelease.Attempts != 0 {
		t.Fatalf("quota release was collected while disabled: %+v", reenabledMetrics.QuotaRelease)
	}
}
