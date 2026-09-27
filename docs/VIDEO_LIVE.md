# 摄像头直播模块

摄像头基础资料（品牌、名称、位置、设备关联）始终可用；直播由可选的独立模块提供，默认关闭。模块未部署、未启用或媒体服务故障时，摄像头的录入、编辑、删除、设备关联、告警中的摄像头信息以及外部视频告警接收都不受影响，API 的启动和 `/health/ready` 也不依赖媒体服务。

## 架构与职责

| 组成 | 位置 | 职责 |
|---|---|---|
| 媒体服务 | `deploy/zlmediakit/`，Compose 服务 `zlmediakit`（profile `video`） | ZLMediaKit 开源版：按需拉取摄像头 RTSP，转为 WebRTC / HLS，可选调用 FFmpeg 转码；只在内部网络开放 HTTP API、Hook 与 HLS |
| 直播业务 | `internal/video/` | 直播配置、ONVIF / RTSP 连接测试、目标地址校验、凭据加密、播放会话、媒体任务生命周期与对账、Hook 处理 |
| HTTP 接口 | `internal/httpapi/video.go` | 模块状态与开关、直播配置、播放会话、WHEP 信令转发、Hook、HLS 鉴权 |
| 存储 | `video_module_state`、`video_camera_live_config`、`video_camera_credential`、`video_play_session`（PostgreSQL；内存仓储对应实现） | 模块开关、每台摄像头的直播配置、加密凭据和播放会话分别保存 |
| 前端 | `LivePlayer.vue`、`LivePlayerDialog.vue`、`CameraLiveConfig.vue`、`LinkedCameras.vue`、`liveVideo.js` | 摄像头管理、设备详情、告警详情与规则联动 `OPEN_CAMERA` 共用同一个播放器 |
| 反向代理 | `iot_front/nginx.conf`（生产）、`iot_front/vite.config.js`（开发） | `/media/hls/` 的每个请求先经 API 校验播放凭证再转给媒体服务 |

视频数据只在摄像头、媒体服务和浏览器之间传输；Go API 只转发 SDP 文本并管理会话，不搬运视频，也不执行转码。

### 固定版本

| 组件 | 版本 |
|---|---|
| ZLMediaKit | 官方镜像 `zlmediakit/zlmediakit:master@sha256:48d1b3c264dd6537f9dcec12fb16b541ed6c9dc5a54322c328c3135fec5c71c8`（源码 revision `10268396b89748bc2b0d19dda79bc29219e97815`，2026-09-22 构建）。该仓库在 Docker Hub 只发布滚动的 `master` 标签，因此按 digest 固定；升级时须重新核对 API、Hook 与配置项并重跑本页验证 |
| FFmpeg | 随上述镜像提供的 6.1.1（Ubuntu 24.04 构建，含 libx264、libx265、libopus） |
| hls.js | 1.7.3（前端锁文件固定，仅在需要 HLS 时按需加载） |

### 转协议与转码

- **转协议**：RTSP → WebRTC / HLS，由媒体服务完成，不改变音视频编码。H.264 摄像头通常只需转协议。
- **转码**：重新编码（如 H.265 → H.264、调整分辨率和码率、音频转 AAC），由 FFmpeg 完成，消耗 CPU。只有转码开关打开且摄像头策略允许时才会发生。

转协议不能解决浏览器不支持的编码：H.265 在多数浏览器的 HLS 中不可用，WebRTC 是否支持 H.265 取决于浏览器和硬件，由播放器实际探测（`RTCRtpReceiver.getCapabilities`），不按浏览器名称猜测。

## 开关语义

| 状态（`/api/v1/video/status`） | 含义 |
|---|---|
| `not_deployed` 未部署 | API 未配置 `IOT_VIDEO_MEDIA_API_URL` |
| `misconfigured` 部署配置无效 | 已配置但密钥、加密密钥或地址不合规；API 照常启动，只有直播不可用 |
| `disabled` 已部署但关闭 | 媒体服务已部署，平台开关关闭 |
| `enabled` 启用且正常 | 平台开关打开，媒体服务健康 |
| `degraded` 启用但媒体服务异常 | 平台开关打开，但媒体服务不可达 |

