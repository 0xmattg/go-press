# 主题交互组件

GoPress 内置一个前台交互脚本，提供主题常用的 8 种交互：开关、导航子菜单、对话框、轮播、滚动状态和回到顶部、渐显、阅读进度、复制。主题在模板里写 `data-gp-*` 属性来配置，不用写脚本。

[主题包](theme-packages.md)不能带脚本，菜单、弹窗、轮播这类交互就用这些组件实现。Go 主题也可以用。

## 引入脚本

在 `layouts/base.tmpl` 里调用 `{{uiScript}}`，放在 `<head>` 里或 `</body>` 前都可以：

```gotemplate
<head>
  …
  {{uiScript}}
</head>
```

输出类似：

```html
<script src="/static/gopress/ui.js?v=3f9a1c0b7e2d" defer></script>
```

- 脚本随 GoPress 发布，地址在本站，主题包的内容安全策略（CSP）已经放行，不用在 `[[external]]` 里声明。
- `?v=` 后面是脚本内容的摘要，脚本更新后地址随之变化，浏览器不会用旧缓存。
- 没调用 `{{uiScript}}` 的主题不加载脚本，页面没有任何变化。
- `/static/gopress/` 由 GoPress 占用。主题放在 `static/gopress/` 下的文件访问不到，主题包检查会给出 `file.shadowed` 警告。

## 约定

### 配置属性和状态属性

| 种类 | 写法 | 谁来写 |
|---|---|---|
| 配置 | `data-gp-<组件>`、`data-gp-<组件>-<选项>` | 主题在模板里写，脚本只读 |
| 状态 | `data-gp-is-<状态>`，例如 `data-gp-is-open` | 脚本写，主题用来写样式，不要在模板里写 |

组件只改状态：设置 `data-gp-is-*`、`aria-*`、`hidden`、`inert` 属性和 CSS 变量 `--gp-progress`。所有样式（显示、隐藏、动画）都由主题的 CSS 决定。

### 属性值

配置属性的值只能是以下几种：

- **元素 ID**：字母开头，由字母、数字、`-`、`_` 组成，最长 64 个字符；
- **整数**：每个属性有自己的范围；
- **固定的词**：用空格分隔。

值写错时，组件在浏览器控制台输出一条以 `GoPress UI:` 开头的警告：

- 指向目标的属性（例如 `data-gp-toggle`）写错时，这个组件不生效；
- 选项（例如 `data-gp-interval`）写错时，按默认值处理。

### ID 必须唯一

文章正文和评论里的 HTML 可以带 `id` 属性。为了不让用户内容冒充主题的元素，组件只认**页面上唯一**的 ID：两个元素用了同一个 ID 时，指向它的组件不生效，并在控制台警告。

建议给主题自己的 ID 加上前缀，例如 `qr-site-nav`，避免和正文里的标题锚点重名。

用户内容里的 `data-gp-*` 属性会被内容净化去掉，作者和评论者没法触发这些组件。

### 页面状态

脚本在 `<html>` 上写两个属性：

| 属性 | 含义 | 用途 |
|---|---|---|
| `data-gp-ready` | 脚本已经运行 | 「没有脚本时不该隐藏」的样式以它为前提，例如渐显 |
| `data-gp-is-locked` | 有模态对话框打开 | 禁止页面滚动：`html[data-gp-is-locked] { overflow: hidden; }` |

要按某个元素的状态改整页样式时，用 `:has()`，例如：

```css
body:has(#qr-site-nav[data-gp-is-open]) { overflow: hidden; }
```

### 没有脚本时

组件是增强，页面在没有脚本时也应该能用：

- 打开对话框的链接写真实地址（例如登录页），脚本接管时才阻止跳转；
- 子菜单在宽屏靠 CSS 的 `:hover`、`:focus-within` 展开；
- 渐显的隐藏样式以 `html[data-gp-ready]` 为前提。

插件输出的片段也可以用这些属性，但只能当增强，因为当前主题不一定加载了脚本。

### 减少动态效果

访客在系统里开了「减少动态效果」（`prefers-reduced-motion: reduce`）时：

- 轮播不自动播放；
- 回到顶部不用平滑滚动；
- 渐显的元素直接显示。

主题自己的过渡动画也应放在 `@media (prefers-reduced-motion: no-preference)` 里。

## 组件

### 1. 开关 `data-gp-toggle`

用于手机菜单、搜索抽屉、评论回复、「展开更多」。

