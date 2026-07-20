package usage

import (
	"context"
	"reflect"
	"testing"

	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/store"
)

type orderedQuotaReleaser struct {
	events *[]string
}

func (releaser orderedQuotaReleaser) ReleaseSuccessCall(context.Context, store.SuccessQuotaReservation) error {
	*releaser.events = append(*releaser.events, "quota rollback")
	return nil
}

func TestQuotaRollbackReleasesSearchPermitFirst(t *testing.T) {
	events := make([]string, 0, 2)
	requestContext := WithSearchPermitRelease(context.Background(), func() {
		events = append(events, "search permit release")
	})

	reservation := store.SuccessQuotaReservation{UserID: "user-1", Period: "2026-01"}
	releaseReservedSuccessCall(orderedQuotaReleaser{events: &events}, requestContext, reservation)

	expectedEvents := []string{"search permit release", "quota rollback"}
	if !reflect.DeepEqual(events, expectedEvents) {
		t.Fatalf("release order = %v, want %v", events, expectedEvents)
	}
}
