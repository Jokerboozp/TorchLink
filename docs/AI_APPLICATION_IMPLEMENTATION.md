# 五项 AI 应用实施记录

需求基准为 [开发方案](AI_APPLICATION_DEVELOPMENT_PLAN.md)。本文记录本任务的实际交付和验证，设计正文不作为验收证据。

## 分阶段交付

| 批次 | 内容 | 当前状态 |
| --- | --- | --- |
| A | 共享持久任务、当前权限校验、事实查询、区间起点、快照、证据与配置版本 | 基础实现和下列回归已通过 |
| B | 数据可信度配置、指标、曲线、发现、核实、导出与显式 AI 解读 | 未交付 |
| C | 监测连续性、历史接入依赖、假设影响、核实、导出与显式 AI 解读 | 未交付 |
| D | 规则历史、固定数据集、隔离双分支、标签、候选发布与报告 | 未交付 |
| E | 流程版本、演练与真实案例、节点、固定复盘、独立整改与值班关联 | 未交付 |
| F | 实物资产、维修验收、观察分母、投入排序、资金权限与人工决定 | 未交付 |
| G | 联合浏览器、备份恢复、真实样本试点与最终文档 | 未交付 |

各批次分别提交并推送；前置基础的完成不代表五项业务已验收。

## A 批次实现边界

- `internal/model/analytics.go` 和 `internal/ports/analytics.go` 定义分析任务、不可变快照、证据、配置版本、人工核实与独立 AI 版本。
- `internal/analytics/` 和 PostgreSQL `analysis_document` 提供同租户同应用单个运行事实任务、事务领取、递增租约令牌、检查点与输出共同提交、停止、幂等、版本冲突及分页。测试内存仓储采用相同状态转换。
- `internal/analytics/service.go` 与 HTTP 层重读当前身份和权限。设备集合必须明确；范围收窄后拒绝包含越权设备的整份任务及结果，列表总数也按当前可见完整任务计算。
- `internal/ports/analytics_facts.go` 的八项批量读取在 PostgreSQL 一致性读取中绑定来源版本。ClickHouse 遥测读取记录独立来源截止点，并对齐已固定的标准消息身份。
- `measurement_availability` 保存存储首次成功确认后登记的不可变可用时间。两次提交之间中断、旧记录或证据缺失保留未知；历史 `processed_at` 只标记为保守的处理阶段依据。原始接收时间、设备时间和可用时间分开。
- 状态区间读取窗口开始前的可信起点及窗口内转换；缺失起点、旧事件不完整、采集前的历史关系分别报告限制。新增配置历史从部署时刻开始，不能补造过去生效区间。
- 公共运行 API 为 `/api/v1/data-quality/runs`、`/api/v1/monitoring-gaps/runs` 和 `/api/v1/rule-lab/runs`，以及任务读取、停止、快照、证据、指标和发现读取。应用处理器尚未注册时创建返回 503，避免生成永不执行的排队任务；各应用完整契约按后续批次接入。

共享服务不持有生产告警、设备控制、消息队列或实时通知写入端口。AI 版本与事实状态分开存储；完整 Harness 解读、MCP 快照工具和各应用页面仍需后续批次交付。

## 资源保护配置

| 环境变量 | 默认值 | 作用 |
| --- | --- | --- |
| `IOT_ANALYTICS_WORKERS` | `2` | 每进程事实计算 worker 数，范围 1–64 |
| `IOT_ANALYTICS_MAX_DEVICES` | `1000` | 单任务显式设备数上限 |
| `IOT_ANALYTICS_QUEUE_LIMIT` | `100` | 同租户同应用待执行任务队列上限 |
| `IOT_ANALYTICS_BATCH_SIZE` | `1000` | 一次提交的指标、发现和证据合计数量上限 |
| `IOT_ANALYTICS_MAX_RANGE` | `744h` | 单任务最大时间窗口 |
| `IOT_ANALYTICS_RUN_TIMEOUT` | `30m` | 从首次领取起的总计算时限，接管不重置 |
| `IOT_ANALYTICS_LEASE` | `30s` | 执行租约，至少 300ms |
| `IOT_ANALYTICS_POLL` | `1s` | 无可领取任务时的检查间隔 |

这些参数只用于资源保护，不证明生产吞吐或统计有效性。API 列表默认 20 条、最多 100 条；内部事实批量读取采用独立上限和绑定来源版本的游标。

## 本次验证

日期：2026-10-01。宿主机运行源码测试，基础服务只使用既有 OrbStack `develop` 虚拟机中的 PostgreSQL / ClickHouse。数据库测试创建独立临时 schema 并在结束后清理，不改业务数据；测试连接配置通过进程环境注入，不记录凭据。

| 验证 | 结论 |
| --- | --- |
| `go test ./cmd/... ./internal/...` | 完整后端源码测试通过；常规全量命令不替代单独注入数据库配置的真实 SQL 回归 |
| `go test ./internal/analytics ./internal/config ./internal/httpapi -count=1` | 公共服务与既有配置、API 回归通过 |
| `go test -race ./internal/analytics -count=1` | 任务执行、固定快照、租约与权限撤销回归通过 |
| `go test ./internal/httpapi -run '^TestAnalysisAPI' -count=1` | 创建幂等、跨租户、范围收窄、分页与完整正文拒绝读取通过 |
| PostgreSQL `go test ./internal/adapters/postgres -run '^TestAnalytics' -count=1` | 真实数据库共享存储、扁平分页、历史转换、Collector 与资料遮蔽回归通过 |

ClickHouse 真实序列一致性、五项页面及 390px 浏览器操作、云模型、备份恢复和真实现场试点尚未作为本批次验收完成。不得将已通过的存储测试解释为五项应用已完成。