```gotemplate
<button type="button" data-gp-toggle="qr-nav" aria-label="{{T .Ctx "menu"}}">☰</button>
<nav id="qr-nav" data-gp-dismiss="escape outside link">…</nav>

<button type="button" data-gp-toggle="qr-reply-{{.ID}}">{{T .Ctx "reply"}}</button>
<form id="qr-reply-{{.ID}}" hidden>
  <textarea data-gp-autofocus></textarea>
  <button type="button" data-gp-toggle="qr-reply-{{.ID}}">{{T .Ctx "cancel"}}</button>
</form>
```

- 点击开关时切换目标的状态：
  - 目标打开时带 `data-gp-is-open`；
  - 目标在页面加载时带 `hidden` 的，同时切换 `hidden`。
- 指向同一目标的所有开关都会设置 `aria-controls` 和 `aria-expanded`，状态保持同步。
- 打开时，目标里带 `data-gp-autofocus` 的元素获得焦点。
- 目标上的 `data-gp-dismiss` 决定还有哪些方式能关闭它，可以写一个或几个词：

  | 词 | 关闭方式 |
  |---|---|
  | `escape` | 按 Esc |
  | `outside` | 点击目标和开关以外的地方 |
  | `link` | 点击目标里的链接 |

- 关闭时焦点在目标里的，焦点回到打开它的开关；点链接关闭的除外，焦点交给链接。
- 目标上的 `data-gp-group="<名字>"`：同组的目标同时只开一个，适合并排的几个下拉。

```css
#qr-nav:not([data-gp-is-open]) { display: none; }
@media (min-width: 900px) { #qr-nav { display: block; } }
```

### 2. 导航子菜单 `data-gp-menu`

放在导航的列表上：

```html
<ul data-gp-menu data-gp-menu-below="900">
  <li><a href="/docs">文档</a>
    <ul>…</ul>
  </li>
  <li><a href="/blog">博客</a><button type="button" aria-label="展开博客子菜单">▾</button>
    <ul>…</ul>
  </li>
</ul>
```

- **窄屏**（视口宽度小于 `data-gp-menu-below`，整数 320–2000，默认 900）：
  - 点击条目的触发控件展开或收起子列表，`li` 带 `data-gp-is-open`，触发控件设置 `aria-expanded`；
  - 触发控件是 `li` 里直接的 `<button>`；没有按钮时是第一个链接，这时链接在窄屏不跳转。上级页面需要能访问的，加一个按钮，或在子列表里放一个同样的链接；
  - 收起上级时，里面展开的下级一起收起；
  - 焦点在子菜单里时按 Esc 收起，焦点回到触发控件。
- **宽屏**不接管点击，子菜单由主题的 CSS 用 `:hover`、`:focus-within` 展开。从窄屏变到宽屏时，脚本清除自己写的状态。
- 子列表必须是 `li` 的直接子元素 `<ul>` 或 `<ol>`。插件输出的同样结构（例如语言切换器）也会生效。
- 按住 Ctrl、⌘、Shift、Alt 点击时，交给浏览器处理（例如在新标签页打开）。

```css
[data-gp-menu] li > ul { display: none; }
[data-gp-menu] li[data-gp-is-open] > ul { display: block; }
@media (min-width: 900px) {
  [data-gp-menu] li:hover > ul,
  [data-gp-menu] li:focus-within > ul { display: block; }
}
```

宽屏的悬停菜单和上级之间如果有间距，要用透明的 `::before` 桥或 `padding-top` 填上，否则鼠标经过间距时菜单会消失。

### 3. 对话框 `data-gp-dialog`

用于登录弹窗、侧边抽屉、图片放大。**只支持原生 `<dialog>` 元素。**

```gotemplate
<a href="{{loginURL .Ctx}}" data-gp-dialog-open="qr-login" aria-haspopup="dialog">{{T .Ctx "sign_in"}}</a>

<dialog id="qr-login" data-gp-dialog data-gp-dismiss="outside" aria-labelledby="qr-login-title">
  <h2 id="qr-login-title">{{T .Ctx "sign_in"}}</h2>
  <input name="email" type="email" data-gp-autofocus>
  <button type="button" data-gp-dialog-close aria-label="{{T .Ctx "close"}}">×</button>
</dialog>
```

- 点击 `data-gp-dialog-open` 时用模态方式打开对话框。浏览器提供遮罩（`::backdrop`）、按 Esc 关闭，并让对话框以外的内容不可操作。
- 脚本补上：
  - 打开时聚焦 `data-gp-autofocus`；
  - 打开期间 `<html>` 带 `data-gp-is-locked`；
  - 关闭后焦点回到打开它的元素。
- `data-gp-dialog-close`：点击关闭所在的对话框。
- `data-gp-dismiss`：
  - `outside`：点击遮罩关闭；
  - `link`：点击对话框里的链接关闭，适合放导航的抽屉。
  
  按 Esc 关闭由浏览器负责，总是有效。
