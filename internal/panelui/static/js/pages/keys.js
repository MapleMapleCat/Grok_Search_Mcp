import { renderEmptyRow } from "../components/metric-card.js";
import { filteredKeys } from "../state.js";
import { escapeAttr, escapeHTML, formatDate, relativeTime } from "../utils.js";

export function renderKeys() {
  const filtered = filteredKeys();
  return `
    <div class="page-head">
      <div>
        <h2>API Keys</h2>
        <p>Manage your active Model Context Protocol keys and permissions.</p>
      </div>
      <button class="button" data-action="open-create-key" type="button">
        <span class="material-symbols-outlined">add</span>
        <span>Create New Key</span>
      </button>
    </div>
    <section class="card table-card">
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Prefix</th>
              <th>Status</th>
              <th>Created Date</th>
              <th>Last Used</th>
              <th class="right">Actions</th>
            </tr>
          </thead>
          <tbody>
            ${filtered.length ? filtered.map(renderKeyRow).join("") : renderEmptyRow("vpn_key", "No API keys yet", "Create a key to connect an MCP client.")}
          </tbody>
        </table>
      </div>
    </section>`;
}

export function renderKeyRow(key) {
  return `
    <tr>
      <td><strong>${escapeHTML(key.name || "Untitled Key")}</strong></td>
      <td class="mono muted">${escapeHTML(key.key_prefix || "mcp_...")}</td>
      <td>
        <label class="toggle" title="${key.enabled ? "Enabled" : "Disabled"}">
          <input type="checkbox" data-key-toggle="${escapeAttr(key.id)}" ${key.enabled ? "checked" : ""}>
          <span></span>
        </label>
      </td>
      <td>${formatDate(key.created_at)}</td>
      <td class="muted">${key.last_used_at ? relativeTime(key.last_used_at) : "Never"}</td>
      <td class="right">
        <span class="row-actions">
          <button class="mini-icon" data-action="key-usage" data-key-id="${escapeAttr(key.id)}" title="Usage" type="button"><span class="material-symbols-outlined">bar_chart</span></button>
          <button class="mini-icon" data-action="edit-key" data-key-id="${escapeAttr(key.id)}" title="Edit" type="button"><span class="material-symbols-outlined">edit</span></button>
          <button class="mini-icon danger" data-action="delete-key" data-key-id="${escapeAttr(key.id)}" title="Delete" type="button"><span class="material-symbols-outlined">delete</span></button>
        </span>
      </td>
    </tr>`;
}
