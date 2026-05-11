# 安装与运行

## 环境要求

- Go 1.25+
- PostgreSQL 14+
- Redis 7+（可选，无 Redis 时自动降级为纯内存缓存）
- `cwebp`（可选，用于生成 WebP 变体；缺失时自动回退为 JPG/PNG 变体）

## 安装步骤

```bash
# 克隆项目
git clone https://github.com/0xmattg/go-press.git
cd go-press

# 安装依赖
go mod download

# 启动服务（首次启动进入 Web 安装器）
go run ./cmd/server/

# 或指定已有站点配置
go run ./cmd/server/ -config sites/localhost/config.toml
```

启动后访问：

| 地址 | 说明 |
|------|------|
| `http://localhost:8080` | 前台网站 |
| `http://localhost:8080/admin` | 后台 CMS |
| `http://localhost:8080/swagger/index.html` | API 文档 |
| `http://localhost:8080/api/v1/content` | REST API |

## Web 安装器

第一次运行时，由于尚未生成站点配置，引擎会进入 Web 安装器模式：

- **两步引导** — 数据库配置（含表前缀、自动创建库）→ 站点信息设置
- **热切换** — 安装完成后自动从安装器模式切换到正常运行模式，无需重启
- **安全** — 配置文件写入权限 `0600`，安装完成后自动锁定

完成安装后，配置文件会被写入 `sites/<your-site>/config.toml`，下次启动直接以此为入口。

## 媒体处理依赖

Go 标准库负责 JPG/PNG decode/encode 和 resize。WebP 编码依赖系统命令 `cwebp`：

```bash
# macOS
brew install webp

# Debian/Ubuntu
apt-get install webp
```

如果运行环境没有 `cwebp`，系统仍会生成 JPG/PNG resize 变体，只是不会生成 WebP；模板会自动回退到非 WebP 版本。详见 [媒体变体管线](../themes/media-variants.md)。