- 对话框里打开的开关，按 Esc 时先关开关，再关对话框。
- 找不到对话框，或目标不是带 `data-gp-dialog` 的 `<dialog>` 时，链接照常跳转。

### 4. 轮播 `data-gp-carousel`

用于首页大图、客户评价、作品图集。

```gotemplate
<section data-gp-carousel data-gp-interval="6000" aria-roledescription="{{T .Ctx "carousel"}}" aria-label="{{T .Ctx "featured"}}">
  {{range .Slides}}<div data-gp-slide>…</div>{{end}}
  <button type="button" data-gp-carousel-prev aria-label="{{T .Ctx "previous"}}">‹</button>
  <button type="button" data-gp-carousel-next aria-label="{{T .Ctx "next"}}">›</button>
  {{range $i, $s := .Slides}}
  <button type="button" data-gp-carousel-to="{{$i}}" aria-label="{{$s.Title}}"></button>
  {{end}}
  <button type="button" data-gp-carousel-pause aria-label="{{T .Ctx "pause"}}">❙❙</button>
</section>
```

- 当前一张带 `data-gp-is-active`，其余带 `aria-hidden="true"` 和 `inert`。页面加载时以带 `data-gp-is-active` 的那张开始，没有就从第一张开始。
- 上一张、下一张循环切换；`data-gp-carousel-to="<序号>"`（从 0 开始）跳到指定一张，当前那个按钮带 `aria-current="true"`。图集的缩略图就写成跳转按钮。
- 焦点在轮播的按钮上时，左右方向键切换。焦点在幻灯片内容里时不切换。
- `data-gp-interval`：自动播放的间隔，毫秒，0 或 2000–60000，默认 0（不自动播放）。自动播放：
  - 鼠标悬停或焦点在轮播里时暂停；
  - 访客开了「减少动态效果」时一开始就是暂停的；
  - 页面在后台时不切换；
  - 暂停按钮切换 `aria-pressed`，访客手动暂停后不再自动恢复。
- 圆点、按钮和它们的无障碍名称由主题写，脚本不生成。切换效果（淡入、滑动）由主题的 CSS 实现：

```css
[data-gp-slide] { opacity: 0; transition: opacity .4s; position: absolute; inset: 0; }
[data-gp-slide][data-gp-is-active] { opacity: 1; position: relative; }
[data-gp-carousel-to][aria-current="true"] { background: var(--accent); }
```

### 5. 滚动状态和回到顶部

```gotemplate
<header data-gp-scroll="48">…</header>
<button type="button" data-gp-scroll="400" data-gp-to-top aria-label="{{T .Ctx "back_to_top"}}">↑</button>
```

- `data-gp-scroll="<像素>"`（0–5000）：页面滚过这个距离后，元素带 `data-gp-is-past`。适合缩小头部、显示回到顶部按钮。
- `data-gp-to-top`：点击后回到页面顶部，焦点移到 `<main>`（脚本会给它加 `tabindex="-1"`）。

```css
.site-header[data-gp-is-past] { box-shadow: 0 1px 8px rgb(0 0 0 / .08); }
.to-top:not([data-gp-is-past]) { visibility: hidden; opacity: 0; }
```

### 6. 渐显 `data-gp-reveal`

元素第一次进入视口时带 `data-gp-is-visible`，之后不再变化。隐藏样式要以 `html[data-gp-ready]` 为前提，这样没有脚本时内容照常显示：

```css
@media (prefers-reduced-motion: no-preference) {
  html[data-gp-ready] [data-gp-reveal]:not([data-gp-is-visible]) { opacity: 0; transform: translateY(16px); }
  [data-gp-reveal] { transition: opacity .5s, transform .5s; }
}
```

访客开了「减少动态效果」，或者浏览器不支持 `IntersectionObserver` 时，元素直接显示。打印前所有元素都会显示。

### 7. 阅读进度 `data-gp-progress`

脚本在元素上设置 CSS 变量 `--gp-progress`，取值 0 到 1：

- 属性留空时按整页计算；
- 写一个元素的 ID 时按那个元素计算：元素顶部到达视口顶部时为 0，元素底部进入视口时为 1。

```gotemplate
<div class="progress" data-gp-progress="qr-article" aria-hidden="true"></div>
<article id="qr-article">…</article>
```

```css
.progress { position: fixed; inset: 0 0 auto; height: 3px; background: var(--accent);
  transform-origin: left; transform: scaleX(var(--gp-progress, 0)); }
```

进度条只是装饰，加上 `aria-hidden="true"`。

