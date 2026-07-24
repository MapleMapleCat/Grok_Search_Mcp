import { renderShell as renderLayoutShell } from "./components/layout.js";
import { renderAccountPage } from "./pages/account.js";
import { renderConfigurationGuidePage } from "./pages/configuration-guide.js";
import { renderInvitesPage } from "./pages/invite-codes.js";
import { renderKeysPage } from "./pages/keys.js";
import { renderOverviewPage } from "./pages/overview.js";
import { renderOperationsMetricsPage } from "./pages/operations-metrics.js";
import { renderSettingsPage } from "./pages/settings.js";
import { renderTiersPage } from "./pages/tiers.js";
import { renderUsagePage } from "./pages/usage.js";
import { renderUsersPage } from "./pages/users.js";

export const pageMetadata = {
  overview: { title: "总览", section: "工作台", render: renderOverviewPage },
  keys: { title: "API 密钥", section: "访问控制", render: renderKeysPage },
  tutorial: { title: "配置教程", section: "访问控制", dataMode: "static", render: renderConfigurationGuidePage },
  usage: { title: "调用分析", section: "可观测性", render: renderUsagePage },
  users: { title: "用户管理", section: "系统管理", render: renderUsersPage },
  tiers: { title: "配额方案", section: "系统管理", render: renderTiersPage },
  invites: { title: "邀请码", section: "系统管理", render: renderInvitesPage },
  operationsMetrics: { title: "运行指标", section: "系统管理", render: renderOperationsMetricsPage },
  settings: { title: "服务设置", section: "系统管理", render: renderSettingsPage },
  account: { title: "账户信息", section: "账户", render: renderAccountPage }
};

export const availablePages = new Set(Object.keys(pageMetadata));
export const adminPages = new Set(["users", "tiers", "invites", "operationsMetrics", "settings"]);

export function isStaticPage(page) {
  return pageMetadata[page]?.dataMode === "static";
}

export function readPageFromLocation(locationHash = window.location.hash) {
  const locationPage = locationHash.replace(/^#\/?/, "").trim();
  if (locationPage === "guide") {
    return "tutorial";
  }
  return availablePages.has(locationPage) ? locationPage : "overview";
}

export function renderCurrentPage(state) {
  return (pageMetadata[state.currentPage]?.render || pageMetadata.overview.render)(state);
}

export function renderShell(state) {
  const currentMetadata = pageMetadata[state.currentPage] || pageMetadata.overview;
  return renderLayoutShell(state, currentMetadata, renderCurrentPage(state));
}
