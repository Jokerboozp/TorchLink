# 一键生成演示数据与功能检查

当前入口：`node scripts/generate-demo-data.mjs`。它编排已有的 `scripts/tests/demo-platform.mjs`，自动准备 Excel/CSV/JSON 样例、执行业务阶段并生成 HTML/JSON 报告。目标地址、租户、演示前缀、TCP/UDP 和 MQTT 地址均可指定；默认前缀每次唯一，名称统一带“演示”。

这是会向目标平台写入数据的脚本；本机测试通过不代表目标服务器已通过。脚本覆盖当前各菜单的主要数据链路，不代表每个按钮和全部异常组合；真实模型、Broker、监听端口或备份依赖不可用时会记录失败，显式跳过的步骤单独列出，不伪造模型回答或成功记录。

## 执行条件

- 在完整源码仓库中运行，需要 Node.js 22.12+ 或兼容的较新版本。MQTT 阶段使用前端现有 `mqtt` 依赖，首次执行前在 `iot_front` 运行 `npm ci`。
- 使用目标租户的内置管理员。密码通过环境变量 `IOT_ADMIN_PASSWORD` 或 `--env-file` 读取；不放在命令行参数或报告中。配置文件按 dotenv 解析，不通过 shell 执行；环境变量优先于文件。
- API、Go 协议编译环境、知识检索、AI/Harness、备份服务按要测试的阶段准备好。模型管理只测试现有配置，不切换全局模型。
- TCP/UDP 端口须空闲、已对运行脚本的机器开放，Docker 部署还须映射到 API 容器中相同端口。默认演示端口为 TCP 29075、UDP 29076；原 Compose 默认仅映射 26875，因此不能直接假定默认演示端口可达。可指定已映射且未被业务网关占用的端口，例如 TCP/UDP 均用26875；若端口在用，请另配映射，或使用 `--skip-sockets`。
- `--skip-sockets` 仍创建停用的演示网关供页面查看，不启用监听、不发送 TCP/UDP 流量。脚本不自动重建你的服务、不改防火墙和端口映射；发现其他已启用网关占用演示端口时会报错。
- 启动 TCP/UDP 模拟器前，脚本还会确认本次演示网关、产品、协议版本、端口一致且状态为 `LISTENING`；无法确认时记录失败并停止该部分测试。

## Linux / CentOS 打包机执行示例

在联网打包机上运行即可通过 HTTP 向服务器生成数据，不要求在部署服务器安装源码与 Node。先准备依赖（从仓库根目录执行）：

```bash
git pull --ff-only origin main
npm --prefix iot_front ci
```

先查看计划，不登录、不写入：

```bash
node scripts/generate-demo-data.mjs \
  --origin http://10.72.234.205:8080 --tenant tenant_001 --dry-run
```

设置目标管理员凭据并执行。下面端口仅在26875已映射且空闲时使用：

```bash
export IOT_ADMIN_USER=admin
read -rsp '目标平台管理员密码: ' IOT_ADMIN_PASSWORD; echo
export IOT_ADMIN_PASSWORD
node scripts/generate-demo-data.mjs \
  --origin http://10.72.234.205:8080 --tenant tenant_001 \
  --mqtt-url mqtt://10.72.234.205:1883 \
  --tcp-port 26875 --udp-port 26875
unset IOT_ADMIN_PASSWORD
```

也可指定本机已有的私有配置文件（不需要复制整个部署配置）：

```bash
node scripts/generate-demo-data.mjs --origin http://10.72.234.205:8080 \
  --env-file /path/to/private-demo.env --tenant tenant_001 --skip-sockets
```

最小文件含 `IOT_ADMIN_USER`、`IOT_ADMIN_PASSWORD`，可另设 `IOT_DEMO_USER_PASSWORD`。如果没有指定文件，兼容读取源码目录 `.env.local`；远程测试应明确使用目标平台的凭据。

CentOS 系统版本无法运行较新 Node 时，可在已具备 `node:22-alpine` 镜像的 Linux Docker 中运行同一脚本。先用该镜像在挂载仓库中安装前端依赖，再执行；密码通过 `-e IOT_ADMIN_PASSWORD` 从宿主环境传入，值不出现在命令文本中：

```bash
sudo docker run --rm --network host -v "$PWD:/workspace" -w /workspace \
  node:22-alpine npm --prefix iot_front ci
sudo --preserve-env=IOT_ADMIN_PASSWORD,IOT_ADMIN_USER docker run --rm --network host \
  -e IOT_ADMIN_PASSWORD -e IOT_ADMIN_USER -v "$PWD:/workspace" -w /workspace \
  node:22-alpine node scripts/generate-demo-data.mjs \
  --origin http://10.72.234.205:8080 --tenant tenant_001 --skip-sockets
```

