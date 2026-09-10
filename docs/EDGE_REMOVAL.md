# 旧版本升级与数据兼容

当前平台保留中心 MQTT / HTTP、Modbus TCP、Modbus RTU over TCP、Go TCP / UDP 接入、独立 Access Gateway 与主子设备关系。本文仅用于升级已有部署。

## 现场节点配置

现场 Agent、节点登记与心跳、现场任务、程序升级、RTU 串口驱动、OPC UA / SNMP / BACnet 现场入口、ONVIF 发现及节点视频目录导入已移除。相关路由不再注册，可信协议目录拒绝 `kind=edge-agent`；旧混合目录需去掉程序条目后重新签名。

`DeviceAccessProfile.edgeNodeId` 仅用于识别旧配置。非空配置不会在中心启动采集、监听、注册或下行，也不能重新保存为有效实例。需要改成中心接入时，先确认设备网络可达和产品协议，再重新配置实例；旧串口任务不能仅清空节点标识后继续运行。外部已部署 Agent 和运行目录不会被平台自动停止或删除，需由部署者处理。

## 历史数据与制品

- 新数据库不创建 `edge_node`、`edge_read_job`、`edge_program`、`device_shadow`、`device_shadow_change` 或 `device_twin_topology`。启动迁移不删除旧表与历史数据，当前 API 不再管理这些资源。
- 设备继续使用主子设备关联，`gatewayId` 表示业务主设备，不代表现场节点。设备状态来自成功解析的上报，主设备在线不会将所有子设备设为在线。
- MQTT 的 `IOT_DATA_DIR/mqtt-inbox/<processRole>/` 目录及 `client-id` 原地复用，必须持久保存，不因升级清空或复制给多个活跃实例。
- Go 源码仍可构建多平台制品，但仅发布端实际试跑的结果标记通过；已有不可变版本不被新源码覆盖。旧内置解析器可用于显式绑定和历史回放，不代表已恢复对应现场采集入口。
- 历史协议设备可能仍留有内部凭据索引；TCP / UDP、Modbus 与子设备不再使用这些凭据通过 HTTP / MQTT 认证。

升级须沿用原环境文件、Compose 项目名、数据卷及协议制品目录。设备数据导出不包含完整环境备份；数据库、配置和密钥须分别保管，无需通过清空数据库完成上述升级。
