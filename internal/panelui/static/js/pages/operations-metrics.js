import { renderPageHeading } from "../components/loading.js";
import { renderMetricCard } from "../components/metric-card.js";
import { escapeHTML, formatDateTime, formatNumber } from "../utils.js";

export function renderOperationsMetricsPage(state) {
  if (state.pageLoading && !state.data.operationsMetrics) {
    return renderOperationsMetricsLoading();
  }

  const metrics = state.data.operationsMetrics || {};
  const sqlite = metrics.sqlite || {};
  const usageWriter = metrics.usage_writer || {};
  const primaryPool = sqlite.primary_write_pool || {};
  const quotaReserve = sqlite.quota_reserve || {};
  const queueLength = numericValue(usageWriter.queue_length);
  const queueCapacity = numericValue(usageWriter.queue_capacity);
  const queueUtilization = queueCapacity > 0 ? queueLength / queueCapacity : 0;
  const busyOrLockedErrors = numericValue(sqlite.busy_or_locked_errors);
  const droppedRecords = numericValue(usageWriter.dropped_records);
  const hasPressureSignal = busyOrLockedErrors > 0 || droppedRecords > 0 || queueUtilization >= 0.8;

  return `
    ${renderPageHeading("运行指标", "观察 SQLite 单写路径、配额延迟、用量批处理和 WAL 维护状态。")}
    ${renderPressureNotice(hasPressureSignal, busyOrLockedErrors, droppedRecords, queueUtilization)}

    <section class="metric-grid" aria-label="运行状态摘要">
      ${renderMetricCard(
        "数据库等待",
        formatDuration(primaryPool.wait_duration_ms),
        `${formatNumber(primaryPool.wait_count)} 次连接等待`,
        "activity",
        "#e8f1ff",
        "#3d83f6",
        numericValue(primaryPool.wait_count) === 0,
        "pulse",
        numericValue(primaryPool.wait_count)
      )}
      ${renderMetricCard(
        "锁竞争错误",
        formatNumber(busyOrLockedErrors),
        busyOrLockedErrors === 0 ? "未观察到 busy / locked" : "累计 SQLite busy / locked",
        "warning",
        busyOrLockedErrors === 0 ? "#e8f8ef" : "#fff0eb",
        busyOrLockedErrors === 0 ? "#238a54" : "#c45335",
        busyOrLockedErrors === 0,
        "pulse",
        busyOrLockedErrors
      )}
      ${renderMetricCard(
        "Usage 队列",
        `${formatNumber(queueLength)} / ${formatNumber(queueCapacity)}`,
        `最老记录 ${formatDuration(usageWriter.oldest_queued_age_ms)}`,
        "layers",
        "#eeeaff",
        "#7667f4",
        queueUtilization < 0.5,
        "nodes",
        Math.ceil(queueUtilization * 6)
      )}
      ${renderMetricCard(
        "Quota 预留",
        formatDuration(quotaReserve.average_duration_ms),
        `最大 ${formatDuration(quotaReserve.maximum_duration_ms)}`,
        "shield",
        "#fff6e5",
        "#d58a19",
        numericValue(quotaReserve.errors) === 0,
        "pulse",
        numericValue(quotaReserve.attempts)
      )}
    </section>

    <section class="operations-grid">
      ${renderConnectionPoolCard(sqlite)}
      ${renderQuotaCard(sqlite)}
      ${renderUsageWriterCard(usageWriter)}
      ${renderUsagePersistenceCard(sqlite.usage_write || {})}
      ${renderCheckpointCard(sqlite)}
      ${renderMaintenanceCard(sqlite.usage_maintenance || {})}
    </section>

    <p class="operations-captured-at">快照时间：${escapeHTML(formatDateTime(metrics.captured_at || sqlite.captured_at))}</p>
  `;
}

function renderPressureNotice(hasPressureSignal, busyOrLockedErrors, droppedRecords, queueUtilization) {
  if (!hasPressureSignal) {
    return `
      <div class="operations-notice is-healthy">
        <strong>当前未发现明显积压</strong>
        <span>继续关注连接等待、quota 最大延迟和 WAL checkpoint 趋势。</span>
      </div>
    `;
  }

  const signals = [];
  if (busyOrLockedErrors > 0) {
    signals.push(`${formatNumber(busyOrLockedErrors)} 次锁竞争错误`);
  }
  if (droppedRecords > 0) {
    signals.push(`${formatNumber(droppedRecords)} 条 usage 记录被丢弃`);
  }
  if (queueUtilization >= 0.8) {
    signals.push(`usage 队列已使用 ${(queueUtilization * 100).toFixed(0)}%`);
  }
  return `
    <div class="operations-notice is-warning">
      <strong>观察到写入压力信号</strong>
      <span>${escapeHTML(signals.join("；"))}。请结合指标增长速度判断是否需要扩容或迁移数据库。</span>
    </div>
  `;
}

