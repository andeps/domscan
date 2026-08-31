# 域见 — 域名可用性检测工具

一个基于 Go 和 Gin、无需数据库的本地 Web 工具：根据关键词、域名主体长度、后缀和数字规则生成候选域名，再通过 RDAP 批量查询注册状态，并展示 RDAP 提供的到期时间。

## 启动

需要 Go 1.25 或更高版本：

```bash
go run .
```

浏览器打开 <http://localhost:8080>。也可以编译成单个可执行文件，网页资源会被一起嵌入：

```bash
go build -o bin/domscan .
./bin/domscan
```

需要本地 Redis 缓存时，可以启动项目自带的 Redis 服务：

```bash
make cache-up
make run
```

指定监听地址：

```bash
./bin/domscan -addr :9090
```

## 功能

- 多关键词生成单词、拼接词和可选中划线组合
- 关键词可留空，按长度从 `a` 开始自动枚举（`a...z, aa, ab...`）
- `.com`、`.cn`、`.io` 等多后缀同时检测
- 限制主体最小/最大长度（不计算后缀）
- 禁止、允许或强制包含数字；数字变体范围为 `0` 到 `99`
- 支持关键词前追加、后追加或两侧追加模糊字符；例如 `ai` + 后追加 + 长度 6 会生成 `aiaaaa`、`aiaaab`…
- 每次获取 1000 个“新候选”并发查询且逐条显示结果；已缓存候选自动跳过并继续向后生成
- 已注册域名展示 RDAP 到期时间；不同注册局字段可能缺失
- 检测过程中可以随时点击“停止检测”，中止浏览器请求和服务端工作协程
- 按状态筛选，导出 UTF-8 CSV
- 可注册域名支持点击查看详情
- 严格区分“可注册”“已注册”和“未知”，网络错误不会误报成可注册

## 检测原理与注意事项

程序默认请求 `https://rdap.org/domain/<域名>`。HTTP 200 表示存在注册记录，404 表示未找到记录，其余响应或网络错误显示为“未知”。RDAP 的结果比 DNS 解析更适合判断注册状态，因为已注册域名不一定配置 DNS。

“可注册”仍不构成注册保证：注册局状态可能短暂变化，保留域名和溢价域名也可能无法按普通价格购买。下单前请以注册商实时结果为准。批量过大可能触发公共 RDAP 服务限流，遇到大量“未知”时请降低数量并稍后重试。

## 配置

程序启动时自动读取项目根目录的 `.env`。可以从 [.env.example](.env.example) 创建本地配置；仓库中的实际 `.env` 已被 Git 忽略。系统环境变量优先于 `.env`，命令行 `-addr` 又优先于 `DOMAIN_TOOL_ADDR`。

| 配置项 | 默认值 | 用途 |
| --- | --- | --- |
| `DOMAIN_TOOL_ADDR` | `:8080` | HTTP 监听地址 |
| `DOMAIN_GIN_MODE` | `release` | Gin 的 `debug`、`release` 或 `test` 模式 |
| `DOMAIN_RDAP_BOOTSTRAP_URL` | `https://data.iana.org/rdap/dns.json` | IANA 权威 RDAP 路由表 |
| `DOMAIN_RDAP_FALLBACK_URLS` | ARIN Bootstrap、rdap.org | 备用检测地址，逗号分隔 |
| `DOMAIN_RDAP_PROVIDER_CONCURRENCY` | `3` | 每个检测源的最大并发数 |
| `DOMAIN_RDAP_TIMEOUT` | `12s` | 单次 RDAP 查询超时 |
| `DOMAIN_HTTP_READ_HEADER_TIMEOUT` | `5s` | HTTP 请求头读取超时 |
| `DOMAIN_HTTP_IDLE_TIMEOUT` | `60s` | HTTP 空闲连接超时 |
| `DOMAIN_HTTP_SHUTDOWN_TIMEOUT` | `5s` | 服务优雅关闭超时 |
| `DOMAIN_HTTP_REQUEST_TIMEOUT` | `15m` | 整个批量请求的最长时间 |
| `DOMAIN_REDIS_ENABLED` | `false` | 是否启用 Redis 读穿缓存 |
| `DOMAIN_REDIS_URL` | `redis://localhost:6379/0` | Redis 连接 URL，支持密码、DB 和 TLS |
| `DOMAIN_REDIS_KEY_PREFIX` | `domscan:availability:` | Redis Key 前缀 |
| `DOMAIN_REDIS_DIAL_TIMEOUT` | `2s` | Redis 连接与读写超时 |
| `DOMAIN_CACHE_TTL` | `24h` | 可注册和已注册结果的缓存时间 |
| `DOMAIN_CACHE_UNKNOWN_TTL` | `5m` | 未知结果的缓存时间 |
| `DOMAIN_EMAIL_ENABLED` | `false` | 是否为新发现的可注册域名发送邮件 |
| `DOMAIN_SMTP_HOST` / `DOMAIN_SMTP_PORT` | `smtp.example.com` / `587` | SMTP 服务器 |
| `DOMAIN_SMTP_USERNAME` / `DOMAIN_SMTP_PASSWORD` | 空 | SMTP 登录信息 |
| `DOMAIN_SMTP_FROM` / `DOMAIN_EMAIL_TO` | 空 | 发件人和逗号分隔的收件人 |
| `DOMAIN_EMAIL_TIMEOUT` | `15s` | 邮件发送超时 |
| `DOMAIN_EMAIL_BATCH_SIZE` | `10` | 每批邮件包含的域名数量；结束时不足一批的剩余域名单独发送 |

