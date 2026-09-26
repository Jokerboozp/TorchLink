# 端到端容量压测方案与记录模板

本文用于在目标硬件上取得可复核的容量数据。已有一次单机实测记录见 [容量压测报告](CAPACITY_TEST_REPORT.md)（2026-09-26，本地隔离环境，只对其硬件与配置成立）；源码限额见 [技术详情 · 容量边界](TECHNICAL_DETAILS.md#容量边界)，不能代替本方案的结果。完成压测前，不对外承诺“支持多少万设备”或“每秒多少万条”。

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
| Kafka 积压 | 各消费组 `iot-platform-*` 的 LAG | 平台 `/metrics` 每 15 秒采样：`kafka_lag` 为本进程全部消费组的总积压，`kafka_lag_storage`、`kafka_lag_parser` 等为分组积压；可用 `docker compose exec redpanda rpk group describe <组名>` 复核 |
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
| S6 | 告警与 AI | 构造告警风暴，开启与生产一致的规则和自动研判 | 告警全部生成；自动研判排队可恢复（自动研判默认并发 1、Harness 部署默认并发 2，可按模型能力调整后重测），失败记录可读，不阻塞入库 |
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

### 分阶梯压测工具 `cmd/capacity-test`

`cmd/capacity-test` 使用**设备凭据**上报，覆盖 S1–S7 中 `loadgen` 不能代替的部分：设备登记、HTTP / MQTT / GB26875 TCP 上报、MQTT 连接规模、管理接口、端到端延迟探针。每个阶梯同时读取平台 `/metrics`，输出本档的归档/秒、解析/秒、告警/秒和 Kafka 积压起止值，并每 500 ms 探测一个管理接口（`-probe`）观察互相影响。`-levels` 为闭环并发，`-rates` 为开环速率；错误率超过 `-stop-err` 或吞吐跌到峰值的 `-stop-drop` 以下时自动停止。结果写入 `-out` 指定的 JSON。

在仓库根目录执行（地址、产品和文件名按实际替换；令牌文件由第 2 节的登录命令保存，不要写进命令行历史）：

```bash
# 1. 在已有标准协议产品下批量登记设备，同时测登记吞吐；凭据以 0600 权限写入文件
go run ./cmd/capacity-test -mode provision -token @token.txt -product <产品> -prefix st-http -count 5000 -par 16 -devices devices.json

# 2. 设备凭据 HTTP 上报：开环恒定速率找端到端可持续值（看每档 lag 是否增长）
go run ./cmd/capacity-test -mode ingest -token @token.txt -devices devices.json -rates 25,50,100,150 -step 60s -out s1.json

# 3. 同一链路闭环加并发直到失败；-alarm-frac 0.5 表示一半报文带 stressAlarm=1（配合“stressAlarm eq 1 触发、eq 0 恢复”的测试规则）
go run ./cmd/capacity-test -mode ingest -token @token.txt -devices devices.json -levels 256,1024,4096 -step 40s -stop-drop 0

# 4. 管理接口阶梯（可用 | 分隔多个路径轮询）
go run ./cmd/capacity-test -mode http -token @token.txt -path "/api/v1/alarms?page=1&pageSize=20&status=ACTIVE" -levels 1,4,16,64,256 -step 15s

# 5. MQTT：先取令牌（令牌 5 分钟失效，取完立即测），再测连接规模或发布速率
go run ./cmd/capacity-test -mode mqtttok -devices mqtt-devices.json -out tokens.json
go run ./cmd/capacity-test -mode mqttconn -mqtt-tokens tokens.json -conn-step 2000 -hold 7m
go run ./cmd/capacity-test -mode mqttpub -token @token.txt -mqtt-tokens tokens.json -conn-step 2000 -rates 50,200,800 -step 40s

# 6. GB26875 TCP：-levels 为连接数，每个连接一台设备，等待平台归档后的 ACK
go run ./cmd/capacity-test -mode tcp -token @token.txt -tcp <平台地址>:26875 -levels 8,32,128 -step 30s

# 7. 端到端延迟探针与指标采样（各开一个终端，Ctrl+C 结束）
go run ./cmd/capacity-test -mode canary -token @token.txt -devices devices.json > canary.csv
go run ./cmd/capacity-test -mode sample -interval 5s > metrics.csv
```

- 设备凭据与 MQTT 令牌文件含秘密，测试结束后删除。
- 宿主机单个客户端受临时端口数量限制（macOS 约 1.6 万）；更大的 MQTT 连接规模需在多台机器或多个容器中分片运行（`-mqtt tcp://emqx:1883`），并确认 EMQX 容器的 `nofile` 足够。
- `mqttpub` 的 PUBACK 只表示 Broker 已接收；是否到达平台以 `archived/s` 和 `docker compose exec emqx /opt/emqx/bin/emqx ctl clients list` 中平台会话的 `dropped_msgs` 为准。
- `canary` 每秒查询原文详情，高频设备的原文位于 ClickHouse，查询开销大，本身会给 ClickHouse 加压；只在需要端到端延迟时开启。

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
