import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const sessionValues = new Map();
globalThis.window = { location: { search: "", hash: "" } };
globalThis.sessionStorage = {
  getItem(key) {
    return sessionValues.get(key) ?? null;
  },
  setItem(key, value) {
    sessionValues.set(key, String(value));
  },
  removeItem(key) {
    sessionValues.delete(key);
  }
};

async function importStandaloneModule(relativeModulePath) {
  const moduleSource = await readFile(new URL(relativeModulePath, import.meta.url), "utf8");
  const encodedModuleSource = Buffer.from(moduleSource).toString("base64");
  return import(`data:text/javascript;base64,${encodedModuleSource}`);
}

async function importBrowserModule(relativeModulePath) {
  const moduleURL = new URL(relativeModulePath, import.meta.url);
  return import(await buildBrowserModuleDataURL(moduleURL));
}

async function buildBrowserModuleDataURL(moduleURL) {
  let moduleSource = await readFile(moduleURL, "utf8");
  const relativeImportPattern = /from\s+["'](\.[^"']+)["']/g;
  const relativeSpecifiers = Array.from(
    moduleSource.matchAll(relativeImportPattern),
    (match) => match[1]
  );
  for (const relativeSpecifier of new Set(relativeSpecifiers)) {
    const dependencyDataURL = await buildBrowserModuleDataURL(new URL(relativeSpecifier, moduleURL));
    moduleSource = moduleSource
      .replaceAll(`"${relativeSpecifier}"`, `"${dependencyDataURL}"`)
      .replaceAll(`'${relativeSpecifier}'`, `'${dependencyDataURL}'`);
  }
  return `data:text/javascript;base64,${Buffer.from(moduleSource).toString("base64")}`;
}

const { panelAPI } = await importStandaloneModule("../static/js/api.js");
const {
  applySettingsResponseToState,
  markSettingsSavedNotApplied,
  reloadSavedNotAppliedSettings,
  savedNotAppliedCondition
} = await importStandaloneModule("../static/js/settings-apply-state.js");
const { renderSettingsPage } = await importBrowserModule("../static/js/pages/settings.js");
const { renderAuthView } = await importBrowserModule("../static/js/components/forms.js");
const { createSettingsEvents } = await importBrowserModule("../static/js/events/settings-events.js");
const { refreshLoginVerificationBeforeMissingTokenError } = await importStandaloneModule(
  "../static/js/login-verification-state.js"
);

test("login form renders Turnstile only when enabled", () => {
  const baseState = {
    registrationMode: "disabled",
    authenticationSettingsStatus: "ready",
    authenticationSettingsError: "",
    authMode: "login",
    authBusy: false,
    authError: "",
    turnstileEnabled: false,
    turnstileSiteKey: ""
  };
  const disabledMarkup = renderAuthView(baseState);
  assert.doesNotMatch(disabledMarkup, /data-turnstile-container/);
  assert.doesNotMatch(disabledMarkup, /name="turnstile_token"/);

  const enabledMarkup = renderAuthView({
    ...baseState,
    turnstileEnabled: true,
    turnstileSiteKey: "public-site-key"
  });
  assert.match(enabledMarkup, /data-turnstile-container/);
  assert.match(enabledMarkup, /name="turnstile_token"/);
  assert.doesNotMatch(enabledMarkup, /private-secret-key/);
});

test("authentication forms stay unavailable when public settings fail", () => {
  const errorMarkup = renderAuthView({
    registrationMode: "free",
    authenticationSettingsStatus: "error",
    authenticationSettingsError: "public settings unavailable",
    authMode: "login",
    authBusy: false,
    authError: "",
    turnstileEnabled: false,
    turnstileSiteKey: ""
  });

  assert.doesNotMatch(errorMarkup, /data-form="login"/);
  assert.doesNotMatch(errorMarkup, /data-form="register"/);
  assert.match(errorMarkup, /public settings unavailable/);
  assert.match(errorMarkup, /retry-authentication-settings/);
});

