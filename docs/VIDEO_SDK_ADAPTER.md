# 摄像头与外部视频事件

平台管理摄像头品牌、名称、点位、建筑、楼层、房间及关联设备。一台摄像头最多关联一个设备，一个设备可以关联多台摄像头。告警返回对应元数据，便于定位现场。这些基础资料始终可用，不依赖直播模块。

直播由可选的独立模块提供，默认关闭：部署后可按 ONVIF 或 RTSP 接入摄像头（含 NVR 通道），在摄像头、设备详情和告警详情中观看。直播配置与加密凭据单独保存，摄像头资料接口和告警摘要不返回直播地址、密码或媒体服务密钥。详见 [摄像头直播](VIDEO_LIVE.md)。平台不做局域网自动发现，也不接入厂商私有云 / SDK。

## 视频告警 Webhook

```http
POST /api/v1/integrations/video/alarm
Content-Type: application/json
X-Video-Platform-ID: video-platform-1
X-Timestamp: <Unix 秒>
X-Signature: <hex(HMAC-SHA256(secret, timestamp + rawBody))>
```

`timestamp` 使用请求头的原始字符串，直接拼接原始请求体字节计算签名，不插入分隔符。生产环境通过 `IOT_VIDEO_PLATFORM_SECRETS` 和 `IOT_VIDEO_PLATFORM_TENANTS` 将外部平台凭据绑定到租户；时间偏差超过五分钟被拒绝，`cameraId` 必须属于该租户且启用。

消息字段以 [VideoAlarmEvent](../internal/model/model.go) 和 [Webhook 处理器](../internal/httpapi/server.go) 为准。事件按 `eventId` 保存并进入视频告警及跨源融合链路，平台补充摄像头与位置元数据，不经过设备 Raw → Parser 链路，也不接触直播流；视频分析告警详情保留事件所属摄像头，直播可用时可直接观看。

MQTT 视频事件沿用 `/external/video/alarm/{tenantId}/{cameraId}`，需明确 `eventId` 并按平台/摄像头范围授权。传输重试保持同一业务 ID，接收保障见 [MQTT 持久接收](DEVICE_RECEIVE_RELIABILITY.md#mqtt-接收保障)。

## 用户权限与告警

摄像头管理属于全租户管理入口，普通用户需设备管理菜单、全部设备范围以及摄像头菜单和对应新增/编辑权限。指定设备用户不开放摄像头管理入口；其有权查看的设备告警仍可携带该设备关联的摄像头摘要。

外部视频告警的写入继续使用平台签名和租户绑定；用户能否读取告警由告警所属设备的范围决定，不因知道摄像头 ID 或告警 ID 获得额外访问权。观看直播另需“观看摄像头直播”权限，规则见 [摄像头直播](VIDEO_LIVE.md#权限与安全) 与 [用户权限](USER_ACCESS_CONTROL.md)。
