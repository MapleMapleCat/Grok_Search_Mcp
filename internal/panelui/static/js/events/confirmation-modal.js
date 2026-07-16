import { showToast } from "../components/toast.js";
import { getErrorMessage } from "./event-helpers.js";

export function createConfirmationModalEvents({
  state,
  modalController,
  executors,
  renderApplication,
  handleSessionError
}) {
  async function executeConfirmedAction() {
    if (state.modal?.type !== "confirm") {
      return;
    }

    const { confirmAction, identifier } = state.modal;
    const executeAction = executors[confirmAction];
    modalController.setModalBusy(true);

    try {
      if (!executeAction) {
        throw new Error("未知的确认操作。");
      }
      await executeAction(identifier);
      modalController.closeModal();
      renderApplication();
      showToast("删除成功", "资源已从服务中永久移除。", "success");
    } catch (error) {
      if (!handleSessionError(error)) {
        modalController.setModalBusy(false, getErrorMessage(error));
      }
    }
  }

  return { executeConfirmedAction };
}
