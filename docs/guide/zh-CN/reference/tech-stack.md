# 技术栈与性能

## 技术栈

| 组件 | 选型 | 说明 |
|------|------|------|
| Web 框架 | [Gin](https://github.com/gin-gonic/gin) | 高性能 HTTP 框架 |
| ORM | [GORM](https://gorm.io) | 自动迁移、预加载、软删除、NamingStrategy |
| 数据库 | PostgreSQL | 主数据存储，表前缀隔离 |
| 缓存 | Redis + 内存 LRU | L1/L2 多级缓存，自动降级 |
| 认证 | [golang-jwt](https://github.com/golang-jwt/jwt) | JWT Bearer Token + API Key |
| 配置 | [Viper](https://github.com/spf13/viper) + TOML | 声明式配置，多站点支持 |
| 日志 | `log/slog` | Go 标准库结构化日志 |
| 国际化 | [go-i18n](https://github.com/nicksnyder/go-i18n) | 多语言翻译 |
| 富文本编辑器 | [Quill 2.0](https://quilljs.com) | MIT 协议，CDN 加载 |
| API 文档 | [swaggo/swag](https://github.com/swaggo/swag) | 代码注解生成 OpenAPI |

## 性能目标

以下目标用于指导架构设计和后续压测优化，公开发布前需要补充可复现的 benchmark、测试环境说明和压测脚本。

| 指标 | 目标值 |
|------|--------|
| 页面缓存命中响应 | < 1ms |
| 首次渲染（无缓存） | < 50ms |
| 并发连接数 | 50,000+ |
| QPS（缓存命中） | 100,000+ |
| QPS（无缓存） | 5,000+ |
| 内存占用（空闲） | < 50MB |

## 与 WordPress 对比

这张表用于说明 GoPress 的设计取舍，不用于评价不同技术栈的绝对优劣。

| 维度 | WordPress (PHP) | GoPress (Go) |
|------|-----------------|--------------|
| 运行方式 | PHP-FPM / Web Server 组合，围绕请求生命周期运行 | Go 单进程服务，适合常驻内存模型 |
| 扩展方式 | 主题与插件生态成熟，运行时动态加载灵活 | Go 接口与 Hook 注册，强调类型安全和可维护性 |
| 缓存策略 | 通常通过插件、对象缓存或反向代理组合增强 | 内置内存 / Redis / 数据库多级缓存路径 |
| 定时任务 | 常见方案包括 WP-Cron 或系统 Cron | 由服务进程内的调度器执行 |
| 部署形态 | Web Server、PHP 运行时、数据库等多组件协作 | 编译后以单一服务进程交付，外接数据库与可选 Redis |