function renderConnectionPoolCard(sqlite) {
  const poolRows = [
    ["主写库", sqlite.primary_write_pool || {}],
    ["读取池", sqlite.read_pool || {}],
    ["Debug 写库", sqlite.debug_write_pool || {}]
  ];
  const rows = poolRows.map(([label, pool]) => `
    <tr>
      <td><strong>${escapeHTML(label)}</strong></td>
      <td>${formatNumber(pool.maximum_open_connections)}</td>
      <td>${formatNumber(pool.open_connections)}</td>
      <td>${formatNumber(pool.in_use_connections)}</td>
      <td>${formatNumber(pool.wait_count)}</td>
      <td>${escapeHTML(formatDuration(pool.wait_duration_ms))}</td>
    </tr>
  `).join("");

  return renderTableCard(
    "连接池",
    "database/sql 当前连接使用与累计等待",
    ["池", "上限", "打开", "使用中", "等待次数", "等待耗时"],
    rows
  );
}

function renderQuotaCard(sqlite) {
  const operationRows = [
    ["Reserve", sqlite.quota_reserve || {}],
    ["Release", sqlite.quota_release || {}]
  ];
  const rows = operationRows.map(([label, operation]) => renderOperationRow(label, operation)).join("");
  const footer = `额度拒绝 ${formatNumber(sqlite.quota_limit_rejections)} 次 · 用户缺失 ${formatNumber(sqlite.quota_user_not_found)} 次`;
  return renderTableCard(
    "Quota 写路径",
    footer,
    ["操作", "次数", "错误", "锁竞争", "平均", "最大"],
    rows
  );
}

function renderUsageWriterCard(usageWriter) {
  const rows = [
    ["接受记录", usageWriter.accepted_records],
    ["成功写入", usageWriter.write_successes],
    ["失败记录", usageWriter.write_failures],
    ["丢弃记录", usageWriter.dropped_records],
    ["写入批次", usageWriter.write_batches],
    ["失败批次", usageWriter.failed_batches],
    ["批处理记录", usageWriter.batched_records],
    ["当前在途", usageWriter.in_flight_records]
  ].map(([label, value]) => renderKeyValueRow(label, formatNumber(value))).join("");

  return renderKeyValueCard(
    "Usage 异步队列",
    `最近批次 ${formatNumber(usageWriter.last_batch_size)} 条`,
    rows,
    [
      ["平均排队", formatDuration(usageWriter.average_queue_delay_ms)],
      ["最大排队", formatDuration(usageWriter.maximum_queue_delay_ms)],
      ["平均写入", formatDuration(usageWriter.average_write_duration_ms)],
      ["最大写入", formatDuration(usageWriter.maximum_write_duration_ms)]
    ]
  );
}

function renderUsagePersistenceCard(usageWrite) {
  const operation = usageWrite.operation || {};
  const rows = [
    ["事务尝试", operation.attempts],
    ["事务错误", operation.errors],
    ["锁竞争错误", operation.busy_or_locked_errors],
    ["尝试记录", usageWrite.records_attempted],
    ["成功记录", usageWrite.records_succeeded],
    ["失败记录", usageWrite.records_failed]
  ].map(([label, value]) => renderKeyValueRow(label, formatNumber(value))).join("");

  return renderKeyValueCard(
    "Usage 数据库写入",
    "主库批量事务与 debug 批量事务的总耗时",
    rows,
    [
      ["平均事务", formatDuration(operation.average_duration_ms)],
      ["最后事务", formatDuration(operation.last_duration_ms)],
      ["最大事务", formatDuration(operation.maximum_duration_ms)]
    ]
  );
}

