# Webhook 事件路由服务

轻量化、纯 API 形态的 Webhook 事件路由服务。接收第三方系统回调事件，依据可配置的路由规则实时分发到目标端点，并实现验签、校验、幂等去重、失败重试与完整投递留痕。

## 功能

- 统一事件入口 `POST /api/v1/events`，HMAC-SHA256 来源验签。
- 负载 JSON 合法性校验、事件类型校验、目标地址白名单（默认拒绝环回与私网）。
- (source_id, event_id) 幂等去重。
- 时间戳防重放：事件携带 `timestamp`（unix 秒），过期或未来时间戳被拒绝。
- 规则引擎：来源/事件类型/JSON 路径条件匹配目标端点，支持多目标规则与增强条件运算符（regex/in/exists/数值比较/and/or/not，数组下标与通配符路径）。
- 转发失败进入重试队列，独立指数退避（1/2/4/8/16 秒 + 随机抖动），超限标记 dead。
- 死信队列与手动重试：`POST /api/v1/deliveries/{id}/retry` 将 dead 投递复位后重新入队。
- SQLite 持久化（开启 WAL 与 foreign_keys），记录来源、规则、目标、事件、投递、尝试与变更日志。
- 审计查询：`GET /api/v1/audit` 查询配置变更历史（before/after 快照）。
- Prometheus 指标：`GET /metrics` 暴露事件与投递计数器。
- 管理接口：来源、规则、目标的 CRUD；投递记录分页查询与尝试明细；健康检查。

## 快速开始

```bash
# 构建
go build ./...

# 运行（默认监听 :8080）
DB_PATH=./data/router.db ./webhook-event-router

# 健康检查
curl http://localhost:8080/healthz
```

## 配置

通过环境变量配置（均有默认值）：

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `HTTP_ADDR` | `:8080` | 监听地址 |
| `DB_PATH` | `./data/router.db` | SQLite 数据库路径 |
| `MAX_PAYLOAD` | `1048576` | 负载最大字节数 |
| `MAX_RETRIES` | `5` | 最大重试次数 |
| `RETRY_BASE_MS` | `1000` | 重试退避基数（毫秒） |
| `FORWARD_TIMEOUT_MS` | `5000` | 转发超时（毫秒） |
| `ALLOW_PRIVATE_TARGET` | `false` | 是否允许私网目标 |
| `ALLOW_LOOPBACK_TARGET` | `false` | 是否允许环回目标 |
| `REPLAY_WINDOW_SEC` | `300` | 时间戳防重放允许的时钟窗口（秒） |

## 主要接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/healthz` | 健康检查，返回 `{"status":"ok"}` |
| POST | `/api/v1/events` | 接入事件 |
| POST/GET | `/api/v1/sources` | 管理来源 |
| GET/PUT/DELETE | `/api/v1/sources/{id}` | 来源详情/更新/删除 |
| POST/GET | `/api/v1/rules` | 管理规则 |
| GET/PUT/DELETE | `/api/v1/rules/{id}` | 规则详情/更新/删除 |
| POST/GET | `/api/v1/targets` | 管理目标 |
| GET/PUT/DELETE | `/api/v1/targets/{id}` | 目标详情/更新/删除 |
| GET | `/api/v1/events/{id}` | 事件详情与投递列表（含尝试明细） |
| GET | `/api/v1/deliveries` | 投递记录分页查询 |
| POST | `/api/v1/deliveries/{id}/retry` | 手动重试 dead 投递 |
| GET | `/api/v1/audit` | 审计日志查询（`entity_type`、`entity_id`、`page`、`limit`） |
| GET | `/metrics` | Prometheus 指标 |

事件接入请求体含 `source_id`、`event_type`、`event_id`、`payload`，并在 `X-Signature` 头携带 HMAC-SHA256 签名。

## Docker

多阶段构建，同时兼容 linux/arm64 与 linux/amd64：

```bash
docker buildx build --platform linux/arm64,linux/amd64 -t webhook-event-router .
docker run -p 8080:8080 -v $(pwd)/data:/data -e DB_PATH=/data/router.db webhook-event-router
```

容器只监听 8080 并 `EXPOSE 8080`，宿主机可映射任意端口。

## 测试

```bash
go test -count=1 ./...
```

## 冒烟测试

```bash
./scripts/smoke_test.sh
```

脚本会生成临时数据库与端口，后台启动服务，等待健康检查通过，完成一次事件接入与投递查询，最后清理进程与临时数据。
