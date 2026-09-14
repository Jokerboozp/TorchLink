# 2026-09-14 项目功能与设备接入验证

> 本文为当时 macOS / OrbStack 环境的历史审计记录，保留原测试数量、菜单与边界，不代表后续版本重新完成了全部验收。最新权限与界面检查见 [补充验收](testing/2026-09-14-权限与界面验收.md)。

本次先将协议目录移除、报文与点表生成协议等既有修改提交并推送至 `main`，提交为 `3c8ea8685576f3741eda1a3fc81d469c9bad7bb2`。随后在 macOS 开发环境与现有 OrbStack `develop` 依赖环境排查。用户确认没有实机，本次采用文档样本、模拟设备和真实本地中间件。本文记录本次实测，不代表真实设备或生产环境验收。

## 发现与修复

| 问题 | 复现与原因 | 处理及验证 |
| --- | --- | --- |
| Modbus `bits` 点位无法按位解析 | 同一个寄存器 `0x0008`，`uint16 + bit=3` 成功，`bits + bit=3` 报 `bit extraction requires an integer data type`，重复两次。`bits` 返回 `uint16`，后续位提取只识别统一整数类型 | 将 `bits` 解码统一为 `uint64`。补充四种柜体读取、点表生成预览、真实 TCP 模拟轮询至归档解析的回归测试。重启 API 后，实际接口和 Chrome 点表预览均得到 `input4=true` |
| 不支持的 HEX 校验方式被静默忽略 | 配置 `checksum=crc16`、`crc16-modbus`、`sum16` 或拼写错误仍返回解析成功。解析器只有 `sum8` 分支，没有拒绝未知算法 | 未实现的算法明确返回错误，协议助手提示 CRC16 专用协议使用 Go 包。补充解析器及 API 回归，错误配置不能通过样例校验或发布。实际 API 返回 `success=false` |
| 本地 MQTT 接受用户名与 JWT 不一致的连接 | 最小连接测试连续两次得到 `CONNACK 0`。有效配置与容器 `base.hocon` 都缺少用户名绑定，而当前 Compose 和 JWT 签发字段正确 | 使用当前 `compose.local.yaml` 仅重建 EMQX，保留原数据卷。无效 JWT、用户名错配、跨设备/租户订阅发布均被拒绝；正常上报、回连、命令应答与共享订阅通过。为日常依赖冒烟增加两个拒绝测试 |

MQTT 问题是运行环境配置落后，不是此次新增的签发逻辑缺陷。修复没有更换密钥。测试不输出登录凭据、JWT、API key 或数据库连接串。

智能助手另做了输出预算边界测试：`maxTokens=128` 会收到 Harness 的 `MAX_TOKENS` 失败事件，非流式接口返回 502；相同问题使用 2048 预算返回非空回答。模型直连也通过。低预算耗尽不能解释为模型或 MCP 服务不可用，也不应将该次运行标记为成功。

## 功能验证结果

