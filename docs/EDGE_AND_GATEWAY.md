# 独立接入进程与现场节点

本文件记录扩大 P2 后实际已接上的能力；完整待办及本轮测试见 `DEVICE_ACCESS_REFACTOR_PROGRESS.md`。当前 Edge 支持 Modbus TCP/RTU、OPC UA、SNMP、BACnet/IP 读取、ONVIF 发现与元数据、Go Worker、远程命令及 HTTPS 持久化补传；程序升级见独立启动器说明。

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

## 现场 Agent

1. 在“Edge 节点登记”新增节点，点击“凭据”生成一次性 Secret；保存后无法再次读取，轮换会使旧 Secret 失效。
2. 在现场配置 `IOT_EDGE_PLATFORM_URL`、`IOT_EDGE_TENANT_ID`、`IOT_EDGE_NODE_ID`、`IOT_EDGE_SECRET`、`IOT_EDGE_DATA_DIR`、`IOT_EDGE_ALLOWED_CIDRS`。默认要求 HTTPS；`IOT_EDGE_ALLOW_HTTP=true` 仅用于明确的隔离测试。
3. 在现场执行 `go run ./cmd/iot-edge-agent --env-file .env.edge`，或运行同名构建产物。
4. 在节点“运行与分配”中将已有 Modbus 采集配置分配给该节点，中心停止执行对应配置。查看实际心跳、队列深度、采集结果及 Raw/Standard 证据。

也可在“添加设备”中选择 Modbus TCP 或 RTU，并在高级设置选择现场节点。节点必须有近期心跳，平台才下发只读诊断；任务只允许被一个执行器领取，10 秒内返回真实响应后由平台解析。测试样例不写入业务报文，失败不签发接入凭证。凭证绑定参数与版本，保存后才启用周期采集。

RTU 需要在现场配置 `IOT_EDGE_SERIAL_PORTS`（例如 `/dev/ttyUSB0` 或 `COM3`，多项逗号分隔），平台不能扩大本地白名单。串口采用 8 数据位，可配置波特率、N/E/O 校验、1/2 停止位和 1–247 站号；支持读功能码 01–04，检查 CRC、站号、功能码、异常和响应长度，并串行使用同一物理串口。RS485 使用自动收发方向的转换器；尚未实现手动 RTS 方向切换，也未验收物理 RS485 硬件。Raw 保存实际 RTU 响应及 CRC，解析复用版本点表。

平台接收 Listener 的 LISTENING/PENDING 心跳并保存运行状态。LISTENING 仅表示监听已启动，不伪造首条数据时间；仅在完整帧成功进入节点补传队列后记录实际接收时间，短连接关闭仍保留该记录。采集成功时间与运行错误时间分别投影，旧配置的心跳不能覆盖用户禁用、重新分配或修改后的配置。

节点每 5 秒同步配置、每 10 秒发送心跳。配置缓存不含节点 Secret；已认证缓存最多离线使用 24 小时，过期停止采集。节点凭据被明确拒绝后停止当前采集；离线时无法即时获知服务端撤销。

磁盘 outbox 最多 64 MiB / 10,000 项，单项最多 1 MiB；一个目录只允许一个进程打开。先写临时文件并刷新，再原子替换；失败不确认接收。上传失败以 1–30 秒退避重试。只有服务端返回匹配的 Raw ID 才删除队列项；重启继续发送同一 ID，不重造业务时间。Windows 使用文件锁及内容刷新，尚未执行 Windows 掉电恢复验收。

被删除、禁用、重新分配或原文冲突的数据收到永久拒收后移入磁盘隔离区，继续发送其余报文；隔离项计入容量上限并在心跳报告数量。修复配置后，停止节点并使用 `--retry-rejected` 重新加入队列；认证失效和暂时故障保留原项退避重试，不删除数据。

## 验证入口

OPC UA / SNMP 同样在添加设备向导选择现场节点，填写 Node ID / OID 与本地凭据引用。协议版本支持 1–64 个读取点，向导目前提供单点表单；多点可通过接入请求的 `readPoints` 数组配置。实际认证/读取/平台解析均通过后才能保存。

凭据通过节点的 `IOT_EDGE_CREDENTIAL_FILE` 指向本地 JSON，Unix 要求权限 `0600`，更改后重启节点加载。示例结构如下（占位值不构成可用凭据）：

```json
{
  "opc-controller": {
    "username": "现场账号",
    "password": "现场密码",
    "certificateFile": "/secure/client.der",
    "privateKeyFile": "/secure/client.pem",
    "serverSha256": "可信服务器证书的64位SHA256"
  },
  "snmp-device": {
    "snmpVersion": "3",
    "username": "现场账号",
    "password": "认证口令",
    "privacyPassword": "隐私口令"
  }
}
```