时间配置使用 Go duration 格式，例如 `500ms`、`12s`、`5m`。如需临时替换 RDAP 服务，也可以直接使用系统环境变量：

```bash
DOMAIN_RDAP_FALLBACK_URLS=https://your-rdap.example/domain/ go run .
```

## 用户注册与登录

项目支持使用邮箱作为账号注册和登录，密码使用 bcrypt 哈希保存。启动 PostgreSQL 后，服务会自动创建 `users` 表，并提供：

```text
POST /api/auth/register  {"email":"user@example.com","password":"至少 8 位"}
POST /api/auth/login     {"email":"user@example.com","password":"..."}
```

默认数据库配置来自 `.env`，可用 `docker compose up -d postgres` 启动 PostgreSQL。生产环境请修改数据库密码和连接串。

## Redis 缓存

启用缓存后，每个域名都会先查询 Redis。命中时直接返回并在页面显示“缓存”，不会再次请求 RDAP；未命中时执行 RDAP 查询并写入 Redis。正常状态和未知状态使用不同 TTL，避免临时网络故障被长期缓存。

可注册结果可以直接点击查看详情，包括主体、后缀、检测源、缓存来源、耗时和检测说明，并支持复制完整域名。启用邮箱后，检测到每个本次新查询的可注册域名就立即在后台发送通知，不阻塞后续检测；缓存命中不会重复通知。

Redis 可以位于本机或远程服务器。使用密码认证但未配置 ACL 用户时，URL 用户名留空：

```dotenv
DOMAIN_REDIS_ENABLED=true
DOMAIN_REDIS_URL=redis://:password@192.168.2.100:6379/0
```

如果 Redis 配置了 ACL 用户，则使用 `redis://username:password@host:port/db`。用户名或密码包含特殊字符时需要进行 URL 编码。Redis 启动连接失败时程序会记录警告并降级为直接 RDAP 查询，不影响主要功能。

本地 Docker Redis 常用命令：

```bash
make cache-up     # 创建并启动 Redis 容器
make cache-logs   # 查看 Redis 日志
make cache-down   # 停止 Redis，数据卷会保留
```

## 项目结构

```text
main.go                   程序入口、配置与优雅退出
config/                   .env 解析、默认值和配置校验
domain/                   候选域名生成和域名格式校验
availability/             RDAP 注册状态检测
rediscache/               Redis 缓存存储实现
httpserver/               Gin API、并发调度、中间件和嵌入式前端
httpserver/web/           HTML、CSS 和 JavaScript
user/                     用户注册、登录和密码安全存储
```

依赖方向为 `cmd → config / httpserver / availability / rediscache`，以及 `httpserver → domain / availability`、`rediscache → availability`。领域生成器不依赖网络和 HTTP，可以单独测试。

`POST /api/search` 是页面使用的主搜索接口。它会读取缓存、跳过已缓存候选，并通过 `offset` 继续生成后续候选；NDJSON 最后一行是 `event=summary` 的跳过统计。`POST /api/check` 仍保留用于直接检测指定域名列表。

## 测试

```bash
go test ./...
```

也可以使用项目命令：

```bash
make check
```
