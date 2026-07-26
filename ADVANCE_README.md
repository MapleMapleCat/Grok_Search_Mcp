# grok-search-mcp 高级配置

[返回中文 README](./README_CN.md)

本文档集中说明通常无需在首次部署时修改的高级启动参数与运维配置。默认值可在
[`advanced.env`](./advanced.env) 中查看；使用本地二进制时应先加载
`advanced.env`，再加载用户自己的 `.env`，以便 `.env` 中的显式设置覆盖高级
默认值。

## Usage 数据保留与 SQLite 维护

用量数据会按逐级降低的时间分辨率保留，避免长期运行后仍保存全部请求明细：

| 环境变量 | 默认值 | 用途 |
|---|---:|---|
| `GROK_USAGE_RAW_RETENTION_DAYS` | `7` | 保留逐请求明细和 debug 数据，之后压缩为小时级历史。 |
| `GROK_USAGE_HOURLY_RETENTION_DAYS` | `90` | 保留小时级历史，之后压缩为日级历史。 |
| `GROK_USAGE_DAILY_RETENTION_DAYS` | `730` | 删除超过此期限的日级历史。 |
| `GROK_USAGE_MAINTENANCE_INTERVAL` | `1h` | 执行聚合、清理以及主库和 debug 库的 WAL checkpoint。 |

小时级保留期限必须大于原始明细期限，日级保留期限必须大于小时级期限。
历史总量和流量图会合并原始、小时和日级数据；最近调用明细与单条 debug
详情只在对应原始记录仍处于保留期内时可用。

主数据库和 `<GROK_DB_PATH>.debug.sqlite` 都使用 WAL 模式。在线备份应对两个
数据库都使用 SQLite 在线备份机制，不能在服务运行时只复制主 `.db` 文件。
如果使用文件系统复制，应先停止服务，并同时复制两个数据库及其 WAL/SHM
旁路文件。定时维护会 checkpoint WAL，但不会自动执行 `VACUUM`；只有在需要
回收数据库文件本身空间时，才应由运维人员低频显式执行 `VACUUM` 或
`VACUUM INTO`。

SQLite 主库有意保持单写连接，避免通过增加连接数制造更多写锁竞争。连接设置
包含 5 秒 `busy_timeout`，usage 后台写入器会将最多 32 条记录或 10ms 内到达的
记录合并为一个事务；定时维护使用非阻塞读者的 `PASSIVE` checkpoint。生产环境
必须把数据库放在本地 SSD 上，不应放在 NFS、SMB 或高延迟网络块存储上。

运行指标默认关闭。管理员需要先在 **服务设置** 中启用
**数据库运行指标**；关闭时，以下接口返回 HTTP `404`。

管理员可以查询实时运行指标：

```bash
curl -sS "http://127.0.0.1:8080/panel/v1/admin/operations/metrics" \
  -H "Authorization: Bearer ${login_token}" | jq
```

该接口仅允许管理员访问，包含：主写库、读库和 debug 库的连接池状态与等待时间；
quota reserve/release 延迟和错误；SQLite busy/locked 次数；usage 批次、队列深度、
最老排队记录年龄、写入/排队延迟和丢弃量；维护及主库/debug 库 WAL checkpoint
延迟与 frame 计数；以及有界来源 IP 注册表的当前/最大条目数、独立条目接纳、
过期清理、降级请求和降级拒绝计数。同一接口还会按公开认证 endpoint 汇总面板
认证保护器的容量、接纳、过期清理、降级请求/拒绝和登录失败容量拒绝计数，
不会暴露 IP 地址或用户名。建议至少为以下情况配置告警：

- `primary_write_pool.wait_count` 或 `wait_duration_ms` 持续快速增长；
- `busy_or_locked_errors` 非零并持续增长；
- usage 队列长期接近容量、`oldest_queued_age_ms` 增长或出现丢弃；
- quota reserve/release 最大或平均延迟持续升高；
- checkpoint 持续出现 busy frame 或耗时升高；
- 来源 IP 注册表持续饱和或降级拒绝数持续增长；
- 面板认证 endpoint 持续出现降级流量、降级拒绝或登录失败容量拒绝。

如果在本地 SSD 和批量写入下仍长期出现上述压力，说明工作负载已超过内嵌
SQLite 单写者模型的目标范围。高写入 QPS 部署应迁移到 PostgreSQL/MySQL，或将
quota 计数迁移到具备原子操作的外部计数器，而不是继续增加 SQLite 写连接数。

