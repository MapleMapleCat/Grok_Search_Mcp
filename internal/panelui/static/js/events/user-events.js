import { deleteAdminUser, updateAdminUser } from "../api.js";
import { showToast } from "../components/toast.js";
import { findItemByIdentifier, removeItemByIdentifier, replaceItemByIdentifier } from "../state.js";
import { formatNumber, normalizeSearchValue } from "../utils.js";
import { handleModalMutationError, openConfirmationModal } from "./event-helpers.js";

export function createUserEvents({
  state,
  applicationElement,
  modalController,
  renderApplication,
  handleSessionError
}) {
  function openEditModal(userIdentifier) {
    const user = findItemByIdentifier(state.data.users, userIdentifier);
    if (!user) {
      showToast("用户不存在", "请刷新页面后重试。", "error");
      return;
    }
    modalController.openModal({ type: "editUser", data: { ...user }, busy: false, error: "" });
  }

  async function submitEdit(formElement) {
    const userIdentifier = formElement.dataset.id;
    const updatePayload = {
      tier_id: String(formElement.elements.tier_id.value || "").trim(),
      revoke_tokens: formElement.elements.revoke_tokens.checked
    };
    if (!formElement.elements.role.disabled) {
      updatePayload.role = formElement.elements.role.value;
    }
    if (!formElement.elements.enabled.disabled) {
      updatePayload.enabled = formElement.elements.enabled.checked;
    }

    modalController.setModalBusy(true);
    try {
      const updatedUser = await updateAdminUser(userIdentifier, updatePayload);
      state.data.users = replaceItemByIdentifier(state.data.users, updatedUser);
      if (updatedUser.id === state.user?.id) {
        state.user = updatedUser;
      }
      modalController.closeModal();
      renderApplication();
      showToast("用户已更新", "角色、等级与会话策略已应用。", "success");
    } catch (error) {
      handleModalMutationError(error, modalController, handleSessionError);
    }
  }

  function updateSearchFilter(searchValue) {
    state.filters.userSearch = searchValue;

    const searchResultsElement = applicationElement?.querySelector("[data-user-search-results]");
    if (!searchResultsElement) {
      return;
    }

    const normalizedSearch = normalizeSearchValue(searchValue);
    const userRowElements = searchResultsElement.querySelectorAll("[data-user-search-username]");
    let matchingUserCount = 0;

    for (const userRowElement of userRowElements) {
      const normalizedUsername = String(userRowElement.dataset.userSearchUsername || "");
      const normalizedUserIdentifier = String(userRowElement.dataset.userSearchId || "");
      const userMatchesSearch = !normalizedSearch
        || normalizedUsername.includes(normalizedSearch)
        || normalizedUserIdentifier.includes(normalizedSearch);
      userRowElement.hidden = !userMatchesSearch;
      if (userMatchesSearch) {
        matchingUserCount += 1;
      }
    }

    const hasMatchingUsers = matchingUserCount > 0;
    const emptyStateElement = searchResultsElement.querySelector("[data-user-search-empty]");
    const userTableElement = searchResultsElement.querySelector("[data-user-search-table]");
    const paginationElement = searchResultsElement.querySelector("[data-user-search-pagination]");
    const paginationCountElement = paginationElement?.querySelector("[data-pagination-current-count]");

    if (emptyStateElement) {
      emptyStateElement.hidden = hasMatchingUsers;
    }
    if (userTableElement) {
      userTableElement.hidden = !hasMatchingUsers;
    }
    if (paginationElement) {
      paginationElement.hidden = !hasMatchingUsers;
    }
    if (paginationCountElement) {
      paginationCountElement.textContent = formatNumber(matchingUserCount);
    }
  }

  function openDeleteConfirmation(userIdentifier) {
    const user = findItemByIdentifier(state.data.users, userIdentifier);
    openConfirmationModal(modalController, {
      confirmAction: "deleteUser",
      identifier: userIdentifier,
      title: "删除用户",
      message: `删除“${user?.username || "该用户"}”会同时删除其全部 API 密钥与调用日志，且无法恢复。`,
      confirmLabel: "删除用户"
    });
  }

  async function deleteConfirmed(userIdentifier) {
    await deleteAdminUser(userIdentifier);
    state.data.users = removeItemByIdentifier(state.data.users, userIdentifier);
  }

  return {
    openEditModal,
    submitEdit,
    updateSearchFilter,
    openDeleteConfirmation,
    deleteConfirmed
  };
}
