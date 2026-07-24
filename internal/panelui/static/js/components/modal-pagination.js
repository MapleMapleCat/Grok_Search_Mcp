import { COLLECTION_PAGE_SIZE_OPTIONS } from "../pagination-config.js";
import { escapeHTML, formatNumber } from "../utils.js";

export function renderModalPagination({
  pagination,
  itemCount,
  leadingContent,
  pageAction,
  pageSizeAction
}) {
  const loadingRecords = Boolean(pagination.loadingRecords);
  const previousPageAvailable = (pagination.previousCursors?.length || 0) > 0;
  const nextPageAvailable = Boolean(pagination.hasMore && pagination.nextCursor);
  const currentPage = (pagination.previousCursors?.length || 0) + 1;

  return `
    ${leadingContent}
    <span class="muted modal-pagination-status">第 ${escapeHTML(formatNumber(currentPage))} 页 · 本页 ${escapeHTML(formatNumber(itemCount))} 条</span>
    <label class="pagination-page-size">
      <span>每页</span>
      <select class="select-input" data-action="${escapeHTML(pageSizeAction)}" aria-label="每页显示条数" ${loadingRecords ? "disabled" : ""}>
        ${COLLECTION_PAGE_SIZE_OPTIONS.map((pageSize) => `<option value="${pageSize}" ${Number(pagination.pageSize) === pageSize ? "selected" : ""}>${pageSize} 条</option>`).join("")}
      </select>
    </label>
    <button class="button button-secondary" type="button" data-action="${escapeHTML(pageAction)}" data-direction="previous" ${!loadingRecords && previousPageAvailable ? "" : "disabled"}>上一页</button>
    <button class="button button-primary" type="button" data-action="${escapeHTML(pageAction)}" data-direction="next" ${!loadingRecords && nextPageAvailable ? "" : "disabled"}>下一页</button>
  `;
}
