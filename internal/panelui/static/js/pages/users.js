import { escapeHTML, formatDateTime, formatLimit, formatNumber, getInitials, normalizeSearchValue } from "../utils.js";
import { renderIcon } from "../components/icons.js";
import { renderEmptyState, renderLoadingTable, renderPageHeading, renderStatusBadge } from "../components/loading.js";
import { renderCollectionPagination } from "../components/pagination.js";

function doesUserMatchSearch(user, normalizedSearch) {
  if (!normalizedSearch) {
    return true;
  }
  return normalizeSearchValue(user?.username).includes(normalizedSearch)
    || normalizeSearchValue(user?.id).includes(normalizedSearch);
}

export function renderUsersPage(state) {
  const users = state.data.users || [];
  const normalizedSearch = normalizeSearchValue(state.filters.userSearch);
  const filteredUsers = users.filter((user) => doesUserMatchSearch(user, normalizedSearch));
  const hasMatchingUsers = filteredUsers.length > 0;
  const paginationMarkup = renderCollectionPagination("users", state.pagination.users, filteredUsers.length);
  const toolbar = `
    <div class="toolbar">
      <label class="search-field">${renderIcon("search")}<span class="sr-only">搜索用户</span><input class="text-input" type="search" value="${escapeHTML(state.filters.userSearch)}" placeholder="搜索用户名或 ID" data-filter="user-search"></label>
      <span class="muted" style="font-size:11px">共 ${formatNumber(state.pagination.users.totalCount || users.length)} 位用户</span>
    </div>
  `;

  if (state.pageLoading && !state.data.users) {
    return `${renderPageHeading("用户管理", "管理账户角色、启用状态、配额方案与现有会话。")}${renderLoadingTable(7, 6)}`;
  }

  return `
    ${renderPageHeading("用户管理", "管理账户角色、启用状态、配额方案与现有会话。")}
    ${toolbar}
    <div class="data-card" data-user-search-results>
      <div data-user-search-empty ${hasMatchingUsers ? "hidden" : ""}>
        ${renderEmptyState("users", "没有匹配的用户", "调整搜索条件后再试。", "")}
      </div>
      <div class="data-table-wrap" data-user-search-table ${hasMatchingUsers ? "" : "hidden"}><table class="data-table">
          <thead><tr><th>用户</th><th>角色</th><th>配额方案</th><th>状态</th><th>本月成功调用</th><th>创建时间</th><th aria-label="操作"></th></tr></thead>
          <tbody>${users.map((user) => {
            const normalizedUsername = normalizeSearchValue(user.username);
            const normalizedUserIdentifier = normalizeSearchValue(user.id);
            const userMatchesSearch = doesUserMatchSearch(user, normalizedSearch);
            return `
            <tr data-user-search-username="${escapeHTML(normalizedUsername)}" data-user-search-id="${escapeHTML(normalizedUserIdentifier)}" ${userMatchesSearch ? "" : "hidden"}>
              <td><div class="primary-cell"><span class="cell-icon">${escapeHTML(getInitials(user.username))}</span><span class="cell-copy"><strong>${escapeHTML(user.username)}</strong><span>${escapeHTML(user.id)}</span></span></div></td>
              <td><span class="role-badge ${user.role === "admin" ? "is-admin" : "is-user"}">${user.role === "admin" ? "管理员" : "用户"}</span></td>
              <td><span class="tier-badge">${escapeHTML(user.tier_name || "未分配")}</span></td>
              <td>${renderStatusBadge(Boolean(user.enabled), "正常", "已禁用")}</td>
              <td>${user.limits_unavailable ? '<span class="text-danger">限额不可用</span>' : `${escapeHTML(formatNumber(user.success_calls))} / ${escapeHTML(formatLimit(user.success_limit))}`}</td>
              <td>${escapeHTML(formatDateTime(user.created_at))}</td>
              <td><div class="table-actions">
                <button class="table-action" type="button" data-action="open-user-usage" data-id="${escapeHTML(user.id)}" aria-label="查看用户用量">${renderIcon("chart")}</button>
                <button class="table-action" type="button" data-action="open-edit-user" data-id="${escapeHTML(user.id)}" aria-label="编辑用户">${renderIcon("edit")}</button>
                <button class="table-action is-danger" type="button" data-action="confirm-delete-user" data-id="${escapeHTML(user.id)}" aria-label="删除用户" ${user.id === state.user?.id ? "disabled" : ""}>${renderIcon("trash")}</button>
              </div></td>
            </tr>
          `;
          }).join("")}</tbody>
        </table></div>
      ${paginationMarkup ? `<div data-user-search-pagination ${hasMatchingUsers ? "" : "hidden"}>${paginationMarkup}</div>` : ""}
    </div>
  `;
}
