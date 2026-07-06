import { render } from "../../app.js";
import { state } from "../state.js";
import { escapeHTML } from "../utils.js";

export function notify(message, type) {
  const id = `${Date.now()}-${Math.random()}`;
  state.toast = { id, message, type: type || "success" };
  window.clearTimeout(notify.timer);
  notify.timer = window.setTimeout(() => {
    if (state.toast && state.toast.id === id) {
      state.toast = null;
      render();
    }
  }, 3600);
}

export function renderToast() {
  if (!state.toast) return "";
  return `
    <aside class="toast ${state.toast.type}">
      <span class="material-symbols-outlined">${state.toast.type === "error" ? "error" : "check_circle"}</span>
      <div>${escapeHTML(state.toast.message)}</div>
    </aside>`;
}
