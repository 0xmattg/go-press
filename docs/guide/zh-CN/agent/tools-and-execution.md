# Tool 模型与执行管线

Tool 是 Core Agent 层的最小可执行能力单位。它同时描述名称、Schema、风险、
权限、执行限制和 Handler，但不包含 MCP annotation 或 HTTP 类型。

## Tool 契约

| 字段 | 含义 |
|---|---|
| `Name` | 全局稳定名称，符合小写分段命名规则，例如 `gopress.content.get` |
| `Title` / `Description` | 给客户端和模型的目标说明，不承担授权语义 |
| `InputSchema` / `OutputSchema` | Core 验证的受限 JSON Schema |
| `Mutability` | `read` 或 `write` |
| `Risk` | `read`、`write`、`publish`、`destructive`、`critical` |
| `Idempotent` | 写 Tool 必须为 true |
| `RequiresConfirmation` | 高风险写调用是否必须带 `confirm: true` |
| `Permission` | 基础 Scope + `resource.action` + 可选 own action |
| `ResolvePermission` | 根据实际参数解析 ContentType、资源和 Owner |
| `Timeout` | Tool 执行超时 |
| `MaxConcurrency` | 单 Tool 进程内并发上限 |
| `Handler` | 只接收标准 Context 与已授权 Invocation 的领域入口 |

Registry 会拒绝无 Handler、重复名称、未知风险、非幂等写 Tool、非法权限或无效
Schema。只读 Tool 的风险必须是 `read`；要求确认的 Tool 必须是写 Tool。

## 受限 JSON Schema

Core 不使用任意递归 JSON Schema 功能，而只允许执行期所需关键词：

```text
type, properties, required, additionalProperties,
items, enum, minLength, maxLength, minimum, maximum,
minItems, maxItems, title, description, default, examples
```

对象必须显式声明 `additionalProperties`。默认限制为：

- Tool 参数 64 KiB。
- Tool 输出 1 MiB。
- JSON / Schema 深度 16。
- 单个 Tool Schema 64 KiB。

输入和输出都验证。即使 Handler 成功返回，如果结果不符合 Output Schema，
Executor 也会以 `invalid_result` 失败并审计，而不是把未声明结构发送给客户端。

## Registry

Registry 是并发安全、Owner-aware 的目录：

- Tool 名全局唯一。
- Snapshot 确定性排序，适合客户端缓存与测试。
- 注册返回精确 Handle，插件停用时调用 `Revoke()`。
- generation 防止旧 Handle 撤销后续同名 Tool。
- `RevokeOwner(owner)` 可清理一个 Owner 的全部 Tool。
- 对外 Snapshot 不包含可执行 Handler 或 PermissionResolver。

## Executor 固定顺序

Tool Handler 不能定制或跳过下面的包装顺序：

1. 校验 `request_id`、Tool 名与基础调用结构。
2. 通过 Credential Service 重新加载 Principal、当前角色和账号状态。
3. 从 Registry 获取 Tool；未知 Tool 返回 `not_found`。
4. 按 Input Schema、大小与深度验证参数。
5. 运行 `ResolvePermission`，绑定实际 ContentType、资源 ID 与 Owner。
6. 执行 Token Scope AND Core RBAC AND Ownership。
7. 执行站点 Tool Profile 与逐 Tool Policy。
8. 对发布/破坏性 Tool 检查 `confirm: true`。
9. 记录开始事件；审计不可用则失败关闭。
10. 写 Tool 从参数提取并占用 `idempotency_key`。
11. 获取 Tool 并发槽并创建带超时 Context。
12. 把刷新后的 Principal 放入 Context，调用 Handler。
13. 提取可选 `ResourceResult`，序列化并验证 Output Schema。
14. 完成幂等记录并写入资源引用和结果。
15. 记录 `succeeded`；重放、拒绝或错误写入对应审计状态。

Handler panic 会被恢复为安全的 `internal_error` 并审计。Context 超时与取消分别
转换为 `timeout` 和 `canceled`。

## 六个只读 Tool