容器写入挂载目录的样例和报告可能属于 root；Docker 示例需要联网下载安装依赖或本机已有依赖。不要把旧 CentOS 的 Node 运行时不兼容误判为平台接口故障。

## 结果与覆盖范围

结束时输出 `.e2e/<演示前缀>/report.html` 和 `report.json`；`results.json` 保存阶段明细及资源标识。报告根据这一次实际运行生成，不使用旧截图或历史成功描述。非零退出码表示有失败、阶段异常或中断。无需浏览器即可直接打开 HTML 查看结果。

| 阶段 | 涉及功能与可见数据 |
| --- | --- |
| `inspect` | 读取各功能入口；巡检无任务404作为空状态 |
| `business` | HTTP产品和独立设备、8条属性记录、在线状态、限定演示标记的规则、确认/关闭及活动告警、原文下载和DRY_RUN回放、摄像头元数据映射 |
| `protocols` | JSON/CSV生成、预览与发布，Go源码上传编译，产品协议绑定、TCP和UDP接入网关 |
| `mqtt` | MQTT产品与设备、属性/状态上报、仅对演示模拟设备执行命令并验证回执 |
| `children` | 主设备、子设备及归属关系 |
| `services` | 演示知识文档、真实AI流式回答、巡检、FULL备份和文件校验；不会恢复业务数据库 |
| `refine` | Excel点表生成和实际解析断言、TCP/UDP模拟上报及ping回执、现有模型连接测试、内置接入测试四种模板 |
| `access` | 演示只读角色、指定演示设备用户和无设备用户；验证租户、设备、告警/提醒范围及禁止普通用户获取MQTT令牌 |

用户与角色、演示协议、产品、设备、摄像头均保留供页面查看。接入测试使用平台内置测试资源（`reset:false`），这些资源由平台生成标识，不带自定义前缀；备份和知识文档也使用服务端生成ID。内置测试资源可能涉及平台自己的旧测试规则清理逻辑，正式环境需了解该接口行为。AI巡检及FULL备份读取当前租户范围，不局限于演示设备。

两个演示账户密码取 `IOT_DEMO_USER_PASSWORD`；未设置时随机生成且不输出，管理员可在页面重置。重复运行同一前缀会更新该前缀的演示资源、轮换演示设备凭据及演示用户密码，也可能新增协议版本、告警、知识文档和备份记录；不要与同一前缀的持续模拟器同时运行。报告目录与目标地址、租户、前缀绑定，拒绝混用旧格式或其他目标的结果。

模拟器在一键脚本结束时停止，历史记录保留；设备之后可能显示离线。需要持续演示时可单独启动 `scripts/tests/demo-devices.mjs` 或 `demo-mqtt-device.mjs`，并通过环境变量传入相同配置：`IOT_DEMO_ORIGIN`、`IOT_DEMO_ENV_FILE`、`IOT_DEMO_PREFIX`、`IOT_DEMO_TCP_PORT`、`IOT_DEMO_UDP_PORT`、`IOT_DEMO_MQTT_URL`、`IOT_DEMO_MINUTES`。它们最多运行指定时长，也可 Ctrl+C 停止。

## 可选参数

`--skip-ai` 跳过真实AI回答、巡检及模型连接测试；`--skip-backup` 跳过备份任务；`--skip-mqtt` 跳过MQTT阶段；`--skip-sockets` 停用新演示网关并跳过TCP/UDP上报。其余阶段照常运行，报告中保留跳过记录。

`--prefix demo-mytest` 固定资源前缀；`--output /path/to/results` 指定本机结果目录；`--device-host` 指定套接字实际连接的主机（默认平台地址主机）。`--stages` 用逗号选择阶段，顺序按传入值执行；单独执行后续阶段需保留同一前缀/输出目录的前置结果，推荐使用默认完整顺序。

## 开发回归与旧入口

`node --test scripts/tests/demo-data.test.mjs` 检查参数、dry-run、网关归属与监听状态校验、失败报告及凭据脱敏。设置 `IOT_DEMO_TEST_API_EXE` 为本机构建的 API 可执行文件后，还会启动隔离内存仓储 API，跑真实 HTTP/Go编译/TCP/UDP/Excel/权限链路；AI、备份和MQTT真实基础设施不由这个测试覆盖。

2026-09-15 本机隔离回归共5项通过，包含上述真实API链路；目标部署服务器尚未执行本脚本，需以目标运行生成的报告为准。

旧的 `demo-report.mjs`、`demo-report-server.mjs` 和浏览器 `demo-platform-check.mjs` 仍是2026-09-14本机验收辅助脚本，含固定目录和历史描述，不作为本入口的一键报告生成器。查看当前报告请用 `report.html`，不要将旧报告当成本次结果。
