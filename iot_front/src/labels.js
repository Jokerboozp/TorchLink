export const alarmTypes = { FIRE_RISK:'火灾风险', FIRE:'火灾告警', SMOKE_DETECTED:'检测到烟雾', FLAME_DETECTED:'检测到火焰', HIGH_TEMPERATURE:'温度过高', DEVICE_FAULT:'设备故障', DEVICE_OFFLINE:'设备离线', WATER_PRESSURE_LOW:'水压过低', WATER_LEVEL_ABNORMAL:'水位异常', ELECTRICAL_FIRE:'电气火灾', GAS_LEAK:'可燃气体泄漏', MANUAL_ALARM:'手动报警' }
export const alarmLevels = { CRITICAL:'紧急', HIGH:'高', MEDIUM:'中', LOW:'低', INFO:'提示' }
export const alarmStatuses = { ACTIVE:'活动中', ACKED:'已确认', RECOVERED:'已恢复', CLOSED:'已关闭', SUPPRESSED:'已抑制' }
export const alarmSources = { device:'设备上报', video:'视频分析', 'managed-device':'受管设备', 'external-ingest':'外部接入', mqtt:'消息队列接入', http:'网页接口接入', manual:'人工录入' }
export const enabledStatuses = { ENABLED:'已启用', DISABLED:'已停用', DRAFT:'草稿', PUBLISHED:'已发布' }
export const deviceRoles = { DIRECT:'直接设备', GATEWAY:'网关设备', CHILD:'子设备' }
export const businessStatuses = { ONLINE:'在线', ALARM:'告警中', OFFLINE:'离线', SUSPECTED_OFFLINE:'疑似离线', NEVER_SEEN:'从未上线', UNKNOWN:'未知' }
export const connectionStatuses = { CONNECTED:'已连接', DISCONNECTED:'未连接', UNKNOWN:'未知' }
export const stateSources = { RAW_MESSAGE:'设备上报', ACTIVE_ALARM:'活动告警', LISTENER_SESSION:'网络会话', RAW_MESSAGE_TIMEOUT:'上报超时' }
export const dataStatuses = { ACTIVE:'数据活跃', SILENT:'数据静默', UNKNOWN:'未知' }
export const backupTypes = { FULL:'设备数据备份', DEVICE_DAILY:'设备数据日备份', INCREMENTAL:'增量备份', RAW_LOGS:'原始日志日备份', RESTORE_DRILL:'文件校验' }
export const backupComponents = { raw:'原始报文', parsed:'解析数据', 'backup-manifest':'备份清单' }
export const backupStatuses = { RUNNING:'执行中', COMPLETED:'已完成', FAILED:'失败' }
export const categories = { smoke:'烟雾探测器', fire:'火灾探测器', water_pressure:'水压传感器', camera:'摄像机', gateway:'边缘网关', sensor:'通用传感器', video_ai:'视频智能分析设备', other:'其他设备' }
export const parsers = { custom_json_parser:'通用结构化数据', configurable_json_parser:'配置驱动结构化数据映射', configurable_hex_parser:'配置驱动十六进制字段', modbus_coil_parser:'工业总线线圈点表', javascript_sandbox_parser:'受限脚本解析器', go_protocol_parser:'可上传的协议程序', gb26875_dahua_parser:'国标消防终端（大华第一版）', fire_smoke_parser:'烟感十六进制', modbus_parser:'工业总线寄存器' }
export const messageTypes = {
  PROPERTY_REPORT: { label:'属性上报', description:'设备测点、开关量和当前状态值' },
  EVENT_REPORT: { label:'事件上报', description:'一次性发生的事件，例如复位、心跳或测试' },
  ALARM_REPORT: { label:'告警上报', description:'设备明确上报的告警事件；无需告警规则即可生成平台告警，规则可额外提供分类、等级或联动动作' },
  STATE_CHANGE: { label:'状态变化', description:'设备在线、离线或业务状态发生变化' },
  COMMAND_REPLY: { label:'指令应答', description:'设备对平台下发指令的响应' },
  LOG_REPORT: { label:'日志上报', description:'设备运行日志或诊断信息' }
}
export const messageTypeLabel = value => messageTypes[String(value ?? '')]?.label || '其他消息'
export const label = (map, value, fallback = '未知') => map[String(value ?? '')] || fallback
export const alarmLevel = value => label(alarmLevels, String(value ?? '').trim().toUpperCase(), '未设置')
export const alarmType = value => label(alarmTypes, value, '其他告警类型')
export function tagType(value) {
  value = String(value ?? '').trim().toUpperCase()
  if (['ONLINE','ENABLED','PUBLISHED','RECOVERED'].includes(value)) return 'success'
  if (['ACTIVE','MEDIUM','ACKED'].includes(value)) return 'warning'
  if (['ALARM','HIGH','CRITICAL'].includes(value)) return 'danger'
  return 'info'
}

export const commandStatuses = { QUEUED:'已排队，等待节点领取', DISPATCHING:'已领取，正在发送或等待应答', SENT:'已发送，执行结果未确认', ACKNOWLEDGED:'已收到协议应答', SUCCEEDED:'执行成功', FAILED:'执行失败', REJECTED:'执行前已拒绝', EXPIRED:'领取前已过期', UNKNOWN:'执行结果未知，不自动重发' }
