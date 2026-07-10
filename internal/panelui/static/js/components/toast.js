import { renderIcon } from "./icons.js";

let configuredToastRegionElement = null;

export function configureToastRegion(toastRegionElement) {
  configuredToastRegionElement = toastRegionElement;
}

export function showToast(title, message, type = "success") {
  const toastRegionElement = configuredToastRegionElement || document.querySelector("#toast-region");
  if (!toastRegionElement) {
    return;
  }

  const toastElement = document.createElement("article");
  toastElement.className = `toast ${type === "error" ? "is-error" : ""}`;
  toastElement.innerHTML = `
    <span class="toast-icon">${renderIcon(type === "error" ? "alert" : "check")}</span>
    <span class="toast-copy"><strong></strong><span></span></span>
    <button class="toast-close" type="button" aria-label="关闭通知">${renderIcon("close")}</button>
  `;
  toastElement.querySelector("strong").textContent = title;
  toastElement.querySelector(".toast-copy span").textContent = message;

  const dismissToast = () => {
    if (toastElement.classList.contains("is-leaving")) {
      return;
    }
    toastElement.classList.add("is-leaving");
    window.setTimeout(() => toastElement.remove(), 220);
  };

  toastElement.querySelector("button").addEventListener("click", dismissToast);
  toastRegionElement.appendChild(toastElement);
  window.setTimeout(dismissToast, type === "error" ? 6500 : 4300);
}
