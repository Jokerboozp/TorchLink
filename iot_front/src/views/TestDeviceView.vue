<script setup>
import { statusLabel } from '../presentation' /* 引入当前代码需要的依赖。 */
import { computed, onMounted, reactive, ref } from 'vue' /* 引入当前代码需要的依赖。 */
import { UiMessage } from '../ui/feedback.js' /* 引入当前代码需要的依赖。 */
import { can } from '../permissions' /* 引入当前代码需要的依赖。 */
import { api, formatTime, notifyError, parseJSON, pretty, session } from '../api' /* 引入当前代码需要的依赖。 */
import { alarmType, label, messageTypeLabel, tagType, parsers } from '../labels' /* 引入当前代码需要的依赖。 */

const emit = defineEmits(['navigate']) /* 声明 emit。 */

const templateNames = { data: '正常数据', alarm: '报警数据', recovery: '恢复数据', event: '事件数据' } /* 声明 templateNames。 */
const templateDescriptions = { /* 声明 templateDescriptions。 */
  data: '用于验证设备上报、解析、设备在线状态和属性历史。', /* 执行当前语句并推进处理流程。 */
  alarm: '温度超过 80℃ 且 smoke=true，设备告警会直接进入告警中心；匹配规则可额外提供类型、等级和联动。', /* 执行当前语句并推进处理流程。 */
  recovery: '温度恢复到安全值且 smoke=false，用于验证告警自动恢复。', /* 执行当前语句并推进处理流程。 */
  event: '带 event 节点的事件上报，用于验证事件消息类型。' /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
const templateOrder = Object.keys(templateNames) /* 声明 templateOrder。 */
const templates = reactive({ data: '', alarm: '', recovery: '', event: '' }) /* 声明 templates。 */
const activeTemplate = ref('data') /* 声明 activeTemplate。 */
const device = ref(null) /* 声明 device。 */
const product = ref(null) /* 声明 product。 */
const protocolPackage = ref(null) /* 声明 protocolPackage。 */
const credential = ref(null) /* 声明 credential。 */
const loading = ref(false) /* 声明 loading。 */
const sending = ref('') /* 声明 sending。 */
const result = ref(null) /* 声明 result。 */
const history = ref([]) /* 声明 history。 */

const currentTemplate = computed({ /* 声明 currentTemplate。 */
  get: () => templates[activeTemplate.value], /* 执行当前语句并推进处理流程。 */
  set: value => { templates[activeTemplate.value] = value } /* 执行当前语句并推进处理流程。 */
}) /* 结束当前表达式或代码块。 */
const currentTemplateName = computed(() => templateNames[activeTemplate.value]) /* 声明 currentTemplateName。 */

function storageKey() { /* 定义 storageKey 函数。 */
  return `iot:test-device-templates:${session.tenant || 'default'}` /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function saveTemplates() { /* 定义 saveTemplates 函数。 */
  localStorage.setItem(storageKey(), JSON.stringify({ ...templates })) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

function restoreTemplates() { /* 定义 restoreTemplates 函数。 */
  try { /* 执行当前语句并推进处理流程。 */
    const value = JSON.parse(localStorage.getItem(storageKey()) || '{}') /* 声明 value。 */
    for (const key of templateOrder) if (typeof value[key] === 'string' && value[key].trim()) templates[key] = value[key] /* 循环处理当前数据。 */
  } catch { /* 结束当前表达式或代码块。 */
    // Ignore an invalid local draft and keep the server defaults.
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function applyServerTemplates(value) { /* 定义 applyServerTemplates 函数。 */
  for (const key of templateOrder) { /* 循环处理当前数据。 */
    if (value?.[key]) templates[key] = pretty(value[key]) /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function resetLocalTemplates() { /* 定义 resetLocalTemplates 函数。 */
  localStorage.removeItem(storageKey()) /* 执行当前语句并推进处理流程。 */
  prepare(true) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

async function prepare(reset = false) { /* 定义 prepare 函数。 */
  if (loading.value) return
  loading.value = true /* 更新 loading.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const data = await api('/api/v1/test-devices/provision', { method: 'POST', body: JSON.stringify({ reset }) }) /* 声明 data。 */
    device.value = data.device /* 更新 device.value 的值。 */
    product.value = data.product /* 更新 product.value 的值。 */
    protocolPackage.value = data.protocolPackage /* 更新 protocolPackage.value 的值。 */
    if (data.credential) credential.value = data.credential /* 判断条件并选择处理分支。 */
    applyServerTemplates(data.templates) /* 执行当前语句并推进处理流程。 */
    if (!reset) restoreTemplates() /* 判断条件并选择处理分支。 */
    if (reset) saveTemplates() /* 判断条件并选择处理分支。 */
    if (reset) { /* 判断条件并选择处理分支。 */
      history.value = [] /* 更新 history.value 的值。 */
      result.value = null /* 更新 result.value 的值。 */
      UiMessage.success('测试设备和默认报文已准备完成') /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    notifyError(error) /* 执行当前语句并推进处理流程。 */
  } finally { /* 结束当前表达式或代码块。 */
    loading.value = false /* 更新 loading.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function uniqueMessageId(kind) { /* 定义 uniqueMessageId 函数。 */
  return `raw_test_${kind}_${Date.now()}_${Math.random().toString(16).slice(2, 8)}` /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function wait(ms) { /* 定义 wait 函数。 */
  return new Promise(resolve => setTimeout(resolve, ms)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

async function loadRawDetail(messageId) { /* 定义 loadRawDetail 函数。 */
  for (let attempt = 0; attempt < 4; attempt += 1) { /* 循环处理当前数据。 */
    try { /* 执行当前语句并推进处理流程。 */
      const detail = await api(`/api/v1/raw-messages/${encodeURIComponent(messageId)}`) /* 声明 detail。 */
      if (detail.standardMessage || attempt === 3) return detail /* 判断条件并选择处理分支。 */
    } catch (error) { /* 结束当前表达式或代码块。 */
      if (attempt === 3) throw error /* 判断条件并选择处理分支。 */
    } /* 结束当前表达式或代码块。 */
    await wait(120) /* 等待异步操作完成。 */
  } /* 结束当前表达式或代码块。 */
  return null /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

async function sendTemplate(kind) { /* 定义 sendTemplate 函数。 */
  if (!device.value) return UiMessage.warning('测试设备还没有准备好') /* 判断条件并选择处理分支。 */
  let body /* 声明 body。 */
  try { /* 执行当前语句并推进处理流程。 */
    body = parseJSON(templates[kind], `${templateNames[kind]}模板`) /* 更新 body 的值。 */
    if (!body || Array.isArray(body) || typeof body !== 'object') throw new Error('报文必须是结构化数据对象') /* 判断条件并选择处理分支。 */
    if (!body.payload || typeof body.payload !== 'object') throw new Error('报文必须包含 payload 对象') /* 判断条件并选择处理分支。 */
    if (!body.messageId || String(body.messageId).includes('<unique>')) body.messageId = uniqueMessageId(kind) /* 判断条件并选择处理分支。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    notifyError(error) /* 执行当前语句并推进处理流程。 */
    return /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */

  sending.value = kind /* 更新 sending.value 的值。 */
  saveTemplates() /* 执行当前语句并推进处理流程。 */
  try { /* 执行当前语句并推进处理流程。 */
    // 测试报文走受权限保护的调试接收接口，再以归档结果确认解析状态。
    const response = await api(`/api/v1/device-registry/${encodeURIComponent(device.value.id)}/debug`, { method: 'POST', body: JSON.stringify(body) }) /* 声明 response。 */
    const messageId = response.archive?.messageId || response.messageId || body.messageId /* 声明 messageId。 */
    const rawDetail = await loadRawDetail(messageId) /* 声明 rawDetail。 */
    const alarmData = await api(`/api/v1/alarms?deviceId=${encodeURIComponent(device.value.id)}&limit=20`) /* 声明 alarmData。 */
    const relatedAlarms = (alarmData.items || []).filter(item => item.triggerId === messageId || (kind === 'alarm' || kind === 'recovery') && item.deviceId === device.value.id) /* 声明 relatedAlarms。 */
    const record = { /* 声明 record。 */
      id: `${messageId}-${Date.now()}`, /* 执行当前语句并推进处理流程。 */
      kind, /* 执行当前语句并推进处理流程。 */
      messageId, /* 执行当前语句并推进处理流程。 */
      sentAt: Date.now(), /* 执行当前语句并推进处理流程。 */
      parsed: rawDetail?.parseStatus === 'PARSED', /* 执行当前语句并推进处理流程。 */
      messageType: rawDetail?.standardMessage?.messageType || '—', /* 执行当前语句并推进处理流程。 */
      alarm: relatedAlarms.some(item => item.status === 'ACTIVE') /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
    history.value = [record, ...history.value].slice(0, 8) /* 更新 history.value 的值。 */
    result.value = { kind, messageId, response, rawDetail, alarms: relatedAlarms, sentAt: Date.now() } /* 更新 result.value 的值。 */
    UiMessage.success(`${templateNames[kind]}已发送，已进入平台处理链路`) /* 执行当前语句并推进处理流程。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    result.value = { kind, error: error?.message || String(error), sentAt: Date.now() } /* 更新 result.value 的值。 */
    notifyError(error) /* 执行当前语句并推进处理流程。 */
  } finally { /* 结束当前表达式或代码块。 */
    sending.value = '' /* 更新 sending.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function resultDetail(value) { /* 定义 resultDetail 函数。 */
  return value?.rawDetail || value?.response || { error: value?.error || '暂无结果' } /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function alarmLabel(value) { /* 定义 alarmLabel 函数。 */
  return alarmType(value) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

onMounted(() => {
  restoreTemplates()
  if (can('POST /api/v1/test-devices/provision')) void prepare(false)
})
</script>

<template>
  <div class="test-device-view"> <!-- 渲染 div 界面元素。 -->
    <div class="page-toolbar"> <!-- 渲染 div 界面元素。 -->
      <ui-button v-permission="'POST /api/v1/test-devices/provision'" type="primary" :loading="loading" @click="prepare(false)">重新准备测试设备</ui-button> <!-- 渲染 ui-button 界面元素。 -->
      <ui-button v-permission="'POST /api/v1/test-devices/provision'" plain type="warning" :loading="loading" @click="resetLocalTemplates">恢复默认配置</ui-button> <!-- 渲染 ui-button 界面元素。 -->
      <ui-button v-permission="'menu:devices'" @click="emit('navigate', 'devices')">查看设备管理</ui-button> <!-- 渲染 ui-button 界面元素。 -->
      <ui-button v-permission="'menu:alarms'" @click="emit('navigate', 'alarms')">打开告警中心</ui-button> <!-- 渲染 ui-button 界面元素。 -->
      <span v-if="can('POST /api/v1/test-devices/provision')">进入页面后自动准备测试设备；如准备失败，可点击“重新准备测试设备”重试。模拟结果不能证明现场设备已接通。</span> <!-- 渲染 span 界面元素。 -->
      <span v-else>当前账号没有准备测试设备的权限。</span>
    </div> <!-- 结束当前界面区域。 -->

    <ui-alert v-if="device" title="设备告警直接进入告警中心" description="测试设备不会自动创建告警规则；发送报警数据会直接产生设备告警。若存在匹配规则，则按规则提供告警类型、等级和联动动作。"
      type="info" :closable="false" show-icon /> <!-- 呈现当前界面内容。 -->

    <div v-if="device" class="test-device-layout top-gap"> <!-- 渲染 div 界面元素。 -->
      <section class="test-device-workbench"> <!-- 渲染 section 界面元素。 -->
        <ui-card shadow="never" class="surface-card"> <!-- 渲染 ui-card 界面元素。 -->
          <template #header>
            <div class="card-header"> <!-- 渲染 div 界面元素。 -->
              <div><strong>测试设备</strong><small>已绑定产品、已发布协议包，可直接开始发送</small></div> <!-- 渲染 div 界面元素。 -->
              <ui-tag type="success" round>设备链路已就绪</ui-tag> <!-- 渲染 ui-tag 界面元素。 -->
            </div> <!-- 结束当前界面区域。 -->
          </template>
          <div class="test-device-summary">
            <div><span>设备名称</span><strong>{{ device.name }}</strong><small>{{ device.id }}</small></div>
            <div><span>产品 / 协议</span><strong>{{ product?.name }}</strong><small>{{ label(parsers, protocolPackage?.parserType, '自定义协议') }} · {{ protocolPackage?.version }}</small></div>
            <div><span>告警处理</span><strong>直接告警 + 可选规则</strong><small>设备主动告警无需规则，规则可补充联动</small></div>
          </div>
          <ui-descriptions :column="1" border>
            <ui-descriptions-item label="设备标识"><code>{{ device.id }}</code></ui-descriptions-item>
            <ui-descriptions-item label="接入密钥"><code>{{ device.accessKey }}</code></ui-descriptions-item>
            <ui-descriptions-item label="设备状态"><ui-tag :type="tagType(device.status)" round>{{ statusLabel(device.status) }}</ui-tag></ui-descriptions-item>
            <ui-descriptions-item label="报警模板条件">temperature &gt; 80 且 smoke = true</ui-descriptions-item>
          </ui-descriptions>
          <ui-alert v-if="credential" class="top-gap" title="设备凭证已生成" description="密钥只在本次准备时返回，请仅在本地测试环境保存；页面内发送数据不需要手动填写凭证。" type="info" :closable="false" show-icon />
          <div v-if="credential" class="credential-box top-gap"><span>设备密钥</span><code>{{ credential.secret }}</code></div>
        </ui-card>

        <ui-card shadow="never" class="surface-card template-card">
          <template #header>
            <div class="card-header"><div><strong>报文模板</strong><small>{{ templateDescriptions[activeTemplate] }}</small></div><ui-tag effect="plain" round>{{ currentTemplateName }}</ui-tag></div> <!-- 渲染 div 界面元素。 -->
          </template>
          <div class="template-switcher" role="tablist" aria-label="测试报文类型">
            <button v-for="key in templateOrder" :key="key" type="button" :class="{ active: activeTemplate === key }" @click="activeTemplate = key">{{ templateNames[key] }}</button>
          </div>
          <ui-input v-model="currentTemplate" class="template-editor" type="textarea" :rows="18" spellcheck="false" aria-label="可编辑报文模板" />
          <div class="template-actions">
            <ui-button v-permission="'POST /api/v1/device-registry/:id/debug'" type="primary" :loading="sending === activeTemplate" @click="sendTemplate(activeTemplate)">发送{{ currentTemplateName }}</ui-button>
            <span>支持直接修改报文内容；带 <code>&lt;unique&gt;</code> 的消息标识会在发送时自动替换。</span>
          </div>
        </ui-card>

        <ui-card shadow="never" class="surface-card quick-send-card">
          <template #header><div class="card-header"><div><strong>快捷发送</strong><small>不打开编辑器也可以直接验证典型链路</small></div></div></template>
          <div class="quick-send-grid"> <!-- 渲染 div 界面元素。 -->
            <button type="button" class="quick-send normal" :disabled="Boolean(sending)" @click="sendTemplate('data')"><strong>发送正常数据</strong><small>属性上报 · 在线状态</small></button> <!-- 渲染 button 界面元素。 -->
            <button type="button" class="quick-send danger" :disabled="Boolean(sending)" @click="sendTemplate('alarm')"><strong>发送报警数据</strong><small>高温 + 烟雾 · 直接入告警中心</small></button> <!-- 渲染 button 界面元素。 -->
            <button type="button" class="quick-send warning" :disabled="Boolean(sending)" @click="sendTemplate('recovery')"><strong>发送恢复数据</strong><small>安全值 · 自动恢复</small></button> <!-- 渲染 button 界面元素。 -->
            <button type="button" class="quick-send event" :disabled="Boolean(sending)" @click="sendTemplate('event')"><strong>发送事件数据</strong><small>心跳事件 · 事件解析</small></button> <!-- 渲染 button 界面元素。 -->
          </div> <!-- 结束当前界面区域。 -->
        </ui-card> <!-- 结束当前界面区域。 -->
      </section> <!-- 结束当前界面区域。 -->

      <aside class="test-device-side"> <!-- 渲染 aside 界面元素。 -->
        <ui-card shadow="never" class="surface-card"> <!-- 渲染 ui-card 界面元素。 -->
          <template #header><strong>建议测试顺序</strong></template>
          <ui-steps direction="vertical" :active="4"> <!-- 渲染 ui-steps 界面元素。 -->
            <ui-step title="发送正常数据" description="确认原始报文归档、标准消息和设备在线状态。" /> <!-- 渲染 ui-step 界面元素。 -->
            <ui-step title="发送报警数据" description="确认设备告警直接进入告警中心，并验证规则联动（如已配置）。" /> <!-- 渲染 ui-step 界面元素。 -->
            <ui-step title="发送恢复数据" description="确认告警从活动中变为已恢复。" /> <!-- 渲染 ui-step 界面元素。 -->
            <ui-step title="修改模板再发送" description="验证自定义字段、事件和异常报文。" /> <!-- 渲染 ui-step 界面元素。 -->
          </ui-steps> <!-- 结束当前界面区域。 -->
        </ui-card> <!-- 结束当前界面区域。 -->

        <ui-card shadow="never" class="surface-card"> <!-- 渲染 ui-card 界面元素。 -->
          <template #header><div class="card-header"><strong>最近发送</strong><small>本次打开页面的记录</small></div></template>
          <ui-empty v-if="!history.length" description="还没有发送记录" :image-size="58" /> <!-- 渲染 ui-empty 界面元素。 -->
          <div v-for="item in history" :key="item.id" class="send-history-item"> <!-- 渲染 div 界面元素。 -->
            <div><strong>{{ templateNames[item.kind] }}</strong><small>{{ formatTime(item.sentAt) }} · {{ item.messageId }}</small></div> <!-- 渲染 div 界面元素。 -->
            <ui-tag :type="item.alarm ? 'danger' : (item.parsed ? 'success' : 'warning')" round>{{ item.alarm ? '已触发告警' : (item.parsed ? messageTypeLabel(item.messageType) : '处理中') }}</ui-tag> <!-- 渲染 ui-tag 界面元素。 -->
          </div> <!-- 结束当前界面区域。 -->
        </ui-card> <!-- 结束当前界面区域。 -->

        <ui-card v-if="result" shadow="never" class="surface-card result-card"> <!-- 渲染 ui-card 界面元素。 -->
          <template #header><div class="card-header"><strong>最近一次结果</strong><ui-tag v-if="result.kind" effect="plain" round>{{ templateNames[result.kind] }}</ui-tag></div></template>
          <ui-alert v-if="result.error" title="发送失败" :description="result.error" type="error" :closable="false" show-icon /> <!-- 渲染 ui-alert 界面元素。 -->
          <template v-else>
            <ui-descriptions :column="1" border> <!-- 渲染 ui-descriptions 界面元素。 -->
              <ui-descriptions-item label="消息编号"><code>{{ result.messageId }}</code></ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
              <ui-descriptions-item label="解析状态">{{ result.rawDetail?.parseStatus ? statusLabel(result.rawDetail.parseStatus) : '已提交' }}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
              <ui-descriptions-item label="标准消息">{{ result.rawDetail?.standardMessage ? `${messageTypeLabel(result.rawDetail.standardMessage.messageType)}（${result.rawDetail.standardMessage.messageType}）` : '等待处理' }}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
              <ui-descriptions-item label="关联告警">{{ result.alarms?.length ? `${result.alarms.length} 条 · ${alarmLabel(result.alarms[0].alarmType)}` : '暂无' }}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            </ui-descriptions> <!-- 结束当前界面区域。 -->
            <pre class="result-json">{{ pretty(resultDetail(result)) }}</pre> <!-- 渲染 pre 界面元素。 -->
          </template>
        </ui-card>
      </aside>
    </div>

    <ui-card v-else v-loading="loading" shadow="never" class="surface-card loading-card"><ui-empty description="正在准备当前租户的测试设备…" /></ui-card>
  </div>
</template>

<style scoped>
.test-device-layout { display: grid; grid-template-columns: minmax(0, 1.45fr) minmax(300px, .75fr); gap: 16px; align-items: start; } /* 定义当前元素的样式规则。 */
.test-device-workbench, .test-device-side { display: grid; gap: 16px; min-width: 0; } /* 定义当前元素的样式规则。 */
.test-device-view code { overflow-wrap: anywhere; word-break: break-word; } /* 定义当前元素的样式规则。 */
.test-device-summary { display: grid; grid-template-columns: repeat(3, 1fr); gap: 10px; margin-bottom: 16px; } /* 定义当前元素的样式规则。 */
.test-device-summary > div { min-width: 0; padding: 13px; border-radius: .625rem; background: #f5f7fa; } /* 定义当前元素的样式规则。 */
.test-device-summary span, .test-device-summary small, .credential-box span { display: block; color: var(--muted-foreground); font-size: 12px; } /* 定义当前元素的样式规则。 */
.test-device-summary strong { display: block; margin: 7px 0 3px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 14px; } /* 定义当前元素的样式规则。 */
.credential-box { display: grid; gap: 6px; padding: 12px; border: 1px solid #fcd34d; border-radius: .625rem; background: #fffbeb; } /* 定义当前元素的样式规则。 */
.credential-box code { overflow-wrap: anywhere; color: #92400e; } /* 定义当前元素的样式规则。 */
.template-switcher { display: flex; gap: 7px; flex-wrap: wrap; margin-bottom: 11px; } /* 定义当前元素的样式规则。 */
.template-switcher button { min-height: 30px; padding: 0 13px; border: 1px solid var(--border); border-radius: 999px; color: var(--muted-foreground); background: #fff; cursor: pointer; font-size: 13px; } /* 定义当前元素的样式规则。 */
.template-switcher button:hover, .template-switcher button.active { border-color: var(--primary); color: var(--accent-foreground); background: var(--accent); } /* 定义当前元素的样式规则。 */
.template-editor :deep(textarea) { min-height: 330px; padding: 13px; color: #dbeafe; background: #0f172a; border-color: #1e293b; border-radius: .625rem; font: 12px/1.65 "SFMono-Regular", Consolas, monospace; } /* 定义当前元素的样式规则。 */
.template-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; margin-top: 12px; } /* 定义当前元素的样式规则。 */
.template-actions span { color: var(--muted-foreground); font-size: 12px; } /* 定义当前元素的样式规则。 */
.quick-send-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 10px; } /* 定义当前元素的样式规则。 */
.quick-send { min-height: 74px; padding: 13px; display: grid; gap: 5px; text-align: left; border: 1px solid var(--border); border-radius: .625rem; background: #fff; cursor: pointer; } /* 定义当前元素的样式规则。 */
.quick-send:hover:not(:disabled) { border-color: var(--primary); box-shadow: 0 2px 8px rgba(37,99,235,.1); } /* 定义当前元素的样式规则。 */
.quick-send:disabled { cursor: not-allowed; opacity: .58; } /* 定义当前元素的样式规则。 */
.quick-send strong { font-size: 13px; } /* 定义当前元素的样式规则。 */
.quick-send small { color: var(--muted-foreground); font-size: 12px; } /* 定义当前元素的样式规则。 */
.quick-send.normal { border-left: 3px solid var(--success); }.quick-send.danger { border-left: 3px solid var(--destructive); }.quick-send.warning { border-left: 3px solid var(--warning); }.quick-send.event { border-left: 3px solid var(--primary); } /* 定义当前元素的样式规则。 */
.send-history-item { min-height: 58px; display: flex; align-items: center; justify-content: space-between; gap: 10px; border-bottom: 1px solid var(--border); } /* 定义当前元素的样式规则。 */
.send-history-item:last-child { border-bottom: 0; }.send-history-item strong, .send-history-item small { display: block; }.send-history-item small { max-width: 190px; margin-top: 3px; overflow: hidden; color: var(--muted-foreground); text-overflow: ellipsis; white-space: nowrap; font-size: 12px; } /* 定义当前元素的样式规则。 */
.result-json { max-height: 300px; margin-top: 13px; } /* 定义当前元素的样式规则。 */
.loading-card { min-height: 300px; display: grid; place-items: center; } /* 定义当前元素的样式规则。 */
@media (max-width: 1050px) { .test-device-layout { grid-template-columns: 1fr; }.test-device-side { grid-template-columns: repeat(2, minmax(0, 1fr)); }.result-card { grid-column: 1 / -1; } } /* 按屏幕条件调整样式。 */
@media (max-width: 640px) { .test-device-summary, .quick-send-grid, .test-device-side { grid-template-columns: 1fr; }.test-device-summary strong { font-size: 13px; }.template-editor :deep(textarea) { min-height: 270px; }.send-history-item small { max-width: 150px; } } /* 按屏幕条件调整样式。 */
</style>
