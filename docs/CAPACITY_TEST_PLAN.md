# 端到端容量压测方案与记录模板

本文用于在目标硬件上取得可复核的容量数据。仓库目前**没有**任何实测容量记录；源码限额见 [技术详情 · 容量边界](TECHNICAL_DETAILS.md#容量边界)，不能代替本方案的结果。完成压测前，不对外承诺“支持多少万设备”或“每秒多少万条”。

## 1. 结论口径

只能按下面的格式陈述结果，每条结论都附对应的记录表：

> 在〈硬件与部署形态〉、〈平台提交号与关键配置〉下，〈N〉台设备按〈报文类型与大小〉以〈R〉条/秒持续〈T〉分钟，积压不增长、错误率〈E〉、端到端 P95〈L〉毫秒。

- 入口 HTTP 202、MQTT PUBACK 或 `/health/ready` 成功只说明“已接收”，不说明解析、入库或告警已完成。
- 一次压测只证明该组合；换硬件、协议、规则数量、保留时间或 AI 配置后需重测。
- “持续”指积压（MQTT 持久队列、Kafka 消费延迟）在观察窗口内保持平稳；只要积压持续增长，该速率就不可持续，哪怕发送端全部成功。

## 2. 环境与前置条件

- 使用与生产隔离的环境和独立租户，不在生产库上压测；压测结束按记录清理测试租户数据。
- 部署形态与目标一致（在线 / 离线、单进程 `combined` 或拆分 `api` + `gateway`），记录每个容器的 CPU、内存限额和磁盘类型。
- 准备代表性产品与报文：至少覆盖实际使用的 JSON、固定长度 HEX 和 Go 协议包；规则数量与现场接近；是否开启 AI 自动研判与生产一致。
- 发压机与平台分开部署，记录两者之间的网络带宽与时延。
- `cmd/loadgen` 需要内置环境管理员（`.env.*` 中的 `IOT_ADMIN_USER`）的令牌：它调用 `/api/v1/raw-messages` 或 `/api/v1/mqtt/load-token`，页面里创建的普通用户无权使用这两个入口。它走管理员上报链路，**不经过设备凭据认证**；设备认证链路需另用真实设备或模拟设备验证，见第 4 节。

获取管理员令牌（在发压机执行，按实际地址、租户和密码替换）：

```bash
TOKEN=$(curl -s -X POST http://<平台地址>:8081/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"tenantId":"<测试租户>","username":"<IOT_ADMIN_USER>","password":"<IOT_ADMIN_PASSWORD>"}' | jq -r .accessToken)
```

## 3. 观测项

每个场景开始前、进行中（每分钟）和结束后分别记录：

| 类别 | 指标 | 获取方式 |
| --- | --- | --- |
| 发送端 | 实际 QPS、错误率、确认 P50/P95/P99 | `loadgen -report <文件>` 输出的 JSON |
| 接收与归档 | `raw_archive_success_total`、`raw_archive_failed_total`、`raw_publish_failed_total` 的增量 | `curl http://<平台地址>:8081/metrics` |
| 解析与告警 | `parse_failed_total`、`alarm_trigger_total`、`ai_analysis_success_total`、`ai_analysis_failed_total` 的增量 | 同上 |
| MQTT 持久队列 | `mqtt_inbox_pending`、`mqtt_inbox_rejected`、`mqtt_inbox_corrupt` | 同上；`pending` 持续上升即不可持续 |
| Kafka 积压 | 各消费组 `iot-platform-*` 的 LAG | `docker compose exec redpanda rpk group describe <组名>`，先用 `rpk group list` 列出 |
| 入库核对 | 测试时段内原始报文条数、标准消息条数 | 按 `internal/adapters/rawstore/` 的实际路由，在 PostgreSQL 或 ClickHouse 中按租户与时间范围计数 |
| 资源 | 各容器 CPU、内存、磁盘 IO、网络；PostgreSQL 连接数与慢查询 | `docker stats`、宿主机监控、`pg_stat_activity` / `pg_stat_statements` |
| 管理端 | 设备列表、告警列表、运行总览接口的 P95 / P99 | 压测期间用独立脚本按固定频率请求并记录耗时 |

端到端延迟：每个场景抽取不少于 100 条带唯一 `messageId` 的报文，记录发送时间，再按 `messageId` 查询其标准消息或告警出现的时间，计算 P50 / P95 / P99。

## 4. 场景

| 编号 | 场景 | 做法 | 通过条件（建议值，需按业务确认） |
| --- | --- | --- | --- |
| S1 | 持续上报 | `loadgen -profile steady`，从目标速率的 25% 开始，每档持续 ≥ 30 分钟，逐档提高 | 积压平稳；发送错误率 ≤ 0.1%；入库数与发送数一致（差值可解释为解析失败）；端到端 P95 在目标内 |
| S2 | 突发 | `-profile burst`，突发速率与时长按现场最坏情况设置 | 突发结束后积压在约定时间内回落到基线；无数据丢失 |
| S3 | 断流恢复 | `-profile offline`，模拟网络中断后集中补传 | 恢复后积压回落；补传报文全部入库，时间顺序与幂等正确 |
| S4 | 设备规模 | 用 `-devices` 覆盖目标设备数，速率取 S1 可持续值 | 限流表、设备状态更新与总览查询无明显退化 |
| S5 | 管理端并发 | S1 可持续速率下，同时按预计在线人数请求管理端接口 | 管理接口 P95 在目标内，不拖慢入库 |
| S6 | 告警与 AI | 构造告警风暴，开启与生产一致的规则和自动研判 | 告警全部生成；自动研判排队可恢复（Harness 默认并发 4），失败记录可读，不阻塞入库 |
| S7 | 设备认证链路 | 用真实设备或模拟设备按设备凭据经 MQTT / HTTP / TCP 上报 | 与 S1 同速率下结果一致；此项不能由 `loadgen` 代替 |

说明：`loadgen` 使用少量客户端向大量设备 ID 发布，**不能**测量“同时在线的 MQTT 连接数”。连接规模需使用专门的 MQTT 压测工具（例如 emqtt-bench）另行测试，并单独记录。

`loadgen` 命令示例（在仓库根目录执行，参数按场景调整）：

```bash
# S1：HTTP 管理员链路，1000 条/秒，10000 个设备 ID，持续 30 分钟
go run ./cmd/loadgen -url http://<平台地址>:8081 -token "$TOKEN" -transport http \
  -profile steady -rate 1000 -devices 10000 -workers 64 -duration 30m \
  -tenant <测试租户> -product <测试产品> -report s1-1000.json

# S2：MQTT 链路突发，50000 条/秒持续 5 分钟
go run ./cmd/loadgen -url http://<平台地址>:8081 -token "$TOKEN" -transport mqtt \
  -mqtt-broker tcp://<EMQX 地址>:1883 -profile burst -rate 1000 -burst-rate 50000 \
  -burst-duration 5m -duration 15m -tenant <测试租户> -product <测试产品> -report s2-burst.json
```

`loadgen` 的 `-max-error-rate`、`-max-p95-ms`、`-min-qps` 只判定发送端，通过不代表端到端通过。

## 5. 记录模板

每个场景复制一份，结果文件与原始截图放在同一目录并在表中写明路径。

```markdown
### 压测记录：S?-〈场景〉-〈速率〉

- 日期 / 执行人：
- 平台提交号：
- 部署形态：（在线 / 离线；combined / api+gateway；副本数）
- 硬件：（CPU 型号与核数、内存、磁盘类型、网络）
- 容器限额：（platform-api、postgres、clickhouse、redpanda、emqx、deepseek-harness）
- 关键配置：（原始报文分层存储配置如 IOT_RAW_HIGH_FREQUENCY_INTERVAL_SEC、保留时间、规则数量、AI 自动研判是否开启、模型）
- 报文：（协议 / 格式 / 平均字节数 / 设备数）
- loadgen 参数：
- 持续时长 / 观察窗口：

| 指标 | 开始 | 中段 | 结束 | 备注 |
| --- | --- | --- | --- | --- |
| 发送 QPS / 错误率 |  |  |  |  |
| 确认 P95 / P99 (ms) |  |  |  |  |
| raw_archive_success_total 增量 |  |  |  |  |
| parse_failed_total 增量 |  |  |  |  |
| mqtt_inbox_pending |  |  |  |  |
| Kafka 最大 LAG（组名） |  |  |  |  |
| 端到端 P50 / P95 / P99 (ms) |  |  |  |  |
| 原始报文数 / 标准消息数 |  |  |  |  |
| 管理接口 P95 (ms) |  |  |  |  |
| API / PostgreSQL / ClickHouse CPU 峰值 |  |  |  |  |

- 积压是否平稳：是 / 否（附曲线）
- 入库核对：发送〈a〉，归档〈b〉，标准消息〈c〉，解析失败〈d〉，差异原因：
- 结论（按第 1 节格式）：
- 未覆盖范围与遗留问题：
```

## 6. 结果归档

- 通过的结论与记录表更新到 [技术详情 · 容量边界](TECHNICAL_DETAILS.md#容量边界)，写明日期、环境与提交号；未通过的档位同样保留，用于确定上限。
- 历史结果不能当作新版本的验证；平台存储、消息或 AI 链路改动后，至少重跑 S1 可持续档与 S6。
