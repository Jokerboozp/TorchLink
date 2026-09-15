# 离线部署

离线部署分两步：有网机器一键打包，目标机器一键导入并启动。目标机器不需要 Go、Node.js 或源码依赖；打包机需预装并启动 Docker Engine / Docker Desktop（Linux 容器）及 Docker Compose 2.24.4+；Linux 目标机缺少 Docker 时会自动从离线包安装。Windows/macOS 目标机仍需预装 Docker Desktop。Linux/macOS 的部署健康检查还需要 curl。

打包机与目标机应使用相同 CPU 架构（例如均为 linux/amd64）；Apple Silicon 默认生成的 ARM64 镜像不能直接作为 x86 服务器离线包。CentOS 7.9 x86_64 使用 linux/amd64 包；新包默认携带 Linux Docker、Compose 和 Buildx 的安装文件与 SHA-256 校验值，首次安装请用 root 或 sudo 执行。

打包机操作系统与目标系统可以不同：CentOS/Linux、Windows 或 macOS 都可以通过 Docker 准备 openEuler 离线包。`--target-os` / `-TargetOS` 指定的是目标部署系统，打包机无需安装为 openEuler。

## 1. 有网机器打包

在仓库根目录执行：

```powershell
# Windows
powershell -ExecutionPolicy Bypass -File .\scripts\package-offline.ps1
```

```bash
# CentOS / 其他 Linux
bash ./scripts/package-offline-linux.sh
# macOS
bash ./scripts/package-offline.sh
```

默认打包平台、存储、消息、备份、监控、Ollama、Weaviate、AI 工作流 Harness、`qwen3:1.7b` 对话模型，以及知识库必需的 `nomic-embed-text` 嵌入模型。无须预先创建 `.env`；脚本先生成独立的 `.env.offline`，平台管理员默认 `admin` / `admin123`，其他服务凭据随机生成，再构建镜像。告警研判和 AI 工作流默认共用 `qwen3:1.7b`，整个运行过程不访问外网。

生成目录：`offline-bundles/iot-platform-offline-时间戳/`。将整个目录复制到目标机器，包括隐藏文件 `.env.offline`。镜像、模型文件、配置和部署脚本必须一起传输。

常用选项：

| 用途 | PowerShell | Bash |
| --- | --- | --- |
| 使用已有配置 | `-EnvFile .\.env.production` | `--env-file ./.env.production` |
| 选择对话模型（默认 `qwen3:1.7b`） | `-OllamaModel qwen3:4b` | `--ollama-model qwen3:4b` |
| 跳过模型归档（目标机已有模型时） | `-SkipOllamaModel` | `--skip-ollama-model` |
| 目标为 openEuler 24.03 LTS-SP4 | `-TargetOS openeuler-24.03-lts-sp4` | `--target-os openeuler-24.03-lts-sp4` |
| 输出父目录 | `-OutputDir D:\offline-bundles` | `--output-dir /data/offline-bundles` |

已有配置会保留业务地址和模型设置；如果配置已启用 Ollama，会自动携带实际配置的对话模型（`IOT_AI_MODEL` 优先于 `IOT_OLLAMA_MODEL`），并让 Harness 使用同一模型。使用 `-EnvFile` 时仍需确保内网地址和所选组件匹配。示例密码和空的必需密钥会被拒绝。

`-SkipOllamaModel` / `--skip-ollama-model` 仅适用于目标机的 `iot-platform_ollama-data` 卷已经包含所需模型；部署默认会检查模型是否存在。当前知识库使用 `nomic-embed-text`，替换嵌入模型需同时考虑向量维度与重新索引；更换对话模型前评估目标环境内存与响应时间。

打包时模型缓存保存在 `iot-platform-offline-build_ollama-data` 卷，完成后停止打包用 Ollama；不操作已有 `iot-platform` 部署。模型归档仅包含模型文件，不包含 Ollama 身份密钥。重复打包可复用缓存；曾下载的其他模型也可能保留在归档中。

## openEuler 24.03 LTS-SP4 专用离线包

对于启用 SELinux 的 openEuler，使用专用目标选项。只携带 Docker 静态二进制不足以构成完整系统依赖；缺少 `container-selinux` 或自定义安装路径标签时，Docker 即使可以响应 `docker info`，仍可能在导入镜像时发生 `failed to mknod(...): permission denied`。

