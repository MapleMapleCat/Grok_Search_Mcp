package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func setEnv(t *testing.T, key, value string) {
	t.Helper()
	t.Setenv(key, value)
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	oldValue, hadValue := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("failed to unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if hadValue {
			_ = os.Setenv(key, oldValue)
			return
		}
		_ = os.Unsetenv(key)
	})
}

// panelEnv 提供 Load 所需的最小环境变量，包括满足最小长度校验的 JWT 密钥。
func panelEnv(t *testing.T) {
	t.Helper()
	unsetEnv(t, "GROK_PROXY_URL")
	unsetEnv(t, "GROK_PROXY_ENABLED")
	unsetEnv(t, "GROK_USAGE_RAW_RETENTION_DAYS")
	unsetEnv(t, "GROK_USAGE_HOURLY_RETENTION_DAYS")
	unsetEnv(t, "GROK_USAGE_DAILY_RETENTION_DAYS")
	unsetEnv(t, "GROK_USAGE_MAINTENANCE_INTERVAL")
	setEnv(t, "CPA_API_KEY", "test-key")
	setEnv(t, "GROK_JWT_SECRET", "jwt-secret-must-be-at-least-32-bytes!")
}

func TestLoadAllowsMissingAPIKeyForDatabaseFallback(t *testing.T) {
	setEnv(t, "CPA_API_KEY", "")
	setEnv(t, "GROK_JWT_SECRET", "jwt-secret-must-be-at-least-32-bytes!")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load should allow a database to provide the CPA key: %v", err)
	}
	if cfg.CPAAPIKey != "" {
		t.Fatalf("expected empty environment CPA key, got %q", cfg.CPAAPIKey)
	}
}

func TestLoadDefaults(t *testing.T) {
	panelEnv(t)
	setEnv(t, "CPA_BASE_URL", "")
	setEnv(t, "GROK_UPSTREAM_PROTOCOL", "")
	setEnv(t, "GROK_MODEL", "")
	setEnv(t, "GROK_HTTP_TIMEOUT", "")
	setEnv(t, "GROK_MCP_DEBUG", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.CPABaseURL != "http://127.0.0.1:8317" {
		t.Fatalf("unexpected base URL: %s", cfg.CPABaseURL)
	}
	if cfg.UpstreamProtocol != UpstreamProtocolResponses {
		t.Fatalf("unexpected upstream protocol: %s", cfg.UpstreamProtocol)
	}
	if cfg.Model != "grok-4.3" {
		t.Fatalf("unexpected model: %s", cfg.Model)
	}
	if cfg.Timeout != 120*time.Second {
		t.Fatalf("unexpected timeout: %v", cfg.Timeout)
	}
	if cfg.Debug {
		t.Fatalf("expected debug disabled by default")
	}
}

func TestNormalizeUpstreamProtocol(t *testing.T) {
	testCases := []struct {
		name         string
		input        UpstreamProtocol
		expected     UpstreamProtocol
		expectsError bool
	}{
		{name: "rejects empty protocol", input: "", expectsError: true},
		{name: "trims and normalizes", input: " Chat_Completions ", expected: UpstreamProtocolChatCompletions},
		{name: "anthropic messages", input: UpstreamProtocolAnthropicMessages, expected: UpstreamProtocolAnthropicMessages},
		{name: "rejects unknown protocol", input: "legacy", expectsError: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			actual, err := NormalizeUpstreamProtocol(testCase.input)
			if testCase.expectsError {
				if err == nil {
					t.Fatalf("expected protocol validation error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize protocol: %v", err)
			}
			if actual != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, actual)
			}
		})
	}
}

func TestLoadDoesNotEnableProxyFromURLAlone(t *testing.T) {
	panelEnv(t)
	setEnv(t, "GROK_PROXY_URL", " http://127.0.0.1:7890 ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.ProxyEnabled {
		t.Fatalf("expected proxy disabled without GROK_PROXY_ENABLED")
	}
	if cfg.ProxyURL != "http://127.0.0.1:7890" {
		t.Fatalf("unexpected proxy URL: %q", cfg.ProxyURL)
	}
}

func TestLoadHonorsExplicitProxyEnabledFlag(t *testing.T) {
	panelEnv(t)
	setEnv(t, "GROK_PROXY_URL", "http://127.0.0.1:7890")
	setEnv(t, "GROK_PROXY_ENABLED", "0")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.ProxyEnabled {
		t.Fatalf("expected GROK_PROXY_ENABLED=0 to disable explicit proxy")
	}
	if cfg.ProxyURL != "http://127.0.0.1:7890" {
		t.Fatalf("unexpected proxy URL: %q", cfg.ProxyURL)
	}
}

func TestLoadRejectsEnabledProxyWithoutURL(t *testing.T) {
	panelEnv(t)
	setEnv(t, "GROK_PROXY_ENABLED", "true")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "GROK_PROXY_URL is required when proxy is enabled") {
		t.Fatalf("expected proxy URL validation error, got %v", err)
	}
}

func TestLoadCustomTimeout(t *testing.T) {
	panelEnv(t)
	setEnv(t, "GROK_HTTP_TIMEOUT", "45")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Timeout != 45*time.Second {
		t.Fatalf("unexpected timeout: %v", cfg.Timeout)
	}
}

