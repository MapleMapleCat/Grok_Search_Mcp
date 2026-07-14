import {
  APIError,
  fetchAdminUsers,
  fetchCurrentUser,
  fetchInviteCodes,
  fetchKeys,
  fetchRegistrationSettings,
  fetchSettings,
  fetchTiers,
  fetchUsage,
  panelAPI
} from "./js/api.js";
import { renderAuthView } from "./js/components/forms.js";
import { renderModal } from "./js/components/modal.js";
import { configureToastRegion, showToast } from "./js/components/toast.js";
import { createApplicationEvents } from "./js/events.js";
import { adminPages, availablePages, pageMetadata, renderShell } from "./js/router.js";
import {
  clearAuthenticatedState,
  commitPageData,
  normalizeUsage,
  pageHasExistingData,
  state
} from "./js/state.js";
import { getUsagePeriodSince } from "./js/utils.js";

const applicationElement = document.querySelector("#app");
const modalRegionElement = document.querySelector("#modal-region");
const toastRegionElement = document.querySelector("#toast-region");

let activePageRequestIdentifier = 0;
let activePageRequestController = null;

function abortCurrentPageLoad() {
  activePageRequestController?.abort();
  activePageRequestController = null;
  activePageRequestIdentifier += 1;
}

function renderApplication() {
  applicationElement.innerHTML = state.authenticated ? renderShell(state) : renderAuthView(state);
  renderModalRegion();
  document.title = state.authenticated
    ? `${pageMetadata[state.currentPage]?.title || "控制台"} · Grok MCP`
    : "登录 · Grok MCP Control";
}

function renderModalRegion() {
  modalRegionElement.innerHTML = renderModal(state);
}

async function initializeApplication() {
  configureToastRegion(toastRegionElement);
  createApplicationEvents({
    applicationElement,
    modalRegionElement,
    renderApplication,
    renderModalRegion,
    loadCurrentPage,
    abortCurrentPageLoad,
    normalizeCurrentPageForRole,
    handleSessionError
  }).register();

  await loadRegistrationMode();
  if (!panelAPI.hasSession()) {
    renderApplication();
    return;
  }

  try {
    state.user = await fetchCurrentUser();
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

async function loadRegistrationMode() {
  try {
    const registrationSettings = await fetchRegistrationSettings();
    state.registrationMode = registrationSettings?.registration_mode || "free";
    if (state.registrationMode === "disabled") {
      state.authMode = "login";
    }
  } catch (error) {
    state.registrationMode = "free";
    state.authError = getErrorMessage(error);
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
}

async function loadCurrentPage(options = {}) {
  abortCurrentPageLoad();

  const page = state.currentPage;
  const requestIdentifier = activePageRequestIdentifier;
  const requestController = new AbortController();
  activePageRequestController = requestController;
  state.pageLoading = !pageHasExistingData(page);
  state.refreshing = Boolean(options.refreshing);
  renderApplication();

  try {
    const pageResult = await loadPageData(page, requestController.signal);
    if (requestIdentifier !== activePageRequestIdentifier) {
      return;
    }
    commitPageData(page, pageResult);
  } catch (error) {
    if (requestIdentifier !== activePageRequestIdentifier) {
      return;
    }
    if (error?.name === "AbortError") {
      return;
    }
    if (handleSessionError(error)) {
      return;
    }
    showToast("加载失败", getErrorMessage(error), "error");
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

async function loadPageData(page, signal) {
  switch (page) {
    case "overview": {
      const [user, keyResponse, usage] = await Promise.all([
        fetchCurrentUser({ signal }),
        fetchKeys({ signal }),
        fetchUsage(getUsagePeriodSince("24h"), { signal })
      ]);
      return {
        user,
        keys: keyResponse?.keys || [],
        overviewUsage: normalizeUsage(usage)
      };
    }
    case "keys": {
      const keyResponse = await fetchKeys({ signal });
      return { keys: keyResponse?.keys || [] };
    }
    case "usage": {
      const usage = await fetchUsage(getUsagePeriodSince(state.filters.usagePeriod), { signal });
      return { usage: normalizeUsage(usage) };
    }
    case "users": {
      const [userResponse, tierResponse] = await Promise.all([
        fetchAdminUsers({ signal }),
        fetchTiers({ signal })
      ]);
      return {
        users: userResponse?.users || [],
        tiers: tierResponse?.tiers || []
      };
    }
    case "tiers": {
      const tierResponse = await fetchTiers({ signal });
      return { tiers: tierResponse?.tiers || [] };
    }
    case "invites": {
      const inviteResponse = await fetchInviteCodes({ signal });
      return { invites: inviteResponse?.invite_codes || [] };
    }
    case "settings":
      return { settings: await fetchSettings({ signal }) };
    case "account":
      return { user: await fetchCurrentUser({ signal }) };
    default:
      return {};
  }
}

function handleSessionError(error) {
  if (!(error instanceof APIError) || error.status !== 401) {
    return false;
  }

  abortCurrentPageLoad();
  panelAPI.clearSession();
  clearAuthenticatedState();
  state.authError = "会话已失效，请重新登录。";
  renderApplication();
  return true;
}

function getErrorMessage(error) {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return "发生未知错误，请稍后重试。";
}

initializeApplication();
