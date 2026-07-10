import { escapeHTML, formatPercent } from "../utils.js";
import { renderIcon } from "./icons.js";

export function renderMetricCard(label, value, note, icon, color, iconColor, positive = false, visualType = "trend", visualValue = 0) {
  return `
    <article class="metric-card metric-card--${visualType}" style="--metric-color:${color};--metric-ink:${iconColor}">
      <div class="metric-card-head"><span class="metric-label">${escapeHTML(label)}</span><span class="metric-icon">${renderIcon(icon)}</span></div>
      <div class="metric-card-body">
        <strong class="metric-value">${escapeHTML(value)}</strong>
        ${renderMetricVisual(visualType, visualValue)}
      </div>
      <span class="metric-note ${positive ? "is-positive" : ""}">${positive ? renderIcon("check") : ""}${escapeHTML(note)}</span>
    </article>
  `;
}

function renderMetricVisual(visualType, visualValue) {
  const numericValue = Number(visualValue);

  if (visualType === "ring") {
    const progressPercent = Number.isFinite(numericValue) ? Math.max(0, Math.min(100, numericValue)) : 0;
    return `
      <span class="metric-visual metric-visual-ring" style="--metric-progress:${progressPercent.toFixed(1)}%" aria-hidden="true">
        <span>${escapeHTML(formatPercent(progressPercent, 0))}</span>
      </span>
    `;
  }

  if (visualType === "pulse") {
    return `
      <span class="metric-visual metric-visual-pulse" aria-hidden="true">
        <span class="metric-live-label"><i></i> Live</span>
        <svg width="96" height="42" viewBox="0 0 96 42" preserveAspectRatio="none">
          <path class="metric-pulse-base" d="M2 25 H94" />
          <path class="metric-pulse-line" d="M2 25 H18 L25 25 L31 8 L38 36 L46 17 L53 25 H68 L74 14 L80 30 L86 25 H94" />
        </svg>
      </span>
    `;
  }

  if (visualType === "nodes") {
    const activeNodeCount = Number.isFinite(numericValue) ? Math.max(1, Math.min(6, Math.round(numericValue))) : 1;
    const nodes = Array.from({ length: 6 }, (_, nodeIndex) => `<i class="${nodeIndex < activeNodeCount ? "is-active" : ""}"></i>`).join("");
    return `
      <span class="metric-visual metric-visual-nodes" aria-hidden="true">
        <svg width="72" height="42" viewBox="0 0 72 42" preserveAspectRatio="none"><path d="M10 10 L36 21 L62 9 M36 21 L59 35 M36 21 L12 34" /></svg>
        <span>${nodes}</span>
      </span>
    `;
  }

  return `
    <span class="metric-visual metric-visual-trend" aria-hidden="true">
      <svg width="96" height="44" viewBox="0 0 96 44" preserveAspectRatio="none">
        <path class="metric-trend-area" d="M2 39 C15 36 18 26 29 29 C41 32 42 16 54 20 C66 24 70 7 82 12 C87 14 91 8 94 5 V42 H2 Z" />
        <path class="metric-trend-line" d="M2 39 C15 36 18 26 29 29 C41 32 42 16 54 20 C66 24 70 7 82 12 C87 14 91 8 94 5" />
        <circle cx="94" cy="5" r="3" />
      </svg>
    </span>
  `;
}