### CentOS / Linux 打包，openEuler 部署

在有网的 CentOS 虚拟机或其他 Linux 打包机的**项目源码根目录**执行。Docker Engine 必须能构建、运行 Linux 容器，Compose 至少为2.24.4；不能仅凭脚本参数可识别就认定当前主机已完成打包验证。

使用仓库 `main` 分支时先更新代码，再打包：

```bash
git pull --ff-only origin main
bash ./scripts/package-offline-linux.sh --target-os openeuler-24.03-lts-sp4
```

Linux 包装脚本会将参数原样传递给 `package-offline.sh`，下面的命令等效，也适用于已启动 Docker Desktop 的 macOS 打包机：

```bash
bash ./scripts/package-offline.sh --target-os openeuler-24.03-lts-sp4
```

已有平台部署需要沿用原凭据时，打包命令追加 `--env-file /实际路径/.env.offline`。使用当前代码重新打包，旧离线包不会自动增加系统依赖。

### Windows 打包，openEuler 部署

在有网且已启动 Docker Desktop（Linux 容器）的 Windows 打包机项目根目录执行：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\package-offline.ps1 -TargetOS openeuler-24.03-lts-sp4
```

无论打包机是 CentOS、Windows 还是 macOS，系统依赖准备都在临时 `openeuler/openeuler:24.03-lts-sp4` 容器内执行，不把 openEuler RPM 安装到打包机宿主系统。该容器会下载容器策略、SELinux 管理工具、iptables、xz、procps、curl 及完整 RPM 依赖，并用空安装根目录执行离线事务测试。依赖包附有 SHA256 和 OS/版本/架构信息，写入 `docker-runtime/packages`；下载或依赖检查失败不会输出打包完成。构建容器与目标 CPU 架构一致，仍不支持用 ARM 应用镜像包部署 x86 服务器。

### 将完整离线包复制到 openEuler

等待出现“离线包已生成”提示，将整个生成目录（包括隐藏文件 `.env.offline`、`images.tar` 和 `docker-runtime/`）复制到 openEuler 服务器，然后在**离线包根目录**执行：

```bash
sudo bash ./scripts/deploy-offline-linux.sh
```

首次安装或修复本项目安装的 Docker 时，部署脚本会：

1. 检查 SELinux 策略和管理工具，缺少时校验并从包内安装 RPM，禁用所有 DNF 软件源。
2. 为 `/usr/local/lib/iot-docker` 设置标准程序目录的持久标签映射，恢复程序、数据及运行目录标签。
3. 对需要修复的受管 Docker 重启服务，确认进程进入 `container_runtime_t` 后才继续。已有容器会受该次重启影响；正常的受管 Docker 重复部署不会因此重启。

脚本不会关闭 SELinux、删除 Docker 数据或更换存储驱动。现有外部 Docker 安装及远程 Docker 上下文不自动修复；自定义 Docker 数据目录需要单独配置策略。修复过的运行环境须继续通过实际镜像导入和服务健康检查，不能以标签检查替代整个部署验收。

`generic` 仍为默认打包目标；专用目标不能与 `SkipDockerRuntime` / `--skip-docker-runtime` 或手工 `DockerPackagesDir` 同时使用。其他 SELinux 发行版需提供本发行版对应的容器策略及依赖包，不能混用 openEuler RPM。

2026-09-15 验证：脚本回归模拟已运行但处于 `init_t` 的 Docker，覆盖缺少策略、包损坏、OS 不匹配、标签和进程修复、幂等及远程上下文；系统调用使用模拟实现。本机没有 Docker Engine，尚未实际下载/构建专用包，也未在 openEuler SELinux 内核完成镜像导入验证。

2026-09-15 补充修复：用户在 CentOS 上实际拉取的 `openeuler/openeuler:24.03-lts-sp4` 返回 `ID="openEuler"`、`VERSION_ID="24.03"`、`VERSION="24.03 (LTS-SP4)"`，旧脚本因 ID 大小写拒绝。`bash scripts/tests/openeuler-release-smoke.sh` 使用该输出作为样本执行实际准备脚本，验证通过版本检查后才调用 DNF，并检查目标服务器身份规范化；DNF 被测试替身拦截，未据此宣称完整 RPM 下载、打包或实机部署通过。

## 2. 目标机器一键部署

进入复制后的离线包目录：

```powershell
# Windows
powershell -ExecutionPolicy Bypass -File .\scripts\deploy-offline.ps1
```

```bash
# Linux
sudo bash ./scripts/deploy-offline-linux.sh
# macOS 已启动 Docker Desktop 时
bash ./scripts/deploy-offline-macos.sh
```

脚本依次校验镜像和模型的 SHA-256、检查 Compose 配置、导入镜像、检查所需镜像、恢复模型、启动服务并检查平台及模型。启动固定使用 `--no-build --pull never`；模型恢复容器也禁止拉取镜像。

默认 Web 地址是 `http://服务器IP:8080`。管理员凭据见 `OFFLINE-CREDENTIALS.txt`；使用外部配置打包时，凭据仍以该配置为准。若需改端口，请先编辑包内 `.env.offline` 的 `IOT_WEB_PORT` 和 `IOT_API_PORT`。整个离线包包含密码，请限制访问和传输范围。

