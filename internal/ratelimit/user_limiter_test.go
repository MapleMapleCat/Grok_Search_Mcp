package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grok-mcp/internal/auth"
	"github.com/grok-mcp/internal/store"
	"golang.org/x/time/rate"
)

func TestUserMiddlewareRejectsNegativeRPM(t *testing.T) {
	l := NewUserLimiter(60)
	defer l.Close()

	var called bool
	h := l.UserMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	user := &store.User{ID: "u1", RPM: -1}
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req = req.WithContext(auth.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if called {
		t.Fatal("next handler must not run for negative RPM")
	}
}

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
