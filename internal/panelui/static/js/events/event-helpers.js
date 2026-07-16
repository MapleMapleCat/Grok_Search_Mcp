import { showToast } from "../components/toast.js";

export async function copyValue(value) {
  if (!value) {
    return;
  }

  try {
    await navigator.clipboard.writeText(value);
    showToast("已复制", "内容已写入剪贴板。", "success");
  } catch {
    const fallbackTextArea = document.createElement("textarea");
    fallbackTextArea.value = value;
    fallbackTextArea.setAttribute("readonly", "");
    fallbackTextArea.style.position = "fixed";
    fallbackTextArea.style.opacity = "0";
    document.body.appendChild(fallbackTextArea);
    fallbackTextArea.select();

    const copySucceeded = document.execCommand("copy");
    fallbackTextArea.remove();
    showToast(
      copySucceeded ? "已复制" : "复制失败",
      copySucceeded ? "内容已写入剪贴板。" : "请手动选择并复制内容。",
      copySucceeded ? "success" : "error"
    );
  }
}

export function getErrorMessage(error) {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return "发生未知错误，请稍后重试。";
}

export function withRetryAfter(message, error) {
  if (Number.isFinite(error?.retryAfterSeconds) && error.retryAfterSeconds > 0) {
    return `${message} 约 ${Math.ceil(error.retryAfterSeconds)} 秒后可重试。`;
  }
  return message;
}
