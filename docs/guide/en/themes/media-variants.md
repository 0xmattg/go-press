# Media Variants

GoPress generates responsive image variants during upload so frontend pages can load appropriate image sizes instead of serving the original file to every viewport.

## Pipeline

```text
upload JPEG/PNG
  -> save original to uploads/YYYY/MM/{hash}.jpg
  -> record width and height in gp_media
  -> generate resized variants
  -> generate WebP variants when cwebp exists
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

## Storage Convention

Originals and variants live in the same year/month folder:

```text
uploads/2026/04/example.png
uploads/2026/04/example-480w.webp
uploads/2026/04/example-1024w.png
uploads/2026/04/example-full.webp
```

Content fields and theme settings continue to store the original URL. Existing content does not need to be migrated.

## Rebuilding Historical Images

The media library provides actions to generate missing variants or force-rebuild all variants for existing uploads. This is useful after enabling `cwebp` or deploying the media variant feature to a site with old images.

## Dependency

WebP generation depends on the `cwebp` command:

```bash
brew install webp
apt-get install webp
```

Without `cwebp`, GoPress still generates JPG/PNG resized variants and templates automatically fall back.

