<script setup>
import { statusLabel } from '../presentation'
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import StandardDeviceCommissioning from '../components/StandardDeviceCommissioning.vue'
import { api, formatTime, notifyError, parseJSON, pretty, session } from '../api'
import { alarmType, label, messageTypeLabel, tagType, parsers } from '../labels'

const emit = defineEmits(['navigate'])

const templateNames = { data: '正常数据', alarm: '报警数据', recovery: '恢复数据', event: '事件数据' }
const templateDescriptions = {
  data: '用于验证设备上报、解析、设备在线状态和属性历史。',
  alarm: '温度超过 80℃ 且 smoke=true，设备告警会直接进入告警中心；匹配规则可额外提供类型、等级和联动。',
  recovery: '温度恢复到安全值且 smoke=false，用于验证告警自动恢复。',
  event: '带 event 节点的事件上报，用于验证事件消息类型。'
}
const templateOrder = Object.keys(templateNames)
const templates = reactive({ data: '', alarm: '', recovery: '', event: '' })
const activeTemplate = ref('data')
const device = ref(null)
const product = ref(null)
const protocolPackage = ref(null)
const credential = ref(null)
const loading = ref(false)
const sending = ref('')
const result = ref(null)
const history = ref([])

const currentTemplate = computed({
  get: () => templates[activeTemplate.value],
  set: value => { templates[activeTemplate.value] = value }
})
const currentTemplateName = computed(() => templateNames[activeTemplate.value])

function storageKey() {
  return `iot:test-device-templates:${session.tenant || 'default'}`
}

function saveTemplates() {
  localStorage.setItem(storageKey(), JSON.stringify({ ...templates }))
}

function restoreTemplates() {
  try {
    const value = JSON.parse(localStorage.getItem(storageKey()) || '{}')
    for (const key of templateOrder) if (typeof value[key] === 'string' && value[key].trim()) templates[key] = value[key]
  } catch {
    // Ignore an invalid local draft and keep the server defaults.
  }
}

function applyServerTemplates(value) {
  for (const key of templateOrder) {
    if (value?.[key]) templates[key] = pretty(value[key])
  }
}

function resetLocalTemplates() {
  localStorage.removeItem(storageKey())
  prepare(true)
}

async function prepare(reset = false) {
  loading.value = true
  try {
    const data = await api('/api/v1/test-devices/provision', { method: 'POST', body: JSON.stringify({ reset }) })
    device.value = data.device
    product.value = data.product
    protocolPackage.value = data.protocolPackage
    if (data.credential) credential.value = data.credential
    applyServerTemplates(data.templates)
    if (!reset) restoreTemplates()
    if (reset) saveTemplates()
    if (reset) {
      history.value = []
      result.value = null
      ElMessage.success('测试设备和默认报文已准备完成')
    }
  } catch (error) {
    notifyError(error)
  } finally {
    loading.value = false
  }
}

function uniqueMessageId(kind) {
  return `raw_test_${kind}_${Date.now()}_${Math.random().toString(16).slice(2, 8)}`
}

function wait(ms) {
  return new Promise(resolve => setTimeout(resolve, ms))
}

async function loadRawDetail(messageId) {
  for (let attempt = 0; attempt < 4; attempt += 1) {
    try {
      const detail = await api(`/api/v1/raw-messages/${encodeURIComponent(messageId)}`)
      if (detail.standardMessage || attempt === 3) return detail
    } catch (error) {
      if (attempt === 3) throw error
    }
    await wait(120)
  }
  return null
}

