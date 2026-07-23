import { COLLECTION_PAGE_SIZE_OPTIONS } from "../pagination-config.js";
import { escapeHTML, formatDateTime, formatNumber } from "../utils.js";
import { renderIcon } from "./icons.js";

export function renderInviteRedemptionsModal(modal, renderModalFrame) {
  const inviteCode = modal.inviteCode || {};
  const inviteLabel = inviteCode.code_prefix || "该邀请码";

  if (modal.loading) {
    return renderModalFrame({
      title: "邀请码注册记录",
      description: `正在加载 ${inviteLabel} 的注册记录。`,
      body: `<div class="inline-alert">${renderIcon("activity")}<span>正在读取邀请码创建的用户和注册时间，请稍候。</span></div>`,
      footer: `<button class="button button-secondary" type="button" data-action="close-modal">关闭</button>`,
      wide: true,
      modalClass: "invite-redemptions-modal"
    });
  }

  if (modal.error) {
    return renderModalFrame({
      title: "无法加载注册记录",
      description: `邀请码 ${inviteLabel} 的审计记录未能读取。`,
      body: `<div class="inline-alert">${renderIcon("alert")}<span>${escapeHTML(modal.error)}</span></div>`,
      footer: `<button class="button button-secondary" type="button" data-action="close-modal">关闭</button>`,
      wide: true,
      modalClass: "invite-redemptions-modal"
    });
  }

  const redemptions = Array.isArray(modal.redemptions) ? modal.redemptions : [];
  const redemptionList = redemptions.length === 0
    ? `<div class="inline-alert">${renderIcon("activity")}<span>该邀请码尚未创建任何用户。</span></div>`
    : `<div class="invite-redemptions-list">${redemptions.map((redemption) => `
        <article class="invite-redemption-row">
          <div class="invite-redemption-copy">
            <strong>${escapeHTML(redemption.username || "未知用户")}</strong>
            <span>${escapeHTML(redemption.user_id || "未知用户 ID")}</span>
          </div>
          <time datetime="${escapeHTML(redemption.redeemed_at || "")}">${escapeHTML(formatDateTime(redemption.redeemed_at))}</time>
        </article>
      `).join("")}</div>`;
  const body = modal.loadingRecords
    ? '<div class="skeleton invite-redemptions-skeleton"></div>'
    : redemptionList;
  const loadingRecords = Boolean(modal.loadingRecords);
  const previousPageAvailable = (modal.previousCursors?.length || 0) > 0;
  const nextPageAvailable = Boolean(modal.hasMore && modal.nextCursor);
  const currentPage = (modal.previousCursors?.length || 0) + 1;
  const footer = `
    <button class="button button-secondary" type="button" data-action="close-modal" >关闭</button>
    <span class="muted modal-pagination-status">第 ${escapeHTML(formatNumber(currentPage))} 页 · 本页 ${escapeHTML(formatNumber(redemptions.length))} 条</span>
    <label class="pagination-page-size">
      <span>每页</span>
      <select class="select-input" data-action="change-invite-redemptions-page-size" aria-label="每页显示条数" ${loadingRecords ? "disabled" : ""}>
        ${COLLECTION_PAGE_SIZE_OPTIONS.map((pageSize) => `<option value="${pageSize}" ${Number(modal.pageSize) === pageSize ? "selected" : ""}>${pageSize} 条</option>`).join("")}
      </select>
    </label>
    <button class="button button-secondary" type="button" data-action="change-invite-redemptions-page" data-direction="previous" ${!loadingRecords && previousPageAvailable ? "" : "disabled"}>上一页</button>
    <button class="button button-primary" type="button" data-action="change-invite-redemptions-page" data-direction="next" ${!loadingRecords && nextPageAvailable ? "" : "disabled"}>下一页</button>
  `;

  return renderModalFrame({
    title: "邀请码注册记录",
    description: `${inviteLabel} · 按注册时间倒序显示`,
    body,
    footer,
    wide: true,
    modalClass: "invite-redemptions-modal"
  });
}