function renderCheckpointCard(sqlite) {
  const checkpointRows = [
    ["主库", sqlite.primary_wal_checkpoint || {}],
    ["Debug 库", sqlite.debug_wal_checkpoint || {}]
  ];
  const rows = checkpointRows.map(([label, checkpoint]) => {
    const operation = checkpoint.operation || {};
    return `
      <tr>
        <td><strong>${escapeHTML(label)}</strong></td>
        <td>${formatNumber(operation.attempts)}</td>
        <td>${formatNumber(checkpoint.last_busy_frames)}</td>
        <td>${formatNumber(checkpoint.last_log_frames)}</td>
        <td>${formatNumber(checkpoint.last_checkpointed_frames)}</td>
        <td>${escapeHTML(formatDuration(operation.last_duration_ms))}</td>
      </tr>
    `;
  }).join("");

  return renderTableCard(
    "WAL Checkpoint",
    "PASSIVE 模式最近一次 frame 结果",
    ["数据库", "次数", "Busy", "WAL Frames", "已处理", "耗时"],
    rows
  );
}

function renderMaintenanceCard(operation) {
  const rows = [
    ["执行次数", formatNumber(operation.attempts)],
    ["执行错误", formatNumber(operation.errors)],
    ["锁竞争错误", formatNumber(operation.busy_or_locked_errors)],
    ["平均耗时", formatDuration(operation.average_duration_ms)],
    ["最后耗时", formatDuration(operation.last_duration_ms)],
    ["最大耗时", formatDuration(operation.maximum_duration_ms)]
  ].map(([label, value]) => renderKeyValueRow(label, value)).join("");

  return renderKeyValueCard(
    "Usage 维护任务",
    "聚合、保留期清理与 WAL checkpoint",
    rows,
    []
  );
}

function renderOperationRow(label, operation) {
  return `
    <tr>
      <td><strong>${escapeHTML(label)}</strong></td>
      <td>${formatNumber(operation.attempts)}</td>
      <td>${formatNumber(operation.errors)}</td>
      <td>${formatNumber(operation.busy_or_locked_errors)}</td>
      <td>${escapeHTML(formatDuration(operation.average_duration_ms))}</td>
      <td>${escapeHTML(formatDuration(operation.maximum_duration_ms))}</td>
    </tr>
  `;
}

function renderTableCard(title, description, headers, rows) {
  return `
    <article class="content-card operations-card">
      <header class="card-header"><div><h2>${escapeHTML(title)}</h2><p>${escapeHTML(description)}</p></div></header>
      <div class="data-table-wrap">
        <table class="data-table operations-table">
          <thead><tr>${headers.map((header) => `<th>${escapeHTML(header)}</th>`).join("")}</tr></thead>
          <tbody>${rows}</tbody>
        </table>
      </div>
    </article>
  `;
}

function renderKeyValueCard(title, description, rows, highlights) {
  const highlightHTML = highlights.length > 0 ? `
    <div class="operations-highlights">
      ${highlights.map(([label, value]) => `
        <span><small>${escapeHTML(label)}</small><strong>${escapeHTML(value)}</strong></span>
      `).join("")}
    </div>
  ` : "";
  return `
    <article class="content-card operations-card">
      <header class="card-header"><div><h2>${escapeHTML(title)}</h2><p>${escapeHTML(description)}</p></div></header>
      ${highlightHTML}
      <div class="operations-key-values">${rows}</div>
    </article>
  `;
}

function renderKeyValueRow(label, value) {
  return `<div><span>${escapeHTML(label)}</span><strong>${escapeHTML(value)}</strong></div>`;
}

function renderOperationsMetricsLoading() {
  return `
    ${renderPageHeading("运行指标", "正在读取 SQLite 与 usage 写入器状态。")}
    <section class="metric-grid">
      ${Array.from({ length: 4 }, () => '<div class="skeleton" style="height:142px;border-radius:16px"></div>').join("")}
    </section>
    <section class="operations-grid">
      ${Array.from({ length: 6 }, () => '<div class="skeleton" style="height:300px;border-radius:16px"></div>').join("")}
    </section>
  `;
}

function formatDuration(value) {
  const milliseconds = numericValue(value);
  if (milliseconds <= 0) {
    return "0 ms";
  }
  const maximumFractionDigits = milliseconds < 1 ? 3 : milliseconds < 100 ? 2 : 1;
  return `${formatNumber(milliseconds, { maximumFractionDigits })} ms`;
}

function numericValue(value) {
  const parsedValue = Number(value);
  return Number.isFinite(parsedValue) ? parsedValue : 0;
}
