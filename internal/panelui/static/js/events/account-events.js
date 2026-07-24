import { changePassword, revokeSessions } from "../api.js";
import { showToast } from "../components/toast.js";
import { createFormDataObject } from "../utils.js";
import { getErrorMessage, withRetryAfter } from "./event-helpers.js";

export function createAccountEvents({ state, renderApplication, handleSessionError }) {
  async function runSessionMutation({ request, successTitle, successMessage, errorTitle }) {
    state.formBusy = true;
    renderApplication();
    try {
      const replacementSession = await request();
      state.user = replacementSession.user;
      showToast(successTitle, successMessage, "success");
    } catch (error) {
      if (!handleSessionError(error)) {
        showToast(errorTitle, withRetryAfter(getErrorMessage(error), error), "error");
      }
    } finally {
      state.formBusy = false;
      if (state.authenticated) {
        renderApplication();
      }
    }
  }

  async function submitPasswordChange(formElement) {
    const formData = createFormDataObject(formElement);
    const currentPassword = String(formData.current_password || "");
    const newPassword = String(formData.new_password || "");
    const confirmedPassword = String(formData.confirm_new_password || "");
    if (newPassword !== confirmedPassword) {
      showToast("无法修改密码", "两次输入的新密码不一致。", "error");
      return;
    }

    await runSessionMutation({
      request: () => changePassword({
        current_password: currentPassword,
        new_password: newPassword
      }),
      successTitle: "密码已更新",
      successMessage: "旧会话已全部失效，当前标签页已切换到新会话。",
      errorTitle: "无法修改密码"
    });
  }

  async function submitSessionRevocation() {
    await runSessionMutation({
      request: revokeSessions,
      successTitle: "会话已吊销",
      successMessage: "此前签发的所有面板会话均已失效。",
      errorTitle: "无法吊销会话"
    });
  }

  return { submitPasswordChange, submitSessionRevocation };
}