func TestLoadInvalidTimeout(t *testing.T) {
	panelEnv(t)
	setEnv(t, "GROK_HTTP_TIMEOUT", "abc")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "GROK_HTTP_TIMEOUT must be a positive integer") {
		t.Fatalf("expected timeout validation error, got %v", err)
	}
}

func TestLoadDebugParsing(t *testing.T) {
	panelEnv(t)

	for _, value := range []string{"1", "true", "yes"} {
		setEnv(t, "GROK_MCP_DEBUG", value)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load failed for %q: %v", value, err)
		}
		if !cfg.Debug {
			t.Fatalf("expected debug enabled for %q", value)
		}
	}

	setEnv(t, "GROK_MCP_DEBUG", "0")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Debug {
		t.Fatalf("expected debug disabled for 0")
	}
}

func TestLoadRequiresJWTSecret(t *testing.T) {
	setEnv(t, "CPA_API_KEY", "test-key")
	setEnv(t, "GROK_JWT_SECRET", "")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "GROK_JWT_SECRET is required") {
		t.Fatalf("expected jwt secret error, got %v", err)
	}
}

func TestLoadRejectsShortJWTSecret(t *testing.T) {
	setEnv(t, "CPA_API_KEY", "test-key")
	setEnv(t, "GROK_JWT_SECRET", "a")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "at least 32 bytes") {
		t.Fatalf("expected short jwt secret error, got %v", err)
	}
}

func TestLoadHTTPDefaults(t *testing.T) {
	panelEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.HTTPAddr != ":8080" ||
		cfg.DBPath != "./grok-mcp.db" ||
		cfg.MCPIPRPM != 300 {
		t.Fatalf("unexpected http defaults: %+v", cfg)
	}
}

func TestLoadUsageRetentionDefaults(t *testing.T) {
	panelEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.UsageRawRetention != 7*24*time.Hour {
		t.Fatalf("raw retention = %v, want 7 days", cfg.UsageRawRetention)
	}
	if cfg.UsageHourlyRetention != 90*24*time.Hour {
		t.Fatalf("hourly retention = %v, want 90 days", cfg.UsageHourlyRetention)
	}
	if cfg.UsageDailyRetention != 730*24*time.Hour {
		t.Fatalf("daily retention = %v, want 730 days", cfg.UsageDailyRetention)
	}
	if cfg.UsageMaintenanceInterval != time.Hour {
		t.Fatalf("maintenance interval = %v, want 1 hour", cfg.UsageMaintenanceInterval)
	}
}

func TestLoadCustomUsageRetention(t *testing.T) {
	panelEnv(t)
	setEnv(t, "GROK_USAGE_RAW_RETENTION_DAYS", "3")
	setEnv(t, "GROK_USAGE_HOURLY_RETENTION_DAYS", "30")
	setEnv(t, "GROK_USAGE_DAILY_RETENTION_DAYS", "365")
	setEnv(t, "GROK_USAGE_MAINTENANCE_INTERVAL", "30m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.UsageRawRetention != 3*24*time.Hour ||
		cfg.UsageHourlyRetention != 30*24*time.Hour ||
		cfg.UsageDailyRetention != 365*24*time.Hour ||
		cfg.UsageMaintenanceInterval != 30*time.Minute {
		t.Fatalf("unexpected usage maintenance config: %+v", cfg)
	}
}

func TestLoadRejectsInvalidUsageRetention(t *testing.T) {
	testCases := []struct {
		name          string
		environment   map[string]string
		expectedError string
	}{
		{
			name:          "non-positive raw retention",
			environment:   map[string]string{"GROK_USAGE_RAW_RETENTION_DAYS": "0"},
			expectedError: "GROK_USAGE_RAW_RETENTION_DAYS must be a positive integer",
		},
		{
			name: "hourly retention must exceed raw retention",
			environment: map[string]string{
				"GROK_USAGE_RAW_RETENTION_DAYS":    "30",
				"GROK_USAGE_HOURLY_RETENTION_DAYS": "30",
			},
			expectedError: "GROK_USAGE_HOURLY_RETENTION_DAYS must exceed",
		},
		{
			name:          "invalid maintenance interval",
			environment:   map[string]string{"GROK_USAGE_MAINTENANCE_INTERVAL": "soon"},
			expectedError: "GROK_USAGE_MAINTENANCE_INTERVAL must be a positive duration",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			panelEnv(t)
			for environmentVariable, value := range testCase.environment {
				setEnv(t, environmentVariable, value)
			}
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), testCase.expectedError) {
				t.Fatalf("expected error containing %q, got %v", testCase.expectedError, err)
			}
		})
	}
}

func TestLoadCustomSecuritySettings(t *testing.T) {
	panelEnv(t)
	setEnv(t, "GROK_MCP_IP_RPM", "123")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.MCPIPRPM != 123 {
		t.Fatalf("unexpected security settings: %+v", cfg)
	}
}

func TestLoadRejectsInvalidSecuritySettings(t *testing.T) {
	panelEnv(t)
	setEnv(t, "GROK_MCP_IP_RPM", "0")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "GROK_MCP_IP_RPM must be a positive integer") {
		t.Fatalf("expected MCP IP RPM validation error, got %v", err)
	}
}

func TestParseBoolEnvUnset(t *testing.T) {
	_ = os.Unsetenv("GROK_MCP_DEBUG")
	if parseBoolEnv("GROK_MCP_DEBUG") {
		t.Fatalf("expected false for unset env")
	}
}
