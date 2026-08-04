package usage

import (
	"context"
	"reflect"
	"testing"

	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/store"
)

type orderedQuotaCompleter struct {
	events *[]string
}

func (completer orderedQuotaCompleter) CompleteSuccessCall(context.Context, store.SuccessQuotaReservation, bool) error {
	*completer.events = append(*completer.events, "quota rollback")
	return nil
}

func TestQuotaRollbackReleasesSearchPermitFirst(t *testing.T) {
	events := make([]string, 0, 2)
	requestContext := WithSearchPermitRelease(context.Background(), func() {
		events = append(events, "search permit release")
	})

	reservation := store.SuccessQuotaReservation{ID: "reservation-1", UserID: "user-1", Period: "2026-01"}
	completeReservedSuccessCall(orderedQuotaCompleter{events: &events}, requestContext, reservation, false)

	expectedEvents := []string{"search permit release", "quota rollback"}
	if !reflect.DeepEqual(events, expectedEvents) {
		t.Fatalf("release order = %v, want %v", events, expectedEvents)
	}
}
