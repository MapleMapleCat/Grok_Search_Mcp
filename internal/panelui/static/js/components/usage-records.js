import { escapeHTML, formatDateTime, formatNumber } from "../utils.js";
import { renderIcon } from "./icons.js";
import { renderEmptyState } from "./loading.js";

export function renderUsageRecords(records = []) {
  if (!records || records.length === 0) {
    return renderEmptyState("activity", "没有调用记录", "所选时间范围内还没有可展示的 MCP 调用。", "");
  }

  return `
    <div class="data-table-wrap"><table class="data-table">
      <thead><tr><th>工具</th><th>结果</th><th>耗时</th><th>密钥 ID</th><th>时间</th><th class="usage-debug-column">调试详情</th></tr></thead>
      <tbody>${records.map((record) => `
        <tr>
          <td><div class="primary-cell"><span class="cell-icon is-green">${renderIcon("activity")}</span><span class="cell-copy"><strong>${escapeHTML(record.tool_name || "unknown")}</strong><span>#${escapeHTML(record.id)}</span></span></div></td>
          <td><span class="status-badge ${record.success ? "is-enabled" : "is-failed"}">${record.success ? "成功" : "失败"}</span></td>
          <td>${escapeHTML(formatNumber(record.duration_ms))} ms</td>
          <td class="mono-value">${escapeHTML(record.key_id || "--")}</td>
          <td>${escapeHTML(formatDateTime(record.timestamp))}</td>
          <td class="usage-debug-column">${record.debug_json ? `<button class="table-action is-debug" type="button" data-action="view-debug-json" data-record-id="${escapeHTML(record.id)}" aria-label="查看调用 #${escapeHTML(record.id)} 的调试详情" title="查看调试详情">${renderIcon("code")}</button>` : '<span class="usage-debug-empty" title="该调用发生时没有采集调试信息">--</span>'}</td>
        </tr>
      `).join("")}</tbody>
    </table></div>
  `;
}