test("an old login page reloads policy before rejecting a missing Turnstile token", async () => {
  const state = {
    authenticationSettingsStatus: "ready",
    authenticationSettingsError: "",
    turnstileEnabled: true,
    turnstileSiteKey: "retired-site-key",
    authError: ""
  };
  let renderCount = 0;

  const loginVerificationReady = await refreshLoginVerificationBeforeMissingTokenError({
    state,
    turnstileToken: "",
    renderApplication() {
      renderCount += 1;
    },
    async reloadAuthenticationSettings() {
      state.authenticationSettingsStatus = "ready";
      state.turnstileEnabled = false;
      state.turnstileSiteKey = "";
      return true;
    }
  });

  assert.equal(loginVerificationReady, true);
  assert.equal(state.turnstileEnabled, false);
  assert.equal(state.authError, "");
  assert.equal(renderCount, 1);
});

test("settings submit sends Turnstile keys without retaining the secret in state", async () => {
  const state = {
    formBusy: false,
    settingsApplyWarning: null,
    registrationMode: "disabled",
    turnstileEnabled: false,
    turnstileSiteKey: "",
    data: {
      settings: { persisted_version: 1, turnstile_secret_key_set: false },
      operationsMetrics: null
    }
  };
  let submittedSettings = null;
  let pageLoadAbortCount = 0;
  const settingsEvents = createSettingsEvents({
    state,
    renderApplication() {},
    handleSessionError() {
      return false;
    },
    abortCurrentPageLoad() {
      pageLoadAbortCount += 1;
    },
    readFormData: () => ({
      cpa_base_url: "http://127.0.0.1:8317",
      cpa_api_key: "",
      upstream_protocol: "responses",
      model: "grok-4.5",
      timeout_seconds: "120",
      mcp_global_search_concurrency: "16",
      mcp_user_search_concurrency: "4",
      proxy_url: "",
      turnstile_site_key: " public-site-key ",
      turnstile_secret_key: " private-secret-key "
    }),
    updateSettingsRequest: async (settingsPayload) => {
      submittedSettings = settingsPayload;
      return {
        registration_mode: "disabled",
        turnstile_enabled: true,
        turnstile_site_key: "public-site-key",
        turnstile_secret_key_set: true,
        operations_metrics_enabled: false
      };
    },
    notify() {}
  });
  const formElement = {
    elements: {
      proxy_enabled: { checked: false },
      registration_mode: { value: "disabled" },
      turnstile_enabled: { checked: true },
      turnstile_site_key: {},
      turnstile_secret_key: {},
      debug: { checked: false },
      operations_metrics_enabled: { checked: false }
    }
  };

  await settingsEvents.submitSettings(formElement);

  assert.equal(submittedSettings.turnstile_enabled, true);
  assert.equal(submittedSettings.expected_version, 1);
  assert.equal(submittedSettings.turnstile_site_key, "public-site-key");
  assert.equal(submittedSettings.turnstile_secret_key, "private-secret-key");
  assert.equal(pageLoadAbortCount, 1);
  assert.equal(state.turnstileEnabled, false);
  assert.equal(state.turnstileSiteKey, "");
  assert.equal(state.data.settings.turnstile_secret_key, undefined);
});

test("settings submit requires a new secret when the Turnstile site key changes", async () => {
  const state = {
    formBusy: false,
    settingsApplyWarning: null,
    data: {
      settings: {
        persisted_version: 1,
        turnstile_site_key: "original-site-key",
        turnstile_secret_key_set: true
      },
      operationsMetrics: null
    }
  };
  let updateCalled = false;
  let validationMessage = "";
  const settingsEvents = createSettingsEvents({
    state,
    renderApplication() {},
    handleSessionError() {
      return false;
    },
    readFormData: () => ({
      mcp_global_search_concurrency: "16",
      mcp_user_search_concurrency: "4",
      turnstile_site_key: "replacement-site-key",
      turnstile_secret_key: ""
    }),
    updateSettingsRequest: async () => {
      updateCalled = true;
      return {};
    }
  });
  const secretKeyInput = {
    setCustomValidity(message) {
      if (message) {
        validationMessage = message;
      }
    },
    reportValidity() {}
  };
  const formElement = {
    elements: {
      turnstile_enabled: { checked: true },
      turnstile_site_key: {},
      turnstile_secret_key: secretKeyInput
    }
  };

  await settingsEvents.submitSettings(formElement);

  assert.equal(updateCalled, false);
  assert.match(validationMessage, /Site Key/);
});

