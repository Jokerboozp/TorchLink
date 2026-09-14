# 配置驱动协议

通用 JSON 可使用 `custom_json_parser`；字段名称或字节布局不同的报文可使用以下两种配置映射。变长、会话或厂商专用协议使用 [Go 源码包](GO_PROTOCOL_PACKAGES.md)。

## JSON 路径映射

解析器选择 `configurable_json_parser`，协议标识使用 `json`，载荷格式使用 `json`。配置示例：

```json
{
  "properties": {
    "temperature": {"path": "$.data.temp", "type": "number", "scale": 0.1},
    "smoke": "$.data.smoke"
  },
  "tags": {
    "deviceType": "$.kind"
  },
  "timestampPath": "$.occurredAt",
  "timestampUnit": "s",
  "messageType": "PROPERTY_REPORT"
}
```

字段支持 `number`、`integer`、`boolean`、`string` 和 `json`，未找到的路径可用 `default` 提供默认值。路径是安全的 JSONPath-lite，只读对象字段和数组下标，不执行脚本。

## 固定字段十六进制映射

解析器选择 `configurable_hex_parser`，协议标识使用 `config-hex`，载荷格式使用 `hex`：

```json
{
  "startHex": "AA",
  "endHex": "55",
  "checksum": "sum8",
  "checksumStartOffset": 1,
  "fields": [
    {"name": "temperature", "offset": 1, "length": 2, "type": "int16", "endian": "little", "scale": 0.1}
  ]
}
```

字段偏移从完整报文第 0 字节开始，支持 `uint8`、`int8`、`uint16`、`int16`、`uint32`、`int32`、`float32`、`ascii` 和 `hex`。这适用于固定长度传感器报文；变长、TLV、多信息体和复杂会话协议统一使用上传的 Go 源码包，包括 GB26875。

发布前请在“协议管理 → 解析测试”中输入样本并确认标准消息结果，再发布并在产品管理绑定。

配置不足以描述的协议使用 [Go 源码包](GO_PROTOCOL_PACKAGES.md)，由平台编译、验证样例并发布。

## 从报文或点表生成协议

入口为「协议管理 → 协议生成」，弹窗标题为「生成协议」。选择输入类型后按以下流程操作：

1. 报文支持上传 `.json/.txt/.hex/.bin` 或填写样本；JSON 自动提取路径，HEX 需提供字段偏移、长度、端序等说明，由已配置 AI 辅助生成固定字段映射。样本最大 1 MiB。
2. 点表支持 `.xlsx/.csv`，文件最大 32 MiB，也可粘贴 CSV；平台直接生成 Modbus TCP / RTU 寄存器或线圈映射及读取块，不调用 AI。当前页面不提供 PDF、DOCX 上传。
3. 在「编辑字段映射」中对照输入字段标识、JSON 路径、Modbus 地址或 HEX 偏移；支持新增、删除，展开行调整倍率、字节序等参数。无需直接编辑源码。
4. 用真实样本执行「解析预览」。Modbus 需填写该响应帧对应的零基起始地址。编辑字段后旧预览清除，须重新验证。
5. 「保存协议」创建不可变版本：无样本可保存为 `DRAFT`；样本通过后为 `VALIDATED`，再发布并绑定产品。已保存映射通过「新建版本」修改，不能覆盖原版本。

点表常用列为 `identifier,name,functionCode,address,addressNotation,dataType,scale`。零基地址明确填写 `addressNotation=zero_based`；未声明基准的 `40001` 等传统地址按 Modbus 表区换算。表单中的 Modbus 地址统一从 0 开始。单个报文不能推断完整的变长、会话或厂商协议，这类接入使用 [Go 源码包](GO_PROTOCOL_PACKAGES.md)。

| 接口 | 用途 |
| --- | --- |
| `POST /api/v1/ai/protocol-assistant/generate` | multipart 上传，`inputKind=sample\|point-table`，支持 `file`、`pointTable`、`samplePayload` |
| `POST /api/v1/ai/protocol-assistant/preview` | 未保存映射的解析预览 |
| `POST /api/v1/ai/protocol-assistant/publish` | 沿用历史路径名称，实际保存 v2 草稿或已校验版本 |
| `POST /api/v2/protocols/{id}/releases/{version}/preview` | 校验已保存版本的真实样本 |
| `POST /api/v2/protocols/{id}/releases/{version}/publish` | 发布已经校验的版本 |

协议列表操作栏统一显示解析测试、下载源码、下载制品、发布；无对应能力时禁用并说明原因，权限仍由菜单和操作授权决定。版本号显示在版本区域。真实设备及原始命令联调统一在「接入测试」，没有独立协议调试菜单。

### 消息类型定义

界面显示中文名称，括号内是接口和 Worker 使用的稳定代码：

| 中文名称 | 代码 | 用途 |
| --- | --- | --- |
| 属性上报 | `PROPERTY_REPORT` | 测点、开关量和当前状态值 |
| 事件上报 | `EVENT_REPORT` | 一次性发生的复位、心跳或测试事件 |
| 告警上报 | `ALARM_REPORT` | 设备明确上报的告警事件；无需告警规则即可生成平台告警，规则可额外提供分类、等级或联动动作 |
| 状态变化 | `STATE_CHANGE` | 在线、离线或业务状态变化 |
| 指令应答 | `COMMAND_REPLY` | 设备对平台指令的响应 |
| 日志上报 | `LOG_REPORT` | 运行日志或诊断信息 |

Excel 点表中的文本仅作为协议资料；平台不会执行其中的脚本、URL 或其他指令。保存、预览及发布需要对应菜单和操作授权，详见 [用户权限](USER_ACCESS_CONTROL.md)。Modbus 映射可先保存再用真实样本校验，专用协议必须上传 Go 源码包并通过样例验证后发布。

## 已有协议

旧 JavaScript、厂商及 Modbus 解析器保留用于显式绑定和历史回放。JavaScript 新建/更新入口已关闭，新专用协议使用 Go 源码；Modbus 点表接入按 [统一设备接入](UNIFIED_DEVICE_ONBOARDING.md) 配置。