OPC UA 固定 Basic256Sha256 + SignAndEncrypt，验证服务器证书指纹、有效期、主机名和用户名身份，不回退到匿名或明文。SNMP v3 使用 SHA256 + AES；显式选择 v2c 时必须提供 `community`，不使用默认社区。Raw 是认证后的实际服务响应 JSON，OPC UA 保留 ReadResponse，SNMP 保留变量、请求号和协议状态；不将其描述为加密链路抓包。坏质量和缺失点保留未解析状态。

BACnet/IP 使用目标 IP 与 UDP 端口（默认 47808）进行只读 ReadProperty，点地址为 `对象类型:实例:属性[:数组下标]`，如 `0:1:85`。本地访问引用配置为 `{"bacnetMode":"ip"}`，并受节点网络白名单约束。原生 BACnet/IP 无密码认证，Raw 明确记录此边界并保留完整请求/响应 HEX；支持普通标量，分段、路由 NPDU、复杂数组和 BACnet/SC 暂不支持，遇到这些响应保留失败证据。

真实服务器测试（依赖装入独立环境，不属于平台运行依赖）：

```bash
python3 -m venv /tmp/iot-protocol-test-venv
/tmp/iot-protocol-test-venv/bin/pip install -r scripts/tests/field-protocol-requirements.txt
IOT_TEST_FIELD_PYTHON=/tmp/iot-protocol-test-venv/bin/python go test -race ./internal/fieldprotocol ./internal/httpapi -run 'Test(AuthenticatedOPCUAAndSNMP|EdgeAuthenticatedFieldOnboarding)' -count=1 -v
```

```bash
go test -race ./internal/edgeagent ./internal/protocolruntime
go test ./internal/httpapi -run 'Test(SplitGatewayHTTPFlow|ExecutionRouteUsesTenantLeaseAndPreservesAuth|EdgeAgentDurableModbusChain)' -count=1 -v
```

`TestEdgeAgentDurableModbusChain` 使用真实本地 Modbus Socket 和 HTTP API，先让上传返回 503，再关闭/重建 Agent，从磁盘补传原消息并检查 StandardMessage。`TestEdgeRTUOnboardingWithActualSerialRead` 使用操作系统伪终端测试现场诊断、接入、串口采集及归档解析，包含非法串口拒绝。业务仓库/总线为测试内存实现，不是厂商真机验收。
# ONVIF 摄像头元数据读取

摄像头新增/编辑页可通过现场节点读取 ONVIF `GetDeviceInformation`，填写空白的品牌、型号和序列号，仍须核对并保存。接口为 `POST /api/v1/integrations/video/onvif/test`，要求 operator 权限，参数为 `edgeNodeId`、`host`、`port`、`credentialRef` 和可选 `endpointPath`。预览不创建摄像头、不产生业务消息。

节点本地凭据文件增加例如 `{"camera":{"username":"现场用户名","password":"现场密码","tlsCaFile":"/etc/iot-edge/camera-ca.pem"}}`；平台只接收引用名称。默认 HTTPS，校验主机名和证书，支持 WS-Security UsernameToken PasswordDigest 及 HTTP Digest（MD5 / SHA-256，auth）。旧设备仅在本地明确配置 `allowInsecureHttp:true` 时使用 HTTP。摄像头时钟须同步。SOAP Fault、认证失败和无效响应返回错误，不填写成功数据。

同一表单支持通过现场节点发现候选地址。节点本地设置 `IOT_EDGE_DISCOVERY_INTERFACES` 为允许发现的 IPv4 网卡地址（多项逗号分隔），响应来源还须属于 `IOT_EDGE_ALLOWED_CIDRS`。操作员选择节点、填写网卡地址并点击发现；平台无法扩大本地许可范围。接口 `POST /api/v1/integrations/video/onvif/discover` 接受 `edgeNodeId` 和 `interfaceAddress`，检查 operator 权限与节点租户归属，使用现有认证任务队列执行。

发现向所选网卡发送 WS-Discovery Probe，默认 IPv4 `239.255.255.250:3702`、TTL 1，三秒内受限重试，最多接收 256 个报文、保留 128 个候选与 256 KiB 数据。校验 SOAP 命名空间、请求关联、UUID、XML 大小/深度；只保留与 UDP 响应源 IP 相同的 HTTP(S) 地址，不解析设备提供的 DNS 名称、不跟随外部地址。重复候选合并；空结果和截断均明确展示。节点本地可用 `IOT_EDGE_DISCOVERY_TARGET` 指定隔离模拟器目标，管理 API 不接受此配置。

