# 容量验收执行手册（P5）

| 项目 | 内容 |
| --- | --- |
| 适用阶段 | [集群方案](CLUSTER_AND_CAPACITY_PLAN.md#171-阶段与依赖) P5：扩容曲线、候选长稳、故障容量、资源与上线建议 |
| 状态 | 工具、预设与步骤已就绪；**尚未在目标硬件上执行**，仓库内没有 P5 实测结果 |
| 工具 | `cmd/capacity-test`（run / resume / compare / serve / agent）、`cmd/harness-mock`、`cmd/cluster-render`、`scripts/cluster-deploy.*` |

日常快速检查可直接使用平台的容量测试模块（部署时 `--capacity on`，见 [部署文档](DEPLOYMENT.md#容量测试模块)）；本手册的多机扩容、长稳与故障验收需要独立发压机，使用控制服务或命令行执行。本手册只描述在目标环境上如何执行和留证。命令用法细节见 [开发与测试](DEVELOPMENT.md#容量验证)，集群部署见 [部署文档](DEPLOYMENT.md#集群部署)。执行结果按实测填写，不得用本文的示例数字代替。

## 1. 执行前准备

| 项目 | 要求 | 检查方式 |
| --- | --- | --- |
| 被测环境 | 专用测试环境，与生产隔离；镜像摘要固定并记录 | `cluster-render -check`、`images.txt` |
| 负载机 | 与被测节点分开；每台运行 `capacity-test agent`，CPU 余量 ≥ 50% | 预检中的 Agent 检查；报告“实发达成”全部通过 |
| 时钟 | 被测节点、负载机、数据库 NTP 同步 | 报告中时钟误差 ≤ `slo.maxClockUncertainty` |
| 清单 | 列出全部平台进程 `/metrics`（每个角色的每个实例）、各主机 node-exporter（`nodes`）、Agent 地址、只读核对账户 | `capacity-test plan validate --plan <计划> --inventory <清单>` |
| 秘密 | 操作员令牌、Agent 令牌、只读 PostgreSQL / ClickHouse、OpenAPI Key 写入 0600 秘密文件 | 报告生成时的秘密泄漏检查 |
| 测试数据 | 专用测试租户；标准协议产品与 `stressAlarm` 规则可由 `fixtures.autoProvision: true` 自动准备（容量测试模块页面默认如此）；测试摄像头（启用视频时）需手工登记 | 预检中的“自动准备测试产品/规则” |
| AI | 先用 `harness-mock` 测平台调度；真实模型单独执行并设置 `maxRuns` 预算 | 报告覆盖表标注 mock / real |
| 备份恢复 | 备份服务配置独立的 `IOT_BACKUP_RESTORE_TARGET_DSN` | 备份页“恢复验证”手动试一次 |
| 故障命令 | 在执行故障的 Agent 主机上登记白名单（0600），命令只作用于测试环境 | 预检“故障动作”项 |

每次运行保留完整结果目录（`capacity-results/<runId>/`），并记录执行人、日期、提交号、镜像摘要与清单版本。

## 2. 基线与边界（单实例）

1. 用 `quick` 预设在低速率（如 10、20 条/秒）确认链路、核对与报告正常，`verdict` 必须为通过且证据完整。
2. 用 `capacity` 预设（示例 `cmd/capacity-test/examples/core-mixed.yaml`）搜索单实例边界，得到“稳定通过 L、在 U 失败”。区间过宽时缩小 `boundaryRelativeWidth` 或增加 `maxSteps` 复测。
3. 若边界由发压能力或策略限流决定（报告“发压能力”“配额/保护策略”），先扩充负载机或按正式限额记录，再继续后续步骤。

## 3. 扩容曲线（1 / 2 / 3 / 6 实例）

目的：确认横向扩容的收益与共享瓶颈。每档只改变被扩容角色的实例数，其余条件（计划负载组合、SLO、数据规模、镜像）保持一致。

1. 在集群清单中把目标角色（通常 `processor`，其次 `parser`、`gateway`）设为 1 实例，`cluster-render` 渲染并部署，执行 `capacity` 预设。
2. 依次调整为 2、3、6 实例，每次重新渲染、滚动部署并执行同一计划。清单的 `metrics` 同步列出新增实例。
3. 比较：

   ```bash
   go run ./cmd/capacity-test compare --runs <n1>,<n2>,<n3>,<n6> --instances <n1>=1,<n2>=2,<n3>=3,<n6>=6
   ```

   输出 `compare.md`、`compare.json`、`scaling.svg`。E(n) 低于 70% 时按报告瓶颈候选排查共享资源（PostgreSQL 锁与 WAL、Kafka 分区数、ClickHouse 合并、热点设备），修复后重测该档。
4. 任一运行证据不完整、负载组合不同或没有确认通过档时，`compare` 只并列不计算 E(n)，需补测后再比较。

## 4. 候选长稳

1. 取扩容后目标规模的建议运行值（报告中为 0.7 × 最高通过档，仅健康状态）。
2. 用 `soak` 预设在该速率保持至少 4 小时（上线前建议 24 小时），`suite: full` 并启用业务模块（示例 `full-system.yaml`），`fixtures.alarmRuleId` 开启告警核对。
3. 通过条件：全程完整性无缺失，积压无持续增长，时延 P95/P99 满足 SLO，主机 CPU、内存、磁盘无单调上升（`hosts.svg`）。
4. 控制机中途重启时用 `run --resume <runId>` 续跑；续跑后报告中会有 `resume` 事件，结论照常判定。

## 5. 故障容量矩阵

每个故障用 `resilience` 预设单独执行（示例 `resilience.yaml`）：背景负载取建议运行值，测量窗口内注入并恢复，`faults.maxRecovery` 为验收上限。常规 SLO 在故障档仅记录；完整性、排空与恢复时间决定结论。

| 故障 | 白名单动作示例（在对应节点的 Agent 上登记） | 期望行为 | 建议恢复上限 |
| --- | --- | --- | --- |
| Parser 实例停止 | `docker compose --project-directory <节点目录> stop iot-parser` / `start iot-parser` | 其余 Parser 接管分区，积压回落，无缺失 | 3 分钟 |
| Processor 实例停止 | 同上，服务 `iot-processor` | 租约到期后接管在途消息，告警与状态不重复、不丢失 | 3 分钟 |
| Gateway 实例重启 | 服务 `iot-gateway` 的 `restart` | 设备重连到其他 Gateway；inbox 未确认消息重放 | 2 分钟 |
| Jobs 实例停止 | 服务 `iot-jobs` | 另一实例在单例租约到期后接管周期任务 | 1 分钟 |
| Redis 主节点停止 | 服务 `redis` 的 `stop` / `start` | Sentinel 选主；限额回退为每实例分摊额度 | 1 分钟 |
| PostgreSQL 主库切换 | Patroni `switchover`（`patronictl` 命令写入白名单） | 写入经 `target_session_attrs=read-write` 连到新主库 | 2 分钟 |
| Redpanda 单 Broker 停止 | 服务 `redpanda` | RF=3 下生产与消费继续，重新选举分区 leader | 2 分钟 |
| ClickHouse 单副本停止 | 服务 `clickhouse` | quorum 写入按配置重试；查询走其余副本 | 3 分钟 |
| EMQX 单节点停止 | 服务 `emqx` | MQTT 设备重连到其余节点，QoS1 消息不丢 | 2 分钟 |
| Harness 实例停止 | 服务 `harness` | 业务流按会话哈希切换到其他实例；AI 模块成功率下降但平台其余功能正常 | 1 分钟 |
| 视频控制实例停止 | 持有 `video/control` 租约的 API 实例 | 备用实例在租约到期后接管 SIP 与媒体任务，播放会话从数据库恢复；其他 API 实例转发到新持有者 | 1 分钟 |

执行步骤：

1. 在故障所在节点运行 Agent：`capacity-test agent --listen :7070 --token-ref capacity-agent --secrets <文件> --fault-allow faults.yaml`，清单 `agents` 登记该 Agent。
2. 计划 `faults.actions` 引用 `{agent: <名>, action: <动作>, at: 60s, duration: 90s}`，`search.rates` 只写一个背景速率，`search.measure` 覆盖注入与恢复观察时间（例如 5 分钟）。
3. 运行后检查报告的故障表与 `recovery.svg`：注入与恢复均成功、恢复用时 ≤ 上限、完整性无缺失。恢复命令失败时 Agent 在释放时会再次尝试恢复，但仍须人工确认环境状态后再进行下一项。

## 6. 结果汇总与上线建议

- 汇总表：单实例边界、扩容曲线（含 E(n)）、长稳结论、各故障恢复用时与完整性、主机资源峰值，每项附运行 ID。
- 推荐运行值使用长稳通过的速率；若要承诺 N−1 容量，须以故障档在单节点失效期间的剩余吞吐为依据，不能直接用健康状态的 0.7 系数。
- 未执行或未通过的项目单列为“未验证”，不写入容量承诺。结果写入独立报告文档，并在 [集群方案 17.4 节](CLUSTER_AND_CAPACITY_PLAN.md#174-实施记录) 登记运行 ID 与结论。
