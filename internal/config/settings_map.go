package config

import "github.com/grok-mcp/internal/store"

// ServerSettingsFromStore maps persisted settings into the runtime-tunable view.
func ServerSettingsFromStore(settings *store.ServerSettings) ServerSettings {
	fields := store.SettingsFieldsFromStore(settings)
	return ServerSettingsFromFields(fields)
}

// StoreServerSettings maps runtime-tunable settings into the persistence shape.
func StoreServerSettings(settings ServerSettings) store.ServerSettings {
	return store.ServerSettingsFromFields(SettingsFieldsFromConfig(settings))
}

// ServerSettingsFromFields builds runtime settings from the shared field set.
func ServerSettingsFromFields(fields store.SettingsFields) ServerSettings {
	return ServerSettings{
		CPABaseURL:     fields.CPABaseURL,
		CPAAPIKey:      fields.CPAAPIKey,
		Model:          fields.Model,
		TimeoutSeconds: fields.TimeoutSeconds,
		ProxyURL:       fields.ProxyURL,
		ProxyEnabled:   fields.ProxyEnabled,
		Debug:          fields.Debug,
	}
}

// SettingsFieldsFromConfig extracts the shared field set from runtime settings.
func SettingsFieldsFromConfig(settings ServerSettings) store.SettingsFields {
	return store.SettingsFields{
		CPABaseURL:     settings.CPABaseURL,
		CPAAPIKey:      settings.CPAAPIKey,
		Model:          settings.Model,
		TimeoutSeconds: settings.TimeoutSeconds,
		ProxyURL:       settings.ProxyURL,
		ProxyEnabled:   settings.ProxyEnabled,
		Debug:          settings.Debug,
	}
}