WS-Discovery 没有设备认证，响应始终标记 `authenticated:false`。点击候选仅填入连接地址；还须指定现场凭据引用、实际执行上述 TLS/WSSE/Digest 读取，最后显式保存摄像头。发现不会自动建档、写入业务消息或证明设备可信。实现参考 [ONVIF Core 23.12 第 7 节](https://www.onvif.org/specs/2312/ONVIF-Core-Spec-v2312.pdf) 与 [WS-Discovery 2005/04](https://specs.xmlsoap.org/ws/2005/04/discovery/ws-discovery.pdf)，当前为单网卡 IPv4 主动发现子集；IPv6、发现代理、Hello/Bye 常驻缓存和 ONVIF 事件订阅尚未实现，不宣称完整 ONVIF 符合性。GB28181 SIP 注册/目录由下文独立元数据节点提供，视频流继续由外部视频平台提供。

2026-09-10 新增验证：`go test -race ./internal/fieldprotocol -run 'TestONVIF(Discovery|Probe)' -count=1 -v` 的真实 UDP 和回环网卡组播通过；`TestEdgeONVIFMetadataWithAuthentication` 的真实 Chrome 完整发现→候选→错误密码拒绝→认证读取→显式保存通过。组播只在本机回环网卡执行，未扫描现场网络、未验收厂商真机。

2026-09-10 浏览器验收：实际 Chrome→平台→Edge→TLS/Digest/WSSE 摄像头模拟器，读取并保存元数据、缺失凭据拒绝和窄屏通过。分布式调度另外通过两个独立采集进程、真实 PostgreSQL 租约与强制进程终止后的接管测试；尚未进行多物理主机/网络分区压力验收。

## Edge Go 协议与远程版本更新

现场节点可执行已发布的 `go-protocol-v2` TCP/UDP ingress Worker。节点本地显式设置 `IOT_EDGE_ALLOW_GO_WORKERS=true`，并以 `IOT_EDGE_LISTEN_ADDRESSES` 指定允许绑定的 IP；平台不能通过分配配置扩大本地监听范围。可先登记设备并分配监听，也可按下文启用协议自动登记。

节点使用自身凭据下载当前分配版本，平台检查节点/租户/产品绑定，双方验证 SHA-256，节点验证 `GOOS/GOARCH`。单个制品最大 64 MiB，制品与样例共享本地 1 GiB 缓存上限；不足时更新失败并保留当前运行配置，不自动删除历史版本。源码发布现在支持选择额外目标平台，节点按本机系统/CPU 选择不可变制品并实际运行样例；未通过时保留旧运行配置，详见 [异构 Edge 制品](GO_PROTOCOL_PACKAGES.md#异构-edge-制品)。

节点每 5 秒同步配置，下载及校验完成后才更新本地绑定。复用现有 Listener 的完整帧/待应答边界切换，保存帧前会话状态和实际版本。收帧写入持久化补传队列后才发协议应答；该应答证明节点接收，不代表中心已解析。停用设备会在下一次配置同步时阻止后续帧。执行权限与现有 Worker 一致，不是强隔离沙箱。远端命令见下文，节点程序升级与回退见 [启动器说明](EDGE_PROGRAM_UPGRADES.md)；历史单平台版本不自动追加其他平台，须从源码发布新版本。

## Edge 协议自动登记

开启需要两处配置：平台 TCP/UDP 监听实例 `autoRegister:true`，现场节点显式设置 `IOT_EDGE_ALLOW_AUTO_REGISTER=true`，同时保留 `IOT_EDGE_ALLOW_GO_WORKERS=true`。默认关闭现场自动登记；节点不会把平台开关当成本地授权。添加设备向导的高级配置可选择现场节点并打开“自动注册后续设备”，样例测试仍不等于现场设备已登记或已解析。

Worker 从实际报文识别新设备 ID 后，Edge 用自身凭据请求 `POST /api/v1/edge/{tenant}/{node}/devices/register`。请求包括 profileId、deviceId、name 和当前配置摘要，不允许选择租户、产品或凭据。平台核对已分配监听实例、自动登记开关、当前发布 ingress 协议、配置摘要、产品和节点状态；每个节点最多每秒 20 次登记请求。只有登记响应确认相同设备身份后，本地才接受设备并进入持久化队列、协议应答及原始上报链路。

未知设备在平台不可达、认证失败或登记被拒绝时不会收到成功应答；已有登记设备仍按有效配置缓存和 outbox 规则采集补传。平台登记成功但响应丢失时，重试返回已有记录，不重复创建。中心仍严格拒绝未登记设备的 Raw，未将自动登记扩大为任意原文身份写入。

中心监听器与 Edge 平台登记共用仓储原子创建。Memory 使用锁，PostgreSQL 使用配置/产品/节点读锁和唯一插入；失效配置、停用设备、产品归属冲突不能覆盖原记录。新协议设备具有唯一库存接入键和空 Secret，不因此取得标准 HTTP/MQTT 认证能力；重复登记保留已有名称、凭据及状态。节点配置中的设备凭据仍不下发，本地库存使用独立派生键避免多个脱敏设备的空键冲突。

平台信任被授权节点和已发布协议包的设备识别结果；此功能不替代协议本身需要的设备认证，也不声称 TCP 来源或设备 ID 就是设备密码认证。新设备来源记录为 PROTOCOL_AUTO，Raw 保留 CollectorID、监听配置和协议快照供追溯。真实消防设备与现场生产自动登记未验收。

## GB28181 专用视频目录节点

独立元数据服务位于 `protocol-packages/gb28181-metadata`，不作为平台内置厂商 Parser，也不与普通 Edge Agent 共用节点凭据。它使用本机 SIP 密码完成注册认证，以平台节点凭据同步元数据；摄像头目录经操作员显式导入，保留已编辑信息和设备关联。部署配置、容量及错误状态见该目录 `README.md`。它不提供视频流、云台或设备配置操作。

## 现场协议命令

现场节点默认不接收控制命令。部署者须同时启用 `IOT_EDGE_ALLOW_GO_WORKERS=true` 和 `IOT_EDGE_ALLOW_COMMANDS=true`；已有监听地址白名单继续有效。节点心跳只有在本机开启这两个选项时才报告 `PROTOCOL_COMMANDS`。平台仍要求 operator 角色及每次人工确认。

沿用协议命令入口 `POST /api/v2/device-access-profiles/{profile}/devices/{device}/commands`。现场实例必须提供 `requestId`（同一次操作的重试复用该值）、`type`、协议参数和 `confirmed:true`。设备/产品/节点必须启用，实例归属、当前发布协议的 encode 能力、近期心跳均经服务端检查；排队前不存在“已发送”或“执行成功”。命令复用 `device_command` 与设备命令历史，不新建第二套业务命令库。查询入口为 `GET /api/v1/device-registry/{device}/commands/{requestId}`。

- `QUEUED`：已记录，等待节点领取；每个节点最多 8 条活动命令，30 秒领取窗口。
- `DISPATCHING`：数据库已唯一领取；尚不能据此推断实际写出。
- `SENT`：协议没有关联应答，Socket 写出已返回；不证明设备执行成功。
- `ACKNOWLEDGED`：实际 Listener ingress 收到关联应答并将原文保存在节点持久化队列，返回 `rawMessageId`。该原文到达中心前可能暂不可查；协议应答不等于设备业务操作成功。
- `REJECTED`：节点执行前的配置、版本、截止时间或认证刷新检查失败，没有调用命令执行。
- `EXPIRED`：领取前已到期，不下发。
- `UNKNOWN`：节点在领取后中断、发送/等待失败或超过结果窗口，结果未知，不自动重发；仍可接受持有原领取令牌的迟到结果。

节点执行前重新同步认证配置，对照排队时的 Profile 摘要与协议版本，保持已检查配置直到原 Listener 完成 encode/写出/应答。配置同步串行化，使用常驻进程上下文，避免短期命令退出导致采集停止。版本切换仍尊重完整帧和待应答边界。

领取只发生一次，服务端不会把 RUNNING/已领取任务重新分发。节点在实际执行前以 `0600` 原子保存 `command-result.json` 的 UNKNOWN 记录；执行结束再替换实际结果。重启仅补传此结果，不重放命令。补传未确认前不领取下一条命令，平台成功确认 ID 后才删除本地记录。服务端允许一天内补传；过期/失去归属的结果改存本机 `command-result.json.rejected`（最近一条），中心历史保留未知状态。本地节点凭据不写入记录，领取令牌不暴露给浏览器或命令列表。

页面重试保留相同 requestId，另有“开始一条新命令”供用户明确发起新操作。结果未知时应核实设备状态，不以创建新 ID 代替重试。未实现 Modbus 写功能或任意 shell 命令，此入口只调用已发布 Go 协议的 encode 契约。
