# 运维手册

面向单机在线 / 离线部署的值守人员：部署组合怎么选、升级与回滚、故障切换和恢复，以及平台告警出现后的处理步骤。命令在仓库根目录（离线为解包目录）执行；配置项含义见 [部署与本地调试](DEPLOYMENT.md)。

## 部署组合

| 类别 | 组件 | 说明 |
| --- | --- | --- |
| 核心（必装） | PostgreSQL + pgvector、Redis、Redpanda、EMQX、RustFS、platform-api 与协议运行器、platform-web、DeepSeek Harness、备份服务、ops-init | 接入、解析、告警、通知、AI 工作流和备份都依赖它们；Harness 按项目约定必装 |
| ClickHouse（默认部署） | ClickHouse | `--clickhouse off` 关闭后原文与遥测全部写 PostgreSQL，见 [ClickHouse](DEPLOYMENT.md#clickhouse)。高频接入或需要长期遥测查询时保留 |
| 监控（默认部署） | Prometheus、Loki、Alloy、Grafana、Alertmanager、node-exporter | `--ops off` 关闭，见 [运维组件](DEPLOYMENT.md#运维组件)。关闭后没有死信、消费阻塞、通知失败等平台自身告警，正式环境建议保留，或把 `/metrics` 接入已有监控 |
| 摄像头直播（默认部署） | ZLMediaKit | `--video off` 关闭，见 [摄像头部署](DEPLOYMENT.md#摄像头部署) |
| 容量测试（正式部署默认关闭） | capacity | `--capacity on` 开启，见 [容量测试模块](DEPLOYMENT.md#容量测试模块) |
| 恢复演练 | rustfs-dr | 仅供隔离恢复验证使用，不是异地副本；异地副本配置 `IOT_BACKUP_OFFSITE_*` |

**最小生产组合**：核心组件 + `--video off --capacity off`（设备少、上报频率低时可再加 `--clickhouse off`）；监控组件建议保留。单机组合不具备高可用，边界见 [高可用边界](DEPLOYMENT.md#高可用边界)。

上线前确认：

- 管理端口只绑定本机（`*_BIND_ADDRESS` 默认 `127.0.0.1`），需要对外的 Kafka 或控制台单独放开。
- 已按 [HTTPS 与 MQTTS](DEPLOYMENT.md#https-与-mqtts) 配置证书。
- 通知渠道和策略已配置并收到测试消息，见 [告警通知](PLATFORM.md#告警通知)。
- 整库备份（`DATABASE`）至少成功一次，并在演练库完成过一次恢复验证；按需配置异地副本。
- `IOT_OPS_TENANTS` 已设置运维租户，值守账号能打开运维中心。

## 升级

1. **备份**：备份中心执行“立即整库备份”并确认成功；离线环境同时保留旧离线包。
2. **停旧任务进程**：多副本或拆分部署时，先停止或一起升级所有 Jobs 进程（`combined` / `jobs`）。新版本的数据库迁移可能改变表结构（例如按月分区），旧进程的保留任务不能在新结构上执行。
3. **部署**：重跑原部署命令（`bash scripts/deploy-online.sh`，或 `bash scripts/deploy-offline.sh --bundle-dir <新离线包>`），不加模块参数时沿用上次的模块选择。
   从使用 MinIO 的版本升级时，对象存储已改为 RustFS，部署后按 [从 MinIO 迁移](DEPLOYMENT.md#从-minio-迁移到-rustfs) 复制一次桶数据（旧数据卷保留）。
4. **迁移**：API 启动时自动执行未完成的迁移，结果记在 `schema_migration`。大表的首次索引或分区准备耗时与数据量成正比，期间写入不中断，但 API 在迁移完成前不会就绪；多个进程同时启动时其余进程等待。迁移失败会使启动失败，修正原因后重启即可续跑，见 [数据库迁移](DEPLOYMENT.md#数据库迁移)。
5. **验证**：`/health/ready` 通过；运维中心“死信”为空，消费者无积压；用测试设备上报一条告警，确认告警中心出现并收到通知。

**回滚**：数据库迁移只向前执行，不提供自动降级。新版本无法使用时，停止服务，用升级前的整库备份恢复数据库（见下文“恢复”），再部署旧版本镜像或旧离线包。升级后新产生的数据会随恢复丢失，回滚前先评估或导出。

## 故障切换

单机 Compose 没有自动切换：容器只在进程退出后原地重启。宿主机、磁盘或数据卷故障时的处理是“在新机器恢复”：

1. 在新机器安装同版本（相同部署脚本或离线包），沿用原环境文件中的全部凭据，尤其是 `IOT_JWT_SECRET`、视频凭据密钥和数据库密码；环境文件不在备份范围内，须另行保管。
2. 启动后用最近的整库备份恢复数据库，再用 `FULL` 与 `DEVICE_DAILY` 备份补回设备消息、知识库原件和 Harness 会话。
3. 设备与对接方改连新地址（DNS 或负载均衡切换），检查 MQTT、TCP 设备重新上线。

需要自动切换时使用 [集群部署](DEPLOYMENT.md#集群部署)；集群切换须在目标环境演练，仓库只验证渲染与编排。

## 恢复

| 场景 | 做法 |
| --- | --- |
| 验证备份可用（例行演练） | 备份中心对整库备份点“恢复验证”，恢复到 `IOT_BACKUP_RESTORE_DATABASE_DSN` 指向的演练库，核对表数量；设备消息、知识库与外部接入的隔离恢复见 [设备数据备份](DEPLOYMENT.md#设备数据备份) |
| 正式恢复整库 | 停止 platform-api、backup-service 等写库服务；从 RustFS `iot-backups`（或异地副本）取回整库制品并核对 SHA-256；在业务库执行 `pg_restore --clean --if-exists --no-owner -d <业务库>`；启动服务，迁移会补齐备份之后新增的结构 |
| 只缺某天设备消息 | 用该日 `DEVICE_DAILY` 恢复到演练库核对，再按需导回 |

隔离恢复不替换现网数据；“备份存在”“下载成功”“恢复验证通过”“正式恢复完成”是不同结论，按实际执行的步骤记录。

## 告警处置

平台自身告警由 Alertmanager 发送（运维中心可查看和静默），与消防业务告警的通知相互独立。

每个 API 响应都带 `X-Request-ID`（沿用代理传入的合法编号，否则由平台生成），访问日志字段为 `requestId`；接口返回“服务内部错误……请提供编号”时，该编号即请求编号，可在 Loki 中按它检索 `request failed` 日志里的详细原因。接口延迟与错误率见指标 `http_request_duration_seconds{route,method,code}`（流式对话不计入）。

| 告警 | 含义 | 处理 |
| --- | --- | --- |
| `IotPlatformDown` | 平台进程不可抓取 | `docker compose ps`、`docker compose logs platform-api`；检查 `/health/ready` 中失败的依赖 |
| `ConsumerBlockedByOutage` | 依赖（数据库、Kafka 等）临时故障，消费者暂停在原位置重试 | 先恢复依赖；恢复后自动继续，不需要回放。持续超过 `IOT_CONSUMER_MAX_BLOCK`（默认 30 分钟）的消息会进入死信 |
| `DeadLetterPublished` | 消息因永久错误或长时间阻塞进入死信 | 运维中心 → 运维总览 → 死信，查看错误原因；修复后逐条“重新投递”（写审计）。存储死信也可用 `cmd/dlq-replay`，见 [开发与测试](DEVELOPMENT.md) |
| `KafkaConsumerLagHigh` | 消费积压 | 看是否伴随阻塞或解析失败；持续增长时检查 Parser / Processor 资源与数据库耗时，必要时拆分角色或增加副本 |
| `RawArchiveFailures`、`ParseFailureSpike` | 原文归档失败、解析失败突增 | 归档失败检查 PostgreSQL / ClickHouse；解析失败在“原始报文”按设备查看错误，多为协议版本或设备配置变化 |
| `ProtocolListenerFull` | TCP / UDP 会话达到上限 | 调整 `IOT_PROTOCOL_LISTENER_MAX_SESSIONS`，核对是否有异常重连的设备 |
| `MQTTSubscriptionLost`、`MQTTDeliveryLoss`、`MQTTInboxBacklog`、`MQTTBrokerObservationMissing` | MQTT 订阅、投递或本地收件箱异常 | 检查 EMQX 状态与管理 API 配置、磁盘空间；收件箱积压在依赖恢复后自动排空 |
| `AlarmNotificationFailures` | 火警通知多次重试仍失败 | 告警详情 → 通知记录查看失败原因；检查渠道地址、加签密钥、SMTP 账号和 `IOT_NOTIFY_ALLOWED_CIDRS`，修复后在通知页发送测试消息 |
| `AlarmEventDeliveryFailures` | 告警事件未能推送到消息总线或实时通道 | 告警已入库，告警中心仍可查询；检查 Kafka / EMQX 状态和平台日志中的 `event delivery failed`，对外消息主题订阅方可能缺少这段时间的事件 |
| `AuditWriteFailures` | 审计记录写入失败 | 操作已生效但缺少审计；检查 PostgreSQL 连接与磁盘，平台日志 `audit write failed` 列出租户与动作 |
| `AIAnalysisFailures` | 研判工作流失败 | 检查 Harness 健康、DeepSeek Key 与额度、MCP 回调地址 |
| `BackupFailures` | 备份、异地副本或恢复演练失败 | 备份中心查看失败任务；检查 RustFS、异地存储凭据、磁盘空间和 PostgreSQL 客户端版本 |
| `RetentionFailures` | 历史数据清理失败 | Jobs 日志中 `retention purge failed` 的表与原因；不处理会使磁盘持续增长 |
| `PartitionMaintenanceFailures` | 未能提前创建月分区 | Jobs 日志中 `create upcoming partitions`；数据会进入 `_default` 分区，仍可读写，修复后若默认分区已有该月数据需人工迁出再建分区 |
| `ScrapeTargetDown`、`HostDiskAlmostFull` | 监控目标不可达、磁盘将满 | 检查对应容器；磁盘不足时先确认保留任务正常，再扩容或缩短保留期 |

处理完成后在运维中心确认告警恢复；临时静默须写明原因和到期时间。
