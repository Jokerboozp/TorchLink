# 集群部署与进程拆分

多节点部署、高可用边界，以及 API 与独立接入网关等进程角色的拆分。单机配置与维护见 [部署总览](DEPLOYMENT.md)。

## 集群部署

多节点部署由**集群清单**统一描述，`scripts/cluster-up.sh` / `.ps1` 一条命令完成镜像、秘密、渲染、节点预检、下发、按阶段启动、初始化与就绪检查；其中 `cmd/cluster-render` 为每个节点生成独立的 Compose 项目（主机网络、固定端口），`scripts/cluster-deploy.sh` / `.ps1` 按阶段下发与启动。Compose 只管理本节点；跨节点布局、故障域和连接预算由清单校验。

| 组件 | 集群形态（示例清单 `deploy/cluster/inventory.example.yaml`） |
| --- | --- |
| Redpanda | 3 节点，业务主题与死信主题复制因子 3，关闭自动建主题 |
| PostgreSQL | Spilo（Patroni）3 成员 + etcd 3 节点，可选同步备库；平台使用多主机 DSN `target_session_attrs=read-write` 连接当前主库，历史查询使用 `prefer-standby` 只读 DSN |
| ClickHouse | 2 分片 × 2 副本 + Keeper 3 节点，`*_local` 复制表与同名 Distributed 表，插入按法定副本确认 |
| Redis | 主 + 2 副本 + Sentinel 3 个，平台经 Sentinel 跟随主节点 |
| EMQX | 3 节点静态集群 |
| 平台 | api、gateway、parser、processor、jobs 各自多实例；API 之间选举视频控制实例；Harness 多实例按会话路由 |
| 监控 | Prometheus 按实例抓取所有平台进程、Redpanda、EMQX 与各节点 node-exporter，加载与单机相同的平台告警规则；同节点 Alertmanager（9093）接收告警，运维中心可查看 |
| RustFS | 示例为单实例；`rustfs.nodes`（至少 4 个节点，每节点一个数据目录）渲染分布式纠删码部署，任一节点故障时读写可用，平台与备份经本机 HAProxy `127.0.0.1:18183` 访问健康节点。RustFS 拒绝同一块盘上的多个数据目录，3 节点单盘无法组成可用集群 |
| 视频媒体 | 示例为单实例；`video.nodes` 第一个为主媒体服务器、其余为备用，直播模块与 HLS 代理（HAProxy `127.0.0.1:18180`，`balance first`）都使用第一台健康的服务器，详见 [备用媒体服务器](DEPLOYMENT.md#摄像头部署) |
| 监控高可用 | `monitoring.nodes`（≥2）在每个节点运行独立的 Prometheus 副本（相同抓取目标，外部标签 `replica`）和组成集群的 Alertmanager（9094 互联）；各副本向全部 Alertmanager 发送告警并去掉 `replica` 标签，由集群去重与共享静默。运维中心经 HAProxy `127.0.0.1:18190` / `18193` 访问健康实例，两个副本的历史数据各自独立 |

校验规则：节点须写明故障域（独立主机/供电/机柜；同一宿主上的虚拟机属于同一故障域）；仲裁组（etcd、Patroni、Redpanda、Keeper、Sentinel）为奇数成员且任一故障域不占多数；同一 ClickHouse 分片的副本、Redis 主从、EMQX 成员跨故障域；同一节点端口不冲突；各角色 PostgreSQL 连接池合计（含一次滚动升级额外实例、备份与初始化连接及预留）不超过 `max_connections`。

### 一键部署与升级

在仓库根目录执行（不带参数即进入向导）：

```bash
bash scripts/cluster-up.sh
```

Windows 控制机（PowerShell，需 Docker Desktop 与 OpenSSH 客户端）：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\cluster-up.ps1
```

向导在开始时一次问完，之后无人值守执行：

1. 集群名称（默认 `torchlink`）与**节点数量**（至少 3 台；1 台请用单机部署，2 台无法形成仲裁）。
2. 每个节点的 IP；SSH 用户名（默认 `root`）与端口（默认 22）。
3. **SSH 登录方式**：所有节点统一密码（输入一次）或每个节点独立密码（逐个输入）。密码隐藏输入、只在内存中使用：脚本用它登录一次，记录主机密钥（`.cluster/<名称>/known_hosts`，首次信任，之后变化即拒绝），并把部署专用公钥写入各节点 `~/.ssh/authorized_keys`（注释为 `torchlink-deploy@<名称>`，可据此撤销）；此后所有操作用私钥 `.cluster/<名称>/deploy_key` 登录，升级时不再询问密码。
4. **服务统一密码**：用于数据库/缓存工具账号、PostgreSQL 应用、Redis、ClickHouse、RustFS、MQTT、Kafka、EMQX 控制台和平台管理员；直接回车使用 `admin123`，工具用户名默认 `admin`。自定义密码至少 8 位，只能包含字母、数字与 `. _ ~ -`。JWT、Harness、摄像头、服务间令牌以及 PostgreSQL 超级用户与复制凭据仍独立生成。默认值只填充缺项；再次执行保留已有秘密，显式指定与现有密码不同的值会拒绝，已有账号密码需单独同步。
5. 是否部署摄像头直播模块；DeepSeek API Key（可留空，部署后可在“模型管理”填写）。

节点布局按节点数自动生成到 `.cluster/<名称>/inventory.yaml`：每个节点视为独立故障域；etcd、PostgreSQL、Redpanda、EMQX、Redis 与 Sentinel 放在前 3 台；ClickHouse 3 台时为 1 分片×3 副本，4–5 台为 2×2，6 台及以上为 2×3；RustFS、视频、容量测试、备份与监控放在最后一台；api、gateway、jobs、Harness、Web 各 2 个实例，parser、processor 各 3 个。可以手动修改该文件后重新执行。已有自写清单时用 `--inventory <文件>`（首次同样询问 SSH 密码）；自有私钥用 `--ssh-key <文件>`，此时不安装部署密钥。

无人值守：`bash scripts/cluster-up.sh --name <名称> --nodes IP1,IP2,IP3 --yes`，SSH 密码与服务统一密码经环境变量 `TORCHLINK_SSH_PASSWORD`、`TORCHLINK_SERVICE_PASSWORD` 提供（PowerShell 为 `-Name`、`-Nodes`、`-Yes`）。

脚本依次完成：

1. **镜像**：用当前源码构建平台、Web、Harness、备份、PostgreSQL pgvector 与媒体镜像（清单中写成 `镜像@sha256:` 的改为拉取），拉取其余第三方镜像。控制机不需要 Go：渲染、初始化与 SSH 准备工具随平台镜像提供。
2. **SSH 与秘密**：按上述方式安装部署密钥；在 `.cluster/<名称>/secrets.yaml`（0600，已被 Git 忽略；可用 `--secrets` 指定）生成缺失的密码与令牌，设置了服务统一密码的项使用它，已有值保持不变。`deepseekApiKey`、`embeddingApiKey` 和独立恢复目标配置需要时自行填写，也可部署后在“模型管理”配置模型密钥。
3. **渲染**：生成各节点的 Compose 项目到 `.cluster/<名称>/rendered`，上一版改名为 `rendered.prev` 用于回滚。
4. **节点预检**：经 SSH 检查每个节点的 Docker 与 Compose v2、Docker 可用空间（至少 20 GiB）、与控制机的时钟差（不超过 5 秒）；首次部署时检查所需端口未被其他程序占用。任一问题都会在改动节点之前列出并停止。
5. **下发镜像**：按镜像 ID 比较，只把节点缺少或版本不同的镜像以 `docker save | gzip | ssh docker load` 方式发送。
6. **启动与初始化**：按阶段启动（coordination → data → init → support → workers → edge），每阶段等待容器健康；init 在第一个 API 节点上用平台镜像运行 `cluster-init`，创建应用库、迁移结构、创建主题并核验 ClickHouse 表，数据库选主期间自动重试；最后逐实例检查 `/health/ready`。
7. **结果**：输出 Web、MQTT、设备 HTTP 与 TCP 入口地址，以及管理员账号和密码所在位置。

交互终端下，脚本在下发前会显示“首次部署/升级”和节点数并请求确认，`--yes`（`-Yes`）跳过确认。`--dry-run` 只渲染到 `rendered.dry-run` 并打印将执行的命令，不改动任何节点，也不询问密码。节点上的 SSH 用户须能直接执行 `docker`（root 或 docker 组成员），sshd 需允许密码登录一次以安装部署密钥。

**离线环境**：在可联网的机器上执行 `bash scripts/cluster-up.sh --name <名称> --bundle cluster-images.tar`（或 `--inventory <清单>`），构建并拉取全部镜像后写入一个归档，这一步不连接节点；把源码、`.cluster/<名称>/` 和归档带到离线控制机，执行时加 `--images cluster-images.tar`（PowerShell 为 `-Bundle` / `-Images`）。

**内部负载均衡**：清单中 `platform.internalURL`、`gatewayURL` 留空时，每个节点运行一个只监听 `127.0.0.1` 的 HAProxy（`18181` 转发到全部 API 实例，`18182` 转发到全部 Gateway，按 `/health/ready` 摘除故障实例），Web 代理、Harness 回调、媒体回调和 API 到 Gateway 的转发都走本机负载均衡，不需要额外的负载均衡设备。如已有负载均衡器，两个地址同时填写即可，此时不渲染 HAProxy。面向用户和设备的入口（Web、EMQX、Gateway 节点）仍需由 DNS、VIP 或外部负载均衡器统一，脚本结束时会列出各组地址。

**升级与回滚**：修改源码或清单后执行 `bash scripts/cluster-up.sh --name <名称>` 即为升级，不再询问问题，秘密沿用，只发送变化的镜像，容器按阶段重建。回滚：

```bash
bash scripts/cluster-deploy.sh --rendered .cluster/<名称>/rendered.prev --ssh-user root --ssh-key .cluster/<名称>/deploy_key --known-hosts .cluster/<名称>/known_hosts
```

知识索引复用 PostgreSQL HA 集群，Spilo/Patroni 镜像同样包含固定版本 pgvector；各管理 API 使用同一外部 Embedding 配置及重建锁。Harness 节点保留动态 Agent 和会话持久化，备份通过受控内部 snapshot 接口收集各实例；向量 API Key 在集群秘密文件中自行填写，脚本不生成虚假的云服务密钥。

`.cluster/<名称>/` 中的秘密文件决定已初始化数据库的密码，`deploy_key` 用于登录节点，务必另行备份；丢失秘密后重新生成的值无法连接已有数据。

### 分步执行

需要只校验清单、只渲染或只操作部分节点时，可单独使用各步骤：

```bash
go run ./cmd/cluster-render -inventory deploy/cluster/inventory.example.yaml -check
```

```bash
go run ./cmd/cluster-render -inventory <清单> -secrets <0600 秘密文件> -init-secrets -out dist/cluster/<名称>
```

```bash
bash scripts/cluster-deploy.sh --rendered dist/cluster/<名称> --ssh-user <用户>
```

- 秘密文件模板为 `deploy/cluster/secrets.example.yaml`，`-init-secrets` 会补齐缺项；嵌入连接串的密码只能用字母、数字与 `. _ ~ -`。秘密只写入需要它的节点的 `.env`（0600）与本机 `init.env`，`compose.yaml` 和配置文件只含变量引用。
- Broker 服务与管理账号首次默认 `admin` / `admin123`；需要自定义时，在秘密文件填写 `kafkaSaslUsername` / `kafkaSaslPassword`、`kafkaSaslMechanism`（默认 `SCRAM-SHA-256`）、`kafkaTls` 及可选 `kafkaTlsCaFile`；Redpanda 管理接口使用成组的 `kafkaAdminUrl` / `kafkaAdminUsername` / `kafkaAdminPassword`。这些参数统一进入所有平台角色及 `init.env`，不要在清单 `env` 中重复设置。使用一键 `cluster-up` 时，把 PEM CA 证书放在秘密文件目录或其子目录内，填写相对路径（以秘密文件目录为准）；直接运行 `cluster-render` 也支持本机绝对路径。渲染器复制证书，各角色和远程 `cluster-init` 只读挂载，本地初始化自动使用控制机上的副本。证书文件不得包含私钥。
- **告警**：监控节点的 Prometheus 加载平台告警规则（`ops/prometheus/alerts.yml` 内嵌进渲染器，平台进程的 job 名在集群中为 `platform`），Alertmanager 与其同节点、只监听 9093 且不组集群。秘密文件填写 `alertWebhookUrl`（http/https）后所有告警（含恢复）推送到该地址，否则只在 Alertmanager 与运维中心可见。清单未写 `images.alertmanager` 时使用 `prom/alertmanager:v0.34.1`。
- **HTTPS 与 MQTTS**：秘密文件填写 `tlsCertFile` 与 `tlsKeyFile`（PEM，路径规则同 `kafkaTlsCaFile`，证书与私钥须匹配）后，渲染器把它们复制到各 Web 与 EMQX 节点的 `tls/` 目录：Web 开启 8443 并把 8080 跳转到 HTTPS（`IOT_WEB_TLS_REDIRECT=false` 可关闭跳转），EMQX 开启 MQTTS 8883 与 WSS 8084；部署计划列出 `web-https`、`mqtts` 入口。未配置时保持明文入口。与单机相同，私钥以 0644 写出，供容器内非 root 用户读取；渲染目录本身已含各节点秘密，须按秘密保管。
- `kafkaPublicBrokers`（逗号分隔的 `host:port`）和 `mqttPublicUrl` 默认由 Broker 节点地址生成；若对接方通过域名、代理或 TLS 端口连接，应显式覆盖并与 Broker 广告地址一致。`mqttToolUsername` / `mqttToolPassword` 配置独立 MQTT 工具账号。首次启动会初始化工具账号和认证；已有数据卷须核对实际账号，参见[Kafka 对接账号认证与授权](DEPLOYMENT.md#kafka-对接账号认证与授权)。
- `cluster-deploy` 不负责镜像：节点需已有镜像，或用 `--images <归档>` 让每个节点整体导入。`--stage`、`--nodes` 可只执行指定阶段和节点，`--dry-run` 只打印命令；`--cluster-init "go run ./cmd/cluster-init"` 改为在控制机本地运行初始化。
- 部署顺序与每阶段内容同上一小节第 6 步。

**扩缩容与升级**：修改清单后重新执行 `cluster-up`；新节点需先满足预检要求。只想操作部分节点时，用 `cluster-deploy` 对变化的阶段和节点执行，例如 `--stage workers --nodes n5`。扩容前先看渲染输出的连接预算。升级按 workers → edge 逐角色滚动；processor/parser 缩容时进程先停止领取，未完成消息的领取租约到期后由其他实例接管。Redpanda、ClickHouse 扩容涉及数据重分布，按组件文档限制重建流量，并把重建期间纳入容量测试。

回滚命令见 [一键部署与升级](#一键部署与升级)。切回配置不会回退已写入的数据，存储迁移需保留备份点与核对记录。

**从单节点迁移**：

1. 在单节点上排空 `parser`/`processor` 积压，停止设备接入或让设备保留未确认报文，记录 PostgreSQL 备份点与 Kafka 偏移。
2. 渲染并按阶段启动集群到 init 完成；PostgreSQL 数据用 `pg_dump`/`pg_restore` 导入新主库后再运行 `cluster-init -execute`。
3. ClickHouse 用迁移工具按月分区回填并核对（默认只输出计划）：

   ```bash
   go run ./cmd/clickhouse-migrate -source <单节点 URL> -target <集群节点 URL> -cluster iot_cluster
   ```

   计划无误后追加 `-execute`；部分填充的目标分区会被阻止，须检查后删除再重跑。核对使用行数、唯一消息数与消息 ID 校验和。
4. 启动 workers 与 edge，用少量设备核对原文、解析、状态、告警与权限，再切换入口并逐步放量。旧环境保留到新集群通过核对与容量快速回归。

渲染与脚本的仓库内验证覆盖清单校验、端口与连接预算、生成文件的 `docker compose config` 解析、部署脚本的阶段顺序（`--dry-run`），以及用模拟的 `docker`/`ssh` 走完一键部署全流程（首次部署、升级、预检拦截、离线归档）；目标机上的真实启动、Patroni/Sentinel/Keeper 实际选主与切换须在目标环境演练并记录。

## 高可用边界

默认 Compose（本地、在线、离线）是**单节点**配置：PostgreSQL、ClickHouse、Redis、Redpanda、EMQX、RustFS、Harness 与 API 各运行一个实例，Redpanda 主题创建为 `--replicas 1`。它可以承担单机生产，但不具备高可用：

- 容器自动重启只在进程退出后拉起同一实例，不能在宿主机、磁盘或数据卷故障时切换。
- MQTT 持久队列（`IOT_DATA_DIR/mqtt-inbox/`）保证已确认报文在本机磁盘上重启后可继续处理，不复制到其他节点。
- 备份用于事后恢复数据，恢复需要停机与人工操作，不是故障切换；备份存在不等于已验证可恢复。
- 拆分 `api` / `gateway` 与多副本 API 只分担接入和查询，前提是数据库、消息与对象存储本身可用。
- 运维中心依赖（`--profile ops` 的 Prometheus、Loki、Grafana、Alertmanager）同样各一个实例；它们停止时接入与告警链路不受影响，但期间的监控数据、日志与告警通知会缺失。

单机部署无法靠增加配置变成高可用：所有组件与数据都在一台宿主机上，宿主机或磁盘故障时只能在新机器上恢复（见 [运维手册](OPERATIONS.md#故障切换)）。需要高可用时使用上一节的 [集群部署](#集群部署)：Redpanda、PostgreSQL、ClickHouse、Redis、EMQX 与各平台角色均为多实例；RustFS（`rustfs.nodes` 分布式）、视频媒体（`video.nodes` 主备）与 Prometheus / Alertmanager（`monitoring.nodes`）也可多实例，示例清单为节省资源仍各放一个节点，按需改为多节点。视频媒体切换会中断正在播放的流并由播放器重建；已部署的单实例 RustFS 改为分布式时新集群从空盘开始，须先用 `rclone sync` 等工具迁移桶数据；Prometheus 副本之间不复制历史数据。知识索引随 PostgreSQL HA 集群保存。节点故障与切换须在目标环境演练，仓库内只验证渲染与部署编排。

### 常见排查

| 现象 | 检查入口 |
| --- | --- |
| 地址或密码似乎未生效 | IDE 进程变量优先；确认原环境文件、Compose 项目和已有数据库密码 |
| API 端口或 MQTT 收件箱已被占用 | 按[进程交接](DEPLOY_LOCAL.md#本地-api-进程交接)核对并正常退出旧实例，不删除锁文件或数据 |
| API 存活但业务不可用 | `/health/ready`、消费者积压、数据库及 `docker compose logs`；live 只表示进程存活 |
| 镜像或制品哈希不符或缺失 | 在有网机器重做完整包，不在离线目标机拉取 |
| Harness `RUNTIME_ERROR` | 用镜像内 `runtime-smoke.mjs` 检查运行用户依赖权限，再分别检查 Key、模型请求和 MCP 回调 |
| 媒体不可播放 | `/api/v1/video/status`、连接测试、目标白名单、RTC 地址、编码及播放权限 |
| openEuler 镜像导入 `mknod` 失败 | 检查 `container-selinux`、受管程序/数据标签和 Docker 进程域 |

## 独立接入进程

中心 API 与独立 Access Gateway 共用业务存储和协议制品；可单进程运行，也可按以下职责拆分。

### 进程职责

- `cmd/iot-platform` 默认 `IOT_PROCESS_ROLE=combined`，保留单进程入口。
- `IOT_PROCESS_ROLE=api` 不启动 Modbus、Listener 或外部 MQTT 上行订阅；默认仍内嵌解析、业务处理与后台任务（`IOT_API_EMBEDDED_WORKERS=true`）。设为 `false` 后 API 只提供管理接口、运维中心和视频控制，必须同时部署下列 Worker 角色，否则上报不会被解析和处理。
- Worker 角色（同一镜像与入口，设置 `IOT_PROCESS_ROLE`）：

  | 角色 | 运行内容 | 扩容依据 |
  | --- | --- | --- |
  | `parser` | 消费 `iot.raw.message`，解析并发布到内部业务流 `iot.device.business`（及对外 property/event/parsed 主题） | 原文积压、解析耗时 |
  | `processor` | 按设备顺序消费 `iot.device.business`：规则、告警、设备状态、完成标记、outbox 转发 | 业务流积压、数据库等待 |
  | `jobs` | 离线扫描、原文重发、视频媒体重试、凭据吊销重试及批量接入恢复，按任务或资源租约协调执行；设备告警通知按消费组分摊，重复投递由 Alertmanager 去重 | 待执行量 |

  Worker 只开放 `/health/*` 与 `/metrics`；`/health/ready` 只检查本角色依赖，各项并行检查、每项最多 3 秒，返回 `role`、`instance`、各项结果与耗时（`durationsMs`）。Redis 只是热状态缓存与共享限流，故障时读取回退数据库、限流回退按进程配额，因此只把 `checks.cache` 标为 `degraded`、HTTP 仍为 200，不会让负载均衡摘除实例；指标 `readiness_ok{dependency}` 记录每项最近一次结果。指标带 `process_info{role,instance,version}`。所有拆分角色都需要共享 PostgreSQL 与 Kafka；只有 `api`（及 `combined`）需要 `IOT_AI_HARNESS_URL`。告警研判由 API 进程按用户操作运行。
- `IOT_INSTANCE_ID` 为实例名（默认主机名），用于指标、租约所有者和日志。显式设置后 MQTT 持久队列目录变为 `mqtt-inbox/<角色>/<实例>`；同一数据卷上运行同角色多副本时每个副本必须设置不同值。未设置时沿用旧目录，升级不会遗留未确认报文。
- `cmd/iot-access-gateway` 强制 gateway 角色：执行通信、鉴权和 Raw 归档，发布到共享 Kafka；不启动 Raw 业务消费者。HTTP 只开放接入与健康相关路由，管理用户身份在目标接口重新校验。
- api/gateway 两个进程必须配置同一个 PostgreSQL、Kafka 及一致的 Raw 分层存储。协议制品目录也必须共享；不能让两个进程各自使用内存仓库或本地消息总线。
- API 通过 `IOT_ACCESS_GATEWAY_URL` 转发添加设备的预检与保存、标准上报、设备连接详情与命令；Gateway 不可达返回 503，不将请求已发送视为操作成功。

本地运行示例（在仓库根目录，凭据来自各自环境文件）：

```bash
go run ./cmd/iot-platform --env-file .env.api
go run ./cmd/iot-access-gateway --env-file .env.gateway
```

可选容器拆分：`docker compose -p iot-platform-online --env-file .env.online -f compose.yaml -f compose.access.yaml config --quiet` 先检查渲染结果；实际启动再运行相同参数的 `up -d --build`。已有部署须替换为原项目名。覆盖层将 TCP/UDP 端口从 API 移到 Gateway，默认 Gateway HTTP 端口为 8082。覆盖层使用 `!override`，要求 Compose 2.24.4 或更新版本，见 [Docker 合并规则](https://docs.docker.com/reference/compose-file/merge/)。

拆分部署同样使用 DeepSeek API；密钥由 API 的模型管理和 Harness 使用，Gateway 不承担模型推理，也不需要部署对话模型。

### 按设备业务流与跨实例一致性

- 解析结果按 `(租户, 设备)` 键写入内部主题 `iot.device.business`，由 `processor` 消费组按设备顺序处理；不同设备并行。原 `iot.property.report`、`iot.event.report`、`iot.parsed.message` 继续发布供外部订阅（`IOT_PUBLISH_EXTERNAL_TOPICS=false` 可关闭），平台内部不再消费。
- 每条标准消息先原子领取（60 秒租约、领取代次），只有最新代次能写入完成标记；再均衡时新消费者等待旧持有者完成或租约到期，旧实例迟到的完成被拒绝并计入 `standard_claim_fenced_total`。
- 一条没有告警变化的消息只访问 PostgreSQL 三次：领取（新消息写入与领取为同一语句）、一次读取设备状态及是否有活动告警、一条语句同时写入设备状态和完成标记（状态版本冲突时两者都不写，重读后重试）。活动告警数由 `alarm_record` 上的触发器维护在 `device_open_alarm`（迁移 0012），任何告警写入路径都会同步更新。
- 告警确认/恢复/关闭、规则停用、离线扫描、连接状态和设备状态写入都基于行版本做乐观并发，冲突时重读重试（`alarm_conflict_total`、`device_state_conflict_total`），不会用旧快照覆盖新上报。
- 规则与协议缓存在各实例本地保留最多 2 秒，跨实例生效时间以此为上界；权限每次请求读取数据库，撤销立即生效。
- 设备上报 20 条/秒、开放 API 密钥 100 次/秒、登录失败锁定（15 分钟窗口内 10 次）在配置 Redis 后由所有实例共享同一额度（固定时间窗口，窗口按 Unix 时间对齐）；Redis 故障时按 `IOT_CLUSTER_INSTANCES` 退化，并计入 `rate_limit_shared_errors`。
- Redis 支持 Sentinel：设置 `IOT_REDIS_MASTER_NAME` 与 `IOT_REDIS_SENTINELS`（逗号分隔）后跟随主节点切换；设备状态缓存按行版本写入，旧写入不会覆盖新缓存。
- 回放任务记录执行实例与心跳；执行进程退出后，超过 60 秒无心跳的任务在查询时标为 `INTERRUPTED`，不会永久停在运行中。
- MQTT 持久队列目录加进程独占锁，同一目录被第二个进程打开时启动失败，避免两个实例共用一个 client ID。

### 多 API 实例：视频控制与 Harness

- 多个 API 实例都设置 `IOT_NODE_URL`（本实例可被其他实例访问的 HTTP 地址）后，通过 `video/control` 租约选出一个实例运行直播模块（SIP 服务、播放会话、媒体任务与清理）；其他实例把 `/api/v1/video/*`、`/api/v1/integrations/video/*`（含媒体服务器回调与 HLS 鉴权）带原用户凭据转发到持有者，由持有者重新校验。持有者续租失败立即停止模块，租约过期后备用实例接管并从数据库恢复播放会话；现有 SIP 连接与 WebRTC 播放需要设备重新注册、浏览器重新点播。单实例部署不设置该变量时行为不变。
- 媒体服务器回调地址、SIP 端口映射须指向当前持有者或能转发到它的入口；权限变化由持有者每 15 秒复核一次后撤销播放。
- `IOT_AI_HARNESS_URL` 可填多个逗号分隔地址。同一会话按会话 ID 固定路由到同一 Harness 实例（多轮上下文保存在该实例）；该实例不可达时改由下一实例开始新会话。告警研判、巡检、运维报告等业务任务没有对话历史，首选实例繁忙时改用其他空闲实例，全部繁忙才排队等待。任一实例健康即视为可用。
- 模型配置与动态智能体以 PostgreSQL 为准（`ai_model_config`、`ai_workflow_manifest`）。每个 API 实例每 10 秒对账一次：采用其他实例保存的模型配置；Harness 重启后（模型配置只保存在 Harness 进程内存，重启会回到环境变量默认值）按实例 ID 识别并重新下发；动态智能体只同步到部分实例时补齐，删除时保留删除标记，防止漏删的实例把它带回。升级前已存在于 Harness 的动态智能体在首次对账时登记入库。

### 执行所有权

启用 `IOT_ACCESS_COORDINATION=true`，并将 `IOT_ACCESS_NODE_URL` 设置为其他实例可达且精确指向本实例的 HTTP(S) 地址。不要使用随机负载均衡地址冒充固定执行节点。

共享 PostgreSQL 的 `execution_lease` 以租户和 Profile 为资源键。租约 10 秒，节点本地取消期限短于数据库期限，续租失败即取消旧执行；接管提升 fencing token。运行时在归档前检查所有权和当前配置。配置停止被扫描时释放租约。

可确定 Profile 的请求按租约中的节点地址转发，保留原用户授权并限制转发次数。

设备命令（`POST /api/v2/device-access-profiles/:id/devices/:deviceId/commands`）只能由持有该接入点监听与设备会话的进程执行：单副本或单个接入网关时直接在本进程执行；多副本时须开启 `IOT_ACCESS_COORDINATION`，请求按租约转发到持有者。未开启协调而请求落到非持有副本时返回 409 并提示开启协调或改为单副本；已开启协调但没有副本持有该接入点（监听未运行）时返回 503，提示确认接入点已启用后重试。设备级路由依赖明确的实例标签或唯一的设备配置关联，不能据此推断所有历史设备、子设备及任意多副本部署均可自动路由。该实现提供互斥执行与接管，不宣称已有按负载最优调度、跨节点迁移现有 TCP 会话或数据库之外的强制 OS 隔离。

### 验证入口

`go test ./internal/platformapp ./internal/httpapi ./internal/protocolruntime` 覆盖进程职责、认证转发与执行协调；环境相关集成测试的实际执行条件见各测试。源码入口为 `internal/httpapi/process_role.go`、`internal/httpapi/execution_route.go` 和 `internal/protocolruntime/coordinator.go`。

### 管理界面与用户范围

设备模板准备页管理物模型、协议版本与公共连接，单台设备登记时填写专属连接参数；`DeviceAccessProfile` 是软件连接配置，`cmd/iot-access-gateway` 是部署进程。首台验收、批量复用、候选配置更新与回滚见[设备接入](INTEGRATION.md#设备接入)。API 和 Gateway 使用同版代码，转发保留原用户身份，并在目标服务重新校验。

普通用户的全租户接入配置需要全部设备范围和相应菜单/按钮权限。指定设备用户的连接详情不暴露共享网关配置及其他设备会话。用户设备和告警范围见 [用户权限](PLATFORM.md#权限与设备范围)。
