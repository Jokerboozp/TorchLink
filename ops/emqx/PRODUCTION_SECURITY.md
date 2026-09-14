# EMQX 认证与授权

仓库 Compose 使用 HS256 JWT、JWT 内嵌 ACL、文件授权兜底拒绝和匿名连接拒绝。平台与 Broker 的 `IOT_JWT_SECRET` 必须一致；凭据由环境准备脚本生成或部署方注入，不使用示例值。

## 身份与 Topic

| 身份 | 有效期 / 续期 | 授权范围 |
| --- | --- | --- |
| 内置管理员 Web 会话 | MQTT 令牌15分钟，前端续期 | 服务端返回的管理订阅；告警主题存在下述已知差异 |
| 普通 Web 用户 | HTTP 登录会话8小时，权限每次请求重验 | 不签发 MQTT 订阅令牌；`GET /api/v1/events` 仅返回授权设备状态和活动告警 |
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

## 普通用户设备范围升级

`POST /api/v1/mqtt/token` 和 `/api/v1/mqtt/load-token` 对普通用户拒绝访问，不以设备管理或总览菜单发放租户通配订阅。浏览器每3秒访问同源 `/api/v1/events`，服务端按最新角色和设备范围过滤。

升级前已签发的普通用户 MQTT 令牌最长15分钟有效。已有会话不能仅通过隐藏菜单撤销；部署切换时需要封禁旧普通用户 username 的重连并断开匹配会话，或等待其令牌全部过期。执行撤销前核对账户身份，避免影响设备 AccessKey 和内置管理员。受限账户的新 HTTP 会话不依赖 Broker 登录。

设备 JWT、平台消息消费者和管理员消息链路保持各自用途，不能用浏览器设备范围替代设备上报认证。配置及迁移步骤见 [部署维护](../../docs/DEPLOYMENT.md#用户权限升级)。

## 管理员告警主题的当前差异

源码中，`mqttToken` 签发的管理员告警订阅为 `/iot/alarm/{tenant}/#`，而 `Alarm.MQTTTopic` 发布格式为 `/iot/alarm/{eventType}/{city}/{district}/{building}/{deviceType}/{device}`，没有租户路径段。两者结构不一致，不能把现有管理员 MQTT 告警链路描述为已经完成主题匹配和租户隔离验收。

此项是本次文档核对发现的源码差异，本次未修改消息协议，也未新增管理员 MQTT 端到端验证。普通用户新的 HTTP 事件接口按登录租户及设备范围过滤，不依赖此主题。后续调整需统一发布、订阅及 ACL 的租户契约，不能仅放宽为全局告警通配订阅。
