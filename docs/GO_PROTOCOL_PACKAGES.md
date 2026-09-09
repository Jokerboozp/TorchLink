# Go 协议包接入

## 推荐：只编写 Go 函数

在“设备接入 → 源码接入”下载“解析模板”或“TCP / UDP 模板”。解压后只修改 `protocol.go`，然后直接上传这个 `.go` 文件，或者将整个项目打成 ZIP 上传。ZIP 可以带一层项目目录。模板包含 Go 模块、平台适配代码和本地样例预检。在项目目录运行 `go test ./...` 会实际编译并执行 Samples/Operations，错误结果会导致测试失败；不需要编写 `protocol.json`、样例 JSON、stdin/stdout 或 `main`。

最小业务代码如下（`Definition`、`Message` 等类型由模板中的 `zz_platform.go` 提供；单文件上传时平台自动补齐）：

```go
package main

import "fmt"

func Protocol() Definition {
    return Definition{
        Decode: decode,
        Samples: []Sample{{
            Name: "温度上报",
            Data: []byte{0xAA, 0x01, 0x2A},
            Want: properties(map[string]any{"temperature": 42}),
        }},
    }
}

func decode(data []byte, ctx Context) (Message, error) {
    if len(data) != 3 || data[0] != 0xAA || data[1] != 0x01 {
        return Message{}, fmt.Errorf("报文须为 AA 01 加一个温度字节")
    }
    return properties(map[string]any{"temperature": int(data[2])}), nil
}
```

这里的 `AA 01 2A` 是教学报文，需替换为厂家真实报文。`Samples` 中独立填写输入和期望值，不能用 `Decode` 的返回结果生成期望值。平台实际执行解析后核对消息类型、属性，以及填写的事件、标签、时间；错误、缺失样例或编译失败均阻止发布，保留现有产品版本。上传不会自动执行任意 `_test.go` 或构建脚本。模板中的 `zz_platform_test.go` 仅用于你主动执行的本地预检，每次协议调用限制 5 秒和 1 MiB 输出；上传仍由平台独立执行完整校验，不能以本地预检代替发布校验。

上传页填写协议标识，可选择绑定产品；版本留空时平台生成唯一时间版本，也可在页面或 `Definition.Version` 中设置。`Definition.Name`、`Transport` 可选；无 `Ingress` 默认 MQTT，有 `Ingress` 默认 TCP。字节函数模式使用 HEX 作为平台内部报文封装，业务函数收到的是解码后的 `[]byte`。旧格式参数放在“高级设置与旧协议包兼容”中；Go 函数模式请清空运行时、能力和编译入口，平台根据实际注册函数生成配置。

| Go 字段 / 函数 | 用途 |
| --- | --- |
| `Decode func([]byte, Context) (Message, error)` | 完整帧转属性、告警、事件等；必填 |
| `Ingress func([]byte, Context) (Frame, error)` | TCP/UDP 分帧、校验、设备识别和应答；可选 |
| `Encode func(Command, Context) (Frame, error)` | 命令转下行字节；可选 |
| `Samples []Sample` | Go 编写的输入、期望消息和可选上下文 |
| `Operations []OperationSample` | Go 编写的分帧/下行样例；每项注册能力必须覆盖完整成功结果 |

完整接入模板已包含上述三种函数和实际试跑样例。半帧返回 `Frame{NeedMore:true}`，完整帧返回 `Consumed` 字节数和从报文识别的 `DeviceID`，应答使用 `Reply: []byte{...}`；错误返回 Go `error`。设备注册和认证策略仍由原接入链路执行，协议自身的校验/认证须在函数中真实完成，不能仅识别一个 ID 就假定设备通过认证。下行通过 `CorrelationID` 与设备应答关联。`Context.State`/`Frame.State` 用于传递会话状态，进程不是常驻服务；状态数值经序列化后按 `float64` 读取，模板有示例。

选择绑定产品并发布后，新报文使用新版本；TCP/UDP 仍需配置监听实例，设备需要连接到实际端口。这项简化不猜测设备地址、凭据、监听端口或厂家协议含义。

以下章节保留已有完整协议包和底层调用契约，使用 Go 函数模板时无需手工编写这些配置。


