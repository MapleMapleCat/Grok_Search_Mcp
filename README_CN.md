# grok-search-mcp

[English](./README.md)

`grok-search-mcp` 是一个仅提供 HTTP 传输的 [Model Context Protocol（MCP）](https://modelcontextprotocol.io/)服务端，将 Grok 的实时网页搜索、X/Twitter 搜索和模型发现能力暴露给 MCP 客户端。

本项目**不直接调用 xAI 官方 API**，而是连接已经部署的 [CLIProxyAPI（CPA）](https://github.com/router-for-me/CLIProxyAPI)。CPA 负责上游 xAI 认证，`grok-search-mcp` 负责 MCP 传输、客户端 API Key、限流配额、用量统计和管理面板。

> [!IMPORTANT]
> 本项目仅支持 **Streamable HTTP**，不提供 stdio 传输，也不内置 TLS 终止。

- `grok-search-mcp` 必须作为独立 HTTP 服务启动，MCP 客户端通过 `http://<host>:<port>/mcp` 连接。
- 不能将本项目配置为由 MCP 客户端通过命令启动、再使用标准输入和标准输出通信的 stdio 服务。
- 服务自身只监听普通 HTTP，不读取 HTTPS 证书或私钥，也不负责 TLS 握手。
- 公网部署时，应在 `grok-search-mcp` 前放置 Nginx、Caddy、Traefik、Kubernetes Ingress 或云负载均衡器等可信反向代理，由代理对外提供 HTTPS，再通过内部 HTTP 转发到 `grok-search-mcp`。

典型的生产请求链路如下：

```text
MCP 客户端 -- HTTPS --> 反向代理 / 负载均衡器 -- HTTP --> grok-search-mcp /mcp
                         （TLS 在此终止）
```

## 功能特性

- `/mcp` Streamable HTTP MCP 端点
- 三个只读 MCP 工具：
  - `grok_web_search`
  - `grok_x_search`
  - `grok_list_models`
- 可选择 CPA 上游协议：OpenAI Responses、OpenAI Chat Completions 或 Anthropic Messages
- 将上游搜索轮次转换为 MCP progress 通知
- 用户级客户端 API Key，可单独启用或禁用
- 基于 tier 的 RPM 和每月成功调用额度
- 面向直连或可信代理的对端感知 `/mcp` 与面板认证 IP 防护
- 使用 SQLite 持久化用户、Key、tier、用量、邀请码和服务设置
- 内嵌管理面板，无独立前端构建步骤
- 上游、搜索并发、代理、注册模式、debug 和运行指标采集设置支持运行时热更新
- 使用非 root 运行镜像的 Docker Compose 部署

## 架构

```text
支持 Streamable HTTP 的 MCP 客户端
        |
        |  POST /mcp
        |  Authorization: Bearer <MCP 客户端 API Key>
        v
grok-search-mcp
  |     |
  |     +---- /panel/ 与 /panel/v1/* ---- 管理员和用户
  |
  +---------- SQLite ------------------- 用户、Key、tier、用量、设置
  |
  |  POST /v1/responses、/v1/chat/completions 或 /v1/messages
  |  GET  /v1/models
  |  Authorization: Bearer <CPA API Key>
  v
CLIProxyAPI
  |
  v
xAI / Grok
```

### 三类凭证不可混用

| 凭证 | 使用位置 | 用途 |
|---|---|---|
| CPA API Key | `grok-search-mcp` -> CPA | 认证所选上游搜索端点和 `/v1/models` 请求。 |
| MCP 客户端 API Key | MCP 客户端 -> `/mcp` | 在面板创建并可按需复制；数据库保存鉴权哈希和由 `GROK_JWT_SECRET` 派生密钥加密的可恢复密文。 |
| 面板 JWT | 浏览器/API 客户端 -> `/panel/v1` | 登录面板后返回，不能用于认证 `/mcp`。 |

## 环境要求

- 当前文档化的本地运行目标为 Linux
- 本地构建需要 Go 1.25.12 或更高版本
- 可访问 `/v1/models`，并至少兼容 `/v1/responses`、`/v1/chat/completions`、`/v1/messages` 之一的 CPA 服务
- 容器部署可选用 Docker 和 Docker Compose
- MCP 客户端需要支持 Streamable HTTP 和自定义 Bearer Header

项目使用纯 Go SQLite 驱动 `modernc.org/sqlite`，不依赖 CGO。

## 快速开始

### 1. 构建

```bash
go build -o grok-search-mcp ./cmd/grok-search-mcp
```

可以在构建时注入版本号：

```bash
go build \
  -ldflags "-X github.com/MapleMapleCat/Grok_Search_Mcp/internal/version.Version=1.2.3" \
  -o grok-search-mcp ./cmd/grok-search-mcp

./grok-search-mcp -version
```

### 2. 配置并启动

启动配置分成两层：

- `.env` 是用户自己的基础配置，保存 CPA 上游地址/端口和首次部署必须填写的
  凭证；
- `advanced.env` 提供其余具有安全默认值的高级配置，首次部署无需修改；参数说明见
  [高级配置文档](./ADVANCE_README.md#简体中文)。

Linux 发布压缩包包含 `.env.example` 和 `advanced.env`，因此使用预编译二进制时
也可以直接采用相同流程。Compose 文件仍只随源码仓库提供。

```bash
cp .env.example .env
${EDITOR:-vi} .env
```

基础 `.env` 包含 CPA 上游地址和两个必填凭证：

```dotenv
CPA_BASE_URL=
CPA_API_KEY=replace-with-your-cpa-api-key
GROK_JWT_SECRET=replace-with-a-strong-random-secret-of-at-least-32-bytes
```

本地二进制运行且 CPA 位于本机时，`CPA_BASE_URL` 可以留空，默认使用
`http://127.0.0.1:8317`。Docker Compose 中留空则默认连接
`http://host.docker.internal:8317`。CPA 位于其他主机或端口时，直接在基础
`.env` 中填写完整地址。

可以使用 `openssl rand -hex 32` 生成 JWT secret。不要把生成结果写入
`advanced.env` 或提交到版本库。

使用本地二进制时，先加载高级默认值，再加载用户 `.env`，这样用户显式设置的
同名变量始终优先：

```bash
mkdir -p data
set -a
source advanced.env
source .env
set +a

./grok-search-mcp
```

使用源码仓库中的 Docker Compose 时，Compose 会按相同顺序自动加载
`advanced.env` 和 `.env`：

```bash
docker compose up -d --build
```

Compose 会为宿主机上的 CPA 使用容器专用默认地址
`http://host.docker.internal:8317`。如需修改 Compose 的 CPA 地址，请直接编辑
基础 `.env` 中的 `CPA_BASE_URL`，使其在 Compose 插值阶段生效。

大多数部署不需要编辑 `advanced.env`。只有需要修改上游协议、监听与存储、数据
保留、认证保护、可信代理、容量限制、debug 或上游代理时才调整它。CPA 地址和
端口统一在基础 `.env` 中配置。现有包含全部变量的 `.env` 仍然兼容；因为 `.env`
最后加载，其中的值会覆盖 `advanced.env`。服务仍然只读取普通环境变量，不存在
额外配置格式。

默认端点：

| 服务 | 地址 |
|---|---|
| MCP | `http://127.0.0.1:8080/mcp` |
| 管理面板 | `http://127.0.0.1:8080/panel/` |
| 面板 REST API | `http://127.0.0.1:8080/panel/v1/` |

### 3. 登录并创建 MCP 客户端 Key

当数据库中没有已启用的管理员时，服务会初始化 `admin` 账号，并把凭据写入精确
权限为 `0600` 的有界 JSON 文件。默认路径是
`<GROK_DB_PATH>.bootstrap-admin`；启动日志只输出文件路径，绝不输出密码。请使用
运行服务的同一操作系统用户读取该文件，并尽快轮换密码。

本地二进制部署从本地数据目录读取：

```bash
bootstrap_password="$(jq -r '.password' ./data/grok-search-mcp.db.bootstrap-admin)"
```

Docker Compose 部署从容器数据卷读取，JSON 仍由宿主机的 `jq` 解析：

```bash
bootstrap_password="$(docker compose exec -T grok-search-mcp \
  sh -c 'cat /app/data/grok-search-mcp.db.bootstrap-admin' | jq -r '.password')"
```

直接使用下文发布镜像的 `docker run` 部署时，通过容器名称读取：

```bash
bootstrap_password="$(docker exec -i grok-search-mcp \
  sh -c 'cat /app/data/grok-search-mcp.db.bootstrap-admin' | jq -r '.password')"
```

取得 bootstrap 密码后，登录、修改密码并创建第一个 MCP Key：

```bash
login_token="$(curl -sS -X POST "http://127.0.0.1:8080/panel/v1/auth/login" \
  -H "Content-Type: application/json" \
  -d "$(jq -n --arg password "${bootstrap_password}" '{username:"admin",password:$password}')" | jq -r '.token')"

replacement_session="$(curl -sS -X POST "http://127.0.0.1:8080/panel/v1/me/change-password" \
  -H "Authorization: Bearer ${login_token}" \
  -H "Content-Type: application/json" \
  -d "$(jq -n --arg current "${bootstrap_password}" --arg new "replace-with-a-new-password" \
    '{current_password:$current,new_password:$new}')")"
login_token="$(jq -r '.token' <<<"${replacement_session}")"

curl -sS -X POST "http://127.0.0.1:8080/panel/v1/keys" \
  -H "Authorization: Bearer ${login_token}" \
  -H "Content-Type: application/json" \
  -d '{"name":"local-client"}'
```

响应中的 `api_key` 可立即使用；之后也可以在 **API 密钥** 页面按需复制。每位
用户默认最多拥有 20 个 Key；已禁用 Key 仍计数，删除 Key 会释放容量。

bootstrap 管理员成功修改密码并提交数据库后，服务会尽力删除凭据文件；删除失败
不会回滚已生效的密码，请手动清理残留文件。若启动在创建账号前失败，下次启动会
安全复用已有的合规凭据文件。不要编辑、放宽权限或从备份恢复过期副本。

### 4. 连接 Claude Code

Claude Code 是当前仓库内提供了明确配置示例的客户端：

```bash
export GROK_SEARCH_MCP_API_KEY="grok_xxx"

claude mcp add --transport http grok-search-mcp http://127.0.0.1:8080/mcp \
  --header "Authorization: Bearer ${GROK_SEARCH_MCP_API_KEY}"
```

项目级 `.mcp.json` 可以使用环境变量展开：

```json
{
  "mcpServers": {
    "grok-search-mcp": {
      "type": "http",
      "url": "http://127.0.0.1:8080/mcp",
      "headers": {
        "Authorization": "Bearer ${GROK_SEARCH_MCP_API_KEY}"
      }
    }
  }
}
```

不要提交真实 API Key。其他客户端只要支持 Streamable HTTP 和自定义 `Authorization: Bearer ...` Header，协议上即可接入；仓库未提供的特定客户端配置不应视为已验证。

## MCP 工具

所有工具均为只读。搜索失败会作为 `isError=true` 的 MCP 工具结果返回，正常工具错误不会中断 MCP 会话。

### `grok_web_search`

通过 Grok 执行实时公开网页搜索。

| 参数 | 类型 | 必填 | 说明 |
|---|---|:---:|---|
| `query` | string | 是 | 非空搜索请求。 |
| `model` | string | 否 | 覆盖默认模型，值必须包含 `grok`。 |
| `allowed_domains` | string[] | 否 | 只搜索指定域名，最多 5 项。 |
| `excluded_domains` | string[] | 否 | 排除指定域名，最多 5 项。 |
| `enable_image_understanding` | boolean | 否 | 启用网页图片理解。 |
| `enable_image_search` | boolean | 否 | 启用图片搜索结果。 |

`allowed_domains` 与 `excluded_domains` 不能同时使用。域名项必须是纯域名，不能是 URL；通配符、IP、端口、路径、`localhost` 和 `.local` 域名会被拒绝。

### `grok_x_search`

通过 Grok 实时搜索 X/Twitter 帖子。

| 参数 | 类型 | 必填 | 说明 |
|---|---|:---:|---|
| `query` | string | 是 | 非空搜索请求。 |
| `model` | string | 否 | 覆盖默认模型，值必须包含 `grok`。 |

域名筛选和图片相关参数只适用于 `grok_web_search`。

具体的上游映射取决于所选协议：Responses 使用 CPA 原生的 `x_search` 工具，Chat Completions 使用 `x` 搜索来源，Anthropic Messages 使用 CPA 支持的服务端网页搜索工具并限制在 `x.com`。之所以不声明自定义 Anthropic `x_search` 工具，是因为 CPA 会将其视为需要客户端执行并回传结果的工具调用，单独使用不会产生最终搜索答案。

### `grok_list_models`

无参数。工具读取 CPA `GET /v1/models`，清理并去重模型 ID，只保留包含 `grok` 且不包含 `imagine`、`video` 的项目。

### 搜索结果结构

```json
{
  "answer": "Grok 综合检索后的回答",
  "citations": [
    "https://example.com/source"
  ],
  "sources": [
    {
      "url": "https://example.com/source",
      "title": "Example source"
    }
  ],
  "usage": {
    "input_tokens": 120,
    "output_tokens": 340,
    "total_tokens": 460,
    "reasoning_tokens": 0
  }
}
```

上游未提供时，`citations`、`sources` 和 `usage` 可能省略。服务会在配额预留和用量统计前主动拒绝 JSON-RPC batch 请求，以及重复或大小写冲突的 `method`、`params`、`params.name` 路由字段。

## 高级配置

高级启动参数、Usage 数据保留、SQLite 运维、客户端 IP 信任模式、持久化设置和
上游协议映射已移至 [高级配置说明](./ADVANCE_README.md#简体中文)。

## 用户、注册、Tier 与配额

新数据库默认禁用注册；只有显式将 `GROK_INITIAL_REGISTRATION_MODE` 设为
`invite` 或 `free` 才会在首次持久化时选择其他模式。初始设置行创建后，注册模式
可以运行时切换，且持久化值始终优先：

| 模式 | 行为 |
|---|---|
| `free` | 允许公开自助注册。 |
| `invite` | 必须使用有效、已启用且未耗尽的邀请码。 |
| `disabled` | 禁止公开注册。 |

管理员可以创建、复制、禁用和删除邀请码，并设置每个邀请码的注册次数上限。注册校验仍使用不可逆哈希；新创建的邀请码同时以 AES-256-GCM 密文保存，仅在管理员明确点击复制时按需解密。升级前创建的 hash-only 邀请码仍可用于注册，但无法恢复完整内容；需要复制时应删除并重新生成。

每个用户属于一个 tier。该用户的所有 API Key 共享 tier 的 RPM 和月成功调用额度。只有实际 `tools/call` 会计量，初始化、ping、工具列表等请求不计入。

系统通过显式的“默认方案”标记决定新注册用户和管理员新建用户的初始 tier，不再依赖 `tier0` 这个名称。任意方案都可以在管理面板中设为默认；切换后只影响后续创建的用户，不会自动改动已有用户。删除非默认方案时，系统会在同一事务内将该方案的全部用户迁移到删除时的当前默认方案，再删除原方案；用户当月已使用的成功调用次数不会重置。默认方案不能直接取消或删除，必须先将另一个方案设为默认。

Tier 不再包含重复的 `level` 字段。方案没有隐含的权限高低，只表达一组 RPM 与月度成功调用额度；管理面板按创建时间稳定展示方案。

新数据库的预置 tier（`tier0` 仅作为初始默认方案）：

| Tier | 初始默认 | RPM | 每月成功调用数 |
|---|:---:|---:|---:|
| `tier0` | 是 | 10 | 800 |
| `tier1` |  | 20 | 4,000 |
| `tier2` |  | 40 | 16,000 |
| `tier3` |  | 60 | 40,000 |
| `tier4` |  | 120 | 160,000 |
| `tier5` |  | 300 | 800,000 |
| `tier6` |  | 不限 | 不限 |

月度周期按 UTC 自然月计算。工具执行前先预留成功调用额度；调用失败时回滚。管理员可以在面板修改 tier 参数。

`/mcp` 中间件顺序保持不变；`IP RPM` 会在 API Key 鉴权前始终解析并校验客户端身份：

```text
MaxBody -> IP RPM -> API Key -> ExtractToolName -> User RPM -> Search Concurrency -> Quota -> Usage -> MCP handler
```

## 管理面板 API 概览

内嵌面板位于 `/panel/`，API 位于 `/panel/v1`。

公开认证路由：

```text
GET  /panel/v1/auth/registration-settings
POST /panel/v1/auth/registration-challenge
POST /panel/v1/auth/register
POST /panel/v1/auth/login
```

注册采用一次性工作量证明：客户端先请求有效期为 5 分钟的签名挑战，在本地计算满足难度要求的 SHA-256 nonce，再将 `proof.challenge` 和 `proof.nonce` 随注册请求提交。默认难度为 20 个前导零位；验证成功后挑战立即失效，不能复用。内嵌面板会在 Web Worker 中完成计算，避免阻塞页面交互。

登录用户路由涵盖用户信息、密码/会话生命周期、API Key 和用量：

```text
GET    /panel/v1/me
POST   /panel/v1/me/change-password
POST   /panel/v1/me/revoke-sessions
GET    /panel/v1/overview/health
GET    /panel/v1/keys
POST   /panel/v1/keys
POST   /panel/v1/keys/{id}/reveal
PATCH  /panel/v1/keys/{id}
DELETE /panel/v1/keys/{id}
GET    /panel/v1/keys/{id}/usage
GET    /panel/v1/usage
GET    /panel/v1/usage/records
GET    /panel/v1/usage/records/{id}
```

修改密码要求 `current_password` 和 `new_password` 均为 8-72 字节。两个生命周期
接口都会递增当前用户的 `token_version`，立即使此前签发的所有面板 JWT 失效，并
返回新的 `token`、`expires_at` 和当前 `user`。账户页面会把替换令牌及过期时间
作为一个值原子写入 `sessionStorage`。吊销全部会话只影响面板 JWT，不影响 MCP
API Key。

`GET /panel/v1/overview/health` 用于向已登录的面板展示上游和模型可用性；它不同于容器对 `/panel/` 执行的未鉴权存活检查。

`/panel/v1/admin/` 下的管理员路由用于管理用户、tier、服务设置、邀请码、模型和用量。除公开路由外，面板请求需要：

```text
Authorization: Bearer <面板 JWT>
```

邀请码复制使用独立的管理员密文读取接口：

```text
POST /panel/v1/admin/invite-codes/{id}/reveal
```

列表和更新响应只包含邀请码元数据及可见前缀，不会批量返回完整邀请码。

## Docker 部署

使用项目提供的 Compose 文件构建并运行当前源码：

```bash
cp .env.example .env
${EDITOR:-vi} .env
docker compose up -d --build
```

直接运行已发布镜像、避免重新构建本地源码：

```bash
docker pull maplemaplecat/grok-search-mcp:latest
docker run -d \
  --name grok-search-mcp \
  --restart unless-stopped \
  --pull always \
  --env-file advanced.env \
  --env-file .env \
  --add-host host.docker.internal:host-gateway \
  -p 127.0.0.1:8080:8080 \
  -v grok-search-mcp-data:/app/data \
  maplemaplecat/grok-search-mcp:latest
```

每次发布 GitHub Release 时，流水线会同时更新不可变的版本标签和可变的
`latest` 标签。需要固定到确切版本的生产部署仍应使用版本标签。仅更新
`latest` 不会替换已经运行的容器；要应用新镜像，仍需通过部署自动化重新创建容器。

直接运行发布镜像时，请在基础 `.env` 中填写容器可访问的 CPA 地址。CPA 运行在
Docker 宿主机上时使用：

```dotenv
CPA_BASE_URL=http://host.docker.internal:8317
```

项目提供的容器：

- 使用 `CGO_ENABLED=0` 构建
- 以非 root `app` 用户运行
- 监听 8080 端口
- 将 SQLite 数据存放在 `/app/data`
- Compose 使用 `grok-search-mcp-data` 命名卷
- 通过 `/panel/` 执行健康检查

Compose 会把 `advanced.env` 中已启用的显式代理或标准代理变量传入容器。代理值
包含真实凭证时，应改放在不提交的 `.env` 或外部 secret 管理系统中。

## 生产部署与安全

- 公网暴露前必须放在 HTTPS 反向代理之后，服务本身不提供 TLS。
- 不要泄露面板 JWT、MCP 客户端 API Key、CPA Key、邀请码或真实 `.env`。
- 初始化管理员登录后应立即轮换凭据。
- 限制 SQLite 文件访问权限，并对其进行安全备份。
- 客户端直连时保持 `GROK_CLIENT_IP_MODE=direct`；转发 Header 会被忽略，无法选择限流身份。
- 反向代理部署应设置 `GROK_CLIENT_IP_MODE=trusted_proxy`，只允许代理的直接对端 CIDR，并由代理覆盖 `X-Real-IP`、重建 `X-Forwarded-For`。缺失 Header 返回 `400`，不可信对端返回 `403`。
- 明文应用端口应只绑定回环或内部网络；项目提供的 Compose 和 `docker run` 示例默认只发布到宿主机回环地址。
- 在代理层对 `/mcp`、面板登录和注册接口增加限流。
- 除排障外保持 debug 关闭。即使认证 Header 会脱敏，debug 上下文仍可能保留请求或响应正文。
- MCP 客户端 API Key 和新创建邀请码的鉴权使用不可逆哈希；可复制内容以 AES-256-GCM 密文保存，并绑定对应记录身份。
- 更换 `GROK_JWT_SECRET` 或升级旧版 hash-only 数据库时，无法解密的 API Key 会自动轮换；客户端需要从面板复制替代密钥并更新配置。

## 开发与测试

运行默认测试：

```bash
go test ./...
```

验证构建：

```bash
go build ./cmd/grok-search-mcp
```

真实 CPA/xAI 集成测试需要显式启用：

```bash
export GROK_INTEGRATION_TEST="1"
export CPA_API_KEY="replace-with-your-cpa-api-key"
export CPA_BASE_URL="http://127.0.0.1:8317"

go test ./test/grok -run TestIntegrationSearchLiveCPA -v
```

面板前端是内嵌的原生 HTML、CSS 和 JavaScript，不需要 Node.js 构建。仓库目前没有 Makefile 或任务运行器。GitHub Actions 的发布/手动工作流会运行 `go test ./...`、构建 Linux 压缩包并发布 Docker 镜像；目前尚无 push 或 pull request 校验工作流。贡献代码时可使用 `gofmt`、`go vet` 等标准 Go 工具。

### 代码结构

```text
cmd/grok-search-mcp/ 进程入口和版本参数
internal/app/       应用组合、初始化、HTTP 服务与优雅退出
internal/auth/      MCP API Key 鉴权和面板 JWT
internal/config/    环境变量与持久化设置映射
internal/grok/      CPA 请求、SSE 解析、模型列表
internal/mcp/       MCP Server Instructions 和工具注册
internal/panel/     面板 REST API
internal/panelui/   内嵌管理前端
internal/quota/     月成功调用额度预留
internal/ratelimit/ 来源 IP 与用户级限流
internal/store/     SQLite schema 和持久化
internal/usage/     工具调用统计及可选 debug 捕获
test/http/          HTTP 集成与防护测试
test/grok/          可选真实上游集成测试
```

## 故障排查

| 现象 | 检查项 |
|---|---|
| `GROK_JWT_SECRET is required` | 在服务环境中设置至少 32 字节的密钥。 |
| 新数据库启动失败 | 设置有效的 `CPA_API_KEY`，并确认数据库目录可写。 |
| MCP 返回 `401` 或 `403` | 使用 MCP 客户端 API Key 而不是面板 JWT，并检查 Key 和用户是否启用。 |
| MCP 返回 `429` | 检查来源 IP RPM、用户 tier RPM 和月成功调用额度。 |
| 上游超时或 HTTP 错误 | 检查 CPA 地址、CPA Key、代理设置和 CPA 健康状态。 |
| Docker 无法访问宿主机 CPA | 使用 Compose 提供的 `http://host.docker.internal:<port>`。 |
| 模型列表为空 | 确认 CPA 返回 Grok 模型 ID；`imagine` 和 `video` 会被主动过滤。 |
| 客户端无法连接 | 确认客户端支持 Streamable HTTP，并向准确的 `/mcp` 地址发送 Bearer Header。 |

## 许可证

本项目采用 [CC BY-NC 4.0](./LICENSE)。在署名并遵守许可证条款的前提下，可用于非商业复制、分发和修改；商业用途需要获得版权持有人的事先书面许可。
