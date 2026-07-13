import { escapeHTML, formatDateTime } from "../utils.js";
import { renderIcon } from "../components/icons.js";
import { renderPageHeading } from "../components/loading.js";

export function renderSettingsPage(state) {
  if (state.pageLoading && !state.data.settings) {
    return `${renderPageHeading("服务设置", "热更新上游连接、默认模型、代理与注册策略。")}
      <div class="settings-layout"><div class="skeleton" style="height:620px;border-radius:16px"></div><div class="skeleton" style="height:330px;border-radius:16px"></div></div>`;
  }

  const settings = state.data.settings || {};
  const modelOptions = state.data.models || [];
  const knownModels = new Set(modelOptions.map((model) => model.id));
  const modelChoices = settings.model && !knownModels.has(settings.model)
    ? [{ id: settings.model }, ...modelOptions]
    : modelOptions;

  return `
    ${renderPageHeading("服务设置", "热更新上游连接、默认模型、代理与注册策略。")}
    <div class="settings-layout">
      <form class="data-card" data-form="settings">
        <section class="settings-section">
          <div class="settings-section-copy"><h3>上游连接</h3><p>配置 CPA 服务地址和访问凭证。留空 API Key 将保留当前值。</p></div>
          <div class="form-grid">
            <label class="field-group is-full"><span class="field-label">CPA Base URL</span><input class="text-input" name="cpa_base_url" type="url" value="${escapeHTML(settings.cpa_base_url || "")}" placeholder="http://127.0.0.1:8317" required></label>
            <label class="field-group is-full"><span class="field-label"><span>CPA API Key</span><span class="field-hint">${settings.cpa_api_key_set ? `已配置 ${escapeHTML(settings.cpa_api_key_preview || "")}` : "尚未配置"}</span></span><input class="text-input" name="cpa_api_key" type="password" autocomplete="new-password" placeholder="留空以保留现有密钥"></label>
          </div>
        </section>
        <section class="settings-section">
          <div class="settings-section-copy"><h3>模型与超时</h3><p>选择默认 Grok 模型，并设置单次上游请求的超时时间。</p></div>
          <div class="form-grid form-grid-align-fields">
            <label class="field-group"><span class="field-label"><span>默认模型</span><button class="button button-ghost button-sm" type="button" data-action="load-models">拉取模型</button></span>
              ${modelChoices.length > 0 ? `<select class="select-input" name="model" required>${modelChoices.map((model) => `<option value="${escapeHTML(model.id)}" ${model.id === settings.model ? "selected" : ""}>${escapeHTML(model.id)}</option>`).join("")}</select>` : `<input class="text-input" name="model" type="text" value="${escapeHTML(settings.model || "")}" placeholder="grok-4.3" required>`}
            </label>
            <label class="field-group"><span class="field-label">超时（秒）</span><input class="text-input" name="timeout_seconds" type="number" min="1" step="1" value="${escapeHTML(settings.timeout_seconds || 120)}" required></label>
          </div>
        </section>
        <section class="settings-section">
          <div class="settings-section-copy"><h3>网络代理</h3><p>在上游网络需要代理时启用。代理地址支持 HTTP 或 HTTPS。</p></div>
          <div>
            <label class="switch-row"><span class="switch-copy"><strong>启用显式代理</strong><span>关闭时使用默认网络路径</span></span><span class="switch"><input name="proxy_enabled" type="checkbox" ${settings.proxy_enabled ? "checked" : ""}><span class="switch-track"></span></span></label>
            <label class="field-group"><span class="field-label">代理 URL</span><input class="text-input" name="proxy_url" type="url" value="${escapeHTML(settings.proxy_url || "")}" placeholder="http://127.0.0.1:7890"></label>
          </div>
        </section>
        <section class="settings-section">
          <div class="settings-section-copy"><h3>访问策略</h3><p>控制公开注册入口与调试日志。调试模式可能记录更多请求信息。</p></div>
          <div class="form-grid">
            <label class="field-group"><span class="field-label">注册模式</span><select class="select-input" name="registration_mode">
              <option value="free" ${settings.registration_mode === "free" ? "selected" : ""}>自由注册</option>
              <option value="invite" ${settings.registration_mode === "invite" ? "selected" : ""}>邀请注册</option>
              <option value="disabled" ${settings.registration_mode === "disabled" ? "selected" : ""}>关闭注册</option>
            </select></label>
            <label class="switch-row"><span class="switch-copy"><strong>调试模式</strong><span>输出扩展诊断信息</span></span><span class="switch"><input name="debug" type="checkbox" ${settings.debug ? "checked" : ""}><span class="switch-track"></span></span></label>
          </div>
        </section>
        <footer class="settings-footer"><button class="button button-primary" type="submit" ${state.formBusy ? "disabled" : ""}>${state.formBusy ? `${renderIcon("refresh")} 正在保存` : `${renderIcon("check")} 保存并应用`}</button></footer>
      </form>

      <aside class="info-card">
        <div class="info-card-top"><span class="info-card-icon">${renderIcon("shield")}</span><h3>运行时热更新</h3><p>这些设置保存后会立即应用到上游客户端，无需重启 grok-mcp 服务。</p></div>
        <div class="info-list">
          <div class="info-row"><span>服务版本</span><strong>${escapeHTML(settings.version || "未知")}</strong></div>
          <div class="info-row"><span>当前模型</span><strong>${escapeHTML(settings.model || "未配置")}</strong></div>
          <div class="info-row"><span>API Key</span><strong>${settings.cpa_api_key_set ? "已安全配置" : "未配置"}</strong></div>
          <div class="info-row"><span>代理</span><strong>${settings.proxy_enabled ? "已启用" : "直连"}</strong></div>
          <div class="info-row"><span>注册</span><strong>${escapeHTML(getRegistrationModeLabel(settings.registration_mode))}</strong></div>
          <div class="info-row"><span>最后更新</span><strong>${escapeHTML(formatDateTime(settings.updated_at))}</strong></div>
        </div>
      </aside>
    </div>
  `;
}

function getRegistrationModeLabel(mode) {
  const labels = { free: "自由注册", invite: "邀请注册", disabled: "关闭注册" };
  return labels[mode] || "未知";
}
