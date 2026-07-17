package usage

import (
	"context"
	"reflect"
	"testing"
)

type orderedQuotaReleaser struct {
	events *[]string
}

func (releaser orderedQuotaReleaser) ReleaseSuccessCall(context.Context, string) error {
	*releaser.events = append(*releaser.events, "quota rollback")
	return nil
}

func TestQuotaRollbackReleasesSearchPermitFirst(t *testing.T) {
	events := make([]string, 0, 2)
	requestContext := WithSearchPermitRelease(context.Background(), func() {
		events = append(events, "search permit release")
	})

	releaseReservedSuccessCall(orderedQuotaReleaser{events: &events}, requestContext, "user-1")

	expectedEvents := []string{"search permit release", "quota rollback"}
	if !reflect.DeepEqual(events, expectedEvents) {
		t.Fatalf("release order = %v, want %v", events, expectedEvents)
	}
}