test("settings conflict reloads the newer persisted revision", async () => {
  const state = {
    formBusy: false,
    settingsApplyWarning: null,
    data: {
      settings: { persisted_version: 11, turnstile_secret_key_set: false },
      operationsMetrics: null
    }
  };
  const latestSettings = {
    persisted_version: 12,
    live_version: 12,
    model: "newer-model",
    operations_metrics_enabled: false
  };
  const notifications = [];
  const settingsEvents = createSettingsEvents({
    state,
    renderApplication() {},
    handleSessionError() {
      return false;
    },
    readFormData: () => ({
      cpa_base_url: "http://127.0.0.1:8317",
      upstream_protocol: "responses",
      model: "stale-model",
      timeout_seconds: "120",
      mcp_global_search_concurrency: "16",
      mcp_user_search_concurrency: "4",
      proxy_url: "",
      turnstile_site_key: "",
      turnstile_secret_key: ""
    }),
    updateSettingsRequest: async () => {
      throw Object.assign(new Error("conflict"), { status: 409 });
    },
    fetchSettingsRequest: async () => latestSettings,
    notify(title, message, type) {
      notifications.push({ title, message, type });
    }
  });
  const formElement = {
    elements: {
      proxy_enabled: { checked: false },
      registration_mode: { value: "disabled" },
      turnstile_enabled: { checked: false },
      turnstile_site_key: {},
      turnstile_secret_key: {},
      debug: { checked: false },
      operations_metrics_enabled: { checked: false }
    }
  };

  await settingsEvents.submitSettings(formElement);

  assert.equal(state.data.settings, latestSettings);
  assert.equal(state.formBusy, false);
  assert.equal(notifications.length, 1);
  assert.equal(notifications[0].title, "设置已刷新");
  assert.equal(notifications[0].type, "error");
});

test("API errors retain saved-not-applied version details", async (testContext) => {
  const originalFetch = globalThis.fetch;
  testContext.after(() => {
    globalThis.fetch = originalFetch;
  });

  globalThis.fetch = async () => new Response(JSON.stringify({
    code: savedNotAppliedCondition,
    error: "settings were saved but runtime components are not fully active",
    persisted_version: 12,
    live_version: 11
  }), {
    status: 500,
    headers: { "Content-Type": "application/json" }
  });

  await assert.rejects(
    panelAPI.request("/panel/v1/admin/settings", {
      method: "PATCH",
      body: { model: "grok-4.4" }
    }),
    (error) => {
      assert.equal(error.code, savedNotAppliedCondition);
      assert.equal(error.details.persisted_version, 12);
      assert.equal(error.details.live_version, 11);
      return true;
    }
  );
});

test("saved-not-applied warning survives a reconciliation read failure", async () => {
  const state = {
    settingsApplyWarning: null,
    registrationMode: "free",
    data: {
      settings: {
        model: "grok-4.3",
        persisted_version: 11,
        live_version: 11,
        apply_state: "applied"
      },
      operationsMetrics: null
    }
  };

  markSettingsSavedNotApplied(state, {
    persisted_version: 12,
    live_version: 11
  });
  await assert.rejects(
    reloadSavedNotAppliedSettings({
      state,
      fetchSettingsRequest: async () => {
        throw new Error("reload failed");
      }
    }),
    /reload failed/
  );

  assert.deepEqual(state.settingsApplyWarning, {
    persistedVersion: 12,
    liveVersion: 11,
    persistedValuesReloaded: false
  });
  assert.equal(state.data.settings.model, "grok-4.3");
  const warningMarkup = renderSettingsPage({
    ...state,
    pageLoading: false,
    formBusy: false,
    data: {
      ...state.data,
      models: []
    }
  });
  assert.match(warningMarkup, /设置已保存，尚未应用/);
  assert.match(warningMarkup, /当前表单可能仍显示提交前内容/);
  assert.match(warningMarkup, /表单模型（可能过期）/);
  assert.match(warningMarkup, /保存版本 12/);
  assert.match(warningMarkup, /其他运行时组件仍使用版本 11/);

  applySettingsResponseToState(state, {
    model: "grok-4.4",
    persisted_version: 12,
    live_version: 12,
    apply_state: "applied"
  });
  assert.equal(state.settingsApplyWarning, null);
});

