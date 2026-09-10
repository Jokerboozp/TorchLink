# EMQX 认证与授权

仓库 Compose 使用 HS256 JWT、JWT 内嵌 ACL、文件授权兜底拒绝和匿名连接拒绝。平台与 Broker 的 `IOT_JWT_SECRET` 必须一致；凭据由环境准备脚本生成或部署方注入，不使用示例值。

## 身份与 Topic

| 身份 | 有效期 / 续期 | 授权范围 |
| --- | --- | --- |
| Web 用户 | 15 分钟，前端续期 | 当前租户告警和设备状态订阅 |
| 标准 HTTP/MQTT 设备 | 300 秒，设备重新取票并重连 | 自身四种 `/iot/up/...` 上行及 `/iot/down/.../command` 订阅 |
| 旧受管 Raw 设备 | 24 小时，以换票响应为准 | 自身 `/external/raw/...` 和命令，以及自身标准主题 |
| 平台接入服务 | 每次重连刷新服务凭据 | 接入主题订阅与平台业务发布，按启动装配签发 |
| 外部视频 / 采集方 | 按其身份配置 | 精确租户、产品或摄像头主题，禁止通配读取其他租户 |

标准主题及设备换票见 [统一设备接入](../../docs/UNIFIED_DEVICE_ONBOARDING.md)。`/jetlinks/raw/...` 是保留的原始接入 Topic 名称，不代表依赖 JetLinks 平台；不授予普通设备该内部通配订阅权限。

## Broker 必需配置

JWT authenticator 必须包含 `verify_claims = [{name = "username", value = "${username}"}]` 和 `disconnect_after_expire = true`，使连接用户名与令牌身份一致，并在过期后断连。Compose 配置中的美元符需写为 `$$`，渲染后 Broker 使用单个 `$`。

已有集群的动态认证配置可能覆盖 base.hocon，更新后需核对实际认证器。配置结构见 [EMQX JWT 文档](https://docs.emqx.com/en/emqx/latest/access-control/authn/jwt.html)。生产网络为 TCP / WebSocket 配置 TLS，将 Dashboard 和管理 API 限制在运维网络；EMQX 集群 Cookie 单独配置随机值。

## 凭据撤销与接收保障

即时封禁与断连需要平台配置专用 EMQX 管理 API Key，不能使用 Dashboard 密码或 MQTT JWT 替代。轮换 Secret 仅阻止后续换票，未配置管理适配器时，已有会话仍依赖 Broker 的过期断连。具体变量、重试和撤销状态见 [凭据与命令](../../docs/UNIFIED_DEVICE_ONBOARDING.md#凭据与命令)。

设备收到 PUBACK 不等于平台归档成功。平台持久队列、固定客户端身份及磁盘恢复边界见 [MQTT 接收保障](../../docs/DEVICE_RECEIVE_RELIABILITY.md#mqtt-接收保障)。
