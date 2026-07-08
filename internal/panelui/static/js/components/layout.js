import { renderInlineLoading } from "./loading.js";
import { isRouteVisibleInNavigation, renderRoute, routeMeta, routes } from "../router.js";
import { isAdmin, state } from "../state.js";
import { escapeHTML, initials } from "../utils.js";

export function renderShell() {
  return `
    <div class="app-shell">
      ${renderSidebar()}
      <header class="topbar">
        <button class="icon-button mobile-menu" data-action="go" data-route="dashboard" title="Dashboard" type="button">
          <span class="material-symbols-outlined">developer_board</span>
        </button>
        <label class="search-box">
          <span class="material-symbols-outlined">search</span>
          <input id="global-search" value="${escapeHTML(state.search)}" placeholder="Search resources, logs..." autocomplete="off">
        </label>
        <div class="top-actions">
          <button class="icon-button" data-action="refresh" title="刷新" type="button"><span class="material-symbols-outlined">notifications</span></button>
          <button class="avatar" data-action="go" data-route="account" title="${escapeHTML(state.user.username)}" type="button">${escapeHTML(initials(state.user.username))}</button>
        </div>
      </header>
      <main class="main">
        <div class="content">
          ${state.loading ? renderInlineLoading() : renderRoute()}
        </div>
      </main>
    </div>`;
}

export function renderSidebar() {
  const visibleRoutes = routes.filter(isRouteVisibleInNavigation);
  const top = visibleRoutes.filter((route) => !routeMeta[route].bottom).map(renderNavLink).join("");
  const bottom = visibleRoutes.filter((route) => routeMeta[route].bottom).map(renderNavLink).join("");
  return `
    <aside class="sidebar">
      <div class="brand">
        <div class="brand-mark">
          <span class="material-symbols-outlined">developer_board</span>
        </div>
        <div class="brand-copy">
          <h1>MCP Central</h1>
          <p>Protocol Management</p>
        </div>
      </div>
      <nav class="nav-list" aria-label="主导航">
        ${top}
        <div class="nav-bottom">${bottom}</div>
      </nav>
    </aside>`;
}

export function renderNavLink(route) {
  const meta = routeMeta[route];
  const locked = meta.admin && !isAdmin();
  return `
    <a class="nav-link ${state.route === route ? "active" : ""} ${locked ? "locked" : ""}" href="#/${route}" title="${escapeHTML(meta.label)}">
      <span class="material-symbols-outlined">${meta.icon}</span>
      <span>${escapeHTML(meta.label)}</span>
    </a>`;
}
