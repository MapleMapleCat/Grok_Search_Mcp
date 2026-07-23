import {
  createKey,
  deleteKey,
  fetchKeyUsage,
  revealKey,
  updateKey
} from "../api.js";
import { showToast } from "../components/toast.js";
import {
  COLLECTION_PAGE_SIZE,
  findItemByIdentifier,
  normalizeUsage,
  removeItemByIdentifier,
  replaceItemByIdentifier
} from "../state.js";
import { createFormDataObject } from "../utils.js";
import { getErrorMessage } from "./event-helpers.js";

export function createKeyEvents({
  state,
  modalController,
  renderApplication,
  renderModalRegion,
  handleSessionError,
  copyValue
}) {
  function openCreateModal() {
    modalController.openModal({ type: "createKey", busy: false, error: "" });
  }

  function openEditModal(keyIdentifier) {
    const apiKey = findItemByIdentifier(state.data.keys, keyIdentifier);
    if (!apiKey) {
      showToast("密钥不存在", "请刷新页面后重试。", "error");
      return;
    }
    modalController.openModal({ type: "editKey", data: { ...apiKey }, busy: false, error: "" });
  }

  async function submitCreate(formElement) {
    const formData = createFormDataObject(formElement);
    modalController.setModalBusy(true);
    try {
      const createResponse = await createKey({ name: String(formData.name || "").trim() });
      state.data.keys = [createResponse.key, ...(state.data.keys || [])]
        .slice(0, COLLECTION_PAGE_SIZE);
      state.modal = {
        type: "secret",
        secretType: "key",
        secret: createResponse.api_key,
        title: "API 密钥已创建",
        subtitle: createResponse.key?.name || "创建成功"
      };
      renderApplication();
    } catch (error) {
      if (!handleSessionError(error)) {
        modalController.setModalBusy(false, getErrorMessage(error));
      }
    }
  }

  async function submitEdit(formElement) {
    const keyIdentifier = formElement.dataset.id;
    const formData = createFormDataObject(formElement);
    modalController.setModalBusy(true);
    try {
      const updatedKey = await updateKey(keyIdentifier, {
        name: String(formData.name || "").trim(),
        enabled: formElement.elements.enabled.checked
      });
      state.data.keys = replaceItemByIdentifier(state.data.keys, updatedKey);
      modalController.closeModal();
      renderApplication();
      showToast("密钥已更新", "名称与访问状态已保存。", "success");
    } catch (error) {
      if (!handleSessionError(error)) {
        modalController.setModalBusy(false, getErrorMessage(error));
      }
    }
  }

  async function copyAPIKey(keyIdentifier, actionElement) {
    if (!keyIdentifier) {
      showToast("无法复制密钥", "密钥标识缺失，请刷新页面后重试。", "error");
      return;
    }

    actionElement.disabled = true;
    try {
      const revealResponse = await revealKey(keyIdentifier);
      await copyValue(String(revealResponse?.api_key || ""));
    } catch (error) {
      if (!handleSessionError(error)) {
        showToast("无法复制密钥", getErrorMessage(error), "error");
      }
    } finally {
      actionElement.disabled = false;
    }
  }

  async function openUsageModal(keyIdentifier) {
    const apiKey = findItemByIdentifier(state.data.keys, keyIdentifier);
    modalController.openModal({
      type: "keyUsage",
      keyIdentifier,
      title: apiKey?.name || "密钥调用分析",
      loading: true,
      usage: null
    });
    const requestContext = modalController.startModalRequest();
    try {
      const usage = await fetchKeyUsage(keyIdentifier, {
        signal: requestContext.requestController.signal
      });
      if (modalController.isCurrentModalRequest(requestContext)
        && state.modal?.type === "keyUsage"
        && state.modal.keyIdentifier === keyIdentifier) {
        state.modal.loading = false;
        state.modal.usage = normalizeUsage(usage);
        renderModalRegion();
      }
    } catch (error) {
      if (error?.name !== "AbortError"
        && modalController.isCurrentModalRequest(requestContext)
        && !handleSessionError(error)) {
        modalController.closeModal();
        showToast("无法加载密钥用量", getErrorMessage(error), "error");
      }
    } finally {
      modalController.finishModalRequest(requestContext);
    }
  }

  function openDeleteConfirmation(keyIdentifier) {
    const apiKey = findItemByIdentifier(state.data.keys, keyIdentifier);
    modalController.openModal({
      type: "confirm",
      confirmAction: "deleteKey",
      identifier: keyIdentifier,
      title: "删除 API 密钥",
      message: `删除“${apiKey?.name || "该密钥"}”后，使用它的 MCP 客户端将立即无法访问服务。此操作无法撤销。`,
      confirmLabel: "删除密钥",
      busy: false,
      error: ""
    });
  }

  async function deleteConfirmed(keyIdentifier) {
    await deleteKey(keyIdentifier);
    state.data.keys = removeItemByIdentifier(state.data.keys, keyIdentifier);
  }

  return {
    openCreateModal,
    openEditModal,
    submitCreate,
    submitEdit,
    copyAPIKey,
    openUsageModal,
    openDeleteConfirmation,
    deleteConfirmed
  };
}
