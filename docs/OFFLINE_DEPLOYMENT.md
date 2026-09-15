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

无论打包机是 CentOS、Windows 还是 macOS，系统依赖准备都在临时 `openeuler/openeuler:24.03-lts-sp4` 容器内执行，不把 openEuler RPM 安装到打包机宿主系统。该容器会下载容器策略、SELinux 管理工具、iptables、xz、procps、curl 及所需 RPM 依赖，关闭弱依赖，并用 `createrepo_c` 生成本地软件源索引。空安装根目录的离线事务测试只请求上述包名，由解析器从本地源选择依赖。依赖包、索引、公钥附有 SHA256，连同 OS/版本/架构信息写入 `docker-runtime/packages`；下载或依赖检查失败不会输出打包完成。构建容器与目标 CPU 架构一致，仍不支持用 ARM 应用镜像包部署 x86 服务器。

### 将完整离线包复制到 openEuler

等待出现“离线包已生成”提示，将整个生成目录（包括隐藏文件 `.env.offline`、`images.tar` 和 `docker-runtime/`）复制到 openEuler 服务器，然后在**离线包根目录**执行：

```bash
sudo bash ./scripts/deploy-offline-linux.sh
```

首次安装或修复本项目安装的 Docker 时，部署脚本会：

1. 检查 SELinux 策略和管理工具，缺少时校验 RPM、索引和公钥，只启用包内 `file://` 软件源，按包名请求 `container-selinux policycoreutils-python-utils`。缺少基础工具时另请求 `iptables xz procps-ng`。保留 RPM 签名校验及系统受保护包规则，不将所有 RPM 作为安装目标；临时源配置在事务结束后移除。
2. 为 `/usr/local/lib/iot-docker` 设置标准程序目录的持久标签映射，恢复程序、数据及运行目录标签。
3. 对需要修复的受管 Docker 重启服务，确认进程进入 `container_runtime_t` 后才继续。已有容器会受该次重启影响；正常的受管 Docker 重复部署不会因此重启。

脚本不会关闭 SELinux、删除 Docker 数据或更换存储驱动。现有外部 Docker 安装及远程 Docker 上下文不自动修复；自定义 Docker 数据目录需要单独配置策略。修复过的运行环境须继续通过实际镜像导入和服务健康检查，不能以标签检查替代整个部署验收。

`generic` 仍为默认打包目标；专用目标不能与 `SkipDockerRuntime` / `--skip-docker-runtime` 或手工 `DockerPackagesDir` 同时使用。其他 SELinux 发行版需提供本发行版对应的容器策略及依赖包，不能混用 openEuler RPM。

2026-09-15 验证：脚本回归模拟已运行但处于 `init_t` 的 Docker，覆盖缺少策略、包损坏、OS 不匹配、标签和进程修复、幂等及远程上下文；系统调用使用模拟实现。本机没有 Docker Engine，尚未实际下载/构建专用包，也未在 openEuler SELinux 内核完成镜像导入验证。

2026-09-15 补充修复：用户在 CentOS 上实际拉取的 `openeuler/openeuler:24.03-lts-sp4` 返回 `ID="openEuler"`、`VERSION_ID="24.03"`、`VERSION="24.03 (LTS-SP4)"`，旧脚本因 ID 大小写拒绝。`bash scripts/tests/openeuler-release-smoke.sh` 使用该输出作为样本执行实际准备脚本，验证通过版本检查后才调用 DNF，并检查目标服务器身份规范化；DNF 被测试替身拦截，未据此宣称完整 RPM 下载、打包或实机部署通过。

### 旧包提示 protected packages: grub2-pc