async function sendTemplate(kind) {
  if (!device.value) return ElMessage.warning('测试设备还没有准备好')
  let body
  try {
    body = parseJSON(templates[kind], `${templateNames[kind]}模板`)
    if (!body || Array.isArray(body) || typeof body !== 'object') throw new Error('报文必须是结构化数据对象')
    if (!body.payload || typeof body.payload !== 'object') throw new Error('报文必须包含 payload 对象')
    if (!body.messageId || String(body.messageId).includes('<unique>')) body.messageId = uniqueMessageId(kind)
  } catch (error) {
    notifyError(error)
    return
  }

  sending.value = kind
  saveTemplates()
  try {
    const response = await api(`/api/v1/device-registry/${encodeURIComponent(device.value.id)}/debug`, { method: 'POST', body: JSON.stringify(body) })
    const messageId = response.archive?.messageId || response.messageId || body.messageId
    const rawDetail = await loadRawDetail(messageId)
    const alarmData = await api(`/api/v1/alarms?deviceId=${encodeURIComponent(device.value.id)}&limit=20`)
    const relatedAlarms = (alarmData.items || []).filter(item => item.triggerId === messageId || (kind === 'alarm' || kind === 'recovery') && item.deviceId === device.value.id)
    const record = {
      id: `${messageId}-${Date.now()}`,
      kind,
      messageId,
      sentAt: Date.now(),
      parsed: rawDetail?.parseStatus === 'PARSED',
      messageType: rawDetail?.standardMessage?.messageType || '—',
      alarm: relatedAlarms.some(item => item.status === 'ACTIVE')
    }
    history.value = [record, ...history.value].slice(0, 8)
    result.value = { kind, messageId, response, rawDetail, alarms: relatedAlarms, sentAt: Date.now() }
    ElMessage.success(`${templateNames[kind]}已发送，已进入平台处理链路`)
  } catch (error) {
    result.value = { kind, error: error?.message || String(error), sentAt: Date.now() }
    notifyError(error)
  } finally {
    sending.value = ''
  }
}

function resultDetail(value) {
  return value?.rawDetail || value?.response || { error: value?.error || '暂无结果' }
}

function alarmLabel(value) {
  return alarmType(value)
}

onMounted(() => prepare())
</script>

