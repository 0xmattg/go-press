# 配置文件

GoPress 使用 TOML 作为配置格式。每个站点一份独立的 `config.toml`，多站点可共享数据库（通过表前缀隔离）。

## 配置发现顺序

```text
-config <path>                  显式参数，优先级最高
sites/{first-host}/config.toml  自动发现的第一个站点配置
sites/default/config.toml       最终回退路径
```

`config/config.toml` 只是随源码提供的模板，不参与运行时回退；其中的
`jwt_secret` 有意留空。Web 安装器会把实际配置写入
`sites/<host>/config.toml`，文件权限为 `0600`。

## 完整示例

```toml
[site]
name = "My Website"
url = "https://example.com"
language = "zh"
timezone = "Asia/Shanghai"
theme = "mono-journal"

[server]
host = "0.0.0.0"
port = 8080
mode = "release"             # 生产用 release；本地调试可用 debug
# trusted_proxies = ["127.0.0.1", "::1"]  # 可信反向代理；留空即只信任本机

[pg]
user = "postgres"
password = "postgres"
hostname = "localhost"
port = "5432"
database = "my_website"
schema = "public"
table_prefix = "gp_"         # 表前缀（类 WordPress wp_）
max_open_conns = 20
max_idle_conns = 10
conn_max_lifetime = "30m"

[redis]
host = "localhost"
port = 6379
password = ""
db = 0
# namespace = "my-site"      # 可选：多个站点共用同一 Redis DB 时的缓存命名空间

[cms]
jwt_secret = ""              # 必填：安装器自动生成；留空或用示例占位值时应用拒绝启动
jwt_expire_hours = 24
upload_dir = "uploads"
upload_max_size_mb = 10      # 单文件上传上限；JPEG/PNG 上传后会自动生成响应式变体

[mail]
driver = "go-mail"
enabled = false
host = "smtp.example.com"
port = 587
encryption = "starttls"      # starttls / ssl / none
username = "smtp-user"
mail_key = "smtp-password-or-app-key"
from_email = "no-reply@example.com"
from_name = "My Website"
reply_to = ""
timeout_seconds = 10

[install]
completed = true
require_token = false        # true：Web 安装器要求输入一次性安装令牌

# [market]                   # 可选，见下文 [market]
# lock_developer_mode = true
```

## 关键字段说明

### `[site]`

- `name` / `url` — 给 SEOBuilder 的静态 baseline；admin「系统设置 > 网站设置」中的 `site_name` / `site_description` 会在运行时覆盖渲染层（详见 [SEO 接入规范](../themes/seo-integration.md)）
- `language` — 默认语言代码（如 `zh`、`en`），影响 i18n 默认 fallback 和多语言插件的默认语言
- `timezone` — 站点时区，使用 IANA 时区名（如 `Asia/Shanghai`、`America/New_York`）或 `Local`。后台发布时间输入会按该时区解析后以 UTC 存储，前台和后台展示再转回该时区。老站点没有该字段时会兼容回退到服务器本地时区；建议在「系统设置 > 网站设置」里保存一次明确值。
- `theme` — 启动时激活的主题 slug

### `[server]`

- `host` / `port` — HTTP 监听地址。前面有同机反向代理（如 Nginx）时，建议 `host = "127.0.0.1"`，只让反向代理访问应用端口
- `mode` — 生产用 `release`；`debug` 会输出详细的 Gin 日志（包括每次路由重建时的路由表），只适合本地调试
- `trusted_proxies` — 可信反向代理的 IP 或 CIDR 列表。只有来自这些地址的请求，其 `X-Forwarded-For` / `X-Real-IP` 才会被用来识别客户端 IP；其他请求一律以 TCP 连接对端地址为准。**留空时只信任本机回环（`127.0.0.1`、`::1`）**，适合反向代理与 GoPress 在同一台机器上的部署。非法条目会被记录错误并回退为只信任本机，不会放开伪造

客户端 IP 用于后台登录限流、REST API 限流、身份插件限流、审计日志和访问统计。如果信任了不该信任的来源，客户端就能伪造 IP 绕过这些限制，所以：

- 反向代理与应用同机：保持默认即可。Nginx 建议用连接地址覆盖转发头，而不是追加：

  ```nginx
  proxy_set_header X-Real-IP $remote_addr;
  proxy_set_header X-Forwarded-For $remote_addr;
  ```

- 反向代理在其他机器上：把它的内网地址写进 `trusted_proxies`，例如 `["10.0.0.5"]`
- 前面还有 CDN：除了本机代理，还要把 CDN 回源网段写进 `trusted_proxies`，GoPress 会沿 `X-Forwarded-For` 从右往左跳过可信代理，取第一个不可信的地址作为客户端 IP

### `[pg]`

