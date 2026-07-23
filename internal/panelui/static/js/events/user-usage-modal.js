import { fetchAdminUserUsage } from "../api.js";
import { showToast } from "../components/toast.js";
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
      loadingRecords: false,
      usage: null,
      recentRecords: null,
      cursor: "",
      nextCursor: "",
      previousCursors: [],
      hasMore: false,
      pageSize: 20
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

    const paginationSnapshot = {
      cursor: state.modal.cursor,
      nextCursor: state.modal.nextCursor,
      previousCursors: [...state.modal.previousCursors],
      hasMore: state.modal.hasMore,
      pageSize: state.modal.pageSize
    };
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
      state.modal.cursor = paginationSnapshot.cursor;
      state.modal.nextCursor = paginationSnapshot.nextCursor;
      state.modal.previousCursors = [...paginationSnapshot.previousCursors];
      state.modal.hasMore = paginationSnapshot.hasMore;
      state.modal.pageSize = paginationSnapshot.pageSize;
      state.modal.loadingRecords = false;
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

    const paginationSnapshot = {
      cursor: state.modal.cursor,
      nextCursor: state.modal.nextCursor,
      previousCursors: [...state.modal.previousCursors],
      hasMore: state.modal.hasMore,
      pageSize: state.modal.pageSize
    };
    state.modal.cursor = "";
    state.modal.nextCursor = "";
    state.modal.previousCursors = [];
    state.modal.hasMore = false;
    state.modal.pageSize = pageSize;
    state.modal.loadingRecords = true;
    renderModalRegion();
    const loaded = await loadAdminUserUsagePage();
    if (!loaded && state.modal?.type === "userUsageLogs") {
      state.modal.cursor = paginationSnapshot.cursor;
      state.modal.nextCursor = paginationSnapshot.nextCursor;
      state.modal.previousCursors = [...paginationSnapshot.previousCursors];
      state.modal.hasMore = paginationSnapshot.hasMore;
      state.modal.pageSize = paginationSnapshot.pageSize;
      state.modal.loadingRecords = false;
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
