# 运维、排障与测试

Agent/MCP 运行在站点公开 HTTP 面上，同时涉及短期 Credential、数据库幂等与
审计，因此上线检查应覆盖应用、代理、客户端和数据层，而不只是确认 `/mcp`
返回 200。

## 后台运行面

`/admin/plugins/gopress-mcp/settings` 提供：

| Tab | 作用 |
|---|---|
| 连接概览 | Endpoint、SDK/插件/协议版本、Tool 数、Registry Revision、复制客户端配置、HTTPS 诊断 |
| 写入策略 | `read_only` / `safe_write`、Policy Revision、六个 Core 写 Tool 独立开关与风险 |
| 访问凭证 | 当前管理员拥有的 Credential、一次性签发、Scope、过期、最后使用与撤销 |
| 调用审计 | 按 Tool、状态、每页数量查询 Agent Audit |

后台设置使用 `mcp.read/update`；凭证使用
`agent_credential.read/create/delete`；审计使用 `agent_audit.read`。每个自定义
GET/POST Handler 单独复用 Core 认证和 RBAC 中间件，并对写请求执行同源保护。

## 诊断接口字段

设置页诊断返回：

```text
ready
endpoint
transport = streamable_http_stateless
authentication = bearer
protocols
sdk_version / plugin_version
registry_revision / registered_tools
policy_profile / policy_revision / enabled_write_tools
secure_transport
```

`ready: true` 代表当前 Adapter、Registry、Executor 与 Credential Service 已装配，
不代表某个 Token 有所有 Tool 权限，也不代表 OAuth 已实现。

## 反向代理检查

- 外部 URL 与 `site.url` 完全一致，生产使用 HTTPS。
- 保留 `Authorization`、`Content-Type`、`Accept`、`Mcp-Protocol-Version`、
  `Mcp-Method` 与 `Mcp-Name`。
- 不缓存 `/mcp`；保留 GoPress 的 `private, no-store`。
- 允许 POST 和 SDK 需要的 Streamable HTTP 响应行为。
- 请求体限制不高于可接受安全范围，也不要低于正常 MCP 消息需要。
- 代理超时大于 Tool 自身 5–15 秒超时，并正确传播客户端断开。
- 明确可信代理与真实来源策略；当前应用基础限流使用连接来源地址。
- 不在 Access Log 中记录 Authorization Header 或完整请求体。

## 多实例

两个支持协议版本均使用无状态 HTTP，不需要粘性 MCP Session。Credential、幂等
与审计使用站点 PostgreSQL，因此多个 GoPress 进程应连接同一站点数据库并使用
一致表前缀。仍需注意：

- Tool 单进程并发门控不是全局分布式限流。
- 来源限流 Bucket 是进程内状态。
- Policy option 更新后的进程间同步依赖站点现有配置/重载方式。
- Phase 4 指标、告警与分布式追踪尚未完成。

## 常见错误

| Code / HTTP | 含义 | 处理 |
|---|---|---|
| 404 | 插件未激活或 Router 未包含 `/mcp` | 激活插件并确认 Router 已重建 |
| 401 / `unauthenticated` | Token 缺失、过期、撤销、账号停用或 Audience 不匹配 | 检查规范 URL，重新签发最小 Scope Token |
| `insufficient_scope` | Credential 没有所需 Scope | 不扩大角色；只为任务补签所需 Scope |
| `permission_denied` | 当前角色或 Owner 不允许 | 检查实时 RBAC、资源 AuthorID/UploadedBy |
| `risk_denied` | Profile 只读或 Tool 未启用 | 在后台逐项核对 Safe Write |
| `confirmation_required` | 高风险操作未显式确认 | 重新读取资源并传 `confirm: true` |
| `idempotency_required` | 写入缺少有效键 | 生成 8–200 字符的业务请求键 |
| `idempotency_pending` | 相同操作仍在执行 | 等待后用同键同参数重试 |
| `conflict` | 陈旧乐观锁或相同键不同参数 | 重新读取；改变意图时使用新键 |
| `invalid_arguments` | JSON、Schema、类型或字段不合法 | 以 `tools/list` 返回 Schema 为准 |
| `audit_unavailable` | 审计存储不可用 | 修复数据库/迁移，禁止绕过继续执行 |

