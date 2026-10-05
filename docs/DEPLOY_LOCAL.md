# 本地运行与调试

源码调试时，API、Vue 和备份服务在本机运行，虚拟机或本机 Docker 只提供基础环境。命令默认在源码仓库根目录执行；Go、Node 版本以 `go.mod`、`iot_front/package.json` 为准。端口、环境变量与维护操作见 [部署总览](DEPLOYMENT.md)。

## 本地运行

### 首次准备

Windows PowerShell：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\setup-local.ps1
```

Linux / macOS：

```bash
bash ./scripts/setup-local.sh
```

脚本生成 `.env.local`，将基础服务工具账号默认设为 `admin` / `admin123`，独立生成内部密钥与令牌，启动依赖与 Harness，设置云端 Embedding 默认配置，执行 `go mod download` 和 `npm ci`。重复执行复用已有配置与数据，登录与工具连接见[工具连接账号](DEPLOYMENT.md#工具连接账号)。

| 需求 | PowerShell 参数 | Bash 参数 |
| --- | --- | --- |
| 自定义 DeepSeek API 模型（默认无需传入） | `-DeepSeekModel deepseek-flash` | `--deepseek-model deepseek-flash` |
| 只准备依赖，不下载源码依赖 | `-SkipCodeDeps` | `--skip-code-deps` |
| Linux 虚拟机部署全部基础环境，源码在本机运行 | 在 Linux 虚拟机执行右侧命令 | `--dependencies-only` |
| 临时运行容器版备份服务 | `-IncludeBackup` | `--include-backup` |
| 启动运维中心依赖（Prometheus、Loki、Grafana、Alertmanager、采集器） | `-IncludeOps` | `--include-ops` |
| 开启 / 关闭摄像头直播媒体服务（默认开启，省略沿用上次选择） | `-Video on` / `-Video off` | `--video on` / `--video off` |
| 开启 / 关闭随源码 API 运行的容量控制器（默认开启，省略沿用上次选择） | `-Capacity on` / `-Capacity off` | `--capacity on` / `--capacity off` |

对话与推理默认使用 DeepSeek API。启动后在“模型管理”填写 API Key 并保存即可，连接测试可选；也可通过各环境文件的 `DEEPSEEK_API_KEY` 配置。未填密钥不阻止平台启动。知识库使用 PostgreSQL + pgvector，向量计算与检索重排由随平台部署的 `embedding` / `reranker` 服务完成，无需密钥，见[知识库向量服务](DEPLOYMENT.md#知识库向量服务)。完整配置、升级与离线联网边界见 [AI 配置](DEPLOYMENT.md#ai-与工作流)。

依赖部署到虚拟机时使用 `--dependencies-only`，网络与配置共享见 [端口与地址](DEPLOYMENT.md#端口与地址)；Mac 使用 [OrbStack 调试](#orbstack-虚拟机本地调试)。

### 日常运行代码

API、前端和备份服务默认都在源码机运行，在三个独立终端启动；虚拟机仅提供基础环境：

```bash
# 终端一：API
go run ./cmd/iot-platform --env-file .env.local
```

```bash
# 终端二：前端
cd iot_front
npm run dev
```

```bash
# 终端三：备份源码服务
go run ./cmd/backup-service --env-file .env.local
```

访问 **http://localhost:5173**。Vite 默认代理到 API `8081`，可通过 `VITE_API_PROXY_TARGET` 修改；备份服务监听 `8092`。Windows 遇到 npm 执行策略限制时使用 `npm.cmd`。

### IDE 调试

- **GoLand**：工作目录为仓库根目录，运行 `cmd/iot-platform`，程序参数 `--env-file .env.local`。
- **WebStorm**：工作目录为 `iot_front`，运行 npm 的 `dev` 脚本。
- **VS Code**：安装 Go 扩展，在 [launch.json](../.vscode/launch.json) 中选择组合后按 F5 启动：`运行 IoT Platform (API + Web)` 等「运行」组合在终端中执行 `go run`，不经过 Delve，编译为正常优化的程序；「调试」组合可打断点，但 Delve 关闭了编译优化（Go 扩展也不接受在 `buildFlags` 中改 `-gcflags`），进程名为 `__debug_bin…`，容量测试须用「运行」组合或 `go run`。另有带 Backup 和 GB26875 Gateway 的同名组合。

进程环境变量优先于环境文件；IDE 中的旧地址和密码可能覆盖 `.env.local`。`IOT_DEV_MODE` 未设置时按生产模式启动，校验 JWT、管理员口令与服务令牌；只有显式设为 `true` 才跳过这些检查并允许不配置 PostgreSQL，`setup-local` 生成的 `.env.local` 写的是 `false`。macOS 调试需要 Delve 和系统“开发者工具访问”授权，停在 `debugserver` 时先检查授权窗口；服务就绪以 `http://localhost:8081/health/ready` 为准。

### 本地 API 进程交接

终端、IDE 与临时测试 API 不能同时使用相同监听端口和 MQTT 收件箱目录。启动提示 `MQTT inbox directory is used by another process` 或 `8081` 被占用时，先核对实际环境文件、`IOT_DATA_DIR`、`IOT_PROCESS_ROLE` 和 `IOT_INSTANCE_ID`，再定位原进程。macOS / Linux 可执行：

