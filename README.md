# grok-mcp

`grok-mcp` 是一个 HTTP-only 的 MCP（Model Context Protocol）服务端。它把 Grok 的实时联网搜索能力封装成两个 MCP 工具：

- `grok_web_search`：实时网页搜索
- `grok_x_search`：实时 X / Twitter 搜索

本项目不直接对接 xAI 官方 API，而是作为已部署的 CLIProxyAPI（CPA）客户端工作：所有搜索请求都会转发到 CPA 的 `POST /v1/responses`，CPA 负责处理到 xAI 的认证。

```text
支持 Streamable HTTP 的 MCP 客户端
        |
        |  POST /mcp
        |  Authorization: Bearer <grok-mcp 客户端 API Key>
        v
grok-mcp
        |
        |  POST /v1/responses
        |  Authorization: Bearer <CPA_API_KEY>
        v
CLIProxyAPI
        |
        v
xAI / Grok
```

项目现在只保留 HTTP 模式，运行时不再提供传输模式开关。

## 功能

- Streamable HTTP MCP 端点：`/mcp`
- 管理 API：`/admin/v1/*`
- 客户端 API Key 鉴权
- 管理端 Bearer Token 鉴权
- 按 API Key 的每分钟限流
- SQLite 持久化 API Key 与调用明细
- 仅统计真实 `tools/call` 调用，握手和工具列表请求不计入用量
- 上游 SSE 流式解析，并把搜索轮次转成 MCP progress 通知

## 快速开始

### 1. 构建

```powershell
go build -o grok-mcp.exe ./cmd/grok-mcp
```

可选：构建时注入版本号。

```powershell
go build -ldflags "-X github.com/grok-mcp/internal/version.Version=1.2.3" -o grok-mcp.exe ./cmd/grok-mcp
```

查看版本：

```powershell
./grok-mcp.exe -version
```

### 2. 配置并启动

```powershell
$env:CPA_BASE_URL = "http://127.0.0.1:8317"
$env:CPA_API_KEY = "replace-with-your-cpa-api-key"
$env:GROK_ADMIN_TOKEN = "replace-with-a-strong-random-token"
$env:GROK_HTTP_ADDR = ":8080"
$env:GROK_DB_PATH = "./grok-mcp.db"

./grok-mcp.exe
```

启动后：

- MCP 端点：`http://127.0.0.1:8080/mcp`
- 管理 API：`http://127.0.0.1:8080/admin/v1/*`

### 3. 创建客户端 API Key

```powershell
$headers = @{ Authorization = "Bearer $env:GROK_ADMIN_TOKEN" }
$body = @{ name = "local-client"; rate_limit = 60 } | ConvertTo-Json

Invoke-RestMethod `
  -Method Post `
  -Uri "http://127.0.0.1:8080/admin/v1/keys" `
  -Headers $headers `
  -ContentType "application/json" `
  -Body $body
```

响应里的 `api_key` 只返回一次。后续 MCP 客户端访问 `/mcp` 时使用：

```text
Authorization: Bearer <api_key>
Accept: application/json, text/event-stream
Content-Type: application/json
```

## Docker Compose

复制配置模板并填入真实值：

```powershell
Copy-Item .env.example .env
```

启动：

```powershell
docker compose up -d --build
```

`.env` 至少需要设置：

- `CPA_API_KEY`
- `GROK_ADMIN_TOKEN`

容器默认监听 `:8080`，SQLite 数据保存到命名卷 `grok-mcp-data`。

## 配置项

| 环境变量 | 必填 | 默认值 | 说明 |
|---|:---:|---|---|
| `CPA_API_KEY` | 是 | 无 | 调用 CPA 的 Bearer Key |
| `GROK_ADMIN_TOKEN` | 是 | 无 | 管理 API 的静态 Bearer Token |
| `CPA_BASE_URL` | 否 | `http://127.0.0.1:8317` | CPA 根地址，不含尾部 `/` |
| `GROK_MODEL` | 否 | `grok-4.3` | 默认模型，可被工具参数 `model` 覆盖 |
| `GROK_HTTP_TIMEOUT` | 否 | `120` | 上游 HTTP 超时，单位秒 |
| `GROK_MCP_DEBUG` | 否 | 无 | 设为 `1`、`true` 或 `yes` 时输出调试日志 |
| `GROK_HTTP_ADDR` | 否 | `:8080` | HTTP 监听地址 |
| `GROK_DB_PATH` | 否 | `./grok-mcp.db` | SQLite 数据库路径 |
| `GROK_DEFAULT_RATE_LIMIT` | 否 | `60` | 新 Key 未单独设置时的每分钟请求数 |