- **部署层**：Compose profile `video` 与 `IOT_VIDEO_*` 配置，由 `scripts/video-module.sh` / `video-module.ps1` 的 `enable` / `disable` 管理。`disable` 停止并移除媒体容器，保留密钥、加密凭据和直播配置。
- **业务层**：平台内置管理员在“摄像头映射”页的“摄像头直播”开关。它是全平台开关（媒体服务被所有租户共用），租户内的受管用户不能操作。关闭时服务端拒绝新播放，撤销全部会话（关闭 WebRTC 连接、HLS 凭证失效），删除全部拉流和转码任务，保留配置；空闲的媒体容器继续运行，完全释放用部署层 `disable`。API 不挂载 Docker socket，不控制容器。
- **单摄像头**：直播配置中的“启用该摄像头直播”。关闭后该摄像头不再可播放。
- **转码**：部署层 `IOT_VIDEO_TRANSCODE_ENABLED` 决定是否允许转码；每台摄像头的转码策略为“关闭 / 自动 / 指定兼容输出”。

## 接入步骤

1. 部署媒体服务（见“部署”）。
2. 平台内置管理员登录，在“摄像头映射”页打开“摄像头直播”开关。
3. 在摄像头行点击“直播配置”：
   - **ONVIF**：填写设备地址、ONVIF 端口、账号密码 →“查询媒体配置”→ 选择主码流 / 子码流 → 连接测试 → 保存。保存时服务端重新向设备查询流地址，浏览器提交的地址不会被采用；设备返回的 RTSP 地址同样要通过目标地址校验，内嵌的账号密码会被丢弃。
   - **RTSP**：选择品牌模板（海康威视、大华、宇视）填写地址、端口和通道号，或选择“通用 RTSP / 手动填写流地址”→ 连接测试 → 保存。连接 NVR 时填写 NVR 地址和摄像头所在通道号。
4. 连接测试结果区分：地址未通过校验、地址不可达、认证失败、通道或码流不存在、设备应答异常、媒体服务不可用、媒体服务取流失败、编码不兼容、需转码播放、可播放。只有媒体服务实际收到可解码的视频帧才报告“可播放”；浏览器第一次真正出画面后，列表状态以此为准。
5. 有观看权限的用户可在摄像头列表“观看”、设备详情“关联摄像头”、告警详情“关联摄像头”中播放；规则联动 `OPEN_CAMERA` 在直播可用且有权观看时直接打开播放器，否则保留原有的资料定位。

品牌模板只是生成常见地址的辅助，不代表所有型号都兼容。只支持厂商 App、P2P 私有云而没有 ONVIF / RTSP 的设备无法接入，测试会如实提示。第一版不做跨网段自动发现或局域网扫描。

### 品牌模板

| 品牌 | 主码流 | 子码流 |
|---|---|---|
| 海康威视 | `/Streaming/Channels/{通道}01` | `/Streaming/Channels/{通道}02` |
| 大华 | `/cam/realmonitor?channel={通道}&subtype=0` | `subtype=1` |
| 宇视 | `/unicast/c{通道}/s0/live` | `/unicast/c{通道}/s1/live` |

账号密码不会拼进 URL：拉流时作为媒体服务的独立参数（`rtsp_user` / `rtsp_pwd`）传递，含 `@ : / # %` 等特殊字符的密码无需转义。

## 播放与生命周期

- 播放会话由服务端创建，流标识为 `HMAC(租户, 摄像头, 码流)`，浏览器不能指定上游地址或流名；同租户同摄像头同码流共享一个上游拉流，同一输出规格共享一个转码任务，并发的首次播放只创建一次任务。
- 会话是短期租约（默认 45 秒），播放器约每 15 秒心跳续期；每次心跳都重新校验账户、会话版本、观看权限、设备范围、摄像头状态、直播配置与模块开关，任一不满足即撤销。服务端每 15 秒还会整体复核一次，用户、角色或设备范围被修改后立即复核本租户会话。会话最长不超过登录令牌有效期（且不超过 12 小时）。
- 最后一位观看者离开（主动关闭、页面刷新或关闭、浏览器崩溃、断网后租约到期）后，经过宽限期（默认 20 秒，外加最多 5 秒检查间隔）释放拉流和转码；不依赖浏览器发送停止请求。FFmpeg 读取源流不计为观看者，转码任务不会因此永久保活。
- 拉流失败按 5 秒起指数退避（上限 2 分钟），媒体服务对单个拉流最多自动重连 3 次，摄像头离线时不会无限快速重连。
- API 重启后从数据库恢复未到期会话、接管仍在运行的任务，并删除平台不认识的拉流和转码（孤儿）；媒体服务重启（`on_server_started`）后任务标记为丢失，下一次心跳按退避重建，播放器随之重连（最多 3 次）。
- Hook 不参与计数：`on_stream_none_reader` 永远回复不关闭，由平台决定释放；重复、延迟或乱序的 Hook 不会破坏状态。
- 撤销时：HLS 凭证立即失效；WebRTC 通过 WHEP 返回的删除地址在媒体服务上关闭连接，不依赖令牌过期。

