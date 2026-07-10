import { calculatePercent, escapeHTML, formatNumber } from "../utils.js";
import { renderEmptyState } from "./loading.js";

export function renderToolBreakdown(byTool = {}) {
  const toolEntries = Object.entries(byTool || {}).sort((firstEntry, secondEntry) => secondEntry[1] - firstEntry[1]);
  if (toolEntries.length === 0) {
    return renderEmptyState("activity", "暂无工具调用", "完成首次 MCP 工具调用后，这里会显示工具分布。", "");
  }

  const maximumCalls = Math.max(1, ...toolEntries.map(([, calls]) => Number(calls || 0)));
  return `<div class="tool-list">${toolEntries.slice(0, 6).map(([toolName, calls], toolIndex) => `
    <div class="tool-row">
      <span class="tool-rank">${String(toolIndex + 1).padStart(2, "0")}</span>
      <span class="tool-copy"><strong>${escapeHTML(toolName)}</strong><span class="tool-bar"><span style="width:${calculatePercent(calls, maximumCalls)}%"></span></span></span>
      <span class="tool-count">${escapeHTML(formatNumber(calls))}</span>
    </div>
  `).join("")}</div>`;
}
