import { fetchAdminUserUsage } from "../api.js";
import { showToast } from "../components/toast.js";
import {
  commitCursorPagination,
  createCursorPaginationState,
  moveCursorPagination,
  resetCursorPagination,
  restoreCursorPagination
} from "../cursor-pagination.js";
import { COLLECTION_PAGE_SIZE_OPTIONS, findItemByIdentifier, normalizeUsage } from "../state.js";
import { getErrorMessage } from "./event-helpers.js";

export function createUserUsageModalEvents({
  state,
  modalController,
  renderModalRegion,
  handleSessionError
}) {
  async function openUserUsageModal(userIdentifier) {
    const user = findItemByIdentifier(state.data.users, userIdentifier);
    modalController.openModal({
      type: "userUsage",
      userIdentifier,
      username: user?.username || "用户",
      loading: true,
      usage: null,
      recentRecords: null,
      ...createCursorPaginationState(20, { loadingRecords: false })
    });
    await loadAdminUserUsagePage({ closeModalOnError: true });
  }

  async function loadAdminUserUsagePage(options = {}) {
    const modal = state.modal;
    if (!isUserUsageModal(modal)) {
      return false;
    }

    const userIdentifier = modal.userIdentifier;
    const requestedCursor = modal.cursor;
    const requestedPageSize = modal.pageSize;
    const requestContext = modalController.startModalRequest();

    try {
      const usageResponse = await fetchAdminUserUsage(userIdentifier, {
        signal: requestContext.requestController.signal,
        cursor: requestedCursor,
        limit: requestedPageSize
      });
      if (isMatchingUsageRequest(
        requestContext,
        userIdentifier,
        requestedCursor,
        requestedPageSize
      )) {
        const usage = normalizeUsage(usageResponse);
        state.modal.loading = false;
        state.modal.usage = usage;
        commitCursorPagination(state.modal, usage);
        if (!Array.isArray(state.modal.recentRecords)) {
          state.modal.recentRecords = usage.records.slice(0, 8);
        }
        renderModalRegion();
        return true;
      }
    } catch (error) {
      if (error?.name !== "AbortError"
        && modalController.isCurrentModalRequest(requestContext)
        && !handleSessionError(error)) {
        if (options.closeModalOnError) {
          modalController.closeModal();
        } else if (isUserUsageModal(state.modal)) {
          state.modal.loading = false;
          state.modal.loadingRecords = false;
          renderModalRegion();
        }
        showToast("无法加载用户用量", getErrorMessage(error), "error");
      }
    } finally {
      modalController.finishModalRequest(requestContext);
    }
    return false;
  }

  function openUserUsageLogsModal() {
    if (state.modal?.type !== "userUsage" || state.modal.loading || !state.modal.usage) {
      return;
    }
    state.modal.type = "userUsageLogs";
    renderModalRegion();
  }

  function openUserUsageSummaryModal() {
    if (state.modal?.type !== "userUsageLogs") {
      return;
    }
    state.modal.type = "userUsage";
    renderModalRegion();
  }

  async function changeUserUsagePage(direction) {
    if (state.modal?.type !== "userUsageLogs" || state.modal.loadingRecords) {
      return;
    }

    const paginationSnapshot = moveCursorPagination(state.modal, direction, true);
    if (!paginationSnapshot) {
      return;
    }

    renderModalRegion();
    const loaded = await loadAdminUserUsagePage();
    if (!loaded && state.modal?.type === "userUsageLogs") {
      restoreCursorPagination(state.modal, paginationSnapshot);
      renderModalRegion();
    }
  }

  async function changeUserUsagePageSize(requestedPageSize) {
    if (state.modal?.type !== "userUsageLogs" || state.modal.loadingRecords) {
      return;
    }

    const pageSize = Number(requestedPageSize);
    if (!COLLECTION_PAGE_SIZE_OPTIONS.includes(pageSize) || pageSize === state.modal.pageSize) {
      return;
    }

    const paginationSnapshot = resetCursorPagination(state.modal, pageSize, true);
    renderModalRegion();
    const loaded = await loadAdminUserUsagePage();
    if (!loaded && state.modal?.type === "userUsageLogs") {
      restoreCursorPagination(state.modal, paginationSnapshot);
      renderModalRegion();
    }
  }

  function isMatchingUsageRequest(
    requestContext,
    userIdentifier,
    requestedCursor,
    requestedPageSize
  ) {
    return modalController.isCurrentModalRequest(requestContext)
      && isUserUsageModal(state.modal)
      && state.modal.userIdentifier === userIdentifier
      && state.modal.cursor === requestedCursor
      && state.modal.pageSize === requestedPageSize;
  }

  return {
    openUserUsageModal,
    openUserUsageLogsModal,
    openUserUsageSummaryModal,
    changeUserUsagePage,
    changeUserUsagePageSize
  };
}

function isUserUsageModal(modal) {
  return Boolean(modal && ["userUsage", "userUsageLogs"].includes(modal.type));
}
