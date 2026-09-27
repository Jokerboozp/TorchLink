# 2026-09-27 容量压测证据

解读与结论见 [全系统压测报告](../../CAPACITY_TEST_REPORT_2026-09-27.md)。这组文件来自 Mac 源码调试 API 与原有 OrbStack `develop` 依赖环境，不是集群或生产服务器验收。全程没有新增容器。

| 文件 | 内容 |
| --- | --- |
| [levels.json](levels.json) | 按场景、速率或并发保存请求结果、错误分类、完整响应吞吐、分位延迟与管道指标 |
| [business.json](business.json) | 巡检、PDF、告警确认、知识库、回放、备份、开放接口等业务操作及核对摘要 |
| [observations.csv](observations.csv) | 1,707 次独立就绪 / 管理接口探针、累计指标及 API / VM 资源采样，约每 5 秒一次 |
| [phases.json](phases.json) | 按阶段汇总独立探针、API RSS 和约 15 秒采样的容器 CPU 峰值 |
| [environment.json](environment.json) | 硬件、提交、API 进程与既有容器标识、创建时间、重启及 OOM 状态 |
| [emqx-observations.jsonl](emqx-observations.jsonl) | MQTT 关键阶段的 Broker 计数与平台订阅状态 |
| [commands.jsonl](commands.jsonl) | CLI 各阶段命令、开始 / 结束时间、进程退出码；凭据参数只保留文件引用 |
| [capacity-curves.png](capacity-curves.png) | 从阶梯数据生成的吞吐、积压、PDF 与查询延迟图 |
| [test-timeline.png](test-timeline.png) | 全程队列积压、管理响应、API RSS 与 VM swap 变化 |

字段口径：

- `okQps` 是整个响应或协议 ACK 完成的速率。HTTP 202、MQTT PUBACK、TCP / UDP ACK 不证明后续解析、规则、持久化或 AI 成功。
- `p95ms` / `p99ms` 包含失败请求；快速拒绝可能让延迟下降。`durationSec` 包含等待在途请求收尾的时间，可能长于预设发压时长。
- `metricsValid=false` 表示管道计数器缺失 / 重置；此时归档、解析数值不能按零吞吐解释。早期工具没有该字段，结合独立探针与数据库核对解读。注册 / 连接模式不测管道吞吐。
- `observations.csv` 的空值表示该次未采到，不按零值解读；各接口的 `status`、`ms`、`error` 分列保留。容器资源保留在 `phases.json` 的阶段峰值中，API RSS、VM swap 保留时间序列。
- `storageLagEnd` 单列存储消费组，`lagEnd` 是全管道 lag，可包含 AI。计数器值是累计值，API 各次重启前后不能直接相减。最终数据核对以唯一原文 / 标准消息 ID 为准。
- lag 归零不代表死信已处理；本次 `business.json` 的 `dlq-audit` 保存了 36 条唯一消息对应的 65 条存储死信及错误分类。ClickHouse 只接收属性 / 告警遥测，核对时排除 STATE_CHANGE，再单独检查缺失遥测。
- `business.json` 的突发操作 `qps` 是全部请求的完成速度，包含错误；需要看 `codes`。MCP 检索将 HTTP 200 中的 `isError=true` 归为结果码 502，原始 HTTP 状态另记于 `httpStatusCounts`。回放的“提交完成”与后台任务处理完成分开保存。
- 设备规模和 MQTT 归档核对统一保存在 `business.json` 的 `device-population-after-protocols`、`mqtt-delivery-audit` 中。巡检提交、清理操作及恢复过程保留数量、状态、分位延迟或首末采样摘要；死信按消息去重并保留投递次数。
- 开放接口 207 按 accepted / rejected 消息数统计；429 是限流，不能按请求全部成功计算。摄像头写入、备份校验等短突发未证明长稳最大吞吐。
- 负载机 `gen-backlog`、临时端口不足、reset、timeout、响应体截断分别保留。不能全部归因于业务服务内部失败。
- `commands.jsonl` 的退出码 0 表示脚本执行完，不表示该阶段业务全部成功。具体成功 / 拒绝 / 超时查看结果文件。

无效或混杂的早期轮次（仅两个发布方的 MQTT 测试，以及前置过载 / 空闲 Kafka 错误影响的两轮总览）不进入主阶梯表；相应故障期仍保留在独立观测中。MQTT 故障轮后期旧计时逻辑导致速率不准确，报告只使用其 PUBACK 数、错误与持久化核对，不采用虚高 QPS。

证据导出删去了登录令牌、设备密钥、API Key、原始业务内容及进程环境。包含本次隔离前缀和组件名称，便于在自己的测试环境核对；这些标识不是生产设备容量承诺。测试结束后的清理已删除 `data/capacity/20260927/` 临时目录，包括一次性脚本、二进制、Python 环境、日志和重复导出；可复用工具位于 `cmd/capacity-test/`，复现入口见报告。
