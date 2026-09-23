# 消防平台 Go 协议包

这里是从六份旧解析/采集程序迁移出的独立 `go-protocol-v2` 包。每个子目录都可独立运行 `go test ./...`，包含平台函数适配器与本地样例测试；`dist/` 中的六个 ZIP 已上传至当前本机平台并发布。FB2018、FB2024、液压、液位包为 `1.0.0`；KUKA 和水炮包为 `1.0.1`，后续修改须递增版本。

| 目录 / 上传协议标识 | 旧代码 | 网络与设备识别 | 主要数据 |
| --- | --- | --- | --- |
| `fb2018` | `D:\developer\fb_2018` | TCP；源地址 6 字节小端序，设备 ID `fb2018_<十进制>` | 系统/部件/传输装置状态与恢复，部件火警和故障 |
| `fb2024` | `D:\developer\fb_2024` | TCP；源地址 6 字节小端序，设备 ID `fb_<十进制>` | 开关量、模拟量、传输装置状态与恢复 |
| `fb-hydraulic` | `D:\developer\fb_ywyy_hydraulic` | TCP；15 位 IMEI | 压力、电池、信号、阈值、历史值、报警、写参数 |
| `fb-liquid-level` | `D:\developer\fb_ywyy_liquid_level` | TCP；15 位 IMEI | 液位、电池、信号、阈值、历史值、报警、写参数 |
| `kuka-modbus` | `D:\developer\kuka_mqtt` | 主动 TCP；接入网关预配置设备 ID | 六个 Modbus 线圈，火花探测、增压、手动测试、系统运行 |
| `sp-cannon` | `D:\developer\sp_mqtt` | 主动 TCP；接入网关预配置设备 ID | 主机火警/故障/泵，八门水炮状态、故障、模式、水流、阀门、雾状 |

六个包都使用新版标准消息。FB2018、FB2024 的一帧多对象放在 `properties.objects`，避免只取第一对象。FB2018 的系统/部件火警、故障与恢复、FB2024 的传输装置电源故障与恢复放在 `event.components`，这类消息使用平台要求的 `STATE_CHANGE` 类型。FB2024 的开关量、模拟量保留原始类型和值；旧代码没有可靠的火警状态映射，不能把非零值一律当成火警。液压/液位报警与恢复也使用 `event.components`。`kuka-modbus`、`sp-cannon` 对每个查询点输出独立部件状态；旧程序的固定设备 ID、MQTT 发布、用户名密码、目标 IP 不写进协议包，由平台产品、设备、接入网关配置管理。

## 接入网关配置

- FB2018、FB2024、液压、液位：配置 `network=tcp`、`connectionMode=listen`，设备侧把上报地址指向平台监听端口。按产品分别创建实例；不同协议不能共用同一监听端口。液压/液位会校验 RTU CRC，支持先带 IMEI 再连续发送多个 RTU 帧以及后续裸 RTU 上报。下行参数为旧命令名：`setDetectionTime`、`setChangeAlarmValue`、`setUploadTime`、`setOffset` 的 `value`，或 `setMultipleParams` 的 `collectionTime`、`alarmLowerLimit`、`alarmUpperLimit`。数值沿用旧代码的原始寄存器单位（压力/液位及阈值均为百分之一单位）。
- KUKA：配置 `network=tcp`、`connectionMode=dial`、设备目标地址/端口及预先登记的设备 ID。`queries` 添加六条不同的 `type`：`coil-0`、`coil-3001`、`coil-3002`、`coil-3003`、`coil-3013`、`coil-3042`，周期按现场要求配置。旧程序使用 Modbus TCP 线圈功能码 `0x01`、站号 1。
- 水炮：同样配置主动连接及预登记设备 ID。`queries` 添加 `type=host`，以及 `fault-1`～`fault-8`、`status-1`～`status-8` 共 17 条不同类型。旧程序使用 Modbus TCP 输入寄存器功能码 `0x04`、站号 1。

注意：KUKA 和水炮旧程序是短连接逐点轮询；新版接入网关保持一个 TCP 会话并串行查询，设备必须允许保持连接。Modbus TCP 响应不含设备唯一标识，所以这两个包要求每个主动连接实例绑定已登记设备，并按现场网络白名单限制目标地址。FB2018 旧程序曾以远端 IP 生成设备 ID；新版函数契约未提供远端 IP，现使用报文源地址。若现场多台 FB2018 都发全零源地址，会得到同一个 ID，必须先给设备配置唯一源地址或使用独立主动连接实例的预配置设备 ID。FB2024 全零源地址也有相同限制。

旧 JetLinks 自动生成子设备台账的方式没有原样迁移。这批包把部件作为控制器下的稳定 `components`；如需每个部件单独成为可授权的设备，应另行按新版平台主子设备协议和 `childProducts` 配置。六个包已用本机虚拟设备完成平台链路测试，结果在 `verification-20260923.json`；现场设备的真实报文和网络连通仍需现场联调。

## 本地验证与打包

在 PowerShell 中对任一子目录运行：

```powershell
cd D:\iot\platform\dev\fb2018
go test ./...
Compress-Archive -Path .\* -DestinationPath ..\dist\fb2018.zip -Force
```

其它五个目录替换名称即可。压缩包里应直接包含 `go.mod`、`protocol.go`、`zz_platform.go`、测试文件；平台构建不执行 `_test.go`，会独立编译并运行 `Samples` 与 `Operations`。旧样本数据来自对应旧项目测试，协议包没有第三方依赖。

`verify-platform.mjs` 使用平台根目录的 `.env.local` 登录本机平台，在 `D:\iot\platform` 中运行。`upload` 发布 ZIP，`setup` 创建六个虚拟测试产品与接入实例，`listen-test` 测试四个 TCP 上报协议，`dial-test` 启动两个虚拟 Modbus 服务器并测试主动轮询。测试结束后六个测试接入实例均停用；产品、设备、协议发布和解析记录保留，便于平台界面复核。运行 `setup` 和 `listen-test` 前需启用相应监听实例。
