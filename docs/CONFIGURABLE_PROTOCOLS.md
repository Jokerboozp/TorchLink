# 配置驱动协议

协议开发页面现在提供三种不需要新增 Go 代码的解析方式：

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

## Go 源码协议包

配置不足以描述的协议，上传 `.go` 或独立项目 ZIP。平台自动编译并验证样例，再发布不可变版本。纯解析使用 `go-json-lines-v1`；含 TCP/UDP 分帧、设备识别、应答和在线下行的完整接入使用 `go-protocol-v2`。契约见 [Go 协议包接入](GO_PROTOCOL_PACKAGES.md)。

## AI 协议接入助手

“协议接入助手”把协议资料整理成同一条人工确认链路：

1. 在页面上传 PDF、DOCX、XLSX/CSV 点表或直接粘贴点表文本，并填写一条真实样本报文。
2. 对 XLSX 点表，平台在 Go 中还原共享字符串和行列关系，直接生成 `modbus_coil_parser` 地址映射，不调用 AI，也不生成 JavaScript；其他资料才会调用已配置的 AI 模型服务生成 Go 映射草稿。
3. 操作员在“字段映射”表单中修改字段名、线圈地址、数据类型、正常值、报出值和消息类型。
4. 点击“运行解析预览”，平台使用 Go 解析器验证真实 Modbus RTU/TCP 响应；专用协议保存草稿后，另行编写并上传 Go 源码包。
5. 点击“保存协议映射草稿”保留设计资料；实际接入统一到“Go 源码接入”上传代码、验证样例并发布。

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

Excel 点表中的文本仅作为协议资料；平台不会执行其中的脚本、URL 或其他指令。发布仍需要 `operator` 权限，Modbus 映射可先保存再用真实样本校验，专用协议必须上传 Go 源码包并通过样例验证后发布。

## 历史兼容：受限 JavaScript 解析器

以下仅供已有绑定和历史回放参考；新建/更新入口已关闭，请改用 Go 源码包。

对于无法用 JSON 路径或固定字节字段描述的设备，选择
`javascript_sandbox_parser`，在协议开发页面直接编写或载入 `.js` 文件。脚本只
允许定义一个纯函数 `parse(raw)`，不能访问文件、网络、环境变量、数据库或平台
服务；单个脚本最大 64 KiB，执行超时会被终止。保存并发布协议包后，脚本内容随
协议包配置存储，设备报文会在运行中的解析器中立即使用，不需要重新构建或部署
API 镜像。

```javascript
function parse(raw) {
  const bytes = hexToBytes(raw.payload)
  const smoke = (bytes[0] & 1) === 1
  return {
    messageType: smoke ? 'ALARM_REPORT' : 'PROPERTY_REPORT',
    properties: {
      smoke,
      temperature: bytes[1] / 10,
      battery: bytes[2]
    },
    tags: { deviceType: 'smoke' }
  }
}
```

解析函数接收完整原始报文对象，返回 `messageType`、`properties`、`event`、
`tags`、`timestamp` 中需要的字段。可以使用内置的 `hexToBytes(text)` 和
`toInt(text)` 辅助函数。这个扩展点适合平台管理员维护的纯转换脚本；如果要让
不受信任的租户上传代码，建议把脚本执行器拆成独立、无网络、限资源的 worker。
