// 专业名称保留通用写法，状态和操作说明使用中文；显示名称不用于构造请求或改写原始报文。
export const transportNames = { /* 执行当前语句并推进处理流程。 */
  MQTT:'MQTT', HTTP:'HTTP', MQTT_HTTP:'MQTT / HTTP', TCP:'TCP', UDP:'UDP', TCP_UDP:'TCP / UDP', /* 执行当前语句并推进处理流程。 */
  MODBUS_RTU_TCP:'Modbus RTU over TCP（串口透传）', TCP_CHILD:'通过主设备接入', MODBUS_TCP:'Modbus TCP', MODBUS_RTU:'Modbus RTU', OPC_UA:'OPC UA', SNMP:'SNMP', BACNET:'BACnet', /* 执行当前语句并推进处理流程。 */
  'iot-standard':'标准设备接入', STANDARD_HTTP:'标准 HTTP', STANDARD_MQTT:'标准 MQTT', LISTENER:'网络监听', POLLER:'定时采集' /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
export const formatNames = { JSON:'JSON', HEX:'HEX（十六进制）', BINARY:'Binary（二进制）', TEXT:'Text（纯文本）', BASE64:'Base64' } /* 执行当前语句并推进处理流程。 */
export const aiProviderOptions = [ /* 执行当前语句并推进处理流程。 */
  { id:'ollama', label:'Ollama', description:'连接部署在服务器或本机的 Ollama 模型服务，不需要 API Key。' }, /* 执行当前语句并推进处理流程。 */
  { id:'deepseek', label:'DeepSeek', description:'使用 DeepSeek 云端模型和 API Key。' }, /* 执行当前语句并推进处理流程。 */
  { id:'openai-compatible', label:'OpenAI 兼容 API', description:'连接兼容 OpenAI Chat Completions API 的模型服务。' } /* 执行当前语句并推进处理流程。 */
] /* 结束当前表达式或代码块。 */
export const statusNames = { /* 执行当前语句并推进处理流程。 */
  INDEXED:'已建立索引', INDEXING:'索引建立中', PENDING:'等待处理', PROCESSING:'处理中', FAILED:'处理失败', ERROR:'异常', /* 执行当前语句并推进处理流程。 */
  ENABLED:'已启用', DISABLED:'已停用', ONLINE:'在线', OFFLINE:'离线', WAITING:'等待心跳', LISTENING:'监听中', /* 执行当前语句并推进处理流程。 */
  CONNECTING:'连接中', CONNECTED:'已连接', DISCONNECTED:'未连接', ACTIVE:'活跃', NEVER_SEEN:'尚未上报', ALARM:'告警中', SUSPECTED_OFFLINE:'疑似离线', /* 执行当前语句并推进处理流程。 */
  DRAFT:'草稿', VALIDATED:'已校验', PUBLISHED:'已发布', DEPRECATED:'已弃用', REVOKED:'已撤销', /* 执行当前语句并推进处理流程。 */
  COMPILED:'已编译', UPLOADED:'已上传', RUNNING:'执行中', COMPLETED:'已完成', SUCCESS:'成功', SUCCEEDED:'执行成功', /* 执行当前语句并推进处理流程。 */
  ACCEPTED:'已接收', RECEIVED:'已接收', QUEUED:'已排队', SENT:'已发送', ACKNOWLEDGED:'已应答', REJECTED:'已拒绝', /* 执行当前语句并推进处理流程。 */
  PARSED:'已解析', UNPARSED:'待解析', UNKNOWN:'未知', CANCELED:'已取消', STOPPED:'已停止', READY:'已就绪' /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
export function displayName(map, value, fallback = '未设置') { /* 执行当前语句并推进处理流程。 */
  const text = String(value ?? '').trim() /* 声明 text。 */
  return map[text.toUpperCase()] || map[text] || map[text.toLowerCase()] || (/\p{Script=Han}/u.test(text) ? text : fallback) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
export const transportLabel = value => displayName(transportNames, value, String(value ?? '').trim() || '未设置') /* 执行当前语句并推进处理流程。 */
export const formatLabel = value => displayName(formatNames, value, String(value ?? '').trim() || '未设置') /* 执行当前语句并推进处理流程。 */
export const statusLabel = value => displayName(statusNames, value, '未知状态') /* 执行当前语句并推进处理流程。 */
export function platformLabel(value) { /* 执行当前语句并推进处理流程。 */
  const [os, arch] = String(value || '').split('-') /* 执行当前语句并推进处理流程。 */
  return `${({linux:'Linux',windows:'Windows',darwin:'macOS'})[os] || os || '未设置系统'} · ${arch || '未设置架构'}` /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
export function errorMessage(error) { /* 执行当前语句并推进处理流程。 */
  const raw = String(error?.message || error || '') /* 声明 raw。 */
  if (/\p{Script=Han}/u.test(raw)) return raw /* 判断条件并选择处理分支。 */
  if (error?.name === 'AbortError') return '操作已取消' /* 判断条件并选择处理分支。 */
  if (error instanceof SyntaxError || /JSON|Unexpected token/i.test(raw)) return '数据格式不正确，请检查括号、引号和字段值' /* 判断条件并选择处理分支。 */
  if (/credential reference was not found/i.test(raw)) return '未找到现场连接凭据，请检查节点中的凭据名称' /* 判断条件并选择处理分支。 */
  if (/cycle/i.test(raw)) return '设备关联不能形成循环，请检查设备之间的关系' /* 判断条件并选择处理分支。 */
  if (/fetch|network|load failed|connection/i.test(raw)) return '无法连接服务，请检查网络后重试' /* 判断条件并选择处理分支。 */
  const status = Number(error?.status) /* 声明 status。 */
  return ({400:'提交内容不正确，请检查填写的参数',401:'身份验证失败，请检查账户信息或重新登录',403:'当前账户没有操作权限',404:'未找到请求的记录',409:'数据已变更，请刷新后重试',413:'文件过大，请缩小文件后重试',429:'请求过于频繁，请稍后重试'})[status] || (status >= 500 ? '服务暂时不可用，请稍后重试' : '操作未完成，请检查配置后重试') /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

const capabilityNames = { chat:'对话问答', 'alarm-analysis':'告警研判', 'rule-draft':'规则草稿', 'json-output':'JSON 输出', 'local-model':'本地模型', fallback:'备用响应', 'tool-call':'工具调用', 'tool-calling':'工具调用', knowledge:'知识检索', 'knowledge-retrieval':'知识检索', 'device-query':'设备查询' } /* 声明 capabilityNames。 */
export const capabilityName = value => displayName(capabilityNames, value, '扩展能力') /* 执行当前语句并推进处理流程。 */
const toolNames = { query_system_overview:'查询系统概况', query_device_latest:'查询设备最新状态', query_alarm_list:'查询告警', query_property_history:'查询属性历史', query_similar_alarms:'查询相似告警', query_knowledge_base:'检索知识库', create_rule_draft:'生成规则草稿' } /* 声明 toolNames。 */
export const toolName = value => displayName(toolNames, String(value || '').replace(/^mcp__iot__/, ''), '业务查询工具') /* 执行当前语句并推进处理流程。 */
