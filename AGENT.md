# AGENT.md

## 项目目标

“域见”是一个本地 Web 域名发现工具。用户可以按关键词、主体长度、顶级域名和数字规则生成候选域名；关键词留空时，程序从 `a` 开始自动枚举。候选域名随后通过 RDAP 并发查询并以 NDJSON 流式返回。

## 技术基线

- Go 1.25，HTTP Web 框架使用 Gin v1.12。
- 后端只提供 Gin API，不嵌入或托管前端资源；前端独立开发和部署。
- 后端不需要 Node.js 构建步骤。
- 默认通过 IANA RDAP Bootstrap 表路由到权威注册局；`DOMAIN_RDAP_FALLBACK_URLS` 提供备用检测地址。
- 运行配置由 `config` 从 `.env` 和系统环境变量加载；系统环境变量优先。
- 可选 Redis 读穿缓存使用官方 `go-redis/v9`；本地服务由 `compose.yaml` 提供。
- 用户模块使用 GORM、PostgreSQL 和 bcrypt；服务启动时自动迁移用户表。

## 目录职责

```text
main.go                            进程入口、环境变量、HTTP 生命周期
config/                            .env 解析、默认值、类型转换和配置校验
domain/                            候选生成、输入规范化、域名校验
availability/                      检测接口、结果模型、RDAP 实现
rediscache/                        availability.Cache 的 Redis 实现
user/                              用户注册、登录和密码哈希
httpserver/server.go               Gin 路由、中间件、输入限制、并发与流式响应
```

依赖必须保持单向：

```text
main.go
    ├── config
    ├── httpserver
    ├── availability
    └── rediscache

httpserver
    ├── domain
    └── availability

rediscache
    └── availability
```

`domain` 不得依赖 HTTP、RDAP 或其他外部服务。`availability` 不得依赖页面或 HTTP API 层。

## 关键业务不变量

1. 域名长度指第一个点之前的主体长度，不包含后缀。
2. 关键词为空时按长度从短到长枚举；同一长度按 `a` 到 `z` 的顺序递增。
3. 单次生成或检测不得超过 1000 个候选；HTTP 查询并发不得超过 20。
4. RDAP 返回 HTTP 200 才标记为 `registered`，404 才标记为 `available`。超时、限流和其他状态必须是 `unknown`，不能误报为可注册。
5. RDAP `events` 中的 expiration 事件写入结果 `expirationTime`，缺失时保持空值。
5. DNS 无记录不等于域名未注册。不要用 DNS 查询替换 RDAP 注册状态判断。
6. `/api/check` 使用 `application/x-ndjson` 逐行返回结果。修改后端或客户端时必须同时保持这一协议。
7. 所有进入检查器的域名必须先通过 `domain.Valid`；当前仅支持小写 ASCII 域名标签。
9. 缓存命中必须跳过底层 RDAP 检查，并在 API 结果中设置 `cached: true`。
10. `available` 和 `registered` 使用正常缓存 TTL；`unknown` 必须使用较短 TTL，不能永久缓存网络故障。
11. Redis 不可用不得阻止程序提供域名检测服务；启动时应降级为无缓存模式。
12. 多检测源必须遵守逐源并发上限；429、超时和 5xx 才能触发回退，不要无条件复制请求来规避对方限流。
13. 可注册结果详情由客户端从同一条检测结果展示，不能为了详情再次发起一次 RDAP 请求。
14. 客户端必须使用 `POST /api/search`，而不是先固定调用 `/api/generate` 再调用 `/api/check`；搜索接口会跳过缓存并通过偏移继续生成新候选。
15. 邮件通知只发送给发起扫描的当前登录用户，且只包含本次新查询得到的可注册结果；缓存命中、匿名请求和已停止的搜索不得触发通知。
16. 邮件按 `DOMAIN_EMAIL_BATCH_SIZE` 批量后台发送；检测结束时不足一批的剩余结果发送一批。
15. `/api/search` 的 NDJSON 最后一行是 `event: summary` 汇总，客户端解析时不能把它当作域名结果渲染。
16. `fuzzyMode` 支持 `none`、`prefix`、`suffix`、`both`；模糊追加字符必须遵守主体长度和数字规则。
17. 停止检测通过请求上下文取消实现；新增检测协程必须监听 `ctx.Done()`，不能在客户端取消后继续发起请求。
18. 用户邮箱作为唯一账号，密码只能保存 bcrypt 哈希，不能记录明文。

## 常用命令

```bash
go run .
go test ./...
go vet ./...
go build -o bin/domscan .
docker compose up -d postgres
docker compose up -d redis
```

## 修改约定

- 新业务规则优先进入 `domain`，并添加表驱动测试。
- 新的注册状态数据源应实现 `availability.Checker`，不要把供应商逻辑写进 HTTP handler。
- 新缓存实现应满足 `availability.Cache`，缓存策略留在 `CachedChecker`，存储包只负责序列化和读写。
- API 错误使用 JSON；检测结果流使用 NDJSON。不要在流中混入日志或非 JSON 文本。
- HTTP handler 使用 `gin.Context`，但 `New` 保持返回标准 `http.Handler`，便于主程序组合和 `httptest` 测试。
- 新的部署级配置统一添加到 `config.Config`、`.env.example` 和 README 配置表，不要在业务包中直接读取环境变量。
- 保持请求取消传播到工作协程和外部 RDAP 请求，避免客户端断开后继续批量查询。
- 客户端展示“可注册”时必须保留最终以注册商结果为准的提示。
- 不提交构建产物、IDE 配置、实际 `.env` 或本地缓存；新增配置时必须同步可提交的 `.env.example`。

## 提交前检查

1. 运行 `gofmt` 格式化修改过的 Go 文件。
2. 运行 `go test ./...` 和 `go vet ./...`。
3. 涉及入口时执行 `go build ./...`。
4. 检查是否意外改变 API JSON 字段、状态值或 NDJSON 流格式。
