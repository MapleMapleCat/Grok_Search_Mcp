import { quotaProgress, summaryItem } from "../components/metric-card.js";
import { state } from "../state.js";
import { escapeHTML, formatDateTime, rpmText } from "../utils.js";

export function renderAccount() {
  const tierBadge = state.user.tier_name
    ? `<span class="badge off">${escapeHTML(state.user.tier_name)}</span>`
    : "";
  return `
    <div class="page-head">
      <div>
        <h2>Account Settings</h2>
        <p>Review the active session, RPM and success limit.</p>
      </div>
      <button class="button secondary" data-action="logout" type="button"><span class="material-symbols-outlined">logout</span><span>Logout</span></button>
    </div>
    <section class="grid settings-grid">
      <div class="card panel">
        <div class="panel-head">
          <h3>Profile</h3>
          <span class="badge ${state.user.enabled ? "" : "error"}">${state.user.enabled ? "Enabled" : "Disabled"}</span>
        </div>
        <div class="summary-list">
          ${summaryItem("Username", escapeHTML(state.user.username))}
          ${summaryItem("Role", `<span class="badge ${state.user.role === "admin" ? "" : "off"}">${escapeHTML(state.user.role)}</span>`)}
          ${tierBadge ? summaryItem("Tier", tierBadge) : ""}
          ${summaryItem("User ID", `<span class="mono">${escapeHTML(state.user.id)}</span>`)}
          ${summaryItem("Created", formatDateTime(state.user.created_at))}
          ${summaryItem("Updated", formatDateTime(state.user.updated_at))}
        </div>
      </div>
      <div class="card panel">
        <div class="panel-head">
          <h3>Quotas</h3>
        </div>
        <div class="quota-list">
          <div class="quota-item">
            <div class="field-row">
              <span class="field-label">RPM</span>
              <span class="mono">${rpmText(state.user.rpm)} req/min</span>
            </div>
            <span class="hint">每分钟请求上限，所有 Key 共享</span>
          </div>
          ${quotaProgress("Success Limit", state.user.success_calls, state.user.success_limit, "successful calls")}
        </div>
      </div>
    </section>`;
}
