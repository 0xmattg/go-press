# 评论与回复

评论属于 core 的通用 CMS 领域，并使用独立数据表，不复用 `Content`。因此评论能跨主题保留，也不会混入内容归档、REST 内容查询、分类关系或多语言内容复制流程。

## 数据与回复层级

`core/comment.Comment` 关联一条内容和一个已登录用户；可选的 `ParentID` 指向父评论。当前策略支持“顶层评论 + 一级直接回复”，继续回复一级回复会被服务端拒绝。数据结构本身可在未来放宽层级，而无需迁移表结构。

评论状态包括 `pending`、`approved`、`spam`、`trash`。新评论默认待审核；可信服务端审核策略也可以把 `CreateInput.InitialStatus` 设为 `approved`，Core 会拒绝 `pending` / `approved` 之外的任何初始值。该字段绝不能直接绑定浏览器输入。匿名访问者只能看到已批准评论；登录用户还能看到自己待审核的评论。

## 为内容类型启用评论

内容类型通过 Registry 的通用能力声明启用评论：

```go
Supports: []string{"title", "content", "comments"}
```

后台内容编辑器随后显示单条内容的 `CommentStatus`（`open` / `closed`）开关。`BaseTheme.renderSingle` 会注入：

| 模板字段 | 含义 |
|---|---|
| `.Comments` | 已批准评论，以及当前用户自己的待审核评论 |
| `.CommentCount` | 已公开评论数量 |
| `.CommentsOpen` | 内容类型支持评论且当前内容开放评论 |
| `.CanComment` | 当前用户拥有 `comment.create` |

主题只负责 HTML、CSS 和交互，通过可选的 `theme.CommentApp` 访问评论服务，通过 `theme.PublicAuthorizationApp` 检查权限；不能直接查询评论表，也不能识别具体身份插件。

## 登录与 RBAC

发表评论必须同时拥有 core 前台会话和 `comment.create`。默认 subscriber、contributor、author、editor 均拥有该能力。审核需要 `comment.moderate`，默认仅 editor 和 super admin 拥有。

主题可以在 `theme.toml` 声明所需身份插件，但运行时代码仍只能使用 `currentUser`、`isLoggedIn`、`loginURL` 和通用授权接口。

所有评论 POST 都必须执行同源检查、目标内容及 Registry 能力检查、父评论同内容归属检查、回复深度检查、正文长度校验和服务端限流。不能依赖前端隐藏按钮作为授权措施。

## 审核策略与内容所有者审核

Core 保留“默认待审核”的安全行为，同时允许主题根据服务端保存的设置决定是否立即批准。主题负责把可信设置转换成 `InitialStatus`；评论作者提交的表单字段或 JSON 属性不具备决定权。

`CommentService.ListVisibleForReview(contentID, viewerID)` 会返回已批准评论及其待审核队列。这个方法有意不判断谁能够审核目标，因此调用它或修改返回评论前，路由必须证明满足以下任一条件：

- 当前账号是目标内容所有者，并拥有该内容类型的 `update_own` 能力；
- 当前账号拥有全局 `comment.moderate` 权限。

评论 ID 还必须校验属于预期内容，并检查正确的父子关系。内容所有者的审核范围应仅限自己内容下的回复；全局审核员继续负责垃圾评论、回收站、下架和异常场景。无论页面是否隐藏按钮，未授权或跨内容 ID 都必须由服务端拒绝。

## Profile 页面

主题可使用 `user.PublicUserView` 渲染当前账号资料。应使用 `/profile` 这类固定路径，不接受用户 ID；必须检查 `profile.read_own` 并返回 `Cache-Control: private, no-store`。不得暴露身份 Provider 原始数据、session token、密码哈希或其他用户的邮箱。

## 缓存与 Hook

core 会触发 `comment.created` 和 `comment.status_changed`。评论进入或离开 `approved` 状态时会清理匿名页面缓存；仅涉及当前作者可见的待审核状态时无需清理公开缓存。
