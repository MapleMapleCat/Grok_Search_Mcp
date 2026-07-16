import { createInviteCode, deleteInviteCode, updateInviteCode } from "../api.js";
import { showToast } from "../components/toast.js";
import {
  COLLECTION_PAGE_SIZE,
  removeItemByIdentifier,
  replaceItemByIdentifier
} from "../state.js";
import { createFormDataObject } from "../utils.js";
import { getErrorMessage } from "./event-helpers.js";

export function createInviteEvents({
  state,
  modalController,
  renderApplication,
  handleSessionError
}) {
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
      state.data.invites = [createResponse.invite_code, ...(state.data.invites || [])]
        .slice(0, COLLECTION_PAGE_SIZE);
      state.modal = {
        type: "secret",
        secretType: "invite",
        secret: createResponse.code,
        title: "邀请码已创建",
        subtitle: `最多可注册 ${createResponse.invite_code?.registration_limit || 1} 位用户`
      };
      renderApplication();
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
    state.data.invites = removeItemByIdentifier(state.data.invites, inviteIdentifier);
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
    openDeleteConfirmation,
    deleteConfirmed
  };
}
