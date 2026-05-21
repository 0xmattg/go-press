# 菜单管理

GoPress 内置完整的导航菜单管理模块，支持多菜单位置、多级嵌套，并预留了插件扩展钩子。

## 核心架构

```
core/menu/
├── Menu          # 菜单实体（ID/Name/Location）
├── Item          # 菜单项（Title/URL/Target/ContentID/Children 树形嵌套）
├── Store         # 内存缓存 + DB 持久化（按 location 和 ID 双索引）
└── Hooks         # menu.location.resolve / menu.deleted 通用扩展点
```

## 功能特性

| 功能 | 说明 |
|------|------|
| **位置注册** | 主题通过 `app.MenuStore().RegisterLocation("header", "顶部导航")` 声明可用位置 |
| **树形结构** | 菜单项支持任意层级嵌套（ParentID → Children），自动构建树 |
| **内存缓存** | `LoadAll()` 启动时加载全部菜单到内存，`GetByLocation()` 零 DB 查询 |
| **内容关联** | 菜单项可关联 `ContentID`，URL 自动解析为对应内容的永久链接 |
| **后台管理** | 创建/编辑/删除菜单，拖拽排序菜单项，分配显示位置 |
| **插件扩展** | `menu.location.resolve` 允许插件替换某位置最终菜单，`menu.deleted` 允许插件清理关联数据 |

## 主题中使用菜单

主题在 `Setup()` 中注册需要的菜单位置：

```go
func (t *MyTheme) Setup(app coreTheme.App) {
    app.MenuStore().RegisterLocation("header", "顶部导航")
    app.MenuStore().RegisterLocation("footer", "底部导航")
}
```

模板中渲染：

```gotemplate
{{$menu := menuByLocation "header"}}
{{if $menu}}
    <ul class="nav-menu">
        {{range $menu.Items}}
            <li><a href="{{.URL}}" class="{{if isMenuURLActive $.Ctx .URL}}active{{end}}">{{.Title}}</a></li>
        {{end}}
        {{renderHook "header.nav.after" .}}
    </ul>
{{end}}
```

当前菜单高亮应基于当前请求 URL 与菜单项 URL 判断，不要在可复用主题里写死内容类型名、菜单标题或 `.ActivePage` 标识。

## 多语言菜单

多语言插件注册 `menu.location.resolve` filter + `menu.deleted` action，实现透明的语言菜单切换，**主题和模板代码零修改**：

下面以主题声明的 `product` 内容类型 URL 为例，实际路径由当前内容类型的 `rewrite_slug` 决定。

```
请求 /zh/products
  → 中间件设置 goroutine 级语言: menu.SetRequestLang("zh")
  → 模板调用 menuByLocation "header"
    → Store.GetByLocation("header") → 取到 header 位置的菜单
    → menu.location.resolve filter 触发:
        1. 查翻译表确定当前菜单的实际语言
        2. 通过 trid 找到 zh 语言对应的菜单
        3. 从 menusById 缓存取出翻译菜单
        4. 克隆 + URL 重写（本地链接加 /zh 前缀，内容关联项解析翻译版 slug）
    → 返回中文菜单（含重写后的 URL）
```

菜单项如果关联的是内容记录，应优先保存 `ContentID`，由 core 按当前 Rewrite 注册表解析 URL。这样后续把某个内容类型的 `rewrite_slug` 从 `products` 改成 `catalog` 时，菜单不需要逐条手动改链接。

后台「翻译管理 → 菜单翻译」按主题注册的菜单位置展示，每个位置每种语言一个下拉框，一键保存分配：

```
📍 header (顶部导航)
  🇬🇧 English:  [main-header ▾]
  🇨🇳 中文:      [main-header-zh ▾]

📍 footer (底部导航)
  🇬🇧 English:  [-- 未分配 -- ▾]
  🇨🇳 中文:      [-- 未分配 -- ▾]

[保存菜单分配]
```

详见 [多语言插件](../plugins/multilang.md)。
