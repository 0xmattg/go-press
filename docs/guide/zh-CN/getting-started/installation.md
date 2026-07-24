# 安装与运行

## 环境要求

- Go 1.25+
- PostgreSQL 14+
- Redis 7+（可选，无 Redis 时自动降级为纯内存缓存）
- `cwebp`（可选，用于生成 WebP 变体；缺失时自动回退为 JPG/PNG 变体）

## `gopress` CLI 简介

GoPress 自带一个小型编排二进制 `gopress`，它包装了 server 入口。每次启动都会扫描 themes 与 plugins 目录，所以新增扩展时**不需要手动改 `cmd/server/main.go`**。

`themes/` 或 `plugins/` 下的目录被识别需要同时满足：

- 根目录有标记文件：主题是 `theme.toml`，插件是 `plugin.toml`
- 根目录至少有一个非 test 的 `.go` 文件

CLI 三个子命令：

| 命令 | 作用 |
|---|---|
| `gopress serve [flags...]` | 重新生成 autoload 并启动服务。任何 flag 都会原样透传给 `cmd/server`（例如 `-config`、`-seed`）。终止信号会转发给子进程，沿用 graceful shutdown。 |
| `gopress build [-o path]` | 重新生成 autoload 并 `go build` 出一个 server 二进制，默认输出到 `build/gopress-server`。 |
| `gopress gen` | 只重新生成 autoload 包——适合 IDE 或 CI 钩子调用。 |

## 安装步骤

最快上手方式是本地 `make gopress` 构建——先试一下不必全局安装。

```bash
# 克隆项目
git clone https://github.com/0xmattg/go-press.git
cd go-press

# 安装依赖
go mod download

# 把 gopress CLI 构建到 ./build/（不需要全局安装）
make gopress

# 启动服务（首次启动进入 Web 安装器）
./build/gopress serve
```

如果长期使用，可以把 CLI 装到 `$PATH` 上，省掉 `./build/` 前缀：

```bash
make install      # 装到 $GOBIN（或 $GOPATH/bin）
gopress serve     # 装完之后任意目录都能跑
```

如果 `$GOBIN` 还不在 `$PATH` 上，`make install` 会打印需要加到 shell rc 的 `export PATH=...` 一行。

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
- **安全** — 配置文件写入权限 `0600`，安装完成后自动锁定；`jwt_secret` 由安装器自动生成为唯一随机值（无需手动填写）

完成安装后，配置文件会被写入 `sites/<your-site>/config.toml`，下次启动直接以此为入口。

## 构建生产二进制

```bash
./build/gopress build                  # -> build/gopress-server
./build/gopress build -o ./myserver    # 自定义输出
./build/gopress-server
```

（如果跑过 `make install`，可以省掉 `./build/` 前缀。）

`gopress build` 先重新生成 `internal/autoload/autoload_gen.go`，再执行 `go build ./cmd/server`，最终二进制里已经把当前的全部 theme/plugin 编译进去——线上部署不依赖 Go 工具链做任何运行时发现。

### 小内存机器编译

`go build` 默认按 `GOMAXPROCS` 个 CPU 并行编译，每个 worker 占用的内存不小。在 1c1g 的小 VPS 上，并行编译经常被 OOM killer 杀掉，或者直接报 `signal: killed`。

遇到这种情况，用 `GOFLAGS` 强制串行编译即可。Go 工具链会自动识别这个环境变量，所以链路中每一步都生效—— `make gopress`、`gopress serve`、`gopress build`、直接的 `go build` 都吃：

```bash
GOFLAGS="-p=1"    make gopress              # 串行编译 CLI 本身
GOFLAGS="-p=1"    ./build/gopress build     # 串行编译 server
GOFLAGS="-p=1"    ./build/gopress serve     # 串行编译后启动
GOFLAGS="-p=1 -v" ./build/gopress build     # 加 -v 让编译过程逐包打印，避免看上去卡死
```

不想走 `gopress`、想直接调 `go build` 的话（记得先 `./build/gopress gen` 刷新 autoload）：

```bash
go build -p 1 -v -o build/gopress-server ./cmd/server
```

`-p 1` 把并行度限制为 1，`-v` 让每编译完一个包就打印一行；代价是编译时间变长，换来的是内存峰值大幅下降。

## Make 目标速查

| 目标 | 用途 |
|---|---|
| `make help` | 列出全部目标（无参数 / 错误目标也会展示同样的帮助）。 |
| `make gopress` | 构建 gopress CLI 到 `build/gopress`。 |
| `make server` | 通过 `gopress build` 构建 server 二进制。 |
| `make gen` | 只重新生成 `internal/autoload`。 |
| `make install` | `go install ./cmd/gopress`（把 `gopress` 放到 `$GOBIN`）。 |
| `make uninstall` | 移除已安装的 `gopress` 二进制。 |
| `make clean` | 清掉 `build/` 目录。 |

## 常用开发命令

下面例子统一用 `./build/gopress`（本地构建）。如果跑过 `make install`，可以省掉 `./build/` 前缀。

```bash
# 启动 server（每次启动都会刷新 autoload）
./build/gopress serve

# 透传 flag 给 cmd/server
./build/gopress serve -config sites/localhost/config.toml
./build/gopress serve -seed

# 只刷新 autoload（不启动任何东西）
./build/gopress gen

# 生成 Swagger 文档
go run ./cmd/gendoc

# 跑测试
go test ./...
```

## 新增主题或插件

1. 把目录拖到 `themes/` 或 `plugins/`。
2. 确保该目录根有 `theme.toml`（主题）或 `plugin.toml`（插件），并且根有至少一个非 test 的 `.go` 文件。
3. 重新执行 `./build/gopress serve` —— autoload 自动重新生成，新模块在启动时被自动 import。

**不需要修改 `cmd/server/main.go`。**

## 媒体处理依赖

Go 标准库负责 JPG/PNG decode/encode 和 resize。WebP 编码依赖系统命令 `cwebp`：

```bash
# macOS
brew install webp

# Debian/Ubuntu
apt-get install webp
```

如果运行环境没有 `cwebp`，系统仍会生成 JPG/PNG resize 变体，只是不会生成 WebP；模板会自动回退到非 WebP 版本。详见 [媒体变体管线](../themes/media-variants.md)。