旧部署脚本执行 `dnf install packages/*.rpm`，把依赖目录的全部候选 RPM 都作为安装目标，可能触发与现有引导包 `grub2-pc` 的冲突。该报错表示 DNF 在事务执行前阻止了安装；不要添加 `--allowerasing`、移除受保护包或关闭保护规则。现在改为本地软件源按需安装，方式可参考 [openEuler 本地软件源文档](https://docs.openeuler.org/zh/docs/24.03_LTS/docs/Administration/%E6%90%AD%E5%BB%BArepo%E6%9C%8D%E5%8A%A1%E5%99%A8.html)。

已有包含完整 RPM 的 openEuler 专用包可以只生成修复补丁，无需重建或传输镜像、模型。在**联网 CentOS/Linux 打包机的源码目录**执行，把参数替换成该旧包在打包机上的实际路径：

```bash
git pull --ff-only origin main
bash ./scripts/repair-offline-openeuler.sh \
  ./offline-bundles/iot-platform-offline-20260915-114745-82113b
```

修复工具校验旧包的目标身份和 RPM，在临时副本中借助 openEuler 容器补充索引、公钥，并执行按包名的离线事务测试。联网仅发生在打包机的准备容器中。失败不修改原包、不输出新补丁；缺少或损坏 RPM 时应重新准备完整包。成功后，旧包旁生成 `原目录名-rpm-repair.tar.gz` 和同名 `.sha256`，内容仅含部署引导脚本、RPM 索引和公钥。

将这两个文件复制到**openEuler 服务器上旧包的上一级目录**，进入该上级目录执行（目录名按实际替换；校验通过后再解压）：

```bash
bundle=iot-platform-offline-20260915-114745-82113b
sha256sum -c "$bundle-rpm-repair.tar.gz.sha256" && \
  tar -xzf "$bundle-rpm-repair.tar.gz" -C "$bundle" && \
  cd "$bundle" && \
sudo bash ./scripts/deploy-offline-linux.sh
```

补丁不包含 `.env.offline`、镜像、模型或业务数据。应用时需对应生成补丁所用的旧包。若安装仍报依赖冲突，保留完整 DNF 日志排查；空安装根目录测试无法代替目标机器已有软件包状态的兼容性验证。

2026-09-15 回归：`docker-rpm-repo-smoke.sh` 用带有引导 RPM 候选的模拟主机重现旧调用触发受保护包错误，验证 DNF/YUM 只请求指定包、本地源及签名校验、事务失败和损坏索引处理；`openeuler-rpm-preparation-smoke.sh` 执行实际准备脚本，替换 DNF、索引工具和公钥读取；`openeuler-repair-smoke.sh` 执行实际修复脚本、归档校验和解压，验证镜像与配置保留。包管理器及 Docker 容器操作均为模拟，尚未完成 openEuler 实机事务与镜像导入验证。

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

同一个包可以重复执行部署命令：配置与数据卷保持原值，模型恢复通过 `scripts/lib/restore-ollama-models.sh` 逐文件补齐缺失文件，先复制 blobs 再复制 manifests，保留已有文件。每个新文件复制到临时文件后再改名，避免中断后留下被下次恢复跳过的半成品。模型解压会临时额外占用一份模型大小的磁盘空间。不要用重新生成随机凭据的新包直接替换已有数据库部署；升级时应沿用原来的 `.env.offline` 作为打包配置。

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
- 精简离线系统若缺少这些基础包，打包时使用 `--docker-packages-dir /path/to/packages` 或 `-DockerPackagesDir C:\\packages` 加入与目标发行版、版本和架构匹配的 RPM/DEB 及全部依赖。手工 RPM 目录还须包含通过目标发行版 `createrepo_c` 生成的 `repodata/` 和本发行版 `RPM-GPG-KEY-*` 公钥，打包入口会为其写入 SHA256。目标机使用 DNF（无 DNF 则用 YUM）仅从本地源按包名解析，缺少索引或包管理器时停止；不再直接批量 `rpm -Uvh`。DEB 仍使用 dpkg。部署不联网解决依赖，也不跳过依赖检查。
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

### API 反复重启：mqtt-inbox permission denied

如果 API 日志反复出现 `mkdir /app/data/mqtt-inbox: permission denied`，前端同时返回 502，应先恢复 API 数据目录写权限。当前 API 镜像使用 distroless `nonroot`（UID/GID `65532:65532`），`/app/data` 使用 `platform-data` 命名卷。旧镜像没有预建属于该用户的数据目录，导致新卷默认属主不匹配。

修复后的 Dockerfile 预建数据目录并以 `65532:65532` 复制进运行镜像，供新卷初始化。已有卷仍保留原内容和属主，单纯重建镜像不能修正已有卷，见 [Docker 数据卷初始化规则](https://docs.docker.com/engine/storage/volumes/)。

对于标准离线部署，在服务器执行下面命令即可恢复已有数据卷，无需联网或重新构建镜像。容器名按实际替换；辅助容器挂载 API 实际使用的卷，命令只处理 `/app/data`，不删除数据、不使用 `chmod 777`：

```bash
sudo docker stop iot-platform-platform-api-1 && \
sudo docker run --rm --pull never --user 0:0 \
  --volumes-from iot-platform-platform-api-1 \
  alpine:3.22 sh -ec 'chown -R 65532:65532 /app/data; chmod u+rwx /app/data' && \
sudo docker run --rm --pull never --user 65532:65532 \
  --volumes-from iot-platform-platform-api-1 \
  alpine:3.22 sh -ec 'mkdir -p /app/data/mqtt-inbox; test -w /app/data/mqtt-inbox' && \
sudo docker start iot-platform-platform-api-1
```

然后检查 API 日志和前端健康接口：

```bash
sudo docker logs --tail 60 iot-platform-platform-api-1
curl -fsS http://127.0.0.1:8080/health/ready
```

启动需要时间；若 API 已正常监听而前端仍连接旧地址，执行 `sudo docker restart iot-platform-platform-web-1` 后再次检查。如果改属主或写入检查仍报权限错误，保留错误并检查实际挂载是否只读及 SELinux 拒绝记录，不关闭 SELinux 绕过检查。

镜像构建后的真实回归入口：`bash scripts/tests/platform-data-permissions-smoke.sh iot-platform-api:offline`，需要本地已有 API 和 `alpine:3.22` 镜像。测试只创建独立容器及匿名卷，用 API 的非 root 身份重现 MQTT 目录创建及持久化读写，结束后清理测试资源。2026-09-15 本机没有 Docker，已核对 Dockerfile 和脚本语法，尚未完成此真实容器测试或目标服务器恢复验收；用户提供的日志是本次启动失败的直接依据。

### AI 助手 RUNTIME_ERROR 与模型列表为空

在模型服务配置页面切换 Ollama、DeepSeek 或兼容接口时，升级后的 API 允许管理员直接填写平台可达的 HTTP/HTTPS 地址，不再使用 `IOT_AI_PROVIDER_TEST_ALLOWED_ORIGINS` 白名单。新离线包不再生成该项，旧环境文件可保留或移除；已有旧包需更新 API 镜像后才生效，单改 `.env.offline` 不会改变旧镜像行为。具体配置见 [界面切换 AI 模型服务](DEPLOYMENT.md#在界面切换-ai-模型服务)。

已有离线部署可以只更新 API 镜像来取消限制。在**联网 CentOS/Linux 打包机源码目录**执行：

```bash
git pull --ff-only origin main
sudo docker build -t iot-platform-api:offline . && \
sudo docker save -o iot-platform-api-update.tar iot-platform-api:offline
```

把 `iot-platform-api-update.tar` 复制到目标服务器的原离线包目录，在该目录执行：

```bash
sudo docker load -i iot-platform-api-update.tar && \
sudo docker compose --project-name iot-platform \
  --env-file .env.offline -f compose.yaml -f compose.offline.yaml \
  up -d --no-deps --force-recreate --no-build --pull never platform-api && \
sudo docker restart iot-platform-platform-web-1
```

命令沿用原配置和数据卷；API 与前端会短暂重启，待服务恢复后在模型配置页重新测试并应用地址。这是增量更新，原包的 `images.tar` 仍包含旧镜像；之后完整部署应使用新包，重跑旧包部署脚本会再次导入旧镜像。

`Harness runtime request failed / RUNTIME_ERROR` 是网关的通用异常提示。若以默认用户执行 `runtime-smoke.mjs` 明确报 `Cannot read package config .../dsh-sdk-client/package.json: permission denied`，说明运行用户无法读取镜像内的依赖。旧构建阶段以 root 测试，未覆盖最终 `node` 用户的权限。修复后的 Harness 镜像显式设置程序目录可读、可遍历，并在最终 `USER node` 后执行同一运行时测试。

当前容器可直接修正程序文件权限，无需联网、重建镜像或将服务改为 root。命令只调整镜像内的程序目录，不修改 `/data` 中的会话和配置；容器重启后仍保留，容器重建后需使用修复后的镜像或重新执行修正：

```bash
sudo docker exec --user 0:0 iot-platform-deepseek-harness-1 \
  sh -ec 'chmod a+rx /harness; chmod -R a+rX /harness/runtime-node /harness/examples' && \
sudo docker exec iot-platform-deepseek-harness-1 \
  node /harness/examples/iot-ops-agent/runtime-smoke.mjs && \
sudo docker restart iot-platform-deepseek-harness-1
```

测试应输出 `DeepSeek Harness runtime smoke passed`。它使用临时目录、模拟模型和 MCP，不证明真实 Ollama 模型已就绪。如果 `sudo docker exec iot-platform-ollama-1 ollama list` 只有表头，说明当前服务没有可列出的模型，仍需恢复模型后才能调用。

在**含 `ollama-data.tgz` 和 `.sha256` 的离线包目录**执行下列命令。旧包需先把更新后仓库的 `scripts/lib/restore-ollama-models.sh` 复制到包内同名路径，新包自动携带。使用当前 Ollama 容器的实际挂载卷，临时解压后仅补齐缺失文件，保留已有模型。临时容器可写层还需容纳一份解压后的模型；目标卷也需足够空间。

```bash
sha256sum -c ollama-data.tgz.sha256 && \
sudo docker run --rm --pull never --user 0:0 \
  --volumes-from iot-platform-ollama-1 \
  --mount "type=bind,source=$PWD,target=/backup,readonly" \
  alpine:3.22 sh /backup/scripts/lib/restore-ollama-models.sh \
  /backup/ollama-data.tgz /root/.ollama && \
sudo docker restart iot-platform-ollama-1
sudo docker exec iot-platform-ollama-1 ollama list
```

若模型归档缺失或校验失败，应从原打包机补传匹配归档及校验文件。恢复脚本会拒绝没有模型 manifests 的归档；仅有 blobs 不能提供可列出的模型。恢复后确认平台所配置的模型名称在列表中，再重试 AI 助手。

2026-09-15 现场反馈：Harness 权限修正后的运行时测试通过，1.5 GB 模型归档 SHA256 通过，但旧恢复命令执行成功后列表仍为空。原因是 [BusyBox 的 `cp -n` 实现](https://raw.githubusercontent.com/mirror/busybox/master/libbb/copy_file.c) 在目标已存在时直接返回，发生在目录递归之前；`cp -an /tmp/restore/. /dst/` 因此跳过整个已有目标目录。此前 GNU cp 的本机验证未覆盖这一差异。

回归入口 `scripts/tests/ollama-restore-smoke.sh` 须用 BusyBox 的 sh/cp/tar 执行；`--legacy` 参数应重现“返回成功但 manifests 不存在”，默认模式验证恢复新文件、保留旧文件、重复恢复及拒绝没有索引的归档。本机 BusyBox Windows 端口 v1.38.0 已重现旧命令失败并通过新脚本测试；此结果不等于目标 Alpine/Docker/Ollama 验收，目标仍需确认恢复文件计数、模型列表和 AI 请求。

### 设备接入配置补充

离线模板包含 `IOT_DEVICE_HTTP_PUBLIC_URL` 与 `IOT_DEVICE_MQTT_PUBLIC_URL`，初始为空。请在目标环境 `.env.offline` 设置实际设备可达的 HTTPS/MQTT TLS 地址；前者为空使用相对 API 路径，后者为空显示未配置。Compose 同时包含 JWT username 校验和到期断连；已有数据卷中的动态认证配置需单独核实。升级沿用当前幂等 schema 迁移，保留历史数据。操作与测试边界见 [统一设备接入](UNIFIED_DEVICE_ONBOARDING.md)。

## 用户与权限交付

离线部署同样使用数据库中的用户、角色和设备范围，不依赖外部身份服务。内置管理员登录后，在「系统维护 → 用户与权限」创建普通用户，并告知其所属租户。设备范围默认无设备；菜单授权不能代替设备授权。

更新已有离线环境时，使用包含同版前后端的新镜像并沿用原数据卷；按 [用户权限升级](DEPLOYMENT.md#用户权限升级) 处理历史用户及旧 MQTT 会话。设备数据导出不包含 `platform_access`，该表需要数据库级备份。
