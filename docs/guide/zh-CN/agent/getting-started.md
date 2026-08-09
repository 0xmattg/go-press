# Agent 与 MCP 快速上手

本章用于在本地或测试站点跑通当前 Phase 3 能力。生产站点在完成 OAuth 2.1、
代理安全复核与真实客户端兼容验证前，仍应把 MCP 视为 Beta 能力。

## 1. 前置条件

- GoPress 已完成安装和数据库迁移。
- `site.url` 是客户端实际访问的规范 URL。
- 生产环境使用 HTTPS；本机 HTTP 只使用 localhost/loopback。
- 你拥有插件管理和 MCP 设置所需的超级管理员权限。
- 使用支持远程 Streamable HTTP 的 MCP 客户端，或先用 curl 检查。

Agent Core 表会随 Core 迁移创建，不需要单独运行插件 SQL：

```text
{prefix}agent_service_accounts
{prefix}agent_credentials
{prefix}agent_idempotency_records
{prefix}agent_audit_events
```

## 2. 激活官方 MCP 插件

进入后台「插件管理」，激活 **GoPress MCP**。激活会通过
`routes.register` 把 `/mcp` 和受保护的插件后台接口加入当前 Router；停用插件
后 Router 重建，这些入口消失。

打开：

```text
/admin/plugins/gopress-mcp/settings
```

在「连接概览」确认：

- Endpoint 等于 `{site.url}/mcp`。
- Transport 为 `Streamable HTTP (stateless)`。
- 协议包含 `2026-07-28` 与 `2025-11-25`。
- HTTPS 诊断通过，或当前确实是本机 loopback 开发环境。

## 3. 签发只读 Credential

在「访问凭证」填写客户端名称，先保留默认 30 天有效期，并只选择实际需要的
只读 Scope：

```text
gopress:site:read
gopress:content:read
gopress:taxonomy:read
gopress:media:read
```

Token 以 `gp_agent_` 开头，只显示一次。立即保存到客户端 Secret Store 或临时
环境变量，不要写进仓库、截图或公开工单。

## 4. 配置 MCP 客户端

通用配置形态如下：

```json
{
  "mcpServers": {
    "gopress": {
      "type": "http",
      "url": "https://example.com/mcp",
      "headers": {
        "Authorization": "Bearer gp_agent_REPLACE_WITH_TOKEN"
      }
    }
  }
}
```

客户端可能使用不同的最外层配置结构，但不要修改 Endpoint 或 Bearer Header 的
语义。最新协议客户端直接协商 `2026-07-28`；仍使用初始化握手的客户端通过
`2025-11-25` 兼容通道连接。

## 5. 验证只读能力

连接成功后，依次执行：

1. `gopress.site.get`：确认站点 URL、语言、时区、Core 版本和 Agent Revision。
2. `gopress.content_types.list`：查看当前注册 ContentType 及其字段声明。
3. `gopress.content.list`：使用 `content_type: "post"` 查询已发布内容。
4. `gopress.content.get`：用列表返回的 `id` 与同一 `content_type` 读取详情。

如果 `tools/list` 只返回部分 Tool，先不要把它当作异常。Tool 列表按当前 Token
Scope 和用户 RBAC 过滤，并使用 30 秒私有缓存。无 `taxonomy.read` 或
`media.read` 的 Credential 不应看到对应 Tool。

## 6. 最小化开启 Safe Write

进入「写入策略」：

1. 把 Profile 从 `read_only` 改为 `safe_write`。
2. 只勾选 `gopress.content.create_draft`。
3. 保存后重新进入「访问凭证」。
4. 新签发一个只包含 `gopress:content:write` 的测试 Token。

已有只读 Token 不会自动获得写 Scope；这正是 Scope Step-up 尚未实现时的安全
替代方案。

创建草稿参数示例：

```json
{
  "content_type": "post",
  "title": "Agent 测试草稿",
  "content": "<p>仅用于测试。</p>",
  "idempotency_key": "draft-smoke-20260809-0001"
}
```

预期结果：

- 内容状态固定为 `draft`。
- 作者绑定 Token 所属用户，客户端不能伪造 `author_id`。
- 重复发送完全相同的 Tool、参数和幂等键时，返回已保存结果而不是再建一条。
- 参数中加入未声明字段会收到 `invalid_arguments`。
- 调用审计出现 `started` 与 `succeeded`，重放时出现 `replayed`。

## 7. 验证乐观锁与显式确认

开启 `gopress.content.update` 后，先用 `content.get` 读取最新 `updated_at`，再把
它原样作为 `expected_updated_at` 传给更新调用。一次成功更新后，继续使用旧
时间戳应得到 `conflict`，证明 Agent 不能覆盖后台或另一客户端的新编辑。

发布或移入回收站还必须同时满足：

- 对应写 Tool 已逐项启用。
- Credential 包含所需 Scope。
- 当前角色具有 `content.publish` 或 `content.delete`/`delete_own`。
- 参数包含最新 `expected_updated_at`、新的 `idempotency_key` 与
  `confirm: true`。

建议在测试环境完整验证后，仍只为生产 Agent 开放完成任务必需的 Tool。

## 8. 验证撤销

在「访问凭证」撤销测试 Token，然后再次调用任意 Tool。预期立即返回认证失败，
不需要等待 Token 原定过期时间。账号被停用或角色被降低时也应在下一次执行时
生效，因为 Executor 会重新加载当前 Principal。

## 下一步

- [分层架构](architecture.md)
- [Tool 与执行管线](tools-and-execution.md)
- [身份、授权与安全](security.md)
- [运维与测试](operations-and-testing.md)
- [完整插件操作与 curl](../plugins/gopress-mcp.md)
