export function createModalController({
  state,
  modalRegionElement,
  renderModalRegion
}) {
  let activeModalRequestController = null;
  let activeModalRequestIdentifier = 0;

  function abortCurrentModalRequest() {
    activeModalRequestController?.abort();
    activeModalRequestController = null;
    activeModalRequestIdentifier += 1;
  }

  function startModalRequest() {
    abortCurrentModalRequest();
    const requestController = new AbortController();
    const requestIdentifier = activeModalRequestIdentifier;
    activeModalRequestController = requestController;
    return { requestController, requestIdentifier };
  }

  function isCurrentModalRequest(requestContext) {
    return requestContext.requestIdentifier === activeModalRequestIdentifier
      && activeModalRequestController === requestContext.requestController;
  }

  function finishModalRequest(requestContext) {
    if (activeModalRequestController === requestContext.requestController) {
      activeModalRequestController = null;
    }
  }

  function openModal(modalState) {
    abortCurrentModalRequest();
    state.modal = modalState;
    renderModalRegion();
    window.requestAnimationFrame(() => {
      modalRegionElement.querySelector("[autofocus]")?.focus();
    });
  }

  function closeModal() {
    abortCurrentModalRequest();
    state.modal = null;
    renderModalRegion();
  }

  function setModalBusy(busy, error = "") {
    if (!state.modal) {
      return;
    }
    state.modal.busy = busy;
    state.modal.error = error;
    renderModalRegion();
  }

  return {
    abortCurrentModalRequest,
    startModalRequest,
    isCurrentModalRequest,
    finishModalRequest,
    openModal,
    closeModal,
    setModalBusy
  };
}
