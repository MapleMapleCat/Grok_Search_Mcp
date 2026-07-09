// Package config 从环境变量加载 grok-mcp 的运行时配置并做基本校验。
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL  = "http://127.0.0.1:8317"
	defaultModel    = "grok-4.3"
	defaultTimeout  = 120 * time.Second
	defaultHTTPAddr = ":8080"
	defaultDBPath   = "./grok-mcp.db"
	// defaultMCPIPRPM 在 API key 鉴权前按来源 IP 限制 /mcp 请求，保护认证存储免受暴力探测和 DoS。
	defaultMCPIPRPM = 300
	// defaultLimiterRPM 是内置内存限流器的兜底 RPM；限额实际取值始终来自 tier。
	defaultLimiterRPM = 60
)

// Config 保存进程启动所需的全部配置项。
//
// 用户限额（RPM / success limit）不再可配置，统一由 tier 决定；
// DefaultUserRPM 仅作为内存限流器在 tier 解析异常时的兜底，不再用于新用户。
type Config struct {
	CPABaseURL     string
	CPAAPIKey      string
	Model          string
	Timeout        time.Duration
	Debug          bool
	HTTPAddr       string
	DBPath         string
	JWTSecret      string
	DefaultUserRPM int
	MCPIPRPM       int
	// TrustedProxies 为可信反向代理 CIDR；仅当 RemoteAddr 命中时才解析 X-Forwarded-For / X-Real-IP。
	// 空表示永不信任转发头（公网直连安全默认）。
	TrustedProxies []*net.IPNet
	ProxyURL       string
	ProxyEnabled   bool
}

// ServerSettings contains the runtime-tunable upstream settings exposed in the
// admin panel. It intentionally excludes listener address, database path, and
// JWT secret because changing those safely requires a process restart.
type ServerSettings struct {
	CPABaseURL     string
	CPAAPIKey      string
	Model          string
	TimeoutSeconds int
	ProxyURL       string
	ProxyEnabled   bool
	Debug          bool
}

// Load 读取并校验配置。
func Load() (*Config, error) {
	proxyURL := strings.TrimSpace(os.Getenv("GROK_PROXY_URL"))
	cfg := &Config{
		CPABaseURL:     strings.TrimRight(envOrDefault("CPA_BASE_URL", defaultBaseURL), "/"),
		CPAAPIKey:      strings.TrimSpace(os.Getenv("CPA_API_KEY")),
		Model:          envOrDefault("GROK_MODEL", defaultModel),
		Timeout:        defaultTimeout,
		Debug:          parseBoolEnv("GROK_MCP_DEBUG"),
		HTTPAddr:       envOrDefault("GROK_HTTP_ADDR", defaultHTTPAddr),
		DBPath:         envOrDefault("GROK_DB_PATH", defaultDBPath),
		JWTSecret:      strings.TrimSpace(os.Getenv("GROK_JWT_SECRET")),
		DefaultUserRPM: defaultLimiterRPM,
		MCPIPRPM:       defaultMCPIPRPM,
		ProxyURL:       proxyURL,
		ProxyEnabled:   resolveProxyEnabledFromEnv(proxyURL),
	}

	if raw := strings.TrimSpace(os.Getenv("GROK_HTTP_TIMEOUT")); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds <= 0 {
			return nil, fmt.Errorf("GROK_HTTP_TIMEOUT must be a positive integer (seconds), got %q", raw)
		}
		cfg.Timeout = time.Duration(seconds) * time.Second
	}

	// GROK_DEFAULT_USER_RPM 仅用于内存限流器的兜底；用户实际 RPM 始终取自 tier。
	if raw := strings.TrimSpace(os.Getenv("GROK_DEFAULT_USER_RPM")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("GROK_DEFAULT_USER_RPM must be a positive integer, got %q", raw)
		}
		cfg.DefaultUserRPM = n
	}

	if raw := strings.TrimSpace(os.Getenv("GROK_MCP_IP_RPM")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("GROK_MCP_IP_RPM must be a positive integer, got %q", raw)
		}
		cfg.MCPIPRPM = n
	}

	// GROK_TRUSTED_PROXIES: 逗号分隔 CIDR 或单 IP（自动补 /32 或 /128）。
	// 仅列出边缘反代地址；空则 IP 限流只用 RemoteAddr。
	if raw := strings.TrimSpace(os.Getenv("GROK_TRUSTED_PROXIES")); raw != "" {
		networks, err := parseTrustedProxyCIDRs(raw)
		if err != nil {
			return nil, err
		}
		cfg.TrustedProxies = networks
	}

	if cfg.CPAAPIKey == "" {
		return nil, fmt.Errorf("CPA_API_KEY is required")
	}
	if cfg.CPABaseURL == "" {
		return nil, fmt.Errorf("CPA_BASE_URL must not be empty")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("GROK_MODEL must not be empty")
	}
	serverSettings, err := NormalizeServerSettings(cfg.ServerSettings())
	if err != nil {
		return nil, err
	}
	cfg.ApplyServerSettings(serverSettings)
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("GROK_JWT_SECRET is required")
	}
	// HS256 的安全性依赖密钥长度；短密钥可被离线暴力破解伪造 token。
	// RFC 7518 推荐 HS256 使用至少 256 位（32 字节）密钥，此处据此拒绝弱密钥。
	const minJWTSecretLen = 32
	if len(cfg.JWTSecret) < minJWTSecretLen {
		return nil, fmt.Errorf("GROK_JWT_SECRET must be at least %d bytes to avoid weak-key attacks on HS256", minJWTSecretLen)
	}

	return cfg, nil
}