test("saved-not-applied reconciliation replaces stale settings", async () => {
  const state = {
    registrationMode: "free",
    data: {
      settings: {
        model: "grok-4.3",
        persisted_version: 11,
        live_version: 11,
        apply_state: "applied"
      },
      operationsMetrics: { captured_at: "stale" }
    }
  };
  const persistedSettings = {
    model: "grok-4.4",
    registration_mode: "invite",
    operations_metrics_enabled: false,
    persisted_version: 12,
    live_version: 11,
    apply_state: "saved_not_applied"
  };

  const notification = await reloadSavedNotAppliedSettings({
    state,
    fetchSettingsRequest: async () => persistedSettings,
    errorDetails: {
      persisted_version: 12,
      live_version: 11
    }
  });

  assert.equal(state.data.settings, persistedSettings);
  assert.equal(state.data.settings.model, "grok-4.4");
  assert.equal(state.registrationMode, "free");
  assert.equal(state.data.operationsMetrics, null);
  assert.equal(notification.title, "设置已保存，尚未应用");
  assert.match(notification.message, /版本 12/);
  assert.match(notification.message, /版本为 11/);
  assert.doesNotMatch(notification.title, /保存失败/);
});

test("settings submit reloads persisted state after partial success", async () => {
  const state = {
    formBusy: false,
    settingsApplyWarning: null,
    registrationMode: "free",
    data: {
      settings: { model: "grok-4.3", persisted_version: 11 },
      operationsMetrics: { captured_at: "stale" }
    }
  };
  const persistedSettings = {
    model: "grok-4.4",
    registration_mode: "invite",
    operations_metrics_enabled: false,
    persisted_version: 12,
    live_version: 11,
    apply_state: "saved_not_applied"
  };
  const notifications = [];
  let renderCount = 0;
  const partialSuccessError = Object.assign(new Error("settings were saved but runtime components are not fully active"), {
    code: savedNotAppliedCondition,
    details: {
      persisted_version: 12,
      live_version: 11
    }
  });
  const settingsEvents = createSettingsEvents({
    state,
    renderApplication() {
      renderCount += 1;
    },
    handleSessionError() {
      return false;
    },
    updateSettingsRequest: async () => {
      throw partialSuccessError;
    },
    fetchSettingsRequest: async () => persistedSettings,
    readFormData: () => ({
      cpa_base_url: "http://127.0.0.1:8317",
      upstream_protocol: "responses",
      model: "grok-4.4",
      timeout_seconds: "120",
      mcp_global_search_concurrency: "16",
      mcp_user_search_concurrency: "4",
      proxy_url: "",
      cpa_api_key: ""
    }),
    notify(title, message, type) {
      notifications.push({ title, message, type });
    }
  });
  const formElement = {
    elements: {
      proxy_enabled: { checked: false },
      registration_mode: { value: "invite" },
      debug: { checked: false },
      operations_metrics_enabled: { checked: false }
    }
  };

  await settingsEvents.submitSettings(formElement);

  assert.equal(state.formBusy, false);
  assert.equal(state.data.settings, persistedSettings);
  assert.equal(state.registrationMode, "free");
  assert.equal(state.settingsApplyWarning, null);
  assert.equal(renderCount, 2);
  assert.equal(notifications.length, 1);
  assert.equal(notifications[0].title, "设置已保存，尚未应用");
  assert.equal(notifications[0].type, "error");
  assert.doesNotMatch(notifications[0].title, /保存失败/);
});