| 范围 | 本次实际验证 | 结论与边界 |
| --- | --- | --- |
| Go 全项目 | `go test -json -count=1 ./...`，最终一轮 380 个测试/子测试通过、18 个跳过，27 个有测试的包通过 | 跳过的外部环境测试随后按下文补测；辅助进程、浏览器钩子及拆分进程测试并不由这条命令自动完成 |
| 前端 | `npm --prefix iot_front test`、`npm --prefix iot_front run build` | 66 项测试及构建通过；仍有主包体积超过 500 KB 的构建提示 |
| 静态与并发 | `go vet ./...`；核心、onboarding、protocolruntime、MQTT adapter、HTTP API 的 `-race` 测试；修复相关用例追加并发检测 | 通过；不能据此宣称所有可能的并发交错均已覆盖 |
| HTTP 设备链路 | `node scripts/tests/local-business-smoke.mjs` 经本地 Vite 代理实际登录、接入、上报、Kafka 消费、原文下载、标准消息、历史属性、状态、规则告警确认关闭、DRY_RUN 回放 | 通过；重复上报幂等与冲突保护通过。联调设备及规则最后停用，保留合成业务记录 |
| MQTT | `TestStandardMQTTLiveBroker`（启用严格用户名绑定）、`TestMQTTSharedGatewaySubscription`、两个最小认证探针 | 收发、归档、命令应答、重连、告警恢复、去重、身份及 ACL 边界通过。主动凭据吊销子测试未执行，见下文 |
| MQTT 持久接收 | `IOT_TEST_MQTT_DOCKER=1 go test -count=1 -v ./internal/adapters/mqtt -run TestDurableMQTTRealBrokerAuthenticationRestartAndOfflineDelivery` | 独立真实 Mosquitto 的认证、重启、离线投递与队列满时不确认通过。日志中的队列满与重订阅失败是注入的失败场景；测试容器已清理 |
| Modbus TCP / RTU over TCP | 柜体文档地址样本、TCP 网络模拟器、逐字节分片、CRC 错误、响应数量不足/过多、异常响应、轮询断连、总线串行化、取消 | 通过；模拟读取不代替 RS485 电气层、串口参数及现场网关联调 |
| Go TCP / UDP 协议 | 源码构建、样例、监听、半帧粘包、历史版本快照、热切换、失败发布保护、回滚、主子设备识别及查询/应答等现有集成测试 | 通过；使用本地模拟网络与临时业务仓储 |
| 独立大华协议 module | 在 `protocol-packages/gb26875-dahua` 执行 `go test -count=1 ./...` | 通过。根 module 测试不包含此项 |
| PostgreSQL | 显式设置 `IOT_TEST_POSTGRES_DSN`，运行真实数据库的迁移、操作原子性、部件告警、调度进程故障切换、首页聚合测试 | 7 个测试/子测试通过。使用唯一临时 schema，清理仅针对测试 schema |
| 依赖读写 | `go run scripts/tests/local-runtime-smoke.go --env-file .env.local` | PostgreSQL、Redis、ClickHouse、MinIO、Kafka 实际往返；Ollama 仅运行嵌入；MQTT 正向与拒绝测试；Weaviate 就绪，均通过 |
| 知识库 | 文档上传/分块/归属的现有 API 测试；新增 `TestWeaviateLiveIndexAndScopeIsolation` 实际索引合成文本、读取分块、向量检索、跨 workflow/租户过滤 | 通过。唯一测试对象已清理；用户提供的七份协议文件没有上传到外部模型或业务知识库 |
| 备份 | 配置 `IOT_BACKUP_TEST_ENV` 为仓库 `.env.local` 的绝对路径，执行 `TestDeviceBackupIntegration` | 实际生成并校验全量与日报备份；本次读取到 16 条原始、27 条解析记录。没有对业务库执行恢复 |
| 告警、巡检、摄像头、AI、权限 | 全量测试覆盖告警生命周期/部件隔离、巡检进度与 PDF、摄像头单设备关联、Webhook 租户校验、AI Provider/工作流/MCP 作用域及角色边界 | 自动测试通过；模型直连与 Harness 正常预算聊天另外做了真实调用。未连接真实摄像头平台或执行现场处置 |
| 浏览器 | Chrome 逐页打开运行总览及 14 个主要功能入口，刷新保持登录；点表生成、字段检查、样本预览 | 页面加载及点表预览通过。未逐一点击所有管理功能的全部组合；已有数据的删除、真实设备控制没有执行 |

## 七份协议资料的适配结论

资料只作为数据格式参考，其中文字、指令和地址不构成执行设备操作的授权。