<template>
  <div class="test-device-view">
    <StandardDeviceCommissioning />
    <div class="page-toolbar">
      <el-button type="primary" :loading="loading" @click="prepare(false)">准备测试设备</el-button>
      <el-button plain type="warning" :loading="loading" @click="resetLocalTemplates">恢复默认配置</el-button>
      <el-button @click="emit('navigate', 'devices')">查看设备管理</el-button>
      <el-button @click="emit('navigate', 'alarms')">打开告警中心</el-button>
      <span>系统会为当前租户生成一台可重复使用的测试烟感设备。</span>
    </div>

    <el-alert v-if="device" title="设备告警直接进入告警中心" description="测试设备不会自动创建告警规则；发送报警数据会直接产生设备告警。若存在匹配规则，则按规则提供告警类型、等级和联动动作。"
      type="info" :closable="false" show-icon />

    <div v-if="device" class="test-device-layout top-gap">
      <section class="test-device-workbench">
        <el-card shadow="never" class="surface-card">
          <template #header>
            <div class="card-header">
              <div><strong>测试设备</strong><small>已绑定产品、已发布协议包，可直接开始发送</small></div>
              <el-tag type="success" round>设备链路已就绪</el-tag>
            </div>
          </template>
          <div class="test-device-summary">
            <div><span>设备名称</span><strong>{{ device.name }}</strong><small>{{ device.id }}</small></div>
            <div><span>产品 / 协议</span><strong>{{ product?.name }}</strong><small>{{ label(parsers, protocolPackage?.parserType, '自定义协议') }} · {{ protocolPackage?.version }}</small></div>
            <div><span>告警处理</span><strong>直接告警 + 可选规则</strong><small>设备主动告警无需规则，规则可补充联动</small></div>
          </div>
          <el-descriptions :column="1" border>
            <el-descriptions-item label="设备标识"><code>{{ device.id }}</code></el-descriptions-item>
            <el-descriptions-item label="接入密钥"><code>{{ device.accessKey }}</code></el-descriptions-item>
            <el-descriptions-item label="设备状态"><el-tag :type="tagType(device.status)" round>{{ statusLabel(device.status) }}</el-tag></el-descriptions-item>
            <el-descriptions-item label="报警模板条件">temperature &gt; 80 且 smoke = true</el-descriptions-item>
          </el-descriptions>
          <el-alert v-if="credential" class="top-gap" title="设备凭证已生成" description="密钥只在本次准备时返回，请仅在本地测试环境保存；页面内发送数据不需要手动填写凭证。" type="info" :closable="false" show-icon />
          <div v-if="credential" class="credential-box top-gap"><span>设备密钥</span><code>{{ credential.secret }}</code></div>
        </el-card>

        <el-card shadow="never" class="surface-card template-card">
          <template #header>
            <div class="card-header"><div><strong>报文模板</strong><small>{{ templateDescriptions[activeTemplate] }}</small></div><el-tag effect="plain" round>{{ currentTemplateName }}</el-tag></div>
          </template>
          <div class="template-switcher" role="tablist" aria-label="测试报文类型">
            <button v-for="key in templateOrder" :key="key" type="button" :class="{ active: activeTemplate === key }" @click="activeTemplate = key">{{ templateNames[key] }}</button>
          </div>
          <el-input v-model="currentTemplate" class="template-editor" type="textarea" :rows="18" spellcheck="false" aria-label="可编辑报文模板" />
          <div class="template-actions">
            <el-button type="primary" :loading="sending === activeTemplate" @click="sendTemplate(activeTemplate)">发送{{ currentTemplateName }}</el-button>
            <span>支持直接修改报文内容；带 <code>&lt;unique&gt;</code> 的消息标识会在发送时自动替换。</span>
          </div>
        </el-card>

        <el-card shadow="never" class="surface-card quick-send-card">
          <template #header><div class="card-header"><div><strong>快捷发送</strong><small>不打开编辑器也可以直接验证典型链路</small></div></div></template>
          <div class="quick-send-grid">
            <button type="button" class="quick-send normal" :disabled="Boolean(sending)" @click="sendTemplate('data')"><strong>发送正常数据</strong><small>属性上报 · 在线状态</small></button>
            <button type="button" class="quick-send danger" :disabled="Boolean(sending)" @click="sendTemplate('alarm')"><strong>发送报警数据</strong><small>高温 + 烟雾 · 直接入告警中心</small></button>
            <button type="button" class="quick-send warning" :disabled="Boolean(sending)" @click="sendTemplate('recovery')"><strong>发送恢复数据</strong><small>安全值 · 自动恢复</small></button>
            <button type="button" class="quick-send event" :disabled="Boolean(sending)" @click="sendTemplate('event')"><strong>发送事件数据</strong><small>心跳事件 · 事件解析</small></button>
          </div>
        </el-card>
      </section>

      <aside class="test-device-side">
        <el-card shadow="never" class="surface-card">
          <template #header><strong>建议测试顺序</strong></template>
          <el-steps direction="vertical" :active="4">
            <el-step title="发送正常数据" description="确认原始报文归档、标准消息和设备在线状态。" />
            <el-step title="发送报警数据" description="确认设备告警直接进入告警中心，并验证规则联动（如已配置）。" />
            <el-step title="发送恢复数据" description="确认告警从活动中变为已恢复。" />
            <el-step title="修改模板再发送" description="验证自定义字段、事件和异常报文。" />
          </el-steps>
        </el-card>

        <el-card shadow="never" class="surface-card">
          <template #header><div class="card-header"><strong>最近发送</strong><small>本次打开页面的记录</small></div></template>
          <el-empty v-if="!history.length" description="还没有发送记录" :image-size="58" />
          <div v-for="item in history" :key="item.id" class="send-history-item">
            <div><strong>{{ templateNames[item.kind] }}</strong><small>{{ formatTime(item.sentAt) }} · {{ item.messageId }}</small></div>
            <el-tag :type="item.alarm ? 'danger' : (item.parsed ? 'success' : 'warning')" round>{{ item.alarm ? '已触发告警' : (item.parsed ? messageTypeLabel(item.messageType) : '处理中') }}</el-tag>
          </div>
        </el-card>

        <el-card v-if="result" shadow="never" class="surface-card result-card">
          <template #header><div class="card-header"><strong>最近一次结果</strong><el-tag v-if="result.kind" effect="plain" round>{{ templateNames[result.kind] }}</el-tag></div></template>
          <el-alert v-if="result.error" title="发送失败" :description="result.error" type="error" :closable="false" show-icon />
          <template v-else>
            <el-descriptions :column="1" border>
              <el-descriptions-item label="消息编号"><code>{{ result.messageId }}</code></el-descriptions-item>
              <el-descriptions-item label="解析状态">{{ result.rawDetail?.parseStatus ? statusLabel(result.rawDetail.parseStatus) : '已提交' }}</el-descriptions-item>
              <el-descriptions-item label="标准消息">{{ result.rawDetail?.standardMessage ? `${messageTypeLabel(result.rawDetail.standardMessage.messageType)}（${result.rawDetail.standardMessage.messageType}）` : '等待处理' }}</el-descriptions-item>
              <el-descriptions-item label="关联告警">{{ result.alarms?.length ? `${result.alarms.length} 条 · ${alarmLabel(result.alarms[0].alarmType)}` : '暂无' }}</el-descriptions-item>
            </el-descriptions>
            <pre class="result-json">{{ pretty(resultDetail(result)) }}</pre>
          </template>
        </el-card>
      </aside>
    </div>

    <el-card v-else v-loading="loading" shadow="never" class="surface-card loading-card"><el-empty description="正在准备当前租户的测试设备…" /></el-card>
  </div>
