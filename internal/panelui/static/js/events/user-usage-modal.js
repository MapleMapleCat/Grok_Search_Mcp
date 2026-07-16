import { fetchAdminUserUsage } from "../api.js";
import { showToast } from "../components/toast.js";
import { COLLECTION_PAGE_SIZE_OPTIONS, normalizeUsage } from "../state.js";
import { getErrorMessage } from "./event-helpers.js";

export function createUserUsageModalEvents({
  state,
  modalController,
  renderModalRegion,
  handleSessionError
}) {
  async function openUserUsageModal(userIdentifier) {
    const user = (state.data.users || []).find(
      (candidateUser) => candidateUser.id === userIdentifier
    );
    modalController.openModal({
      type: "userUsage",
      userIdentifier,
      username: user?.username || "用户",
      loading: true,
      loadingRecords: false,
      usage: null,
      recentRecords: null,
      pageSize: 20,
      cursor: "",
      nextCursor: "",
      previousCursors: [],
      hasMore: false
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
        state.modal.loadingRecords = false;
        state.modal.usage = usage;
        state.modal.nextCursor = usage.next_cursor;
        state.modal.hasMore = Boolean(usage.has_more && usage.next_cursor);
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

    const paginationSnapshot = createPaginationSnapshot(state.modal);
    if (direction === "next") {
      if (!state.modal.hasMore || !state.modal.nextCursor) {
        return;
      }
      state.modal.previousCursors.push(state.modal.cursor);
      state.modal.cursor = state.modal.nextCursor;
    } else if (direction === "previous" && state.modal.previousCursors.length > 0) {
      state.modal.cursor = state.modal.previousCursors.pop() || "";
    } else {
      return;
    }

    state.modal.loadingRecords = true;
    renderModalRegion();
    const loaded = await loadAdminUserUsagePage();
    if (!loaded && state.modal?.type === "userUsageLogs") {
      restorePaginationSnapshot(state.modal, paginationSnapshot);
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

    const paginationSnapshot = createPaginationSnapshot(state.modal);
    state.modal.pageSize = pageSize;
    state.modal.cursor = "";
    state.modal.nextCursor = "";
    state.modal.previousCursors = [];
    state.modal.hasMore = false;
    state.modal.loadingRecords = true;
    renderModalRegion();
    const loaded = await loadAdminUserUsagePage();
    if (!loaded && state.modal?.type === "userUsageLogs") {
      restorePaginationSnapshot(state.modal, paginationSnapshot);
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

function createPaginationSnapshot(modal) {
  return {
    cursor: modal.cursor,
    nextCursor: modal.nextCursor,
    previousCursors: [...modal.previousCursors],
    hasMore: modal.hasMore,
    pageSize: modal.pageSize,
    usage: modal.usage
  };
}

function restorePaginationSnapshot(modal, snapshot) {
  modal.cursor = snapshot.cursor;
  modal.nextCursor = snapshot.nextCursor;
  modal.previousCursors = [...snapshot.previousCursors];
  modal.hasMore = snapshot.hasMore;
  modal.pageSize = snapshot.pageSize;
  modal.usage = snapshot.usage;
  modal.loadingRecords = false;
}