平台支持直接上传完整 Go 源码：在“设备接入 → Go 源码接入”上传 `.go` 文件或 Go 项目 ZIP，平台自动编译、试跑样例、发布并绑定产品，后续报文立即使用新代码。Go 源码就是自定义协议的唯一页面上传入口，支持单文件和多文件源码 ZIP；编译制品由平台自动生成。

## 源码上传

1. 下载页面中的完整 Go 模板，修改 `Decode(raw RawMessage) (Message, error)`，保留 `main` 的输入输出入口。模板是独立、可编译的普通 Go 程序；允许自由定义函数、类型、导入标准库和项目内包，不使用解释器语法子集。
2. 单文件直接上传 `.go`。多文件项目把 `go.mod`、源码及项目内包放在 ZIP 根目录；第三方依赖先执行 `go mod vendor`，把 `vendor` 一起上传。默认构建入口为 `.`，也可填写 `cmd/worker` 等项目内目录。
3. 填写协议标识；新版本号、传输方式、报文格式等可在项目根目录的 `protocol.json` 中维护，页面未填写时自动读取。样例可在页面填写，或放在 ZIP 的 `samples/cases.json` 中并清空页面样例框。页面样例优先。
4. 选择已有产品后点击“上传、编译并发布”。平台始终构建服务器 OS/CPU，可同时选择现场节点平台；发布端全部样例通过后切换该产品的绑定版本；未选产品时只发布，可稍后在“协议与版本”页绑定。选择“仅保存已校验版本”时不改变产品绑定，稍后可发布。
5. 更新代码时更换版本号。语法错误会在页面保留编译日志；样例失败、panic、超时均阻止发布。同一已保存版本不能覆盖。在“协议与版本”页可切换或回滚，历史原始报文保留实际使用版本。

源码入口：`POST /api/v2/protocols/{id}/source-releases`，multipart 字段为 `file`、`version`、`name`、`transport`、`payloadFormat`、`entrypoint`、`runtime`、`capabilities`、`targetPlatforms`（均为 JSON 数组）、`cases`、`publish`、`productId`。`publish` 默认 true，`productId` 可选。模板及编译器可用状态：`GET /api/v2/protocol-source-template`。写入需要 operator/admin 权限。

平台自动生成 manifest 和版本制品，并保留原始源码、源文件 SHA-256、Worker SHA-256 及测试数量，无需用户手工打包二进制。源码上限 32 MiB，ZIP 最多 4096 条目、展开不超过 128 MiB；每个平台编译超时 120 秒，同一 API 进程同时运行一个上传构建/试跑任务；样例 1–100 条、总预算 60 秒，单例最多 10 秒。运行时仍逐报文启动 Worker，默认最多 5 秒、输出最多 1 MiB。

