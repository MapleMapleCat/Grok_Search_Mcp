import {
  createInviteCode,
  deleteInviteCode,
  fetchInviteCodeRedemptions,
  updateInviteCode
} from "../api.js";
import { showToast } from "../components/toast.js";
import { replaceItemByIdentifier, resetPagination } from "../state.js";
import { createFormDataObject } from "../utils.js";
import { getErrorMessage } from "./event-helpers.js";

export function createInviteEvents({
  state,
  modalController,
  renderApplication,
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
    const inviteCode = findInvite(inviteIdentifier);
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
    const inviteCode = findInvite(inviteIdentifier);
    if (!inviteCode) {
      showToast("无法加载注册记录", "邀请码不存在，请刷新页面后重试。", "error");
      return;
    }

    modalController.openModal({
      type: "inviteRedemptions",
      inviteIdentifier,
      inviteCode,
      loading: true,
      error: "",
      redemptions: []
    });
    const requestContext = modalController.startModalRequest();
    try {
      const response = await fetchInviteCodeRedemptions(inviteIdentifier, {
        signal: requestContext.requestController.signal
      });
      if (modalController.isCurrentModalRequest(requestContext)
        && state.modal?.type === "inviteRedemptions"
        && state.modal.inviteIdentifier === inviteIdentifier) {
        state.modal.loading = false;
        state.modal.redemptions = Array.isArray(response?.redemptions)
          ? response.redemptions
          : [];
        renderApplication();
      }
    } catch (error) {
      if (error?.name !== "AbortError"
        && modalController.isCurrentModalRequest(requestContext)
        && !handleSessionError(error)) {
        state.modal.loading = false;
        state.modal.error = getErrorMessage(error);
        renderApplication();
      }
    } finally {
      modalController.finishModalRequest(requestContext);
    }
  }

  function openDeleteConfirmation(inviteIdentifier) {
    const inviteCode = findInvite(inviteIdentifier);
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

  function findInvite(inviteIdentifier) {
    return (state.data.invites || []).find(
      (candidateInvite) => candidateInvite.id === inviteIdentifier
    );
  }

  return {
    openCreateModal,
    submitCreate,
    toggleEnabled,
    openRedemptions,
    openDeleteConfirmation,
    deleteConfirmed
  };
}
