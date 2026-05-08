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

## Output Behavior

- Local uploads with variants render as `<picture>`.
- WebP sources are used when a suitable WebP variant exists.
- JPG/PNG fallback variants include the original image as the final candidate.
- External images render as plain `<img>`.
- Missing variants do not break the page; the original URL is used.

See [Media Variants](media-variants.md) for the generation pipeline.

