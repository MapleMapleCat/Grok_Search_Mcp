import { state } from "../state.js";

export function renderAuth() {
  const active = state.authMode;
  return `
    <main class="auth-screen">
      <section class="auth-card" aria-label="MCP Central 登录">
        <div class="auth-head">
          <div class="auth-logo"><span class="material-symbols-outlined">hub</span></div>
          <h1 class="auth-title">MCP Central</h1>
          <p class="auth-subtitle">Protocol Management Platform</p>
        </div>
        <div class="auth-body">
          <div class="auth-tabs" data-active="${active}">
            <span class="tab-indicator"></span>
            <button class="tab-button ${active === "login" ? "active" : ""}" data-action="auth-tab" data-tab="login" type="button">Login</button>
            <button class="tab-button ${active === "register" ? "active" : ""}" data-action="auth-tab" data-tab="register" type="button">Register</button>
          </div>
          <form id="login-form" class="form-stack ${active === "login" ? "" : "hidden"}">
            <div class="field">
              <label for="login-username">Username / ID</label>
              <div class="input-shell">
                <span class="material-symbols-outlined">person</span>
                <input id="login-username" name="username" class="input with-icon mono" autocomplete="username" placeholder="admin" required>
              </div>
            </div>
            <div class="field">
              <label for="login-password">Password</label>
              <div class="input-shell">
                <span class="material-symbols-outlined">lock</span>
                <input id="login-password" name="password" class="input with-icon mono" type="password" autocomplete="current-password" placeholder="••••••••" required>
              </div>
            </div>
            <button class="button" type="submit">
              <span>Authenticate</span>
              <span class="material-symbols-outlined">arrow_forward</span>
            </button>
          </form>
          <form id="register-form" class="form-stack ${active === "register" ? "" : "hidden"}">
            <div class="field">
              <label for="register-username">Desired Username</label>
              <div class="input-shell">
                <span class="material-symbols-outlined">badge</span>
                <input id="register-username" name="username" class="input with-icon mono" autocomplete="username" placeholder="new_user" required>
              </div>
            </div>
            <div class="field">
              <label for="register-password">Create Password</label>
              <div class="input-shell">
                <span class="material-symbols-outlined">key</span>
                <input id="register-password" name="password" class="input with-icon mono" type="password" autocomplete="new-password" placeholder="至少 8 位" minlength="8" required>
              </div>
            </div>
            <button class="button secondary" type="submit">
              <span class="material-symbols-outlined">person_add</span>
              <span>Create Account</span>
            </button>
          </form>
        </div>
      </section>
    </main>`;
}
