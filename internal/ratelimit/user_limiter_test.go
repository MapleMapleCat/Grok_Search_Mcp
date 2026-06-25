package ratelimit

import (
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestUserLimiterRebuildsWhenRPMFallsBackToDefault(t *testing.T) {
	l := NewUserLimiter(60)
	defer l.Close()

	custom := 120
	if !l.allow("u1", custom) {
		t.Fatal("expected allow under custom rpm")
	}
	l.mu.Lock()
	entry := l.entries["u1"]
	customLimit := entry.limiter.Limit()
	l.mu.Unlock()
	if int(customLimit*60) != custom {
		t.Fatalf("custom limit want %d got %v", custom, customLimit)
	}

	if !l.allow("u1", 0) {
		t.Fatal("expected allow under default rpm")
	}
	l.mu.Lock()
	entry = l.entries["u1"]
	defaultLimit := entry.limiter.Limit()
	l.mu.Unlock()
	want := rate.Every(time.Minute / 60)
	if entry.limiter.Limit() != want || defaultLimit != want {
		t.Fatalf("expected default limiter %v got %v", want, entry.limiter.Limit())
	}
}
