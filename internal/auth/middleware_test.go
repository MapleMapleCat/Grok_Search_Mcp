package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grok-mcp/internal/keyhash"
	"github.com/grok-mcp/internal/store"
)

type memStore struct {
	store.TestStore
	byHash map[string]*store.APIKey
	users  map[string]*store.User
}

func (m *memStore) GetKeyByHash(_ context.Context, hash string) (*store.APIKey, error) {
	return m.byHash[hash], nil
}

func (m *memStore) GetUserByID(_ context.Context, id string) (*store.User, error) {
	if u, ok := m.users[id]; ok {
		return u, nil
	}
	return nil, store.ErrUserNotFound
}

func TestAPIKeyMiddleware(t *testing.T) {
	raw := "grok_testtoken"
	hash := keyhash.HashAPIKey(raw)
	st := &memStore{
		byHash: map[string]*store.APIKey{
			hash: {ID: "id-1", UserID: "u1", Enabled: true},
		},
		users: map[string]*store.User{
			"u1": {ID: "u1", Enabled: true},
		},
	}

	var gotID string
	h := APIKeyMiddleware(NewStoreAPIKeyResolver(st))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		k, ok := APIKeyFromContext(r.Context())
		if !ok {
			t.Fatal("missing key in context")
		}
		gotID = k.ID
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || gotID != "id-1" {
		t.Fatalf("code=%d id=%s", rec.Code, gotID)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec2.Code)
	}
}
