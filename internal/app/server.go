// Package app is the HTTP composition root for grok-mcp.
//
// It wires store, auth, rate limits, quota/usage middleware, panel API, and the
// Streamable HTTP MCP endpoint. cmd/grok-mcp stays a thin entrypoint.
package app

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/grok-mcp/internal/auth"
	"github.com/grok-mcp/internal/config"
	"github.com/grok-mcp/internal/panel"
	"github.com/grok-mcp/internal/panelui"
	"github.com/grok-mcp/internal/quota"
	"github.com/grok-mcp/internal/ratelimit"
	"github.com/grok-mcp/internal/store"
	"github.com/grok-mcp/internal/usage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/crypto/bcrypt"
)

const bootstrapAdminUsername = "admin"

var contentSecurityPolicy = strings.Join([]string{
	"default-src 'self'",
	"script-src 'self'",
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
	"font-src 'self' https://fonts.gstatic.com data:",
	"img-src 'self' data: blob: https:",
	"connect-src 'self'",
	"base-uri 'none'",
	"frame-ancestors 'none'",
	"form-action 'self'",
}, "; ")

// BootstrapAdminCredentials holds the one-time bootstrap admin password printed at startup.
type BootstrapAdminCredentials struct {
	Username string
	Password string
}

// Run starts the HTTP server (MCP + panel) and blocks until ctx is cancelled or ListenAndServe fails.
func Run(ctx context.Context, cfg *config.Config, server *mcp.Server, settingsApplier panel.ServerSettingsApplier) error {
	st, err := store.OpenSQLite(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	if err := InitializeServerSettings(ctx, st, cfg, settingsApplier); err != nil {
		return fmt.Errorf("initialize server settings: %w", err)
	}

	bootstrapCredentials, err := EnsureBootstrapAdmin(ctx, st)
	if err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}
	if bootstrapCredentials != nil {
		log.Printf("BOOTSTRAP ADMIN CREATED username=%s password=%s", bootstrapCredentials.Username, bootstrapCredentials.Password)
	}

	usageWriter := store.NewAsyncUsageWriter(st, 256)
	defer usageWriter.Close()

	userLim := ratelimit.NewUserLimiter(cfg.DefaultUserRPM)
	defer userLim.Close()
	mcpIPLimiter := ratelimit.NewIPLimiter(cfg.MCPIPRPM)
	mcpIPLimiter.SetTrustedProxies(cfg.TrustedProxies)
	defer mcpIPLimiter.Close()

	authResolver := auth.NewCachedAPIKeyResolver(st, 30*time.Second)

	mcpHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{Stateless: true})

	// MCP 中间件由内到外包装；请求实际顺序为：
	// MaxBody → IP RPM → API Key → ExtractToolName → User RPM(tools/call) → Quota → Usage → MCP handler
	// MaxBody 必须在 Extract/Usage 读 Body 之前生效，避免中间件路径绕过体积上限。
	var mcpChain http.Handler = mcpHandler
	mcpChain = usage.MCPMiddleware(st, usageWriter)(mcpChain)
	mcpChain = quota.MCPMiddleware(st)(mcpChain)
	mcpChain = userLim.UserMiddleware()(mcpChain)
	mcpChain = usage.ExtractToolNameMiddleware()(mcpChain)
	mcpChain = auth.APIKeyMiddleware(authResolver)(mcpChain)
	mcpChain = mcpIPLimiter.Middleware()(mcpChain)
	mcpChain = panel.MaxBodyMiddleware(panel.MaxPanelBodyBytes())(mcpChain)

	rootMux := http.NewServeMux()
	rootMux.Handle("/mcp/", mcpChain)
	rootMux.Handle("/mcp", mcpChain)

	panelHandler := &panel.Handler{Store: st, Config: cfg, SettingsApplier: settingsApplier, AuthCache: authResolver}
	if modelLister, ok := settingsApplier.(panel.ModelLister); ok {
		panelHandler.ModelLister = modelLister
	}
	panelMux := panel.NewMux(panelHandler)
	jwtSkip := map[string]struct{}{
		"/panel/v1/auth/register": {},
		"/panel/v1/auth/login":    {},
	}
	var panelChain http.Handler = panelMux
	panelChain = panel.MaxBodyMiddleware(panel.MaxPanelBodyBytes())(panelChain)
	panelChain = auth.JWTMiddleware(cfg.JWTSecret, st, jwtSkip)(panelChain)
	rootMux.Handle("/panel/v1/", panelChain)
	rootMux.Handle("/panel/v1", panelChain)

	panelUI := panelui.Handler()
	rootMux.Handle("/panel/", panelUI)
	rootMux.Handle("/panel", panelUI)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           SecurityHeadersMiddleware(rootMux),
		ReadHeaderTimeout: 10 * time.Second,
		// MaxBytesReader only caps request size. ReadTimeout also bounds how long a
		// client may take to send the body after headers, mitigating slow-body DoS.
		ReadTimeout: 30 * time.Second,
		// SSE 流式响应（/mcp tools/call）是长连接，WriteTimeout 不能短于上游超时；
		// 设为略大于 cfg.Timeout 兜底，避免在合法的长时间搜索中被中断。
		WriteTimeout: cfg.Timeout + 30*time.Second,
		IdleTimeout:  120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if isWildcardHTTPAddr(cfg.HTTPAddr) {
			log.Printf("WARNING: grok-mcp is listening on %s without built-in TLS; use an HTTPS reverse proxy before exposing it publicly", cfg.HTTPAddr)
		}
		log.Printf("grok-mcp HTTP listening on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// InitializeServerSettings loads DB settings (or env defaults), normalizes, persists, and applies them.
func InitializeServerSettings(ctx context.Context, st store.Store, cfg *config.Config, settingsApplier panel.ServerSettingsApplier) error {
	storedSettings, err := st.GetServerSettings(ctx)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}

	settings := cfg.ServerSettings()
	if storedSettings != nil {
		settings = config.ServerSettingsFromStore(storedSettings)
	}

	normalizedSettings, err := config.NormalizeServerSettings(settings)
	if err != nil {
		return err
	}
	if _, err := st.UpsertServerSettings(ctx, config.StoreServerSettings(normalizedSettings)); err != nil {
		return fmt.Errorf("persist settings: %w", err)
	}

	cfg.ApplyServerSettings(normalizedSettings)
	if settingsApplier != nil {
		if err := settingsApplier.ApplyServerSettings(normalizedSettings); err != nil {
			return fmt.Errorf("apply settings: %w", err)
		}
	}
	return nil
}

