import { escapeHTML } from "../utils.js";
import { renderIcon } from "./icons.js";

export function renderAuthView(state) {
  const authenticationSettingsReady = state.authenticationSettingsStatus === "ready";
  const registrationAvailable = state.registrationMode !== "disabled";
  const activeMode = registrationAvailable ? state.authMode : "login";

  return `
    <main class="auth-layout">
      <section class="auth-panel">
        <div class="auth-brand">
          <span class="brand-symbol" aria-hidden="true"></span>
          <span>Grok Search MCP Control</span>
        </div>

        <div class="auth-panel-inner">
          <p class="eyebrow">Secure control plane</p>
          <h1 class="auth-title">Grok Search MCP<br>控制平面</h1>
          <p class="auth-copy">集中管理 MCP 访问密钥、用量策略和 Grok 上游连接。</p>

          ${authenticationSettingsReady ? `
            ${registrationAvailable ? `
              <div class="auth-tabs" role="tablist" aria-label="账户操作">
                <button class="auth-tab ${activeMode === "login" ? "is-active" : ""}" type="button" role="tab" aria-selected="${activeMode === "login"}" data-action="switch-auth" data-mode="login">登录</button>
                <button class="auth-tab ${activeMode === "register" ? "is-active" : ""}" type="button" role="tab" aria-selected="${activeMode === "register"}" data-action="switch-auth" data-mode="register">注册</button>
              </div>
            ` : ""}
            ${activeMode === "register" ? renderRegisterForm(state) : renderLoginForm(state)}
          ` : renderAuthenticationSettingsState(state)}
        </div>
      </section>

      <aside class="auth-visual" aria-hidden="true">
        <div class="auth-orbit"></div>
        <div class="auth-visual-content">
          <span class="visual-kicker">${renderIcon("spark")} Realtime intelligence</span>
          <h2 class="visual-title">Search.<span>Observe.</span>Control.</h2>
          <div class="visual-stats">
            <div class="visual-stat"><strong>3</strong><span>MCP tools</span></div>
            <div class="visual-stat"><strong>Live</strong><span>Usage telemetry</span></div>
            <div class="visual-stat"><strong>JWT</strong><span>Secure session</span></div>
          </div>
        </div>
      </aside>
    </main>
  `;
}

function renderAuthenticationSettingsState(state) {
  const loading = state.authenticationSettingsStatus === "loading";
  const description = loading
    ? "正在加载当前登录访问策略..."
    : escapeHTML(state.authenticationSettingsError || "无法加载当前登录访问策略，请稍后重试。");
  return `
    <div class="auth-settings-state" role="${loading ? "status" : "alert"}">
      <span class="auth-settings-state-icon">${renderIcon(loading ? "refresh" : "alert")}</span>
      <p>${description}</p>
      ${loading ? "" : `<button class="button button-secondary button-wide" type="button" data-action="retry-authentication-settings">重新加载</button>`}
    </div>
  `;
}

function renderLoginForm(state) {
  const turnstileEnabled = Boolean(state.turnstileEnabled && state.turnstileSiteKey);
  return `
    <form class="auth-form" data-form="login" novalidate>
      ${state.authError ? `<div class="inline-alert">${renderIcon("alert")}<span>${escapeHTML(state.authError)}</span></div>` : ""}
      <label class="field-group">
        <span class="field-label">用户名</span>
        <input class="text-input" name="username" type="text" autocomplete="username" maxlength="128" placeholder="输入用户名" required autofocus>
      </label>
      <label class="field-group">
        <span class="field-label">密码</span>
        <span class="password-wrap">
          <input class="text-input" id="login-password" name="password" type="password" autocomplete="current-password" minlength="8" maxlength="72" placeholder="输入密码" required>
          <button class="input-icon-button" type="button" data-action="toggle-password" data-target="login-password" aria-label="显示或隐藏密码">${renderIcon("eye")}</button>
        </span>
      </label>
      ${turnstileEnabled ? `
        <div class="turnstile-field">
          <input name="turnstile_token" type="hidden" value="">
          <div class="turnstile-widget" data-turnstile-container data-turnstile-state="loading" aria-busy="true" aria-label="人机验证"></div>
          <div class="turnstile-status-row">
            <p class="turnstile-status" data-turnstile-status aria-live="polite">正在加载人机验证组件...</p>
            <button class="turnstile-retry" type="button" data-action="retry-turnstile" hidden>重新加载</button>
          </div>
        </div>
      ` : ""}
      <button class="button button-primary button-wide auth-submit" type="submit" ${state.authBusy || turnstileEnabled ? "disabled" : ""}>
        ${state.authBusy ? `${renderIcon("refresh")} 正在登录` : `进入控制台 ${renderIcon("arrowRight")}`}
      </button>
    </form>
  `;
}

function renderRegisterForm(state) {
  return `
    <form class="auth-form" data-form="register" novalidate>
      ${state.authError ? `<div class="inline-alert">${renderIcon("alert")}<span>${escapeHTML(state.authError)}</span></div>` : ""}
      <label class="field-group">
        <span class="field-label">用户名</span>
        <input class="text-input" name="username" type="text" autocomplete="username" maxlength="128" placeholder="创建用户名" required autofocus>
      </label>
      <label class="field-group">
        <span class="field-label">密码</span>
        <span class="password-wrap">
          <input class="text-input" id="register-password" name="password" type="password" autocomplete="new-password" minlength="8" maxlength="72" placeholder="设置安全密码" required>
          <button class="input-icon-button" type="button" data-action="toggle-password" data-target="register-password" aria-label="显示或隐藏密码">${renderIcon("eye")}</button>
        </span>
      </label>
      ${state.registrationMode === "invite" ? `
        <label class="field-group">
          <span class="field-label">邀请码</span>
          <input class="text-input mono-value" name="invite_code" type="text" autocomplete="off" placeholder="输入管理员提供的邀请码" required>
        </label>
      ` : ""}
      <button class="button button-primary button-wide auth-submit" type="submit" ${state.authBusy ? "disabled" : ""}>
        ${state.authBusy ? `${renderIcon("refresh")} 正在完成本地验证` : `创建账户 ${renderIcon("arrowRight")}`}
      </button>
    </form>
  `;
}