```bash
lsof -nP -iTCP:8081 -sTCP:LISTEN
lsof ./data/mqtt-inbox/combined/inbox.lock
ps -p <已确认的PID> -o pid,ppid,command
```

收件箱路径按实际配置替换；默认目录为 `<IOT_DATA_DIR>/mqtt-inbox/<角色>`，显式实例 ID 会再增加一层实例目录。Windows 可用 `Get-NetTCPConnection -LocalPort 8081 -State Listen` 查看 `OwningProcess`，再用 `Get-Process -Id <PID>` 核对进程。

确认是需要交接的旧实例后，在原终端按 Ctrl+C 或停止 IDE 调试；无原终端时，macOS / Linux 可对该 PID 发送 `kill -TERM <PID>`。等待旧进程退出，再检查端口与锁持有者并启动新实例。锁由操作系统在进程退出时释放；**不要删除 `inbox.lock` 或收件箱数据**，删除锁文件可能让两个进程分别锁住不同文件。临时测试应使用隔离配置、数据目录及空闲端口，并在结束时正常退出；仅更换实例 ID 不能解决端口和其他资源冲突。基础依赖继续沿用，无需为交接重建容器。

### OrbStack 虚拟机本地调试

依赖使用 Ubuntu 虚拟机自己的 Docker Engine。Mac 安装 Go 和符合 `iot_front/package.json` 的 Node.js，使用共享的仓库目录编辑、运行和调试源码。以下命令均在 **Mac 仓库根目录**运行，`develop` 替换为 `orb list` 中的虚拟机名称：

```bash
# 仅首次创建；已有 develop 时跳过，不删除原机器
orb create ubuntu:24.04 develop
orb -m develop sudo bash scripts/setup-local.sh --dependencies-only
go mod download
(cd iot_front && npm ci)
```

也可以进入 `develop` 的共享仓库目录，直接运行 `sudo bash scripts/setup-local.sh --dependencies-only`。脚本识别 OrbStack，并自动把 API 回调设为 `host.orb.internal`；新配置使用 `127.0.0.1` 连接依赖，已有配置保留依赖地址，显式 `--dependency-host 127.0.0.1` 可切回本机转发。Mac 直接使用共享目录中的 `.env.local`，运维配置文件也通过共享目录生效。AI 通过 DeepSeek API 配置。第二套环境须指定独立 `--env-file`，并避免同时占用相同转发端口。

运维规则与通知配置默认在共享的 `data/ops`；可在 `.env.local` 设置 `IOT_LOCAL_OPS_DIR=./data/local-develop/ops` 指定单独目录，脚本同步 Compose 挂载和 API 配置路径。重建虚拟机不会删除 Mac 共享目录：需要干净运维配置时使用新的目录，旧规则和通知文件仍保留。

摄像头直播默认随依赖一起部署；不传 `--video` 沿用上次选择，需要关闭或重新开启时：

```bash
orb -m develop sudo bash scripts/setup-local.sh --dependencies-only --video on
orb -m develop sudo bash scripts/setup-local.sh --dependencies-only --video off
```

切换后重启本机 API 加载配置。开启会生成并保留媒体密钥；关闭仅移除媒体容器，保留摄像头资料与密钥。需要转码时加 `--transcode`，普通虚拟机还需 `--rtc-ip <浏览器可访问的虚拟机IP>`；允许的摄像头网段可用 `--allowed-cidrs` 指定。接入真实 GB28181 设备时，设备须能访问源码机的 SIP 端口 5060 与虚拟机的 RTP 端口范围，并在 `.env.local` 设置 `IOT_GB28181_MEDIA_IP=<设备可访问的虚拟机IP>`（OrbStack 的 localhost 转发只对本机可用）。观看权限在平台内分配。

Mac 使用 OrbStack 自动提供的 `localhost` 端口转发，因此上述命令不依赖虚拟机 IP 或 VPN 对内网 IP 的路由。确保 Mac 和其他虚拟机没有占用相同端口；同时测试两套依赖时先停掉其中一套，避免连接到错误的环境。`host.orb.internal` 是 OrbStack 提供的 Mac 回调地址；`host.docker.internal` 在虚拟机内安装的 Docker 中指向虚拟机，不能用于此处的 Mac API 回调。地址机制参见 [OrbStack 网络文档](https://docs.orbstack.dev/machines/network)。

需要其他源码机直接访问虚拟机时，可把 `--dependency-host` 换成 `orb -m develop hostname -I` 返回的 IPv4 或 `<机器名>.orb.local`，并确保 VPN/路由允许直连。该模式会开放依赖端口；虚拟机 IP 改变后重跑完整命令更新地址，凭据和数据保留。

随后在 Mac 启动 API、Vite 和备份源码服务；VS Code 选择 `IoT Platform (API + Web + Backup)`。命令见 [日常运行代码](#日常运行代码)。IDE 与终端均须选择符合 `iot_front/package.json` 的 Node.js。

查看、停止依赖仍在 Mac 仓库根目录执行，停止不会删除卷：

```bash
orb -m develop sudo docker compose --project-name iot-platform-local --env-file .env.local -f compose.local.yaml --profile ops ps
orb -m develop sudo docker compose --project-name iot-platform-local --env-file .env.local -f compose.local.yaml --profile ops stop
```