构建只执行 `go build`，不运行脚本、`go generate` 或 `go test`；关闭在线依赖下载、工作区和自动工具链下载，使用纯 Go（`CGO_ENABLED=0`）。因此需 CGO 的库、项目根目录外的本地 replace、任意 shell 构建流程不在这条上传路径支持范围内。Go 模块目录要求见 [Go 官方模块文档](https://go.dev/ref/mod)。

### 异构 Edge 制品

“现场节点平台”可选 Linux、Windows、macOS 的 amd64/arm64，共六种组合，平台代码为 `linux-amd64` 等。也可在 `protocol.json` 设置 `"targetPlatforms":["linux-amd64","linux-arm64"]`；页面或 multipart 明确指定时覆盖该数组，留空沿用包内值。无配置时只构建发布端，发布端平台始终包含且必须实际通过样例。沿用 CGO 关闭、vendor、禁止在线下载/构建脚本、最小环境和构建并发限制。任何所选目标编译失败都会拒绝整个新版本，并保留已有版本及产品绑定。

不可变包为每个平台保存独立 Worker 路径、SHA-256、大小及统一样例包哈希。单 Worker 至多 64 MiB、全部 Worker 至多 128 MiB，最终 ZIP 仍至多 64 MiB；超限须减少目标或源码依赖。原始上传字节保留，源码/制品下载照常验证。历史版本不原地补入新平台，新增目标须使用新版本号。

发布端制品标记 `PASSED` 并记录实跑数量；其他源码目标为 `COMPILED`，预编译包中的其他目标为 `UNTESTED`，均不宣称已在目标系统执行。版本页分别展示这些状态。现场 Agent 按本机平台选择制品，经真实节点认证下载并校验哈希，再复用发布端同一套 decode/ingress/encode 样例校验。样例通过后才应用配置和切换 Listener；样例不是网络设备操作，不进入 Raw 业务链路。失败保留旧配置并在节点心跳显示实际错误，不能用“编译成功”替代目标平台试跑。

节点的制品与样例缓存共同计入 1 GiB 上限；进程重启重新跑本地已校验样例，不依赖再次下载，配置缓存仍受原有认证及 24 小时上限约束。历史单平台版本沿用原发布校验记录，兼容早期 `GOOS/GOARCH` 表示法；它们没有自动变成跨平台版本。多平台运行仍是有节点账户权限的 Worker 子进程，并非强隔离沙箱。中心解析/回放使用原有发布端制品；本功能针对异构 Edge，不自动重建异构 API/Gateway 的共享运行环境。

验证入口：`go test -race ./internal/protocolbuild -run '^TestCrossPlatformCompilerProducesActualTargets$' -count=1 -v` 检查六平台实际编译结果的 ELF/PE/Mach-O 与 CPU 类型。`TestCrossPlatformSourceEdgeActualExecution` 默认运行真实上传、编译、发布端样例、同机 Edge TCP/补传/解析和缓存重启；显式设置测试进程 runner 与共享目录才运行 Linux ARM64 子进程，配置 Chrome 才运行浏览器。缺失环境的子用例明确跳过，不能当作已执行。实际环境与结果见进度文档。

**部署范围**：本次平台功能升级需要部署一次新的 API 和前端。新的 Dockerfile 在 API 镜像中带入 Go 工具链；宿主机运行时须让 `go` 在 API 的 PATH 中可用。能力部署后，协议上传、版本切换和回滚均无需重启系统。当前仍是具有服务进程操作系统权限的子进程，最小环境变量与超时不是强隔离沙箱；应由可信的协议开发者上传代码。

**接入范围**：`go-json-lines-v1` 扩展 `RawMessage → StandardMessage` 解析；`go-protocol-v2` 另支持 TCP/UDP 入站分帧、设备识别、会话状态、应答和在线下行。平台提供通用监听器，协议包不需要依赖平台源码或内置解析器。通用主动拨号、定时轮询和连接事件回调尚不属于该接口。

## 完整协议包（go-protocol-v2）

以 `protocol-packages/gb26875-dahua` 为独立 module 示例，可复制到任何 Git 仓库。目录结构：

```text
go.mod
protocol.json
main.go
gb26875/...
samples/cases.json
samples/operations.json
```

`protocol.json` 示例：

```json
{"id":"gb26875-dahua","name":"GB26875 大华消防终端","version":"1.0.0","runtime":"go-protocol-v2","transport":"TCP_UDP","payloadFormat":"hex","capabilities":["decode","ingress","encode"],"entrypoint":"."}
```

上传时表单非空值覆盖元数据；`id` 必须与 URL 协议标识一致。`TCP` / `UDP` 只支持对应网络，`TCP_UDP` 支持两种监听实例。每个实例固定一个租户和产品；同一网络端口只允许一个启用实例。新增版本须继续支持产品上所有已启用实例的网络。

Worker 每次启动接收一个 JSON 请求，向 stdout 输出一个 JSON 结果并退出；日志写 stderr。`version` 固定为 2，操作如下：

| operation | 输入 | 输出 |
|---|---|---|
| `decode` | `raw`：完整 RawMessage；`state`：归档的帧前状态；`now`：receivedAt | `standardMessage`：标准消息 |
| `ingress` | `data`：当前缓冲区 HEX；`state`：前次状态；`now`：当前 Unix 毫秒 | `consumed`：本次完整帧字节数；`deviceId`；可选 `deviceName`、`reply`、`state`、`correlationId` |
| `encode` | `command`：包含 `type` 的协议自定义对象；`state`；`now` | `reply`：下行 HEX；可选 `state`、`correlationId` |

帧尚未完整时仅返回 `{"needMore":true}`，不得消耗数据、登记设备或发送应答。完整帧必须从缓冲起点开始，`consumed` 为正且不超过输入字节数。TCP 会继续处理剩余缓冲；UDP 一个数据报必须恰好是一帧，不能返回 needMore。错误返回 `{"error":"原因"}`：TCP 断开连接，UDP 丢弃该报文，不发送应答。

`deviceId` 最长 128 字节，无空白、控制字符或路径分隔符；同一会话不能更换设备 ID。平台只接受该租户、该产品下启用的设备；实例开启 autoRegister 时才创建未知设备。协议的状态须为可序列化 JSON，最多 64 KiB；入站缓冲、完整帧及应答最多 64 KiB（UDP 下行还受 65507 字节限制）。TCP 半帧最多等待 30 秒，连接空闲两分钟关闭。每实例最多 128 会话、每进程最多 32 个监听操作并发；解析子进程仍按单次调用启动。

平台将帧前状态写入 `raw.metadata.protocolState` 并在 decode 请求的 state 中传递，历史回放同样读取这份快照，不依赖仍在线的连接。完整帧先进入正常原始消息归档及事件投递，成功后才发送 `reply`。这表示平台已接收原文，不表示 Kafka 下游异步解析和告警已经完成。`decode` 应对 ingress 接收的每种完整帧都返回有效标准消息，包括 ACK。协议必须避免对 ACK 再 ACK。

`encode` 返回 correlationId 时，平台等待后续 ingress 的相同 ID；无 ID 时只返回 `sent`，有匹配成功入站则返回 `acknowledged`。命令超时不自动重发；重试与幂等策略由协议定义。半帧和待应答命令保留旧版本，版本切换清空状态；新版本可从下一次设备上报重建状态。Worker 的单次执行最长仍受 5 秒默认值约束（配置最大 10 秒），命令等待按实例 timeoutMs，最多 30 秒。

## 操作样例与监听 API

`decode` 仍使用 `samples/cases.json`，每条含 input、expectedMessageType，可选 expectedProperties。声明 ingress/encode 的包必须附 `samples/operations.json`，每项能力至少有一个完整成功样例。平台实际执行全部操作，expected 按字段递归匹配；半帧样例不算完整 ingress 覆盖。例如：

```json
[{"name":"半帧","request":{"operation":"ingress","data":"4040","now":1787553015000},"expected":{"needMore":true,"consumed":0}}]
```

上例只展示格式，不能单独用于完整包发布；完整成功/应答/下行样例参见 GB 包。失败样例、编译错误、panic 和超时均阻止该版本保存和绑定。

```http
POST /api/v2/device-access-profiles
Content-Type: application/json

{"id":"dahua-tcp","mode":"listener","network":"tcp","host":"0.0.0.0","port":26875,"productId":"fire-terminal","protocolId":"gb26875-dahua","protocolVersion":"1.0.0","timeoutMs":5000,"enabled":true,"autoRegister":true}
```

UDP 使用另一个 id 及 `network:"udp"`。通过 `PUT /api/v2/device-access-profiles/{id}` 更新或停用，约一秒生效。产品需预先绑定该协议版本。`GET /api/v2/device-access-profiles` 返回当前绑定版本与本进程监听状态；该接口不会把旧绑定版本写回实例。

```http
POST /api/v2/device-access-profiles/dahua-tcp/devices/gb26875_123456789012/commands
Content-Type: application/json

{"type":"time-sync"}
```

监听管理、命令及下载需要 operator/admin 权限。下载 API：

- `GET /api/v2/protocols/{id}/releases/{version}/source`：精确返回当时上传的 .go 或 ZIP；仅源码构建版本支持。
- `GET /api/v2/protocols/{id}/releases/{version}/package`：含当前服务器架构的 Worker、manifest、样例与原始源码的 ZIP。跨 OS/CPU 迁移应重新上传源码编译。

两种下载均检查租户归属和 SHA-256，可用于把平台版本归档回外部协议仓库。平台没有自动拉取远程仓库；可由外部 CI 调用源码上传 API。

## 历史兼容 API（不提供页面入口）

以下仅记录旧集成的二进制上传 API；新接入统一使用上方 Go 源码入口，页面不再提供单独的“上传自定义协议包”。

1. 旧集成使用已有的 `go_protocol_parser` 协议草稿。
2. 上传匹配部署操作系统和 CPU 架构的可执行文件：

   ```http
   POST /api/v1/protocol-packages/{id}/artifact
   Content-Type: multipart/form-data
   field: artifact
   ```

3. 平台把文件保存到 `IOT_DATA_DIR/protocol-packages/{tenant}/{package}/{version}/`，记录 SHA-256，不接受客户端直接提交的绝对路径。
4. 先用“解析调试”运行样本，再把协议包发布并绑定到产品。运行时每条原始报文都会启动受限时长的独立 worker 进程；修改文件、超时、输出过大或返回非法标准消息都会导致本次解析失败，不会拖死 API 进程。

此旧接口接收二进制，源码请使用上方 V2 源码入口。执行器限制路径、文件大小（64 MiB）、单次运行时间（默认 5 秒，最多 10 秒）、标准输出大小（1 MiB），并且不向 Worker 传递数据库密码、JWT 密钥等应用环境变量。

## Worker 契约

worker 从标准输入读取一行 JSON，内容就是平台的 `RawMessage`：

```json
{
  "messageId": "raw_1",
  "tenantId": "tenant_001",
  "productId": "product_fire",
  "deviceId": "device_1",
  "protocol": "vendor-v2",
  "transport": "TCP",
  "payloadFormat": "hex",
  "payload": "AA 01 2A",
  "receivedAt": 1710000000000
}
```

worker 输出一行标准消息（也可以包在 `standardMessage` 字段中）：

```json
{
  "messageType": "PROPERTY_REPORT",
  "timestamp": 1710000000000,
  "properties": {"temperature": 42},
  "event": {},
  "tags": {"vendor": "example"}
}
```

`messageType` 必须是平台支持的标准消息类型。租户、产品和设备 ID 由平台原始报文覆盖，worker 不能借此把数据写入其他租户。发生业务解析错误时输出 `{"error":"..."}` 并以非零状态退出。

示例 worker 位于 `examples/go-protocol-worker`，可以直接编译：

```powershell
go build -trimpath -ldflags="-s -w" -o protocol-worker.exe ./examples/go-protocol-worker
```

Docker Linux 部署要编译 Linux 二进制，例如：

```powershell
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -trimpath -ldflags="-s -w" -o protocol-worker ./examples/go-protocol-worker
```

## 版本与回滚

每个协议包版本独立保存 artifact 和摘要。不要覆盖已经发布版本；上传新版本后先用样本验证，再把产品切换到新版本。旧版本文件保留在数据卷中，可通过重新绑定产品回滚。真实设备联调仍需验证设备端重发、网络隔离、CPU/内存限制和请求/应答超时，代码测试不能替代现场验收。

通用监听会话属于当前 API 进程；Compose 的单 API 实例可直接使用。仓库 K8s 多副本示例未增加 TCP/UDP 服务暴露和跨副本命令路由；使用它时需让设备接入与命令请求到同一个持有连接的实例，不能把多副本 HTTP 负载均衡视为自动完成了会话路由。

## 可信远程目录

设备接入页新增“协议目录”：从部署管理员配置的 HTTPS 签名目录安装 Go 源码，复用本文件所述源码编译、样例试跑、不可变版本和发布回滚流程。安装仅产生校验版本，管理员明确确认代码执行后才进行。配置、签名发布工具、认证与错误语义见 `PROTOCOL_CATALOG.md`。

### 部件级火警、故障与恢复

一台控制器上报多个部件时，使用 `event.components` 明确每个部件的稳定 ID、名称、位置、发生时间及各告警类型的布尔状态。平台按部件和类型独立生成与恢复告警，未上报项不变化。完整 Go 示例、乱序策略及旧版本迁移边界见 [部件告警与 MQTT 持久接收](DEVICE_RECEIVE_RELIABILITY.md)。无需维护额外 JSON 配置文件。
