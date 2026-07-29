import { renderPageHeading } from "../components/loading.js";
import { renderMetricCard } from "../components/metric-card.js";
import { escapeHTML, formatDateTime, formatNumber } from "../utils.js";

export function renderOperationsMetricsPage(state) {
  if (state.pageLoading && !state.data.operationsMetrics) {
    return renderOperationsMetricsLoading();
  }

  const metrics = state.data.operationsMetrics || {};
  const runtimeMetrics = metrics.runtime || {};
  const runtimeMemory = runtimeMetrics.memory || {};
  const sqlite = metrics.sqlite || {};
  const usageWriter = metrics.usage_writer || {};
  const ipLimiter = metrics.ip_limiter || {};
  const userLimiter = metrics.user_limiter || {};
  const authProtector = metrics.auth_protector || {};
  const primaryPool = sqlite.primary_write_pool || {};
  const queueLength = numericValue(usageWriter.queue_length);
  const queueCapacity = numericValue(usageWriter.queue_capacity);
  const queueUtilization = queueCapacity > 0 ? queueLength / queueCapacity : 0;
  const busyOrLockedErrors = numericValue(sqlite.busy_or_locked_errors);
  const droppedRecords = numericValue(usageWriter.dropped_records);
  const pressureSignals = collectPressureSignals({
    busyOrLockedErrors,
    droppedRecords,
    queueUtilization,
    ipLimiter,
    userLimiter,
    authProtector
  });

  return `
    ${renderPageHeading("运行指标", "观察 Go 进程、内存与 GC、SQLite、用量写入、限流和认证保护状态。")} 
    ${renderPressureNotice(pressureSignals)}

    <section class="metric-grid" aria-label="运行状态摘要">
      ${renderMetricCard(
        "Goroutine",
        formatNumber(runtimeMetrics.goroutines),
        `已运行 ${formatUptime(runtimeMetrics.uptime_ms)}`,
        "activity",
        "#e8f8ef",
        "#238a54",
        true,
        "pulse",
        numericValue(runtimeMetrics.goroutines)
      )}
      ${renderMetricCard(
        "堆内存",
        formatBytes(runtimeMemory.heap_allocated_bytes),
        `${formatNumber(runtimeMemory.heap_object_count)} 个存活对象`,
        "layers",
        "#eeeaff",
        "#7667f4",
        true,
        "nodes",
        numericValue(runtimeMemory.garbage_collection_count)
      )}
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
    </section>

    <section class="operations-grid">
      ${renderRuntimeCard(runtimeMetrics)}
      ${renderRuntimeMemoryCard(runtimeMemory)}
      ${renderConnectionPoolCard(sqlite)}
      ${renderQuotaCard(sqlite)}
      ${renderUsageWriterCard(usageWriter)}
      ${renderUsagePersistenceCard(sqlite.usage_write || {})}
      ${renderCheckpointCard(sqlite)}
      ${renderMaintenanceCard(sqlite.usage_maintenance || {})}
      ${renderLimiterCard(ipLimiter, userLimiter)}
      ${renderAuthEndpointCard(authProtector)}
      ${renderLoginProtectionCard(authProtector)}
    </section>

    <p class="operations-captured-at">快照时间：${escapeHTML(formatDateTime(metrics.captured_at || sqlite.captured_at))}</p>
  `;
}

function collectPressureSignals({
  busyOrLockedErrors,
  droppedRecords,
  queueUtilization,
  ipLimiter,
  userLimiter,
  authProtector
}) {
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

  const limiterFallbackRejections = numericValue(ipLimiter.fallback_rejections)
    + numericValue(userLimiter.fallback_rejections);
  if (limiterFallbackRejections > 0) {
    signals.push(`${formatNumber(limiterFallbackRejections)} 次请求限流降级拒绝`);
  }

  const authenticationRejections = calculateAuthenticationRejections(authProtector);
  if (authenticationRejections > 0) {
    signals.push(`${formatNumber(authenticationRejections)} 次认证保护拒绝`);
  }
  return signals;
}

function renderPressureNotice(pressureSignals) {
  if (pressureSignals.length === 0) {
    return `
      <div class="operations-notice is-healthy">
        <strong>当前未发现明显积压</strong>
        <span>继续关注 goroutine、堆内存、GC 暂停、连接等待、限流降级和 WAL checkpoint 趋势。</span>
      </div>
    `;
  }

  return `
    <div class="operations-notice is-warning">
      <strong>观察到运行压力信号</strong>
      <span>${escapeHTML(pressureSignals.join("；"))}。请结合连续快照的增长速度定位资源或容量瓶颈。</span>
    </div>
  `;
}