## 启动环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `GROK_JWT_SECRET` | 无 | 面板 HS256 签名密钥，必填且至少 32 字节，始终通过环境变量提供。 |
| `CPA_API_KEY` | 无 | 新数据库必填；后续启动可以由 SQLite 中的服务设置提供。 |
| `CPA_BASE_URL` | `http://127.0.0.1:8317` | CPA 根地址。 |
| `GROK_UPSTREAM_PROTOCOL` | `responses` | 搜索协议：`responses`、`chat_completions` 或 `anthropic_messages`。 |
| `GROK_MODEL` | `grok-4.5` | 默认 Grok 模型。 |
| `GROK_HTTP_TIMEOUT` | `120` | 上游连接、TLS 握手和响应头各阶段的超时秒数，不限制已建立 SSE 响应体的持续时间；总搜索生命周期由调用方取消控制。 |
| `GROK_HTTP_ADDR` | `:8080` | HTTP 监听地址，修改后需要重启。 |
| `GROK_DB_PATH` | `./grok-search-mcp.db` | SQLite 路径，修改后需要重启。 |
| `GROK_BOOTSTRAP_CREDENTIALS_PATH` | `<GROK_DB_PATH>.bootstrap-admin` | bootstrap 管理员 JSON 凭据文件的仅启动时路径；已有路径必须是精确 `0600` 的普通非符号链接文件。 |
| `GROK_CLIENT_IP_MODE` | `direct` | 仅启动时生效的客户端身份模式：`direct` 使用 `RemoteAddr` 并忽略转发 Header；`trusted_proxy` 先认证直接对端再接受转发 Header。 |
| `GROK_TRUSTED_PROXY_CIDRS` | 空 | 可信直接代理对端的 IPv4/IPv6 CIDR，逗号分隔。仅在 `trusted_proxy` 模式下必填、解析并校验；`direct` 模式会忽略该变量。 |
| `GROK_INITIAL_REGISTRATION_MODE` | `disabled` | 初始注册策略：`disabled`、`invite` 或 `free`。仅在尚无持久化服务设置行时使用。 |
| `GROK_MAX_API_KEYS_PER_USER` | `20` | 仅启动时生效的单用户 API Key 行数上限，范围 1-1,000；禁用仍计数，删除释放容量。 |
| `GROK_AUTH_PASSWORD_MAX_CONCURRENT` | `4` | 登录、注册和密码修改共享的进程级 bcrypt 并发上限，范围 1-64。 |
| `GROK_AUTH_KEY_MISS_MAX_CONCURRENT` | `32` | 不同无效 API Key 并发解析的 SQLite 上限；相同 Key 的 miss 会合并，范围 1-1,024。 |
| `GROK_USAGE_RAW_RETENTION_DAYS` | `7` | 原始用量和 debug 明细保留期限，之后压缩为小时级数据。 |
| `GROK_USAGE_HOURLY_RETENTION_DAYS` | `90` | 小时级用量保留期限，之后压缩为日级数据。 |
| `GROK_USAGE_DAILY_RETENTION_DAYS` | `730` | 日级聚合超过此期限后删除。 |
| `GROK_USAGE_MAINTENANCE_INTERVAL` | `1h` | 聚合、清理和 WAL checkpoint 的执行间隔。 |
| `GROK_SEARCH_MCP_IP_RPM` | `300` | 在 MCP API Key 鉴权前，对每个请求按 `GROK_CLIENT_IP_MODE` 选出的来源 IP 应用 RPM。 |
| `GROK_SEARCH_MCP_IP_MAX_ENTRIES_PER_SHARD` | `2048` | 64 个注册表分片各自最多保留的独立来源 IP 令牌桶数；默认进程总上限为 131,072 个条目，可配置范围为 1-65,536，修改后需要重启。 |
| `GROK_SEARCH_MCP_IP_FALLBACK_BUCKETS_PER_SHARD` | `16` | 分片满载且清理过期条目后仍无容量时，新 IP 使用的固定共享降级桶数；可配置范围为 1-1,024，修改后需要重启。 |
| `GROK_SEARCH_MCP_GLOBAL_SEARCH_CONCURRENCY` | `16` | 进程级流式搜索同时在途上限的环境默认值；初始化后以面板持久化设置为准。 |
| `GROK_SEARCH_MCP_USER_SEARCH_CONCURRENCY` | `4` | 单用户上限的环境默认值，不得超过全局上限；初始化后以面板持久化设置为准。 |
| `GROK_AUTH_USER_RPM_MAX_ENTRIES` | `16,384` | 已鉴权用户 RPM 专用状态的启动时容量上限，范围 1-65,536；溢出身份使用固定共享降级桶。 |
| `GROK_AUTH_USER_RPM_FALLBACK_BUCKETS` | `64` | 已鉴权用户 RPM 共享降级桶数量，范围 1-1,024。 |
| `GROK_SEARCH_MCP_DEBUG` | `false` | `1`、`true` 或 `yes` 启用；可能在用量记录中捕获调试上下文。 |
| `GROK_PROXY_URL` | 空 | 显式上游 HTTP(S) 代理。 |
| `GROK_PROXY_ENABLED` | `false` | 显式代理开关；必须与 `GROK_PROXY_URL` 一起设置为 `true`，仅设置 URL 不会启用项目显式代理。 |
| `HTTP_PROXY`、`HTTPS_PROXY`、`NO_PROXY` | Go 默认行为 | 未启用显式代理时由标准 HTTP Transport 使用。 |