- `table_prefix` — 类似 WordPress 的 `wp_`，**多个 GoPress 实例可共享同一数据库**，靠前缀隔离。详见 [数据库表前缀](../reference/database-prefix.md)
- 连接池参数 (`max_open_conns` 等) 影响并发性能上限，生产环境建议根据 PG 实例规格调整

### `[redis]`

可选段。完全删除或留空，引擎自动降级为纯内存 LRU（L1 缓存）。

- `namespace` — 可选。所有 Redis 缓存 Key 都以 `gopress:<namespace>:` 开头，后台「清理缓存」、整页缓存失效等操作也只删除本命名空间下的 Key。留空时 core 会根据 `[pg]` 的 `hostname`、`port`、`database`、`schema`、`table_prefix` 派生一个稳定的短哈希（形如 `s1a2b3c4d5e6f`），因此只要站点数据不同，命名空间就不同。显式填写时只保留字母、数字、`.`、`-`、`_`，最长 64 个字符
- 多个站点共用同一个 Redis DB 时，同路径、同语言的页面互不串用，一个站点清理缓存也不会影响其他站点。若希望命名空间不随数据库连接参数变化（例如迁移数据库主机），请显式设置 `namespace`

### `[cms]`

- `jwt_secret` — 后台会话与 API Bearer Token 的签名密钥，**每个站点必须是唯一的随机值**，泄露后等于把后台和 API 钥匙交给攻击者。安装器会自动生成；**为空或仍是示例占位值（`go-press-change-this-secret-in-production`）时应用会拒绝启动**，可用 `openssl rand -base64 32` 手动生成
- `jwt_expire_hours` — 后台/ API Token 有效期（小时）。注意：被禁用或降级的账号无需等待过期，下一次请求即失效（服务端会按数据库实时状态复核）
- `upload_dir` — 上传根目录，子目录按年月组织（`uploads/2026/04/...`）

## 安全默认行为

以下防护由 core 默认启用，无需额外配置：

- **后台会话 Cookie** — `HttpOnly` + `SameSite=Lax`，且当 `[site].url` 为 `https://` 时自动加 `Secure`（HTTPS 部署不会再明文传输后台 Token）。因此请把生产站点的 `url` 配成 `https://…`
- **CSRF 同源校验** — 后台所有改状态请求（含登录、插件后台路由）校验 `Origin`/`Referer` 与站点同源，跨站请求被拒（`403`）
- **登录限流** — 后台登录按来源 IP 限制失败次数（默认 5 分钟内 10 次失败即临时拦截，返回 `429`），失败尝试写入审计日志
- **客户端 IP 防伪造** — 只信任 `[server].trusted_proxies`（默认仅本机回环）转发的 `X-Forwarded-For` / `X-Real-IP`，直接访问的客户端无法通过伪造请求头冒充其他 IP 绕过上述限流
- **账号状态实时生效** — 禁用账号或调整角色后，其已签发的后台/API Token 立即失效或按新角色鉴权，无需等到 Token 过期
- **前台 Session 可即时撤销** — 前台用户 Session 存在数据库中且每次请求复核账号状态；停用账号后，下一次请求会撤销数据库 Session 并清除浏览器 Cookie
- **上传文件防 XSS** — 上传目录中 `svg`/`html`/`xml` 等可执行文档在返回时强制 `Content-Disposition: attachment` + CSP `sandbox`，避免被当作页面在站点域下执行脚本；通过 `<img>` 内联引用的图片不受影响，照常显示

### `[mail]`

邮件发送配置是站点级配置，后台「邮件设置」页会写入当前站点的 `config.toml`。配置文件由安装器和 `config.Save()` 以 `0600` 权限保存，`mail_key` 不会在后台表单中回显。

- `enabled` — SMTP 总开关。关闭时通知规则仍保存，但不会投递邮件
- `driver` — SMTP 发信驱动，默认 `go-mail`；可切换为 `stdlib` 使用 Go 标准库 SMTP 分支
- `host` / `port` / `encryption` — SMTP 服务器、端口和加密方式。`encryption` 支持 `starttls`、`ssl`、`none`
- `username` / `mail_key` — SMTP 登录凭据；`mail_key` 建议填写邮箱服务商提供的 app password 或 API key
- `from_email` / `from_name` — 默认发件人
- `reply_to` — 默认 Reply-To。联系留言通知会优先使用留言人的邮箱作为 Reply-To
- `timeout_seconds` — SMTP 连接超时

Gmail 常用配置：`host = "smtp.gmail.com"`、`port = 587`、`encryption = "starttls"`，`username` 和 `from_email` 都填写 Gmail 地址，`mail_key` 填 Google 生成的 App Password，不要填 Google 账号登录密码。

### `[install]`

