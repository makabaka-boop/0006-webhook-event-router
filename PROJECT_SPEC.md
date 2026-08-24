项目文档：规则驱动的 Webhook 事件路由服务

一、业务目标与真实使用者
本系统是一套轻量化、纯 API 形态的 Webhook 事件路由服务，面向研发与运维人员。真实使用者包括：负责接收 GitHub/GitLab/CI/CD/监控告警等第三方回调的平台工程师；需要把同一类事件分发到多个内部系统的集成与值班人员；需要审计每条回调是否送达、为何失败、何时重试的运维与安全人员。核心价值：统一事件入口，用可配置规则替代硬编码分发，提供幂等去重、来源签名校验、事件类型校验、负载 JSON 合法性校验、目标地址白名单、失败重试与完整投递留痕，降低各系统各自实现回调接收的重复成本，并让事件流转可观测、可审计、可恢复。系统保持 API 服务形态，不提供前端页面，所有能力通过 HTTP JSON 接口与后台任务暴露。

二、核心业务闭环
1. 事件接入：调用方携带来源标识与签名向 POST /api/v1/events 提交事件 JSON。
2. 来源识别与验签：按来源 ID 查 Source，用其密钥对原始 body 做 HMAC-SHA256 校验。
3. 基础校验：校验事件类型是否在来源允许列表内、负载是否为合法 JSON 对象且不超限。
4. 幂等去重：以 (source_id, event_id) 唯一键判断重复，重复则返回既有处理结果，不重复转发。
5. 规则匹配：加载启用规则，按来源、事件类型与负载条件（JSON 路径加运算符）匹配出目标端点。
6. 白名单与转发：目标地址必须命中白名单约束，随后对每个目标执行 HTTP POST 转发并留痕。
7. 失败重试：转发非 2xx 或网络错误进入重试队列，按指数退避重试，超限标记 dead；全程写投递与尝试记录。
8. 查询与变更：规则、来源、目标管理接口，投递记录查询接口，健康检查接口；所有配置变更写变更日志。

三、实体字段与关系
Source（事件源）：id、name、secret（验签密钥）、enabled、allowed_event_types、created_at、updated_at。
Event（事件）：id（内部 UUID）、source_id、event_type、event_id（业务幂等键）、payload（JSON 文本）、status、received_at、created_at。
Target（目标端点）：id、name、url、enabled、created_at、updated_at。
Rule（路由规则）：id、name、source_id（可空，表示所有来源）、event_type（可空）、condition（JSON 条件数组）、target_id、priority、enabled、created_at、updated_at。
Delivery（投递记录）：id、event_id、rule_id、target_id、status、attempts、next_retry_at、last_error、created_at、updated_at。
DeliveryAttempt（投递尝试）：id、delivery_id、status、request_body、response_status、response_body、error、started_at、finished_at。
ChangeLog（变更日志）：id、entity_type、entity_id、action、before、after、actor、created_at。
关系：Source 1-N Event；Event 1-N Delivery；Rule N-1 Target；Rule 可选 N-1 Source；Delivery N-1 Rule、Target、Event；Delivery 1-N DeliveryAttempt；Source/Rule/Target 每次写操作对应 1-N ChangeLog。

四、状态流转与约束
Event：received → accepted（校验与入库成功）→ delivered（全部目标终态成功）/ partially_failed（存在失败且仍在重试）/ failed（全部目标 dead）/ rejected（验签、类型、JSON 或白名单校验失败，记录拒绝原因）/ duplicate（命中幂等，返回既有结果）。约束：payload 最大 1MB；event_id 必填且在来源内唯一；来源禁用时拒绝接入。
Delivery：pending → sent → delivered（目标返回 2xx）/ failed（非 2xx 或网络、超时错误）→ retrying → delivered / dead。约束：最大重试 5 次，退避 1s/2s/4s/8s/16s 加随机抖动；仅 failed 且 attempts 小于最大值时才能进入 retrying；dead 不再自动重试但可手动重试。目标必须为 http/https 且命中白名单，默认拒绝环回与私网地址，除非配置显式放行。

五、API 输入输出与错误语义
统一错误体为 error 对象，含 code 与 message 两个字段；成功体按资源返回 JSON 对象。主要接口：
GET /healthz → 200 且 body 含 status 为 ok。
POST /api/v1/events：入参含 source_id、event_type、event_id、payload；成功返回 202 与 event_id、status 为 accepted；错误：400 payload_invalid（非法 JSON）、401 signature_invalid、404 source_not_found、422 event_type_not_allowed、409 duplicate_event、413 payload_too_large、403 target_not_whitelisted（目标不在白名单时记录到投递失败而非整体拒绝）。
POST/GET /api/v1/sources，GET/PUT/DELETE /api/v1/sources/{id}：管理事件源。
POST/GET /api/v1/rules，GET/PUT/DELETE /api/v1/rules/{id}：管理规则。
POST/GET /api/v1/targets，GET/PUT/DELETE /api/v1/targets/{id}：管理目标。
GET /api/v1/events/{id}：事件详情及其投递列表。
GET /api/v1/deliveries?event_id=&status=&page=&limit=：投递记录分页查询。
错误语义统一为可枚举 code 与明确 message，HTTP 状态码与 code 一一对应，便于调用方程序化处理。

