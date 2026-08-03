package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const serverSettingsID = "default"

type storedServerSettingsAPIKey struct {
	ciphertext        string
	nonce             string
	encryptionVersion int
}

type storedTurnstileSecretKey struct {
	ciphertext        string
	nonce             string
	encryptionVersion int
}

func scanServerSettings(row interface {
	Scan(dest ...any) error
}) (*ServerSettings, storedServerSettingsAPIKey, storedTurnstileSecretKey, error) {
	var settings ServerSettings
	var storedAPIKey storedServerSettingsAPIKey
	var storedTurnstileKey storedTurnstileSecretKey
	var proxyEnabled int
	var registrationMode string
	var turnstileEnabled int
	var debug int
	var operationsMetricsEnabled int
	var createdAt string
	var updatedAt string
	err := row.Scan(
		&settings.ID,
		&settings.Revision,
		&settings.CPABaseURL,
		&storedAPIKey.ciphertext,
		&storedAPIKey.nonce,
		&storedAPIKey.encryptionVersion,
		&settings.UpstreamProtocol,
		&settings.Model,
		&settings.TimeoutSeconds,
		&settings.MCPGlobalSearchConcurrency,
		&settings.MCPUserSearchConcurrency,
		&settings.ProxyURL,
		&proxyEnabled,
		&registrationMode,
		&turnstileEnabled,
		&settings.TurnstileSiteKey,
		&storedTurnstileKey.ciphertext,
		&storedTurnstileKey.nonce,
		&storedTurnstileKey.encryptionVersion,
		&debug,
		&operationsMetricsEnabled,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, storedServerSettingsAPIKey{}, storedTurnstileSecretKey{}, err
	}
	settings.ProxyEnabled = proxyEnabled != 0
	var normalizeErr error
	settings.RegistrationMode, normalizeErr = NormalizeRegistrationMode(RegistrationMode(registrationMode))
	if normalizeErr != nil {
		return nil, storedServerSettingsAPIKey{}, storedTurnstileSecretKey{}, normalizeErr
	}
	settings.TurnstileEnabled = turnstileEnabled != 0
	settings.Debug = debug != 0
	settings.OperationsMetricsEnabled = operationsMetricsEnabled != 0
	var parseErr error
	settings.CreatedAt, parseErr = parseTime(createdAt)
	if parseErr != nil {
		return nil, storedServerSettingsAPIKey{}, storedTurnstileSecretKey{}, parseErr
	}
	settings.UpdatedAt, parseErr = parseTime(updatedAt)
	if parseErr != nil {
		return nil, storedServerSettingsAPIKey{}, storedTurnstileSecretKey{}, parseErr
	}
	return &settings, storedAPIKey, storedTurnstileKey, nil
}

const serverSettingsColumns = `id, revision, cpa_base_url, cpa_api_key_ciphertext, cpa_api_key_nonce, cpa_api_key_encryption_version, upstream_protocol, model, timeout_seconds, mcp_global_search_concurrency, mcp_user_search_concurrency, proxy_url, proxy_enabled, registration_mode, turnstile_enabled, turnstile_site_key, turnstile_secret_key_ciphertext, turnstile_secret_key_nonce, turnstile_secret_key_encryption_version, debug, operations_metrics_enabled, created_at, updated_at`

func serverSettingsAPIKeyRecordIdentity(settingsID string) string {
	return "server-settings:" + settingsID + ":cpa-api-key"
}

func turnstileSecretKeyRecordIdentity(settingsID string) string {
	return "server-settings:" + settingsID + ":turnstile-secret-key"
}

func (s *SQLiteStore) GetServerSettings(ctx context.Context) (*ServerSettings, error) {
	row := s.readDB.QueryRowContext(ctx, `SELECT `+serverSettingsColumns+` FROM server_settings WHERE id = ?`, serverSettingsID)
	return s.scanAndDecryptServerSettings(row)
}

