package panel

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/grok-mcp/internal/auth"
	"github.com/grok-mcp/internal/config"
	"github.com/grok-mcp/internal/store"
)

func panelTestServer(t *testing.T) (*httptest.Server, *store.SQLiteStore, *config.Config) {
	t.Helper()
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := &config.Config{
		PanelKey:                "panel-secret",
		JWTSecret:               "jwt-secret",
		DefaultUserRPM:          60,
		DefaultUserTotalLimit:   0,
		DefaultUserSuccessLimit: 0,
	}
	h := &Handler{Store: st, Config: cfg}
	mux := NewMux(h)
	skip := map[string]struct{}{
		"/panel/v1/auth/register": {},
		"/panel/v1/auth/login":    {},
	}
	var chain http.Handler = mux
	chain = auth.AdminRoleMiddleware()(chain)
	chain = auth.JWTMiddleware(cfg.JWTSecret, st, skip)(chain)
	chain = auth.PanelKeyMiddleware(cfg.PanelKey)(chain)
	return httptest.NewServer(chain), st, cfg
}

func withPanel(req *http.Request, panelKey, jwt string) *http.Request {
	req.Header.Set("X-Panel-Key", panelKey)
	if jwt != "" {
		req.Header.Set("Authorization", "Bearer "+jwt)
	}
	return req
}

func TestRegisterFirstUserIsAdmin(t *testing.T) {
	ts, _, _ := panelTestServer(t)
	defer ts.Close()

	body := `{"username":"alice","password":"password123"}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/register", bytes.NewBufferString(body))
	req = withPanel(req, "panel-secret", "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register status %d", resp.StatusCode)
	}
	var u UserResponse
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		t.Fatal(err)
	}
	if u.Role != store.RoleAdmin {
		t.Fatalf("expected admin, got %s", u.Role)
	}
}

func TestRegisterRequiresPanelKey(t *testing.T) {
	ts, _, _ := panelTestServer(t)
	defer ts.Close()

	body := `{"username":"bob","password":"password123"}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/register", bytes.NewBufferString(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestLoginAndMe(t *testing.T) {
	ts, _, cfg := panelTestServer(t)
	defer ts.Close()

	reg := `{"username":"carol","password":"password123"}`
	r1, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/register", bytes.NewBufferString(reg))
	r1 = withPanel(r1, cfg.PanelKey, "")
	http.DefaultClient.Do(r1)

	login := `{"username":"carol","password":"password123"}`
	r2, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/login", bytes.NewBufferString(login))
	r2 = withPanel(r2, cfg.PanelKey, "")
	resp, err := http.DefaultClient.Do(r2)
	if err != nil {
		t.Fatal(err)
	}
	var lr LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	r3, _ := http.NewRequest(http.MethodGet, ts.URL+"/panel/v1/me", nil)
	r3 = withPanel(r3, cfg.PanelKey, lr.Token)
	resp3, err := http.DefaultClient.Do(r3)
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("me status %d", resp3.StatusCode)
	}
}

func TestSecondUserIsNotAdmin(t *testing.T) {
	ts, _, cfg := panelTestServer(t)
	defer ts.Close()

	for _, body := range []string{
		`{"username":"first","password":"password123"}`,
		`{"username":"second","password":"password123"}`,
	} {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/register", bytes.NewBufferString(body))
		req = withPanel(req, cfg.PanelKey, "")
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
	}

	login := `{"username":"second","password":"password123"}`
	r2, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/login", bytes.NewBufferString(login))
	r2 = withPanel(r2, cfg.PanelKey, "")
	resp, _ := http.DefaultClient.Do(r2)
	var lr LoginResponse
	json.NewDecoder(resp.Body).Decode(&lr)
	resp.Body.Close()
	if lr.User.Role != store.RoleUser {
		t.Fatalf("expected user role, got %s", lr.User.Role)
	}
}