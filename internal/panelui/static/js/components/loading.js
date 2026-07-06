import { escapeHTML } from "../utils.js";

export function renderInlineLoading() {
  return `<section class="card empty"><div><div class="spinner"></div><p>Loading current view...</p></div></section>`;
}

export function renderLoading(text) {
  return `<main class="loading-screen"><div><div class="spinner"></div><p>${escapeHTML(text)}</p></div></main>`;
}
