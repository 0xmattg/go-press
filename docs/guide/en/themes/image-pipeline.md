# Theme Image Integration

Frontend templates should not output original upload URLs directly when a responsive image helper can be used. Core helpers let all themes share media variants, WebP fallback, loading priority, and stable markup.

## Helpers

```gotemplate
{{responsiveImage .ImageURL .Title "card-image" "(max-width: 768px) 100vw, 33vw" "lazy"}}
{{responsiveImagePriority .ImageURL .Title "hero-image" "100vw"}}
{{responsiveImagePreload (settingOr .Settings "home_hero_1_image" "") "100vw"}}
```

Arguments are original image URL, alt text, CSS class, `sizes`, and loading mode.

## CSS Contract

Treat generated images as normal `<img>` elements:

```css
.card-image {
  width: 100%;
  height: 180px;
  object-fit: cover;
  display: block;
}
```

Use fixed dimensions or `aspect-ratio` to avoid layout shifts.

## Avoid Inline Background Images

Prefer absolutely positioned `<img>` elements for hero backgrounds. Inline `background-image` prevents the browser from using `srcset`, preload, and fetch priority effectively.

```gotemplate
<div class="hero">
  {{responsiveImagePriority .Hero.ImageURL .Hero.Title "hero-bg" "100vw"}}
  <div class="hero-content">...</div>
</div>
```

```css
.hero { position: relative; }
.hero-bg { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; }
.hero-content { position: relative; z-index: 1; }
```

## Output Behavior

- Local uploads with variants render as `<picture>`.
- WebP sources are used when a suitable WebP variant exists.
- JPG/PNG fallback variants include the original image as the final candidate.
- A WebP candidate set is preferred only when its maximum width is at least the
  fallback set's maximum width; `*-full.webp` provides a ceiling candidate for
  large or high-DPR displays.
- External images render as plain `<img>`.
- Missing variants do not break the page; the original URL is used.
- `responsiveImagePriority` sets eager loading and high fetch priority for LCP
  media; `responsiveImagePreload` exposes the same source in `<head>`.

See [Media Variants](media-variants.md) for the generation pipeline.
