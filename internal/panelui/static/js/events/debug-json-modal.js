import { fetchUsageRecordDetail } from "../api.js";
import { showToast } from "../components/toast.js";
import { getErrorMessage } from "./event-helpers.js";

export function buildCopyableDebugJSON(record = {}) {
  const rawDebugJSON = String(record.debug_json || "");

  try {
    const parsedDebugJSON = JSON.parse(rawDebugJSON);
    if (!isPlainObject(parsedDebugJSON)) {
      return rawDebugJSON;
    }

    const completeDebugJSON = { ...parsedDebugJSON };
    if (typeof record.debug_request_body === "string") {
      completeDebugJSON.request = {
        ...(isPlainObject(parsedDebugJSON.request) ? parsedDebugJSON.request : {}),
        body: record.debug_request_body
      };
    }
    if (typeof record.debug_response_body === "string") {
      completeDebugJSON.response = {
        ...(isPlainObject(parsedDebugJSON.response) ? parsedDebugJSON.response : {}),
        body: record.debug_response_body
      };
    }

    return JSON.stringify(completeDebugJSON, null, 2);
  } catch {
    return rawDebugJSON;
  }
}

export function createDebugJSONModalEvents({
  state,
  modalController,
  renderModalRegion,
  handleSessionError,
  copyValue
}) {
  async function openDebugJSONModal(recordIdentifier) {
    const pageUsageRecords = state.data.usage?.records || [];
    const modalUsageRecords = state.modal?.usage?.records || [];
    const matchingRecord = [...modalUsageRecords, ...pageUsageRecords].find(
      (usageRecord) => String(usageRecord.id) === String(recordIdentifier)
    );

    if (!matchingRecord?.debug_json) {
      showToast("调试详情不可用", "该调用没有可展示的调试数据，请刷新后重试。", "error");
      return;
    }

    modalController.openModal({
      type: "debugJSON",
      record: { ...matchingRecord },
      loading: true,
      busy: false,
      error: ""
    });

    const requestContext = modalController.startModalRequest();
    try {
      const recordDetail = await fetchUsageRecordDetail(recordIdentifier, {
        signal: requestContext.requestController.signal
      });
      if (isMatchingDebugRequest(requestContext, recordIdentifier)) {
        state.modal.record = recordDetail;
        state.modal.loading = false;
        renderModalRegion();
      }
    } catch (error) {
      if (error?.name !== "AbortError"
        && isMatchingDebugRequest(requestContext, recordIdentifier)
        && !handleSessionError(error)) {
        state.modal.loading = false;
        state.modal.error = getErrorMessage(error);
        renderModalRegion();
      }
    } finally {
      modalController.finishModalRequest(requestContext);
    }
  }

  async function copyDebugJSON() {
    if (state.modal?.type !== "debugJSON") {
      return;
    }
    await copyValue(buildCopyableDebugJSON(state.modal.record));
  }

  function isMatchingDebugRequest(requestContext, recordIdentifier) {
    return modalController.isCurrentModalRequest(requestContext)
      && state.modal?.type === "debugJSON"
      && String(state.modal.record?.id) === String(recordIdentifier);
  }

  return {
    openDebugJSONModal,
    copyDebugJSON
  };
}

function isPlainObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