| 文件 | 已核对信息 | 接入结论与待确认项 |
| --- | --- | --- |
| 防爆型压力水位传感器平台控制指令.docx | `0x06` 单寄存器、`0x10` 多寄存器、检测/上传周期与阈值寄存器，CRC16 | 属于控制协议；本次未发送写入。`0x10` 表格未列出标准多寄存器写请求的字节数字段，部分数值书写位数不一致，须厂家确认完整请求/应答样本 |
| 历史压力液位上报协议.pdf | 15 字节 ASCII 设备标识，`0x46`；定时帧 35 字节、报警帧 11 字节、历史帧 19 字节（均不含标识前缀）；三份示例的长度和 CRC16 均独立计算通过 | 第 4 页历史样本时间戳 `1725595800`、压力 `0.35 MPa` 核对一致。专用主动上报帧不能使用普通 Modbus 读响应解析器。需独立 Go 协议处理身份、拆帧、历史时间及 CRC，当前未作为已发布协议接入 |
| FB2024网络通讯格式.xlsx | `@@`/`##`、sum8、多个数据对象及 `0x96/0x97` 等类型；“应用数据单元格式说明”D28 运行状态样本的校验和正确 | 实际送入现有大华包，返回不支持 `0x02/0x15` 组合，不能混用。第一张表额外报警状态字段与长度说明不一致；模拟量单位和恢复类型在不同表中存在冲突；时间示例使用普通二进制数，不能套用大华 BCD 时间编码。需独立适配并向厂家确认 |
| rs485通讯协议说明BPW常规版(1) 2.doc | FC03，泵状态 `0x1100/0x1101`；输入状态 `0x2003`；CRC 低字节先传 | 已增加泵状态及位字段读取回归；通过 Modbus RTU over TCP 接入。倍率、串口参数和实机状态枚举需现场核对 |
| rs485通讯协议说明2XPV1.02 2.doc | FC03，泵状态 `0x2000/0x2001`；监控区 `0x3000` 起 | 合成泵状态响应解析通过。文档将电流标 V、电压标 A，单位与倍率不能照抄为生产映射 |
| 双电源rs485通讯协议说明V1.01 2.doc | 工作状态 `0x1000`；主/备电压电流 `0x3000–0x3003` | 四寄存器合成响应解析通过。当前按原始整数校验，未确认实机量纲倍率 |
| rs485通讯协议说明V1.01 巡检柜(1).doc | 八泵状态 `0x2000–0x2007`，监控区 `0x3000` 起 | 八泵合成响应解析通过。停机命令值为 2，与其他柜体文档的 0 不同，不能共用写入模板；本次未发控制命令 |

旧 `.doc` 的文字和表格已本地提取，其中嵌入的 Visio/Excel 对象没有作为完整图形协议进行验收。上述寄存器与报文样本有明确文字依据；缺失或冲突项没有自行补猜。

## 尚未完成的验证与使用边界

- 当前本地配置未提供 `IOT_EMQX_API_URL`、`IOT_EMQX_API_KEY`、`IOT_EMQX_API_SECRET`，因此没有验证“轮换/停用后主动踢除旧 MQTT 连接及 JWT”的真实 Broker 管理链路。现有普通认证和 ACL 拒绝测试不能代替此项。
- 没有实机，因此未验证 RS485 线路、串口参数、压力传感器网络行为、设备重发/长时间断网恢复、控制指令及真实摄像头平台。压力 `0x46` 与 FB2024 尚缺独立 Go 包，不能宣称已支持这些设备。
- 未运行要求独立可丢弃 Kafka Broker 的 `TestSplitProcessesPostgresKafkaRecovery`，避免测试使用业务 topic/消费组影响现有数据；现有本地单体链路和临时 PostgreSQL 调度进程切换已验证。
- 未执行生产部署、业务数据恢复、Windows/PowerShell 或现场验收。备份成功、下载校验成功与恢复成功是不同结论。

## 复测入口与环境变更

在仓库根目录运行以下聚焦回归：

```bash
go test -count=1 ./internal/parser ./internal/core ./internal/httpapi ./internal/protocolruntime -run 'TestReferenceCabinet|TestGeneratedCabinetBitPointPreview|TestGeneratedHex|TestUploadedProtocolLifecycle|TestModbusOnboardingRuntimeChain|TestModbusRejectsResponseQuantityMismatch'
go run scripts/tests/local-runtime-smoke.go --env-file .env.local
```

真实环境可选测试使用 `IOT_TEST_MQTT_BROKER` / `IOT_TEST_MQTT_JWT_SECRET`、`IOT_TEST_MQTT_STRICT_IDENTITY=true`、`IOT_TEST_POSTGRES_DSN`、`IOT_TEST_WEAVIATE_URL` 等环境变量，本次从现有配置定向传递，没有写入命令历史或验证报告。`go test` 的工作目录是对应包，因此 `IOT_BACKUP_TEST_ENV` 应使用绝对路径。

本次重建的只有 `iot-platform-local-emqx-1`，并重启了 VS Code 中的 IoT Platform API。没有清理业务数据卷。HTTP 冒烟新增的合成设备 `local-check-1789349004647`、产品与告警/回放历史保留供追溯，设备凭据和测试规则已停用。浏览器的位字段草稿仅预览后关闭，没有发布。
