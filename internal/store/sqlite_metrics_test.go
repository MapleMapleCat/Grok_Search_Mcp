package store

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestSQLiteMetricsAreDisabledByDefault(t *testing.T) {
	sqliteStore := openTestDB(t)
	userID := testUserID(t, sqliteStore)

	if _, err := sqliteStore.ReserveSuccessCall(context.Background(), userID, 1); err != nil {
		t.Fatal(err)
	}

	metrics := sqliteStore.SQLiteMetrics()
	if !reflect.DeepEqual(metrics, SQLiteMetricsSnapshot{}) {
		t.Fatalf("metrics collected while disabled: %+v", metrics)
	}
}

func TestSQLiteConnectionPoolMetricsExposeAllDatabaseStatsCounters(t *testing.T) {
	sqliteStore := openTestDB(t)
	sqliteStore.SetMetricsEnabled(true)

	serializedMetrics, err := json.Marshal(sqliteStore.SQLiteMetrics().PrimaryWritePool)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(serializedMetrics, &fields); err != nil {
		t.Fatal(err)
	}
	expectedFieldNames := []string{
		"maximum_open_connections",
		"open_connections",
		"in_use_connections",
		"idle_connections",
		"wait_count",
		"wait_duration_ms",
		"max_idle_closed",
		"max_idle_time_closed",
		"max_lifetime_closed",
	}
	for _, expectedFieldName := range expectedFieldNames {
		if _, exists := fields[expectedFieldName]; !exists {
			t.Fatalf("connection pool metric %q is missing from %s", expectedFieldName, serializedMetrics)
		}
		delete(fields, expectedFieldName)
	}
	if len(fields) != 0 {
		t.Fatalf("unexpected connection pool fields: %+v", fields)
	}
}

func TestSQLiteMetricsCollectOnlyWhileEnabled(t *testing.T) {
	sqliteStore := openTestDB(t)
	userID := testUserID(t, sqliteStore)
	requestContext := context.Background()

	sqliteStore.SetMetricsEnabled(true)
	reservation, err := sqliteStore.ReserveSuccessCall(requestContext, userID, 1)
	if err != nil {
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
	if err := sqliteStore.CompleteSuccessCall(requestContext, reservation, false); err != nil {
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

func TestSQLiteMetricsCountRejectedQuotaReleaseAsError(t *testing.T) {
	sqliteStore := openTestDB(t)
	sqliteStore.SetMetricsEnabled(true)

	invalidReservation := SuccessQuotaReservation{ID: "reservation-1", UserID: "user-1", Period: "January"}
	if err := sqliteStore.CompleteSuccessCall(context.Background(), invalidReservation, false); err == nil {
		t.Fatal("invalid reservation should be rejected")
	}

	metrics := sqliteStore.SQLiteMetrics()
	if metrics.QuotaRelease.Attempts != 1 || metrics.QuotaRelease.Errors != 1 {
		t.Fatalf("unexpected quota release metrics: %+v", metrics.QuotaRelease)
	}
}
