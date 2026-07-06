package panel

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grok-mcp/internal/auth"
	"github.com/grok-mcp/internal/config"
	"github.com/grok-mcp/internal/store"
)

func panelTestServer(t *testing.T) (*httptest.Server, *store.SQLiteStore, *config.Config) {
	return panelTestServerWithAuthProtector(t, nil)
}

func panelTestServerWithAuthProtector(t *testing.T, authProtector *AuthProtector) (*httptest.Server, *store.SQLiteStore, *config.Config) {
	t.Helper()
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := &config.Config{
		JWTSecret:      "jwt-secret-must-be-at-least-32-bytes!",
		DefaultUserRPM: 60,
	}
	h := &Handler{Store: st, Config: cfg, AuthProtector: authProtector}
	mux := NewMux(h)
	skip := map[string]struct{}{
		"/panel/v1/auth/register": {},
		"/panel/v1/auth/login":    {},
	}
	var chain http.Handler = mux
	chain = auth.JWTMiddleware(cfg.JWTSecret, st, skip)(chain)
	return httptest.NewServer(chain), st, cfg
}

func withJWT(req *http.Request, jwt string) *http.Request {
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

func TestRegisterWithoutHeadersSucceeds(t *testing.T) {
	ts, _, _ := panelTestServer(t)
	defer ts.Close()

	body := `{"username":"bob","password":"password123"}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/register", bytes.NewBufferString(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 without headers, got %d", resp.StatusCode)
	}
}

func TestLoginAndMe(t *testing.T) {
	ts, _, _ := panelTestServer(t)
	defer ts.Close()

	reg := `{"username":"carol","password":"password123"}`
	r1, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/register", bytes.NewBufferString(reg))
	http.DefaultClient.Do(r1)

	login := `{"username":"carol","password":"password123"}`
	r2, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/login", bytes.NewBufferString(login))
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
	r3 = withJWT(r3, lr.Token)
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
	ts, _, _ := panelTestServer(t)
	defer ts.Close()

	for _, body := range []string{
		`{"username":"first","password":"password123"}`,
		`{"username":"second","password":"password123"}`,
	} {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/register", bytes.NewBufferString(body))
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
	}

	login := `{"username":"second","password":"password123"}`
	r2, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/login", bytes.NewBufferString(login))
	resp, _ := http.DefaultClient.Do(r2)
	var lr LoginResponse
	json.NewDecoder(resp.Body).Decode(&lr)
	resp.Body.Close()
	if lr.User.Role != store.RoleUser {
		t.Fatalf("expected user role, got %s", lr.User.Role)
	}
}

func TestRegisterRejectsOversizedPassword(t *testing.T) {
	ts, _, _ := panelTestServer(t)
	defer ts.Close()

	longPassword := strings.Repeat("a", maxPanelPasswordBytes+1)
	body := `{"username":"oversized","password":"` + longPassword + `"}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/register", bytes.NewBufferString(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversized password, got %d", resp.StatusCode)
	}
}

func TestAuthEndpointRateLimitByIP(t *testing.T) {
	authProtector := NewAuthProtector(AuthProtectorConfig{
		LoginIPRequestsPerMinute:    100,
		LoginIPBurst:                100,
		RegisterIPRequestsPerMinute: 1,
		RegisterIPBurst:             1,
	})
	ts, _, _ := panelTestServerWithAuthProtector(t, authProtector)
	defer ts.Close()

	body := `{"username":"ratelimited","password":"short"}`
	firstRequest, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/register", bytes.NewBufferString(body))
	firstResponse, err := http.DefaultClient.Do(firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	firstResponse.Body.Close()
	if firstResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected first request to reach handler validation, got %d", firstResponse.StatusCode)
	}

	secondRequest, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/register", bytes.NewBufferString(body))
	secondResponse, err := http.DefaultClient.Do(secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer secondResponse.Body.Close()
	if secondResponse.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after IP rate limit, got %d", secondResponse.StatusCode)
	}
	if secondResponse.Header.Get("Retry-After") == "" {
		t.Fatalf("expected Retry-After header on rate limited response")
	}
}

func TestLoginFailureLocksUsernameIPPair(t *testing.T) {
	authProtector := NewAuthProtector(AuthProtectorConfig{
		LoginIPRequestsPerMinute:    100,
		LoginIPBurst:                100,
		RegisterIPRequestsPerMinute: 100,
		RegisterIPBurst:             100,
		LoginFailureThreshold:       1,
		LoginBaseLockout:            time.Minute,
		LoginMaxLockout:             time.Minute,
	})
	ts, _, _ := panelTestServerWithAuthProtector(t, authProtector)
	defer ts.Close()

	registerBody := `{"username":"lockuser","password":"password123"}`
	registerRequest, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/register", bytes.NewBufferString(registerBody))
	registerResponse, err := http.DefaultClient.Do(registerRequest)
	if err != nil {
		t.Fatal(err)
	}
	registerResponse.Body.Close()
	if registerResponse.StatusCode != http.StatusCreated {
		t.Fatalf("expected registration before lockout test, got %d", registerResponse.StatusCode)
	}

	badLoginBody := `{"username":"lockuser","password":"wrongpass"}`
	badLoginRequest, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/login", bytes.NewBufferString(badLoginBody))
	badLoginResponse, err := http.DefaultClient.Do(badLoginRequest)
	if err != nil {
		t.Fatal(err)
	}
	badLoginResponse.Body.Close()
	if badLoginResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected first bad login to fail credentials, got %d", badLoginResponse.StatusCode)
	}

	goodLoginBody := `{"username":"lockuser","password":"password123"}`
	goodLoginRequest, _ := http.NewRequest(http.MethodPost, ts.URL+"/panel/v1/auth/login", bytes.NewBufferString(goodLoginBody))
	goodLoginResponse, err := http.DefaultClient.Do(goodLoginRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer goodLoginResponse.Body.Close()
	if goodLoginResponse.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected lockout to reject login before bcrypt, got %d", goodLoginResponse.StatusCode)
	}
	if goodLoginResponse.Header.Get("Retry-After") == "" {
		t.Fatalf("expected Retry-After header on lockout response")
	}
}