同一个包可以重复执行部署命令：配置与数据卷保持原值，模型恢复只补齐缺失文件。模型解压会临时额外占用一份模型大小的磁盘空间。不要用重新生成随机凭据的新包直接替换已有数据库部署；升级时应沿用原来的 `.env.offline` 作为打包配置。

离线部署项目名固定为 `iot-platform`，本地运行和在线部署使用各自的项目名；同一台机器运行多套系统时仍需调整重叠的宿主机端口。

## 查看状态与排错

在离线包目录执行（Windows / Linux / macOS 通用）：

```text
docker compose --project-name iot-platform --env-file .env.offline -f compose.yaml -f compose.offline.yaml ps
docker compose --project-name iot-platform --env-file .env.offline -f compose.yaml -f compose.offline.yaml logs --tail=100 platform-api
```

- 依赖准备报 `Unexpected package preparation OS`：旧检查把官方镜像的 `ID="openEuler"` 误判为不支持。更新源码后重试；当前检查统一发行版 ID 大小写，并兼容 `24.03 (LTS SP4)` / `24.03 (LTS-SP4)` 两种显示形式，仍拒绝其他发行版或 SP 版本。包内目标信息与服务器检查使用相同规范形式。无需重装 CentOS 或修改镜像的 `/etc/os-release`。
- `--target-os` 提示未知参数：打包机源码需要包含提交 `271d279d` 或更新版本；在源码根目录执行 `bash ./scripts/package-offline.sh --help` 核对参数。不要在旧离线包目录尝试打包。
- 缺少镜像或 SHA-256 不匹配：在有网机器重新打包并完整复制，不要在离线目标机执行拉取。
- 缺少 `nomic-embed-text` 或对话模型：重新携带模型打包，再部署到原目录/配置；无需删除已有模型卷。
- 需要自行诊断：可显式使用 `-SkipHealthCheck` / `--skip-health-check`；只在确认传输完整性后使用 `-SkipHashCheck` / `--skip-hash-check`。跳过检查不代表部署验收通过。
- 已有业务数据迁移：使用项目备份及数据库、对象存储恢复流程；离线安装包只包含程序、配置和模型，不包含业务数据。

GB26875 等协议统一上传 Go 源码包，并在平台启用通用 TCP/UDP 监听实例；新离线包不再打包专用 GB 网关。默认映射 26875，可用 IOT_PROTOCOL_PORTS 预留同号端口范围。

## Docker 自动安装

Ubuntu、CentOS 等使用 systemd 的 Linux 在线部署与离线部署共用检测逻辑：已有可用 Docker 和 Compose 2.24.4+ 时复用；本项目安装的本机 Docker 还会检查 SELinux 策略和进程标签，确需修复时重启；仅缺 Compose 时只补插件；Docker 服务未启动时尝试启动。在线构建还会检测 Buildx。脚本不删除数据卷、不更改现有 daemon.json，也不会自动升级或降级已有 Docker Engine。