| Tool | Scope / RBAC | 输入与边界 |
|---|---|---|
| `gopress.site.get` | `gopress:site:read` + `dashboard.read` | 无参数；只返回安全站点元数据、Core 版本与 Agent Revision |
| `gopress.content_types.list` | `gopress:content:read` + `content.read` | 无参数；返回注册类型、supports、taxonomies 与 Meta 字段声明 |
| `gopress.content.list` | `gopress:content:read` + `content.read` | 必须指定 `content_type`；状态、搜索、taxonomy/term 与分页均有界 |
| `gopress.content.get` | `gopress:content:read` + `content.read` | 必须同时指定 `content_type` 与 `id`；正文清理，Meta 白名单 |
| `gopress.taxonomy.list` | `gopress:taxonomy:read` + `taxonomy.read` | 只查询指定 ContentType 实际挂载的 taxonomy |
| `gopress.media.list` | `gopress:media:read` + `media.read` | MIME 与分页过滤；不暴露文件系统路径 |

内容列表状态只接受 `published`、`pending`、`draft`、`archived`、`trash`，默认
为 published。分页默认 20，最大 100。

## 六个写 Tool

| Tool | Scope / RBAC | 风险控制 |
|---|---|---|
| `gopress.content.create_draft` | `gopress:content:write` + `content.create` | 只能创建 draft；类型/字段/Meta/Parent/Slug/HTML 校验；幂等 |
| `gopress.content.update` | `gopress:content:write` + `content.update` 或 `update_own` | ContentType + ID + Owner；乐观锁；不能改状态 |
| `gopress.content.publish` | `gopress:content:publish` + `content.publish` | 独立发布命令；乐观锁、幂等、`confirm: true` |
| `gopress.content.move_to_trash` | `gopress:content:write` + `content.delete` 或 `delete_own` | 独立软删除；乐观锁、幂等、`confirm: true` |
| `gopress.content.restore` | `gopress:content:write` + `content.update` | 只把匹配类型的 trash 恢复为 draft；乐观锁、幂等 |
| `gopress.media.update_metadata` | `gopress:media:write` + `media.update` 或 `update_own` | 只修改 alt/title/caption；Owner、乐观锁、幂等 |

当前 Editor 有 `content.publish`，Super Admin 通过 `*.*` 获得；Author 没有发布
权限。Scope 只是收紧条件，不能把 Author 提升为 Editor。

## 动态 ContentType

内容 Tool 不硬编码主题业务类型。调用者先运行 `content_types.list`，再把返回的
类型名传给 `content.list/get/create_draft/update/...`。Core 会根据当前 Registry：

- 判断类型是否存在、是否只读、是否层级化。
- 判断 `content`、`excerpt`、`thumbnail` 等 supports。
- 只接受 ContentType 声明的 Meta 字段。
- 校验 Parent 是否属于同一类型。
- 限制 taxonomy 必须真实挂载到该类型。

这样主题可以声明业务内容模型，但 Tool 仍保持少量、稳定和主题无关。

## ResourceResult

写 Handler 可返回 `agent.ResultForResource(value, resourceType, resourceID)`。
Executor 只把 `value` 作为公共输出，同时把资源类型与 ID 写入幂等记录，方便
重试收敛和后续内部诊断。协议适配器不会看到领域 Handler 的特殊类型。

## 稳定错误码

Core Error Code 与传输无关，主要包括：

| 类别 | Code |
|---|---|
| 请求/Schema | `invalid_request`、`invalid_arguments`、`invalid_result` |
| 认证/授权 | `unauthenticated`、`insufficient_scope`、`permission_denied` |
| 资源/并发 | `not_found`、`conflict` |
| 风险控制 | `risk_denied`、`confirmation_required` |
| 幂等 | `idempotency_required`、`idempotency_pending` |
| 执行 | `timeout`、`canceled`、`audit_unavailable`、`internal_error` |

MCP Adapter 将它们包装为结构化 Tool Error。内部错误使用通用消息，Cause 仅留在
服务端诊断中。