- `completed` — 由 Web 安装器在完成时写入；为 `false` 时服务运行安装器而不是站点
- `require_token` — 设为 `true` 时，安装器第一屏要求输入一次性安装令牌。令牌在安装器启动时打印到服务器日志，并保存为 `config.toml` 旁边的 `install-token` 文件（权限 `0600`），安装完成后自动删除。默认关闭，需要在首次启动前创建的配置里打开。见[不要让安装器暴露在公网](installation.md#不要让安装器暴露在公网)

### `[market]`

可选段。不配置时，站点使用官方主题市场 gopress.xyz，信任的签名公钥内置在 GoPress 里（0.7.40 起）。

站点只在这些时候联系市场：管理员打开后台「主题 → 主题市场」时拉取目录；安装或升级时下载主题包；更新检查开关打开时，大约每天一次询问从市场安装的主题有没有新版本。请求带 `User-Agent: GoPress/<版本>`，不带安装 ID；更新检查只发送每个主题的标识和已安装版本。不打开市场页、也没开更新检查的站点不会联系市场。

配置了下面三项，站点就整体改用自建市场，不再使用官方市场。

```toml
[market]
# 用自建市场代替官方市场：三项要么都配，要么都不配
endpoint     = "https://market.example.com/api/market/v1"
catalog_keys = ["<base64 编码的 Ed25519 公钥>"]
package_keys = ["<base64 编码的 Ed25519 公钥>"]

# 不论后台怎么设置，开发者模式始终关闭
lock_developer_mode = true
```

- `endpoint`、`catalog_keys`、`package_keys` — 让站点改用自建市场，整体替换官方市场，所以三项必须一起配；只配了一部分时服务拒绝启动。`catalog_keys` 校验市场目录的签名，`package_keys` 校验市场对每个主题包的加签；每组最多 4 把公钥，同一把公钥不能同时出现在两组里。端点必须用 HTTPS（只有本机回环地址可以用 HTTP，方便本地测试）
- `lock_developer_mode` — 运维锁。开发者模式始终关闭：后台打不开，之前打开过的也视为关闭。生产站建议开启，这样即使后台会话被盗，也装不了没有市场签名的包。只配它不算配置了自建市场

自建市场需要提供：`<endpoint>/catalog?schema=1` 返回用 `gopress pkg catalog sign` 签名的目录；每个主题包放在目录条目的 `download_url`。下载只用 HTTPS、不跟随重定向，并且大小必须和目录里记录的一致，所以地址要直接指向文件本身。详见[主题包](../themes/theme-packages.md#安装和管理)；目录格式和加签见[发布到 GoPress 市场](../themes/publishing.md#自建市场)。

自建市场还要在 `POST <endpoint>/updates/check` 回答更新检查。后台的更新检查开关（系统设置 →「官方更新检查与 Beta 使用情况反馈」）打开时，站点会把从这个市场安装、带市场签名的主题发过去，不带安装 ID：

```json
{
  "protocol_version": 1,
  "targets": [
    { "kind": "theme", "slug": "aurora", "current_version": "1.2.0" }
  ]
}
```

市场返回有新版本的主题：

```json
{
  "protocol_version": 1,
  "ttl_seconds": 86400,
  "updates": [
    {
      "kind": "theme",
      "slug": "aurora",
      "latest_version": "1.3.0",
      "severity": "security",
      "release_url": "https://market.example.com/themes/aurora/1.3.0"
    }
  ]
}
```

- `severity` 是 `security`、`unsupported`（已安装的版本不再受支持），普通更新不填。`release_url` 可选，必须是 HTTPS。
- 站点没有问到的主题、版本号不合法的条目会被忽略。`ttl_seconds` 是这次结果的有效期，范围 6 小时到 7 天。
- 返回结果只用来显示更新提示，升级仍然要经过签名目录。市场没有实现这个接口时不显示提示，站点的其他更新检查照常进行。

## 多站点配置

`sites/` 下每个子目录代表一个站点：

```
sites/
├── localhost/
│   ├── config.toml
│   └── public/              # 站点级生成物，如 sitemap.xml
├── staging.gopress.xyz/config.toml
└── prod.gopress.xyz/config.toml
```

启动时通过 `-config <path>` 指定具体哪个生效。多站点可指向同一 PG 实例不同 `database` 或同一 `database` 不同 `table_prefix`。多个站点也可以共用同一个 Redis DB，缓存按 `[redis].namespace`（未配置时自动派生）隔离。

已安装的主题包保存在站点目录的 `packages/archives/` 下（原始的 `.zip` 文件）。备份时要把每个站点的 `packages/` 目录和数据库一起备份：主题包是文件，数据库导出里没有它们。从市场安装的包，只要市场还提供这个版本，就能重新安装；手动上传的包只有本站这一份。

每个站点只运行一个 GoPress 进程。主题包的安装和选项、菜单等其他运行中的改动一样，目前不会在多个进程之间同步。

后台生成的 `sitemap.xml` 会写入当前站点目录下的 `public/`，例如 `sites/prod.gopress.xyz/public/sitemap.xml`。后续 `robots.txt`、`llms.txt` 等站点级公开生成物也应放在同一目录，避免多个站点共享应用根目录时互相覆盖。