</template>

<style scoped>
.test-device-layout { display: grid; grid-template-columns: minmax(0, 1.45fr) minmax(300px, .75fr); gap: 16px; align-items: start; }
.test-device-workbench, .test-device-side { display: grid; gap: 16px; min-width: 0; }
.test-device-view code { overflow-wrap: anywhere; word-break: break-word; }
.test-device-summary { display: grid; grid-template-columns: repeat(3, 1fr); gap: 10px; margin-bottom: 16px; }
.test-device-summary > div { min-width: 0; padding: 13px; border: 1px solid var(--border); border-radius: .625rem; background: #f8fafc; }
.test-device-summary span, .test-device-summary small, .credential-box span { display: block; color: var(--muted-foreground); font-size: 11px; }
.test-device-summary strong { display: block; margin: 7px 0 3px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 14px; }
.credential-box { display: grid; gap: 6px; padding: 12px; border: 1px solid #fcd34d; border-radius: .625rem; background: #fffbeb; }
.credential-box code { overflow-wrap: anywhere; color: #92400e; }
.template-switcher { display: flex; gap: 7px; flex-wrap: wrap; margin-bottom: 11px; }
.template-switcher button { min-height: 30px; padding: 0 13px; border: 1px solid var(--border); border-radius: 999px; color: var(--muted-foreground); background: #fff; cursor: pointer; font-size: 12px; }
.template-switcher button:hover, .template-switcher button.active { border-color: var(--primary); color: var(--accent-foreground); background: var(--accent); }
.template-editor :deep(textarea) { min-height: 330px; padding: 13px; color: #dbeafe; background: #0f172a; border-color: #1e293b; border-radius: .625rem; font: 12px/1.65 "SFMono-Regular", Consolas, monospace; }
.template-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; margin-top: 12px; }
.template-actions span { color: var(--muted-foreground); font-size: 11px; }
.quick-send-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 10px; }
.quick-send { min-height: 74px; padding: 13px; display: grid; gap: 5px; text-align: left; border: 1px solid var(--border); border-radius: .625rem; background: #fff; cursor: pointer; }
.quick-send:hover:not(:disabled) { border-color: var(--primary); box-shadow: 0 2px 8px rgba(37,99,235,.1); }
.quick-send:disabled { cursor: not-allowed; opacity: .58; }
.quick-send strong { font-size: 13px; }
.quick-send small { color: var(--muted-foreground); font-size: 11px; }
.quick-send.normal { border-left: 3px solid var(--success); }.quick-send.danger { border-left: 3px solid var(--destructive); }.quick-send.warning { border-left: 3px solid var(--warning); }.quick-send.event { border-left: 3px solid var(--primary); }
.send-history-item { min-height: 58px; display: flex; align-items: center; justify-content: space-between; gap: 10px; border-bottom: 1px solid var(--border); }
.send-history-item:last-child { border-bottom: 0; }.send-history-item strong, .send-history-item small { display: block; }.send-history-item small { max-width: 190px; margin-top: 3px; overflow: hidden; color: var(--muted-foreground); text-overflow: ellipsis; white-space: nowrap; font-size: 10px; }
.result-json { max-height: 300px; margin-top: 13px; }
.loading-card { min-height: 300px; display: grid; place-items: center; }
@media (max-width: 1050px) { .test-device-layout { grid-template-columns: 1fr; }.test-device-side { grid-template-columns: repeat(2, minmax(0, 1fr)); }.result-card { grid-column: 1 / -1; } }
@media (max-width: 640px) { .test-device-summary, .quick-send-grid, .test-device-side { grid-template-columns: 1fr; }.test-device-summary strong { font-size: 13px; }.template-editor :deep(textarea) { min-height: 270px; }.send-history-item small { max-width: 150px; } }
</style>