// EnsureBootstrapAdmin creates a default admin when no enabled admin exists.
func EnsureBootstrapAdmin(ctx context.Context, st store.Store) (*BootstrapAdminCredentials, error) {
	enabledAdminCount, err := st.CountEnabledAdmins(ctx)
	if err != nil {
		return nil, fmt.Errorf("count enabled admins: %w", err)
	}
	if enabledAdminCount > 0 {
		return nil, nil
	}

	password, err := randomBootstrapPassword(12)
	if err != nil {
		return nil, fmt.Errorf("generate password: %w", err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	if _, err := st.CreateUser(ctx, bootstrapAdminUsername, string(passwordHash), store.RoleAdmin); err != nil {
		if errors.Is(err, store.ErrUsernameTaken) {
			return promoteExistingBootstrapAdmin(ctx, st, string(passwordHash), password)
		}
		return nil, fmt.Errorf("create admin user: %w", err)
	}

	return &BootstrapAdminCredentials{Username: bootstrapAdminUsername, Password: password}, nil
}

// promoteExistingBootstrapAdmin 在 "admin" 用户名已被普通用户占用时，
// 将其提升为启用状态的管理员并重置密码，返回新的凭证。
func promoteExistingBootstrapAdmin(ctx context.Context, st store.Store, passwordHash, password string) (*BootstrapAdminCredentials, error) {
	existingUser, err := st.GetUserByUsername(ctx, bootstrapAdminUsername)
	if err != nil {
		return nil, fmt.Errorf("lookup existing admin user: %w", err)
	}
	if existingUser == nil {
		return nil, fmt.Errorf("username taken but user not found")
	}
	enabled := true
	adminRole := store.RoleAdmin
	revokeTokens := true
	if _, err := st.UpdateUser(ctx, existingUser.ID, store.UserUpdates{
		Enabled:      &enabled,
		Role:         &adminRole,
		PasswordHash: &passwordHash,
		RevokeTokens: &revokeTokens,
	}); err != nil {
		return nil, fmt.Errorf("promote existing admin: %w", err)
	}
	return &BootstrapAdminCredentials{Username: bootstrapAdminUsername, Password: password}, nil
}

func randomBootstrapPassword(length int) (string, error) {
	const passwordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	if length <= 0 {
		return "", fmt.Errorf("password length must be positive")
	}

	passwordBytes := make([]byte, length)
	maxIndex := big.NewInt(int64(len(passwordAlphabet)))
	for index := range passwordBytes {
		randomIndex, err := rand.Int(rand.Reader, maxIndex)
		if err != nil {
			return "", err
		}
		passwordBytes[index] = passwordAlphabet[randomIndex.Int64()]
	}
	return string(passwordBytes), nil
}

// SecurityHeadersMiddleware attaches baseline browser security headers.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		if isSensitiveHTTPPath(r.URL.Path) {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func isSensitiveHTTPPath(requestPath string) bool {
	return requestPath == "/mcp" || strings.HasPrefix(requestPath, "/mcp/") || requestPath == "/panel/v1" || strings.HasPrefix(requestPath, "/panel/v1/")
}

func isWildcardHTTPAddr(httpAddr string) bool {
	trimmedAddr := strings.TrimSpace(httpAddr)
	if trimmedAddr == "" || strings.HasPrefix(trimmedAddr, ":") {
		return true
	}
	host, _, err := net.SplitHostPort(trimmedAddr)
	if err != nil {
		return false
	}
	return host == "" || host == "0.0.0.0" || host == "::" || host == "[::]"
}

// BootstrapAdminUsername is the reserved username created when no enabled admin exists.
func BootstrapAdminUsername() string {
	return bootstrapAdminUsername
}
