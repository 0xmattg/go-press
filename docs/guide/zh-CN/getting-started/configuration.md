# 配置文件

GoPress 使用 TOML 作为配置格式。每个站点一份独立的 `config.toml`，多站点可共享数据库（通过表前缀隔离）。

## 完整示例

```toml
[site]
name = "My Website"
url = "https://example.com"
language = "zh"
timezone = "Asia/Shanghai"
theme = "modern-company"

[server]
host = "0.0.0.0"
port = 8080
mode = "debug"               # debug / release

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

[cms]
jwt_secret = "your-secret-key"
jwt_expire_hours = 24
upload_dir = "uploads"
upload_max_size_mb = 10      # 单文件上传上限；JPEG/PNG 上传后会自动生成响应式变体

[install]
completed = true
```

## 关键字段说明

### `[site]`

- `name` / `url` — 给 SEOBuilder 的静态 baseline；admin「系统设置 > 网站设置」中的 `site_name` / `site_description` 会在运行时覆盖渲染层（详见 [SEO 接入规范](../themes/seo-integration.md)）
- `language` — 默认语言代码（如 `zh`、`en`），影响 i18n 默认 fallback 和多语言插件的默认语言
- `timezone` — 站点时区，使用 IANA 时区名（如 `Asia/Shanghai`、`America/New_York`）或 `Local`。后台发布时间输入会按该时区解析后以 UTC 存储，前台和后台展示再转回该时区。老站点没有该字段时会兼容回退到服务器本地时区；建议在「系统设置 > 网站设置」里保存一次明确值。
- `theme` — 启动时激活的主题 slug

### `[pg]`

- `table_prefix` — 类似 WordPress 的 `wp_`，**多个 GoPress 实例可共享同一数据库**，靠前缀隔离。详见 [数据库表前缀](../reference/database-prefix.md)
- 连接池参数 (`max_open_conns` 等) 影响并发性能上限，生产环境建议根据 PG 实例规格调整

### `[redis]`

可选段。完全删除或留空，引擎自动降级为纯内存 LRU（L1 缓存）。

### `[cms]`

- `jwt_secret` — **生产环境必须替换**，泄露后等于把后台和 API 钥匙交给攻击者
- `upload_dir` — 上传根目录，子目录按年月组织（`uploads/2026/04/...`）

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

启动时通过 `-config <path>` 指定具体哪个生效。多站点可指向同一 PG 实例不同 `database` 或同一 `database` 不同 `table_prefix`。

后台生成的 `sitemap.xml` 会写入当前站点目录下的 `public/`，例如 `sites/prod.gopress.xyz/public/sitemap.xml`。后续 `robots.txt`、`llms.txt` 等站点级公开生成物也应放在同一目录，避免多个站点共享应用根目录时互相覆盖。
