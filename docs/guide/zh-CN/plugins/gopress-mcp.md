# GoPress MCP（Agent 接入）

`gopress-mcp` 是 GoPress 官方的可选 MCP 协议适配插件。它把 Core 中协议无关的
Agent Tool Registry 与 Executor 暴露为站点级远程 MCP Server，使兼容客户端
可以在 GoPress 权限边界内读取或维护内容。

本篇聚焦插件启用、客户端连接和排障；Core Agent 的设计原则、执行管线、身份
安全与扩展契约见独立的 [Agent 与 MCP 模块](../agent/overview.md)。

当前实现处于 **Safe Write Beta（Phase 3）**：Phase 0–3 已完成，Phase 4 的
OAuth 2.1 与 Phase 5 的 Resources、Prompts、Tasks、MCP Apps 尚未实现。插件
默认未激活，激活后的 Tool Profile 仍默认为 `read_only`。

## 当前能力

| 项目 | 当前实现 |
|---|---|
| Endpoint | 站点公开 URL 下的 `POST /mcp` |
| 传输 | 无状态 Streamable HTTP |
| 协议 | `2026-07-28`，兼容 `2025-11-25` |
| SDK | 官方 `modelcontextprotocol/go-sdk` `v1.7.0-pre.3` |
| 认证 | 管理员签发的短期 Bearer Token |
| Tool | 6 个只读 Tool + 6 个受控写 Tool |
| 默认策略 | 插件未激活；激活后 `read_only` |
| Tool 列表 | 按凭证 Scope、当前用户 RBAC 与站点策略过滤，私有缓存 30 秒 |
| 审计 | 记录主体、Tool、状态、耗时、参数摘要与结果摘要，不记录 Token 或参数值 |

这不是 REST API 的自动包装。MCP 插件只负责协议、传输和认证映射；内容规则、
权限、所有权、幂等、审计、Hook 与缓存失效由 Core 的通用 Agent/领域服务执行。

```mermaid
flowchart LR
    C["MCP 客户端"] -->|"Bearer + POST /mcp"| P["gopress-mcp 插件"]
    P --> E["Core Agent Executor"]
    E --> A["Scope + RBAC + 所有权 + Tool Policy"]
    E --> R["Agent Tool Registry"]
    R --> D["Content / Taxonomy / Media 服务"]
    D --> H["Hook + 缓存失效"]
    E --> U["Agent 审计 + 幂等记录"]
```

## 启用与首次连接

### 1. 确认站点公开 URL

MCP Endpoint 由站点公开 URL 派生，例如：

```text
https://example.com/mcp
```

生产站点应使用 HTTPS。本机开发仅允许使用 `http://localhost`、
`http://127.0.0.1` 或 `http://[::1]`。Token 的 `audience` 会精确绑定签发时的
Endpoint；修改域名、协议或站点 URL 后，应重新签发 Token。

### 2. 激活插件

进入后台「插件管理」，激活 `GoPress MCP`。插件是默认停用的，只有激活期间
才会注册 `/mcp` 和自己的后台管理接口；停用后路由会随 Router 重建被移除，
Tool Profile 也会回落到只读。

### 3. 打开插件设置

在插件卡片进入设置，或直接访问：

```text
/admin/plugins/gopress-mcp/settings
```

设置页包含四个 Tab：

- **连接概览**：Endpoint、协议、SDK、Tool 数和运行诊断。
- **写入策略**：`read_only` / `safe_write` 与六个写 Tool 的逐项开关。
- **访问凭证**：签发、查看状态和撤销 Token。
- **调用审计**：按 Tool、状态和分页查询 Agent 调用。

这些入口使用 `mcp.*`、`agent_credential.*` 与 `agent_audit.*` 独立权限保护。
默认角色中只有超级管理员的通配权限可以访问，不能仅凭已登录 Cookie 越过
对应 `resource.action` 检查。

### 4. 签发最小权限 Token

在「访问凭证」中填写名称和有效期，选择客户端真正需要的 Scope。默认有效期
为 30 天，可选范围为 1–90 天。Token 只在创建成功后显示一次，数据库只保存
单向摘要；遗失后无法找回，应撤销旧 Token 并重新签发。

只读客户端通常选择：

```text
gopress:site:read
gopress:content:read
gopress:taxonomy:read
gopress:media:read
```

写 Scope 只有在站点已经启用对应写 Tool 后才允许签发。已签发 Token 仍受其
所属用户的实时账号状态与角色约束；账号停用、Token 过期或撤销后，下次请求
立即失败。

### 5. 配置客户端

设置页可以复制不绑定具体产品的通用远程 MCP 配置：

```json
{
  "mcpServers": {
    "gopress": {
      "type": "http",
      "url": "https://example.com/mcp",
      "headers": {
        "Authorization": "Bearer gp_agent_REPLACE_WITH_YOUR_TOKEN"
      }
    }
  }
}
```

不同客户端的最外层配置名可能不同，但 URL、HTTP 传输与 Authorization Header
保持一致。不要把真实 Token 提交到 Git、文档、聊天记录或公开日志。

