import {
  createInviteCode,
  deleteInviteCode,
  fetchInviteCodeRedemptions,
  updateInviteCode
} from "../api.js";
import { showToast } from "../components/toast.js";
import {
  COLLECTION_PAGE_SIZE_OPTIONS,
  findItemByIdentifier,
  replaceItemByIdentifier,
  resetPagination
} from "../state.js";
import { createFormDataObject } from "../utils.js";
import { getErrorMessage } from "./event-helpers.js";

export function createInviteEvents({
  state,
  modalController,
  renderApplication,
  renderModalRegion,
  handleSessionError,
  loadCurrentPage
}) {
  async function reloadInviteCollectionFromFirstPage() {
    const previousInviteCodes = state.data.invites;
    const previousPagination = {
      ...state.pagination.invites,
      previousCursors: [...state.pagination.invites.previousCursors]
    };

    resetPagination("invites");
    const loaded = await loadCurrentPage({ refreshing: true });
    if (!loaded && state.authenticated) {
      state.data.invites = previousInviteCodes;
      state.pagination.invites = previousPagination;
      renderApplication();
    }
    return loaded;
  }

  function openCreateModal() {
    modalController.openModal({ type: "createInvite", busy: false, error: "" });
  }

  async function submitCreate(formElement) {
    const formData = createFormDataObject(formElement);
    modalController.setModalBusy(true);
    try {
      const createResponse = await createInviteCode({
        registration_limit: Number(formData.registration_limit)
      });
      state.modal = {
        type: "secret",
        secretType: "invite",
        secret: createResponse.code,
        title: "邀请码已创建",
        subtitle: `最多可注册 ${createResponse.invite_code?.registration_limit || 1} 位用户`
      };
      await reloadInviteCollectionFromFirstPage();
    } catch (error) {
      if (!handleSessionError(error)) {
        modalController.setModalBusy(false, getErrorMessage(error));
      }
    }
  }

  async function toggleEnabled(inviteIdentifier) {
    const inviteCode = findItemByIdentifier(state.data.invites, inviteIdentifier);
    if (!inviteCode) {
      return;
    }
    try {
      const updatedInviteCode = await updateInviteCode(inviteIdentifier, {
        enabled: !inviteCode.enabled
      });
      state.data.invites = replaceItemByIdentifier(state.data.invites, updatedInviteCode);
      renderApplication();
      showToast(
        updatedInviteCode.enabled ? "邀请码已启用" : "邀请码已停用",
        "注册策略已即时更新。",
        "success"
      );
    } catch (error) {
      if (!handleSessionError(error)) {
        showToast("操作失败", getErrorMessage(error), "error");
      }
    }
  }

  async function openRedemptions(inviteIdentifier) {
    const inviteCode = findItemByIdentifier(state.data.invites, inviteIdentifier);
    if (!inviteCode) {
      showToast("无法加载注册记录", "邀请码不存在，请刷新页面后重试。", "error");
      return;
    }

    modalController.openModal({
      type: "inviteRedemptions",
      inviteIdentifier,
      inviteCode,
      loading: true,
      loadingRecords: false,
      error: "",
      redemptions: [],
      cursor: "",
      nextCursor: "",
      previousCursors: [],
      hasMore: false,
      pageSize: 50
    });
    await loadRedemptionsPage({ initialLoad: true });
  }

  async function loadRedemptionsPage(options = {}) {
    const modal = state.modal;
    if (modal?.type !== "inviteRedemptions") {
      return false;
    }

    const inviteIdentifier = modal.inviteIdentifier;
    const requestedCursor = modal.cursor;
    const requestedPageSize = modal.pageSize;
    const requestContext = modalController.startModalRequest();
    try {
      const response = await fetchInviteCodeRedemptions(inviteIdentifier, {
        signal: requestContext.requestController.signal,
        cursor: requestedCursor,
        limit: requestedPageSize
      });
      if (isMatchingRedemptionsRequest(
        requestContext,
        inviteIdentifier,
        requestedCursor,
        requestedPageSize
      )) {
        state.modal.loading = false;
        state.modal.loadingRecords = false;
        state.modal.error = "";
        state.modal.redemptions = Array.isArray(response?.redemptions)
          ? response.redemptions
          : [];
        state.modal.nextCursor = String(response?.next_cursor || "");
        state.modal.hasMore = Boolean(response?.has_more && state.modal.nextCursor);
        renderModalRegion();
        return true;
      }
    } catch (error) {
      if (error?.name !== "AbortError"
        && modalController.isCurrentModalRequest(requestContext)
        && !handleSessionError(error)) {
        if (options.initialLoad && state.modal?.type === "inviteRedemptions") {
          state.modal.loading = false;
          state.modal.error = getErrorMessage(error);
          renderModalRegion();
        } else {
          showToast("无法加载注册记录", getErrorMessage(error), "error");
        }
      }
    } finally {
      modalController.finishModalRequest(requestContext);
    }
    return false;
  }

  async function changeRedemptionsPage(direction) {
    if (state.modal?.type !== "inviteRedemptions" || state.modal.loadingRecords) {
      return;
    }

    const inviteIdentifier = state.modal.inviteIdentifier;
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
    const loaded = await loadRedemptionsPage();
    if (!loaded && isInviteRedemptionsModalFor(state.modal, inviteIdentifier)) {
      state.modal.cursor = paginationSnapshot.cursor;
      state.modal.nextCursor = paginationSnapshot.nextCursor;
      state.modal.previousCursors = [...paginationSnapshot.previousCursors];
      state.modal.hasMore = paginationSnapshot.hasMore;
      state.modal.pageSize = paginationSnapshot.pageSize;
      state.modal.loadingRecords = false;
      renderModalRegion();
    }
  }

  async function changeRedemptionsPageSize(requestedPageSize) {
    if (state.modal?.type !== "inviteRedemptions" || state.modal.loadingRecords) {
      return;
    }

    const pageSize = Number(requestedPageSize);
    if (!COLLECTION_PAGE_SIZE_OPTIONS.includes(pageSize) || pageSize === state.modal.pageSize) {
      return;
    }

    const inviteIdentifier = state.modal.inviteIdentifier;
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
    const loaded = await loadRedemptionsPage();
    if (!loaded && isInviteRedemptionsModalFor(state.modal, inviteIdentifier)) {
      state.modal.cursor = paginationSnapshot.cursor;
      state.modal.nextCursor = paginationSnapshot.nextCursor;
      state.modal.previousCursors = [...paginationSnapshot.previousCursors];
      state.modal.hasMore = paginationSnapshot.hasMore;
      state.modal.pageSize = paginationSnapshot.pageSize;
      state.modal.loadingRecords = false;
      renderModalRegion();
    }
  }

  function isMatchingRedemptionsRequest(
    requestContext,
    inviteIdentifier,
    requestedCursor,
    requestedPageSize
  ) {
    return modalController.isCurrentModalRequest(requestContext)
      && state.modal?.type === "inviteRedemptions"
      && state.modal.inviteIdentifier === inviteIdentifier
      && state.modal.cursor === requestedCursor
      && state.modal.pageSize === requestedPageSize;
  }

  function openDeleteConfirmation(inviteIdentifier) {
    const inviteCode = findItemByIdentifier(state.data.invites, inviteIdentifier);
    modalController.openModal({
      type: "confirm",
      confirmAction: "deleteInvite",
      identifier: inviteIdentifier,
      title: "删除邀请码",
      message: `删除“${inviteCode?.code_prefix || "该邀请码"}”后，尚未使用的注册名额也会立即失效。`,
      confirmLabel: "删除邀请码",
      busy: false,
      error: ""
    });
  }

  async function deleteConfirmed(inviteIdentifier) {
    await deleteInviteCode(inviteIdentifier);
    await reloadInviteCollectionFromFirstPage();
  }

  return {
    openCreateModal,
    submitCreate,
    toggleEnabled,
    openRedemptions,
    changeRedemptionsPage,
    changeRedemptionsPageSize,
    openDeleteConfirmation,
    deleteConfirmed
  };
}

function isInviteRedemptionsModalFor(modal, inviteIdentifier) {
  return modal?.type === "inviteRedemptions"
    && modal.inviteIdentifier === inviteIdentifier;
}