### 播放器

- 默认静音，自动播放被浏览器拦截时显示“开始播放”。
- 先用 WebRTC；10 秒内无画面，或播放中画面冻结 8 秒，切换一次到 HLS，不在两种协议间循环；服务端拒绝（权限、配置、编码、上限）不会触发协议切换。
- 支持主 / 子码流切换、重试、全屏；关闭弹窗、切换摄像头、退出登录或令牌失效时销毁连接并释放会话，旧请求的晚到结果会被丢弃。
- HLS 由 hls.js 播放（Chrome、Edge、Firefox），不支持 MSE 的浏览器使用原生 HLS（iOS Safari）。播放凭证只在内存中使用，不写入浏览器存储。

### 转码策略

| 策略 | 行为 |
|---|---|
| 关闭 | 只播放兼容码流；H.265 在不支持的浏览器上给出明确提示 |
| 自动 | 只根据**视频**编码和播放端能力判断：H.265 且浏览器所用协议不支持时转为 H.264；管理员声明“源含 B 帧”时 WebRTC 转码。网络失败不会被当作需要转码 |
| 指定兼容输出 | 使用管理员选定的输出规格 |

输出规格（FFmpeg 命令模板在 `deploy/zlmediakit/config.ini.tpl` 的 `[ffmpeg] iot_*`，由服务端固定，用户不能提交命令、滤镜或参数）：

| 规格 | 输出 |
|---|---|
| H.264 1080p / 720p / 480p | libx264，无 B 帧、单 slice，最高 1920×1080 / 1280×720 / 854×480，25 / 25 / 15 fps，约 3 / 1.5 / 0.8 Mbps；有音频时转 AAC 64 kbps |
| 视频直通 + 音频转 AAC | 视频不重新编码，仅音频转 AAC（HLS 播放 G.711 音频用） |

平台启动转码后会核对媒体服务实际运行的命令包含预期的编码参数（`-c:v libx264` / `-c:a aac`）；ZLMediaKit 在模板缺失时会静默改用默认命令，此时平台会停止该任务并报错。转码并发受 `IOT_VIDEO_TRANSCODE_MAX` 限制，达到上限返回“转码任务数量已达上限”，不会无限派生进程。`IOT_VIDEO_TRANSCODE_HWACCEL` 为硬件加速预留，当前只接受 `none`：容器不会自动获得宿主机 GPU，需要专门的镜像和设备映射，尚未验证。

### 音频

播放默认静音。音频按源编码透传，不会因为音频自动转码：WebRTC 支持 G.711（PCMA/PCMU）与 Opus，不支持 AAC；HLS 支持 AAC，不支持 G.711。播放器下方会说明当前协议下的音频情况。需要在 HLS 下听到 G.711 摄像头的声音时，管理员可指定“视频直通 + 音频转 AAC”。

## 权限与安全

- **管理**：摄像头管理沿用全租户约束（设备管理菜单、全部设备范围、摄像头菜单）；直播配置、连接测试和 ONVIF 查询是摄像头菜单下的独立操作权限（“配置摄像头直播”“直播连接测试”“查询 ONVIF 媒体配置”）。
- **观看**：设备管理菜单下的“观看摄像头直播”（`POST /api/v1/video/cameras/:id/play-sessions`），需单独授予；心跳、WHEP、停止属于同一权限，不单独出现在权限目录中。
  - 摄像头关联了设备：用户的设备范围必须包含该设备。指定设备范围的用户只能从有权访问的设备或告警进入，不会因此看到摄像头目录或其他设备的直播。
  - 摄像头未关联设备：只有全部设备范围且拥有摄像头菜单的用户可以观看。
  - 知道 cameraId、流标识、会话号或播放 URL 都不构成授权；跨租户一律拒绝。
