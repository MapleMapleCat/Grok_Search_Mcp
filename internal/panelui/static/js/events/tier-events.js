import { createTier, deleteTier, updateTier } from "../api.js";
import { showToast } from "../components/toast.js";
import {
  COLLECTION_PAGE_SIZE,
  compareTiers,
  findItemByIdentifier,
  removeItemByIdentifier,
  replaceItemByIdentifier
} from "../state.js";
import { createFormDataObject } from "../utils.js";
import { handleModalMutationError, openConfirmationModal } from "./event-helpers.js";

export function createTierEvents({
  state,
  modalController,
  renderApplication,
  handleSessionError
}) {
  function openCreateModal() {
    modalController.openModal({ type: "createTier", busy: false, error: "" });
  }

  function openEditModal(tierIdentifier) {
    const tier = findItemByIdentifier(state.data.tiers, tierIdentifier);
    if (!tier) {
      showToast("等级不存在", "请刷新页面后重试。", "error");
      return;
    }
    modalController.openModal({ type: "editTier", data: { ...tier }, busy: false, error: "" });
  }

  async function submitCreate(formElement) {
    await submitTier(formElement, false);
  }

  async function submitEdit(formElement) {
    await submitTier(formElement, true);
  }

  async function submitTier(formElement, isEdit) {
    const formData = createFormDataObject(formElement);
    const tierPayload = {
      name: String(formData.name || "").trim(),
      rpm: Number(formData.rpm),
      success_limit: Number(formData.success_limit),
      is_default: Boolean(formElement.elements.is_default.checked)
    };
    const tierIdentifier = formElement.dataset.id;
    modalController.setModalBusy(true);

    try {
      const tier = isEdit
        ? await updateTier(tierIdentifier, tierPayload)
        : await createTier(tierPayload);

      const existingTiers = state.data.tiers || [];
      const normalizedExistingTiers = tier.is_default
        ? existingTiers.map((existingTier) => ({ ...existingTier, is_default: false }))
        : existingTiers;
      if (isEdit) {
        state.data.tiers = replaceItemByIdentifier(normalizedExistingTiers, tier);
      } else {
        state.data.tiers = [...normalizedExistingTiers, tier]
          .sort(compareTiers)
          .slice(0, COLLECTION_PAGE_SIZE);
      }
      if (tier.is_default) {
        state.data.defaultTier = tier;
      }
      modalController.closeModal();
      renderApplication();
      const successMessage = tier.is_default
        ? "该方案现为新用户的默认配额方案。"
        : "新的配额方案已可以分配给用户。";
      showToast(isEdit ? "方案已更新" : "方案已创建", successMessage, "success");
    } catch (error) {
      handleModalMutationError(error, modalController, handleSessionError);
    }
  }

  function openDeleteConfirmation(tierIdentifier) {
    const tier = findItemByIdentifier(state.data.tiers, tierIdentifier);
    openConfirmationModal(modalController, {
      confirmAction: "deleteTier",
      identifier: tierIdentifier,
      title: "删除配额方案",
      message: `将永久删除“${tier?.name || "该方案"}”。默认方案或仍有用户使用的方案无法删除。`,
      confirmLabel: "删除方案"
    });
  }

  async function deleteConfirmed(tierIdentifier) {
    await deleteTier(tierIdentifier);
    state.data.tiers = removeItemByIdentifier(state.data.tiers, tierIdentifier);
  }

  return {
    openCreateModal,
    openEditModal,
    submitCreate,
    submitEdit,
    openDeleteConfirmation,
    deleteConfirmed
  };
}
