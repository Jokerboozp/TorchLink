import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'
import { join } from 'node:path'
import test from 'node:test'

const root = new URL('..', import.meta.url)

async function appSource() {
  return (await Promise.all(['src/App.vue', 'src/pageGuide.js'].map(path => readFile(new URL(path, root), 'utf8')))).join('\n')
}

async function sourceText(directory = new URL('src/', root)) {
  const entries = await readdir(directory, { withFileTypes: true })
  const contents = await Promise.all(entries.map(async entry => {
    const path = new URL(entry.name + (entry.isDirectory() ? '/' : ''), directory)
    return entry.isDirectory() ? sourceText(path) : readFile(path, 'utf8')
  }))
  return contents.flat().join('\n')
}

test('management controls and Chinese labels remain available', async () => {
  const source = await sourceText()
  for (const label of ['查看详情', '批量下载', '未注册设备', '一键注册', '摄像头映射', '保存规则', '火灾风险', '紧急', '活动中', '告警中', '疑似离线']) {
    assert.match(source, new RegExp(label), `missing label: ${label}`)
  }
  const labels = await import('../src/labels.js')
  assert.equal(labels.alarmLevel('CRITICAL'), '紧急')
  assert.equal(labels.alarmLevel('high'), '高')
  assert.equal(labels.alarmLevel(''), '未设置')
  const alarmsView = await readFile(new URL('src/views/AlarmsView.vue', root), 'utf8')
  assert.match(alarmsView, /alarmLevel\(analysis\.riskLevel\)/)
})

test('account logout is available from the top-right avatar menu', async () => {
  const app = await appSource()
  assert.match(app, /<el-dropdown class="account-dropdown"/)
  assert.match(app, /aria-label="打开用户菜单"/)
  assert.match(app, /command="logout"/)
  assert.match(app, /function handleAccountCommand\(command\)/)
  assert.doesNotMatch(app, /class="logout-button"/)
})

test('login form never exposes a built-in password', async () => {
  const app = await appSource()
  assert.match(app, /loginForm = ref\(\{ tenantId: 'tenant_001', username: 'admin', password: '' \}\)/)
  assert.doesNotMatch(app, /password:\s*'admin123'/)
})

test('shared controls keep file pickers and text actions visibly shaped', async () => {
  const styles = await readFile(new URL('src/styles.css', root), 'utf8')
  const assistant = await readFile(new URL('src/views/ProtocolAssistantView.vue', root), 'utf8')
  const protocols = await readFile(new URL('src/views/ProtocolsView.vue', root), 'utf8')
  assert.match(styles, /\.el-button\.is-text, \.el-button\.is-link \{[^}]*border: 1px solid var\(--border\)/)
  assert.match(styles, /input\[type="file"\]::file-selector-button/)
  assert.match(styles, /\.el-button--small \{[^}]*min-height: 28px/)
  assert.match(`${assistant}\n${protocols}`, /<FilePicker/)
})

