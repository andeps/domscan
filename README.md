# 域见后端

基于 Go、Gin 和 GORM 的域名发现 API。服务根据关键词、主体长度、顶级域名和数字规则生成候选域名，通过 IANA RDAP Bootstrap 路由到权威注册局查询，并以 NDJSON 流式返回结果。

本仓库只提供后端 API，不嵌入或托管前端资源。前端应独立开发和部署。

## 环境要求

- Go 1.25+
- PostgreSQL（用户注册、登录和邮件通知需要；Compose 使用 17）
- Redis（可选，用于 RDAP 结果缓存；Compose 使用 8.2）

## 快速启动

复制并按需修改配置：

```bash
cp .env.example .env
docker compose up -d postgres
go run .
```

服务默认监听 `:8080`，健康检查：

```bash
curl http://localhost:8080/api/health
```

编译运行：

```bash
go build -o bin/domscan .
./bin/domscan
```

`-addr` 可以临时覆盖监听地址：

```bash
./bin/domscan -addr :9090
```

不需要用户模块时可设置 `DOMAIN_DATABASE_ENABLED=false`。数据库不可用时服务会保留域名检测 API，但禁用注册、登录和邮件通知。

## API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/health` | 服务和缓存状态 |
| `POST` | `/api/generate` | 仅生成候选域名 |
| `POST` | `/api/check` | 检测指定域名，返回 NDJSON |
| `POST` | `/api/search` | 生成并检测新候选，返回 NDJSON；用户模块启用时需要登录 |
| `POST` | `/api/auth/register` | 邮箱注册 |
| `POST` | `/api/auth/login` | 登录并获取 JWT |
| `PUT` | `/api/auth/email` | 修改邮箱并返回新 JWT |
| `PUT` | `/api/auth/password` | 修改密码 |
| `PUT` | `/api/auth/avatar` | 修改头像 |

认证接口使用：

```http
Authorization: Bearer <token>
```

请求示例：

```text
POST /api/auth/register  {"email":"user@example.com","password":"至少 8 位"}
POST /api/auth/login     {"email":"user@example.com","password":"..."}
PUT  /api/auth/email     {"newEmail":"next@example.com"}
PUT  /api/auth/password  {"password":"新的至少 8 位密码"}
PUT  /api/auth/avatar    {"avatar":"data:image/..."}
```

`POST /api/search` 会跳过 Redis 中已有的候选，并通过 `offset` 继续生成，直到返回请求数量的新结果。每检测完成一个域名，服务端会立即写入一行 JSON 并刷新响应，不等待整批完成；最后一行是 `event: "summary"` 汇总，不能作为域名结果解析。`POST /api/check` 和 `/api/search` 的响应类型均为 `application/x-ndjson`，前端应通过 `ReadableStream` 或逐行读取方式消费，不能使用等待完整响应的 `response.json()`。

单次生成或检测最多 1000 个候选，请求并发上限为 20。客户端取消请求后，服务端会停止后续生成和查询。

## 检测规则

- RDAP HTTP 200：`registered`
- RDAP HTTP 404：`available`
- 超时、限流或其他异常：`unknown`
- 已注册结果会尽可能解析 RDAP expiration 事件
- DNS 无记录不代表域名未注册

服务启动时下载 IANA RDAP Bootstrap 表，并优先查询对应顶级域名的权威注册局。Bootstrap 不可用或查询失败时使用 `DOMAIN_RDAP_FALLBACK_URLS` 中的备用源。

“可注册”仅代表查询时未发现 RDAP 注册记录，不构成注册保证。保留域名、溢价域名或注册局状态变化仍可能导致无法注册，下单前应以注册商实时结果为准。

## 配置

服务依次读取 `.env` 和系统环境变量，系统环境变量优先。关闭某个可选功能后，该功能的连接参数和超时配置不会参与校验。

### HTTP 与 RDAP

