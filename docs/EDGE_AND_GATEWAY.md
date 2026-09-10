# 独立接入进程

当前保留中心 API 与独立 Access Gateway。2026-09-10 按用户要求移除现场 Agent、边缘节点管理和程序升级；范围与旧配置处理见 [移除说明](EDGE_REMOVAL.md)。

## 进程职责

- `cmd/iot-platform` 默认 `IOT_PROCESS_ROLE=combined`，保留单进程入口。
- `IOT_PROCESS_ROLE=api` 不启动 Modbus、Listener 或外部 MQTT 上行订阅；保留 Raw 消费、Parser、规则及管理接口。
- `cmd/iot-access-gateway` 强制 gateway 角色：执行通信、鉴权和 Raw 归档，发布到共享 Kafka；不启动 Raw 业务消费者。HTTP 只开放接入与健康相关路由，管理用户身份在目标接口重新校验。
- api/gateway 两个进程必须配置同一个 PostgreSQL、Kafka 及一致的 Raw 分层存储。协议制品目录也必须共享；不能让两个进程各自使用内存仓库或本地消息总线。
- API 通过 `IOT_ACCESS_GATEWAY_URL` 转发接入测试、创建、标准上报、连接详情与命令；Gateway 不可达返回 503，不将请求已发送视为操作成功。

本地运行示例（在仓库根目录，凭据来自各自环境文件）：

```bash
go run ./cmd/iot-platform --env-file .env.api
go run ./cmd/iot-access-gateway --env-file .env.gateway
```

可选容器拆分：`docker compose --env-file .env.online -f compose.yaml -f compose.access.yaml config` 先检查渲染结果；实际启动再运行相同参数的 `up -d --build`。覆盖层将 TCP/UDP 端口从 API 移到 Gateway，默认 Gateway HTTP 端口为 8082。覆盖层使用 `!override`，要求 Compose 2.24.4 或更新版本，见 [Docker 合并规则](https://docs.docker.com/reference/compose-file/merge/)。本次未替用户部署已有业务服务。

## 执行所有权

启用 `IOT_ACCESS_COORDINATION=true`，并将 `IOT_ACCESS_NODE_URL` 设置为其他实例可达且精确指向本实例的 HTTP(S) 地址。不要使用随机负载均衡地址冒充固定执行节点。

共享 PostgreSQL 的 `execution_lease` 以租户和 Profile 为资源键。租约 10 秒，节点本地取消期限短于数据库期限，续租失败即取消旧执行；接管提升 fencing token。运行时在归档前检查所有权和当前配置。配置停止被扫描时释放租约。

设备或 Profile 的会话命令通过租约中的节点地址转发，保留原用户授权并限制转发次数。该实现提供互斥执行与接管，不宣称已有按负载最优调度、跨节点迁移现有 TCP 会话或数据库之外的强制 OS 隔离。

## 验证入口

`go test ./internal/platformapp ./internal/httpapi ./internal/protocolruntime` 覆盖进程职责、认证转发与执行协调；环境相关集成测试的实际执行条件见各测试。历史边缘验证记录保留在 `DEVICE_ACCESS_REFACTOR_PROGRESS.md`，不代表当前仍提供这些入口。
