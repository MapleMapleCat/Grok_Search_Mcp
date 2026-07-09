import { isAdmin, state } from "../state.js";
import { escapeAttr, escapeHTML, formatDateTime } from "../utils.js";

export function renderServerSettings() {
  if (!isAdmin()) {
    return `
      <div class="page-head">
        <div>
          <h2>Server Settings</h2>
          <p>Admin role is required to manage server settings.</p>
        </div>
      </div>
      <section class="card empty">
        <div>
          <span class="material-symbols-outlined">lock</span>
          <h3>Admin required</h3>
          <p>当前账号没有管理员权限。</p>
        </div>
      </section>`;
  }

  const settings = state.serverSettings || {};
  const cpaBaseURL = settings.cpa_base_url || "";
  const model = settings.model || "grok-4.3";
  const timeoutSeconds = Number(settings.timeout_seconds) || 120;
  const proxyURL = settings.proxy_url || "";
  const proxyEnabled = Boolean(settings.proxy_enabled);
  const debugEnabled = Boolean(settings.debug);
  const registrationMode = settings.registration_mode || "free";
  const registrationModeLabel = registrationModeLabelText(registrationMode);
  const apiKeyConfigured = Boolean(settings.cpa_api_key_set);
  const apiKeyPreview = settings.cpa_api_key_preview || "configured";
  const lastUpdated = settings.updated_at ? formatDateTime(settings.updated_at) : "Not saved yet";

  return `
    <div class="page-head">
      <div>
        <h2>Server Settings</h2>
        <p>配置上游链接、密钥、模型、超时和代理设置。</p>
      </div>
      <button class="button secondary" data-action="reload-server-settings" type="button">
        <span class="material-symbols-outlined">refresh</span>
        <span>Refresh</span>
      </button>
    </div>

    <section class="grid settings-grid">
      <article class="card settings-card">
        <div class="card-head-inline">
          <div>
            <h3>Upstream Connection</h3>
            <p>这些设置会保存到数据库，并热更新后续 MCP 搜索请求使用的上游连接。</p>
          </div>
        </div>
        <form id="server-settings-form" class="form-stack settings-form">
          <div class="field">
            <label for="settings-cpa-base-url">Upstream Base URL</label>
            <input id="settings-cpa-base-url" name="cpa_base_url" class="input mono" value="${escapeAttr(cpaBaseURL)}" placeholder="http://127.0.0.1:8317" required>
            <span class="hint">会自动调用该地址下的 <span class="mono">/v1/responses</span>。</span>
          </div>

          <div class="field">
            <label for="settings-cpa-api-key">Upstream API Key</label>
            <input id="settings-cpa-api-key" name="cpa_api_key" class="input mono" type="password" autocomplete="off" placeholder="${apiKeyConfigured ? `Leave blank to keep ${escapeAttr(apiKeyPreview)}` : "Paste upstream API key"}">
            <span class="hint">${apiKeyConfigured ? `当前已配置：<span class="mono">${escapeHTML(apiKeyPreview)}</span>。留空则保留现有密钥。` : "尚未配置上游密钥，保存前必须填写。"}</span>
          </div>

          <div class="field two-column-fields">
            <span>
              <label for="settings-model">Default Model</label>
              <input id="settings-model" name="model" class="input mono" value="${escapeAttr(model)}" placeholder="grok-4.3" required>
            </span>
            <span>
              <label for="settings-timeout-seconds">Timeout Seconds</label>
              <input id="settings-timeout-seconds" name="timeout_seconds" class="input mono" type="number" min="1" step="1" value="${escapeAttr(timeoutSeconds)}" required>
            </span>
          </div>

          <div class="field">
            <label for="settings-registration-mode">Registration Mode</label>
            <select id="settings-registration-mode" name="registration_mode" class="select">
              <option value="free" ${registrationMode === "free" ? "selected" : ""}>Free registration</option>
              <option value="invite" ${registrationMode === "invite" ? "selected" : ""}>Invite-code registration</option>
              <option value="disabled" ${registrationMode === "disabled" ? "selected" : ""}>Registration disabled</option>
            </select>
            <span class="hint">只有在邀请码注册模式下，注册表单才要求邀请码并消耗邀请码额度；自由注册和禁止注册模式不会消耗任何邀请码。</span>
          </div>

          <div class="field-row">
            <span>
              <strong>Enable Proxy</strong>
              <span class="hint" style="display: block;">启用后，上游请求会通过下面的 HTTP/HTTPS 代理地址转发。</span>
            </span>
            <label class="toggle">
              <input type="checkbox" name="proxy_enabled" ${proxyEnabled ? "checked" : ""}>
              <span></span>
            </label>
          </div>

          <div class="field">
            <label for="settings-proxy-url">Proxy URL</label>
            <input id="settings-proxy-url" name="proxy_url" class="input mono" value="${escapeAttr(proxyURL)}" placeholder="http://127.0.0.1:7890">
            <span class="hint">支持 <span class="mono">http://</span> 或 <span class="mono">https://</span> 代理地址。关闭代理时可留空。</span>
          </div>

          <div class="field-row">
            <span>
              <strong>Debug Request Capture</strong>
              <span class="hint" style="display: block;">启用后完整捕获每次 MCP tools/call 的请求、响应和元数据，并在调用记录中查看 JSON。请避免在生产环境长期打开。</span>
            </span>
            <label class="toggle">
              <input type="checkbox" name="debug" ${debugEnabled ? "checked" : ""}>
              <span></span>
            </label>
          </div>

          <div class="form-actions">
            <button class="button secondary" data-action="reload-server-settings" type="button">
              <span class="material-symbols-outlined">restart_alt</span>
              <span>Reset</span>
            </button>
            <button class="button" type="submit">
              <span class="material-symbols-outlined">save</span>
              <span>Save Settings</span>
            </button>
          </div>
        </form>
      </article>

      <aside class="card settings-card">
        <div class="tutorial-card-head">
          <span class="material-symbols-outlined">dns</span>
          <div>
            <h3>Current Status</h3>
            <p>运行时配置摘要。</p>
          </div>
        </div>
        <div class="summary-list settings-summary-list">
          ${renderSettingsSummaryItem("Base URL", cpaBaseURL || "Not configured")}
          ${renderSettingsSummaryItem("Responses Endpoint", cpaBaseURL ? `${cpaBaseURL}/v1/responses` : "Not configured")}
          ${renderSettingsSummaryItem("API Key", apiKeyConfigured ? apiKeyPreview : "Not configured")}
          ${renderSettingsSummaryItem("Registration", registrationModeLabel)}
          ${renderSettingsSummaryItem("Proxy", proxyEnabled ? proxyURL || "Enabled, URL missing" : "Disabled")}
          ${renderSettingsSummaryItem("Last Updated", lastUpdated)}
        </div>
        <div class="warning-box settings-warning-box">
          <span class="material-symbols-outlined">info</span>
          <div>
            <strong>密钥不会明文回显。</strong>
            <p>保存新的 API Key 后，面板只显示掩码预览；需要更换密钥时重新粘贴即可。</p>
          </div>
        </div>
      </aside>
    </section>`;
}

function renderSettingsSummaryItem(label, value) {
  return `
    <div class="summary-item settings-summary-item">
      <span class="summary-label">${escapeHTML(label)}</span>
      <span class="mono">${escapeHTML(value)}</span>
    </div>`;
}

function registrationModeLabelText(registrationMode) {
  if (registrationMode === "invite") return "Invite-code registration";
  if (registrationMode === "disabled") return "Registration disabled";
  return "Free registration";
}