// ServerSettings returns the current runtime-tunable server settings.
func (c *Config) ServerSettings() ServerSettings {
	timeoutSeconds := int(c.Timeout / time.Second)
	if timeoutSeconds <= 0 {
		timeoutSeconds = int(defaultTimeout / time.Second)
	}
	return ServerSettings{
		CPABaseURL:     c.CPABaseURL,
		CPAAPIKey:      c.CPAAPIKey,
		Model:          c.Model,
		TimeoutSeconds: timeoutSeconds,
		ProxyURL:       c.ProxyURL,
		ProxyEnabled:   c.ProxyEnabled,
		Debug:          c.Debug,
	}
}

// ApplyServerSettings updates the runtime-tunable fields on the config object.
func (c *Config) ApplyServerSettings(settings ServerSettings) {
	c.CPABaseURL = settings.CPABaseURL
	c.CPAAPIKey = settings.CPAAPIKey
	c.Model = settings.Model
	c.Timeout = time.Duration(settings.TimeoutSeconds) * time.Second
	c.ProxyURL = settings.ProxyURL
	c.ProxyEnabled = settings.ProxyEnabled
	c.Debug = settings.Debug
}

// NormalizeServerSettings trims, validates, and canonicalizes settings that can
// be edited from the admin panel.
func NormalizeServerSettings(settings ServerSettings) (ServerSettings, error) {
	settings.CPABaseURL = strings.TrimRight(strings.TrimSpace(settings.CPABaseURL), "/")
	settings.CPAAPIKey = strings.TrimSpace(settings.CPAAPIKey)
	settings.Model = strings.TrimSpace(settings.Model)
	settings.ProxyURL = strings.TrimSpace(settings.ProxyURL)

	if settings.CPAAPIKey == "" {
		return settings, fmt.Errorf("CPA_API_KEY is required")
	}
	if settings.CPABaseURL == "" {
		return settings, fmt.Errorf("CPA_BASE_URL must not be empty")
	}
	if err := validateHTTPURL("CPA_BASE_URL", settings.CPABaseURL); err != nil {
		return settings, err
	}
	if settings.Model == "" {
		return settings, fmt.Errorf("GROK_MODEL must not be empty")
	}
	if err := ValidateModel(settings.Model); err != nil {
		return settings, err
	}
	if settings.TimeoutSeconds <= 0 {
		return settings, fmt.Errorf("GROK_HTTP_TIMEOUT must be a positive integer (seconds), got %d", settings.TimeoutSeconds)
	}
	if settings.ProxyEnabled {
		if settings.ProxyURL == "" {
			return settings, fmt.Errorf("GROK_PROXY_URL is required when proxy is enabled")
		}
		if err := validateHTTPURL("GROK_PROXY_URL", settings.ProxyURL); err != nil {
			return settings, err
		}
	}

	return settings, nil
}

// ValidateModel 校验模型名是否合法：只需包含 "grok"（不区分大小写）即可。
// 供 config.NormalizeServerSettings 与 grok.validateModel 共享同一规则，
// 避免面板保存的模型名在请求时被 grok 层拒绝导致全部搜索不可用。
func ValidateModel(model string) error {
	if !strings.Contains(strings.ToLower(model), "grok") {
		return fmt.Errorf("unsupported model: %q (must contain 'grok')", model)
	}
	return nil
}

func validateHTTPURL(name, rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return fmt.Errorf("%s must be a valid http(s) URL", name)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("%s must use http or https", name)
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func parseBoolEnv(key string) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	return raw == "1" || raw == "true" || raw == "yes"
}

func resolveProxyEnabledFromEnv(proxyURL string) bool {
	if raw, ok := os.LookupEnv("GROK_PROXY_ENABLED"); ok {
		normalizedRawValue := strings.TrimSpace(strings.ToLower(raw))
		return normalizedRawValue == "1" || normalizedRawValue == "true" || normalizedRawValue == "yes"
	}

	// Treat GROK_PROXY_URL by itself as an explicit proxy configuration. When it
	// is absent, the HTTP client falls back to standard HTTP_PROXY/HTTPS_PROXY
	// environment variables through net/http.
	return strings.TrimSpace(proxyURL) != ""
}

// parseTrustedProxyCIDRs 解析逗号分隔的 CIDR 或单 IP 列表。
func parseTrustedProxyCIDRs(raw string) ([]*net.IPNet, error) {
	parts := strings.Split(raw, ",")
	networks := make([]*net.IPNet, 0, len(parts))
	for _, part := range parts {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}
		if !strings.Contains(entry, "/") {
			ip := net.ParseIP(entry)
			if ip == nil {
				return nil, fmt.Errorf("GROK_TRUSTED_PROXIES entry %q is not a valid IP or CIDR", entry)
			}
			if ip.To4() != nil {
				entry = entry + "/32"
			} else {
				entry = entry + "/128"
			}
		}
		_, network, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, fmt.Errorf("GROK_TRUSTED_PROXIES entry %q: %w", strings.TrimSpace(part), err)
		}
		networks = append(networks, network)
	}
	if len(networks) == 0 {
		return nil, fmt.Errorf("GROK_TRUSTED_PROXIES must contain at least one IP or CIDR")
	}
	return networks, nil
}
