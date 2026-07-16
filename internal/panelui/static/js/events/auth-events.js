import { login, panelAPI, register } from "../api.js";
import { renderIcon } from "../components/icons.js";
import { showToast } from "../components/toast.js";
import { clearAuthenticatedState, clearCachedData } from "../state.js";
import { createFormDataObject } from "../utils.js";
import { getErrorMessage, withRetryAfter } from "./event-helpers.js";

export function createAuthEvents({
  state,
  modalController,
  renderApplication,
  loadCurrentPage,
  abortCurrentPageLoad
}) {
  function switchAuthMode(mode) {
    state.authMode = mode || "login";
    state.authError = "";
    renderApplication();
  }

  function togglePasswordVisibility(actionElement) {
    const passwordInput = document.getElementById(actionElement.dataset.target);
    if (!passwordInput) {
      return;
    }
    const shouldShowPassword = passwordInput.type === "password";
    passwordInput.type = shouldShowPassword ? "text" : "password";
    actionElement.innerHTML = renderIcon(shouldShowPassword ? "eyeOff" : "eye");
    passwordInput.focus();
  }

  function logout() {
    abortCurrentPageLoad();
    modalController.abortCurrentModalRequest();
    panelAPI.clearSession();
    clearAuthenticatedState();
    state.authError = "";
    state.currentPage = "overview";
    window.history.replaceState(null, "", `${window.location.pathname}${window.location.search}`);
    renderApplication();
    showToast("已退出", "当前会话已从浏览器标签页中清除。", "success");
  }

  async function submitLogin(formElement) {
    const credentials = createFormDataObject(formElement);
    state.authBusy = true;
    state.authError = "";
    renderApplication();

    try {
      const loginResponse = await login({
        username: String(credentials.username || "").trim(),
        password: String(credentials.password || "")
      });
      panelAPI.saveSession(loginResponse.token, loginResponse.expires_at);
      state.user = loginResponse.user;
      state.authenticated = true;
      state.currentPage = "overview";
      state.authBusy = false;
      state.authError = "";
      clearCachedData();
      window.history.replaceState(null, "", "#overview");
      renderApplication();
      showToast("欢迎回来", `已以 ${state.user.username} 的身份安全登录。`, "success");
      await loadCurrentPage();
    } catch (error) {
      state.authBusy = false;
      state.authError = withRetryAfter(getErrorMessage(error), error);
      renderApplication();
    }
  }

  async function submitRegistration(formElement) {
    const registrationData = createFormDataObject(formElement);
    state.authBusy = true;
    state.authError = "";
    renderApplication();

    try {
      await register({
        username: String(registrationData.username || "").trim(),
        password: String(registrationData.password || ""),
        ...(state.registrationMode === "invite"
          ? { invite_code: String(registrationData.invite_code || "").trim() }
          : {})
      });
      state.authBusy = false;
      state.authMode = "login";
      state.authError = "";
      renderApplication();
      showToast("账户已创建", "请使用刚刚设置的用户名和密码登录。", "success");
    } catch (error) {
      state.authBusy = false;
      state.authError = withRetryAfter(getErrorMessage(error), error);
      renderApplication();
    }
  }

  return {
    switchAuthMode,
    togglePasswordVisibility,
    logout,
    submitLogin,
    submitRegistration
  };
}
