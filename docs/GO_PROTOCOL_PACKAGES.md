# Go 协议包接入

平台支持直接上传完整 Go 源码：在“设备接入 → Go 源码接入”上传 `.go` 文件或 Go 项目 ZIP，平台自动编译、试跑样例、发布并绑定产品，后续报文立即使用新代码。旧的预编译 Worker 上传方式继续兼容。

## 源码上传（2026-09-05）

1. 下载页面中的完整 Go 模板，修改 `Decode(raw RawMessage) (Message, error)`，保留 `main` 的输入输出入口。模板是独立、可编译的普通 Go 程序；允许自由定义函数、类型、导入标准库和项目内包，不使用解释器语法子集。
2. 单文件直接上传 `.go`。多文件项目把 `go.mod`、源码及项目内包放在 ZIP 根目录；第三方依赖先执行 `go mod vendor`，把 `vendor` 一起上传。默认构建入口为 `.`，也可填写 `cmd/worker` 等项目内目录。
3. 填写协议标识、新版本号、传输方式、报文格式和样例测试。样例可在页面填写，或放在 ZIP 的 `samples/cases.json` 中并清空页面样例框。页面样例优先。
4. 选择已有产品后点击“上传、编译并发布”。平台使用服务器 OS/CPU 编译，全部样例通过后切换该产品的绑定版本；未选产品时只发布，可稍后在“协议与版本”页绑定。选择“仅保存已校验版本”时不改变产品绑定，稍后可发布。
5. 更新代码时更换版本号。语法错误会在页面保留编译日志；样例失败、panic、超时均阻止发布。同一已保存版本不能覆盖。在“协议与版本”页可切换或回滚，历史原始报文保留实际使用版本。

源码入口：`POST /api/v2/protocols/{id}/source-releases`，multipart 字段为 `file`、`version`、`name`、`transport`、`payloadFormat`、`entrypoint`、`cases`、`publish`、`productId`。`publish` 默认 true，`productId` 可选。模板及编译器可用状态：`GET /api/v2/protocol-source-template`。写入需要 operator/admin 权限。

平台自动生成 manifest 和版本制品，并保留原始源码、源文件 SHA-256、Worker SHA-256 及测试数量，无需用户手工打包二进制。源码上限 32 MiB，ZIP 最多 4096 条目、展开不超过 128 MiB；编译超时 120 秒，同一 API 进程同时运行一个上传构建/试跑任务；样例 1–100 条、总预算 60 秒，单例最多 10 秒。运行时仍逐报文启动 Worker，默认最多 5 秒、输出最多 1 MiB。

构建只执行 `go build`，不运行脚本、`go generate` 或 `go test`；关闭在线依赖下载、工作区和自动工具链下载，使用纯 Go（`CGO_ENABLED=0`）。因此需 CGO 的库、项目根目录外的本地 replace、任意 shell 构建流程不在这条上传路径支持范围内。Go 模块目录要求见 [Go 官方模块文档](https://go.dev/ref/mod)。

**部署范围**：本次平台功能升级需要部署一次新的 API 和前端。新的 Dockerfile 在 API 镜像中带入 Go 工具链；宿主机运行时须让 `go` 在 API 的 PATH 中可用。能力部署后，协议上传、版本切换和回滚均无需重启系统。当前仍是具有服务进程操作系统权限的子进程，最小环境变量与超时不是强隔离沙箱；应由可信的协议开发者上传代码。

**接入范围**：源码扩展的是 `RawMessage → StandardMessage` 数据解析。MQTT/HTTP 和现有网关继续负责接收报文；上传源码不会自动开放新的 TCP/UDP 监听端口，也不提供常驻会话、主动连接、粘拆包或通用指令驱动。模板里的 `Decode` 可以返回属性、事件、告警等标准消息；`main` 也可自行实现相同 JSON Lines 契约。

## 旧版预编译 Worker 接口

1. 在“协议开发”中新建协议包，解析器选择 `go_protocol_parser`，保存为草稿。
2. 在“上传 Go 协议包”处上传当前部署平台对应操作系统和 CPU 架构的可执行文件。也可以调用：

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
