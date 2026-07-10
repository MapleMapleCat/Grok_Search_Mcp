import { escapeHTML, formatLimit, formatNumber } from "../utils.js";
import { renderIcon } from "../components/icons.js";
import { renderEmptyState, renderPageHeading } from "../components/loading.js";

export function renderTiersPage(state) {
  const createButton = `<button class="button button-primary" type="button" data-action="open-create-tier">${renderIcon("plus")} 新建方案</button>`;
  if (state.pageLoading && !state.data.tiers) {
    return `${renderPageHeading("配额方案", "集中管理每种方案的 RPM 与月度成功调用额度。", createButton)}<div class="tier-grid">${Array.from({ length: 6 }, () => '<div class="skeleton" style="height:250px;border-radius:16px"></div>').join("")}</div>`;
  }

  const tiers = state.data.tiers || [];
  return `
    ${renderPageHeading("配额方案", "方案决定用户的请求速率和月度成功调用额度；展示顺序仅用于排列。", createButton)}
    ${tiers.length === 0 ? `<div class="data-card">${renderEmptyState("layers", "还没有配额方案", "创建方案后即可向用户分配统一限额。", createButton)}</div>` : `
      <section class="tier-grid">${tiers.map((tier) => `
        <article class="tier-card">
          <span class="tier-kind ${String(tier.name).toLowerCase() === "tier0" ? "is-default" : ""}">${String(tier.name).toLowerCase() === "tier0" ? "默认方案" : "自定义方案"}</span>
          <h3>${escapeHTML(tier.name)}</h3>
          <div class="tier-limits">
            <div class="tier-limit"><span>Requests / min</span><strong>${escapeHTML(formatLimit(tier.rpm))}</strong></div>
            <div class="tier-limit"><span>Monthly success</span><strong>${escapeHTML(formatLimit(tier.success_limit))}</strong></div>
          </div>
          <footer class="tier-card-footer">
            <span class="tier-user-count">${escapeHTML(formatNumber(tier.user_count))} 位用户</span>
            <div class="table-actions">
              <button class="table-action" type="button" data-action="open-edit-tier" data-id="${escapeHTML(tier.id)}" aria-label="编辑配额方案">${renderIcon("edit")}</button>
              <button class="table-action is-danger" type="button" data-action="confirm-delete-tier" data-id="${escapeHTML(tier.id)}" aria-label="删除配额方案" ${Number(tier.user_count) > 0 ? "disabled" : ""}>${renderIcon("trash")}</button>
            </div>
          </footer>
        </article>
      `).join("")}</section>
    `}
  `;
}
