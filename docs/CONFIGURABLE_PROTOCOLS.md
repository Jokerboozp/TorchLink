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

发布前请在“解析调试”中输入样本并确认标准消息结果，再将协议包发布并绑定到产品。

配置不足以描述的协议使用 [Go 源码包](GO_PROTOCOL_PACKAGES.md)，由平台编译、验证样例并发布。

## AI 协议接入助手

“协议接入助手”把协议资料整理成同一条人工确认链路：

1. 在页面上传 PDF、DOCX、XLSX/CSV 点表或直接粘贴点表文本，并填写一条真实样本报文。
2. 对 XLSX 点表，平台在 Go 中还原共享字符串和行列关系，直接生成 `modbus_coil_parser` 地址映射，不调用 AI，也不生成 JavaScript；其他资料才会调用已配置的 AI 模型服务生成 Go 映射草稿。
3. 操作员在“字段映射”表单中修改字段名、线圈地址、数据类型、正常值、报出值和消息类型。
4. 点击“运行解析预览”，平台使用 Go 解析器验证真实 Modbus RTU/TCP 响应；专用协议保存草稿后，另行编写并上传 Go 源码包。
5. 点击“保存协议映射草稿”保留设计资料；实际接入统一到“设备接入 → 源码接入”上传代码、验证样例并发布。

对应 API 为：

- `POST /api/v1/ai/protocol-assistant/generate`（`multipart/form-data`，字段 `file`、`pointTable`、`samplePayload` 等）
- `POST /api/v1/ai/protocol-assistant/preview`
- `POST /api/v1/ai/protocol-assistant/publish`

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

Excel 点表中的文本仅作为协议资料；平台不会执行其中的脚本、URL 或其他指令。保存映射草稿仍需要 `operator` 权限，Modbus 映射可先保存再用真实样本校验，专用协议必须上传 Go 源码包并通过样例验证后发布。

## 已有协议

旧 JavaScript、厂商及 Modbus 解析器保留用于显式绑定和历史回放。JavaScript 新建/更新入口已关闭，新专用协议使用 Go 源码；Modbus 点表接入按 [统一设备接入](UNIFIED_DEVICE_ONBOARDING.md) 配置。
