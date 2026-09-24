import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
import { readdir, readFile } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { join } from 'node:path' /* 引入当前代码需要的依赖。 */
import test from 'node:test' /* 引入当前代码需要的依赖。 */

const root = new URL('..', import.meta.url) /* 声明 root。 */

async function appSource() { /* 定义 appSource 函数。 */
  return (await Promise.all(['src/App.vue', 'src/pageGuide.js'].map(path => readFile(new URL(path, root), 'utf8')))).join('\n') /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

async function sourceText(directory = new URL('src/', root)) { /* 定义 sourceText 函数。 */
  const entries = await readdir(directory, { withFileTypes: true }) /* 声明 entries。 */
  const contents = await Promise.all(entries.map(async entry => { /* 声明 contents。 */
    const path = new URL(entry.name + (entry.isDirectory() ? '/' : ''), directory) /* 声明 path。 */
    return entry.isDirectory() ? sourceText(path) : readFile(path, 'utf8') /* 返回当前处理结果。 */
  })) /* 结束当前表达式或代码块。 */
  return contents.flat().join('\n') /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

test('management controls and Chinese labels remain available', async () => { /* 执行当前语句并推进处理流程。 */
  const source = await sourceText() /* 声明 source。 */
  for (const label of ['查看详情', '批量下载', '未注册设备', '一键注册', '摄像头映射', '保存规则', '火灾风险', '紧急', '活动中', '告警中', '疑似离线']) { /* 循环处理当前数据。 */
    assert.match(source, new RegExp(label), `missing label: ${label}`) /* 验证实际结果符合预期。 */
  } /* 结束当前表达式或代码块。 */
  const labels = await import('../src/labels.js') /* 声明 labels。 */
  assert.equal(labels.alarmLevel('CRITICAL'), '紧急') /* 验证实际结果符合预期。 */
  assert.equal(labels.alarmLevel('high'), '高') /* 验证实际结果符合预期。 */
  assert.equal(labels.alarmLevel(''), '未设置') /* 验证实际结果符合预期。 */
  const alarmsView = await readFile(new URL('src/views/AlarmsView.vue', root), 'utf8') /* 声明 alarmsView。 */
  assert.match(alarmsView, /alarmLevel\(analysis\.riskLevel\)/) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('account logout is available from the top-right avatar menu', async () => { /* 执行当前语句并推进处理流程。 */
  const app = await appSource() /* 声明 app。 */
  assert.match(app, /<ui-dropdown class="account-dropdown"/) /* 确认账户菜单已经使用 Naive UI。 */
  assert.match(app, /aria-label="打开用户菜单"/) /* 验证实际结果符合预期。 */
  assert.match(app, /command="logout"/) /* 验证实际结果符合预期。 */
  assert.match(app, /function handleAccountCommand\(command\)/) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(app, /class="logout-button"/) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('login form never exposes a built-in password', async () => { /* 执行当前语句并推进处理流程。 */
  const app = await appSource() /* 声明 app。 */
  assert.match(app, /loginForm = ref\(\{ tenantId: 'tenant_001', username: 'admin', password: '' \}\)/) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(app, /password:\s*'admin123'/) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('shared controls keep file pickers and text actions visibly shaped', async () => { /* 执行当前语句并推进处理流程。 */
  const styles = await readFile(new URL('src/styles.css', root), 'utf8') /* 声明 styles。 */
  const assistant = await readFile(new URL('src/views/ProtocolAssistantView.vue', root), 'utf8') /* 声明 assistant。 */
  const protocols = await readFile(new URL('src/views/ProtocolsView.vue', root), 'utf8') /* 声明 protocols。 */
  assert.match(styles, /\.n-button\.el-button\.n-button--text \{[^}]*border: 1px solid var\(--border\)/) /* 验证 Naive UI 文本按钮有可见轮廓。 */
  assert.match(styles, /input\[type="file"\]::file-selector-button/) /* 验证实际结果符合预期。 */
  assert.match(styles, /\.table-actions \.el-button, \.el-table \.el-button \{[^}]*min-height: 28px/) /* 表格操作维持紧凑尺寸。 */
  assert.match(`${assistant}\n${protocols}`, /<FilePicker/) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('global alarm popup handles raised alarms, fault events and tenant-scoped settings', async () => { /* 执行当前语句并推进处理流程。 */
  const app = await appSource() /* 声明 app。 */
  const popup = await readFile(new URL('src/components/GlobalAlertPopup.vue', root), 'utf8') /* 声明 popup。 */
  const alerts = await import('../src/globalAlert.js') /* 声明 alerts。 */

  assert.match(app, /GlobalAlertPopup/) /* 验证实际结果符合预期。 */
  for (const label of ['告警提醒', '显示报警弹窗', '弹窗静默时段', '播放警报声', '查看告警详情', '查看原始报文', '设备名称', '报警内容', '报警类型', '报警时间', '报警等级']) { /* 循环处理当前数据。 */
    assert.match(popup, new RegExp(label), `missing global alarm UI label: ${label}`) /* 验证实际结果符合预期。 */
  } /* 结束当前表达式或代码块。 */
  assert.match(popup, /function alertContent\(item\)/) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(popup, /global-alert-identifiers/) /* 验证实际结果符合预期。 */
  for (const technicalLabel of ['设备 ID', '告警编号', '消息 ID']) assert.doesNotMatch(popup, new RegExp(technicalLabel), `technical alarm identifier should not be shown: ${technicalLabel}`) /* 循环处理当前数据。 */
  assert.match(popup, /window\.addEventListener\('iot:realtime'/) /* 验证实际结果符合预期。 */

  const raised = alerts.parseRealtimeAlert( /* 声明 raised。 */
    '/iot/alarm/raised/city-1/district-1/building-1/smoke/device-1', /* 执行当前语句并推进处理流程。 */
    JSON.stringify({ alarmId:'alarm-1', triggerId:'message-1', deviceId:'device-1', deviceName:'东区烟感', alarmType:'FIRE_RISK', alarmLevel:'CRITICAL', source:'device', lastTriggeredAt:1760000000000 }) /* 执行当前语句并推进处理流程。 */
  ) /* 结束当前表达式或代码块。 */
  assert.equal(raised.kind, 'alarm') /* 验证实际结果符合预期。 */
  assert.equal(raised.alarmId, 'alarm-1') /* 验证实际结果符合预期。 */
  assert.equal(raised.deviceName, '东区烟感') /* 验证实际结果符合预期。 */
  assert.equal(raised.detail, '检测到设备异常报警，请及时处理。') /* 验证实际结果符合预期。 */
  assert.deepEqual(alerts.alertKeys(raised), ['alarm-1', 'message-1']) /* 验证实际结果符合预期。 */
  assert.equal(alerts.parseRealtimeAlert('/iot/parsed/tenant-a/product-a/device-1/ALARM_REPORT', { messageId:'message-1', messageType:'ALARM_REPORT', deviceId:'device-1' }), null) /* 正式告警由 raised 事件通知，解析消息不再重复弹窗。 */

  const fault = alerts.parseRealtimeAlert( /* 声明 fault。 */
    '/iot/parsed/tenant-a/product-a/device-1/EVENT_REPORT', /* 执行当前语句并推进处理流程。 */
    { messageId:'message-fault', rawMessageId:'raw-message-fault', messageType:'EVENT_REPORT', deviceId:'device-1', event:{ type:'FAULT', description:'主电源故障' } } /* 执行当前语句并推进处理流程。 */
  ) /* 结束当前表达式或代码块。 */
  assert.equal(fault.kind, 'fault') /* 验证实际结果符合预期。 */
  assert.equal(fault.messageId, 'raw-message-fault') /* 验证实际结果符合预期。 */
  assert.equal(fault.alarmType, 'DEVICE_FAULT') /* 验证实际结果符合预期。 */
  assert.equal(fault.detail, '主电源故障') /* 验证实际结果符合预期。 */
  assert.equal(alerts.parseRealtimeAlert('/iot/parsed/tenant-a/product-a/device-1/EVENT_REPORT', { messageType:'EVENT_REPORT', event:{ type:'HEARTBEAT' } }), null) /* 验证实际结果符合预期。 */

  const values = new Map() /* 声明 values。 */
  const storage = { getItem:key => values.get(key) ?? null, setItem:(key, value) => values.set(key, value) } /* 声明 storage。 */
  const saved = alerts.saveAlertSettings(storage, { tenant:'tenant-a', user:'operator' }, { popupEnabled:false, soundEnabled:false, quietStart:'22:00', quietEnd:'07:00' }) /* 声明 saved。 */
  assert.equal(alerts.loadAlertSettings(storage, { tenant:'tenant-a', user:'operator' }).popupEnabled, false) /* 验证实际结果符合预期。 */
  assert.equal(alerts.loadAlertSettings(storage, { tenant:'tenant-b', user:'operator' }).popupEnabled, true) /* 验证实际结果符合预期。 */
  assert.equal(alerts.isWithinQuietHours(new Date(2026, 0, 1, 23, 30), saved), true) /* 验证实际结果符合预期。 */
  assert.equal(alerts.isWithinQuietHours(new Date(2026, 0, 1, 12, 0), saved), false) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('long dialogs keep the viewport fixed and scroll within the dialog body', async () => { /* 执行当前语句并推进处理流程。 */
  const styles = await readFile(new URL('src/styles.css', root), 'utf8') /* 声明 styles。 */
  const dialogStyle = styles.match(/\.el-dialog\s*\{[^}]+\}/)?.[0] /* 声明 dialogStyle。 */
  const dialogBodyStyle = styles.match(/\.el-dialog__body\s*\{[^}]+\}/)?.[0] /* 声明 dialogBodyStyle。 */
  assert.ok(dialogStyle, 'dialog layout style must remain explicit') /* 验证实际结果符合预期。 */
  assert.ok(dialogBodyStyle, 'dialog body layout style must remain explicit') /* 验证实际结果符合预期。 */
  assert.match(styles, /\.main-content\s*\{[^}]*flex:\s*1 1 auto[^}]*overflow-y:\s*auto/) /* 验证实际结果符合预期。 */
  assert.match(styles, /\.el-overlay-dialog\s*\{[^}]*overflow:\s*hidden/) /* 验证实际结果符合预期。 */
  assert.match(dialogStyle, /display:\s*flex/) /* 验证实际结果符合预期。 */
  assert.match(dialogStyle, /flex-direction:\s*column/) /* 验证实际结果符合预期。 */
  assert.match(dialogStyle, /max-height:\s*calc\(100vh\s*-\s*48px\)/) /* 验证实际结果符合预期。 */
  assert.match(dialogBodyStyle, /min-height:\s*0/) /* 验证实际结果符合预期。 */
  assert.match(dialogBodyStyle, /overflow-y:\s*auto/) /* 验证实际结果符合预期。 */
  assert.match(dialogBodyStyle, /overscroll-behavior:\s*contain/) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('camera metadata and AI workflow playground remain available', async () => { /* 执行当前语句并推进处理流程。 */
  const source = await sourceText() /* 声明 source。 */
  for (const label of ['摄像头点位', '不解析、拉取或预览视频流', '智能助手', '运行轨迹', '工具调用']) { /* 循环处理当前数据。 */
    assert.match(source, new RegExp(label), `missing feature label: ${label}`) /* 验证实际结果符合预期。 */
  } /* 结束当前表达式或代码块。 */
  const cameraView = await readFile(new URL('src/views/CameraMappingsView.vue', root), 'utf8') /* 声明 cameraView。 */
  assert.doesNotMatch(cameraView, /VideoStreamPlayer|autoPreview|openPreview|streamUrl|hls\.js/) /* 验证实际结果符合预期。 */
  assert.match(source, /\.provider-select-row\s*\{[^}]*width:100%;[^}]*min-width:0;/, '模型选择行应填满表单内容区域') /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('knowledge management uploads files and lists tenant documents', async () => { /* 执行当前语句并推进处理流程。 */
  const app = await appSource() /* 声明 app。 */
  const view = await readFile(new URL('src/views/KnowledgeView.vue', root), 'utf8') /* 声明 view。 */
  for (const label of ['知识库', '上传知识文档', '上传并建立索引', '已上传文档', '打开智能助手', '知识文档详情与切片', '索引与切片规则', '字符范围', '切片内容', '向量化', '知识库策略', '每次强制检索', '最低相似度', '无匹配知识时', '保存知识库策略']) { /* 循环处理当前数据。 */
    assert.match(`${app}\n${view}`, new RegExp(label), `missing knowledge UI label: ${label}`) /* 验证实际结果符合预期。 */
  } /* 结束当前表达式或代码块。 */
  assert.match(view, /api\((?:'|`)[^'`]*\/api\/v1\/knowledge\/documents(?:\?|['`])/) /* 验证实际结果符合预期。 */
  assert.match(view, /method:'POST', body:form/) /* 验证实际结果符合预期。 */
  assert.match(view, /new FormData\(\)/) /* 验证实际结果符合预期。 */
  assert.match(view, /persistentIndex/) /* 验证实际结果符合预期。 */
  assert.match(view, /api\((?:'|`)[^'`]*\/api\/v1\/ai\/workflows(?:\?|['`])/) /* 验证实际结果符合预期。 */
  assert.match(view, /form\.append\('workflowId', workflowId\.value\)/) /* 验证实际结果符合预期。 */
  assert.match(view, /<ui-select v-model="workflowId"/) /* 确认知识库选择器已经迁移。 */
  assert.match(view, /api\(`\/api\/v1\/knowledge\/documents\/\$\{encodeURIComponent\(document\.id\)\}`\)/) /* 验证实际结果符合预期。 */
  assert.match(view, /固定窗口 \+ 重叠/) /* 验证实际结果符合预期。 */
  assert.match(view, /row\.startChar/) /* 验证实际结果符合预期。 */
  assert.match(view, /row\.overlapChars/) /* 验证实际结果符合预期。 */
  assert.match(view, /row\.vectorized/) /* 验证实际结果符合预期。 */
  assert.match(view, /knowledge-binding/) /* 验证实际结果符合预期。 */
  assert.match(view, /function loadBinding\(\)/) /* 验证实际结果符合预期。 */
  assert.match(view, /function saveBinding\(\)/) /* 验证实际结果符合预期。 */
  for (const label of ['知识分类', '知识标签', '告警处置操作规程']) assert.match(view, new RegExp(label), `missing knowledge metadata UI: ${label}`) /* 循环处理当前数据。 */
}) /* 结束当前表达式或代码块。 */

test('knowledge statistic cards use readable foreground colors', async () => { /* 执行当前语句并推进处理流程。 */
  const view = await readFile(new URL('src/views/KnowledgeView.vue', root), 'utf8') /* 声明 view。 */
  const statsStyle = view.match(/\.knowledge-stats span,\.knowledge-stats small \{[^}]+\}/)?.[0] /* 声明 statsStyle。 */
  assert.ok(statsStyle, 'knowledge statistic label style must remain explicit') /* 验证实际结果符合预期。 */
  assert.match(statsStyle, /color:var\(--muted-foreground\)/) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(statsStyle, /color:var\(--muted\)/) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('AI workbench uses cancellable SSE workflows and stable message keys', async () => { /* 执行当前语句并推进处理流程。 */
  const aiView = await readFile(new URL('src/views/AiView.vue', root), 'utf8') /* 声明 aiView。 */
  const apiSource = await readFile(new URL('src/api.js', root), 'utf8') /* 声明 apiSource。 */
  const sseSource = await readFile(new URL('src/sse.js', root), 'utf8') /* 声明 sseSource。 */

  assert.match(aiView, /api\((?:'|`)[^'`]*\/api\/v1\/ai\/workflows(?:\?|['`])/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /runtimeRequestSequence/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /requestSequence !== runtimeRequestSequence/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /workflowManageRequestSequence/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /loadWorkflowManagement\(true\)/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /apiStream\('\/api\/v1\/ai\/chat\/stream'/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /new AbortController\(\)/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /:key="message\.id"/) /* 验证实际结果符合预期。 */
  for (const field of ['question','conversationId','workflowId','model','maxTokens']) assert.match(aiView, new RegExp(`\\b${field}\\b`), `missing AI request field: ${field}`) /* 循环处理当前数据。 */
  for (const eventType of ['run.started','text.delta','tool.started','tool.completed','run.completed','run.failed']) assert.ok(aiView.includes(`'${eventType}'`), `missing stream event: ${eventType}`) /* 循环处理当前数据。 */
  assert.doesNotMatch(aiView, /event\.reasoning/) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(aiView, /conversationId\.value\s*=\s*event\.conversationId/) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(aiView, /model:[^\n]*runtime\.value\.active/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /conversationId\.value\s*=\s*makeId\('conversation'\)/) /* 验证实际结果符合预期。 */
  for (const label of ['智能体插件管理', '新建智能体', '智能体配置清单', '校验并创建智能体', '保存后智能体会立即进入工作流列表']) assert.match(aiView, new RegExp(label), `missing dynamic Agent UI: ${label}`) /* 循环处理当前数据。 */
  for (const field of ['schemaVersion','id','name','description','version','enabled','persona','defaultModel','maxTokens','capabilities','allowedTools']) assert.match(aiView, new RegExp(`name:'${field}'`), `missing Agent field documentation: ${field}`) /* 循环处理当前数据。 */
  assert.match(aiView, /结构化数据标准不支持注释/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /允许使用的工具/) /* 验证实际结果符合预期。 */
  for (const label of ['工作流插件', '智能体管理']) assert.match(aiView, new RegExp(label), `missing workflow control: ${label}`) /* 循环处理当前数据。 */
  assert.match(aiView, /class="chat-workflow-select"/) /* 工作流切换控件位于对话顶部。 */
  assert.doesNotMatch(aiView, /class="surface-card control-card"|controlsExpanded/) /* 不再占用单独的运行侧栏。 */
  assert.doesNotMatch(aiView, /runConfig\.maxTokens|label="最大输出词元"/) /* 最大输出词元统一在模型管理配置。 */
  assert.doesNotMatch(aiView, /<div class="control-section-label"><span>04<\/span>运行环境/) /* 左侧不重复展示顶部模型状态。 */
  assert.match(aiView, /<ui-drawer v-model="managementVisible" title="智能体管理"/) /* 确认智能体管理抽屉已经迁移。 */
  assert.doesNotMatch(aiView, /<ui-menu/) /* 管理抽屉不引入嵌套菜单。 */
  assert.doesNotMatch(aiView, /Provider 测试/) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(aiView, /panel-knowledge|panel-provider/) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(aiView, /<ui-collapse/) /* 管理抽屉不隐藏主要内容。 */
  assert.match(aiView, /method:'PUT'/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /api\('\/api\/v1\/ai\/workflows', \{ method:'POST'/) /* 验证实际结果符合预期。 */
  for (const marker of ['/api/v1/ai/workflows/admin', "method:'DELETE'", '工作流插件管理', '已配置的工作流插件', '内置只读', '启用', '禁用', '删除']) { /* 循环处理当前数据。 */
    assert.match(aiView, new RegExp(marker.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')), `missing workflow management marker: ${marker}`) /* 验证实际结果符合预期。 */
  } /* 结束当前表达式或代码块。 */
  for (const marker of ['agentPreviewVisible', 'agentPreviewJson', 'viewAgent', '查看内置智能体', '内置智能体配置清单（只读）']) { /* 循环处理当前数据。 */
    assert.match(aiView, new RegExp(marker.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')), `missing built-in Agent preview marker: ${marker}`) /* 验证实际结果符合预期。 */
  } /* 结束当前表达式或代码块。 */
  for (const workflowId of ['alarm-handler', 'device-health-inspector', 'protocol-assistant']) { /* 循环处理当前数据。 */
    assert.match(aiView, new RegExp(workflowId), `missing non-chat workflow classification: ${workflowId}`) /* 验证实际结果符合预期。 */
  } /* 结束当前表达式或代码块。 */
  assert.match(aiView, /const nonChatWorkflowIds = new Set\(\[/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /isChatWorkflow\(item\)/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /value\.items\.filter\(isChatWorkflow\)/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /@click="viewAgent\(item\)"/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /@click="startCreateAgent"/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /function startCreateAgent\(\)/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /agentEditorVisible\.value = true/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /function openAgentManagement\(\)/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /agentEditorRef/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /@click="openAgentManagement"/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /method:'PUT'/) /* 验证实际结果符合预期。 */
  assert.match(apiSource, /export async function apiStream/) /* 验证实际结果符合预期。 */
  assert.match(apiSource, /request\.cache = 'no-store'/) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(aiView, /providerProfileStorageKey|testPlugin|sandbox\.provider/) /* 验证实际结果符合预期。 */
  assert.match(apiSource, /text\/event-stream/) /* 验证实际结果符合预期。 */
  assert.match(sseSource, /getReader\(\)/) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('AI streaming keeps Markdown rendering and scroll work bounded', async () => { /* 执行当前语句并推进处理流程。 */
  const aiView = await readFile(new URL('src/views/AiView.vue', root), 'utf8') /* 声明 aiView。 */

  assert.match(aiView, /queueAssistantText/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /flushAssistantText/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /<MarkdownContent v-if="message\.text && message\.role === 'assistant' && message\.status !== 'streaming'"/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /<p v-else-if="message\.text">\{\{ message\.text \}\}<\/p>/) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(aiView, /behavior:'smooth'/) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('AI chat sizes sent question bubbles to content until the readable max width', async () => { /* 执行当前语句并推进处理流程。 */
  const aiView = await readFile(new URL('src/views/AiView.vue', root), 'utf8') /* 声明 aiView。 */
  const messageContentStyle = aiView.match(/\.message-content \{[^}]+\}/)?.[0] /* 声明 messageContentStyle。 */
  const userContentStyle = aiView.match(/\.message-row\.user \.message-content \{[^}]+\}/)?.[0] /* 声明 userContentStyle。 */
  const userMessageStyle = aiView.match(/\.message-row\.user \.chat-message \{[^}]+\}/)?.[0] /* 声明 userMessageStyle。 */
  assert.ok(messageContentStyle, 'AI message content width must remain explicit') /* 验证实际结果符合预期。 */
  assert.ok(userContentStyle, 'AI user message content width must remain explicit') /* 验证实际结果符合预期。 */
  assert.ok(userMessageStyle, 'AI user chat message width must remain explicit') /* 验证实际结果符合预期。 */
  assert.match(messageContentStyle, /width:100%/) /* 验证实际结果符合预期。 */
  assert.match(userContentStyle, /width:fit-content/) /* 验证实际结果符合预期。 */
  assert.match(userContentStyle, /max-width:min\(82%,760px\)/) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(userContentStyle, /width:100%/) /* 验证实际结果符合预期。 */
  assert.match(userMessageStyle, /width:fit-content/) /* 验证实际结果符合预期。 */
  assert.match(userMessageStyle, /max-width:100%/) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('AI answers render safe Markdown in chat and health inspection', async () => { /* 执行当前语句并推进处理流程。 */
  const markdown = await import('../src/markdown.js') /* 声明 markdown。 */
  const html = markdown.renderMarkdown('# 标题\n\n- **重点**\n\n`code`') /* 声明 html。 */
  assert.match(html, /<h1>标题<\/h1>/) /* 验证实际结果符合预期。 */
  assert.match(html, /<ul>[\s\S]*<strong>重点<\/strong>[\s\S]*<\/ul>/) /* 验证实际结果符合预期。 */
  assert.match(html, /<code>code<\/code>/) /* 验证实际结果符合预期。 */
  const unsafeHtml = markdown.renderMarkdown('<script>alert(1)</script>') /* 声明 unsafeHtml。 */
  assert.match(unsafeHtml, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(unsafeHtml, /<script>/) /* 验证实际结果符合预期。 */

  const aiView = await readFile(new URL('src/views/AiView.vue', root), 'utf8') /* 声明 aiView。 */
  const inspectionView = await readFile(new URL('src/views/HealthInspectionView.vue', root), 'utf8') /* 声明 inspectionView。 */
  assert.match(aiView, /<MarkdownContent[^>]*:source="message\.text"/) /* 验证实际结果符合预期。 */
  assert.match(inspectionView, /<MarkdownContent[^>]*:source="report\.aiAdvice"/) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('health inspection report survives menu-driven view recreation and stays tenant scoped', async () => { /* 执行当前语句并推进处理流程。 */
  const { HEALTH_INSPECTION_STORAGE_PREFIX, healthInspectionStorageKey, saveHealthInspection, loadHealthInspection } = await import('../src/healthInspectionState.js') /* 执行当前语句并推进处理流程。 */
  const values = new Map() /* 声明 values。 */
  const storage = { getItem:key => values.get(key) ?? null, setItem:(key,value) => values.set(key,value), removeItem:key => values.delete(key) } /* 声明 storage。 */
  const session = { tenant:'tenant-a', user:'alice' } /* 声明 session。 */
  const report = { generatedAt:1760000000000, summary:'巡检完成', counts:{ total:3, healthy:2 }, items:[], aiAdvice:'需要复核一台设备。' } /* 声明 report。 */
  assert.equal(saveHealthInspection(storage, session, report), true) /* 验证实际结果符合预期。 */
  assert.ok([...values.keys()][0].startsWith(HEALTH_INSPECTION_STORAGE_PREFIX)) /* 验证实际结果符合预期。 */
  assert.deepEqual(loadHealthInspection(storage, session), report) /* 验证实际结果符合预期。 */
  assert.equal(loadHealthInspection(storage, { tenant:'tenant-b', user:'alice' }), null) /* 验证实际结果符合预期。 */
  assert.equal(loadHealthInspection(storage, { tenant:'tenant-a', user:'bob' }), null) /* 验证实际结果符合预期。 */
  values.set(healthInspectionStorageKey(session), '{invalid') /* 执行当前语句并推进处理流程。 */
  assert.equal(loadHealthInspection(storage, session), null) /* 验证实际结果符合预期。 */
  assert.equal(values.has(healthInspectionStorageKey(session)), false) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('AI model administration has its own menu and business overview', async () => { /* 执行当前语句并推进处理流程。 */
  const app = await appSource() /* 声明 app。 */
  const aiView = await readFile(new URL('src/views/AiView.vue', root), 'utf8') /* 声明 aiView。 */
  const providerView = await readFile(new URL('src/views/AiProvidersView.vue', root), 'utf8') /* 声明 providerView。 */
  const knowledgeView = await readFile(new URL('src/views/KnowledgeView.vue', root), 'utf8') /* 声明 knowledgeView。 */
  assert.match(app, /knowledge: \{ \.\.\.pageGuide.knowledge/) /* 验证实际结果符合预期。 */
  assert.match(app, /aiProviders: \{ \.\.\.pageGuide.aiProviders/) /* 验证实际结果符合预期。 */
  assert.match(app, /items: \['aiProviders', 'ai', 'knowledge'\]/) /* 验证实际结果符合预期。 */
  assert.match(app, /allowedPages = new Set\(\[[^\]]*'aiProviders'/s) /* 验证实际结果符合预期。 */
  assert.match(aiView, /<ui-dialog v-model="agentEditorVisible" :title="editingAgentId \? '编辑智能体' : '新建智能体'"/) /* 确认智能体编辑弹窗已经迁移。 */
  assert.match(aiView, /function cancelAgentEditor\(\)/) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(aiView, /Provider 测试|连接并测试插件|\/api\/v1\/ai\/providers\/test/) /* 验证实际结果符合预期。 */
  assert.match(aiView, /providerLabel\(runtime\.config\?\.provider \|\| runtime\.active\?\.id\)/) /* 顶部保留当前模型状态。 */
  assert.doesNotMatch(aiView, /providerForm|saveProviderConfig|\/api\/v1\/ai\/providers\/config/) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(aiView, /<ui-menu-item index="knowledge"|<ui-menu-item index="provider"/) /* 不恢复旧的嵌套菜单。 */
  for (const label of ['连接并启用模型服务', '模型服务配置', '选择模型来源', '填写连接信息', '设置模型与输出', '智能业务能力', '可用模型服务', '测试配置', '应用配置', '智能告警研判', '智能巡检']) { /* 循环处理当前数据。 */
    assert.match(providerView, new RegExp(label), `missing AI Provider management label: ${label}`) /* 验证实际结果符合预期。 */
  } /* 结束当前表达式或代码块。 */
  assert.match(providerView, /label="最大输出词元"/)
  for (const label of ['AI Provider', 'Provider 配置', '可用 Provider', '测试并应用']) { /* 循环处理当前数据。 */
    assert.doesNotMatch(providerView, new RegExp(label), `English Provider wording should not be visible: ${label}`) /* 验证实际结果符合预期。 */
  } /* 结束当前表达式或代码块。 */
  assert.match(providerView, /\/api\/v1\/ai\/providers\?page=1&pageSize=100/) /* 验证实际结果符合预期。 */
  assert.match(providerView, /\/api\/v1\/ai\/providers\/test/) /* 验证实际结果符合预期。 */
  assert.match(providerView, /\/api\/v1\/ai\/providers\/config/) /* 验证实际结果符合预期。 */
  assert.match(providerView, /function testProviderConfig\(\)/) /* 验证实际结果符合预期。 */
  assert.match(providerView, /function applyProviderConfig\(\)/) /* 验证实际结果符合预期。 */
  assert.match(providerView, /:disabled="!canApply"/) /* 验证实际结果符合预期。 */
  assert.match(providerView, /测试只验证连接，点击“应用配置”后才会生效/) /* 验证测试和启用的区别有明确说明。 */
  assert.match(providerView, /所有智能功能立即生效/) /* 验证实际结果符合预期。 */
  assert.match(knowledgeView, /知识库策略/) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('protocol v2 point-table, package release and device collection flows are visible', async () => { /* 执行当前语句并推进处理流程。 */
  const protocols = await readFile(new URL('src/views/ProtocolsView.vue', root), 'utf8') /* 声明 protocols。 */
  const raw = await readFile(new URL('src/views/RawView.vue', root), 'utf8') /* 声明 raw。 */
  const devices = await readFile(new URL('src/views/DevicesView.vue', root), 'utf8') /* 声明 devices。 */
  const app = await appSource() /* 声明 app。 */
  for (const label of ['新建平台连接配置', '上传源码', '版本', '平台连接配置', '连接测试']) assert.match(protocols, new RegExp(label), `missing label: ${label}`) /* 循环处理当前数据。 */
  for (const route of ['/api/v2/protocols', '/api/v2/device-access-profiles']) assert.match(protocols, new RegExp(route.replaceAll('/', '\\/'))) /* 循环处理当前数据。 */
  assert.match(protocols, /模板绑定协议/) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(protocols, /commandOpen|下行命令/) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(protocols, /go-json-lines-v1|source\.cases|旧协议包/) /* 验证实际结果符合预期。 */
  assert.match(app, /label: '设备接入'/) /* 验证实际结果符合预期。 */
  assert.match(app, /integration: \{ \.\.\.pageGuide.integration/) /* 验证实际结果符合预期。 */
  assert.match(raw, /standardMessage/) /* 验证实际结果符合预期。 */
    assert.match(devices, /hasReported\(row\)/) /* 验证实际结果符合预期。 */
  assert.match(devices, /查看数据/) /* 验证实际结果符合预期。 */
  assert.match(devices, /连接详情/) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(devices, /轮换凭证|连接指南/) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('test device workbench provisions a fixture and sends editable data and alarm templates', async () => { /* 执行当前语句并推进处理流程。 */
  const app = await appSource() /* 声明 app。 */
  const view = await readFile(new URL('src/views/TestDeviceView.vue', root), 'utf8') /* 声明 view。 */
  for (const label of ['测试设备', '发送正常数据', '发送报警数据', '发送恢复数据', '报文模板', '建议测试顺序']) { /* 循环处理当前数据。 */
    assert.match(`${app}\n${view}`, new RegExp(label), `missing test device label: ${label}`) /* 验证实际结果符合预期。 */
  } /* 结束当前表达式或代码块。 */
  assert.match(app, /testDevice/) /* 验证实际结果符合预期。 */
  assert.match(app, /component: TestDeviceView/) /* 验证实际结果符合预期。 */
  assert.match(view, /api\('\/api\/v1\/test-devices\/provision'/) /* 验证实际结果符合预期。 */
  assert.match(view, /device-registry\/\$\{encodeURIComponent\(device\.value\.id\)\}\/debug/) /* 验证实际结果符合预期。 */
  assert.match(view, /messageId.*<unique>/) /* 验证实际结果符合预期。 */
  assert.match(view, /alarm.*true/) /* 验证实际结果符合预期。 */
  assert.match(view, /localStorage/) /* 验证实际结果符合预期。 */
  assert.match(view, /不会自动创建告警规则/) /* 验证实际结果符合预期。 */
  assert.match(view, /告警会直接进入告警中心/) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(view, /系统生成的高温烟雾规则/) /* 验证实际结果符合预期。 */
  assert.match(app, /action\.type === 'OPEN_PAGE'/) /* 验证实际结果符合预期。 */
  assert.match(app, /openPage\(action\.page\)/) /* 验证实际结果符合预期。 */
  assert.match(app, /'devices'/) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('alarm acknowledgement action is unavailable after the alarm is acknowledged', async () => { /* 执行当前语句并推进处理流程。 */
  const actions = await import('../src/alarmActions.js') /* 声明 actions。 */
  const alarms = await readFile(new URL('src/views/AlarmsView.vue', root), 'utf8') /* 声明 alarms。 */
  assert.equal(actions.canAcknowledgeAlarm('ACTIVE'), true) /* 验证实际结果符合预期。 */
  assert.equal(actions.canAcknowledgeAlarm('ACKED'), false) /* 验证实际结果符合预期。 */
  assert.equal(actions.canAcknowledgeAlarm('CLOSED'), false) /* 验证实际结果符合预期。 */
  assert.equal(actions.canCloseAlarm('ACKED'), true) /* 验证实际结果符合预期。 */
  assert.match(alarms, /canAcknowledgeAlarm\(row\.status\)/) /* 验证实际结果符合预期。 */
  assert.match(alarms, /canCloseAlarm\(row\.status\)/) /* 验证实际结果符合预期。 */
  assert.match(alarms, /actionPending\[row\.alarmId\]/) /* 验证实际结果符合预期。 */
  assert.match(alarms, /await load\(\)/) /* 验证实际结果符合预期。 */
  assert.match(alarms, /analysisProgress/) /* 验证实际结果符合预期。 */
  assert.match(alarms, /estimatedRemainingMs/) /* 验证实际结果符合预期。 */
  assert.match(alarms, /progress\/\$\{encodeURIComponent\(jobId\)\}/) /* 验证实际结果符合预期。 */
  assert.match(alarms, /progress`\)/)
  assert.match(alarms, /function handleDetailClosed\(\)/)
})

test('backup center exposes history, artifact downloads and restore drills', async () => {
  const app = await appSource()
  const view = await readFile(new URL('src/views/BackupsView.vue', root), 'utf8')
  const labels = await readFile(new URL('src/labels.js', root), 'utf8')
  for (const marker of ['备份中心', '立即备份设备数据', '备份昨日数据', '详情 / 文件', '文件校验', '下载文件清单', '备份文件']) {
    assert.match(`${app}\n${view}`, new RegExp(marker), `missing backup center marker: ${marker}`)
  }
  assert.match(labels, /backupTypes/)
  assert.match(labels, /backupStatuses/)
  for (const route of ['/api/v1/backups', '/restore-drill', '/files']) assert.match(view, new RegExp(route.replaceAll('/', '\\/')))
  assert.match(view, /download\(/)
  assert.match(app, /backups/)
})

test('device access and health inspection pages expose the new runtime workflow', async () => {
  const protocol = await readFile(new URL('src/views/ProtocolsView.vue', root), 'utf8')
  const inspection = await readFile(new URL('src/views/HealthInspectionView.vue', root), 'utf8')
  const app = await appSource()
  for (const label of ['设备连接平台', '保存平台连接配置', '协议版本', '平台连接配置']) {
    assert.match(protocol, new RegExp(label), `missing protocol v2 label: ${label}`)
  }
  for (const label of ['设备健康巡检', '立即巡检', '状态正常', '活动告警', '智能巡检建议']) {
    assert.match(inspection, new RegExp(label), `missing inspection label: ${label}`)
  }
  assert.match(inspection, /api\('\/api\/v1\/ai\/health-inspection\/run'/)
  assert.match(inspection, /api\('\/api\/v1\/ai\/health-inspection\/progress'/)
  assert.match(inspection, /loadHealthInspection/)
  assert.match(inspection, /saveHealthInspection/)
  assert.match(inspection, /sessionStorage/)
  assert.match(inspection, /function pollProgress\(/)
  assert.match(inspection, /任务在后台继续执行，切换页面后会自动恢复/)
  assert.match(inspection, /estimatedRemainingMs/)
  assert.match(inspection, /onBeforeUnmount/)
  assert.doesNotMatch(inspection, /onMounted\(run\)/)
  assert.match(inspection, /点击上方“立即巡检”开始检查/)
  assert.match(app, /title:\s*'设备通信协议'/)
  assert.match(app, /title:'智能巡检'/)
  assert.doesNotMatch(app, /protocolAssistant/)
  assert.match(app, /inspection/)
})

test('AI rule drafts keep failures visible in the page', async () => {
  const rulesView = await readFile(new URL('src/views/RulesView.vue', root), 'utf8')
  assert.match(rulesView, /draftError\.value=e\?\.message/)
  assert.match(rulesView, /v-if="draftError"/)
  assert.match(rulesView, /规则草稿生成失败/)
})

test('Agent automation drafts and allowlisted UI actions remain wired end to end', async () => {
  const aiView = await readFile(new URL('src/views/AiView.vue', root), 'utf8')
  const rulesView = await readFile(new URL('src/views/RulesView.vue', root), 'utf8')
  const app = await appSource()
  const cameras = await readFile(new URL('src/views/CameraMappingsView.vue', root), 'utf8')
  assert.match(aiView, /RULE_DRAFT_READY/)
  assert.match(aiView, /clientAction\.persisted/)
  assert.match(aiView, /AI_HISTORY_STORAGE_PREFIX/)
  assert.match(aiView, /restoreConversation\(\)/)
  assert.match(aiView, /persistConversation\(\)/)
  assert.match(aiView, /mcp__iot__create_rule_draft/)
  assert.match(rulesView, /联动动作结构化数据/)
  assert.match(rulesView, /detail\.ruleDraft/)
  assert.match(rulesView, /detail\.persisted/)
  assert.match(app, /\/ui-action\//)
  assert.match(app, /action\.type === 'OPEN_CAMERA'/)
  assert.match(app, /allowedPages/)
  assert.match(cameras, /detail\.cameraId/)
  assert.doesNotMatch(cameras, /autoPreview|openPreview|VideoStreamPlayer/)
})

test('AI conversation history survives view recreation and stays tenant scoped', async () => {
  const { AI_HISTORY_STORAGE_PREFIX, loadAIHistory, saveAIHistory } = await import('../src/aiHistory.js')
  const values = new Map()
  const storage = { getItem:key => values.get(key) ?? null, setItem:(key,value) => values.set(key,value), removeItem:key => values.delete(key) }
  const session = { tenant:'tenant-a', user:'alice' }
  assert.equal(saveAIHistory(storage, session, { conversationId:'conversation-1', selectedWorkflowId:'ops-assistant', messages:[{ id:'m1', role:'user', status:'succeeded', text:'温度超过 80' }, { id:'m2', role:'assistant', status:'streaming', text:'' }], runs:[{ id:'r1', status:'running' }] }), true)
  assert.ok([...values.keys()][0].startsWith(AI_HISTORY_STORAGE_PREFIX))
  const restored = loadAIHistory(storage, session, 123456)
  assert.equal(restored.conversationId, 'conversation-1')
  assert.equal(restored.messages[0].text, '温度超过 80')
  assert.equal(restored.messages[1].status, 'canceled')
  assert.equal(restored.runs[0].status, 'canceled')
  assert.equal(restored.runs[0].finishedAt, 123456)
  assert.equal(loadAIHistory(storage, { tenant:'tenant-b', user:'alice' }), null)
})

test('AI conversations are isolated by workflow and legacy history stays readable', async () => {
  const { loadAIHistory, saveAIHistory } = await import('../src/aiHistory.js')
  const values = new Map()
  const storage = { getItem:key => values.get(key) ?? null, setItem:(key,value) => values.set(key,value), removeItem:key => values.delete(key) }
  const session = { tenant:'tenant-a', user:'alice' }
  const state = (workflow, text) => ({ conversationId:`conversation-${workflow}`, selectedWorkflowId:workflow, messages:[{ id:workflow, role:'user', status:'succeeded', text }], runs:[] })
  saveAIHistory(storage, session, state('workflow-a', 'A 的对话'), 'workflow-a')
  saveAIHistory(storage, session, state('workflow-b', 'B 的对话'), 'workflow-b')
  assert.equal(loadAIHistory(storage, session, Date.now(), 'workflow-a').messages[0].text, 'A 的对话')
  assert.equal(loadAIHistory(storage, session, Date.now(), 'workflow-b').messages[0].text, 'B 的对话')
  assert.equal(loadAIHistory(storage, session, Date.now(), 'workflow-c'), null)
})

test('AI rule draft cards reconcile persisted snapshots with current rule state', async () => {
  const { reconcileRuleDraftMessages } = await import('../src/ruleDraftStatus.js')
  const messages = [{ id:'assistant-1', ruleDraftPersisted:true, ruleDraftState:'draft', ruleDraft:{ id:'rule-1', name:'旧名称', enabled:false } }]
  assert.equal(reconcileRuleDraftMessages(messages, [{ id:'rule-1', name:'已启用规则', enabled:true, version:2 }]), 1)
  assert.equal(messages[0].ruleDraftState, 'enabled')
  assert.equal(messages[0].ruleDraft.enabled, true)
  assert.equal(messages[0].ruleDraft.name, '已启用规则')
  reconcileRuleDraftMessages(messages, [])
  assert.equal(messages[0].ruleDraftState, 'missing')
})

test('frontend builds independently and proxies backend routes', async () => {
  const vite = await readFile(new URL('vite.config.js', root), 'utf8')
  const nginx = await readFile(new URL('nginx.conf', root), 'utf8')
  assert.match(vite, /outDir:\s*'dist'/)
  assert.doesNotMatch(vite, /internal\/httpapi\/static/)
  for (const route of ['/api/', '/health/', '/mcp']) assert.ok(nginx.includes(route), `nginx is missing ${route}`)
  assert.ok(nginx.includes('platform-api:8080'))
  assert.match(nginx, /location = \/api\/v1\/ai\/chat\/stream\s*\{[\s\S]*?proxy_buffering off;[\s\S]*?proxy_cache off;[\s\S]*?gzip off;[\s\S]*?proxy_read_timeout 3600s;[\s\S]*?proxy_set_header Connection "";/)
  assert.doesNotMatch(nginx, /IOT_VIDEO_PREVIEW_CSP_SOURCES/)
  assert.doesNotMatch(nginx, /connect-src[^;]*\bhttp:\s+https:/)
})

test('Naive UI feedback and controls replace Element Plus', async () => {
  const main = await readFile(new URL('src/main.js', root), 'utf8')
  const feedback = await readFile(new URL('src/ui/feedback.js', root), 'utf8')
  const packageJson = JSON.parse(await readFile(new URL('package.json', root), 'utf8'))
  assert.match(main, /installUi\(app\)/)
  assert.match(feedback, /createDiscreteApi\(\['message', 'dialog'\]\)/)
  assert.ok(packageJson.dependencies['naive-ui'])
  assert.equal(packageJson.dependencies['element-plus'], undefined)
})

test('frontend rejects Node versions unsupported by the build toolchain', async () => {
  const packageJson = JSON.parse(await readFile(new URL('package.json', root), 'utf8'))
  const packageLock = JSON.parse(await readFile(new URL('package-lock.json', root), 'utf8'))
  const npmrc = await readFile(new URL('.npmrc', root), 'utf8')

  const supportedNodeVersions = '^20.19.0 || >=22.12.0'
  assert.equal(packageJson.engines?.node, supportedNodeVersions)
  assert.equal(packageLock.packages?.['']?.engines?.node, supportedNodeVersions)
  assert.match(npmrc, /^engine-strict=true\s*$/m)
})

test('component alarm popup identifies the actual part and location', async () => {
  const { parseRealtimeAlert, alertKeys } = await import('../src/globalAlert.js')
  const alert = parseRealtimeAlert('/iot/alarm/raised/c/d/b/fire/controller', {
    alarmId:'component-alarm', triggerId:'shared-report', deviceId:'controller', deviceName:'消防控制器',
    alarmType:'FIRE', componentId:'loop-1/node-7', componentName:'烟感探测器', componentLocation:'二楼走廊'
  })
  assert.equal(alert.deviceName, '消防控制器')
  assert.equal(alert.detail, '烟感探测器 · 二楼走廊 · 火灾告警')
  assert.deepEqual(alertKeys(alert), ['component-alarm'])
  assert.equal(parseRealtimeAlert('/iot/parsed/t/p/d/ALARM_REPORT', { messageType:'ALARM_REPORT', event:{components:[{id:'a'},{id:'b'}]} }), null)
})
