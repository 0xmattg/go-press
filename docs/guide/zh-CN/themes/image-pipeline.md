# 主题图片接入规范

主题前台模板**不要**直接输出原图 URL，也**不要**把动态图片写成内联 `background-image:url('{{.ImageURL}}')`。统一使用核心图片 helper，让所有主题共享媒体变体、WebP fallback 和首屏图片优先级策略。

## 核心 helper

```gotemplate
{{/* 列表/卡片图：懒加载，按容器宽度选择变体 */}}
{{responsiveImage .ImageURL .Title "card-image" "(max-width: 768px) 100vw, 33vw" "lazy"}}

{{/* 首屏/LCP 图：eager + fetchpriority=high */}}
{{responsiveImagePriority .ImageURL .Title "hero-image" "100vw"}}

{{/* 布局 head 中预加载首屏图 */}}
{{responsiveImagePreload (settingOr .Settings "home_hero_1_image" "") "100vw"}}
```

参数依次为：原图 URL、alt 文字、CSS class、`sizes` 属性、`loading` 属性。

## CSS 处理约定

CSS 侧把这些图片当真实 `<img>` 处理，使用固定尺寸或 `aspect-ratio` + `object-fit: cover` 保持布局稳定：

```css
.card-image {
    width: 100%;
    height: 180px;
    object-fit: cover;
    display: block;
}
```

## 不要写 `background-image`

如果设计上必须保留背景图效果，也优先用绝对定位的 `<img>` 作为背景层，而不是内联 `background-image`。这样浏览器才能参与资源优先级、`srcset` 选择和 preload。

```html
<!-- ✅ 正确 -->
<div class="hero" style="position:relative;">
    {{responsiveImagePriority .Hero.ImageURL .Hero.Title "hero-bg" "100vw"}}
    <div class="hero-content">...</div>
</div>

<style>
.hero-bg { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; z-index: 0; }
.hero-content { position: relative; z-index: 1; }
</style>
```

```html
<!-- ❌ 错误：浏览器无法做 srcset / fetchpriority -->
<div class="hero" style="background-image: url('{{.Hero.ImageURL}}');">...</div>
```

## 输出逻辑

`responsiveImage` 系列 helper 会根据图片是否在本地、是否生成了变体来决定输出形式：

- 本地 `/static/uploads/...` 且存在 variants：输出 `<picture>`，WebP `<source>` 优先，`img` fallback 到 JPG/PNG 变体
- 非 WebP fallback 的 `srcset` 会把原图作为最后候选，避免没有合适变体时把较小图片放大
- WebP 候选集只有在最大宽度不小于 fallback 候选集时才优先输出；安装 `cwebp` 后会生成 `{hash}-full.webp`，用于高 DPR 或大图框场景的 ceiling 候选
- 本地图片但尚未生成 variants：输出普通 `<img>`，保留原 URL，页面不报错
- 外链图片：输出普通 `<img>`，不尝试本地变体
- `responsiveImagePriority` 用于首屏 LCP 图，默认 `loading="eager"` + `fetchpriority="high"`
- `responsiveImagePreload` 用于在布局 `<head>` 里提前发现首屏图片

详细的变体生成机制见 [媒体变体管线](media-variants.md)。
