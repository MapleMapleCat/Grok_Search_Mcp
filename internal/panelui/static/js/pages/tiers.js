import { escapeHTML, formatLimit, formatNumber } from "../utils.js";
import { renderIcon } from "../components/icons.js";
import { renderEmptyState, renderPageHeading } from "../components/loading.js";
import { renderCollectionPagination } from "../components/pagination.js";

export function renderTiersPage(state) {
  const createButton = `<button class="button button-primary" type="button" data-action="open-create-tier">${renderIcon("plus")} 创建配额方案</button>`;
  if (state.pageLoading && !state.data.tiers) {
    return renderTiersLoading(createButton);
  }

  const tiers = state.data.tiers || [];
  const assignedUserCount = state.pagination.tiers.assignedUserCount
    || tiers.reduce((totalUserCount, tier) => totalUserCount + getNonNegativeNumber(tier.user_count), 0);
  const totalTierCount = state.pagination.tiers.totalCount || tiers.length;
  const defaultTier = state.data.defaultTier || tiers.find((tier) => isDefaultTier(tier));

  return `
    <div class="tiers-page">
      ${renderPageHeading("配额方案", "为不同用户群体设置清晰、统一且可复用的调用上限。", createButton)}
      ${renderTierOverview(totalTierCount, assignedUserCount, defaultTier)}
      ${tiers.length === 0 ? renderEmptyTiers(createButton) : `
        <div class="tier-catalog-heading">
          <div>
            <span class="tier-catalog-kicker">方案目录</span>
            <h2>当前配额策略</h2>
          </div>
          <p>方案按创建时间排列；调整限额后，会同步应用到已分配用户。</p>
        </div>
        <section class="tier-grid" aria-label="配额方案列表">
          ${tiers.map((tier) => renderTierCard(tier)).join("")}
        </section>
        ${renderCollectionPagination("tiers", state.pagination.tiers, tiers.length)}
      `}
    </div>
  `;
}

function renderTierOverview(tierCount, assignedUserCount, defaultTier) {
  return `
    <section class="tier-overview" aria-label="配额方案概览">
      <div class="tier-overview-copy">
        <span class="tier-overview-kicker">${renderIcon("spark")} Quota control</span>
        <h2>让每一档服务能力，都有明确边界。</h2>
        <p>集中维护请求速率与月度成功调用额度。方案变更会直接作用于正在使用它的用户，无需逐个调整。</p>
      </div>
      <div class="tier-overview-stats">
        <div class="tier-overview-stat">
          <span>方案总数</span>
          <strong>${escapeHTML(formatNumber(tierCount))}</strong>
          <small>套可分配策略</small>
        </div>
        <div class="tier-overview-stat">
          <span>覆盖用户</span>
          <strong>${escapeHTML(formatNumber(assignedUserCount))}</strong>
          <small>位用户已纳入管理</small>
        </div>
        <div class="tier-overview-stat">
          <span>新用户默认</span>
          <strong class="is-textual">${defaultTier ? escapeHTML(defaultTier.name) : "未设置"}</strong>
          <small>${defaultTier ? "注册与新建用户自动分配" : "请设置一个默认方案"}</small>
        </div>
      </div>
    </section>
  `;
}

function renderTierCard(tier) {
  const tierIsDefault = isDefaultTier(tier);
  const assignedUserCount = getNonNegativeNumber(tier.user_count);
  const tierHasAssignedUsers = assignedUserCount > 0;
  const tierIdentifier = escapeHTML(tier.id);

  return `
    <article class="tier-card ${tierIsDefault ? "is-default" : ""}">
      <header class="tier-card-header">
        <div class="tier-identity">
          <div class="tier-symbol">${renderIcon(tierIsDefault ? "spark" : "layers")}</div>
          <div class="tier-title-group">
            <div class="tier-card-meta">
              <span>配额方案</span>
              ${tierIsDefault ? '<span class="tier-default-note"><i></i> 默认方案</span>' : ""}
            </div>
            <h3>${escapeHTML(tier.name)}</h3>
          </div>
        </div>
        <button class="tier-edit-button" type="button" data-action="open-edit-tier" data-id="${tierIdentifier}">
          ${renderIcon("edit")}<span>编辑</span>
        </button>
      </header>

      <div class="tier-limits" aria-label="方案限额">
        ${renderTierLimit("activity", "每分钟请求", "Requests per minute", tier.rpm, "RPM")}
        ${renderTierLimit("shield", "月度成功调用", "Monthly successful calls", tier.success_limit, "次 / 月")}
      </div>

      <footer class="tier-card-footer">
        <div class="tier-assignment">
          <div class="tier-assignment-icon">${renderIcon("users")}</div>
          <div>
            <strong>${escapeHTML(formatNumber(assignedUserCount))} 位用户</strong>
            <span>${renderAssignmentDescription(tierIsDefault, tierHasAssignedUsers)}</span>
          </div>
        </div>
        <button class="tier-delete-button" type="button" data-action="confirm-delete-tier" data-id="${tierIdentifier}" ${renderDeleteButtonState(tierIsDefault, tierHasAssignedUsers)}>
          ${renderIcon("trash")}<span>删除</span>
        </button>
      </footer>
    </article>
  `;
}

function renderTierLimit(iconName, label, description, value, unit) {
  return `
    <div class="tier-limit">
      <div class="tier-limit-icon">${renderIcon(iconName)}</div>
      <div class="tier-limit-copy">
        <span>${escapeHTML(label)}</span>
        <small>${escapeHTML(description)}</small>
      </div>
      <div class="tier-limit-value">
        <strong>${escapeHTML(formatLimit(value))}</strong>
        <span>${escapeHTML(unit)}</span>
      </div>
    </div>
  `;
}

function renderEmptyTiers(createButton) {
  return `<div class="data-card tier-empty-card">${renderEmptyState("layers", "还没有配额方案", "先创建一套限额策略，再将它分配给需要统一管理的用户。", createButton)}</div>`;
}

function renderTiersLoading(createButton) {
  return `
    <div class="tiers-page">
      ${renderPageHeading("配额方案", "正在同步配额策略与用户分配情况。", createButton)}
      <div class="skeleton tier-overview-skeleton"></div>
      <div class="tier-catalog-heading">
        <div>
          <span class="tier-catalog-kicker">方案目录</span>
          <h2>当前配额策略</h2>
        </div>
      </div>
      <div class="tier-grid">
        ${Array.from({ length: 4 }, () => '<div class="skeleton tier-card-skeleton"></div>').join("")}
      </div>
    </div>
  `;
}

function isDefaultTier(tier) {
  return Boolean(tier?.is_default);
}

function renderDeleteButtonState(tierIsDefault, tierHasAssignedUsers) {
  if (tierIsDefault) {
    return 'disabled title="请先将其他方案设为默认方案"';
  }
  if (tierHasAssignedUsers) {
    return 'disabled title="已有用户使用此方案，无法删除"';
  }
  return 'title="删除配额方案"';
}

function renderAssignmentDescription(tierIsDefault, tierHasAssignedUsers) {
  if (tierHasAssignedUsers) {
    return "修改限额将同步影响这些用户";
  }
  if (tierIsDefault) {
    return "新用户会自动使用此方案";
  }
  return "尚未分配，可随时调整或删除";
}

function getNonNegativeNumber(value) {
  const numericValue = Number(value);
  return Number.isFinite(numericValue) && numericValue > 0 ? numericValue : 0;
}
