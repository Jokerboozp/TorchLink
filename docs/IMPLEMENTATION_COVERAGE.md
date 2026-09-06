# 设计文档实现覆盖表

JetLinks 协议包改造（原文第 6 章）按用户要求排除。其余章节映射如下：

| 文档范围 | 实现位置 | 状态 |
|---|---|---|
| Go 平台二开与主链路 | `internal/core`、`internal/ports`、`cmd/iot-platform` | 已实现 |
| EMQX + Kafka/Redpanda | JWT/ACL、浏览器 MQTT WS、微批 Kafka Writer、解析成功后的 Kafka/MQTT 双通道输出、Compose | 已实现；解析成功的标准消息同时进入类型化 Kafka Topic 和租户 MQTT Topic，失败报文不外发 |
| Topic、幂等、Retry/DLQ | Raw outbox 状态、重试扫描、Kafka consumer retry 和独立 DLQ | 已实现 |
| Parser/Normalizer | `internal/parser` | 已实现插件注册表及 JSON/烟感/Modbus 示例 |
| 独立设备管理与接入 | 产品/协议包/设备注册表、设备凭证 API、协议调试与管理页面 | 已实现，不依赖 JetLinks |
| L0-L9 分层存储 | MinIO、PostgreSQL、ClickHouse、Redis、Weaviate 适配器；原始日志按设备频率分层，backup-service 每日汇总到 MinIO | 已实现；默认 60 秒阈值，定时任务和目标环境仍需运行态验收 |
| 在线/离线判定 | `internal/core/engine.go` | 已实现三状态、超时扫描和状态事件 |
| 规则、告警生命周期 | JSON + Gengine、物模型校验、冲突检测、Redis 活动缓存 | 已实现并有自动化测试 |
| MQTT WebSocket 前端订阅 | 内置 MQTT 3.1.1 WS 客户端、短期 ACL Token、自动续期 | 已实现并由真实 EMQX 鉴权实测 |
| API/BFF 与消防页面 | `internal/httpapi`、`iot_front` | 已实现 Gin API/MCP 与独立 Vue 3 Web 前端，Nginx 同源代理 |
| 视频识别接入 | Webhook/MQTT、HMAC、摄像头元数据、摄像头到设备一对多反向查询、跨源融合、持久异步媒体转存 | 已实现并有自动化测试；平台不解析/采集/代理直播流 |
| AI 告警诊断 | Eino 编排、Ollama、10m/1h/24h 趋势、同类告警、知识检索 | 已实现异步旁路与失败降级 |
| AI 运维助手 | `core.OpsChat` | 已实现受控数据上下文，不执行 SQL/设备控制 |
| RAG | PDF/DOCX/PPTX/XLSX/ODF/文本抽取、重叠分块、MinIO、持久化 Weaviate、Agent 直接归属 | 已实现并有 Office 抽取、Agent 隔离测试；Compose 默认启动 Weaviate/Ollama |
| 规则助手 | Eino rule-draft、纯 JSON 字段说明、注释版 Gengine 占位符、Schema/物模型/Gengine/冲突校验、人工确认 | 已实现，AI 草稿强制禁用 |
| 智能巡检文档 | 确定性健康快照、AI 建议、同一快照 PDF 下载 | 已实现并有 PDF 解析与 HTTP 下载测试 |
| 摄像头关系与直播定位 | 摄像头基础信息、一个摄像头最多一个设备、一个设备多个摄像头、按设备反向查询、告警带出元数据 | 已实现；平台不保存直播地址，不提供预览或拉流服务，直播由外部视频平台负责 |
| 厂商视频 SDK | 外部视频平台的 HMAC Webhook/MQTT 告警接入 | 已实现租户绑定和签名单测；海康/大华取流 SDK 不再由平台运行时接线 |
| 报表 | AI reports API | 已实现日报/周报/月报数据聚合与生成入口 |
| MCP 工具服务 | `internal/mcpserver`、`ai_tool_call_log` | mcp-go Streamable HTTP、租户隔离、每次工具调用审计 |
| 回放/补偿 | `internal/core/replay.go` | 三模式、指定版本、限速、真实前后消息差异与审计均已实现 |
| 备份/恢复 | 多存储 backup-service、原始日志每日 `RAW_LOGS` 制品、MinIO-DR、WAL、manifest/哈希、恢复演练 API、备份中心记录与制品下载 | 已实现；全量/增量改为手动，原始日志默认每天 00:05（Asia/Shanghai）备份，目标环境仍需复核定时任务和实际恢复 |
| 安全 | JWT/RBAC、设备换票、MQTT ACL/HMAC、媒体白名单、AI 工具边界 | 已实现；生产 TLS/企业身份源仍属部署配置 |
| 监控日志 | 全量核心指标、Prometheus、EMQX/Redpanda scrape、备份告警、Grafana/Loki | 已实现 |
| Compose/Kubernetes | `compose.yaml`、`deploy/k8s` | 已实现，YAML 有自动解析测试 |
| 市级容量与压测 | HTTP/MQTT + steady/burst/offline loadgen、JSON 百分位报告 | 工具已实现；本机 100 QPS 烟测通过，30k/50k 和 2–8h 须在目标集群验收 |

说明：代码功能项和本地集成链路已完成，但设计中的市级 30k/50k 吞吐、2–8 小时稳定性、HA 切换时长与异地网络 RPO/RTO 是部署验收指标，不能由单机源码测试宣称达标。仓库已提供可复现压测、备份和恢复演练入口。
