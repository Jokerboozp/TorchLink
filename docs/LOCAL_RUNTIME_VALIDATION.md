# Ubuntu 虚拟机本地运行验证

验证日期：2026-09-09。运行方式为 Ubuntu 虚拟机内的 Docker Engine 启动依赖，Mac 运行 Go API、Vue/Vite 和备份服务。操作说明统一维护在 [部署文档](DEPLOYMENT.md#orbstack-虚拟机本地调试)。本记录不包含环境凭据。

## 环境与结论

| 项目 | ARM64 | x86_64 |
|---|---|---|
| 虚拟机 | OrbStack `develop`，Ubuntu 26.04.1，原生 ARM64 | OrbStack `iot-verify-amd64`，Ubuntu 24.04.5，ARM Mac 上模拟 x86 |
| Docker / Compose / Buildx | 28.5.2 / 2.27.3 / 0.14.1 | 28.5.2 / 2.27.3 / 0.14.1 |
| 缺少 Docker、Git 的首次准备 | 脚本自动安装并完成启动 | 脚本自动安装；补充 EMQX 模拟环境兼容参数后，完整一键启动通过 |
| Harness 镜像 | 完整构建、无网络编译及镜像内运行时检查通过 | 同左 |
| 依赖真实读写 | 八项全部通过 | 八项全部通过 |
| Linux 对应架构 Go API | 启动及全部就绪检查通过 | 启动及全部就绪检查通过 |
| Mac Go API + Vite | 登录成功，浏览器页面错误为零 | 登录成功，浏览器页面错误为零 |
| 容器访问 Mac API | `host.orb.internal:8081` 返回 HTTP 200 | 同左 |
| 本地模型 | `nomic-embed-text` 嵌入和 `qwen3:1.7b` 推理通过 | 同左 |

Mac 使用 Go 1.27.0、Node 22.23.2。原有 Node 18 不满足前端要求；已安装 Homebrew `node@22`，使用显式 PATH 运行前端依赖安装、构建和 Vite，没有替换系统默认 Node。

## 复现命令与验证层次

在 Mac 仓库根目录运行；日常使用 ARM `develop`，环境文件为被 Git 忽略的 `.env.local`：

```bash
orb -m develop sudo bash scripts/setup-local.sh --skip-code-deps --include-ai \
  --dependency-host 127.0.0.1 --api-host host.orb.internal
go run scripts/tests/local-runtime-smoke.go --env-file .env.local
```

x86 验证使用独立 `.env.verify-amd64`，加入 `IOT_EMQX_ERL_FLAGS="+JMsingle true"`，命令增加 `--env-file .env.verify-amd64` 并替换虚拟机名称。两套依赖串行运行，避免共享的 localhost 转发端口相互覆盖。x86 模型文件从 ARM Ollama 卷复制后实际加载推理，未重复下载；其余镜像及 Harness 构建使用 x86 架构。

`local-runtime-smoke.go` 通过临时资源检查 PostgreSQL 写入读取、Redis 认证读写、ClickHouse 认证读写、MinIO 上传下载、Kafka 广播地址与生产消费、Ollama 嵌入、MQTT JWT 认证与发布订阅、Weaviate 就绪；检查后清理临时资源。额外在 ARM PostgreSQL 执行以下真实数据库集成用例并通过：

```text
TestMigrateLegacyAIAnalysisTenantOwnership
TestDeviceOperationsMigrationAndAtomicity
```

API 使用 `/health/ready` 验证 repository、archive、eventBus、realtime、knowledge 全部为 `ok`；ARM 环境还启动源码版备份服务，其 `/health/ready` 返回 `UP`。Chrome 无头浏览器通过 Vite 代理登录并等待管理端加载。Qwen 通过 Ollama `/api/generate` 实际完成推理并返回 `OK`；这不代表复杂 AI 工作流或告警研判效果已验收。

以下为源码与部署工具验证，和以上真实中间件验证分别计数：

- `go test ./internal/deploycheck` 通过。
- `scripts/tests/deployment-smoke.sh` 在 macOS Bash 与 Ubuntu Bash 通过：使用真实 Compose 解析，模拟 Docker/HTTP 操作，覆盖本地准备、重复执行和在线/离线失败路径。
- `scripts/tests/docker-bootstrap-smoke.sh`、`scripts/tests/docker-ubuntu-smoke.sh` 通过，覆盖 ARM/x86 安装包选择、Ubuntu 基础依赖及 Git 缺失/安装失败场景；安装操作使用模拟测试。
- Node 22 下 `npm ci`、`npm run build` 通过；构建仍有既有大 chunk 提示。
- Go API 分别交叉编译为 Linux ARM64、amd64 静态程序，并在上述对应架构虚拟机中运行。
- 当前 Compose 使用的第三方基础镜像清单均包含 `linux/arm64` 和 `linux/amd64`。

## 本次修复

1. Linux 本地准备接入已有 Docker/Compose/Buildx 安装能力，并按需安装 Harness 必需的 Git。
2. 为 Redpanda 初始化任务补充成功完成依赖，避免 Compose `up --wait` 将正常退出的初始化容器误判为启动失败。
3. Harness 显式安装依赖后关闭 pnpm 脚本前自动重装，完整编译阶段禁用网络；避免可选 CLI 二进制被反复下载而超时。源码版本标记不再无变化重写，减少重复启动时的文件改写。
4. EMQX 提供模拟环境的可选 Erlang JIT 参数。独立复现确认默认双重映射在此次 x86 模拟下导致 QUIC NIF 加载失败，单一映射后模块、MQTT 与健康检查通过；原生架构保持默认参数。
5. 修复 macOS Bash 的中文标点变量展开和测试中的 GNU `sed -i` 依赖，补充对应回归检查。
6. 补充 OrbStack 共享路径、localhost 转发、Mac API 回调和 Node 版本使用说明。

## 验证边界

本次覆盖本地依赖容器部署和源码调试。x86 结果来自仿真虚拟机，不是物理 x86 服务器验收；未实际执行完整在线生产部署、离线包跨机恢复、真实消防设备接入或生产验收。Docker Hub 在重复构建时返回临时 EOF，直接拉取基础镜像后重跑完整启动成功；首次准备仍需要可访问镜像仓库、Git 与依赖/模型下载源。