旧的 `GROK_MCP_IP_RPM`、`GROK_MCP_GLOBAL_SEARCH_CONCURRENCY`、
`GROK_MCP_USER_SEARCH_CONCURRENCY` 和 `GROK_MCP_DEBUG` 仍作为兼容别名接受。
当新旧名称同时配置时，对应的 `GROK_SEARCH_MCP_*` 变量优先生效。

任一搜索并发容量耗尽时，服务会立即返回 HTTP `503` 和 `Retry-After: 1`，不会继续排队并占用长连接 goroutine/socket。搜索响应通过 `X-Grok-Search-Queue-Time-Ms` 暴露 semaphore 获取耗时。

## 客户端 IP 信任模式

应用层 IP 防护始终要求有效客户端身份。以下入口注入并共用同一个仅启动时配置的解析器：

- `/mcp` API Key 鉴权前的 IP 令牌桶；
- 面板登录和注册接口的 IP 令牌桶；
- 面板“用户名 + IP”维度的登录失败锁定。

两种模式的行为如下：

| 模式 / 请求状态 | IP 防护行为 |
|---|---|
| `direct`（默认） | 每个请求都使用连接 `RemoteAddr` 中的规范化 IP，并完全忽略 `X-Real-IP` 与 `X-Forwarded-For`，包括格式错误或伪造值。缺失、格式错误或带 zone 的 `RemoteAddr` 返回 HTTP `400`。 |
| `trusted_proxy`，直接对端不在 `GROK_TRUSTED_PROXY_CIDRS` | 不接受任何转发身份，直接返回 HTTP `403`。 |
| `trusted_proxy`，可信对端未提供转发 Header | 返回 HTTP `400`，不存在无 Header 绕过。 |
| `trusted_proxy`，可信对端提供了格式错误、重复、超长、跳数过多或冲突的转发 Header | 返回 HTTP `400`。 |
| `trusted_proxy`，可信对端提供有效转发 Header | 优先使用 `X-Real-IP`，否则使用 `X-Forwarded-For` 中第一个 IP；两者同时存在时，规范化客户端地址必须一致。 |

`GROK_TRUSTED_PROXY_CIDRS` 最多接受 256 个逗号分隔的规范 IPv4/IPv6
前缀；该限制仅在 trusted-proxy 模式解析配置时生效，且该模式必须提供列表。
direct 模式不会解析或校验该变量，因为它会忽略所有代理身份配置。信任只针对
TCP 直接对端；可信代理仍负责删除客户端提供的同名 Header，并根据自身连接元数据
重新生成转发 Header。

`/mcp` 来源 IP 注册表具有硬容量上限。现有 IP 会持续使用自己的独立令牌桶，
直到正常空闲 TTL 到期；系统不会为了接纳新身份而淘汰活跃条目。分片已满时，
限流器会先同步删除过期条目；如果仍然满载，则通过进程随机哈希把新 IP 映射到
固定的共享降级桶，不再创建对应的 map 条目。因此多个降级身份可能共享限流状态，
并在饱和期间共同收到 `429`。启用运维指标后，管理员指标接口会暴露注册表容量、
当前条目数、独立条目接纳/过期数和降级请求/拒绝数，但不会暴露 IP 地址。

公开面板认证保护器同样具有固定的进程内硬容量：登录 endpoint 最多保留 4,096
个独立 IP 桶，注册 endpoint 为 2,048 个，注册 challenge endpoint 为 2,048 个，
规范化“用户名 + IP”登录失败状态为 8,192 个。三个 endpoint 拥有相互独立的容量
域和各自 16 个固定共享降级桶。endpoint 满载时会先同步回收过期条目；若仍满载，
新 IP 共享降级限流状态且不会新增 map 条目。系统不会为了接纳新身份而淘汰仍然
有效的独立令牌桶。

