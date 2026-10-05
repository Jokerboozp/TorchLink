# 离线部署

离线包在联网打包机制作，目标机无需 Go、Node 或源码。离线安装命令在离线包根目录执行；模块开关、配置与维护见 [部署总览](DEPLOYMENT.md)。

## 离线部署

打包机需联网、Docker 和 Compose 2.24.4+，CPU 架构须与目标一致。Linux 目标机可从包内安装 Docker；Windows/macOS 须先安装 Docker Desktop。目标机无需 Go、Node 或源码。包内包含应用、基础环境、备份、运维组件、Harness 和带 pgvector 的 PostgreSQL。包内含知识向量与重排模型（embedding / reranker 服务），不含对话大模型；AI 对话仍需访问外部 API。

### 获取或制作离线包

在 GitHub Actions 页面手动运行 `.github/workflows/offline-bundle.yml`（workflow_dispatch）构建 Linux amd64 包，成功后发布到 Releases，不随合并自动发布；下载同一版本的全部分卷、`SHA256SUMS` 与 `DEPLOY.txt`，按说明校验和解压。GitHub 的 Source code 不是部署包。公开包不含现场密码或 API Key，首次安装在目标机生成配置；管理员和基础服务工具账号默认使用 `admin` / `admin123`，内部密钥独立生成。

手工打包（可带现有私有配置）：

```bash
bash scripts/package-offline.sh
# openEuler 目标追加：--target-os openeuler-24.03-lts-sp4
# 沿用已有配置追加：--env-file /path/to/.env.offline
# 不打包直播媒体服务：--without-video
```

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\package-offline.ps1
# 对应参数：-TargetOS openeuler-24.03-lts-sp4 -EnvFile <路径> -WithoutVideo
```

Linux / macOS 共用 Bash 入口。默认同时输出 `offline-bundles/iot-platform-offline-*` 目录、同名 `.tar` 和 `.tar.sha256`。归档含完整顶层目录及隐藏配置，只需上传归档和校验文件；目录输出仍可用于本机检查或直接部署。`.tar` 不压缩内容，可减少逐个传输文件的开销，但会额外占用约一份部署目录的磁盘空间。只需目录时，Bash 使用 `--skip-bundle-archive`，PowerShell 使用 `-SkipBundleArchive`。GitHub 公开发布流程跳过私有归档，在清除凭据后另行压缩分卷。

包内保留 `README.md` 和完整 `docs/` 目录，安装与维护入口为 `docs/DEPLOYMENT.md`，消防管理说明为 `docs/FIRE_SAFETY.md`。文档中的源码与开发测试入口在源码仓库使用。

在 Linux 服务器上，将归档和校验文件放在同一目录，替换下面的文件名后执行：

```bash
sha256sum -c iot-platform-offline-xxxx.tar.sha256
tar -xf iot-platform-offline-xxxx.tar
cd iot-platform-offline-xxxx
# 升级已有服务：部署前先复制旧包的 .env.offline 到当前目录。
bash scripts/deploy-offline.sh
```

已有目录也可以直接归档，无需重新构建镜像；在源码仓库根目录执行：

```bash
tar -cf offline-bundles/iot-platform-offline-xxxx.tar \
  -C offline-bundles iot-platform-offline-xxxx