## 审计查询方法

后台审计可按 Tool 和状态精确过滤。排障时建议按顺序查看：

1. 是否有 `denied`，以及 `error_code` 是 Scope、RBAC、Policy 还是确认。
2. 是否先 `started` 后 `failed`，说明已进入领域执行。
3. 是否出现 `replayed`，说明客户端重试已由幂等收敛。
4. `credential_id`、主体与客户端是否符合预期。
5. `arguments_summary` 的字节数、Hash 与顶层 Key 是否一致，不尝试从审计恢复值。

## 测试分层

### Core 单元测试

`core/agent` 当前覆盖：

- Registry 并发注册、排序、Revision、Owner/Handle 撤销和 generation。
- Schema、Payload 大小/深度、未知字段和输出校验。
- Scope + RBAC + Owner 授权与权限化发现。
- Principal 角色刷新、过期、撤销、账号/Service Account 停用与 Audience。
- 幂等并发收敛、重放、不同参数冲突和 Executor 只调用一次。
- 默认只读 Policy 与逐 Tool Grant。
- Handler panic、超时/错误安全映射和审计失败关闭。

### 领域回归

`core/content` 与 `core/media` 覆盖：

- Command Service 无 Gin 环境执行。
- 保留 Slug、未声明 Meta、跨类型 ID、批量类型边界。
- HTML/SVG/Embed 清理。
- 乐观锁、独立发布/回收/恢复状态转换。
- 媒体 Owner 与可写字段边界。

### MCP 契约测试

`plugins/gopress-mcp` 当前覆盖：

- 官方客户端连接最新协议。
- `2025-11-25` 初始化兼容与不支持版本拒绝。
- Token 缺失、错误 Audience、过期/撤销与协议 Header。
- Tool 列表过滤，以及显式调用隐藏 Tool 仍产生 Scope/RBAC 拒绝审计。
- Safe Write Policy、幂等重放、跨源保护、Body 限制与有界来源限流。
- 后台专用 RBAC、同源写入、Credential 所有权防 IDOR、Policy 持久化。

建议开发阶段运行：

```bash
go test ./core/agent ./core/content ./core/media ./core/audit ./plugins/gopress-mcp
go test -race ./core/agent ./core/content ./core/media ./plugins/gopress-mcp
go vet ./core/agent ./core/content ./core/media ./plugins/gopress-mcp
```

完整交付仍应运行：

```bash
go test ./...
```

## 上线验收清单

- [ ] 插件只在需要的站点激活。
- [ ] Endpoint 为 HTTPS 且与 Credential Audience 一致。
- [ ] 默认 `read_only` 已验证。
- [ ] 每个 Agent 使用独立、最小 Scope、短有效期 Token。
- [ ] Token 未进入 Git、日志、截图或监控标签。
- [ ] 无权限角色、他人资源、跨 ContentType ID 均被拒绝。
- [ ] Safe Write 只启用任务需要的单个 Tool。
- [ ] 发布/回收确认、乐观锁与幂等重试已在测试站验证。
- [ ] 审计成功、拒绝、失败与重放都可查询。
- [ ] 撤销、账号停用和角色降低能在下一请求生效。
- [ ] 代理保留必需 Header，不缓存请求/响应，不记录 Secret。
- [ ] 数据库备份包含 Agent 表，恢复流程已验证。
- [ ] 已明确 OAuth、Resources、Prompts、Tasks 与 MCP Apps 尚未提供。

## Beta 发布边界

在 Phase 4 完成前，不应宣称“生产级 OAuth”“兼容所有 MCP 客户端”或“Agent 可
操作全部后台”。适合的 Beta 场景是：受控站点、明确操作者、短期 Credential、
最小 Tool 集、可复现测试和人工可回滚的内容工作流。
