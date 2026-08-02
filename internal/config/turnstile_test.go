package config

import (
	"strings"
	"testing"

	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/store"
)

func validTurnstileTestSettings() ServerSettings {
	return ServerSettings{
		CPABaseURL:                 "http://127.0.0.1:8317",
		CPAAPIKey:                  "test-cpa-key",
		UpstreamProtocol:           UpstreamProtocolResponses,
		Model:                      "grok-4.5",
		TimeoutSeconds:             120,
		MCPGlobalSearchConcurrency: 16,
		MCPUserSearchConcurrency:   4,
		RegistrationMode:           store.RegistrationModeDisabled,
	}
}

func TestNormalizeServerSettingsValidatesTurnstileConfiguration(t *testing.T) {
	testCases := []struct {
		name          string
		siteKey       string
		secretKey     string
		expectedError string
	}{
		{name: "missing site key", secretKey: "secret-key", expectedError: "turnstile_site_key"},
		{name: "missing secret key", siteKey: "site-key", expectedError: "turnstile_secret_key"},
		{name: "complete configuration", siteKey: " site-key ", secretKey: " secret-key "},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			serverSettings := validTurnstileTestSettings()
			serverSettings.TurnstileEnabled = true
			serverSettings.TurnstileSiteKey = testCase.siteKey
			serverSettings.TurnstileSecretKey = testCase.secretKey

			normalizedSettings, err := NormalizeServerSettings(serverSettings)
			if testCase.expectedError != "" {
				if err == nil || !strings.Contains(err.Error(), testCase.expectedError) {
					t.Fatalf("NormalizeServerSettings error = %v, want %q", err, testCase.expectedError)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeServerSettings failed: %v", err)
			}
			if normalizedSettings.TurnstileSiteKey != "site-key" || normalizedSettings.TurnstileSecretKey != "secret-key" {
				t.Fatalf("Turnstile keys were not normalized: %+v", normalizedSettings)
			}
		})
	}
}

func TestNormalizeServerSettingsAllowsDisabledTurnstileWithoutKeys(t *testing.T) {
	serverSettings := validTurnstileTestSettings()
	normalizedSettings, err := NormalizeServerSettings(serverSettings)
	if err != nil {
		t.Fatalf("disabled Turnstile configuration was rejected: %v", err)
	}
	if normalizedSettings.TurnstileEnabled {
		t.Fatal("Turnstile was unexpectedly enabled")
	}
}
