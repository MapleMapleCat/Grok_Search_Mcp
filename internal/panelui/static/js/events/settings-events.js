import { fetchModels, fetchSettings, updateSettings } from "../api.js";
import { renderIcon } from "../components/icons.js";
import { showToast } from "../components/toast.js";
import { renderSafeHTML } from "../safe-html.js";
import {
  applySettingsResponseToState,
  markSettingsSavedNotApplied,
  reloadSavedNotAppliedSettings,
  savedNotAppliedCondition
} from "../settings-apply-state.js";
import { createFormDataObject } from "../utils.js";
import { getErrorMessage } from "./event-helpers.js";

export function createSettingsEvents({
  state,
  renderApplication,
  handleSessionError,
  abortCurrentPageLoad = () => {},
  fetchSettingsRequest = fetchSettings,
  updateSettingsRequest = updateSettings,
  readFormData = createFormDataObject,
  notify = showToast
}) {
  async function submitSettings(formElement) {
    const formData = readFormData(formElement);
    const expectedSettingsVersion = Number(state.data.settings?.persisted_version);
    if (!Number.isSafeInteger(expectedSettingsVersion) || expectedSettingsVersion < 0) {
      notify("无法保存设置", "当前设置版本不可用，请刷新页面后重试。", "error");
      return;
    }
    const globalSearchConcurrency = Number(formData.mcp_global_search_concurrency);
    const userSearchConcurrency = Number(formData.mcp_user_search_concurrency);
    if (userSearchConcurrency > globalSearchConcurrency) {
      const userConcurrencyInput = formElement.elements.mcp_user_search_concurrency;
      userConcurrencyInput.setCustomValidity("单用户搜索并发不得超过全局搜索并发。");
      userConcurrencyInput.reportValidity();
      userConcurrencyInput.setCustomValidity("");
      return;
    }
    const turnstileEnabled = Boolean(formElement.elements.turnstile_enabled?.checked);
    const turnstileSiteKey = String(formData.turnstile_site_key || "").trim();
    const turnstileSecretKey = String(formData.turnstile_secret_key || "").trim();
    const currentTurnstileSiteKey = String(state.data.settings?.turnstile_site_key || "").trim();
    if (turnstileEnabled && !turnstileSiteKey) {
      const siteKeyInput = formElement.elements.turnstile_site_key;
      siteKeyInput?.setCustomValidity("启用 Turnstile 时必须填写 Site Key。");
      siteKeyInput?.reportValidity();
      siteKeyInput?.setCustomValidity("");
      return;
    }
    if (
      state.data.settings?.turnstile_secret_key_set
      && turnstileSiteKey !== currentTurnstileSiteKey
      && !turnstileSecretKey
    ) {
      const secretKeyInput = formElement.elements.turnstile_secret_key;
      secretKeyInput?.setCustomValidity("更换 Site Key 时必须同时填写对应的 Secret Key。");
      secretKeyInput?.reportValidity();
      secretKeyInput?.setCustomValidity("");
      return;
    }
    if (turnstileEnabled && !state.data.settings?.turnstile_secret_key_set && !turnstileSecretKey) {
      const secretKeyInput = formElement.elements.turnstile_secret_key;
      secretKeyInput?.setCustomValidity("首次启用 Turnstile 时必须填写 Secret Key。");
      secretKeyInput?.reportValidity();
      secretKeyInput?.setCustomValidity("");
      return;
    }

    const settingsPayload = {
      expected_version: expectedSettingsVersion,
      cpa_base_url: String(formData.cpa_base_url || "").trim(),
      upstream_protocol: String(formData.upstream_protocol || ""),
      model: String(formData.model || "").trim(),
      timeout_seconds: Number(formData.timeout_seconds),
      mcp_global_search_concurrency: globalSearchConcurrency,
      mcp_user_search_concurrency: userSearchConcurrency,
      proxy_url: String(formData.proxy_url || "").trim(),
      proxy_enabled: formElement.elements.proxy_enabled.checked,
      registration_mode: formElement.elements.registration_mode.value,
      turnstile_enabled: turnstileEnabled,
      turnstile_site_key: turnstileSiteKey,
      debug: formElement.elements.debug.checked,
      operations_metrics_enabled: formElement.elements.operations_metrics_enabled.checked
    };
    const apiKey = String(formData.cpa_api_key || "").trim();
    if (apiKey) {
      settingsPayload.cpa_api_key = apiKey;
    }
    if (turnstileSecretKey) {
      settingsPayload.turnstile_secret_key = turnstileSecretKey;
    }

    abortCurrentPageLoad();
    state.formBusy = true;
    renderApplication();
    try {
      const updatedSettings = await updateSettingsRequest(settingsPayload);
      applySettingsResponseToState(state, updatedSettings);
      state.formBusy = false;
      renderApplication();
      notify("设置已应用", "运行时配置与登录访问策略已更新。", "success");
    } catch (error) {
      state.formBusy = false;
      if (handleSessionError(error)) {
        return;
      }
      if (error?.status === 409) {
        try {
          const latestSettings = await fetchSettingsRequest();
          applySettingsResponseToState(state, latestSettings);
          renderApplication();
          notify("设置已刷新", "其他会话已更新设置，请确认最新值后重新提交。", "error");
        } catch (reloadError) {
          if (!handleSessionError(reloadError)) {
            renderApplication();
            notify("设置版本冲突", getErrorMessage(reloadError), "error");
          }
        }
        return;
      }
      if (error?.code === savedNotAppliedCondition) {
        markSettingsSavedNotApplied(state, error?.details);
        try {
          const notification = await reloadSavedNotAppliedSettings({
            state,
            fetchSettingsRequest,
            errorDetails: error?.details
          });
          renderApplication();
          notify(notification.title, notification.message, "error");
        } catch (reloadError) {
          if (!handleSessionError(reloadError)) {
            renderApplication();
            notify(
              "设置已保存，尚未应用",
              `设置已经持久化，但无法重新读取最新值：${getErrorMessage(reloadError)}`,
              "error"
            );
          }
        }
        return;
      }
      renderApplication();
      notify("保存失败", getErrorMessage(error), "error");
    }
  }

  async function loadAvailableModels(actionElement) {
    const previousContentNodes = Array.from(
      actionElement.childNodes,
      (childNode) => childNode.cloneNode(true)
    );
    actionElement.disabled = true;
    renderSafeHTML(actionElement, `${renderIcon("refresh")} 正在拉取`);
    try {
      const modelResponse = await fetchModels();
      state.data.models = modelResponse?.models || [];
      renderApplication();
      showToast("模型列表已更新", `发现 ${state.data.models.length} 个可用 Grok 模型。`, "success");
    } catch (error) {
      if (!handleSessionError(error)) {
        actionElement.disabled = false;
        actionElement.replaceChildren(...previousContentNodes);
        showToast("模型加载失败", getErrorMessage(error), "error");
      }
    }
  }

  return {
    submitSettings,
    loadAvailableModels
  };
}