test('global alarm popup handles raised alarms, fault events and tenant-scoped settings', async () => {
  const app = await appSource()
  const popup = await readFile(new URL('src/components/GlobalAlertPopup.vue', root), 'utf8')
  const alerts = await import('../src/globalAlert.js')

  assert.match(app, /GlobalAlertPopup/)
  for (const label of ['告警提醒', '显示报警弹窗', '弹窗静默时段', '播放警报声', '查看告警详情', '查看原始报文', '设备名称', '报警内容', '报警类型', '报警时间', '报警等级']) {
    assert.match(popup, new RegExp(label), `missing global alarm UI label: ${label}`)
  }
  assert.match(popup, /function alertContent\(item\)/)
  assert.doesNotMatch(popup, /global-alert-identifiers/)
  for (const technicalLabel of ['设备 ID', '告警编号', '消息 ID']) assert.doesNotMatch(popup, new RegExp(technicalLabel), `technical alarm identifier should not be shown: ${technicalLabel}`)
  assert.match(popup, /window\.addEventListener\('iot:realtime'/)

  const raised = alerts.parseRealtimeAlert(
    '/iot/alarm/raised/city-1/district-1/building-1/smoke/device-1',
    JSON.stringify({ alarmId:'alarm-1', triggerId:'message-1', deviceId:'device-1', deviceName:'东区烟感', alarmType:'FIRE_RISK', alarmLevel:'CRITICAL', source:'device', lastTriggeredAt:1760000000000 })
  )
  assert.equal(raised.kind, 'alarm')
  assert.equal(raised.alarmId, 'alarm-1')
  assert.equal(raised.deviceName, '东区烟感')
  assert.equal(raised.detail, '检测到设备异常报警，请及时处理。')
  assert.deepEqual(alerts.alertKeys(raised), ['alarm-1', 'message-1'])

  const fault = alerts.parseRealtimeAlert(
    '/iot/parsed/tenant-a/product-a/device-1/EVENT_REPORT',
    { messageId:'message-fault', rawMessageId:'raw-message-fault', messageType:'EVENT_REPORT', deviceId:'device-1', event:{ type:'FAULT', description:'主电源故障' } }
  )
  assert.equal(fault.kind, 'fault')
  assert.equal(fault.messageId, 'raw-message-fault')
  assert.equal(fault.alarmType, 'DEVICE_FAULT')
  assert.equal(fault.detail, '主电源故障')
  assert.equal(alerts.parseRealtimeAlert('/iot/parsed/tenant-a/product-a/device-1/EVENT_REPORT', { messageType:'EVENT_REPORT', event:{ type:'HEARTBEAT' } }), null)

  const values = new Map()
  const storage = { getItem:key => values.get(key) ?? null, setItem:(key, value) => values.set(key, value) }
  const saved = alerts.saveAlertSettings(storage, { tenant:'tenant-a', user:'operator' }, { popupEnabled:false, soundEnabled:false, quietStart:'22:00', quietEnd:'07:00' })
  assert.equal(alerts.loadAlertSettings(storage, { tenant:'tenant-a', user:'operator' }).popupEnabled, false)
  assert.equal(alerts.loadAlertSettings(storage, { tenant:'tenant-b', user:'operator' }).popupEnabled, true)
  assert.equal(alerts.isWithinQuietHours(new Date(2026, 0, 1, 23, 30), saved), true)
  assert.equal(alerts.isWithinQuietHours(new Date(2026, 0, 1, 12, 0), saved), false)
})

test('long dialogs keep the viewport fixed and scroll within the dialog body', async () => {
  const styles = await readFile(new URL('src/styles.css', root), 'utf8')
  const dialogStyle = styles.match(/\.el-dialog\s*\{[^}]+\}/)?.[0]
  const dialogBodyStyle = styles.match(/\.el-dialog__body\s*\{[^}]+\}/)?.[0]
  assert.ok(dialogStyle, 'dialog layout style must remain explicit')
  assert.ok(dialogBodyStyle, 'dialog body layout style must remain explicit')
  assert.match(styles, /\.main-content\s*\{[^}]*flex:\s*1 1 auto[^}]*overflow-y:\s*auto/)
  assert.match(styles, /\.el-overlay-dialog\s*\{[^}]*overflow:\s*hidden/)
  assert.match(dialogStyle, /display:\s*flex/)
  assert.match(dialogStyle, /flex-direction:\s*column/)
  assert.match(dialogStyle, /max-height:\s*calc\(100vh\s*-\s*48px\)/)
  assert.match(dialogBodyStyle, /min-height:\s*0/)
  assert.match(dialogBodyStyle, /overflow-y:\s*auto/)
  assert.match(dialogBodyStyle, /overscroll-behavior:\s*contain/)
})

test('camera metadata and AI workflow playground remain available', async () => {
  const source = await sourceText()
  for (const label of ['摄像头点位', '不解析、拉取或预览视频流', '智能助手', '运行轨迹', '工具调用']) {
    assert.match(source, new RegExp(label), `missing feature label: ${label}`)
  }
  const cameraView = await readFile(new URL('src/views/CameraMappingsView.vue', root), 'utf8')
  assert.doesNotMatch(cameraView, /VideoStreamPlayer|autoPreview|openPreview|streamUrl|hls\.js/)
  assert.match(source, /\.provider-select-row\s*\{[^}]*width:100%;[^}]*min-width:0;/, 'provider selector row must fill the Element Plus form content width')
})