登录失败状态不使用共享降级桶，避免哈希碰撞导致无关用户被共同锁定。清理过期
条目后仍满载时，新的“用户名 + IP”会在用户查询和 bcrypt 前收到通用 `429`；
已有失败计数、活跃锁定和在途尝试会继续保留。这些预算是固定安全上限，不属于
环境变量或面板设置。管理员运行指标只暴露聚合容量和饱和计数。所有面板认证
保护状态及累计计数均为进程内状态，服务重启后会重置。

> [!IMPORTANT]
> 只有确认 `grok-search-mcp` 实际看到的代理 CIDR 后才启用 `trusted_proxy`；它可能是容器网桥或负载均衡子网，而不是代理公网地址。CIDR 配错会安全失败并返回 `403`。代理层仍应对 `/mcp`、`/panel/v1/auth/login` 和 `/panel/v1/auth/register` 配置限流。

反向代理必须覆盖 `X-Real-IP`，并根据连接来源重新生成 `X-Forwarded-For`。由于应用会选择 `X-Forwarded-For` 中第一个有效 IP，不应保留不可信客户端提供的原始转发链。

Nginx 转发示例：

```nginx
location / {
    proxy_pass http://grok-search-mcp:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $remote_addr;
}
```

同时配置类似以下启动变量：

```dotenv
GROK_CLIENT_IP_MODE=trusted_proxy
GROK_TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128
```

Compose 发布为 `0.0.0.0:8080:8080`，虚拟机、局域网客户端和容器网络客户端
均可通过宿主机访问服务。公网部署时，应使用防火墙和可信 HTTPS 反向代理限制
外部访问。

## 持久化与热更新

服务启动时，环境变量提供初始运行时默认值。`GROK_INITIAL_REGISTRATION_MODE` 只在 SQLite 尚无服务设置行时提供注册策略，安全默认值为 `disabled`。如果 SQLite 已保存服务设置，则完整持久化对象优先，包括注册模式；重启时修改初始值不会覆盖管理员选择。监听地址、数据库路径、JWT 密钥、客户端 IP 信任模式/CIDR、IP RPM/注册表容量、保留期限和维护周期仍只由环境变量控制。管理员可以在 **Server Settings** 中热更新：

- CPA 地址和 API Key
- 上游搜索协议
- 默认模型和超时
- 显式代理地址及开关
- 注册模式
- Debug 模式
- 进程级和单用户流式搜索并发上限
- 运行指标采集开关

设置更新会先写入持久化存储，再应用到当前运行进程。面板分别显示已保存设置版本和已确认运行版本。如果持久化成功但运行时应用失败，保存值仍然有效，面板会明确提示“已保存，尚未应用”而不是笼统的保存失败，并重新加载持久化值。版本不一致期间，上游健康状态返回未知，避免使用混合配置进行探测。服务重启成功后会加载持久化版本，并恢复两个版本一致。

监听地址、数据库路径、JWT 密钥、客户端 IP 模式/可信 CIDR、来源 IP RPM、
注册表容量和降级桶数量仍然是仅启动时生效的配置。

> [!WARNING]
> CPA API Key 会持久化到 SQLite。请将数据库视为敏感数据进行权限控制和备份；面板响应只返回掩码预览。

## 上游协议映射

| 配置值 | 端点 | 搜索映射 |
|---|---|---|
| `responses` | `POST /v1/responses` | CPA Responses 内置工具（`web_search` / `x_search`）；这是向后兼容的默认值，并可提供搜索轮次进度。 |
| `chat_completions` | `POST /v1/chat/completions` | xAI 兼容的 `search_parameters`，使用 `web` 或 `x` 来源并解析 Chat Completions 流。对于“正在搜索”等仅表示状态的短回复，会在有限次数内自动请求继续回答，确保 MCP 收到最终答案或明确错误。 |
| `anthropic_messages` | `POST /v1/messages` | Anthropic 服务端 `web_search_20250305` 工具与 Messages SSE。网页搜索保留配置的域名筛选；X 搜索使用同一服务端工具并限制在 `x.com`，同时要求返回直接的 X 帖子链接。 |

实际能力取决于所使用的 CPA 版本、提供方和模型。对于现有 Grok/CPA 部署，Responses 仍是兼容性最稳妥的选项。图片搜索选项仅在 Responses 协议存在对应字段时生效，其他协议没有等价字段时会忽略。

即使答案正文相同，不同协议暴露的元数据也可能不同：

- Responses 通常提供最完整的搜索轮次进度和结构化引用数据。
- Chat Completions 只有在 CPA 返回兼容的非标准搜索事件时才会提供进度；标准 Chat 数据块可能只有最终文本和用量。
- Anthropic Messages 可能在答案正文中包含来源 URL，但是否返回结构化 citation 数据块取决于 CPA 的提供方转换实现。
- 只要上游提供 token 统计，服务会在不同协议之间统一规范化 `usage` 字段。
