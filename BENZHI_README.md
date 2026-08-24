# Webhook 事件路由服务（BENZHI 评测说明）

轻量化、纯 Go 实现的 Webhook 事件路由服务。接收第三方系统回调事件，依据可配置路由规则实时分发到目标端点，实现验签、校验、幂等去重、失败重试与完整投递留痕。纯 Go 项目，无前端工程。

## 标准构建 / 运行 / 测试命令

```bash
go build ./...      # 编译（全部包）
go run ./cmd/server # 启动服务（默认监听 :8080）
go test ./...       # 测试
```

启动后可通过 `go run ./cmd/server` 运行，默认监听 `:8080`；健康检查：

```bash
curl http://localhost:8080/healthz   # 期望返回 {"status":"ok"}
```

主要配置项通过环境变量提供（均有默认值）：`HTTP_ADDR`（默认 `:8080`）、`DB_PATH`（默认 `./data/router.db`）、`MAX_PAYLOAD`、`MAX_RETRIES`、`RETRY_BASE_MS`、`FORWARD_TIMEOUT_MS`、`ALLOW_PRIVATE_TARGET`、`ALLOW_LOOPBACK_TARGET`、`REPLAY_WINDOW_SEC`。

## 冒烟测试

```bash
./scripts/smoke_test.sh
```

脚本会生成临时数据库与端口，后台启动服务，等待健康检查通过，完成一次事件接入与投递查询，最后清理进程与临时数据。

## 镜像构建与使用

```bash
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh webhook-event-router linux/amd64
./build_benzhi_docker.sh webhook-event-router linux/arm64
docker run -it webhook-event-router   # 启动后进入 bash
```

容器内保留完整 Go 工具链，依赖已在构建阶段通过 `go mod download` 预下载，离线也可编译、测试。
