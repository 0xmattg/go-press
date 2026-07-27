# 独立页面

GoPress 内置了 **page（页面）** 内容类型，用于 About、条款、隐私政策等几乎每个站点都需要、且不依赖具体主题的独立页面。页面复用与文章、主题自定义类型相同的统一 `Content` 模型，**无需新建数据库表**。

## 核心概念

| 概念 | 说明 |
|---|---|
| `page` 类型 | 核心内容类型（与 `post`、`contact_message` 并列），由引擎注册，切换主题后依然存在。 |
| 根级永久链接 | 页面 URL 位于站点根：`/about`、`/privacy`，没有 `/blog/…` 这类前缀。 |
| 层级 | 页面可设父页面（`ParentID`），后台可按树形组织。 |
| 无归档 | 页面没有公开列表页，每个页面是独立 URL。 |
| 页面模板 | 主题声明、可供页面选用的布局（如全宽、落地页、嵌入页）。 |

由于 `page` 就是一个普通的已注册内容类型，后台的**页面**菜单、侧边栏入口、列表/新建/编辑/删除、路由，全部由和其它类型一样的数据驱动机制自动生成——**没有任何针对页面的特判后台代码**。

## 后台管理

后台侧边栏「内容」分区下会出现**页面**入口，可以：

- 用通用内容编辑器新建/编辑/删除页面（标题、富文本正文、摘要、主图）。
- 设置**父页面**，把页面嵌套到另一页面下。
- 指定**分类**——页面与文章共用核心 `category` 分类，便于把同属性页面归类（并会出现在 `/category/{分类}` 归档中）。
- 选择**页面模板**（见下文）。
- 填写**嵌入代码**片段（见下文）。
- 像其它内容一样发布 / 存草稿 / 定时。

## 根级 URL 与保留别名

一个 slug 为 `about` 的已发布页面，访问地址是 `/about`。页面解析在**最后**进行——先尝试所有带前缀的内容类型和分类，都不匹配才当作页面。因此页面永远不会遮蔽 `/blog`、`/products` 这类归档。

正因如此，保存时如果页面 slug 与系统路由（`admin`、`api`、`static`、`sitemap.xml` …）或已有归档/分类前缀（`blog`、`products`、`category` …）冲突，会被拒绝——否则页面将无法访问。遇到提示时换一个 slug 即可。

> 当前版本支持**扁平**页面 URL（`/about`）。`ParentID` 用于后台组织，暂不产生 URL 嵌套（`/about/team`）。

## 页面模板

主题可在 `theme.toml` 声明可选的每页布局（对标 WordPress 的「页面模板」）：

```toml
[[page_templates]]
name = "全宽页面"
template = "page-full-width"

[[page_templates]]
name = "嵌入功能页"
template = "page-embed"
```

- `template` 为 `templates/pages/` 下的页面文件名（不含 `.tmpl`）。
- `name` 为后台**页面模板**下拉里显示的名称。
- 选择结果按页存入 `page_template` meta。

渲染时按如下层级解析模板（命中即用）：

```
<所选 page_template>  →  page-<slug>  →  page  →  single  →  index
```

未选择时回退到 `page-<slug>`，再到通用 `page`。切换到没有该模板的主题时，会沿同一条链优雅回退。页面始终通过主题的 base 布局渲染，因此**自动共享站点的页头、页脚与样式**。

一个最简页面模板只需用到标准内容数据：

```gotemplate
{{define "content"}}
<section class="section"><div class="container">
  <h1>{{.Item.Title}}</h1>
  <div class="post-body">{{safeHTML .Item.Content}}</div>
</div></section>
{{end}}
```

## 嵌入外部组件

把功能片段（股票图、地图、视频、计算器等）放到页面上有两种方式，按「谁掌控这段标记」来选。

### 1. 每页嵌入代码（内容编辑者）

页面编辑器有**嵌入代码**字段。粘贴第三方 `<iframe>` 嵌入代码，即可在页面里以响应式容器渲染：

```html
<iframe src="https://www.youtube.com/embed/VIDEO_ID" title="Player"
        allow="encrypted-media; picture-in-picture" allowfullscreen></iframe>
```

粘贴的内容原样保存，渲染时经 **iframe 白名单**（`content.SanitizeEmbed`）消毒：

- **允许**：`<iframe>`（`src` 限 `http`/`https`）及安全属性（`title`、`width`、`height`、`style`、`allow`、`allowfullscreen`、`loading`、`referrerpolicy`、`sandbox` …），以及少量包裹标签（`div`、`p`、`span`、`figure`）。
- **剥离**：`<script>`（内联与外链）、`on*` 事件、`javascript:` URL、`<object>`、`<embed>`，以及一切不在白名单内的东西。

实际含义：

- ✅ 纯 `<iframe>` 形式可用——YouTube、Vimeo、Bilibili、Google 地图、TradingView 的 *iframe* 版 widget，以及大多数图表/视频/地图服务。
- ❌ `<script>` 形式的片段**不可用**（脚本会被剥掉）。请改用它的 iframe 版本，或用下面的模板方式。
- 消毒只过滤**你粘贴的这段标记**（防止在你自己页面里造成存储型 XSS）；被嵌入网页**内部**的 JavaScript（YouTube 播放器、图表自身脚本）照常运行，因为它跑在 iframe 的独立源里。
- iframe 跨源隔离，读不到 GoPress 页面的 DOM/cookie。页面编辑受 RBAC 限制（editor 及以上可信角色）。你在 iframe 里自带的 `sandbox` 属性会被保留。

### 2. 模板承载功能（主题开发者）

当「这个页面本身就是一个功能」——定制仪表盘、脚本型 widget、自定义 JS/CSS——把标记写进主题的**页面模板**（见上文）。模板是可信代码，**不消毒**，脚本和任意标记都允许。在 `[[page_templates]]` 里声明，编辑时按页选用即可，页面正文可作为周边说明文案。

## 架构说明

- `page` 属于框架能力，由引擎像 `post` 一样注册——它不是主题或业务类型，因此不违反「core 不得硬编码内容类型」的边界。
- 其余全部由 `ContentTypeDef` 上的**通用能力**驱动，而非针对 `page` 特判：`Rewrite.Rootless`（根级单页 URL）、`Hierarchical`（父/子）、`ReadOnly`（后台是否给完整 CRUD）。主题也能用同样方式声明自己的 rootless / 层级类型。
- 主题只通过核心扩展点参与（模板层级、`theme.toml` 的 `[[page_templates]]` 声明、`category` 分类），未引入 theme↔plugin 耦合，依赖方向保持 `theme → core`。
- 页面 CRUD 复用核心 `content` RBAC 资源，`content.create/read/update/delete` 与文章一样约束页面。
