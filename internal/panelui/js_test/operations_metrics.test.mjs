import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

globalThis.window = { location: { search: "", hash: "" } };

async function importBrowserModule(relativeModulePath) {
  const moduleURL = new URL(relativeModulePath, import.meta.url);
  return import(await buildBrowserModuleDataURL(moduleURL));
}

async function buildBrowserModuleDataURL(moduleURL) {
  let moduleSource = await readFile(moduleURL, "utf8");
  const relativeImportPattern = /from\s+["'](\.[^"']+)["']/g;
  const relativeSpecifiers = Array.from(
    moduleSource.matchAll(relativeImportPattern),
    (match) => match[1]
  );
  for (const relativeSpecifier of new Set(relativeSpecifiers)) {
    const dependencyDataURL = await buildBrowserModuleDataURL(new URL(relativeSpecifier, moduleURL));
    moduleSource = moduleSource
      .replaceAll(`"${relativeSpecifier}"`, `"${dependencyDataURL}"`)
      .replaceAll(`'${relativeSpecifier}'`, `'${dependencyDataURL}'`);
  }
  return `data:text/javascript;base64,${Buffer.from(moduleSource).toString("base64")}`;
}

const { renderOperationsMetricsPage } = await importBrowserModule(
  "../static/js/pages/operations-metrics.js"
);

function createRegistryMetrics(overrides = {}) {
  return {
    current_entries: 2,
    maximum_entries: 100,
    fallback_bucket_count: 4,
    dedicated_admissions: 20,
    expired_entries_removed: 3,
    fallback_requests: 1,
    fallback_rejections: 0,
    ...overrides
  };
}

test("operations page renders process, database, limiter, and auth metrics", () => {
  const markup = renderOperationsMetricsPage({
    pageLoading: false,
    data: {
      operationsMetrics: {
        captured_at: "2026-07-29T10:00:00Z",
        runtime: {
          uptime_ms: 93784000,
          go_version: "go1.25.12",
          go_os: "linux",
          go_arch: "amd64",
          cpu_count: 8,
          gomaxprocs: 6,
          goroutines: 42,
          cgo_calls: 7,
          memory: {
            allocated_bytes: 2048,
            total_allocated_bytes: 4096,
            system_bytes: 8192,
            malloc_count: 120,
            free_count: 80,
            heap_allocated_bytes: 1024,
            heap_system_bytes: 8192,
            heap_idle_bytes: 2048,
            heap_in_use_bytes: 4096,
            heap_released_bytes: 512,
            heap_object_count: 40,
            stack_in_use_bytes: 1024,
            stack_system_bytes: 2048,
            metadata_in_use_bytes: 256,
            next_gc_bytes: 16384,
            last_garbage_collection_at: "2026-07-29T09:59:59Z",
            garbage_collection_count: 9,
            forced_garbage_collection_count: 1,
            garbage_collection_pause_total_ms: 4.5,
            last_garbage_collection_pause_ms: 0.25,
            garbage_collection_cpu_fraction: 0.125
          }
        },
        sqlite: {
          primary_write_pool: {
            maximum_open_connections: 1,
            open_connections: 1,
            in_use_connections: 0,
            idle_connections: 1,
            wait_count: 2,
            wait_duration_ms: 3,
            max_idle_closed: 11,
            max_idle_time_closed: 12,
            max_lifetime_closed: 13
          },
          read_pool: {},
          debug_write_pool: {},
          busy_or_locked_errors: 0
        },
        usage_writer: { queue_length: 1, queue_capacity: 16 },
        ip_limiter: createRegistryMetrics(),
        user_limiter: createRegistryMetrics(),
        auth_protector: {
          login: createRegistryMetrics(),
          register: createRegistryMetrics(),
          registration_challenge: createRegistryMetrics(),
          login_failures: {
            current_entries: 1,
            maximum_entries: 50,
            admissions: 5,
            expired_entries_removed: 1,
            capacity_rejections: 0,
            fallback_attempts: 0,
            fallback_rejections: 0
          },
          username_failures: {
            current_entries: 2,
            maximum_entries: 60,
            fallback_bucket_count: 8,
            admissions: 6,
            expired_entries_removed: 2,
            capacity_rejections: 0,
            fallback_attempts: 1,
            fallback_rejections: 0
          },
          password_work: {
            current_work: 1,
            capacity: 4,
            admissions: 10,
            rejections: 0
          }
        }
      }
    }
  });

  assert.match(markup, /Go 运行时/);
  assert.match(markup, /go1\.25\.12/);
  assert.match(markup, /linux \/ amd64/);
  assert.match(markup, /内存与 GC/);
  assert.match(markup, /GC CPU 占比/);
  assert.match(markup, /最近 GC 时间/);
  assert.match(markup, /12\.5%/);
  assert.match(markup, /超空闲上限关闭/);
  assert.match(markup, /空闲超时关闭/);
  assert.match(markup, /生命周期关闭/);
  assert.match(markup, /请求限流器/);
  assert.match(markup, /已认证用户/);
  assert.match(markup, /公开认证入口/);
  assert.match(markup, /注册 Challenge/);
  assert.match(markup, /登录与密码工作保护/);
  assert.match(markup, /bcrypt 当前 \/ 上限/);
});

test("operations page includes limiter and authentication rejections in pressure notice", () => {
  const markup = renderOperationsMetricsPage({
    pageLoading: false,
    data: {
      operationsMetrics: {
        runtime: { memory: {} },
        sqlite: {},
        usage_writer: {},
        ip_limiter: createRegistryMetrics({ fallback_rejections: 2 }),
        user_limiter: createRegistryMetrics({ fallback_rejections: 3 }),
        auth_protector: {
          login: createRegistryMetrics({ fallback_rejections: 4 }),
          password_work: { rejections: 5 }
        }
      }
    }
  });

  assert.match(markup, /观察到运行压力信号/);
  assert.match(markup, /5 次请求限流降级拒绝/);
  assert.match(markup, /9 次认证保护拒绝/);
});
