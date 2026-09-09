# Edge 程序升级与回退

独立 `iot-edge-launcher` 管理一个真实 `iot-edge-agent` 子进程。管理员设置目标版本；现场启动器只从本地部署者信任的 HTTPS 签名目录取得当前操作系统和架构的程序。平台接口不接收下载 URL、命令行或 shell 脚本。

## 构建与现场运行

在仓库根目录构建，版本必须与发布目录一致：

```bash
go build -trimpath -ldflags="-s -w -X iot-platform/internal/edgeagent.Version=1.0.0" -o ./data/iot-edge-agent ./cmd/iot-edge-agent
go build -trimpath -ldflags="-s -w" -o ./data/iot-edge-launcher ./cmd/iot-edge-launcher
```

Windows 可将输出命名为 `.exe`。交叉构建以 Go 的 `GOOS`、`GOARCH`、`CGO_ENABLED=0` 指定目标；跨平台编译通过不等于目标现场运行通过。Dockerfile 包含启动器，但默认入口仍为平台 API，不会自动升级已有容器。

按 [Edge 部署说明](EDGE_AND_GATEWAY.md#现场-agent) 准备现场私有 `.env.edge`（节点身份、凭据、Agent 数据目录、本地网络和 Worker 权限）。文件中的相对路径相对于该文件所在目录。启动器使用相同文件启动新旧 Agent，不迁移或删除补传队列。启动器程序目录必须独立于 Agent 数据目录；不要同时手动启动使用同一数据目录的 Agent。

```bash
./data/iot-edge-launcher --env-file .env.edge --bootstrap ./data/iot-edge-agent --program-dir ./data/edge-programs --catalog-policy ./data/edge-program-policy.json
```

配置文件包含真实凭据，应限制到运行账户读取；Unix 使用 `chmod 600 .env.edge`，Windows 使用相应账户文件 ACL。启动器继承通用环境变量，清除继承的 `IOT_EDGE_*` 后由私有文件为子进程提供节点配置。不要把另一套节点环境变量混入启动器终端。远程程序与 Agent/Worker 拥有运行账户权限，不是强隔离沙箱。

不指定 `--catalog-policy` 时可监督本地初始程序，远程目标会明确失败。启动器首次启动/重启需要平台认证和配置同步可达；已有子进程运行时，控制接口的暂时网络错误不会停止它，Agent 自身的配置有效期和凭据拒绝策略继续生效。

## 签名发布

沿用 [可信目录](PROTOCOL_CATALOG.md) 的 Ed25519 密钥生成、签名命令及本地策略格式。策略只由现场部署者配置，包含 HTTPS 目录地址、信任公钥和可选 CA 文件。签名者在目录 payload 中加入程序条目：

```json
{
  "kind": "edge-agent",
  "platform": "linux/arm64",
  "id": "iot-edge-agent",
  "version": "1.0.0",
  "name": "现场 Edge Agent",
  "publisher": "部署维护团队",
  "license": "内部授权",
  "sourceUrl": "https://catalog.example.com/edge/1.0.0/linux-arm64",
  "size": 123456,
  "sha256": "填写实际文件的64位小写SHA256",
  "tags": ["edge"]
}
```

示例 `size` 和 `sha256` 必须替换为实际构建文件值，不能直接签名发布示例。制品为可执行文件本身，最多 32 MiB；使用 `-s -w` 降低体积。支持 Linux、Windows、macOS 的 amd64/arm64 标签，每个版本可以发布多个平台条目。目录有效期最长 24 小时，HTTPS 同源、证书、Ed25519 签名、大小和 SHA-256 校验全部成功后才能切换。禁止重定向。协议源码条目和程序条目共用签名目录，协议管理页只展示/安装源码条目。

## 请求与实际状态

设备接入 → Edge 节点 → 程序升级，可查看实际版本、目标版本、请求版本号、上报时间和失败原因。设置目标需要管理员角色及显式确认。失败后修复现场问题，再次设置同一目标会创建新的尝试请求；清空目标保留当前程序。

| 接口 | 权限与用途 |
|---|---|
| `GET /api/v1/edge-nodes/{node}/program` | viewer，读取当前租户节点目标和实际状态 |
| `POST /api/v1/edge-nodes/{node}/program` | admin，`version`、`expectedGeneration`、`confirmed:true`；过期版本返回 409 |
| `GET /api/v1/edge/{tenant}/{node}/program` | 节点实际凭据认证，获取目标 |
| `POST /api/v1/edge/{tenant}/{node}/program` | 节点实际凭据认证，上报程序状态 |

目标更新使用仓储 CAS；状态上报只修改 status，不覆盖并发管理员目标。PostgreSQL 启动迁移创建 `edge_program` 表。状态中 `generation` 是该次观察对应的请求，不能只看目标版本断言已升级。`RUNNING` 说明最近一次启动器观察到受管进程运行，过期上报不证明当前仍在线。

## 切换与恢复边界

1. 每 15 秒认证获取目标。先下载、验证、写入程序目录；当前进程继续运行。切换前重新获取目标，版本已变或认证失败则不按旧请求切换。
2. 原子保存尝试日志后停止旧子进程，最多等待 5 秒，超时强制终止。启动新程序，最多等待 30 秒。
3. 新 Agent 必须在本次进程中成功获取并应用平台配置，完成实际认证心跳，写入含节点身份、版本、进程 PID 与随机 nonce 的就绪文件。历史缓存、进程创建成功、版本标签本身不能代替该证明。
4. 成功后原子记录当前/上一版本路径、SHA-256 和请求号。就绪失败回到之前的程序；已启用版本异常退出或重启时本地制品哈希不符，也尝试上一个版本。回退程序同样需要认证就绪；无可用上一版本或回退启动失败时，启动器退出并记录失败，不报告成功。
5. 切换未完成时掉电，磁盘日志仍指向原版本；同一失败请求不自动反复执行。中断后需新的请求号重试。当前、上一版和一个候选制品最多约 96 MiB；只清理启动器自己的哈希命名文件，清理失败时阻止继续下载落盘。

就绪证明覆盖配置认证与进程启动，不代表每台现场设备都已采集成功；须继续检查节点心跳、首条 Raw、解析结果和设备状态。旧版与新版须兼容既有本地配置、补传队列和命令日志格式。切换不是无中断热更新，不能保证已在内存中但未持久化的数据不丢失。已领取命令沿用 UNKNOWN 日志与结果补传，不在重启时重复执行。

下载阶段的连接中断、响应中断、HTTP 408/429/5xx 会保留当前运行程序，并按轮询间隔（至少 1 秒）指数退避、最长 5 分钟后重试同一目标，无需重复提交升级请求。下次重试时间和次数写入启动器本地状态文件 `program.json`，重启后继续尊重退避；等待时状态为 STAGING，并显示实际失败原因。每次重试重新获取当前授权目标并重新执行可信目录与制品校验。TLS 证书、签名、摘要、格式错误及 HTTP 401/403 等不会作为临时下载故障自动放行，仍保留旧版本并报告失败；修复后显式提交新请求。候选启动和实际认证就绪失败继续走原回退规则。

当前提供单节点受控升级、版本请求和失败回退。分批发布策略、跨版本数据格式迁移、启动器自身更新和跨主机滚动验收尚未实现；真实硬件及生产升级未验收。

## 验证入口

```bash
go test -race ./internal/httpapi -run '^TestEdgeProgramAuthenticatedProcessUpgradeAndRollback$' -count=1 -v
go test -race ./internal/adapters/memory ./internal/protocolcatalog ./internal/edgeagent
```

首项实际构建并启动两个版本的 Edge 可执行程序，运行真实 HTTP 节点认证与 TLS 签名目录，验证制品篡改、版本不符回退、缓存不能伪造就绪、启动器重启、节点禁用退出和磁盘队列保留；使用内存业务仓储，不等于真实 PostgreSQL 验收。设置 `IOT_TEST_BROWSER` 为 Chrome 可执行路径并先构建前端，可执行其中浏览器子用例；没有设置时明确跳过。数据库共享合约由 `IOT_TEST_POSTGRES_DSN` 下的 `TestDeviceOperationsMigrationAndAtomicity` 在独立临时 schema 验证。
