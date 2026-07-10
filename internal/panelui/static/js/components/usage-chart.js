import { escapeHTML, formatNumber } from "../utils.js";
import { renderIcon } from "./icons.js";

export function renderChart(trafficBuckets = []) {
  const sortedBuckets = [...trafficBuckets]
    .filter((bucket) => bucket && bucket.start)
    .sort((firstBucket, secondBucket) => new Date(firstBucket.start) - new Date(secondBucket.start));

  if (sortedBuckets.length === 0) {
    return `<div class="chart-empty"><div>${renderIcon("chart")}<p>当前时间范围还没有调用数据</p></div></div>`;
  }

  const chartWidth = 760;
  const chartHeight = 250;
  const padding = { top: 18, right: 16, bottom: 31, left: 34 };
  const innerWidth = chartWidth - padding.left - padding.right;
  const innerHeight = chartHeight - padding.top - padding.bottom;
  const calls = sortedBuckets.map((bucket) => Math.max(0, Number(bucket.calls || 0)));
  const maximumCalls = Math.max(1, ...calls);
  const denominator = Math.max(1, sortedBuckets.length - 1);

  const points = sortedBuckets.map((bucket, bucketIndex) => {
    const xCoordinate = padding.left + (bucketIndex / denominator) * innerWidth;
    const yCoordinate = padding.top + innerHeight - (Math.max(0, Number(bucket.calls || 0)) / maximumCalls) * innerHeight;
    return { xCoordinate, yCoordinate, bucket };
  });

  if (points.length === 1) {
    points.push({ ...points[0], xCoordinate: padding.left + innerWidth });
  }

  const linePath = points
    .map((point, pointIndex) => `${pointIndex === 0 ? "M" : "L"} ${point.xCoordinate.toFixed(2)} ${point.yCoordinate.toFixed(2)}`)
    .join(" ");
  const areaPath = `${linePath} L ${points.at(-1).xCoordinate.toFixed(2)} ${(padding.top + innerHeight).toFixed(2)} L ${points[0].xCoordinate.toFixed(2)} ${(padding.top + innerHeight).toFixed(2)} Z`;
  const horizontalGridLines = Array.from({ length: 5 }, (_, lineIndex) => {
    const yCoordinate = padding.top + (innerHeight / 4) * lineIndex;
    const labelValue = Math.round(maximumCalls - (maximumCalls / 4) * lineIndex);
    return `
      <line class="chart-grid-line" x1="${padding.left}" x2="${chartWidth - padding.right}" y1="${yCoordinate}" y2="${yCoordinate}" />
      <text class="chart-label" x="0" y="${yCoordinate + 3}">${escapeHTML(formatNumber(labelValue, { compact: true }))}</text>
    `;
  }).join("");

  const desiredLabelCount = Math.min(5, sortedBuckets.length);
  const labelIndexes = new Set(
    Array.from({ length: desiredLabelCount }, (_, labelIndex) => Math.round((labelIndex / Math.max(1, desiredLabelCount - 1)) * (sortedBuckets.length - 1)))
  );
  const horizontalLabels = sortedBuckets.map((bucket, bucketIndex) => {
    if (!labelIndexes.has(bucketIndex)) {
      return "";
    }
    const xCoordinate = padding.left + (bucketIndex / denominator) * innerWidth;
    return `<text class="chart-label" text-anchor="middle" x="${xCoordinate}" y="${chartHeight - 6}">${escapeHTML(formatChartDate(bucket.start))}</text>`;
  }).join("");

  const visiblePoints = points.length <= 16
    ? points.slice(0, sortedBuckets.length).map((point) => `<circle class="chart-point" cx="${point.xCoordinate}" cy="${point.yCoordinate}" r="3.2" />`).join("")
    : "";

  return `
    <svg class="chart-svg" width="${chartWidth}" height="${chartHeight}" viewBox="0 0 ${chartWidth} ${chartHeight}" role="img" aria-label="调用量趋势图" preserveAspectRatio="none">
      <defs>
        <linearGradient id="chartAreaGradient" x1="0" x2="0" y1="0" y2="1">
          <stop offset="0%" stop-color="#238a54" stop-opacity="0.22" />
          <stop offset="100%" stop-color="#238a54" stop-opacity="0" />
        </linearGradient>
      </defs>
      ${horizontalGridLines}
      <path class="chart-area" d="${areaPath}" />
      <path class="chart-line" d="${linePath}" />
      ${visiblePoints}
      ${horizontalLabels}
    </svg>
  `;
}

function formatChartDate(value) {
  const dateValue = new Date(value);
  if (Number.isNaN(dateValue.getTime())) {
    return "--";
  }
  return new Intl.DateTimeFormat("zh-CN", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit"
  }).format(dateValue);
}
