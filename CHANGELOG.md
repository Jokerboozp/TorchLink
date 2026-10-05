# 变更记录

按发布版本记录对部署、使用和接口有影响的变化；每个 GitHub Release 的说明另附上次发布以来的完整提交列表。发布前把“未发布”改为版本号与日期。

## 未发布

### 新增

- 智能助手对话保存到服务端（按租户、用户、智能体与授权版本隔离，随 AI 日志留存天数清理），页面提供历史对话列表、新对话与“回到底部”。
- `/metrics` 可用 `IOT_METRICS_TOKEN` 校验 Bearer 令牌，部署脚本自动生成，Prometheus、容量测试与集群渲染同步携带。
- 每个响应带 `X-Request-ID`，内部错误编号即请求编号；新增 `http_request_duration_seconds` 指标与接口 5xx、延迟、主机内存与负载、核心组件、Redpanda 副本告警。
- `iot-platform migrate [--check]`：升级前单独执行或查看待执行的数据库迁移。
- 前端：深色主题、侧栏菜单搜索、窄屏表格卡片与筛选折叠、表单字段级错误提示、月份选择控件。
- 由路由生成的 [接口清单](docs/API.md)，CI 检查与路由同步。

### 变更

- 聊天与业务 AI 运行共用 `aiworkflow` 编排；Harness 工具凭据使用独立签名密钥（默认由 `IOT_JWT_SECRET` 派生，可用 `IOT_HARNESS_JWT_SECRET` 指定）。
- 知识文档索引改用共享锁并按轮处理队列，重建仍独占。
- 单机 Compose 为长驻服务设置可覆盖的内存上限；Web 以非 root（`nginx-unprivileged`）运行，备份服务降权运行并自动修正旧暂存卷属主；Dockerfile 基础镜像以摘要固定。
- Web 代理只对外转发 `/health/live`，`/health/ready` 只在 API 端口与内网访问。
- Prometheus 主配置、告警规则与 Grafana 数据源直接引用 `ops/` 文件，离线包同步包含 `ops/`。
- 服务端内部错误不再把错误原文返回浏览器。

### 修复

- 受限设备范围用户不能读取回放任务；分页参数统一校验。
- 手机端按钮式单选组与设备范围选择不再超出弹层。
- Markdown 渲染去除原文中的 NUL，避免伪造占位符复制链接。
