# 缓存与 i18n

## 多级缓存

GoPress 把缓存当作**架构基础**而不是优化手段：

| 层级 | 介质 | 用途 |
|---|---|---|
| L1 | 进程内 LRU | 单实例热数据，零网络延迟 |
| L2 | Redis | 跨实例共享、容量大 |
| 整页缓存 | L1+L2 复用 | 命中时 < 1ms 直接返回完整 HTML |

**缓存 Key 自动包含语言维度** — 多语言场景下不同语言的页面缓存独立，不会互相污染。

**按标签批量失效** — 内容更新、菜单调整、设置变更触发对应标签清理，避免脏数据。

**Redis 可选** — 配置中删除 `[redis]` 段即降级为纯内存缓存，单实例部署完全可用。

## 核心 i18n 系统

GoPress 的国际化能力由 **Core 层** 提供，多语言插件负责数据库覆盖和管理后台，两者通过接口解耦：

```
┌─────────────────────────────────────────────────────────────────────┐
│  Core 层                                                            │
│                                                                     │
│  core/i18n/                                                         │
│  ├── Manager           # i18n 管理器（go-i18n Bundle + Localizer）  │
│  ├── T(c, key)         # 模板函数：翻译 UI 字符串                   │
│  ├── TranslateOption() # 翻译单个主题设置项                         │
│  └── TranslateSettings()  # 批量翻译所有可翻译设置项                │
│                                                                     │
│  core/option/                                                       │
│  ├── RegisterTranslatable(key, section, label)  # 注册可翻译设置键  │
│  ├── IsTranslatable(key)                        # 判断是否可翻译    │
│  └── AllTranslatableKeys()                      # 返回所有注册键    │
└────────────────────────────────┬────────────────────────────────────┘
                                 │ LoadMessageFileFS() / AddMessages()
                                 ▼
┌─────────────────────────────────────────────────────────────────────┐
│  数据来源（优先级从高到低）                                          │
│                                                                     │
│  1. DB StringTranslation   ← multilang 插件 loadDBOverrides()      │
│     domain="theme"  → UI 字符串覆盖                                 │
│     domain="option" → 主题设置翻译（_opt. 前缀，防 ID 碰撞）        │
│  2. 主题 locale 文件       ← themes/xxx/locales/en.json, zh.json   │
│  3. message ID 原样返回    ← 兜底                                   │
└─────────────────────────────────────────────────────────────────────┘
```

### UI 字符串翻译

主题在 `locales/` 目录提供 JSON 翻译文件，模板中 `{{T .Ctx "welcome"}}` 直接使用。multilang 插件后台「字符串翻译管理」允许在 DB 中覆盖或新增翻译条目。

### 主题设置翻译

主题在 `translatable.go` 中调用 `option.RegisterTranslatable()` 声明可翻译的设置键（如 hero 标题、about 描述）。Core 的 `TranslateSettings()` 在渲染时自动翻译 settings map 中的可翻译键。multilang 插件后台「主题设置翻译」提供按分组的翻译编辑界面。

```go
// 主题声明可翻译设置（themes/modern-company/translatable.go）
func registerTranslatableOptions() {
    option.RegisterTranslatable("home_hero_title", "hero", "Hero 标题")
    option.RegisterTranslatable("home_about_title", "about", "关于标题")
    // ... 可翻译设置键覆盖首页 hero / about 区块、数据统计、产品/服务展示、CTA 等区块全文案
}

// Core 自动翻译（渲染时，theme handler 中调用）
func (p *PageData) TranslateSettings(c *gin.Context, mgr *i18n.Manager) {
    p.Settings = mgr.TranslateSettings(c, p.Settings, option.IsTranslatable, option.AllTranslatableKeys())
}
```

### 模板可用函数

下面以主题声明的 `product` 内容类型 URL 为例，实际路径由当前内容类型的 `rewrite_slug` 决定。

```go
// 翻译 UI 字符串
{{T .Ctx "welcome"}}

// 获取当前语言代码
{{currentLang .Ctx}}

// 生成带语言前缀的 URL
{{langPrefixURL .Ctx "/products/hepa-filters"}}

// 生成内容类型归档和详情 URL，读取 Rewrite 注册表
{{archiveURL "product"}}
{{contentURL . "product"}}
```

详细多语言能力（内容翻译、菜单翻译、URL 路由、语言检测、智能跳转等）见 [多语言插件](../plugins/multilang.md)。