- **媒体入口**：HLS 播放列表和分片都经 nginx `auth_request`（开发时为 Vite 中间件）逐请求校验路径中的播放凭证；WebRTC 信令经带登录令牌的 API 转发，媒体服务的 `on_play` 再次校验。媒体服务不接受任何外部推流（`on_publish` 只允许本机 FFmpeg 输出）。
- **Hook**：地址中携带 `IOT_VIDEO_HOOK_SECRET`，并校验 `mediaServerId`；媒体服务的内部令牌只接受来自本机回环地址的请求。
- **凭据**：摄像头账号密码使用 AES-256-GCM 加密，租户和摄像头作为附加数据绑定，密钥来自 `IOT_VIDEO_CREDENTIAL_KEY`（无默认值）。密码只写不读：表单留空保留原密码，勾选“清除已保存的密码”才删除。资料接口、告警摘要、日志、错误和审计都不包含密码、带凭据的地址或媒体服务密钥；媒体服务错误信息会去除 URL 中的账号密码和令牌。
- **目标地址**：取流和 ONVIF 只允许 `IOT_VIDEO_ALLOWED_CIDRS` 网段与 `IOT_VIDEO_ALLOWED_PORTS` 端口。主机名解析出的所有地址都必须在允许范围内，连接和交给媒体服务的地址都固定为已校验的 IP（防 DNS 重绑定）；ONVIF 不跟随重定向；云元数据、链路本地、组播、未指定地址始终拒绝；不提供任意 URL 代理。
- **删除与变更**：删除摄像头在同一事务中删除直播配置与加密凭据，并撤销会话、停止任务；修改关联设备或停用摄像头会结束其播放，修改名称和位置不影响直播配置和播放。

## 配置

| 变量 | 默认 | 说明 |
|---|---|---|
| `IOT_VIDEO_MEDIA_API_URL` | 空 | 媒体服务 API；为空即“未部署” |
| `IOT_VIDEO_MEDIA_SECRET` / `IOT_VIDEO_HOOK_SECRET` | 无 | 至少 24 个随机字符，`enable` 首次生成 |
| `IOT_VIDEO_CREDENTIAL_KEY` | 无 | 32 字节 base64；生成后不要更换，否则已保存的摄像头密码需重新填写 |
| `IOT_VIDEO_MEDIA_SERVER_ID` | `torchlink-media-1` | 第一版单节点；直播配置的 `mediaNodeId` 为 `default` |
| `IOT_VIDEO_RTC_EXTERN_IP` / `IOT_VIDEO_RTC_PORT` | 空 / `8000` | 浏览器访问 WebRTC 媒体的地址与 UDP/TCP 端口 |
| `IOT_VIDEO_ALLOWED_CIDRS` / `IOT_VIDEO_ALLOWED_PORTS` | 私网三段 / 常用摄像头端口 | 摄像头网段与端口白名单 |
| `IOT_VIDEO_TRANSCODE_ENABLED` / `IOT_VIDEO_TRANSCODE_MAX` / `IOT_VIDEO_TRANSCODE_THREADS` | `false` / `2` / `2` | 转码开关、并发上限、每任务编码线程 |
| `IOT_VIDEO_MAX_SESSIONS` / `IOT_VIDEO_MAX_SOURCE_STREAMS` | `64` / `32` | 会话与拉流上限 |
| `IOT_VIDEO_SESSION_LEASE` / `IOT_VIDEO_IDLE_GRACE` / `IOT_VIDEO_START_TIMEOUT` | `45s` / `20s` / `15s` | 租约、释放宽限期、取流等待 |
| `IOT_VIDEO_HLS_PUBLIC_PATH` | `/media/hls` | 浏览器访问 HLS 的路径前缀 |
| `IOT_VIDEO_MEDIA_CPUS` / `IOT_VIDEO_MEDIA_MEMORY` / `IOT_VIDEO_HLS_TMPFS_SIZE` | `2` / `2g`（本地 `1g`） / `512m` | 媒体容器资源限制 |

## 部署

