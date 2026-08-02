import {
  APIError,
  fetchAllTiers,
  fetchAdminUsers,
  fetchCurrentUser,
  fetchInviteCodes,
  fetchKeys,
  fetchOperationalMetrics,
  fetchOverviewHealth,
  fetchRegistrationSettings,
  fetchSettings,
  fetchTiers,
  fetchUsage,
  fetchUsageRecords,
  panelAPI
} from "./js/api.js";
import { renderAuthView } from "./js/components/forms.js";
import { configureCustomSelects } from "./js/components/custom-select.js";
import { renderModal } from "./js/components/modal.js";
import { configureToastRegion, showToast } from "./js/components/toast.js";
import { createApplicationEvents } from "./js/events.js";
import { getErrorMessage } from "./js/events/event-helpers.js";
import { adminPages, availablePages, isStaticPage, pageMetadata, renderShell } from "./js/router.js";
import { renderSafeHTML } from "./js/safe-html.js";
import {
  applyAuthenticationSettings,
  clearAuthenticatedState,
  COLLECTION_PAGE_SIZE,
  commitPageData,
  normalizeUsage,
  pageHasExistingData,
  state
} from "./js/state.js";
import { getUsagePeriodSince } from "./js/utils.js";
import { synchronizeLoginTurnstile } from "./js/turnstile.js";

const applicationElement = document.querySelector("#app");
const modalRegionElement = document.querySelector("#modal-region");
const toastRegionElement = document.querySelector("#toast-region");

let activePageRequestIdentifier = 0;
let activePageRequestController = null;
let activeOverviewHealthRequestController = null;
let authenticationSettingsRequestIdentifier = 0;

function abortCurrentPageLoad() {
  activePageRequestController?.abort();
  activePageRequestController = null;
  activeOverviewHealthRequestController?.abort();
  activeOverviewHealthRequestController = null;
  activePageRequestIdentifier += 1;
}

function renderApplication() {
  renderSafeHTML(applicationElement, state.authenticated ? renderShell(state) : renderAuthView(state));
  synchronizeLoginTurnstile({
    enabled: !state.authenticated && !state.authBusy && state.authMode === "login" && state.turnstileEnabled,
    siteKey: state.turnstileSiteKey
  });
  renderModalRegion();
  document.title = state.authenticated
    ? `${pageMetadata[state.currentPage]?.title || "控制台"} · Grok Search MCP`
    : "登录 · Grok Search MCP Control";
}

function renderModalRegion() {
  renderSafeHTML(modalRegionElement, renderModal(state));
}

async function initializeApplication() {
  configureToastRegion(toastRegionElement);
  configureCustomSelects();
  createApplicationEvents({
    applicationElement,
    modalRegionElement,
    renderApplication,
    renderModalRegion,
    loadCurrentPage,
    abortCurrentPageLoad,
    normalizeCurrentPageForRole,
    handleSessionError,
    reloadAuthenticationSettings: loadAuthenticationSettings
  }).register();

  await loadAuthenticationSettings();
  if (!panelAPI.hasSession()) {
    renderApplication();
    return;
  }

  try {
    state.user = await fetchCurrentUser();
    if (state.user?.role === "admin") {
      try {
        state.data.settings = await fetchSettings();
        state.settingsApplyWarning = null;
      } catch (error) {
        if (error instanceof APIError && error.status === 401) {
          throw error;
        }
        state.data.settings = null;
      }
    }
    state.authenticated = true;
    normalizeCurrentPageForRole();
    renderApplication();
    await loadCurrentPage();
  } catch (error) {
    panelAPI.clearSession();
    clearAuthenticatedState();
    if (!(error instanceof APIError && error.status === 401)) {
      state.authError = getErrorMessage(error);
    }
    renderApplication();
  }
}

async function loadAuthenticationSettings() {
  authenticationSettingsRequestIdentifier += 1;
  const requestIdentifier = authenticationSettingsRequestIdentifier;
  state.authenticationSettingsStatus = "loading";
  state.authenticationSettingsError = "";
  try {
    const registrationSettings = await fetchRegistrationSettings();
    if (requestIdentifier !== authenticationSettingsRequestIdentifier) {
      return false;
    }
    return applyAuthenticationSettings(state, registrationSettings);
  } catch (error) {
    if (requestIdentifier !== authenticationSettingsRequestIdentifier) {
      return false;
    }
    state.authenticationSettingsStatus = "error";
    state.authenticationSettingsError = getErrorMessage(error);
    return false;
  }
}

function normalizeCurrentPageForRole() {
  if (!availablePages.has(state.currentPage)) {
    state.currentPage = "overview";
  }
  if (adminPages.has(state.currentPage) && state.user?.role !== "admin") {
    state.currentPage = "overview";
    window.history.replaceState(null, "", "#overview");
  }
  if (state.currentPage === "operationsMetrics" && !state.data.settings?.operations_metrics_enabled) {
    state.currentPage = "overview";
    window.history.replaceState(null, "", "#overview");
  }
}

