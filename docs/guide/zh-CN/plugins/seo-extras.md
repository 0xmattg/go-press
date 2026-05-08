# SEO Extras 插件 (Yoast-like)

GoPress 内置的 Yoast 风格 per-content SEO 覆盖插件。激活后，每条内容编辑页底部会多出一个折叠的「SEO 设置（可选）」面板，提供 4 个独立字段让编辑给单条内容定制 SEO 输出。

## 它解决什么问题

默认情况下，单内容页的 SEO 字段是从内容自身字段推断的：

| SEOMeta 字段 | 默认数据源 |
|---|---|
| `<meta description>` / `og:description` | `Content.Excerpt`（自动 truncate 到 160） |
| `og:image` | `Content.ImageURL` |
| `og:title` | `Content.Title` |
| `<meta robots>` | `index, follow` |

但有些场景需要"独立覆盖"——譬如：

- 内容标题想短一点好阅读，但 `<title>` 想塞更多 SEO 关键词
- description 不想自动用 Excerpt（可能被截断或不利于点击率）
- 社交分享卡用一张特制的 og:image，而不是页面主图
- 临时下架的产品想 `noindex`，但页面还要保留给老链接访问者

## 安装

激活方式：后台「插件管理」 → 找到「seo-extras」 → 点击「启用」。

任意支持内容编辑的内容类型页面滚到底，会出现：

```
▶ SEO 设置（可选）

  SEO Title         [_______________] (推荐 50–60 字符)
  SEO Description   [_______________] (推荐 50–160 字符)
  Open Graph 分享图 [_______________] [选择图片]
  Robots 指令       [默认 (index, follow) ▾]
```

4 个字段全是可选的，留空走默认。

## 数据存储

字段以 `_seo_` 前缀存到 `gp_content_meta`：

| 字段 | meta key | 覆盖什么 |
|---|---|---|
| SEO Title | `_seo_title` | `seo.Title` + `og:title` |
| SEO Description | `_seo_description` | `<meta description>` + `og:description` |
| Open Graph 分享图 | `_seo_image` | `og:image` |
| Robots 指令 | `_seo_robots` | `<meta robots>` |

下划线前缀避开 `typeDef.MetaFields` 注册的常规 meta 字段，清晰归属本插件。

**字段为空时执行 `DeleteMeta` 而非保存空字符串**——保证读路径"不存在 = 用默认"语义清晰，不会因为残留 `_seo_title=""` 把默认行为屏蔽掉。

## 实现架构

它是一个**纯插件**：core 完全不动、零数据库表（直接复用 `gp_content_meta`）、零路由。激活时仅注册 3 个 hook：

```
admin.content_form.fields  filter → 渲染 meta box HTML
admin.content.saved         action → 把 form 值持久化到 gp_content_meta
seo.content.meta            filter → 在 SEOBuilder 输出后 patch SEOMeta
```

完整数据流（包含插件介入）：

```
SEOBuilder.ForContent(item, typeDef)
                ▼
ApplySiteOptionOverrides         （site_name / site_description 兜底）
                ▼
ApplyContentMetaSEO              （触发 seo.content.meta filter 链）
   ├── seo-extras 插件插入        （读 _seo_* meta，覆盖 Title/Description/OGImage/Robots）
   └── 你的其它 SEO 插件...
                ▼
data.SEO / data["SEO"]          （注入模板）
                ▼
{{seoHeadFor .}}                （渲染 HTML）
```

## 行为契约

| 场景 | 行为 |
|---|---|
| 插件**未激活** | meta box 不显示；`SEOContentMeta` 过滤器无订阅者；SEO 输出回退到默认（Excerpt + ImageURL） |
| 插件激活但**字段全空** | meta box 显示但都没填；过滤器读到空 meta，原样返回 SEOMeta；输出仍是默认 |
| 插件激活 + 部分字段填值 | 填了的字段覆盖，没填的走默认（混合模式） |
| 插件**激活后又停用** | hook 全部 Remove，meta box 消失；已存的 `_seo_*` meta 留在库里但不再被读 |

## 自定义 struct 主题需要做的事

如果你的主题用自定义 PageService（参考 modern-company / financial-news），要让 seo-extras 这类插件生效，`buildContentSEO` 末尾必须调一次 `ApplyContentMetaSEO`：

```go
import (
    "go-press/core/hook"
    coreTheme "go-press/core/theme"
)

type PageService struct {
    seoBuilder  *rewrite.SEOBuilder
    registry    *content.Registry
    hookBus     *hook.Bus            // 新增：从 engine.Hooks 获取
    contentRepo *content.Repository
}

func (s *PageService) buildContentSEO(item *content.Content, typeName string) rewrite.SEOMeta {
    seo := s.seoBuilder.ForContent(item, s.registry.GetType(typeName))
    s.applySEOOverrides(&seo)
    coreTheme.ApplyContentMetaSEO(s.hookBus, s.contentRepo, &seo, item)  // ← 让插件能 patch
    return seo
}
```

`BaseTheme + gin.H` 主题完全不用关心，core 的 `renderSingle` 已经替你调好了。

详见 [主题 SEO 接入规范](../themes/seo-integration.md)。

## 自己写"扩展 SEO"插件

如果你想加自己的 SEO 字段（比如 schema.org 产品规格、自定义 og:type），完全可以再写一个插件，跟 seo-extras 并存。每个插件订阅同一组 hook，按 `priority` 顺序累加修改 SEOMeta，互不干扰。

这才是这套架构的真正价值：**SEO 不是 core 写死的特性，而是可叠加的插件能力**。