| 配置项 | 默认值 | 用途 |
| --- | --- | --- |
| `DOMAIN_TOOL_ADDR` | `:8080` | HTTP 监听地址 |
| `DOMAIN_GIN_MODE` | `release` | Gin 模式：`debug`、`release` 或 `test` |
| `DOMAIN_HTTP_READ_HEADER_TIMEOUT` | `5s` | 请求头读取超时 |
| `DOMAIN_HTTP_IDLE_TIMEOUT` | `60s` | HTTP 空闲连接超时 |
| `DOMAIN_HTTP_SHUTDOWN_TIMEOUT` | `5s` | 优雅关闭超时 |
| `DOMAIN_HTTP_REQUEST_TIMEOUT` | `15m` | 整个批量请求的最长时间 |
| `DOMAIN_RDAP_BOOTSTRAP_URL` | IANA DNS Bootstrap | 权威 RDAP 路由表 |
| `DOMAIN_RDAP_FALLBACK_URLS` | ARIN Bootstrap、rdap.org | 逗号分隔的备用检测源 |
| `DOMAIN_RDAP_PROVIDER_CONCURRENCY` | `3` | 每个检测源的并发上限，范围 1–20 |
| `DOMAIN_RDAP_TIMEOUT` | `12s` | 单次 RDAP 请求超时 |

### 用户数据库

| 配置项 | 默认值 | 用途 |
| --- | --- | --- |
| `DOMAIN_DATABASE_ENABLED` | `true` | 是否启用用户模块 |
| `DOMAIN_DATABASE_URL` | 本地 `domscan` PostgreSQL | GORM PostgreSQL 连接串 |
| `DOMAIN_JWT_SECRET` | 内置开发值 | JWT 签名密钥，至少 32 字符；生产环境必须修改 |

服务启动时通过 GORM 自动迁移 `users` 表。密码只保存 bcrypt 哈希。

### Redis 缓存

| 配置项 | 默认值 | 用途 |
| --- | --- | --- |
| `DOMAIN_REDIS_ENABLED` | `false` | 是否启用 Redis 读穿缓存 |
| `DOMAIN_REDIS_URL` | `redis://localhost:6379/0` | Redis URL，支持密码、ACL 用户和 TLS |
| `DOMAIN_REDIS_KEY_PREFIX` | `domscan:availability:` | 缓存 Key 前缀 |
| `DOMAIN_REDIS_DIAL_TIMEOUT` | `2s` | Redis 连接与读写超时 |
| `DOMAIN_CACHE_TTL` | `24h` | `available`、`registered` 缓存时间 |
| `DOMAIN_CACHE_UNKNOWN_TTL` | `5m` | `unknown` 缓存时间 |

启动本地 Redis：

```bash
docker compose up -d redis
docker compose logs -f redis
```

Redis 连接失败时自动降级为直接查询 RDAP。缓存命中的结果会设置 `cached: true`。

### 邮件通知

| 配置项 | 默认值 | 用途 |
| --- | --- | --- |
| `DOMAIN_EMAIL_ENABLED` | `false` | 是否向当前登录用户发送新发现的可注册域名 |
| `DOMAIN_SMTP_HOST` | 空 | SMTP 主机 |
| `DOMAIN_SMTP_PORT` | `587` | SMTP 端口；465 使用隐式 TLS |
| `DOMAIN_SMTP_USERNAME` | 空 | SMTP 用户名；留空表示无需认证 |
| `DOMAIN_SMTP_PASSWORD` | 空 | SMTP 密码 |
| `DOMAIN_SMTP_FROM` | 空 | 发件人地址 |
| `DOMAIN_EMAIL_TIMEOUT` | `15s` | 单批邮件发送超时 |
| `DOMAIN_EMAIL_BATCH_SIZE` | `10` | 每批包含的可注册域名数量 |

邮件通知依赖数据库用户模块。收件人不从配置文件读取，而是使用 `/api/search` 当前 JWT 对应的登录邮箱；匿名请求、缓存命中和 `unknown`/`registered` 结果不会发送邮件。

## 项目结构

```text
main.go                   进程入口、依赖初始化和优雅退出
config/                   配置加载与校验
domain/                   候选域名生成和格式校验
availability/             RDAP 查询、IANA 路由、回退与缓存策略
rediscache/               Redis 缓存存储
notification/             SMTP 邮件通知
user/                     GORM 用户仓库、JWT 和 HTTP Handler
httpserver/               Gin API、并发调度和中间件
```

## 前端接入

后端不提供静态资源。开发环境可由前端开发服务器代理 `/api` 到 `http://localhost:8080`；不同源直连时需要在网关或后端配置 CORS。

## 验证

```bash
go test ./...
go vet ./...
go build ./...
```