test('knowledge management uploads files and lists tenant documents', async () => {
  const app = await appSource()
  const view = await readFile(new URL('src/views/KnowledgeView.vue', root), 'utf8')
  for (const label of ['知识库', '上传知识文档', '上传并建立索引', '已上传文档', '打开智能助手', '知识文档详情与切片', '索引与切片规则', '字符范围', '切片内容', '向量化', '知识库策略', '每次强制检索', '最低相似度', '无匹配知识时', '保存知识库策略']) {
    assert.match(`${app}\n${view}`, new RegExp(label), `missing knowledge UI label: ${label}`)
  }
  assert.match(view, /api\((?:'|`)[^'`]*\/api\/v1\/knowledge\/documents(?:\?|['`])/)
  assert.match(view, /method:'POST', body:form/)
  assert.match(view, /new FormData\(\)/)
  assert.match(view, /persistentIndex/)
  assert.match(view, /api\((?:'|`)[^'`]*\/api\/v1\/ai\/workflows(?:\?|['`])/)
  assert.match(view, /form\.append\('workflowId', workflowId\.value\)/)
  assert.match(view, /<el-select v-model="workflowId"/)
  assert.match(view, /api\(`\/api\/v1\/knowledge\/documents\/\$\{encodeURIComponent\(document\.id\)\}`\)/)
  assert.match(view, /固定窗口 \+ 重叠/)
  assert.match(view, /row\.startChar/)
  assert.match(view, /row\.overlapChars/)
  assert.match(view, /row\.vectorized/)
  assert.match(view, /knowledge-binding/)
  assert.match(view, /function loadBinding\(\)/)
  assert.match(view, /function saveBinding\(\)/)
  for (const label of ['知识分类', '知识标签', '告警处置操作规程']) assert.match(view, new RegExp(label), `missing knowledge metadata UI: ${label}`)
})

test('knowledge statistic cards use readable foreground colors', async () => {
  const view = await readFile(new URL('src/views/KnowledgeView.vue', root), 'utf8')
  const statsStyle = view.match(/\.knowledge-stats span,\.knowledge-stats small \{[^}]+\}/)?.[0]
  assert.ok(statsStyle, 'knowledge statistic label style must remain explicit')
  assert.match(statsStyle, /color:var\(--muted-foreground\)/)
  assert.doesNotMatch(statsStyle, /color:var\(--muted\)/)
})

