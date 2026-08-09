# Agent Tool 扩展开发

Core 已允许业务插件通过 `plugin.AgentHost` 向通用 Registry 注册 Tool。MCP
Adapter 不枚举插件 slug，也不调用插件专用接口；它只读取 Registry Snapshot。

这个扩展点当前属于**底层契约已实现、产品化治理未完成**的状态。只读 Tool 可以
在严格复用现有 Scope/RBAC 语义时接入；自定义 Scope、第三方写 Tool 的后台
审批和凭证签发仍属于 Phase 5。

## 公共 Host

```go
type AgentHost interface {
    AgentToolRegistry() *agent.Registry
    AgentExecutor() *agent.Executor
}
```

插件在 `Activate` 中 type assert 自己需要的最小 Host，并在 `Deactivate` 中撤销
所有 Handle。不要 type assert 整个 `*core.Engine` 后访问未声明内部字段。

## 注册只读 Tool

下面示例只展示结构。`Permission` 必须映射到真实存在且语义一致的 Scope/RBAC，
不能为了让 Tool 出现在列表中借用不相关权限：

```go
type Plugin struct {
    agentHandles []*agent.Handle
}

func (p *Plugin) Activate(app plugin.App) {
    host, ok := app.(plugin.AgentHost)
    if !ok || host.AgentToolRegistry() == nil {
        return
    }

    handle, err := host.AgentToolRegistry().Register("example-reports", agent.Tool{
        Name:         "example.reports.summary",
        Title:        "Get report summary",
        Description:  "Return a bounded summary without private row data.",
        InputSchema:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
        OutputSchema: json.RawMessage(`{"type":"object","required":["count"],"properties":{"count":{"type":"integer","minimum":0}},"additionalProperties":false}`),
        Mutability:   agent.MutabilityRead,
        Risk:         agent.RiskRead,
        Permission: agent.PermissionRequirement{
            Scope: "gopress:site:read",
            Resource: "dashboard",
            Action: "read",
        },
        Timeout:        5 * time.Second,
        MaxConcurrency: 4,
        Handler: func(ctx context.Context, invocation agent.Invocation) (any, error) {
            return map[string]any{"count": 0}, nil
        },
    })
    if err == nil {
        p.agentHandles = append(p.agentHandles, handle)
    }
}

func (p *Plugin) Deactivate(app plugin.App) {
    for _, handle := range p.agentHandles {
        handle.Revoke()
    }
    p.agentHandles = nil
}
```

上例只有当“报告摘要”确实等价于安全 Dashboard 读取时才合理。插件专有业务数据
通常应拥有自己的 Scope 和 RBAC resource；当前官方凭证 UI 尚不能签发任意第三
方 Scope，因此正式上线前应等待/实现统一治理契约，而不是降级复用宽泛 Core
权限或另开无审计路由。

## Handler 规则

- 只接收 `context.Context` 与 `agent.Invocation`，不接收 Gin Context 或 MCP
  Request。
- 使用插件自己的服务/仓储，但所有查询必须携带传入 Context。
- 不相信参数中的用户、角色、Owner、站点或租户字段；从 Invocation Principal
  与服务端资源解析。
- 列表必须有分页和最大页长；输出只包含任务所需字段。
- 不返回数据库模型中未审查的 Secret、路径或内部 option。
- 尊重 Context 取消，不启动无法追踪的无限 goroutine。
- 领域失败返回稳定 `agent.Error`，内部 Cause 不进入公开消息。

## PermissionResolver 与所有权

当权限取决于参数中的资源 ID 时，Tool 必须提供 `ResolvePermission`：

```go
ResolvePermission: func(ctx context.Context, principal agent.Principal, raw json.RawMessage) (agent.PermissionRequirement, error) {
    var input struct {
        ID uint `json:"id"`
    }
    if json.Unmarshal(raw, &input) != nil || input.ID == 0 {
        return agent.PermissionRequirement{}, agent.NewError(agent.CodeInvalidArguments, "valid id required")
    }
    row, err := repo.FindByIDContext(ctx, input.ID)
    if err != nil {
        return agent.PermissionRequirement{}, agent.NewError(agent.CodeNotFound, "resource not found")
    }
    return agent.PermissionRequirement{
        Scope: "example:records:write",
        Resource: "example_record",
        Action: "update",
        OwnAction: "update_own",
        ResourceOwnerID: row.OwnerID,
    }, nil
}
```

Resolver 只解析授权所需事实，不执行写操作。即便当前 Principal 没权限，资源不存在
与跨类型资源也应返回一致的 Not Found，避免 IDOR 枚举。

## 写 Tool 要求

Registry 会拒绝 `MutabilityWrite` 但 `Idempotent == false` 的 Tool。写扩展至少要：

- 定义独立、最小 Scope 与 RBAC action。
- 使用明确 Risk；发布与破坏操作拆分，不混入 update。
- 在 Input Schema 要求 `idempotency_key`。
- 对资源更新要求版本号或 `expected_updated_at`。
- 高风险操作设置 `RequiresConfirmation`。
- 事务内验证资源类型、Owner 和并发版本。
- 成功时使用 `ResultForResource` 关联持久化资源。
- 触发自己的稳定 Hook/缓存失效，而不是由 MCP 插件补业务副作用。

但是当前 `gopress-mcp` 管理页的 `safe_write` 白名单只接受六个 Core 写 Tool，写
Scope 页面也只从这些 Tool 推导。因此第三方写 Tool 即使注册成功，仍会被默认
Risk Policy 拒绝，不能通过当前 UI 正式开放。这是刻意的安全门槛，不应绕过。

## Schema 设计建议

- `additionalProperties: false`，拒绝模型幻觉字段。
- 为字符串、数组、数字设置合理上限。
- 输入使用 ID + 类型，而不是只用可猜测 ID。
- 输出 Schema 不直接引用大型数据库 DTO。
- Tool 名使用插件稳定命名空间，避免 `gopress.*` Core 保留前缀。
- Description 说明目标和边界，不在文案中承诺客户端确认即可越权。

## 生命周期与冲突

- Tool 名已存在时 Registry 返回 `ErrToolAlreadyRegistered`。
- 只保存本次注册返回的 Handle；不要在停用时按名字删除其他 Owner 的 Tool。
- 注册多个 Tool 过程中任一失败，应撤销之前已经成功的 Handle，保持原子装配。
- 插件停用后 Router、设置或 Worker 的清理与 Tool Handle 清理应一起完成。
- Registry Revision 会自动递增，客户端的私有 Tool 缓存最多在 TTL 后更新。

## 测试清单

第三方 Tool 至少覆盖：

- 无 Scope 拒绝、无 RBAC 拒绝、有权限成功。
- `update_own`/`delete_own` 对本人成功、他人拒绝。
- ID 与类型不匹配返回 Not Found。
- 未知字段、过大输入、过深 JSON 与无效输出被拒绝。
- 写幂等并发只执行一次；相同键不同参数冲突。
- 乐观锁拒绝陈旧值。
- Handler panic、超时和取消被包装并审计。
- Deactivate 后 Tool 从 Registry 消失，旧 Handle 不影响后续注册。

Phase 5 应在开放第三方写 Tool 前补齐 Scope 注册表、后台授权审核、Credential
Scope 动态发现、写 Tool 策略 Provider、风险分级和兼容性测试流程。