六、持久化与变更留存
使用 SQLite 持久化，开启 WAL 与 foreign_keys；DB 路径由环境变量指定，容器内挂载到可写目录。启动时执行 schema 迁移并记录 schema_version。Source/Rule/Target 的创建、修改、删除（软删除或停用）均在同一事务内写入 ChangeLog，before/after 保存 JSON 快照；DeliveryAttempt 追加式写入，保证每次转发、重试与失败原因可回溯。重试状态在事务中更新，避免并发重复重试。

七、模块边界
cmd/server：装配与启动。internal/config：读取并校验环境变量。internal/domain：实体、状态枚举与状态机约束。internal/store：存储接口与错误。internal/store/sqlite：SQLite 实现与迁移。internal/webhook：验签、负载校验、幂等去重。internal/engine：规则条件匹配与取值。internal/dispatcher：转发执行与后台重试任务。internal/httpapi：路由、处理器、中间件与统一响应。internal/errs：类型化错误与错误码。各层仅依赖其下一层接口，handler 不直接写 SQL，转发不依赖 HTTP 层，保证可测试。

八、关键测试
规则匹配：来源与事件类型过滤、JSON 路径条件命中与不命中、禁用规则跳过。
重复事件幂等：同 (source_id, event_id) 第二次请求返回既有结果且不新增投递。
失败重试：用 httptest 目标返回 500，断言 attempts 递增、状态进入 retrying 并最终 dead，退避时间可注入。
非法负载拒绝：非 JSON、超限、未知事件类型、错误签名分别返回对应错误码。
白名单：目标为 http 或私网、环回地址时拒绝转发并留痕。
配置变更审计：创建、修改、删除规则后 ChangeLog 有对应 before/after 记录。
健康检查：返回 200 与 ok。

九、启动冒烟与验收场景
scripts/smoke_test.sh 必须：生成临时 DB 与端口；后台真实启动二进制；循环请求 GET /healthz 直至 200；再以最小来源加目标加规则完成一次事件接入与投递查询；最后 trap 清理服务进程与临时数据文件，失败时以非零码退出。
验收场景：1) 正确签名事件命中规则并投递成功；2) 同事件重复接入幂等不重复投递；3) 目标返回 500 触发重试并最终 dead 且留痕；4) 非法 JSON 返回 400；5) 未白名单目标被拒；6) 健康检查正常；7) docker buildx 同时产出 linux/arm64 与 linux/amd64 镜像，容器只监听 8080 且 EXPOSE 8080，可映射宿主机任意端口。

十、两阶段交付计划
初始核心版本：目标是完整可运行的 MVP，非测试 Go 有效代码约 2050 行、22 个生产 .go 文件（落在 1600～2200 行、18～24 个文件的约束内）。文件布局为上述 cmd/server、internal/config、internal/errs、internal/domain、internal/store、internal/store/sqlite（db、source_store、rule_store、target_store、event_store、delivery_store、changelog）、internal/webhook、internal/engine、internal/dispatcher、internal/httpapi（server、health、events、sources、rules、targets、deliveries），共 22 个文件。已包含验签、校验、幂等、规则匹配、转发、重试、留痕、管理接口与健康检查，全部被 API、后台任务或启动路径真实调用，无空实现与死代码。
第一次业务扩写：新增真实业务能力，使非测试 Go 有效代码达到约 3200 行、34 个生产 .go 文件（落在 2600～3800 行、28～40 个文件的约束内）。新增能力：多目标规则（rule_targets 关联）、条件运算符增强（regex/in/exists/数值比较与 and/or/not）、时间戳防重放、独立退避与死信队列、投递尝试明细拆分、手动重试接口、审计查询接口、Prometheus 指标接口与指标采集。新增文件：internal/engine/expression.go、internal/engine/path.go、internal/webhook/replay.go、internal/dispatcher/backoff.go、internal/dispatcher/deadletter.go、internal/metrics/metrics.go、internal/httpapi/audit.go、internal/httpapi/metrics.go、internal/httpapi/retry.go、internal/store/sqlite/rule_target_store.go、internal/store/sqlite/audit_store.go、internal/store/sqlite/attempt_store.go，共 12 个，合计 34 个文件。即使初始版本已超过 2000 行，也必须通过上述新增真实业务能力完成第一次扩写，不得为凑行数或文件数而添加无调用模块、复制粘贴或空实现。