func (s *SQLiteStore) scanAndDecryptServerSettings(row interface {
	Scan(dest ...any) error
}) (*ServerSettings, error) {
	settings, storedAPIKey, storedTurnstileSecretKey, err := scanServerSettings(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	plaintextAPIKey, err := s.decryptServerSettingsAPIKey(settings.ID, storedAPIKey)
	if err != nil {
		return nil, err
	}
	settings.CPAAPIKey = plaintextAPIKey
	plaintextTurnstileSecretKey, err := s.decryptTurnstileSecretKey(settings.ID, storedTurnstileSecretKey)
	if err != nil {
		return nil, err
	}
	settings.TurnstileSecretKey = plaintextTurnstileSecretKey
	return settings, nil
}

func (s *SQLiteStore) decryptServerSettingsAPIKey(settingsID string, storedAPIKey storedServerSettingsAPIKey) (string, error) {
	hasCompleteCiphertext := storedAPIKey.ciphertext != "" && storedAPIKey.nonce != "" && storedAPIKey.encryptionVersion != 0
	if !hasCompleteCiphertext {
		return "", fmt.Errorf("server settings CPA API key ciphertext is incomplete")
	}
	plaintextAPIKey, err := s.secretCipher.Decrypt(
		storedAPIKey.ciphertext,
		storedAPIKey.nonce,
		serverSettingsAPIKeyRecordIdentity(settingsID),
		storedAPIKey.encryptionVersion,
	)
	if err != nil {
		return "", fmt.Errorf("decrypt server settings CPA API key: %w", err)
	}
	return plaintextAPIKey, nil
}

func (s *SQLiteStore) decryptTurnstileSecretKey(
	settingsID string,
	storedSecretKey storedTurnstileSecretKey,
) (string, error) {
	hasNoCiphertext := storedSecretKey.ciphertext == "" && storedSecretKey.nonce == "" && storedSecretKey.encryptionVersion == 0
	if hasNoCiphertext {
		return "", nil
	}
	hasCompleteCiphertext := storedSecretKey.ciphertext != "" && storedSecretKey.nonce != "" && storedSecretKey.encryptionVersion != 0
	if !hasCompleteCiphertext {
		return "", fmt.Errorf("server settings Turnstile secret key ciphertext is incomplete")
	}
	plaintextSecretKey, err := s.secretCipher.Decrypt(
		storedSecretKey.ciphertext,
		storedSecretKey.nonce,
		turnstileSecretKeyRecordIdentity(settingsID),
		storedSecretKey.encryptionVersion,
	)
	if err != nil {
		return "", fmt.Errorf("decrypt server settings Turnstile secret key: %w", err)
	}
	return plaintextSecretKey, nil
}

func (s *SQLiteStore) UpsertServerSettings(ctx context.Context, settings ServerSettings) (*ServerSettings, error) {
	cpaBaseURL := strings.TrimSpace(settings.CPABaseURL)
	cpaAPIKey := strings.TrimSpace(settings.CPAAPIKey)
	upstreamProtocol := strings.TrimSpace(string(settings.UpstreamProtocol))
	model := strings.TrimSpace(settings.Model)
	proxyURL := strings.TrimSpace(settings.ProxyURL)
	turnstileSiteKey := strings.TrimSpace(settings.TurnstileSiteKey)
	turnstileSecretKey := strings.TrimSpace(settings.TurnstileSecretKey)
	if cpaBaseURL == "" {
		return nil, fmt.Errorf("cpa_base_url is required")
	}
	if cpaAPIKey == "" {
		return nil, fmt.Errorf("cpa_api_key is required")
	}
	if upstreamProtocol == "" {
		return nil, fmt.Errorf("upstream_protocol is required")
	}
	if model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if settings.TimeoutSeconds <= 0 {
		return nil, fmt.Errorf("timeout_seconds must be positive")
	}
	if settings.MCPGlobalSearchConcurrency <= 0 {
		return nil, fmt.Errorf("mcp_global_search_concurrency must be positive")
	}
	if settings.MCPUserSearchConcurrency <= 0 {
		return nil, fmt.Errorf("mcp_user_search_concurrency must be positive")
	}
	if settings.MCPUserSearchConcurrency > settings.MCPGlobalSearchConcurrency {
		return nil, fmt.Errorf("mcp_user_search_concurrency must not exceed mcp_global_search_concurrency")
	}
	registrationMode, err := NormalizeRegistrationMode(settings.RegistrationMode)
	if err != nil {
		return nil, err
	}
	if settings.TurnstileEnabled && turnstileSiteKey == "" {
		return nil, fmt.Errorf("turnstile_site_key is required when Turnstile is enabled")
	}
	if settings.TurnstileEnabled && turnstileSecretKey == "" {
		return nil, fmt.Errorf("turnstile_secret_key is required when Turnstile is enabled")
	}
	ciphertext, nonce, encryptionVersion, err := s.secretCipher.Encrypt(
		cpaAPIKey,
		serverSettingsAPIKeyRecordIdentity(serverSettingsID),
	)
	if err != nil {
		return nil, fmt.Errorf("encrypt server settings CPA API key: %w", err)
	}
	turnstileCiphertext := ""
	turnstileNonce := ""
	turnstileEncryptionVersion := 0
	if turnstileSecretKey != "" {
		turnstileCiphertext, turnstileNonce, turnstileEncryptionVersion, err = s.secretCipher.Encrypt(
			turnstileSecretKey,
			turnstileSecretKeyRecordIdentity(serverSettingsID),
		)
		if err != nil {
			return nil, fmt.Errorf("encrypt server settings Turnstile secret key: %w", err)
		}
	}

	now := formatTime(time.Now().UTC())
	var expectedRevisionArgument any
	if settings.ExpectedRevision != nil {
		expectedRevisionArgument = *settings.ExpectedRevision
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO server_settings (
			id, revision, cpa_base_url, cpa_api_key_ciphertext, cpa_api_key_nonce, cpa_api_key_encryption_version,
			upstream_protocol, model, timeout_seconds, mcp_global_search_concurrency, mcp_user_search_concurrency,
			proxy_url, proxy_enabled, registration_mode, turnstile_enabled, turnstile_site_key,
			turnstile_secret_key_ciphertext, turnstile_secret_key_nonce, turnstile_secret_key_encryption_version,
			debug, operations_metrics_enabled, created_at, updated_at
		) VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			revision = server_settings.revision + 1,
			cpa_base_url = excluded.cpa_base_url,
			cpa_api_key_ciphertext = excluded.cpa_api_key_ciphertext,
			cpa_api_key_nonce = excluded.cpa_api_key_nonce,
			cpa_api_key_encryption_version = excluded.cpa_api_key_encryption_version,
			upstream_protocol = excluded.upstream_protocol,
			model = excluded.model,
			timeout_seconds = excluded.timeout_seconds,
			mcp_global_search_concurrency = excluded.mcp_global_search_concurrency,
			mcp_user_search_concurrency = excluded.mcp_user_search_concurrency,
			proxy_url = excluded.proxy_url,
			proxy_enabled = excluded.proxy_enabled,
			registration_mode = excluded.registration_mode,
			turnstile_enabled = excluded.turnstile_enabled,
			turnstile_site_key = excluded.turnstile_site_key,
			turnstile_secret_key_ciphertext = excluded.turnstile_secret_key_ciphertext,
			turnstile_secret_key_nonce = excluded.turnstile_secret_key_nonce,
			turnstile_secret_key_encryption_version = excluded.turnstile_secret_key_encryption_version,
			debug = excluded.debug,
			operations_metrics_enabled = excluded.operations_metrics_enabled,
			updated_at = excluded.updated_at
		WHERE ? IS NULL OR server_settings.revision = ?
		RETURNING `+serverSettingsColumns,
		serverSettingsID,
		cpaBaseURL,
		ciphertext,
		nonce,
		encryptionVersion,
		upstreamProtocol,
		model,
		settings.TimeoutSeconds,
		settings.MCPGlobalSearchConcurrency,
		settings.MCPUserSearchConcurrency,
		proxyURL,
		boolAsInteger(settings.ProxyEnabled),
		string(registrationMode),
		boolAsInteger(settings.TurnstileEnabled),
		turnstileSiteKey,
		turnstileCiphertext,
		turnstileNonce,
		turnstileEncryptionVersion,
		boolAsInteger(settings.Debug),
		boolAsInteger(settings.OperationsMetricsEnabled),
		now,
		now,
		expectedRevisionArgument,
		expectedRevisionArgument,
	)
	storedSettings, err := s.scanAndDecryptServerSettings(row)
	if err != nil {
		return nil, fmt.Errorf("upsert server settings: %w", err)
	}
	if storedSettings == nil && settings.ExpectedRevision != nil {
		return nil, ErrServerSettingsConflict
	}
	return storedSettings, nil
}