## 用 curl 验证

优先使用真实 MCP 客户端或 Inspector。需要从命令行检查连通性时，可以先通过
兼容协议执行初始化：

```bash
export GOPRESS_MCP_URL='https://example.com/mcp'
export GOPRESS_MCP_TOKEN='gp_agent_REPLACE_WITH_YOUR_TOKEN'

curl --fail-with-body -sS \
  -X POST "$GOPRESS_MCP_URL" \
  -H "Authorization: Bearer $GOPRESS_MCP_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  --data-binary '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
      "protocolVersion": "2025-11-25",
      "capabilities": {},
      "clientInfo": {"name": "curl-test", "version": "1.0.0"}
    }
  }'
```

然后查询当前凭证可见的 Tool：

```bash
curl --fail-with-body -sS \
  -X POST "$GOPRESS_MCP_URL" \
  -H "Authorization: Bearer $GOPRESS_MCP_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H 'Mcp-Protocol-Version: 2025-11-25' \
  --data-binary '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/list",
    "params": {}
  }'
```

若只看到部分 Tool，通常是正常的：`tools/list` 会同时应用站点 Tool Profile、
Token Scope 与当前用户 RBAC。修改写入策略或签发 Scope 后，应重新查询 Tool
列表；结果带 `cacheScope: private`、`ttlMs: 30000` 和 Agent Revision。

## Scope 与 Tool 参考

### 只读 Tool

| Tool | Scope | Core RBAC | 主要参数 |
|---|---|---|---|
| `gopress.site.get` | `gopress:site:read` | `dashboard.read` | 无 |
| `gopress.content_types.list` | `gopress:content:read` | `content.read` | 无 |
| `gopress.content.list` | `gopress:content:read` | `content.read` | `content_type`；可选 `status`、`search`、`taxonomy`、`term`、`page`、`per_page` |
| `gopress.content.get` | `gopress:content:read` | `content.read` | `content_type`、`id` |
| `gopress.taxonomy.list` | `gopress:taxonomy:read` | `taxonomy.read` | `content_type`；可选 `taxonomy` |
| `gopress.media.list` | `gopress:media:read` | `media.read` | 可选 `mime_type`、`page`、`per_page` |

内容状态仅接受 `published`、`pending`、`draft`、`archived`、`trash`；默认读取
`published`。列表默认每页 20 条，最大 100 条。内容详情只返回注册 ContentType
允许的 Meta，正文继续经过 Core sanitizer；媒体列表不会返回服务器文件路径。

### 写 Tool

| Tool | Scope | Core RBAC | 风险与附加条件 |
|---|---|---|---|
| `gopress.content.create_draft` | `gopress:content:write` | `content.create` | 只能创建草稿；必须有 `idempotency_key` |
| `gopress.content.update` | `gopress:content:write` | `content.update` 或本人资源的 `content.update_own` | 必须有 `expected_updated_at` 与 `idempotency_key` |
| `gopress.content.publish` | `gopress:content:publish` | `content.publish` | 必须有乐观锁、幂等键与 `confirm: true` |
| `gopress.content.move_to_trash` | `gopress:content:write` | `content.delete` 或本人资源的 `content.delete_own` | 软删除；必须有乐观锁、幂等键与 `confirm: true` |
| `gopress.content.restore` | `gopress:content:write` | `content.update` | 只把回收站内容恢复为草稿；必须有乐观锁与幂等键 |
| `gopress.media.update_metadata` | `gopress:media:write` | `media.update` 或本人媒体的 `media.update_own` | 只允许 `alt_text`、`title`、`caption`；必须有乐观锁与幂等键 |

写 Tool 只处理当前注册且可编辑的 ContentType。创建草稿会校验类型支持的字段、
父内容类型、Meta 声明、Slug 唯一性与内容清理；`update` 不允许通过通用字段
偷偷切换发布状态，发布、回收和恢复必须使用独立 Tool。

## 开启 Safe Write

写能力需要同时满足四层条件：

```text
站点 Profile 为 safe_write
AND 对应 Tool 已逐项启用
AND Token 含对应 gopress:* Scope
AND 当前主体拥有 Core resource.action 权限（含所有权判断）
```

建议从 `gopress.content.create_draft` 和 `gopress.content.update` 开始，只给测试
Token 添加 `gopress:content:write`。发布和移入回收站不会因选择 `safe_write`
自动开启，必须单独勾选。`content.publish` 默认只授予编辑和超级管理员，作者或
投稿者不能靠 Token Scope 获得发布权限。

所有写调用必须使用长度 8–200 的 `idempotency_key`。同一凭证、同一 Tool、
同一键和相同参数的重试会返回已保存结果；相同键配不同参数会冲突。更新、
发布、回收、恢复和媒体更新还必须传入最近一次读取结果中的 RFC 3339
`updated_at` 作为 `expected_updated_at`。资源已被其他请求修改时，Core 拒绝
陈旧写入，客户端应重新读取、重新判断后使用新的幂等键重试。

