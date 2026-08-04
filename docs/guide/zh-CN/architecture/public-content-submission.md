# 前台用户内容提交

GoPress 提供通用的 Core 服务，让已登录前台用户能够通过主题自有界面创建和维护内容。它适用于问题、社区主题、信息条目等用户生成内容，但不会把这些具体业务概念硬编码进 Core。

## 架构边界

职责划分如下：

- **Core**：负责内容模型、策略判断、账号状态复核、RBAC、所有权校验、状态赋值、输入长度限制、Slug 唯一性、内容清理、限流和持久化。
- **主题**：负责路由、表单、模板、内容类型专属校验，以及根据可信站点策略决定是否允许立即发布。
- **身份插件**：只负责认证用户；提交工作流不得 import 或探测具体身份 Provider。

启用策略后 Core 不会自动创建前台路由或 UI。主题必须通过声明式配置选择启用，再使用 Core 的通用服务完成写操作。

## 声明提交策略

在主题自定义内容类型下添加嵌套配置：

```toml
[[content_types]]
name = "question"
label = "问题"
label_plural = "问题列表"
supports = ["title", "content", "excerpt", "comments"]
has_archive = true
rewrite_slug = "questions"

[content_types.public_submission]
enabled = true
roles = ["subscriber", "contributor"]
default_status = "pending"
allow_update_own = true
allow_delete_own = true
```

角色列表默认拒绝：列表为空时任何角色都不能提交。`default_status` 支持 `draft`，其他值会安全回退到 `pending`。启用这项策略不会自动授予全局编辑或审核权限。

主题激活期间，Core 会把声明转换为临时的内容类型能力：

- `<type>.create`
- `<type>.read_own`
- `allow_update_own` 开启时的 `<type>.update_own`
- `allow_delete_own` 开启时的 `<type>.delete_own`

切换主题时只撤销当前主题产生的授权；激活主题前已经存在的 RBAC 能力会被保留。

## 主题接入

通过可选的 `theme.PublicSubmissionApp` 契约获取服务：

```go
host, ok := app.(theme.PublicSubmissionApp)
if !ok || host.PublicSubmissionService() == nil {
    return
}
submissions := host.PublicSubmissionService()
```

创建、更新和移入回收站时必须使用当前认证账号 ID，不能信任浏览器提交的作者字段：

```go
item, err := submissions.CreateOwn(c, content.PublicSubmissionInput{
    ContentType: "question",
    UserID:      account.ID,
    Title:       c.PostForm("title"),
    Content:     c.PostForm("content"),
    Excerpt:     c.PostForm("excerpt"),
})
```

服务提供 `CreateOwn`、`UpdateOwn` 和 `TrashOwn`。主题自有的活动列表或个人内容页还必须单独检查 `<type>.read_own`，并把查询强制限定到当前认证作者 ID。

## 编辑状态

普通前台提交会根据声明策略进入 `pending` 或 `draft`。`PublishImmediately` 是可信服务端策略输入：设为 true 时，Core 会保存为 `published` 并设置 `PublishedAt`。

不得把 `PublishImmediately` 直接绑定到表单或 JSON 字段。主题只有在读取服务端审核设置或完成其他可信授权判断后才能设置它；不能让用户通过自定义复选框绕过审核。

后台通用编辑器支持 `published`、`pending`、`draft` 和 `archived`，会忽略伪造的状态值；普通编辑表单也不能直接把内容改成 `trash`。

## 校验与滥用限制

即使绕过浏览器校验，Core 仍会强制执行：

- 当前前台 Session 必须与 `UserID` 一致，并实时确认数据库账号仍处于启用状态；
- 内容类型必须启用策略、明确允许当前角色，并具备对应 RBAC action；
- 更新和删除必须同时匹配内容类型与所有者；访问他人资源统一返回未找到，降低资源枚举风险；
- 标题和正文必填；标题最多 240 个字符、正文 100,000 个字符、摘要 1,000 个字符；
- Slug 只保留 Unicode 字母和数字，最多 120 个字符；唯一性检查不继承请求 Content Scope，避免产生重复 canonical slug；
- 每个用户、每种内容类型每分钟最多创建 3 条、滚动 24 小时最多 20 条；移入回收站的数据仍计入限流；
- 内容通过标准的安全内容仓储路径清理并保存。

主题可以增加更严格的业务校验，但不能削弱这些 Core 规则。

## 路由安全检查清单

每个主题自有提交路由都必须：

1. 要求有效前台 Session，并检查明确的 `<type>.<action>` 能力；
2. 从请求上下文取得当前用户，不能信任 URL、表单或 JSON 里的用户 ID；
3. 所有改状态请求执行同源校验；
4. 写操作调用 Core 服务，不能绕过服务直接保存内容；
5. 对资源 ID 校验类型和所有权，且不能泄露他人资源是否存在；
6. 个人活动、草稿和审核队列返回 `Cache-Control: private, no-store`；
7. 测试覆盖匿名用户和无权限角色被拒绝，以及所有者成功执行。

内容类型同时支持评论时，评论创建和审核仍使用独立契约，详见[评论与回复](comments.md)。