本地源码调试统一通过 `setup-local.sh --video on|off` 开关媒体服务，PowerShell 对应 `setup-local.ps1 -Video on|off`；虚拟机全部依赖模式同时传 `--dependencies-only`。OrbStack 命令、远程地址和转码参数见 [本地调试部署](DEPLOYMENT.md#orbstack-虚拟机本地调试)。切换后重启本机 API，首次开启后仍需管理员在摄像头页打开业务直播开关。

在线部署（仓库根目录，Linux / macOS）：

```bash
bash scripts/video-module.sh enable --rtc-ip <浏览器可访问的服务器 IP> --transcode
```

Windows：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\video-module.ps1 enable -RtcIp <服务器 IP> -Transcode
```

查看状态、日志与停用：

```bash
bash scripts/video-module.sh status
bash scripts/video-module.sh logs
bash scripts/video-module.sh disable
```

`enable` 写入 `.env.online` 的 `IOT_VIDEO_*` 与 `COMPOSE_PROFILES=…,video`，构建固定版本镜像、启动媒体服务并重建 `platform-api`；之后重复执行 `deploy-online` 会保持启用。本地源码调试使用 `--mode local`（写入 `.env.local`，媒体服务 API 在 `127.0.0.1:18580`，需重启本地 `go run` 的 API）；离线部署见 [离线部署](OFFLINE_DEPLOYMENT.md#摄像头直播可选)。

### 网络要求

| 链路 | 要求 |
|---|---|
| 媒体服务 → 摄像头 / NVR | 容器内能访问摄像头 RTSP（默认 554）端口；媒体服务使用 RTP over TCP。平台 API 也需访问摄像头的 RTSP / ONVIF 端口（连接测试与 ONVIF 查询由 API 发起） |
| 浏览器 → 媒体服务 WebRTC | `IOT_VIDEO_RTC_EXTERN_IP:IOT_VIDEO_RTC_PORT` 的 UDP 与 TCP 均需可达；地址必须是浏览器看到的服务器地址，不能是容器内地址。容器 HTTP 端口不对外发布 |
| 浏览器 → HLS | 与前端同源的 `/media/hls/`，由 `platform-web` 的 nginx 转发；前面再有 HTTPS 反向代理时，需把 `/media/hls/` 原样转给 `platform-web`，并关闭该路径的缓存 |
| 媒体服务 → API Hook | 容器网络内 `http://platform-api:8080/api/v1/video/hooks/*` |

HTTPS 反向代理只承载信令（API）和 HLS，**不承载** WebRTC 媒体；浏览器与媒体服务之间的 UDP/TCP 端口不通时，WebRTC 无法出画面，播放器会改用 HLS。跨 NAT 或复杂网络需要 TURN：媒体服务内置 TURN 默认关闭，第一版未配置和验证 TURN。不要把摄像头或媒体服务 HTTP 端口暴露到公网，不要把开发机 IP 写成所有环境的固定地址。

### 资源

实测（2026-09-27，Apple M1 Pro，OrbStack arm64 Linux 虚拟机，合成测试图案，数值为 `docker stats` 单核百分比）：媒体服务空闲约 36 MiB；一路 H.264 720p 直接拉流（转协议）约 1.6% CPU；再加一路 H.265 720p → H.264 720p 转码约 20–28% CPU、内存增加约 250 MiB（FFmpeg 常驻约 110 MiB，另含 HLS 内存盘）。真实摄像头画面更复杂、分辨率更高时消耗会明显增加，并发路数须在目标服务器上按实际摄像头压测后确定，本文不承诺支持多少路。

## 已验证范围

2026-09-26 至 2026-09-27，在开发机（macOS，OrbStack Docker，Google Chrome 153 与 Claude 内置浏览器）上，使用上述固定版本的真实 ZLMediaKit 与 FFmpeg，以及另一个 ZLMediaKit 实例 + FFmpeg 生成的合成 RTSP 摄像头（Digest 认证，密码含特殊字符）联调：

| 项目 | 结果 |
|---|---|
| H.264 720p（G.711A 音频）直接播放 | Chrome WebRTC 持续出画面；HLS（hls.js）按实时推进 |
| H.265 → H.264 720p 转码播放 | Chrome WebRTC 与 HLS 均出画面；运行命令确认使用 libx264 |
| H.265 直接播放 | 该 Chrome 报告支持 WebRTC H.265，直接播放出画面 |
| 音频 | WebRTC 解码 PCMA（有音频能量）；HLS 解码转码输出的 AAC |
| 设备详情、告警详情、摄像头列表入口 | 真实 Chrome 中播放、关闭后释放会话 |
| 直播配置连接测试 | 地址校验、不可达、认证失败、通道不存在、编码不兼容、需转码、可播放均按预期区分 |
| 生产 nginx HLS 鉴权 | 凭证正确可取播放列表与分片；伪造凭证 403；撤销后播放列表与分片均 403；未部署媒体服务时前端照常启动 |
| 生命周期 | 并发首播共享一个拉流；宽限期释放；租约到期回收；模块关闭清空任务；API 重启恢复会话并清理孤儿；媒体服务重启后心跳重建 |
| 部署脚本 | `video-module.sh` 本地模式真实启停；Bash 部署冒烟覆盖离线打包、模块开关与在线 profile；`video-module.ps1` 在 PowerShell 7.4 容器中模拟 Docker 验证 |

联调中发现并已处理：`rtsp.directProxy=1` 时，参数集只在 SDP 中下发的源（如 FFmpeg 推流）在 Chrome WebRTC 中首个 GOP 之后停止解码，现改为 `directProxy=0`（重新封包、每个 IDR 前带 SPS/PPS，不重新编码）；x264 `zerolatency` 默认多 slice 会导致 WebRTC 帧率异常，模板已固定单 slice。

自动化测试：`internal/video`（凭据、目标地址、品牌模板、RTSP DESCRIBE 分类、ONVIF 摘要认证与流地址校验、转码判断、生命周期、Hook、HLS 鉴权）使用模拟媒体服务；`internal/httpapi/video_live_test.go` 覆盖跨租户、设备范围、权限撤销、摄像头删除与重新关联；`iot_front/tests/live-video.test.mjs`；真实浏览器检查脚本 `iot_front/tests/browser/camera-live-check.mjs`（需要运行中的 API、前端和媒体服务）。

ONVIF 仅在模拟服务上验证（WS-Security 摘要、HTTP Digest 回退、流地址校验），未连接真实 ONVIF 设备。

## 待实机验证

- 海康威视：IPC（如 DS-2CD 系列）主 / 子码流、H.265 / H.265+、NVR（如 DS-7600 系列）按通道取流、ONVIF 查询。
- 大华：IPC（如 IPC-HFW 系列）`realmonitor` 路径、NVR 通道、ONVIF。
- 宇视：IPC（如 IPC2 系列）`unicast` 路径与老型号 `/media/video1`、NVR 通道、ONVIF。
- 各型号的 G.711 / AAC 音频、B 帧设置、多 slice 编码与 WebRTC 兼容性。
- 目标服务器上的 WebRTC 端口可达性、HTTPS 反向代理下的 HLS、跨网段 / NAT 与 TURN、真实画面下的转码并发与资源占用。
- 非 Chrome 浏览器（Firefox、Edge、Safari / iOS）的 WebRTC 与 HLS 实际播放。

## 不在本次范围

录像存储与回放、云台控制、语音对讲、视频 AI、大规模监控墙、厂商私有云 / SDK 覆盖、完整 GB28181（注册、目录、点播信令）。ZLMediaKit 能接收国标媒体不代表平台已具备 GB28181 接入。

## 停用与排查

| 现象 | 排查 |
|---|---|
| 页面显示“未部署” | `IOT_VIDEO_MEDIA_API_URL` 为空，或 API 未重启加载新配置；运行 `video-module status` |
| “部署配置无效” | API 日志 `camera live module configuration is invalid`：检查密钥长度、`IOT_VIDEO_CREDENTIAL_KEY` 是否为 32 字节 base64 |
| “媒体服务异常” | `video-module status` / `logs`；媒体容器健康检查；API 能否访问 `IOT_VIDEO_MEDIA_API_URL` |
| 连接测试“地址未通过校验” | 摄像头地址与端口加入 `IOT_VIDEO_ALLOWED_CIDRS` / `IOT_VIDEO_ALLOWED_PORTS` |
| 测试可播放但 WebRTC 不出画面、自动改用 HLS | 浏览器到 `IOT_VIDEO_RTC_EXTERN_IP:IOT_VIDEO_RTC_PORT` 的 UDP/TCP 是否可达；地址是否为浏览器看到的服务器地址 |
| HLS 403 | 播放会话已结束（权限变更、关闭、模块关闭）；重新打开播放器 |
| HLS 502 | `platform-web` 无法解析或访问 `zlmediakit`（媒体服务未部署或未运行） |
| 转码失败 / 达到上限 | `IOT_VIDEO_TRANSCODE_ENABLED`、`IOT_VIDEO_TRANSCODE_MAX`；媒体服务日志中的 FFmpeg 输出 |
| 摄像头密码“无法解密” | `IOT_VIDEO_CREDENTIAL_KEY` 被更换；恢复原密钥或重新填写密码 |

媒体服务日志和 `listStreamProxy` 输出可能包含摄像头地址，按敏感信息处理；摄像头密码不会出现在拉流 URL 中。
