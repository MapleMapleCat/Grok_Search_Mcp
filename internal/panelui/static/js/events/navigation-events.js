import { showToast } from "../components/toast.js";
import { adminPages, availablePages, readPageFromLocation } from "../router.js";
import {
  movePaginationCursor,
  resetPagination,
  restorePaginationCursor,
  setPaginationPageSize
} from "../state.js";

const pageByCollection = {
  keys: "keys",
  users: "users",
  tiers: "tiers",
  invites: "invites",
  usageRecords: "usage"
};
const collectionByPage = Object.fromEntries(
  Object.entries(pageByCollection).map(([collectionName, page]) => [page, collectionName])
);

export function createNavigationEvents({
  state,
  modalController,
  renderApplication,
  loadCurrentPage,
  normalizeCurrentPageForRole
}) {
  function navigateToPage(page) {
    if (!availablePages.has(page)) {
      return;
    }
    if (adminPages.has(page) && state.user?.role !== "admin") {
      showToast("权限不足", "当前账户无法访问系统管理页面。", "error");
      return;
    }
    if (page === "operationsMetrics" && !state.data.settings?.operations_metrics_enabled) {
      showToast("运行指标未启用", "请先在服务设置中启用数据库运行指标。", "error");
      return;
    }

    state.sidebarOpen = false;
    modalController.abortCurrentModalRequest();
    state.modal = null;
    if (state.currentPage === page) {
      renderApplication();
      return;
    }
    window.location.hash = page;
  }

  function handleLocationChange() {
    if (!state.authenticated) {
      return;
    }
    const requestedPage = readPageFromLocation();
    if (requestedPage === state.currentPage) {
      return;
    }
    state.currentPage = requestedPage;
    normalizeCurrentPageForRole();
    state.sidebarOpen = false;
    modalController.abortCurrentModalRequest();
    state.modal = null;
    loadCurrentPage();
  }

  async function refreshCurrentPage() {
    resetCurrentPagePagination();
    await loadCurrentPage({ refreshing: true });
  }

  async function changeListPage(collectionName, direction) {
    if (pageByCollection[collectionName] !== state.currentPage) {
      return;
    }

    const cursorSnapshot = movePaginationCursor(collectionName, direction);
    if (!cursorSnapshot) {
      return;
    }
    const loaded = await loadCurrentPage({ refreshing: true });
    if (!loaded && state.authenticated) {
      restorePaginationCursor(collectionName, cursorSnapshot);
      renderApplication();
    }
  }

  async function changeUsagePageSize(requestedPageSize) {
    if (state.currentPage !== "usage") {
      return;
    }

    const collectionName = "usageRecords";
    const previousPageSize = state.pagination.usageRecords.pageSize;
    if (!setPaginationPageSize(collectionName, requestedPageSize)) {
      return;
    }

    const loaded = await loadCurrentPage({ refreshing: true });
    if (!loaded && state.authenticated) {
      setPaginationPageSize(collectionName, previousPageSize);
      renderApplication();
    }
  }

  async function setUsagePeriod(period) {
    state.filters.usagePeriod = period || "24h";
    state.data.usage = null;
    resetPagination("usageRecords");
    await loadCurrentPage();
  }

  function resetCurrentPagePagination() {
    const collectionName = collectionByPage[state.currentPage];
    if (collectionName) {
      resetPagination(collectionName);
    }
  }

  return {
    navigateToPage,
    handleLocationChange,
    refreshCurrentPage,
    changeListPage,
    changeUsagePageSize,
    setUsagePeriod
  };
}
