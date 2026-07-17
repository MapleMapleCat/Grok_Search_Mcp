import { escapeHTML, formatDateTime } from "../utils.js";
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
  const body = redemptions.length === 0
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

  return renderModalFrame({
    title: "邀请码注册记录",
    description: `${inviteLabel} · 共 ${redemptions.length} 位用户`,
    body,
    footer: `<button class="button button-secondary" type="button" data-action="close-modal">关闭</button>`,
    wide: true,
    modalClass: "invite-redemptions-modal"
  });
}
