package panel

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/grok-mcp/internal/config"
	"github.com/grok-mcp/internal/store"
)

func (h *Handler) adminGetServerSettings(w http.ResponseWriter, r *http.Request) {
	settings, updatedAt, err := h.loadEffectiveServerSettings(r)
	if err != nil {
		log.Printf("admin get server settings failed: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load server settings")
		return
	}
	writeJSON(w, http.StatusOK, toServerSettingsResponse(settings, updatedAt))
}

func (h *Handler) adminUpdateServerSettings(w http.ResponseWriter, r *http.Request) {
	currentSettings, _, err := h.loadEffectiveServerSettings(r)
	if err != nil {
		log.Printf("admin load current server settings failed: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load server settings")
		return
	}

	var req UpdateServerSettingsRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}

	updatedSettings := mergeServerSettingsRequest(currentSettings, req)
	normalizedSettings, err := config.NormalizeServerSettings(updatedSettings)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	storedSettings, err := h.Store.UpsertServerSettings(r.Context(), toStoreServerSettings(normalizedSettings))
	if err != nil {
		log.Printf("admin persist server settings failed: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to save server settings")
		return
	}

	if h.Config != nil {
		h.Config.ApplyServerSettings(normalizedSettings)
	}
	if h.SettingsApplier != nil {
		if err := h.SettingsApplier.ApplyServerSettings(normalizedSettings); err != nil {
			log.Printf("admin apply server settings failed: %v", err)
			writeError(w, http.StatusInternalServerError, "settings saved but failed to apply")
			return
		}
	}

	updatedAt := storedSettings.UpdatedAt
	writeJSON(w, http.StatusOK, toServerSettingsResponse(normalizedSettings, &updatedAt))
}

func (h *Handler) loadEffectiveServerSettings(r *http.Request) (config.ServerSettings, *time.Time, error) {
	storedSettings, err := h.Store.GetServerSettings(r.Context())
	if err != nil {
		return config.ServerSettings{}, nil, err
	}
	if storedSettings != nil {
		updatedAt := storedSettings.UpdatedAt
		return toConfigServerSettings(storedSettings), &updatedAt, nil
	}
	if h.Config == nil {
		return config.ServerSettings{}, nil, nil
	}
	settings, err := config.NormalizeServerSettings(h.Config.ServerSettings())
	if err != nil {
		return config.ServerSettings{}, nil, err
	}
	return settings, nil, nil
}

func mergeServerSettingsRequest(currentSettings config.ServerSettings, req UpdateServerSettingsRequest) config.ServerSettings {
	mergedSettings := currentSettings
	if req.CPABaseURL != nil {
		mergedSettings.CPABaseURL = *req.CPABaseURL
	}
	if req.CPAAPIKey != nil && strings.TrimSpace(*req.CPAAPIKey) != "" {
		mergedSettings.CPAAPIKey = *req.CPAAPIKey
	}
	if req.Model != nil {
		mergedSettings.Model = *req.Model
	}
	if req.TimeoutSeconds != nil {
		mergedSettings.TimeoutSeconds = *req.TimeoutSeconds
	}
	if req.ProxyURL != nil {
		mergedSettings.ProxyURL = *req.ProxyURL
	}
	if req.ProxyEnabled != nil {
		mergedSettings.ProxyEnabled = *req.ProxyEnabled
	}
	if req.Debug != nil {
		mergedSettings.Debug = *req.Debug
	}
	return mergedSettings
}

func toConfigServerSettings(settings *store.ServerSettings) config.ServerSettings {
	return config.ServerSettings{
		CPABaseURL:     settings.CPABaseURL,
		CPAAPIKey:      settings.CPAAPIKey,
		Model:          settings.Model,
		TimeoutSeconds: settings.TimeoutSeconds,
		ProxyURL:       settings.ProxyURL,
		ProxyEnabled:   settings.ProxyEnabled,
		Debug:          settings.Debug,
	}
}

func toStoreServerSettings(settings config.ServerSettings) store.ServerSettings {
	return store.ServerSettings{
		CPABaseURL:     settings.CPABaseURL,
		CPAAPIKey:      settings.CPAAPIKey,
		Model:          settings.Model,
		TimeoutSeconds: settings.TimeoutSeconds,
		ProxyURL:       settings.ProxyURL,
		ProxyEnabled:   settings.ProxyEnabled,
		Debug:          settings.Debug,
	}
}
