import { readPageFromLocation } from "./router.js";

export const state = {
  authenticated: false,
  user: null,
  registrationMode: "free",
  authMode: "login",
  authBusy: false,
  authError: "",
  currentPage: readPageFromLocation(),
  pageLoading: false,
  refreshing: false,
  formBusy: false,
  sidebarOpen: false,
  modal: null,
  filters: {
    usagePeriod: "24h",
    userSearch: ""
  },
  data: {
    keys: null,
    overviewUsage: null,
    usage: null,
    users: null,
    tiers: null,
    invites: null,
    settings: null,
    models: null
  }
};

export function clearCachedData() {
  for (const dataKey of Object.keys(state.data)) {
    state.data[dataKey] = null;
  }
}

export function clearAuthenticatedState() {
  state.authenticated = false;
  state.user = null;
  state.authBusy = false;
  state.formBusy = false;
  state.pageLoading = false;
  state.refreshing = false;
  state.sidebarOpen = false;
  state.modal = null;
  state.authMode = "login";
  clearCachedData();
}

export function pageHasExistingData(page) {
  switch (page) {
    case "overview":
      return Boolean(state.data.overviewUsage && state.data.keys);
    case "usage":
      return Boolean(state.data.usage);
    case "keys":
      return Boolean(state.data.keys);
    case "users":
      return Boolean(state.data.users && state.data.tiers);
    case "tiers":
      return Boolean(state.data.tiers);
    case "invites":
      return Boolean(state.data.invites);
    case "settings":
      return Boolean(state.data.settings);
    default:
      return false;
  }
}

export function commitPageData(page, pageResult) {
  switch (page) {
    case "overview":
      state.user = pageResult.user;
      state.data.keys = pageResult.keys;
      state.data.overviewUsage = pageResult.overviewUsage;
      break;
    case "keys":
      state.data.keys = pageResult.keys;
      break;
    case "usage":
      state.data.usage = pageResult.usage;
      break;
    case "users":
      state.data.users = pageResult.users;
      state.data.tiers = pageResult.tiers;
      break;
    case "tiers":
      state.data.tiers = pageResult.tiers;
      break;
    case "invites":
      state.data.invites = pageResult.invites;
      break;
    case "settings":
      state.data.settings = pageResult.settings;
      break;
    default:
      break;
  }
}

export function normalizeUsage(usage) {
  return {
    total_calls: Number(usage?.total_calls || 0),
    success_calls: Number(usage?.success_calls || 0),
    current_rpm: Number(usage?.current_rpm || 0),
    by_tool: usage?.by_tool || {},
    traffic_buckets: usage?.traffic_buckets || [],
    records: usage?.records || []
  };
}

export function replaceItemByIdentifier(items, updatedItem) {
  if (!Array.isArray(items) || !updatedItem?.id) {
    return Array.isArray(items) ? [...items] : [];
  }

  return items.map((item) => item.id === updatedItem.id ? updatedItem : item);
}

export function removeItemByIdentifier(items, identifier) {
  return Array.isArray(items) ? items.filter((item) => item.id !== identifier) : [];
}

export function compareTiers(firstTier, secondTier) {
  return Number(firstTier.level || 0) - Number(secondTier.level || 0)
    || String(firstTier.name || "").localeCompare(String(secondTier.name || ""), "zh-CN");
}