function renderRuntimeCard(runtimeMetrics) {
  const rows = [
    ["Go 版本", runtimeMetrics.go_version || "未知"],
    ["操作系统 / 架构", `${runtimeMetrics.go_os || "未知"} / ${runtimeMetrics.go_arch || "未知"}`],
    ["运行时间", formatUptime(runtimeMetrics.uptime_ms)],
    ["Goroutine", formatNumber(runtimeMetrics.goroutines)],
    ["逻辑 CPU", formatNumber(runtimeMetrics.cpu_count)],
    ["GOMAXPROCS", formatNumber(runtimeMetrics.gomaxprocs)],
    ["CGO 调用", formatNumber(runtimeMetrics.cgo_calls)]
  ].map(([label, value]) => renderKeyValueRow(label, value)).join("");

  return renderKeyValueCard(
    "Go 运行时",
    "进程环境、调度容量与累计 CGO 调用",
    rows,
    []
  );
}

function renderRuntimeMemoryCard(memory) {
  const rows = [
    ["当前分配", formatBytes(memory.allocated_bytes)],
    ["累计分配", formatBytes(memory.total_allocated_bytes)],
    ["系统保留", formatBytes(memory.system_bytes)],
    ["堆已分配", formatBytes(memory.heap_allocated_bytes)],
    ["堆系统内存", formatBytes(memory.heap_system_bytes)],
    ["堆使用中", formatBytes(memory.heap_in_use_bytes)],
    ["堆空闲", formatBytes(memory.heap_idle_bytes)],
    ["堆已归还", formatBytes(memory.heap_released_bytes)],
    ["栈使用中", formatBytes(memory.stack_in_use_bytes)],
    ["栈系统内存", formatBytes(memory.stack_system_bytes)],
    ["运行时元数据", formatBytes(memory.metadata_in_use_bytes)],
    ["存活堆对象", formatNumber(memory.heap_object_count)],
    ["Malloc / Free", `${formatNumber(memory.malloc_count)} / ${formatNumber(memory.free_count)}`],
    ["GC / 强制 GC", `${formatNumber(memory.garbage_collection_count)} / ${formatNumber(memory.forced_garbage_collection_count)}`],
    ["最近 GC 时间", formatDateTime(memory.last_garbage_collection_at)]
  ].map(([label, value]) => renderKeyValueRow(label, value)).join("");

  return renderKeyValueCard(
    "内存与 GC",
    "Go 堆、栈、分配器与垃圾回收快照",
    rows,
    [
      ["下次 GC 目标", formatBytes(memory.next_gc_bytes)],
      ["累计 GC 暂停", formatDuration(memory.garbage_collection_pause_total_ms)],
      ["最近 GC 暂停", formatDuration(memory.last_garbage_collection_pause_ms)],
      ["GC CPU 占比", formatFractionPercent(memory.garbage_collection_cpu_fraction)]
    ]
  );
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
      <td>${formatNumber(pool.idle_connections)}</td>
      <td>${formatNumber(pool.wait_count)}</td>
      <td>${escapeHTML(formatDuration(pool.wait_duration_ms))}</td>
      <td>${formatNumber(pool.max_idle_closed)}</td>
      <td>${formatNumber(pool.max_idle_time_closed)}</td>
      <td>${formatNumber(pool.max_lifetime_closed)}</td>
    </tr>
  `).join("");

  return renderTableCard(
    "连接池",
    "database/sql 当前连接、累计等待与连接回收原因",
    ["池", "上限", "打开", "使用中", "空闲", "等待次数", "等待耗时", "超空闲上限关闭", "空闲超时关闭", "生命周期关闭"],
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

function renderLimiterCard(ipLimiter, userLimiter) {
  const limiterRows = [
    ["来源 IP", ipLimiter],
    ["已认证用户", userLimiter]
  ];
  const rows = limiterRows.map(([label, limiter]) => `
    <tr>
      <td><strong>${escapeHTML(label)}</strong></td>
      <td>${escapeHTML(formatCapacity(limiter.current_entries, limiter.maximum_entries))}</td>
      <td>${formatNumber(limiter.fallback_bucket_count)}</td>
      <td>${formatNumber(limiter.dedicated_admissions)}</td>
      <td>${formatNumber(limiter.expired_entries_removed)}</td>
      <td>${formatNumber(limiter.fallback_requests)}</td>
      <td>${formatNumber(limiter.fallback_rejections)}</td>
    </tr>
  `).join("");

  return renderTableCard(
    "请求限流器",
    "有界身份注册表容量、回收与共享降级流量",
    ["范围", "当前 / 上限", "降级桶", "独立接纳", "过期回收", "降级请求", "降级拒绝"],
    rows
  );
}

function renderAuthEndpointCard(authProtector) {
  const endpointRows = [
    ["登录", authProtector.login || {}],
    ["注册", authProtector.register || {}],
    ["注册 Challenge", authProtector.registration_challenge || {}]
  ];
  const rows = endpointRows.map(([label, endpoint]) => `
    <tr>
      <td><strong>${escapeHTML(label)}</strong></td>
      <td>${escapeHTML(formatCapacity(endpoint.current_entries, endpoint.maximum_entries))}</td>
      <td>${formatNumber(endpoint.fallback_bucket_count)}</td>
      <td>${formatNumber(endpoint.dedicated_admissions)}</td>
      <td>${formatNumber(endpoint.expired_entries_removed)}</td>
      <td>${formatNumber(endpoint.fallback_requests)}</td>
      <td>${formatNumber(endpoint.fallback_rejections)}</td>
    </tr>
  `).join("");

  return renderTableCard(
    "公开认证入口",
    "登录、注册和 challenge 的 IP 防护容量",
    ["入口", "当前 / 上限", "降级桶", "独立接纳", "过期回收", "降级请求", "降级拒绝"],
    rows
  );
}

function renderLoginProtectionCard(authProtector) {
  const loginFailures = authProtector.login_failures || {};
  const usernameFailures = authProtector.username_failures || {};
  const passwordWork = authProtector.password_work || {};
  const rows = [
    ["用户名 + IP 状态", formatCapacity(loginFailures.current_entries, loginFailures.maximum_entries)],
    ["用户名 + IP 接纳", formatNumber(loginFailures.admissions)],
    ["用户名 + IP 过期回收", formatNumber(loginFailures.expired_entries_removed)],
    ["用户名 + IP 容量拒绝", formatNumber(loginFailures.capacity_rejections)],
    ["用户名 + IP 降级尝试", formatNumber(loginFailures.fallback_attempts)],
    ["用户名 + IP 降级拒绝", formatNumber(loginFailures.fallback_rejections)],
    ["用户名状态", formatCapacity(usernameFailures.current_entries, usernameFailures.maximum_entries)],
    ["用户名降级桶", formatNumber(usernameFailures.fallback_bucket_count)],
    ["用户名接纳", formatNumber(usernameFailures.admissions)],
    ["用户名过期回收", formatNumber(usernameFailures.expired_entries_removed)],
    ["用户名容量拒绝", formatNumber(usernameFailures.capacity_rejections)],
    ["用户名降级尝试", formatNumber(usernameFailures.fallback_attempts)],
    ["用户名降级拒绝", formatNumber(usernameFailures.fallback_rejections)],
    ["bcrypt 当前 / 上限", formatCapacity(passwordWork.current_work, passwordWork.capacity)],
    ["bcrypt 接纳", formatNumber(passwordWork.admissions)],
    ["bcrypt 拒绝", formatNumber(passwordWork.rejections)]
  ].map(([label, value]) => renderKeyValueRow(label, value)).join("");

  return renderKeyValueCard(
    "登录与密码工作保护",
    "失败状态容量和进程级 bcrypt 并发准入",
    rows,
    []
  );
}

function calculateAuthenticationRejections(authProtector) {
  const endpointRejections = [
    authProtector.login,
    authProtector.register,
    authProtector.registration_challenge
  ].reduce((total, endpoint) => total + numericValue(endpoint?.fallback_rejections), 0);
  const failureRejections = [
    authProtector.login_failures,
    authProtector.username_failures
  ].reduce((total, failureMetrics) => total
    + numericValue(failureMetrics?.capacity_rejections)
    + numericValue(failureMetrics?.fallback_rejections), 0);
  return endpointRejections
    + failureRejections
    + numericValue(authProtector.password_work?.rejections);
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
  const widthClass = headers.length >= 7 ? " is-wide" : "";
  return `
    <article class="content-card operations-card${widthClass}">
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
    ${renderPageHeading("运行指标", "正在读取 Go 运行时、SQLite、用量与防护状态。")} 
    <section class="metric-grid">
      ${Array.from({ length: 4 }, () => '<div class="skeleton" style="height:142px;border-radius:16px"></div>').join("")}
    </section>
    <section class="operations-grid">
      ${Array.from({ length: 11 }, () => '<div class="skeleton" style="height:300px;border-radius:16px"></div>').join("")}
    </section>
  `;
}

function formatBytes(value) {
  const bytes = Math.max(0, numericValue(value));
  if (bytes === 0) {
    return "0 B";
  }

  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const unitIndex = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1
  );
  const scaledValue = bytes / (1024 ** unitIndex);
  return `${formatNumber(scaledValue, { maximumFractionDigits: scaledValue < 10 ? 2 : 1 })} ${units[unitIndex]}`;
}

function formatUptime(value) {
  const totalSeconds = Math.floor(Math.max(0, numericValue(value)) / 1000);
  const days = Math.floor(totalSeconds / 86400);
  const hours = Math.floor((totalSeconds % 86400) / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;

  if (days > 0) {
    return `${formatNumber(days)} 天 ${formatNumber(hours)} 小时`;
  }
  if (hours > 0) {
    return `${formatNumber(hours)} 小时 ${formatNumber(minutes)} 分`;
  }
  if (minutes > 0) {
    return `${formatNumber(minutes)} 分 ${formatNumber(seconds)} 秒`;
  }
  return `${formatNumber(seconds)} 秒`;
}

function formatFractionPercent(value) {
  return `${formatNumber(numericValue(value) * 100, { maximumFractionDigits: 2 })}%`;
}

function formatCapacity(currentValue, maximumValue) {
  return `${formatNumber(currentValue)} / ${formatNumber(maximumValue)}`;
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
