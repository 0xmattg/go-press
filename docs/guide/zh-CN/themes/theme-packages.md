# 主题包

主题包是不含 Go 代码的主题：一个普通的 `.zip` 文件，里面只有 `theme.toml`、模板、样式、图片、字体、语言包和可选的演示数据。主题包不需要重新编译 GoPress，适合分发给其他站点。

当前版本提供主题包的格式、检查、打包和签名工具，以及在开发站点上运行主题包的[本地预览](#本地预览)。在站点后台安装主题包的功能会在后续版本提供。

## 和 Go 主题的区别

| | Go 主题 | 主题包 |
|---|---|---|
| 代码 | 可以写 Go：自定义路由、处理函数、模板函数 | 只有模板和静态文件 |
| 模板函数 | core 的函数，加主题自己的函数 | 只能用 core 的函数，包括[只读数据函数](creating-themes.md#只读数据函数) |
| 模板结构 | 页面 bundle 或经典层级 | 只支持页面 bundle |
| 设置页 | `[[settings]]` 或自定义后台模板 | 只能用 `[[settings]]`，由 core 生成设置页 |
| 脚本 | 可以 | **不允许**，改用 core 的[交互组件](interactions.md) |
| 外部资源 | 不限制 | 必须在 `[[external]]` 里声明 |

## 目录结构

```text
aurora-1.2.0.zip
├── theme.toml                 必须
├── templates/
│   ├── layouts/base.tmpl      必须，定义 {{define "base"}}
│   ├── partials/*.tmpl        可选
│   └── pages/*.tmpl           必须有首页，见「模板」
├── static/                    样式、图片、字体
├── locales/<lang>.json        可选，前台文案
├── locales/admin/<lang>.json  可选，设置页文案
├── demo/                      可选，演示数据（demo/data/seed.toml 和图片）
├── screenshot.png             可选，由 [theme] screenshot 指定
├── README.md、CHANGELOG.md、LICENSE   可选
└── signatures/                签名后才有：publisher.json、market.json
```

- 模板只能直接放在 `templates/layouts/`、`templates/partials/`、`templates/pages/` 三个目录里，扩展名是 `.tmpl`。只有 `layouts/base.tmpl` 会被加载。
- 不支持覆盖插件页面的模板（`templates/<namespace>/`）：插件页面用插件自带的片段，套在主题的 `base` 布局里。
- 不能有 `templates/admin/`：设置页由 `[[settings]]` 生成，见[主题设置页](creating-themes.md#主题设置页)。
- 顶层只能有上面列出的目录和文件。
- 语言包是「字符串到字符串」的 JSON 对象。

## theme.toml

```toml
[theme]
slug = "aurora"
name = "Aurora"
version = "1.2.0"
description = "A clean magazine theme."
author = "Example Studio"
screenshot = "screenshot.png"

[requires]
core = ">=0.7.23"

[[settings]]
key = "hero_title"
type = "string"
label = "Hero title"
default = "Welcome"
translatable = true

[[external]]
host = "fonts.googleapis.com"
kinds = ["style"]
purpose = "Web font stylesheet"

[[external]]
host = "fonts.gstatic.com"
kinds = ["font"]
purpose = "Web font files"
```

主题包必须写：

| 字段 | 要求 |
|---|---|
| `[theme] slug` | 主题的唯一标识：2–64 个小写字母和数字，单词之间用单个连字符连接。不能和站点里已有的主题、插件重名 |
| `[theme] name` | 显示名称 |
| `[theme] version` | 语义化版本，例如 `1.2.0` |
| `[requires] core` | 支持的 GoPress 版本范围，例如 `">=0.7.23"` |

`[[settings]]`、`[[setting_groups]]`、`[[menu_locations]]`、`[[page_templates]]`、`[[content_types]]` 和 `[requires] plugins` 的写法和 Go 主题相同。`[[page_templates]]` 引用的页面必须存在于 `templates/pages/`。

`[theme] slug` 和 `[[external]]` 对 Go 主题是可选的。Go 主题的 slug 来自注册名。

## 外部资源：`[[external]]`

主题从其他域名加载的每一种资源，都要在 `[[external]]` 里声明：

| 键 | 说明 |
|---|---|
| `host` | 小写域名，例如 `fonts.gstatic.com`。可以用 `*.` 前缀覆盖所有子域：`*.example.com` 匹配 `img.example.com`，但不匹配 `example.com` 本身。不能写协议、端口、路径或 IP 地址 |
| `kinds` | 从这个域名加载的资源种类，见下表 |
| `purpose` | 必填，说明用途，最多 200 字符。管理员安装或切换主题时会看到 |

最多声明 20 个域名，同一个域名只能声明一次。

| kind | 包括 |
|---|---|
| `style` | `<link rel="stylesheet">`、CSS 的 `@import` |
| `font` | `@font-face` 里的 `url()`、`<link rel="preload" as="font">` |
| `image` | `<img>`、`srcset`、`<video poster>`、CSS 的 `url()`、图标、SVG 的 `<image>`，以及图片类设置项的默认值 |
| `media` | `<video>`、`<audio>`、`<source src>`、`<track>` |
| `frame` | `<iframe>` |

没有 `script`：主题包不能加载脚本。

检查只看**加载资源**的地址。`<a href>`、`<form action>` 这类跳转不用声明。由模板动作拼出来的地址（例如来自设置的图片）在打包时无法确定，站点会用内容安全策略（CSP）按 `[[external]]` 限制它们。用 `http://` 加载的资源会得到一条警告：HTTPS 站点上浏览器会拦截这种资源。

## 不能使用脚本

主题包不能带任何形式的脚本，检查遇到下面任何一种情况都会拒绝：

- `.js`、`.mjs`、`.cjs` 文件；
- `<script>` 元素。只有内联的 `<script type="application/ld+json">` 结构化数据例外，而且不能带 `src`；
- `onclick` 这类 `on*` 事件属性；
- `javascript:`、`vbscript:` 地址。链接只能用 `http`、`https`、`mailto`、`tel`、`sms` 和相对地址；图片可以用 `data:`；
- `<iframe srcdoc>`、`<object>`、`<embed>`、`<applet>`、`<base>`、`<meta http-equiv="refresh">`；
- CSS 的 `expression()`、`behavior:`、`-moz-binding`，以及 `url()` 里的非 `http`、`https`、`data` 地址；
- SVG 里的 `<foreignObject>`、DTD 声明和 `xml-stylesheet` 处理指令。

这些规则也作用于被模板拆开的写法。检查会把模板里的动作和 `{{if}}` 等分支去掉，再检查剩下的文本，所以 `<scr{{""}}ipt>`、`java{{""}}script:` 一样会被发现。元素名或属性名不能由动作或分支拼出来，例如 `<h{{.Level}}>`、`<a o{{"nclick"}}=…>`：请在每个分支里写出完整的名字。属性整体由 `{{if}}` 控制是可以的，例如 `<input {{if .Checked}}checked{{end}}>`；`data-` 和 `aria-` 开头的属性名也可以带动作。

菜单、弹窗、轮播这类常用交互，请用 GoPress 自带的[交互组件](interactions.md)：在布局里调用 `{{uiScript}}`，再用 `data-gp-*` 属性配置。`{{uiScript}}` 输出的脚本标签不属于包里的文本，不受上面的规则限制。更简单的交互只用 HTML 和 CSS 就行，例如 `<details>`、`:focus-within`。

## 内容安全策略（CSP）

活动主题是主题包时，前台的每个响应都会带上 `Content-Security-Policy` 响应头。这样即使有脚本混进了包里，也不会执行：

| 指令 | 允许 |
|---|---|
| `script-src` | 本站，加上已启用插件声明的来源。不允许任意内联脚本和 `eval` |
| `style-src` | 本站、内联样式，以及主题用 `style` 种类声明的域名 |
| `font-src` | 本站、`data:` 字体，以及用 `font` 种类声明的域名 |
| `img-src`、`media-src`、`frame-src` | 本站和任意 `https:` 来源（图片另允许 `data:`），因为文章里的图片、视频和嵌入内容可能来自任何网站 |
| `connect-src` | 本站，加上已启用插件声明的来源 |
| `object-src`、`base-uri` | 都不允许；只允许本站 |

后台页面、JSON API 和 API 浏览页不加这个头；GoPress 内置的主题完全不加。模板和 CSS 要从哪些域名加载样式或字体，都要在 `[[external]]` 里声明，没声明的会被拦下。用 `gopress serve -theme-package` 预览时同样生效，浏览器控制台会列出被拦下的内容。

## 限额

| 项目 | 上限 |
|---|---|
| `.zip` 文件 | 10 MB |
| 解压后总大小（按实际读出的字节计算） | 25 MB |
| 文件数 | 1000 |
| 单个文件 | 5 MB |
| 单个模板 | 512 KB |
| 包内路径长度 | 200 字节 |

另外：

- 路径只能用 `/` 分隔，不能是绝对路径，不能含 `..`、`:` 或控制字符，路径段不能以 `.` 开头；
- 不能有符号链接或其他特殊文件，不能加密，压缩方式只能是存储或 deflate；
- 不能有重名条目，只差大小写的也不行，一个名字也不能既是文件又是目录。

## 允许的文件

| 类型 | 扩展名 | 位置 |
|---|---|---|
| 模板 | `.tmpl` | `templates/layouts/`、`templates/partials/`、`templates/pages/` |
| 样式和网页 | `.css`、`.html` | `static/` 等 |
| 图片 | `.png`、`.jpg`、`.jpeg`、`.gif`、`.webp`、`.avif`、`.svg`、`.ico` | `static/`、`demo/`，截图可以放在顶层 |
| 字体 | `.woff`、`.woff2`、`.ttf`、`.otf` | `static/` |
| 数据 | `.json` | `locales/`、`static/` 等；`.toml` 只能是顶层的 `theme.toml` 和 `demo/` 里的文件 |
| 文本 | `.md`、`.txt`、`LICENSE` | 顶层只能放 `README.md`、`CHANGELOG.md`、`LICENSE` |

文本文件必须是 UTF-8。图片和字体的文件头必须和扩展名一致。

## 模板

- 每个 `pages/*.tmpl` 和 `layouts/base.tmpl`、全部 `partials/*.tmpl` 一起解析，执行 `base` 模板。写法和 Go 主题的页面 bundle 相同，见[创建主题](creating-themes.md#模板命名约定)。
- 用哪一页按名字决定，和 Go 主题一样：

  | 页面 | 名字（按顺序取第一个存在的） |
  |---|---|
  | 首页（必须有） | `front-page`、`home`、`index` |
  | 找不到页面（状态码 404） | `404`；没有这一页时返回纯文本 |
  | 内容详情和列表 | `single-<类型>-<slug>`、`single-<类型>`、`single`、`archive-<类型>`、`archive` 等，完整顺序见[模板命名约定](creating-themes.md#模板命名约定)；都没有时用 core 内置的页面 |

  每页拿到的数据和 Go 主题相同：详情页有 `.Item`，列表页有 `.Items`，所有页面都有 `.Settings`、`.SEO`、`.Ctx`。
- 主题包不能注册路由，所以需要自己处理提交的表单（例如联系表单）暂时做不了。列表页每页固定 20 条。
- 只能用 core 提供的模板函数。用了 Go 主题自己的函数时，检查报 `template.parse`，并给出函数名。
- 检查会用 `html/template` 的转义器把每一页预跑一遍。例如某个 `{{if}}` 的两个分支停在不同的上下文里（一边在属性值里，一边在标签外），站点上这一页每次都会渲染失败，检查报 `template.escape`。

## 静态文件

`static/` 里的文件和 Go 主题一样，通过 `/static/...` 访问。主题包里的 `.svg`、`.html`、`.xml` 文件在浏览器里直接打开时会被下载而不是显示，和上传的文件一样。在 `<img>` 或 CSS 的 `url()` 里使用 SVG 不受影响。

## 演示数据

主题包可以带演示数据 `demo/data/seed.toml`，格式和 Go 主题相同，管理员在「主题」页导入。因为数据来自第三方，导入主题包的演示数据时规则更严格：

- `[[settings]]` 只能设置 `theme.toml` 的 `[[settings]]` 里声明过的设置，值要符合声明的类型；
- `image_url` 只能是包里的文件（写成 `/static/...`），或者 `https://` 地址，而且主机要在 `[[external]]` 里声明了 `image`。站点不会下载远程图片，只是引用它；
- 内容正文会像在编辑器里保存时一样被清理。

只要有一条不符合，就什么都不导入，错误信息会列出所有问题。

## 本地预览

在开发站点上运行主题包：

```bash
gopress serve -theme-package ./aurora
```

- 路径可以是主题包的源码目录或 `.zip`。参数可以写多次，加载多个包。
- 检查方式和安装时完全相同，`[requires] core` 用站点的 GoPress 版本判断；有问题的包不会加载，后台「主题」页和服务日志会说明原因。不验签名。
- 主题会出现在后台「主题」页，在那里切换。改了文件要重启服务。
- `server.mode` 是 `"release"` 时不能用这个参数。
- 之后不带这个参数启动，站点会先用其他主题，直到这个包重新加载，后台会显示提示。

## 安装和管理

站点在后台「主题 → 主题市场」安装主题包。只有拥有 `extension.*` 权限的角色可以操作（默认只有超级管理员）：`extension.read` 打开市场页，`extension.install` 安装、升级、上传和回滚，`extension.delete` 卸载。安装不等于启用，启用要到「主题」页切换。

**从市场安装**：市场页每次进入都实时拉取目录，安装或升级到适合当前 GoPress 版本的最新版本。保存之前，GoPress 先确认下载到的包和签名目录描述的完全一致（身份、大小、内容摘要、外部域名），再按和本地预览相同的规则检查包本身。安装对话框会列出主题会从哪些外部域名加载资源。市场撤销或下架的版本不能安装。

**手动上传**：「上传主题包」用来安装 `.zip`：

- 带有效市场加签（可信市场签发的 `signatures/market.json`）的包直接安装。
- 其他包（没有签名、只有开发者签名、签名后改过文件）需要**开发者模式**，由超级管理员在「系统设置 → 开发者选项」里打开。后台会显示包的名称、版本、内容摘要和外部域名，确认的就是这一份包才会安装。这类包在各处都标为「未签名」。
- 关闭开发者模式后，已装好的未签名包继续可用，但不能再安装新的未签名包或版本。生产站建议设置 `[market] lock_developer_mode = true`，见[配置文件](../getting-started/configuration.md#market)。

无论哪种方式，包的检查规则都一样：开发者模式只是不再要求签名。

**升级和回滚**：升级正在使用的主题会立即生效。新版本需要本站没有的插件时，拒绝升级；新版本无法启用时，仍保留原来的版本。每个主题保留当前版本和最近安装的 3 个旧版本，在「已安装」标签里可以回滚到其中任何一个，回滚前会重新检查保存的文件。

**更新提示**：后台的更新检查开关（系统设置 →「官方更新检查与 Beta 使用情况反馈」）打开时，GoPress 大约每天还会问一次市场：从它安装的主题有没有新版本。请求里只有每个主题的标识和已安装版本。有新版本时，主题卡片和「已安装」标签会显示「有新版本 x.y.z」；安全更新和市场不再支持的版本用红色显示。点击会跳到市场里的这个主题，照常升级。手动上传安装的包不检查。

**卸载**：正在使用的主题不能卸载，要先切换。卸载会删除全部版本和文件，但保留主题设置和已导入的示例内容，重新安装后设置还在。

**撤销**：市场撤销某个版本后，站点一拉到这份目录（打开市场页或安装时）就会把它标为「已撤销」；切换主题、回滚和上传时也会用最近一次拉到的目录检查，不需要联网。已撤销的版本不能切换过去、回滚过去，也不能安装。如果它正在使用，主题会继续运行，后台每个页面都显示提示横幅，直到切换或升级。

已安装的主题包以文件形式保存在站点目录里，备份时要和数据库一起备份（见[配置文件](../getting-started/configuration.md#多站点配置)）。

## 命令行

这些命令不需要 GoPress 源码目录：

```bash
gopress pkg check ./aurora
```

```bash
gopress pkg build ./aurora
```

```bash
gopress pkg sign -key publisher.key aurora-1.2.0.zip
```

```bash
gopress pkg verify aurora-1.2.0.zip
```

- **`check`**：检查目录或 `.zip`，按规则列出所有问题，有问题时退出码为 1。加 `-core 0.7.23` 时同时检查 `[requires] core` 是否支持这个版本。
- **`build`**：检查目录，通过后打包成 `<slug>-<version>.zip`（`-out` 可以改文件名）。以 `.` 开头的路径（`.git`、`.DS_Store`）和 `signatures/` 不会打进去。同样的文件每次打出的 zip 字节完全相同。
- **`sign`**：给目录或 `.zip` 签名，把 `signatures/publisher.json` 写进去。签名文件不计入内容摘要，所以签名前后摘要不变。
- **`verify`**、**`digest`**：验签、输出内容摘要。

流程是先 `build`，再对生成的 zip `sign`。

## 规则编号

每个问题都带一个固定的规则编号：

| 编号 | 含义 |
|---|---|
| `zip.size` | zip 文件超过 10 MB |
| `entry.special`、`entry.encrypted`、`entry.method`、`entry.duplicate`、`entry.corrupt` | 特殊文件、加密、不支持的压缩方式、重名、无法读取 |
| `path.encoding`、`path.unsafe`、`path.hidden`、`path.length` | 路径编码、不安全路径、隐藏文件、路径过长 |
| `size.total`、`size.files`、`size.file`、`size.template` | 总大小、文件数、单个文件、单个模板超限 |
| `file.type`、`file.place`、`file.content`、`file.json`、`file.toml` | 不允许的类型、位置不对、内容和类型不符、JSON 或 TOML 无效 |
| `script.file`、`script.tag`、`script.handler`、`script.url`、`script.css`、`script.embed`、`script.base`、`script.refresh`、`script.svg`、`script.dynamic` | 各类脚本；`script.dynamic` 指由动作拼出的元素名或属性名 |
| `template.admin`、`template.place`、`template.layout`、`template.pages`、`template.home`、`template.base` | 模板结构；`template.home` 指包里没有首页 |
| `template.parse`、`template.escape`、`template.complex` | 解析失败、转义失败、页面展开后过大 |
| `manifest.missing`、`manifest.syntax`、`manifest.slug`、`manifest.reserved`、`manifest.name`、`manifest.version`、`manifest.core`、`manifest.core-unmet`、`manifest.requires`、`manifest.settings`、`manifest.page-template`、`manifest.screenshot` | `theme.toml` 的各项要求 |
| `external.invalid`、`external.undeclared`、`external.kind` | `[[external]]` 写法无效、域名没有声明、域名没有声明这种资源 |

警告不影响通过：`template.unused`（`layouts/` 里除 `base.tmpl` 外的文件不会被加载）、`file.shadowed`（`static/admin/`、`static/uploads/`、`static/gopress/` 由 GoPress 自己提供，这些目录下的文件永远访问不到）、`manifest.unknown`（`theme.toml` 里未知的键）、`external.insecure`（`http://` 资源）、`source.hidden`（打包时略过的隐藏路径）。