下面是创建草稿的 `tools/call` 参数示例（外层协议消息由 MCP 客户端生成）：

```json
{
  "name": "gopress.content.create_draft",
  "arguments": {
    "content_type": "post",
    "title": "Agent 创建的草稿",
    "content": "<p>正文</p>",
    "comment_status": "open",
    "idempotency_key": "draft-20260809-0001"
  }
}
```

发布示例：

```json
{
  "name": "gopress.content.publish",
  "arguments": {
    "content_type": "post",
    "id": 42,
    "expected_updated_at": "2026-08-09T08:30:00.123456Z",
    "idempotency_key": "publish-post-42-0001",
    "confirm": true
  }
}
```

## 安全与审计边界

- `/mcp` 不接受后台 Cookie、普通后台 JWT 或 REST API Key，只接受专用 Agent
  Bearer Token。
- Token 绑定用户、Scope、Audience、有效期与撤销状态；每次执行前重新加载
  当前用户角色和激活状态。
- Tool 参数先通过受限 JSON Schema、64 KiB 参数上限和最大 16 层 JSON 深度
  校验；HTTP 请求体上限为 256 KiB，Tool 输出上限为 1 MiB。
- Endpoint 使用跨源保护、`private, no-store`、每来源每分钟 120 次的基础限流，
  并传播请求取消。
- 每个 Tool 有独立超时和并发上限。当前读 Tool 超时为 5–10 秒，写 Tool 为
  15 秒。
- 内容和媒体资源同时校验类型与所有权，避免仅凭可猜测 ID 访问或修改其他资源。
- Agent 审计会记录 `started`、`succeeded`、`denied`、`failed`、`replayed`。
  参数只保存字节数、摘要和顶层字段名，结果只保存摘要，不保存正文、参数值或
  Bearer Token。

审计不可用时 Executor 会失败关闭，不会在缺少审计记录的情况下继续执行 Tool。

## 反向代理与部署

生产部署至少确认以下项目：

- 外部访问 URL 与后台配置的站点公开 URL 完全一致，并使用 HTTPS。
- 反向代理把 `Authorization`、`Content-Type`、`Accept`、`Mcp-*` Header 原样
  转发到 GoPress。
- 不要对 `/mcp` 做共享响应缓存；GoPress 已返回 `private, no-store`。
- 代理层请求体、超时和限流不要比 GoPress 的协议约束更宽松到足以形成绕过，
  也不要短到中断正常 Tool 调用。
- Token 只放在客户端密钥存储或环境变量中，并建立定期轮换与即时撤销流程。

当前协议传输本身是无状态的，但凭证、幂等记录和审计保存在站点数据库中，
因此同一站点的多个进程可以共享这些安全状态。Phase 4 之前没有 OAuth 浏览器
授权、Refresh Token 或自动 Scope 提升，客户端需要人工配置 Bearer Token。

## 排障

| 现象 | 检查项 |
|---|---|
| `/mcp` 返回 404 | 插件是否已激活；插件启停后 Router 是否完成重建 |
| 401 | Token 是否完整、过期或撤销；站点 URL 是否变化导致 Audience 不匹配 |
| 403 / `insufficient_scope` | Token 是否包含 Tool 所需 Scope |
| `permission_denied` | Token 所属用户的当前角色是否具有对应 Core 权限；本人资源是否确实归该用户所有 |
| `risk_denied` | Profile 是否为 `safe_write`，且该写 Tool 是否逐项启用 |
| `confirmation_required` | 发布或回收请求是否包含 `confirm: true` |
| `conflict` | `expected_updated_at` 是否陈旧，或幂等键是否被不同参数复用 |
| Tool 数量少于 12 | 这是权限化列表；检查 Profile、逐 Tool 开关、Scope 和 RBAC |
| 非本机 HTTP 诊断告警 | 改用 HTTPS，并确认反向代理公开 URL 与站点 URL 一致 |

设置页「连接概览」中的运行诊断会返回 Endpoint、传输、认证、协议、SDK/插件
版本、Registry Revision、已注册 Tool 数和安全传输状态；「调用审计」可进一步
确认请求在认证、授权、策略或领域层的失败位置。

## 尚未实现

以下能力属于后续 Phase，不应按当前版本能力对外承诺：

- OAuth 2.1 授权服务器、Protected Resource Metadata、PKCE、Refresh Token
  轮换和逐步提升 Scope。
- MCP Resources、Prompts、Tasks、MCP Apps、订阅和 Registry 发布。
- 用户、角色、插件、主题、任意站点设置、文件删除、数据库维护、订单退款或
  支付操作。
- “兼容所有 MCP 客户端”或“Agent 可以操作全部后台”的宽泛保证。

更完整的设计实现见 [Agent 与 MCP 模块](../agent/overview.md)；Phase 决策、威胁
模型和后续路线见
[框架级 Agent 与 MCP 能力规划](../architecture/mcp-agent-capability-plan.md)。
