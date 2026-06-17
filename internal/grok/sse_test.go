package grok

import (
	"strings"
	"testing"
)

func TestForEachSSEEventMultiLineData(t *testing.T) {
	input := "data: line1\n" +
		"data: line2\n" +
		"\n" +
		"data: {\"type\":\"done\"}\n" +
		"\n"

	var events []string
	err := forEachSSEEvent(strings.NewReader(input), func(payload string) error {
		events = append(events, payload)
		return nil
	})
	if err != nil {
		t.Fatalf("forEachSSEEvent failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d (%v)", len(events), events)
	}
	if events[0] != "line1\nline2" {
		t.Fatalf("unexpected multi-line payload: %q", events[0])
	}
	if events[1] != `{"type":"done"}` {
		t.Fatalf("unexpected second payload: %q", events[1])
	}
}

func TestForEachSSEEventDoneMarker(t *testing.T) {
	input := "data: [DONE]\n\n" +
		"data: {\"type\":\"after\"}\n" +
		"\n"

	var events []string
	err := forEachSSEEvent(strings.NewReader(input), func(payload string) error {
		events = append(events, payload)
		return nil
	})
	if err != nil {
		t.Fatalf("forEachSSEEvent failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event after [DONE], got %d (%v)", len(events), events)
	}
	if events[0] != `{"type":"after"}` {
		t.Fatalf("unexpected payload: %q", events[0])
	}
}

func TestForEachSSEEventTrailingDataWithoutBlankLine(t *testing.T) {
	input := "data: trailing\n"

	var events []string
	err := forEachSSEEvent(strings.NewReader(input), func(payload string) error {
		events = append(events, payload)
		return nil
	})
	if err != nil {
		t.Fatalf("forEachSSEEvent failed: %v", err)
	}
	if len(events) != 1 || events[0] != "trailing" {
		t.Fatalf("expected trailing flush, got %v", events)
	}
}