实现状态：第一次业务扩写已完成并落地。新增 12 个生产文件均已接入 API 处理器、后台重试或启动路径：多目标规则经 rule_targets 关联表持久化并由引擎 MatchRules/SelectTargets 展开；条件表达式（expression.go/path.go）支撑 regex/in/exists/数值比较/and/or/not 与数组下标、通配符路径；时间戳防重放（webhook/replay.go）在事件接入时校验；独立退避（dispatcher/backoff.go）替换内联退避；死信队列与手动重试（dispatcher/deadletter.go）复位 attempts 后重新入队；投递尝试明细拆分为独立存储（attempt_store.go）并在事件详情中回显；审计查询（audit.go + audit_store.go）暴露变更历史；Prometheus 指标（metrics 包 + httpapi/metrics.go）采集事件与投递计数。规模已达约 3100 行、37 个生产 .go 文件，满足 28～40 个文件约束。

十一、规模与质量约束
规模口径仅统计非测试生产 .go 文件中的有效代码；测试代码、前端 HTML/CSS/JS/TS、vendor、空行与纯注释均不计入。最终验收要求：非测试 Go 源码严格大于 2000 且小于 5000 行、非测试 .go 文件严格大于 20 且小于 50 个；本计划最终约 3200 行、34 个文件满足该要求。所有生产代码必须被 API、业务服务、后台任务或启动路径真实调用，禁止复制粘贴、空实现、无调用模块、无意义包装及其他死代码；不得用一句话需求加行数指标直接凑代码。

十二、代码质量约束（强制性）
除规模约束外，本项目的生产代码必须满足以下工程质量约束，验收时逐条检查：
1. 依赖方向：各层仅依赖其下一层接口（httpapi → dispatcher/engine/webhook/store → sqlite），禁止跨层反向依赖、禁止 handler 直接编写 SQL、禁止 dispatcher 引用 HTTP 路由层。
2. 无死代码：每个生产文件、导出类型、导出函数与结构体字段都必须被 API 处理器、后台任务、业务服务或启动路径真实调用；禁止空实现、占位 TODO、未使用参数、仅用于凑行数的无意义包装与复制粘贴重复代码。
3. 错误处理：所有错误必须被显式处理或向上返回，禁止吞错（忽略 error）；对外暴露的错误统一走 internal/errs 类型化错误，携带可枚举 code、明确 message 与一一对应的 HTTP 状态码，禁止在 handler 中硬编码裸状态码字符串。
4. 并发安全：重试状态的推进、投递尝试的追加写入、变更日志的落库必须在同一数据库事务内完成，避免并发重复重试与读写竞争；共享可变状态必须加锁或通过单一后台 goroutine 串行处理。
5. 安全约束：验签密钥不得出现在日志或响应正文（JSON 序列化时 `json:"-"`）；负载读取使用 LimitReader 防止超限；目标地址强制 http/https 协议、默认拒绝环回与私网，仅当显式配置放行时才允许。
6. 资源管理：数据库连接、HTTP 响应体、文件句柄等资源必须 defer 关闭；HTTP 客户端必须设置超时；优雅停机需在信号到来后关闭 HTTP 服务并取消后台任务。
7. 命名与组织：package、类型、函数命名符合 Go 惯例与文档模块边界；文件按职责拆分到 cmd/server 与 internal/* 各目录，单一职责，禁止把所有逻辑堆进一个文件或函数。
8. 可测试性：核心逻辑（规则匹配、幂等去重、重试退避、白名单、验签）必须可注入依赖、退避时间可替换，以便用 httptest 与内存 SQLite 编写单元/集成测试；关键路径测试覆盖见第八节。
9. 配置校验：环境变量读取后必须校验（DB_PATH 非空、MAX_PAYLOAD/MAX_RETRIES 为正数等），非法配置在启动阶段失败而非运行时崩溃。
10. 日志与观测：服务启动、监听地址、转发失败原因、重试与死亡（dead）均需可定位，为审计与运维提供足够上下文。

十三、交付与缺陷说明
交付物包括 Go 源码、go.mod/go.sum、Dockerfile、scripts/smoke_test.sh 与必要的迁移 SQL。Dockerfile 使用多阶段构建与 buildx 同时构建 linux/arm64 与 linux/amd64 镜像，服务唯一监听 8080 并以 EXPOSE 8080 暴露，运行时允许映射宿主机任意端口。本次交付计划产出 1 条可复现的 Bug 数据，用于下游缺陷修复流程验证：Bug 需提供清晰复现步骤与预期/实际行为差异，且不计入规模统计口径。

Bug 数据（不计入规模统计）：
- 标题：手动重试接口对 dead 投递未复位 attempts，导致立即再次判 dead。
- 复现步骤：创建一个返回 500 的 httptest 目标与命中规则；接入事件使投递在最大重试后进入 dead；调用手动重试接口。
- 预期行为：attempts 归零并重新进入 retrying，按退避重试。
- 实际行为：attempts 继续沿用旧值，重试一次即再次判定 dead，无法触发完整退避周期。