## MCP 工具

### `grok_web_search`

参数：

| 参数 | 类型 | 必填 | 说明 |
|---|---|:---:|---|
| `query` | string | 是 | 搜索问题 |
| `model` | string | 否 | 覆盖默认模型 |
| `allowed_domains` | string[] | 否 | 仅搜索指定域名，最多 5 个 |
| `excluded_domains` | string[] | 否 | 排除指定域名，最多 5 个 |
| `enable_image_understanding` | bool | 否 | 启用网页图片理解 |
| `enable_image_search` | bool | 否 | 启用图片搜索结果 |

`allowed_domains` 和 `excluded_domains` 不能同时使用。

### `grok_x_search`

参数：

| 参数 | 类型 | 必填 | 说明 |
|---|---|:---:|---|
| `query` | string | 是 | 搜索问题 |
| `model` | string | 否 | 覆盖默认模型 |

## 返回结构

两个工具都返回同一类结构：

```json
{
  "answer": "Grok 综合检索后给出的答案文本",
  "citations": [
    "https://example.com/source-1"
  ],
  "sources": [
    {"url": "https://example.com/source-1", "title": "Source One"}
  ],
  "usage": {
    "input_tokens": 120,
    "output_tokens": 340,
    "total_tokens": 460,
    "reasoning_tokens": 0
  }
}
```

## 管理 API

所有管理 API 都需要：

```text
Authorization: Bearer <GROK_ADMIN_TOKEN>
```

路由：

```text
POST   /admin/v1/keys
GET    /admin/v1/keys
GET    /admin/v1/keys/{id}
PATCH  /admin/v1/keys/{id}
DELETE /admin/v1/keys/{id}
GET    /admin/v1/keys/{id}/usage
GET    /admin/v1/stats
```

创建 Key：

```json
{
  "name": "client-name",
  "rate_limit": 60
}
```

更新 Key：

```json
{
  "name": "new-name",
  "rate_limit": 120,
  "enabled": true
}
```

用量查询支持可选 `since` 参数，格式为 RFC3339：

```text
GET /admin/v1/keys/{id}/usage?since=2026-06-23T00:00:00Z
```

## 代码结构

```text
cmd/grok-mcp/
  main.go                 进程入口，固定启动 HTTP 服务
  http.go                 /mcp 与 /admin 路由组装

internal/config/          环境变量加载与校验
internal/mcp/             MCP 工具注册与工具调用入口
internal/grok/            CPA /v1/responses 请求、SSE 解析、结果汇总
internal/auth/            API Key 与 Admin Token 鉴权
internal/ratelimit/       按 API Key 的内存限流器
internal/usage/           MCP tools/call 用量统计中间件
internal/store/           SQLite 存储、迁移、异步 usage writer
internal/admin/           Key 管理和统计查询 REST API
internal/logx/            调试日志辅助
internal/version/         版本号

test/http/                HTTP 鉴权与管理流程集成测试
test/grok/                上游请求和解析相关测试
```

## 测试

默认测试不触发真实上游调用：

```powershell
go test ./...
```

构建验证：

```powershell
go build ./cmd/grok-mcp
```

Docker Compose 配置验证：

```powershell
Copy-Item .env.example .env
docker compose config
Remove-Item .env
```

真实 CPA / xAI 集成测试需要显式打开：

```powershell
$env:GROK_INTEGRATION_TEST = "1"
$env:CPA_API_KEY = "replace-with-your-cpa-api-key"
$env:CPA_BASE_URL = "http://127.0.0.1:8317"
go test ./test/grok -run TestIntegrationSearchLiveCPA -v
```