test('AI workbench uses cancellable SSE workflows and stable message keys', async () => {
  const aiView = await readFile(new URL('src/views/AiView.vue', root), 'utf8')
  const apiSource = await readFile(new URL('src/api.js', root), 'utf8')
  const sseSource = await readFile(new URL('src/sse.js', root), 'utf8')

  assert.match(aiView, /api\((?:'|`)[^'`]*\/api\/v1\/ai\/workflows(?:\?|['`])/)
  assert.match(aiView, /runtimeRequestSequence/)
  assert.match(aiView, /requestSequence !== runtimeRequestSequence/)
  assert.match(aiView, /workflowManageRequestSequence/)
  assert.match(aiView, /loadWorkflowManagement\(true\)/)
  assert.match(aiView, /apiStream\('\/api\/v1\/ai\/chat\/stream'/)
  assert.match(aiView, /new AbortController\(\)/)
  assert.match(aiView, /:key="message\.id"/)
  for (const field of ['question','conversationId','workflowId','model','maxTokens']) assert.match(aiView, new RegExp(`\\b${field}\\b`), `missing AI request field: ${field}`)
  for (const eventType of ['run.started','text.delta','tool.started','tool.completed','run.completed','run.failed']) assert.ok(aiView.includes(`'${eventType}'`), `missing stream event: ${eventType}`)
  assert.doesNotMatch(aiView, /event\.reasoning/)
  assert.doesNotMatch(aiView, /conversationId\.value\s*=\s*event\.conversationId/)
  assert.doesNotMatch(aiView, /model:[^\n]*runtime\.value\.active/)
  assert.match(aiView, /conversationId\.value\s*=\s*makeId\('conversation'\)/)
  for (const label of ['智能体插件管理', '新建智能体', '智能体配置清单', '校验并创建智能体', '保存后智能体会立即进入工作流列表']) assert.match(aiView, new RegExp(label), `missing dynamic Agent UI: ${label}`)
  for (const field of ['schemaVersion','id','name','description','version','enabled','persona','defaultModel','maxTokens','capabilities','allowedTools']) assert.match(aiView, new RegExp(`name:'${field}'`), `missing Agent field documentation: ${field}`)
  assert.match(aiView, /结构化数据标准不支持注释/)
  assert.match(aiView, /允许使用的工具/)
  for (const label of ['本次运行', '选择工作流', '运行参数', '运行环境', '智能体管理']) assert.match(aiView, new RegExp(label), `missing workflow hierarchy label: ${label}`)
  assert.match(aiView, /<el-drawer v-model="managementVisible" title="智能体管理"/)
  assert.doesNotMatch(aiView, /<el-menu/)
  assert.doesNotMatch(aiView, /Provider 测试/)
  assert.doesNotMatch(aiView, /panel-knowledge|panel-provider/)
  assert.doesNotMatch(aiView, /<el-collapse/)
  assert.match(aiView, /method:'PUT'/)
  assert.match(aiView, /api\('\/api\/v1\/ai\/workflows', \{ method:'POST'/)
  for (const marker of ['/api/v1/ai/workflows/admin', "method:'DELETE'", '工作流插件管理', '已配置的工作流插件', '内置只读', '启用', '禁用', '删除']) {
    assert.match(aiView, new RegExp(marker.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')), `missing workflow management marker: ${marker}`)
  }
  for (const marker of ['agentPreviewVisible', 'agentPreviewJson', 'viewAgent', '查看内置智能体', '内置智能体配置清单（只读）']) {
    assert.match(aiView, new RegExp(marker.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')), `missing built-in Agent preview marker: ${marker}`)
  }
  for (const workflowId of ['alarm-handler', 'device-health-inspector', 'protocol-assistant']) {
    assert.match(aiView, new RegExp(workflowId), `missing non-chat workflow classification: ${workflowId}`)
  }
  assert.match(aiView, /const nonChatWorkflowIds = new Set\(\[/)
  assert.match(aiView, /isChatWorkflow\(item\)/)
  assert.match(aiView, /value\.items\.filter\(isChatWorkflow\)/)
  assert.match(aiView, /@click="viewAgent\(item\)"/)
  assert.match(aiView, /@click="startCreateAgent"/)
  assert.match(aiView, /function startCreateAgent\(\)/)
  assert.match(aiView, /agentEditorVisible\.value = true/)
  assert.match(aiView, /function openAgentManagement\(\)/)
  assert.match(aiView, /agentEditorRef/)
  assert.match(aiView, /@click="openAgentManagement"/)
  assert.match(aiView, /method:'PUT'/)
  assert.match(apiSource, /export async function apiStream/)
  assert.match(apiSource, /request\.cache = 'no-store'/)
  assert.doesNotMatch(aiView, /providerProfileStorageKey|testPlugin|sandbox\.provider/)
  assert.match(apiSource, /text\/event-stream/)
  assert.match(sseSource, /getReader\(\)/)
})

test('AI streaming keeps Markdown rendering and scroll work bounded', async () => {
  const aiView = await readFile(new URL('src/views/AiView.vue', root), 'utf8')

  assert.match(aiView, /queueAssistantText/)
  assert.match(aiView, /flushAssistantText/)
  assert.match(aiView, /<MarkdownContent v-if="message\.text && message\.role === 'assistant' && message\.status !== 'streaming'"/)
  assert.match(aiView, /<p v-else-if="message\.text">\{\{ message\.text \}\}<\/p>/)
  assert.doesNotMatch(aiView, /behavior:'smooth'/)
})

test('AI chat sizes sent question bubbles to content until the readable max width', async () => {
  const aiView = await readFile(new URL('src/views/AiView.vue', root), 'utf8')
  const messageContentStyle = aiView.match(/\.message-content \{[^}]+\}/)?.[0]
  const userContentStyle = aiView.match(/\.message-row\.user \.message-content \{[^}]+\}/)?.[0]
  const userMessageStyle = aiView.match(/\.message-row\.user \.chat-message \{[^}]+\}/)?.[0]
  assert.ok(messageContentStyle, 'AI message content width must remain explicit')
  assert.ok(userContentStyle, 'AI user message content width must remain explicit')
  assert.ok(userMessageStyle, 'AI user chat message width must remain explicit')
  assert.match(messageContentStyle, /width:100%/)
  assert.match(userContentStyle, /width:fit-content/)
  assert.match(userContentStyle, /max-width:min\(82%,760px\)/)
  assert.doesNotMatch(userContentStyle, /width:100%/)
  assert.match(userMessageStyle, /width:fit-content/)
  assert.match(userMessageStyle, /max-width:100%/)
})

test('AI answers render safe Markdown in chat and health inspection', async () => {
  const markdown = await import('../src/markdown.js')
  const html = markdown.renderMarkdown('# 标题\n\n- **重点**\n\n`code`')
  assert.match(html, /<h1>标题<\/h1>/)
  assert.match(html, /<ul>[\s\S]*<strong>重点<\/strong>[\s\S]*<\/ul>/)
  assert.match(html, /<code>code<\/code>/)
  const unsafeHtml = markdown.renderMarkdown('<script>alert(1)</script>')
  assert.match(unsafeHtml, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/)
  assert.doesNotMatch(unsafeHtml, /<script>/)

  const aiView = await readFile(new URL('src/views/AiView.vue', root), 'utf8')
  const inspectionView = await readFile(new URL('src/views/HealthInspectionView.vue', root), 'utf8')
  assert.match(aiView, /<MarkdownContent[^>]*:source="message\.text"/)
  assert.match(inspectionView, /<MarkdownContent[^>]*:source="report\.aiAdvice"/)
})

test('health inspection report survives menu-driven view recreation and stays tenant scoped', async () => {
  const { HEALTH_INSPECTION_STORAGE_PREFIX, healthInspectionStorageKey, saveHealthInspection, loadHealthInspection } = await import('../src/healthInspectionState.js')
  const values = new Map()
  const storage = { getItem:key => values.get(key) ?? null, setItem:(key,value) => values.set(key,value), removeItem:key => values.delete(key) }
  const session = { tenant:'tenant-a', user:'alice' }
  const report = { generatedAt:1760000000000, summary:'巡检完成', counts:{ total:3, healthy:2 }, items:[], aiAdvice:'需要复核一台设备。' }
  assert.equal(saveHealthInspection(storage, session, report), true)
  assert.ok([...values.keys()][0].startsWith(HEALTH_INSPECTION_STORAGE_PREFIX))
  assert.deepEqual(loadHealthInspection(storage, session), report)
  assert.equal(loadHealthInspection(storage, { tenant:'tenant-b', user:'alice' }), null)
  assert.equal(loadHealthInspection(storage, { tenant:'tenant-a', user:'bob' }), null)
  values.set(healthInspectionStorageKey(session), '{invalid')
  assert.equal(loadHealthInspection(storage, session), null)
  assert.equal(values.has(healthInspectionStorageKey(session)), false)
})

test('AI model administration has its own menu and business overview', async () => {
  const app = await appSource()
  const aiView = await readFile(new URL('src/views/AiView.vue', root), 'utf8')
  const providerView = await readFile(new URL('src/views/AiProvidersView.vue', root), 'utf8')
  const knowledgeView = await readFile(new URL('src/views/KnowledgeView.vue', root), 'utf8')
  assert.match(app, /knowledge: \{ \.\.\.pageGuide.knowledge/)
  assert.match(app, /aiProviders: \{ \.\.\.pageGuide.aiProviders/)
  assert.match(app, /items: \['aiProviders', 'ai', 'knowledge'\]/)
  assert.match(app, /allowedPages = new Set\(\[[^\]]*'aiProviders'/s)
  assert.match(aiView, /<el-dialog v-model="agentEditorVisible" :title="editingAgentId \? '编辑智能体' : '新建智能体'"/)
  assert.match(aiView, /function cancelAgentEditor\(\)/)
  assert.doesNotMatch(aiView, /Provider 测试|连接并测试插件|\/api\/v1\/ai\/providers\/test/)
  assert.match(aiView, /管理模型服务/)
  assert.doesNotMatch(aiView, /providerForm|saveProviderConfig|\/api\/v1\/ai\/providers\/config/)
  assert.doesNotMatch(aiView, /<el-menu-item index="knowledge"|<el-menu-item index="provider"/)
  for (const label of ['统一管理智能模型与业务能力', '模型服务配置', '智能业务能力', '可用模型服务', '测试配置', '应用配置', '智能告警研判', '智能巡检']) {
    assert.match(providerView, new RegExp(label), `missing AI Provider management label: ${label}`)
  }
  for (const label of ['AI Provider', 'Provider 配置', '可用 Provider', '测试并应用']) {
    assert.doesNotMatch(providerView, new RegExp(label), `English Provider wording should not be visible: ${label}`)
  }
  assert.match(providerView, /\/api\/v1\/ai\/providers\?page=1&pageSize=100/)
  assert.match(providerView, /\/api\/v1\/ai\/providers\/test/)
  assert.match(providerView, /\/api\/v1\/ai\/providers\/config/)
  assert.match(providerView, /function testProviderConfig\(\)/)
  assert.match(providerView, /function applyProviderConfig\(\)/)
  assert.match(providerView, /:disabled="!canApply"/)
  assert.match(providerView, /当前填写内容未生效/)
  assert.match(providerView, /所有智能功能立即生效/)
  assert.match(knowledgeView, /知识库策略/)
})

test('protocol v2 point-table, package release and device collection flows are visible', async () => {
  const protocols = await readFile(new URL('src/views/ProtocolsView.vue', root), 'utf8')
  const raw = await readFile(new URL('src/views/RawView.vue', root), 'utf8')
  const devices = await readFile(new URL('src/views/DevicesView.vue', root), 'utf8')
  const app = await appSource()
  const integration = await readFile(new URL('src/views/IntegrationView.vue', root), 'utf8')
  for (const label of ['网络监听', '源码接入', '不可变版本', '接入实例', '连接测试']) assert.match(protocols, new RegExp(label), `missing label: ${label}`)
  for (const route of ['/api/v2/protocols', '/api/v2/device-access-profiles']) assert.match(protocols, new RegExp(route.replaceAll('/', '\\/')))
  assert.match(protocols, /go-protocol-v2/)
  assert.match(protocols, /protocol\.json/)
  assert.match(app, /label: '设备接入'/)
  assert.match(app, /integration: \{ \.\.\.pageGuide.integration/)
  assert.match(integration, /设备连接指南/)
  assert.match(raw, /standardMessage/)
    assert.match(devices, /hasReported\(row\)/)
  assert.match(devices, /查看数据/)
  assert.match(devices, /配置接入/)
})

test('test device workbench provisions a fixture and sends editable data and alarm templates', async () => {
  const app = await appSource()
  const view = await readFile(new URL('src/views/TestDeviceView.vue', root), 'utf8')
  for (const label of ['测试设备', '发送正常数据', '发送报警数据', '发送恢复数据', '报文模板', '建议测试顺序']) {
    assert.match(`${app}\n${view}`, new RegExp(label), `missing test device label: ${label}`)
  }
  assert.match(app, /testDevice/)
  assert.match(view, /api\('\/api\/v1\/test-devices\/provision'/)
  assert.match(view, /device-registry\/\$\{encodeURIComponent\(device\.value\.id\)\}\/debug/)
  assert.match(view, /messageId.*<unique>/)
  assert.match(view, /alarm.*true/)
  assert.match(view, /localStorage/)
  assert.match(view, /不会自动创建告警规则/)
  assert.match(view, /告警会直接进入告警中心/)
  assert.doesNotMatch(view, /系统生成的高温烟雾规则/)
  assert.match(app, /action\.type === 'OPEN_PAGE'/)
  assert.match(app, /openPage\(action\.page\)/)
  assert.match(app, /'devices'/)
})

test('alarm acknowledgement action is unavailable after the alarm is acknowledged', async () => {
  const actions = await import('../src/alarmActions.js')
  const alarms = await readFile(new URL('src/views/AlarmsView.vue', root), 'utf8')
  assert.equal(actions.canAcknowledgeAlarm('ACTIVE'), true)
  assert.equal(actions.canAcknowledgeAlarm('ACKED'), false)
  assert.equal(actions.canAcknowledgeAlarm('CLOSED'), false)
  assert.equal(actions.canCloseAlarm('ACKED'), true)
  assert.match(alarms, /canAcknowledgeAlarm\(row\.status\)/)
  assert.match(alarms, /canCloseAlarm\(row\.status\)/)
  assert.match(alarms, /actionPending\[row\.alarmId\]/)
  assert.match(alarms, /await load\(\)/)
  assert.match(alarms, /analysisProgress/)
  assert.match(alarms, /estimatedRemainingMs/)
  assert.match(alarms, /progress\/\$\{encodeURIComponent\(jobId\)\}/)
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
  for (const label of ['上传完整设备协议包后，在这里启用设备监听端口', '保存接入实例', '协议与版本', '接入实例']) {
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
  assert.match(inspection, /点击“立即巡检”开始检查/)
  assert.match(app, /title:\s*'设备接入'/)
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

test('programmatic Element Plus services include their global styles', async () => {
  const main = await readFile(new URL('src/main.js', root), 'utf8')
  assert.match(main, /element-plus\/theme-chalk\/el-message-box\.css/)
  assert.match(main, /element-plus\/theme-chalk\/el-message\.css/)
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