cd offline-bundles
sha256sum iot-platform-offline-xxxx.tar > iot-platform-offline-xxxx.tar.sha256
```

手工生成的私有包包含凭据，不作为公开下载包分发；实际管理员密码以包内环境文件为准。`--skip-docker-runtime` 仅用于目标机已有 Docker。

若打包在 Harness 拉取阶段提示 `Your local changes ... would be overwritten by checkout`，且新克隆目录的修改集中于图片、字体等二进制文件，检查 `git --version`；Git 2.10 以前对上游 `text=auto eol=lf` 属性的处理可能触发此问题。拉取脚本通过 `.git/info/attributes` 保留仓库原始字节，在首次检出前设置该覆盖，不修改上游源码。真实源码修改仍会整体备份到 `upstream/deepseek-harness.backup-*`。同步最新 `scripts/fetch-deepseek-harness.sh`（Windows 对应 `scripts/lib/deployment.ps1`）后，可先单独拉取 Harness，再重跑原打包命令；无需删除 Docker 镜像或数据卷。

### 安装与升级

**确认版本**：`/health/live` 返回 `version`，用户菜单底部显示“平台版本”，启动日志 `platform build` 记录版本与提交。发布工作流用 `IOT_VERSION`、`IOT_REVISION` 构建参数写入离线包版本号与提交；自行构建未设置时显示 `dev`。单机 Compose 的 `platform-api` 与集群渲染的各平台角色都用镜像内的 `/app/iot-platform healthcheck` 探测本进程 `/health/live`（按容器的 `IOT_HTTP_ADDR`），`docker compose ps` 显示 `healthy` 只代表进程存活，依赖是否就绪仍看 `/health/ready`。Web 端口只转发 `/health/live`；`/health/ready` 含依赖明细，只能在 API 端口或内网访问。

**数据库迁移**：平台进程启动时自动执行待执行的数据库迁移。大版本升级可先在新镜像中单独执行并查看：`docker compose run --rm --no-deps platform-api /app/iot-platform migrate --check` 只列出将要执行的迁移、不修改数据库；去掉 `--check` 则执行迁移后退出，随后再重建 API。源码环境为 `go run ./cmd/iot-platform --env-file .env.local migrate --check`。已执行的迁移文件被修改时命令报错，不会继续。

升级前把原 `.env.offline` 复制到新包，保持原项目、数据卷、协议制品和密钥，不能用新配置中的凭据直接连接旧数据库。在包根目录执行：

```bash
sudo bash scripts/deploy-offline.sh
# macOS 使用同一脚本，省略 sudo
```

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\deploy-offline.ps1
```

脚本校验哈希和 CPU 架构、导入镜像、启动并检查服务，使用 `--no-build --pull never`。默认项目为 `iot-platform`，Web 为 `http://服务器IP:8080`。重复部署保留已有配置与数据；不创建模型卷、不下载模型。离线部署包不含业务数据库备份，AI 与知识向量计算仍需联网和相应 API Key。

仅替换 API/Web 镜像时，使用相同标签构建、导出与校验，在原包目录导入后重建 API；若已启用容量模块，同时重建使用该 API 镜像的 `capacity` 服务，再重建 Web（让 nginx 重新解析地址）。此方式不更新 Compose 或 Harness；这些组件变化时交付完整新包，不能再用旧 `images.tar` 覆盖更新。

### Linux 与 openEuler

Linux 自动安装要求 systemd、tar、iptables、xz、ps；健康检查另需 curl。已有 Docker/Compose 可用则复用，不自动更换 daemon 配置、清理数据或升级 Engine。在线补齐缺失工具；离线只从包内 `docker-runtime/` 校验安装。精简发行版须在打包时用 `--docker-packages-dir` / `-DockerPackagesDir` 携带匹配系统、版本和架构的 RPM/DEB 及依赖。

openEuler 24.03 LTS-SP4 用专用目标参数打包，准备容器内生成本地 RPM 源、索引和公钥，不把 RPM 安装到打包机。目标机保留签名与受保护包检查，按包名解析依赖，修复受管 Docker 的 SELinux 标签；不关闭 SELinux、不删 Docker 数据。专用目标不能同时跳过 Docker runtime 或混用其他发行版依赖。

旧专用包出现 `protected packages: grub2-pc` 时，在联网源码机运行 `bash scripts/repair-offline-openeuler.sh <旧包路径>`，校验输出补丁后解压到对应旧包，再运行部署。补丁只含引导脚本、RPM 索引及公钥；不使用 `--allowerasing` 或关闭引导包保护。缺失、损坏 RPM 需重新打包。
