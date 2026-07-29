# Media Variants

GoPress generates responsive image variants during upload so frontend pages can load appropriate image sizes instead of serving the original file to every viewport.

## Pipeline

```text
upload JPEG/PNG
  -> save original to uploads/YYYY/MM/{hash}.jpg
  -> record width and height in gp_media
  -> generate thumb, 480w, 768w, 1024w, and 1440w resized variants
  -> generate matching WebP variants and a full-width WebP when cwebp exists
  -> write gp_media_variants
  -> templates resolve variants by original URL
```

## Key Modules

| Module | Responsibility |
|---|---|
| `core/media/media.go` | Original media model. |
| `core/media/variant.go` | Variant model. |
| `core/media/image.go` | Resize and WebP generation. |
| `core/media/repository.go` | Variant lookup and maintenance. |
| `core/admin/service.go` | Upload, delete, and rebuild workflows. |
| `core/theme/images.go` | Responsive image template helpers. |
| `core/engine.go` | Versioned static/upload cache headers and matching GET/HEAD behavior. |

## Storage Convention

Originals and variants live in the same year/month folder:

```text
uploads/2026/04/example.png
uploads/2026/04/example-480w.webp
uploads/2026/04/example-1024w.png
uploads/2026/04/example-full.webp
```

Content fields and theme settings continue to store the original URL. Existing content does not need to be migrated.

Public URLs use `/static/uploads/YYYY/MM/...`; `gp_media.path` points to the
original and `gp_media_variants.path` records each derivative.

## Template Usage

```gotemplate
{{responsiveImage .ImageURL .Title "card-image" "(max-width: 768px) 100vw, 33vw" "lazy"}}
{{responsiveImagePriority .ImageURL .Title "hero-image" "100vw"}}
{{responsiveImagePreload (settingOr .Settings "home_hero_1_image" "") "100vw"}}
```

## Rebuilding Historical Images

The media library provides two operations:

- **Generate missing variants** scans existing JPEG/PNG rows and adds only
  derivatives that are absent. Installing `cwebp` later also adds full WebP
  variants without re-uploading originals.
- **Force rebuild variants** removes and recreates all derivatives.

This supports sites upgraded from a version that stored originals only.

## Dependency

WebP generation depends on the `cwebp` command:

```bash
brew install webp
apt-get install webp
```

Without `cwebp`, GoPress still generates JPG/PNG resized variants and templates automatically fall back.