- 在线：从 Docker / docker GitHub 官方地址下载缺失文件，安装后继续部署。
- 离线：仅读取包内 `docker-runtime/`，校验架构和 SHA-256 后安装，不会访问下载地址或软件源。旧包若没有安装文件且目标机缺少 Docker，需在有网机器重新打包。
- 首次安装使用 Linux 静态二进制和 systemd；amd64 / arm64 均支持。3.x / 4.x 内核选择 Docker 24.0.9 兼容分支，其余选择 28.5.2；Compose 2.27.3，Buildx 0.14.1。旧内核兼容分支不代表 CentOS 7 仍受 Docker 官方维护。静态安装方式见 [Docker 官方说明](https://docs.docker.com/engine/install/binaries/)。
- Linux 系统需已有 systemd、tar、iptables、xz 和 ps；健康检查需 curl。在线安装会通过系统包管理器补充 iptables/xz/procps，CentOS 7 使用临时 Vault 源，不覆盖已有 yum 配置。
- 精简离线系统若缺少这些基础包，打包时使用 `--docker-packages-dir /path/to/packages` 或 `-DockerPackagesDir C:\\packages` 加入与目标发行版、版本和架构匹配的 RPM/DEB 及全部依赖。RPM 优先使用禁用所有软件源的 DNF 安装；无 DNF 时使用 rpm，DEB 使用 dpkg。部署不联网解决依赖，也不跳过依赖检查。
- 已有 Docker 的服务器可通过打包参数 `--skip-docker-runtime` / `-SkipDockerRuntime` 减小包体。该参数不适用于尚未安装 Docker 的目标机。

Linux 首次部署示例：

```bash
# 在线：在仓库根目录执行
sudo bash ./scripts/deploy-online.sh
# 离线：在新离线包根目录执行
sudo bash ./scripts/deploy-offline-linux.sh
```

Windows/macOS 的 Docker Desktop 安装与虚拟化设置不在此 Linux 自动安装流程内；PowerShell 打包脚本仍会准备 Linux 安装文件，方便在 Windows 打包后拷贝到 CentOS。

### Ubuntu 部署

Ubuntu 使用相同的一键命令，无需切换脚本或另外准备 Docker 软件源。在线缺少基础依赖时，使用 APT 无人值守安装 iptables、xz-utils、procps、curl 和 ca-certificates；精简系统若连 curl/wget 都没有，会先通过 APT 安装下载工具。

离线安装 Docker 本体仍使用包内的 Linux 静态文件；若 Ubuntu 缺少基础依赖，使用打包参数 `--docker-packages-dir` / `-DockerPackagesDir` 携带同一 Ubuntu 版本和 CPU 架构的 DEB 及全部依赖。部署时校验后用 dpkg 安装，不调用 APT 联网补依赖。Ubuntu 不会误用同时携带的 CentOS RPM。

部署脚本检查入口为 `scripts/tests/docker-bootstrap-smoke.sh`、`scripts/tests/docker-ubuntu-smoke.sh`；它们验证模拟安装分支，实际安装仍需在目标系统确认。

### 设备接入配置补充

离线模板包含 `IOT_DEVICE_HTTP_PUBLIC_URL` 与 `IOT_DEVICE_MQTT_PUBLIC_URL`，初始为空。请在目标环境 `.env.offline` 设置实际设备可达的 HTTPS/MQTT TLS 地址；前者为空使用相对 API 路径，后者为空显示未配置。Compose 同时包含 JWT username 校验和到期断连；已有数据卷中的动态认证配置需单独核实。升级沿用当前幂等 schema 迁移，保留历史数据。操作与测试边界见 [统一设备接入](UNIFIED_DEVICE_ONBOARDING.md)。

## 用户与权限交付

离线部署同样使用数据库中的用户、角色和设备范围，不依赖外部身份服务。内置管理员登录后，在「系统维护 → 用户与权限」创建普通用户，并告知其所属租户。设备范围默认无设备；菜单授权不能代替设备授权。

更新已有离线环境时，使用包含同版前后端的新镜像并沿用原数据卷；按 [用户权限升级](DEPLOYMENT.md#用户权限升级) 处理历史用户及旧 MQTT 会话。设备数据导出不包含 `platform_access`，该表需要数据库级备份。
