import { escapeHTML } from "../utils.js";
import { renderIcon } from "./icons.js";

export function renderStatusBadge(enabled, enabledLabel = "已启用", disabledLabel = "已停用") {
  return `<span class="status-badge ${enabled ? "is-enabled" : "is-disabled"}">${escapeHTML(enabled ? enabledLabel : disabledLabel)}</span>`;
}

export function renderPageHeading(title, description, actions = "") {
  return `
    <div class="page-heading">
      <div>
        <h1>${escapeHTML(title)}</h1>
        <p>${escapeHTML(description)}</p>
      </div>
      ${actions ? `<div class="heading-actions">${actions}</div>` : ""}
    </div>
  `;
}

export function renderEmptyState(iconName, title, description, action = "") {
  return `
    <div class="empty-state">
      <div class="empty-state-inner">
        <div class="empty-state-icon">${renderIcon(iconName)}</div>
        <h3>${escapeHTML(title)}</h3>
        <p>${escapeHTML(description)}</p>
        ${action}
      </div>
    </div>
  `;
}

export function renderLoadingTable(columnCount = 6, rowCount = 5) {
  const columns = Array.from({ length: columnCount }, () => '<th><div class="skeleton" style="width:64px;height:10px"></div></th>').join("");
  const rows = Array.from({ length: rowCount }, () => `
    <tr>${Array.from({ length: columnCount }, (_, columnIndex) => `<td><div class="skeleton" style="width:${columnIndex === 0 ? 150 : 78}px;height:${columnIndex === 0 ? 32 : 12}px"></div></td>`).join("")}</tr>
  `).join("");
  return `<div class="data-card"><div class="data-table-wrap"><table class="data-table"><thead><tr>${columns}</tr></thead><tbody>${rows}</tbody></table></div></div>`;
}
