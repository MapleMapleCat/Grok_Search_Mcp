import { loadRouteData, render } from "../app.js";
import { renderAccount } from "./pages/account.js";
import { renderDashboard } from "./pages/dashboard.js";
import { renderKeys } from "./pages/keys.js";
import { renderTiers } from "./pages/tiers.js";
import { renderServerSettings } from "./pages/settings.js";
import { renderConfigurationTutorial } from "./pages/tutorial.js";
import { renderUsage } from "./pages/usage.js";
import { renderUsers } from "./pages/users.js";
import { isAdmin, state } from "./state.js";

export const routes = ["dashboard", "keys", "usage", "users", "tiers", "tutorial", "settings", "account"];

export const routeMeta = {
  dashboard: { label: "Dashboard", icon: "dashboard" },
  keys: { label: "Keys", icon: "vpn_key" },
  usage: { label: "Usage Stats", icon: "bar_chart" },
  users: { label: "User Management", icon: "group", admin: true },
  tiers: { label: "Tier Management", icon: "workspace_premium", admin: true },
  tutorial: { label: "Configuration Tutorial", icon: "menu_book" },
  settings: { label: "Server Settings", icon: "settings_applications", bottom: true, admin: true },
  account: { label: "Account Settings", icon: "settings", bottom: true }
};

export function renderRoute() {
  if (state.route === "dashboard") return renderDashboard();
  if (state.route === "keys") return renderKeys();
  if (state.route === "usage") return renderUsage();
  if (state.route === "users") return renderUsers();
  if (state.route === "tiers") return renderTiers();
  if (state.route === "tutorial") return renderConfigurationTutorial();
  if (state.route === "settings") return renderServerSettings();
  if (state.route === "account") return renderAccount();
  return renderDashboard();
}

export function readRoute() {
  const raw = window.location.hash.replace(/^#\/?/, "");
  return routes.includes(raw) ? raw : "dashboard";
}

export function navigate(route) {
  const next = routes.includes(route) ? route : "dashboard";
  if (routeMeta[next].admin && !isAdmin()) {
    state.route = next;
    window.location.hash = `#/${next}`;
    render();
    return;
  }
  if (window.location.hash !== `#/${next}`) {
    window.location.hash = `#/${next}`;
  } else {
    state.route = next;
    loadRouteData().then(render);
  }
}
