# 媒体变体管线

GoPress 的媒体系统在上传阶段生成前台可直接使用的响应式图片变体，解决移动端直接加载原图导致的 LCP、总传输体积和缓存命中问题。

## 核心流程

```
后台上传 JPEG/PNG
    ├→ 保存原图到 uploads/YYYY/MM/{hash}.jpg
    ├→ 读取并写入原图 width/height 到 gp_media
    ├→ 生成同目录 resize 变体
    │    ├→ {hash}-thumb.jpg
    │    ├→ {hash}-480w.jpg
    │    ├→ {hash}-768w.jpg
    │    ├→ {hash}-1024w.jpg
    │    └→ {hash}-1440w.jpg
    ├→ 如运行环境存在 cwebp，同时生成 WebP
    │    ├→ {hash}-480w.webp
    │    ├→ {hash}-1440w.webp
    │    └→ {hash}-full.webp
    └→ 写入 gp_media_variants，模板按原图 URL 自动查找 variants
```

## 关键模块

| 模块 | 责任 |
|---|---|
| `core/media/media.go` | 原始媒体模型，保存原图 URL、mime、宽高等基础信息 |
| `core/media/variant.go` | `MediaVariant` 模型，记录每个派生图的尺寸、格式、路径、大小 |
| `core/media/image.go` | 图片尺寸计算、resize、同格式变体写入、WebP 变体写入 |
| `core/media/repository.go` | `FindByPath`、`ListVariants`、`UpsertVariant` 等查询/维护 API |
| `core/admin/service.go` | 上传后生成变体；删除媒体时清理变体；重建历史图片变体 |
| `core/theme/images.go` | `responsiveImage` / `responsiveImagePriority` / `responsiveImagePreload` 模板输出 |
| `pkg/middleware/middleware.go` | `/static/uploads/` 长缓存，带版本参数的静态资源长缓存 |

## 存储约定

原图和变体放在同一个年月目录，方便备份、迁移和静态文件服务：

```text
uploads/2026/04/82b1502e3773c17831542303ee65dc1e.png
uploads/2026/04/82b1502e3773c17831542303ee65dc1e-480w.webp
uploads/2026/04/82b1502e3773c17831542303ee65dc1e-768w.webp
uploads/2026/04/82b1502e3773c17831542303ee65dc1e-1024w.webp
uploads/2026/04/82b1502e3773c17831542303ee65dc1e-1440w.webp
uploads/2026/04/82b1502e3773c17831542303ee65dc1e-full.webp
```

公开 URL 对应为 `/static/uploads/YYYY/MM/...`。`gp_media.path` 始终指向原图，`gp_media_variants.path` 指向派生图。**内容字段和主题设置继续保存原图 URL，不需要迁移旧内容。**

## 模板调用

主题应优先使用核心 helper（详见 [图片接入规范](image-pipeline.md)）：

```gotemplate
{{responsiveImage .ImageURL .Title "card-image" "(max-width: 768px) 100vw, 33vw" "lazy"}}
{{responsiveImagePriority .ImageURL .Title "hero-image" "100vw"}}
{{responsiveImagePreload (settingOr .Settings "home_hero_1_image" "") "100vw"}}
```

## 历史图片重建

生产环境已有老图无需重新上传。部署新版本并完成 AutoMigrate 后，在后台「媒体库」点击：

- **「补齐图片变体」** — 扫描 `gp_media` 中已有 JPEG/PNG，补生成缺失变体并写入 `gp_media_variants`。如果环境新增了 `cwebp`，补齐任务也会为已有图片补 `{hash}-full.webp`
- **「强制重建图片变体」** — 删除并重建全部变体

## 生产环境依赖

Go 标准库负责 JPG/PNG decode/encode 和 resize。WebP 编码依赖系统命令 `cwebp`：

```bash
# macOS
brew install webp

# Debian/Ubuntu
apt-get install webp
```

如果运行环境没有 `cwebp`，系统仍会生成 JPG/PNG resize 变体，只是不会生成 WebP；模板会自动回退到非 WebP 版本。
