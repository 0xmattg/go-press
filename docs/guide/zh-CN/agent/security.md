# 身份、授权与安全

Agent Endpoint 的安全目标不是“验证一个 Token 就允许调用”，而是确保机器请求
始终代表一个可识别、可撤销、权限实时更新的 GoPress 主体，并让每次资源操作
同时通过 Scope、RBAC、所有权、风险和数据一致性检查。

## Principal

Core Principal 包含：

```text
Kind            user | service_account
SubjectID       当前用户或 Service Account ID
Username        审计显示名
Role            当前 Core 角色
Scopes          当前 Credential Scope
Audience        当前 Agent Resource
CredentialID    本次凭证 ID
```

Protocol Adapter 不能构造一个静态 Principal 后长期信任。Executor 在每个 Tool
调用前通过 `PrincipalValidator` 重新读取 Credential 和主体状态，因此账号停用、
角色降低、Service Account 停用、Token 撤销或过期会在下一次调用生效。

当前官方 MCP 后台只给当前管理员签发 `user` Credential；Core 已有
`service_account` 模型和服务，但尚未提供完整的站点管理员 UI 与委派流程。

## Credential 生命周期

- 明文 Token 使用 `gp_agent_` 前缀和高熵随机值。
- 数据库只保存 SHA-256 摘要与有限可见前缀。
- Token 绑定 Subject、Scope、Audience、过期时间与创建者。
- 默认有效期 30 天，最大 90 天。
- 明文只在签发成功响应中出现一次。
- 列表只返回名称、前缀、Scope、Audience、过期、最后使用和撤销状态。
- 撤销接口使用 Credential ID + Subject 双条件，阻止跨用户 IDOR 撤销。

Audience 当前精确绑定 `{site.url}/mcp`。站点从 HTTP 切换到 HTTPS、变更域名或
路径后，旧 Token 不应继续使用，必须重新签发。

## Scope AND RBAC

授权器首先检查 Credential Scope，再检查当前 Core Role：

```text
principal.HasScope(requirement.Scope)
AND rbac.Can(principal.Role, requirement.Resource, requirement.Action)
```

如果 Tool 声明 `OwnAction`，只有 `ResourceOwnerID == SubjectID` 时才允许使用
`update_own` / `delete_own` 等能力。全局 action 和 own action 都不满足时拒绝。

通配 Scope `*`、`gopress:*` 和命名空间 `:*` 在 Core 可解析，但官方 MCP
凭证页面只展示当前允许的明确 Scope，避免管理员日常签发宽泛权限。

## Tool 可见性不等于执行授权

`tools/list` 使用基础 Permission 做预过滤。需要实际参数才能确定 Owner 的 Tool
可以在拥有 `update_own`/`delete_own` 时显示，但执行时仍必须运行
`ResolvePermission`，加载真实资源并重新授权。客户端看到 Tool 从来不代表对
任意 ID 都有权限。

## Risk Policy

Core Policy 是站点级上限：

- `read_only`：所有 read Tool 允许继续授权，所有 write Tool 拒绝。
- `safe_write`：只有明确列入 `enabled_write_tools` 的写 Tool才允许继续。

Policy 与 Credential Scope 独立。开启 Tool 不会修改旧 Token，签发 Scope 也
不会开启 Tool。Registry 或 Policy Revision 变化会改变权限化 Tool 列表 Revision。

## ContentType、ID 与所有权

所有内容调用同时传 `content_type` 和 `id`，查询使用两者共同约束。目标 ID 存在
但属于另一类型时，对外仍返回 Not Found，避免跨类型枚举。

更新与回收先解析真实 `AuthorID`，媒体更新解析真实 `UploadedBy`。客户端传入的
Owner 字段不在 Schema 中，不能伪造。Parent 也必须存在且属于同一 ContentType。

## 幂等

所有写 Tool 都必须包含 8–200 字符的 `idempotency_key`。唯一键为：