### 8. 复制 `data-gp-copy`

```gotemplate
<input id="qr-share-url" value="{{.CanonicalURL}}" readonly>
<button type="button" data-gp-copy="qr-share-url" data-gp-copy-done="qr-copied">{{T .Ctx "copy_link"}}</button>
<span role="status"><span id="qr-copied" hidden>{{T .Ctx "copied"}}</span></span>
```

- 点击后把目标的内容写进剪贴板：输入框和文本框取它的值，其他元素取它的文字（去掉首尾空白）。
- 成功后，按钮带 `data-gp-is-done`，`data-gp-copy-done` 指向的提示元素去掉 `hidden`，3 秒后恢复。把提示放在一个始终存在的 `role="status"` 元素里，屏幕阅读器会读出它。
- 浏览器不允许写剪贴板时（例如不是 HTTPS），脚本选中目标的内容，让访客自己复制。

## 用 HTML 和 CSS 实现的交互

下面这些不需要脚本：

| 需求 | 写法 |
|---|---|
| 平滑滚动到锚点 | `@media (prefers-reduced-motion: no-preference) { html { scroll-behavior: smooth; } }` |
| 表单校验 | `required`、`type="email"`、`pattern`，用 `:user-invalid` 写错误样式 |
| 大菜单、下拉的悬停 | `:hover`、`:focus-within`，加透明的 `::before` 悬停桥或 `transition-delay` |
| 手风琴、常见问题 | `<details>`；同一组只开一个时写 `<details name="faq">` |
| 脚注、小提示 | `popover` 属性和 `popovertarget` 按钮 |
| 横向滚动的列表 | `overflow-x: auto` 和 `scroll-snap-type` |
| 分享到社交网站 | 普通链接，`target="_blank" rel="noopener"` |

标签页暂时没有组件，可以用 `<details>`，或者用开关组件加 `data-gp-group`。

## 组件不做的事

- 不生成元素和文字：不用 `innerHTML` 一类的写法，圆点、按钮、提示语都由主题在模板里写，用主题自己的翻译；
- 不把属性值当代码或地址使用，不读链接地址；
- 不发网络请求，不用 Cookie 和本地存储；
- 只认主题模板里写的 `data-gp-*` 属性。

## 浏览器支持

近三年发布的 Chrome、Edge、Firefox 和 Safari（包括 iOS）。用到原生 `<dialog>`、`inert`、`IntersectionObserver` 和剪贴板接口。

更老的浏览器不会报错，只是没有交互。主题的 CSS 要保证这时导航仍然可用，例如宽屏的菜单靠 `:hover` 和 `:focus-within` 展开。

## 属性速查

| 属性 | 写在 | 值 |
|---|---|---|
| `data-gp-toggle` | 开关 | 目标 ID |
| `data-gp-dismiss` | 开关的目标、对话框 | `escape`、`outside`、`link`，可组合 |
| `data-gp-group` | 开关的目标 | 组名 |
| `data-gp-autofocus` | 目标或对话框里的元素 | 无 |
| `data-gp-menu` | 导航列表 | 无 |
| `data-gp-menu-below` | 导航列表 | 320–2000，默认 900 |
| `data-gp-dialog` | `<dialog>` | 无 |
| `data-gp-dialog-open` | 链接或按钮 | 对话框 ID |
| `data-gp-dialog-close` | 对话框里的按钮 | 无 |
| `data-gp-carousel` | 轮播容器 | 无 |
| `data-gp-interval` | 轮播容器 | 0 或 2000–60000，默认 0 |
| `data-gp-slide` | 幻灯片 | 无 |
| `data-gp-carousel-prev`、`-next`、`-pause` | 轮播里的按钮 | 无 |
| `data-gp-carousel-to` | 轮播里的按钮 | 从 0 开始的序号 |
| `data-gp-scroll` | 任意元素 | 0–5000 |
| `data-gp-to-top` | 按钮 | 无 |
| `data-gp-reveal` | 任意元素 | 无 |
| `data-gp-progress` | 进度条 | 留空，或元素 ID |
| `data-gp-copy` | 按钮 | 来源元素 ID |
| `data-gp-copy-done` | 复制按钮 | 提示元素 ID |

| 状态属性 | 出现在 |
|---|---|
| `data-gp-ready`、`data-gp-is-locked` | `<html>` |
| `data-gp-is-open` | 打开的开关目标、窄屏展开的 `li` |
| `data-gp-is-active` | 当前幻灯片 |
| `data-gp-is-past` | 滚过距离的元素 |
| `data-gp-is-visible` | 已显示的渐显元素 |
| `data-gp-is-done` | 刚复制成功的按钮 |