async function loadCurrentPage(options = {}) {
  abortCurrentPageLoad();

  const page = state.currentPage;
  if (isStaticPage(page)) {
    state.pageLoading = false;
    state.refreshing = false;
    renderApplication();
    return true;
  }

  const requestIdentifier = activePageRequestIdentifier;
  const requestController = new AbortController();
  activePageRequestController = requestController;
  state.pageLoading = !pageHasExistingData(page);
  state.refreshing = Boolean(options.refreshing);
  renderApplication();

  try {
    const pageResult = await loadPageData(page, requestController.signal);
    if (requestIdentifier !== activePageRequestIdentifier) {
      return false;
    }
    commitPageData(page, pageResult);
    if (page === "overview") {
      void loadOverviewHealthIndependently(requestIdentifier);
    }
    return true;
  } catch (error) {
    if (requestIdentifier !== activePageRequestIdentifier) {
      return false;
    }
    if (error?.name === "AbortError") {
      return false;
    }
    if (handleSessionError(error)) {
      return false;
    }
    showToast("加载失败", getErrorMessage(error), "error");
    return false;
  } finally {
    if (activePageRequestController === requestController) {
      activePageRequestController = null;
    }
    if (requestIdentifier === activePageRequestIdentifier && state.authenticated) {
      state.pageLoading = false;
      state.refreshing = false;
      renderApplication();
    }
  }
}

async function loadOverviewHealthIndependently(requestIdentifier) {
  const requestController = new AbortController();
  activeOverviewHealthRequestController = requestController;

  try {
    const overviewHealth = await fetchOverviewHealth({ signal: requestController.signal });
    if (requestIdentifier !== activePageRequestIdentifier || state.currentPage !== "overview") {
      return;
    }
    state.data.overviewHealth = overviewHealth;
  } catch (error) {
    if (requestIdentifier !== activePageRequestIdentifier || error?.name === "AbortError") {
      return;
    }
    if (handleSessionError(error)) {
      return;
    }
    state.data.overviewHealth = { status: "unknown", checked_at: "" };
  } finally {
    if (activeOverviewHealthRequestController === requestController) {
      activeOverviewHealthRequestController = null;
    }
    if (
      requestIdentifier === activePageRequestIdentifier
      && state.authenticated
      && state.currentPage === "overview"
    ) {
      renderApplication();
    }
  }
}

async function loadPageData(page, signal) {
  switch (page) {
    case "overview": {
      const settingsRequest = state.user?.role === "admin"
        ? fetchSettings({ signal })
        : Promise.resolve(null);
      const [user, keyResponse, usage, settings] = await Promise.all([
        fetchCurrentUser({ signal }),
        fetchKeys({ signal, limit: COLLECTION_PAGE_SIZE }),
        fetchUsage(getUsagePeriodSince("24h"), { signal }),
        settingsRequest
      ]);
      return {
        user,
        keyResponse,
        overviewUsage: normalizeUsage(usage),
        settings
      };
    }
    case "keys": {
      return { keyResponse: await fetchCollectionPage(fetchKeys, state.pagination.keys, signal) };
    }
    case "usage": {
      const since = getUsagePeriodSince(state.filters.usagePeriod);
      const cursor = state.pagination.usageRecords.cursor;
      const pageSize = state.pagination.usageRecords.pageSize;
      if (cursor) {
        const recordPage = await fetchUsageRecords(since, { signal, cursor, limit: pageSize });
        return {
          usage: normalizeUsage({
            ...(state.data.usage || {}),
            records: recordPage?.records || [],
            next_cursor: recordPage?.next_cursor || "",
            has_more: Boolean(recordPage?.has_more)
          })
        };
      }
      const usage = await fetchUsage(since, { signal, limit: pageSize });
      return { usage: normalizeUsage(usage) };
    }
    case "users": {
      const [userResponse, tierResponse] = await Promise.all([
        fetchCollectionPage(fetchAdminUsers, state.pagination.users, signal),
        fetchAllTiers({ signal, limit: 100 })
      ]);
      return { userResponse, tierResponse };
    }
    case "tiers": {
      return { tierResponse: await fetchCollectionPage(fetchTiers, state.pagination.tiers, signal) };
    }
    case "invites": {
      return { inviteResponse: await fetchCollectionPage(fetchInviteCodes, state.pagination.invites, signal) };
    }
    case "settings":
      return { settings: await fetchSettings({ signal }) };
    case "operationsMetrics":
      return { operationsMetrics: await fetchOperationalMetrics({ signal }) };
    case "account":
      return { user: await fetchCurrentUser({ signal }) };
    default:
      return {};
  }
}

function fetchCollectionPage(fetchCollection, pagination, signal) {
  return fetchCollection({
    signal,
    cursor: pagination.cursor,
    limit: COLLECTION_PAGE_SIZE
  });
}

function handleSessionError(error) {
  if (!(error instanceof APIError) || error.status !== 401) {
    return false;
  }

  abortCurrentPageLoad();
  panelAPI.clearSession();
  clearAuthenticatedState();
  state.authError = "会话已失效，请重新登录。";
  state.authenticationSettingsStatus = "loading";
  state.authenticationSettingsError = "";
  renderApplication();
  void loadAuthenticationSettings().then(() => {
    if (!state.authenticated) {
      renderApplication();
    }
  });
  return true;
}

initializeApplication();