```text
credential_id + tool_name + idempotency_key
```

`IdempotencyStore` 对 JSON 参数做规范化摘要，状态机为：

```text
new -> in_progress -> completed
                   -> failed
```

- 相同键、相同参数、已完成：重放保存结果，不再次调用 Handler。
- 相同键、相同参数、执行中：返回 `idempotency_pending`。
- 相同键、相同参数、已失败：重放稳定错误。
- 相同键、不同参数：返回 `conflict`。
- 记录默认 24 小时过期；过期后可重新占用。

幂等防止网络重试重复产生副作用，但不能替代并发版本检查。

## 乐观锁

更新、发布、回收、恢复和媒体元数据更新要求 `expected_updated_at`。Core 在事务
中同时检查持久化时间戳和带时间戳的 UPDATE 条件；任一不匹配都返回 conflict。

正确客户端流程是：

1. 读取资源与最新 `updated_at`。
2. 基于最新内容做决策。
3. 使用一个新的幂等键提交写入。
4. conflict 时重新读取，不自动覆盖。

## 显式确认

`publish` 和 `move_to_trash` 声明 `RequiresConfirmation`。Executor 在 Handler 前
要求参数中存在 `confirm: true`。MCP annotation、自然语言中的“我确认”或客户端
声称用户已批准都不能替代这个服务端字段。

## 内容与 Prompt Injection

站点正文、标题、Meta、媒体 Caption 都是不可信数据。Agent 可能在读取内容时
看到 Prompt Injection，但这不应影响服务端权限：

- Tool 描述和内容正文不会改变 Scope、RBAC 或 Policy。
- HTML 仍通过 Core sanitizer。
- Tool 不能执行 Shell、SQL、Go 函数或模板代码。
- 不提供用户、角色、插件、主题、任意 option、硬删除、支付或退款 Tool。

客户端仍应把站点内容视为不可信上下文，并在高风险工作流中要求人工确认。

## 审计

Agent Audit 状态包括 `started`、`succeeded`、`denied`、`failed`、`replayed`。
事件记录：

- Request ID 与受限 Trace ID。
- Adapter、协议、客户端版本、User-Agent。
- Principal Kind、Subject、Username、Credential ID。
- Tool 名、Owner、Risk、状态、稳定错误码与耗时。
- 参数字节数、SHA-256 摘要和顶层字段名。
- 结果摘要、带运行时密钥的来源摘要。

不记录 Bearer Token、参数值、Content 正文或数据库 Cause。Audit Store 不可用
时 Executor 返回 `audit_unavailable` 并失败关闭。

## HTTP 安全

当前 `/mcp` 具备：

- 只接受专用 Bearer Credential，不接受后台 Cookie/JWT 或 REST API Key。
- 非初始化请求强制受支持的 `Mcp-Protocol-Version`。
- HTTP Body 最大 256 KiB；Tool 参数另有 64 KiB 限制。
- Go 标准库 Cross-Origin Protection。
- `Cache-Control: private, no-store`、`Pragma: no-cache` 与 `nosniff`。
- 按来源每分钟 120 次基础限流，Bucket 数量有界。
- 无状态 Streamable HTTP、请求取消传播、Tool 超时和并发门控。
- 非 loopback Endpoint 应使用 HTTPS。

反向代理必须保留 Authorization 与 `Mcp-*` Header，且不能把 `/mcp` 放入共享
缓存。代理可信地址处理、全局 WAF、DDoS 防护、Secret 管理和 TLS 仍属于部署方
责任。

## 当前未完成的生产边界

Phase 4 前仍没有 OAuth 2.1 Protected Resource Metadata、Authorization Code +
PKCE、Refresh Token 轮换、Scope Step-up、完整 OpenTelemetry 与自动审计保留
策略。当前 Token 需要管理员人工签发和复制，因此文档与 UI 必须持续标记 Beta。
