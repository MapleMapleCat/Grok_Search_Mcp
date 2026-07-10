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
    const hasRecentActivity = Number.isFinite(numericValue) && numericValue > 0;
    return `
      <span class="metric-visual metric-visual-pulse ${hasRecentActivity ? "is-active" : "is-idle"}" aria-hidden="true">
        <span class="metric-live-label"><i></i>${hasRecentActivity ? "活跃" : "空闲"}</span>
        <svg width="96" height="42" viewBox="0 0 96 42" preserveAspectRatio="none">
          <path class="metric-pulse-base" d="M2 25 H94" />
          <path class="metric-pulse-line" d="${hasRecentActivity ? "M2 25 H18 L25 25 L31 8 L38 36 L46 17 L53 25 H68 L74 14 L80 30 L86 25 H94" : "M2 25 H94"}" />
        </svg>
      </span>
    `;
  }

  if (visualType === "nodes") {
    const activeNodeCount = Number.isFinite(numericValue) ? Math.max(0, Math.min(6, Math.round(numericValue))) : 0;
    const nodes = Array.from({ length: 6 }, (_, nodeIndex) => `<i class="${nodeIndex < activeNodeCount ? "is-active" : ""}"></i>`).join("");
    return `
      <span class="metric-visual metric-visual-nodes" aria-hidden="true">
        <svg width="72" height="42" viewBox="0 0 72 42" preserveAspectRatio="none"><path d="M10 10 L36 21 L62 9 M36 21 L59 35 M36 21 L12 34" /></svg>
        <span>${nodes}</span>
      </span>
    `;
  }

  return renderTrendVisual(visualValue);
}

function renderTrendVisual(trafficBuckets) {
  const callValues = Array.isArray(trafficBuckets)
    ? [...trafficBuckets]
      .filter((bucket) => bucket && bucket.start)
      .sort((firstBucket, secondBucket) => new Date(firstBucket.start) - new Date(secondBucket.start))
      .map((bucket) => Math.max(0, Number(bucket.calls || 0)))
    : [];

  if (callValues.length === 0) {
    return `
      <span class="metric-visual metric-visual-trend is-empty" aria-hidden="true">
        <svg width="96" height="44" viewBox="0 0 96 44" preserveAspectRatio="none">
          <path class="metric-trend-baseline" d="M2 38 H94" />
        </svg>
      </span>
    `;
  }

  const chartLeft = 2;
  const chartRight = 94;
  const chartTop = 5;
  const chartBottom = 38;
  const maximumCalls = Math.max(...callValues);
  const minimumCalls = Math.min(...callValues);
  const callRange = maximumCalls - minimumCalls;
  const pointDenominator = Math.max(1, callValues.length - 1);
  const points = callValues.map((callCount, pointIndex) => {
    const xCoordinate = chartLeft + (pointIndex / pointDenominator) * (chartRight - chartLeft);
    const yCoordinate = callRange === 0
      ? (maximumCalls === 0 ? chartBottom : (chartTop + chartBottom) / 2)
      : chartBottom - ((callCount - minimumCalls) / callRange) * (chartBottom - chartTop);
    return { xCoordinate, yCoordinate };
  });

  if (points.length === 1) {
    points.push({ ...points[0], xCoordinate: chartRight });
  }

  const linePath = points
    .map((point, pointIndex) => `${pointIndex === 0 ? "M" : "L"}${point.xCoordinate.toFixed(2)} ${point.yCoordinate.toFixed(2)}`)
    .join(" ");
  const firstPoint = points[0];
  const lastPoint = points.at(-1);
  const areaPath = `${linePath} L${lastPoint.xCoordinate.toFixed(2)} 42 L${firstPoint.xCoordinate.toFixed(2)} 42 Z`;

  return `
    <span class="metric-visual metric-visual-trend" aria-hidden="true">
      <svg width="96" height="44" viewBox="0 0 96 44" preserveAspectRatio="none">
        <path class="metric-trend-area" d="${areaPath}" />
        <path class="metric-trend-line" d="${linePath}" />
        <circle cx="${lastPoint.xCoordinate.toFixed(2)}" cy="${lastPoint.yCoordinate.toFixed(2)}" r="3" />
      </svg>
    </span>
  `;
}
