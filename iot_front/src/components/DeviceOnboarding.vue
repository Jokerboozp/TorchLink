<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { api, apiAll, formatTime, notifyError, session } from '../api'
import { can } from '../permissions'
import { createClientId } from '../clientId'
import { categories } from '../labels'
import { statusLabel, transportLabel } from '../presentation'
import { UiMessage } from '../ui/feedback.js'
import { CheckCircle2, CircleDashed, Copy, XCircle } from '@lucide/vue'
import ProtocolAssistantView from '../views/ProtocolAssistantView.vue'
import { configurationText, diagnosisTagTypes, enrollRequest, fieldConfiguration, modeLabels, preflightQuery, protocolOptions, transportChoices, usesPlatformIdentity } from '../onboardingPlan'

const emit = defineEmits(['close', 'done', 'navigate', 'detail'])
const steps = [
  { title: '选择型号', hint: '确定模板和接入方式' },
  { title: '设备与连接', hint: '填写编号和连接参数' },
  { title: '现场配置与验证', hint: '设备上报并确认结果' }
]
const randomId = prefix => `${prefix}_${createClientId().replaceAll('-', '').slice(0, 12)}`
const blankConnection = () => ({ choice: '', transport: '', network: '', port: null, publicHost: '', bindHost: '', host: '', unitId: 1, timeoutMs: 3000 })
const fresh = () => ({
  step: 0, source: 'existing', productId: '',
  newProduct: { id: randomId('product'), name: '', category: 'other', protocolPackageId: '', transport: '', manufacturer: '', model: '' },
  device: { id: '', name: '', role: '', description: '' }, labels: [], connection: blankConnection(),
  requestId: createClientId(), result: null, checkSince: 0
})
const draft = reactive(fresh())
const products = ref([]), protocols = ref([]), loading = ref(false), loadError = ref('')
const preflight = ref(null), checking = ref(false), preflightError = ref('')
const saving = ref(false), saveError = ref('')
// 设备密钥只保存在内存中，刷新页面或离开向导后不可再读取。
const credential = ref(null)
const status = ref(null), statusError = ref(''), statusAt = ref(0), refreshing = ref(false)
const protocolHelper = ref(false)

const storageKey = () => `iot:device-onboarding:v2:${session.tenant}:${session.user}`
const product = computed(() => products.value.find(item => item.id === draft.productId))
const plan = computed(() => preflight.value?.plan || null)
const mode = computed(() => plan.value?.mode === 'listener' && draft.connection.choice === 'dial' ? 'dial' : plan.value?.mode || '')
const category = computed(() => draft.source === 'existing' ? product.value?.category : draft.newProduct.category)
const templateName = computed(() => draft.source === 'existing' ? product.value?.name || draft.productId : draft.newProduct.name.trim() || '新型号')
const selectedProtocol = computed(() => protocols.value.find(item => item.id === draft.newProduct.protocolPackageId))
const templateTransports = computed(() => transportChoices(selectedProtocol.value?.transport))
const canCreateTemplate = computed(() => can('POST /api/v1/products'))
const canCreateListener = computed(() => can('POST /api/v2/device-access-profiles'))
const listeners = computed(() => preflight.value?.profiles || [])
const canContinue = computed(() => Boolean(preflight.value?.ready) && !checking.value && (draft.source === 'existing' ? Boolean(product.value) : Boolean(draft.newProduct.name.trim())))
const liveProfile = computed(() => status.value?.profile || draft.result?.profile || null)
const accessInfo = computed(() => status.value?.accessInfo || draft.result?.accessInfo || null)
const configuration = computed(() => fieldConfiguration({ ...draft.result, profile: liveProfile.value }, accessInfo.value, credential.value))
// 密钥只在上方提示框中显示一次；“复制全部”仍包含它。
const visibleConfiguration = computed(() => configuration.value.filter(row => row.name !== 'Secret' && !(credential.value && row.name === 'AccessKey')))
const diagnosis = computed(() => status.value?.diagnosis || null)
const checkIcons = { passed: CheckCircle2, waiting: CircleDashed, failed: XCircle }
const checkStates = { passed: '通过', waiting: '等待', failed: '未通过' }
const values = computed(() => Object.entries(status.value?.ingest?.standardMessage?.properties || {}).map(([id, value]) => {
  const field = status.value?.product?.thingModel?.properties?.find(item => item.identifier === id)
  return { name: field?.name || id, unit: field?.unit || '', value: typeof value === 'object' ? JSON.stringify(value) : String(value) }
}))

function usableListener(profile) { return profile.enabled && Boolean(profile.publicHost) && (plan.value?.networks || []).includes(profile.network) }
function listenerLabel(profile) { return `${profile.publicHost || '未配置对外地址'}:${profile.port}` }

function persist() {
  const saved = JSON.parse(JSON.stringify(draft))
  saved.labels = saved.labels.filter(row => !/(secret|token|password|access.?key|密钥|令牌|密码)/i.test(row.key || ''))
  try { localStorage.setItem(storageKey(), JSON.stringify(saved)) } catch { /* 存储不可用时只影响草稿恢复。 */ }
}
function restore() {
  try {
    const saved = JSON.parse(localStorage.getItem(storageKey()) || 'null')
    if (saved && typeof saved === 'object' && saved.requestId) Object.assign(draft, fresh(), saved, { connection: { ...blankConnection(), ...saved.connection } })
  } catch { forget() }
}
function forget() { try { localStorage.removeItem(storageKey()) } catch { /* 忽略存储错误。 */ } }
watch(draft, persist, { deep: true })
watch(() => draft.step, async () => { await nextTick(); document.querySelector('.app-content')?.scrollTo({ top: 0 }) })

async function load() {
  loading.value = true; loadError.value = ''
  try {
    const [productData, catalog] = await Promise.all([apiAll('/api/v1/products'), api('/api/v2/protocols')])
    products.value = productData.items || []
    protocols.value = protocolOptions(catalog.items || [])
    if (draft.step < 2) await runPreflight()
    else await refreshStatus()
  } catch (cause) { loadError.value = cause.message || '读取设备模板失败' }
  finally { loading.value = false }
}

let preflightVersion = 0
async function runPreflight() {
  const version = ++preflightVersion
  preflight.value = null; preflightError.value = ''
  if (draft.source === 'existing' ? !draft.productId : !draft.newProduct.protocolPackageId) { checking.value = false; return }
  checking.value = true
  try {
    const result = await api(`/api/v1/onboarding/preflight?${preflightQuery(draft)}`)
    if (version !== preflightVersion) return
    preflight.value = result
    prepareConnection(result.plan)
  } catch (cause) { if (version === preflightVersion) preflightError.value = cause.message || '接入预检失败' }
  finally { if (version === preflightVersion) checking.value = false }
}
function prepareConnection(p) {
  const c = draft.connection
  if (p.mode === 'standard' && !['MQTT', 'HTTP'].includes(c.transport)) c.transport = p.connector
  if (p.mode === 'listener') {
    if (!(p.networks || []).includes(c.network)) c.network = p.networks?.[0] || ''
    const known = c.choice === 'new' || (c.choice === 'dial' && p.dial) || listeners.value.some(item => item.id === c.choice)
    if (!known) c.choice = listeners.value.find(usableListener)?.id || (canCreateListener.value ? 'new' : p.dial ? 'dial' : '')
  } else c.choice = ''
  if (p.mode === 'poll' && !c.port) c.port = 502
}
function selectSource(value) {
  if (value === 'new' && !canCreateTemplate.value) return UiMessage.warning('当前账号不能新建设备模板，请选择已有型号')
  draft.source = value
  draft.connection = blankConnection()
  runPreflight()
}
function chooseProduct() { draft.connection = blankConnection(); runPreflight() }
function chooseProtocol() {
  draft.newProduct.transport = transportChoices(selectedProtocol.value?.transport)[0] || ''
  draft.connection = blankConnection()
  runPreflight()
}
// 模板通道决定新设备默认的上报通道。
function changeTemplateTransport() { draft.connection.transport = ''; runPreflight() }
function next() {
  if (!preflight.value) return UiMessage.warning('请先选择设备型号')
  if (!preflight.value.ready) return UiMessage.warning(plan.value?.reason || '该型号暂不能添加设备')
  if (draft.source === 'new' && !draft.newProduct.name.trim()) return UiMessage.warning('请填写新型号的模板名称')
  if (!draft.device.role) draft.device.role = category.value === 'gateway' ? 'GATEWAY' : 'DIRECT'
  draft.step = 1
}
function usePlatformId() { draft.device.id = randomId('device') }
const validPort = value => Number.isInteger(Number(value)) && Number(value) >= 1 && Number(value) <= 65535
function validate() {
  if (!draft.device.name.trim()) return '请填写设备名称'
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(draft.device.id.trim())) return '设备编号须为 1 至 128 位字母、数字、点、横线或下划线，且以字母或数字开头'
  const c = draft.connection
  if (mode.value === 'listener') {
    if (!c.choice) return '请选择接入点，或新建共享监听'
    if (c.choice === 'new' && !c.publicHost.trim()) return '请填写现场设备可访问的平台对外地址'
    if (c.choice === 'new' && !validPort(c.port)) return '请填写 1 至 65535 之间的监听端口'
  }
  if (mode.value === 'dial' || mode.value === 'poll') {
    if (!c.host.trim()) return '请填写平台可以访问的设备 IP 或域名'
    if ((mode.value === 'dial' || c.port) && !validPort(c.port)) return '请填写 1 至 65535 之间的设备端口'
  }
  if (mode.value === 'poll' && !(Number(c.unitId) >= 0 && Number(c.unitId) <= 255)) return '站号须在 0 至 255 之间'
  return ''
}
async function submit() {
  if (saving.value) return
  const problem = validate()
  if (problem) return UiMessage.warning(problem)
  saving.value = true; saveError.value = ''
  try {
    const result = await api('/api/v1/onboarding', { method: 'POST', body: JSON.stringify(enrollRequest(draft, plan.value)) })
    credential.value = result.credential?.secret ? result.credential : null
    draft.result = { device: result.device, product: { id: result.product?.id, name: result.product?.name }, mode: result.mode, profile: result.profile || null, accessInfo: result.accessInfo || null, reused: Boolean(result.reused) }
    draft.checkSince = Number(result.device?.createdAt || Date.now())
    draft.step = 2
    status.value = null
    if (result.reused) UiMessage.info('这台设备此前已添加，已恢复接入信息；设备密钥只在首次创建时显示。')
    await refreshStatus()
  } catch (cause) { saveError.value = cause.message || '保存失败，请稍后重试' }
  finally { saving.value = false }
}

let statusVersion = 0
async function refreshStatus() {
  const id = draft.result?.device?.id
  if (!id) return
  const version = ++statusVersion
  refreshing.value = true
  try {
    const data = await api(`/api/v1/device-registry/${encodeURIComponent(id)}/connection?since=${encodeURIComponent(draft.checkSince || Date.now())}`)
    if (version !== statusVersion) return
    status.value = data; statusError.value = ''; statusAt.value = Date.now()
  } catch (cause) { if (version === statusVersion) statusError.value = cause.message || '读取接入状态失败' }
  finally { if (version === statusVersion) refreshing.value = false }
}
let timer = 0, realtimeTimer = 0
function stopPolling() { clearInterval(timer); timer = 0 }
function schedulePolling() {
  stopPolling()
  if (draft.step === 2 && draft.result) timer = setInterval(() => { if (document.visibilityState !== 'hidden') refreshStatus() }, 5000)
}
watch(() => [draft.step, draft.result?.device?.id], schedulePolling)
function realtime(event) {
  const id = draft.result?.device?.id
  if (draft.step !== 2 || !id) return
  const text = `${event.detail?.topic || ''} ${typeof event.detail?.payload === 'string' ? event.detail.payload : JSON.stringify(event.detail?.payload || '')}`
  if (!text.includes(id)) return
  clearTimeout(realtimeTimer)
  realtimeTimer = setTimeout(refreshStatus, 800)
}

async function copy(text, message = '已复制') {
  try { await navigator.clipboard.writeText(text); UiMessage.success(message) }
  catch { UiMessage.warning('浏览器不允许复制，请手动选择文本') }
}
function copyAll() { copy(configurationText(draft.result, accessInfo.value, credential.value), '接入信息已复制') }
function openRaw() { persist(); emit('navigate', 'raw', { deviceId: draft.result.device.id, rawMessageId: status.value?.ingest?.rawMessageId }) }
function addAnother() {
  const result = draft.result
  const choice = result?.mode === 'listener' && result.profile?.id ? result.profile.id : ''
  Object.assign(draft, { step: 1, source: 'existing', productId: result?.product?.id || draft.productId, device: { id: '', name: '', role: draft.device.role, description: '' }, labels: [], connection: { ...blankConnection(), choice, transport: draft.connection.transport }, requestId: createClientId(), result: null, checkSince: 0 })
  credential.value = null; status.value = null; saveError.value = ''
  load()
}
function close() { if (draft.step === 2) forget(); emit('close') }
function finish() { forget(); emit('done') }
function openDetail() { const id = draft.result?.device?.id; forget(); emit('detail', id) }
function restart() { forget(); Object.assign(draft, fresh()); credential.value = null; status.value = null; preflight.value = null; saveError.value = ''; preflightError.value = '' }
async function protocolSaved() { await load() }
function navigate(page) { persist(); emit('navigate', page) }

onMounted(() => { restore(); load(); window.addEventListener('iot:realtime', realtime); schedulePolling() })
onBeforeUnmount(() => { stopPolling(); clearTimeout(realtimeTimer); window.removeEventListener('iot:realtime', realtime) })
</script>

<template>
  <section class="onboarding">
    <header class="onboarding__header">
      <div>
        <h2>添加设备</h2>
        <p>选择型号，填写设备编号和连接方式，再按提示在现场设备上完成配置并确认数据。</p>
      </div>
      <div class="onboarding__header-actions">
        <ui-button v-if="draft.step < 2" @click="restart">重新开始</ui-button>
        <ui-button @click="close">返回设备列表</ui-button>
      </div>
    </header>

    <ol class="onboarding__steps" aria-label="添加设备进度">
      <li v-for="(item, index) in steps" :key="item.title" :class="{ 'is-active': draft.step === index, 'is-done': draft.step > index }" :aria-current="draft.step === index ? 'step' : undefined">
        <span class="onboarding__step-number">{{ index + 1 }}</span>
        <span class="onboarding__step-copy"><strong>{{ item.title }}</strong><small>{{ item.hint }}</small></span>
      </li>
    </ol>

    <ui-alert v-if="loadError" class="onboarding__alert" :title="loadError" type="error" :closable="false" show-icon />

    <!-- 第 1 步：型号与接入方式 -->
    <section v-if="draft.step === 0" class="onboarding__card" aria-labelledby="onboarding-step-template">
      <h3 id="onboarding-step-template" class="onboarding__title">这台设备是什么型号？</h3>
      <ui-radio-group :model-value="draft.source" class="segmented-choice-group" aria-label="型号来源" @update:model-value="selectSource">
        <ui-radio-button value="existing">已有型号</ui-radio-button>
        <ui-radio-button value="new" :disabled="!canCreateTemplate" :title="canCreateTemplate ? '' : '当前账号不能新建设备模板'">新型号</ui-radio-button>
      </ui-radio-group>

      <div v-if="draft.source === 'existing'" class="onboarding__fields">
        <ui-form-item label="设备模板" required>
          <ui-select v-model="draft.productId" filterable placeholder="按名称选择设备模板" aria-label="设备模板" :loading="loading" @change="chooseProduct">
            <ui-option v-for="item in products" :key="item.id" :value="item.id" :label="`${item.name || item.id}${item.status === 'ENABLED' ? '' : '（未启用）'}`" :disabled="item.status !== 'ENABLED'" />
          </ui-select>
        </ui-form-item>
        <p v-if="!loading && !products.length" class="onboarding__muted">还没有设备模板。<ui-button v-if="canCreateTemplate" link type="primary" @click="selectSource('new')">新建型号</ui-button></p>
      </div>

      <div v-else class="onboarding__fields">
        <div class="onboarding__grid">
          <ui-form-item label="模板名称" required><ui-input v-model="draft.newProduct.name" maxlength="256" placeholder="例如 厂商 + 型号" aria-label="模板名称" /></ui-form-item>
          <ui-form-item label="设备分类">
            <ui-select v-model="draft.newProduct.category" aria-label="设备分类" @change="runPreflight"><ui-option v-for="(text, key) in categories" :key="key" :value="key" :label="text" /></ui-select>
          </ui-form-item>
          <ui-form-item label="通信协议" required>
            <ui-select v-model="draft.newProduct.protocolPackageId" filterable placeholder="选择已发布的协议版本" aria-label="通信协议" @change="chooseProtocol"><ui-option v-for="item in protocols" :key="item.id" :value="item.id" :label="item.name" /></ui-select>
          </ui-form-item>
          <ui-form-item v-if="templateTransports.length" label="上报通道">
            <ui-radio-group v-model="draft.newProduct.transport" class="segmented-choice-group" aria-label="上报通道" @change="changeTemplateTransport"><ui-radio-button v-for="item in templateTransports" :key="item" :value="item">{{ item }}</ui-radio-button></ui-radio-group>
          </ui-form-item>
        </div>
        <details class="onboarding__more">
          <summary>型号信息与模板标识（选填）</summary>
          <div class="onboarding__grid">
            <ui-form-item label="厂商"><ui-input v-model="draft.newProduct.manufacturer" /></ui-form-item>
            <ui-form-item label="型号"><ui-input v-model="draft.newProduct.model" /></ui-form-item>
            <ui-form-item label="模板标识"><ui-input v-model="draft.newProduct.id" placeholder="创建后不可修改" /></ui-form-item>
          </div>
        </details>
        <div class="onboarding__hint-row">
          <span>找不到匹配的协议？可以用报文或点表生成，复杂协议在协议页面上传 Go 源码。</span>
          <ui-button v-permission="'POST /api/v1/ai/protocol-assistant/generate'" size="small" @click="protocolHelper = true">生成协议</ui-button>
          <ui-button v-permission="'menu:protocols'" size="small" @click="navigate('protocols')">前往协议页面</ui-button>
        </div>
      </div>

      <section v-if="checking || preflight || preflightError" class="onboarding__preflight" aria-live="polite">
        <p v-if="checking" class="onboarding__muted">正在检查接入条件…</p>
        <ui-alert v-else-if="preflightError" :title="preflightError" type="warning" :closable="false" show-icon />
        <template v-else-if="preflight">
          <div class="onboarding__plan">
            <span>接入方式</span>
            <strong>{{ modeLabels[plan.mode] || plan.mode }}</strong>
            <small v-if="plan.protocol?.id">协议 {{ plan.protocol.id }} · {{ plan.protocol.version }}</small>
          </div>
          <ul class="onboarding__checks">
            <li v-for="check in preflight.checks" :key="check.key" :class="`is-${check.state}`">
              <component :is="check.state === 'passed' ? CheckCircle2 : check.state === 'failed' ? XCircle : CircleDashed" class="onboarding__check-icon" aria-hidden="true" />
              <span><strong>{{ check.label }}</strong>{{ check.detail }}</span>
            </li>
          </ul>
        </template>
      </section>

      <footer class="onboarding__actions">
        <ui-button type="primary" :disabled="!canContinue" @click="next">下一步</ui-button>
      </footer>
    </section>

    <!-- 第 2 步：设备身份与连接 -->
    <section v-else-if="draft.step === 1" class="onboarding__card" aria-labelledby="onboarding-step-device">
      <h3 id="onboarding-step-device" class="onboarding__title">设备与连接</h3>
      <p class="onboarding__summary"><strong>{{ templateName }}</strong><span>{{ modeLabels[mode] || '接入方式待确认' }}</span></p>
      <ui-alert v-if="!plan" title="接入条件未确认，请返回上一步重新选择型号。" type="warning" :closable="false" show-icon />

      <div class="onboarding__grid">
        <ui-form-item label="设备名称" required><ui-input v-model="draft.device.name" maxlength="256" placeholder="例如 一层东侧烟感" aria-label="设备名称" /></ui-form-item>
        <ui-form-item label="设备编号" required>
          <div class="onboarding__id-field">
            <ui-input v-model="draft.device.id" maxlength="128" placeholder="设备上报使用的编号" aria-label="设备编号" />
            <ui-button v-if="usesPlatformIdentity(mode)" @click="usePlatformId">使用平台编号</ui-button>
          </div>
        </ui-form-item>
      </div>
      <p class="onboarding__muted">{{ usesPlatformIdentity(mode) ? '标准或 HTTP 接口上报的设备可以使用平台生成的编号；编号保存后不可修改。' : '编号须与协议从报文中识别出的设备标识一致，保存后不可修改。' }}</p>
      <ui-form-item label="设备角色">
        <ui-radio-group v-model="draft.device.role" class="segmented-choice-group" aria-label="设备角色">
          <ui-radio-button value="DIRECT">独立设备</ui-radio-button>
          <ui-radio-button value="GATEWAY">主设备（下接子设备）</ui-radio-button>
        </ui-radio-group>
      </ui-form-item>

      <section class="onboarding__section">
        <h4>连接方式</h4>
        <template v-if="plan?.mode === 'standard'">
          <ui-form-item label="上报通道">
            <ui-radio-group v-model="draft.connection.transport" class="segmented-choice-group" aria-label="上报通道"><ui-radio-button value="MQTT">MQTT</ui-radio-button><ui-radio-button value="HTTP">HTTP</ui-radio-button></ui-radio-group>
          </ui-form-item>
          <p class="onboarding__muted">平台为设备签发 AccessKey 和 Secret，保存后显示设备端需要填写的地址和示例。</p>
        </template>
        <p v-else-if="plan?.mode === 'managed'" class="onboarding__muted">设备使用平台签发的 AccessKey 和 Secret，通过 HTTP 接口上报原始数据，平台按模板协议解析。</p>
        <template v-else-if="plan?.mode === 'listener'">
          <div class="onboarding__options" role="radiogroup" aria-label="接入点">
            <label v-for="item in listeners" :key="item.id" class="onboarding__option" :class="{ 'is-selected': draft.connection.choice === item.id, 'is-disabled': !usableListener(item) }">
              <input v-model="draft.connection.choice" type="radio" name="listener" :value="item.id" :disabled="!usableListener(item)" />
              <span><strong>{{ listenerLabel(item) }}</strong><small>{{ transportLabel(String(item.network).toUpperCase()) }} · {{ item.enabled ? statusLabel(item.runtimeStatus || 'PENDING') : '已停用' }}<template v-if="!item.publicHost"> · 需先在接入点补齐对外地址</template></small></span>
            </label>
            <label v-if="canCreateListener" class="onboarding__option" :class="{ 'is-selected': draft.connection.choice === 'new' }">
              <input v-model="draft.connection.choice" type="radio" name="listener" value="new" />
              <span><strong>新建共享监听</strong><small>同型号的其他设备可继续使用这个端口</small></span>
            </label>
            <label v-if="plan.dial" class="onboarding__option" :class="{ 'is-selected': draft.connection.choice === 'dial' }">
              <input v-model="draft.connection.choice" type="radio" name="listener" value="dial" />
              <span><strong>平台主动连接设备</strong><small>设备作为 TCP 服务端，平台按地址连接</small></span>
            </label>
          </div>
          <p v-if="!listeners.length && !canCreateListener && !plan.dial" class="onboarding__muted">该型号还没有可用接入点，请联系管理员在设备模板的“接入点”中创建。</p>
          <div v-if="draft.connection.choice === 'new'" class="onboarding__grid">
            <ui-form-item v-if="(plan.networks || []).length > 1" label="网络">
              <ui-radio-group v-model="draft.connection.network" class="segmented-choice-group" aria-label="网络"><ui-radio-button v-for="item in plan.networks" :key="item" :value="item">{{ item.toUpperCase() }}</ui-radio-button></ui-radio-group>
            </ui-form-item>
            <ui-form-item label="平台对外地址" required><ui-input v-model="draft.connection.publicHost" placeholder="现场设备可访问的域名或 IP" aria-label="平台对外地址" /></ui-form-item>
            <ui-form-item label="监听端口" required><ui-input-number v-model="draft.connection.port" :min="1" :max="65535" placeholder="例如 26875" aria-label="监听端口" /></ui-form-item>
            <ui-form-item label="本机监听地址"><ui-input v-model="draft.connection.bindHost" placeholder="默认 0.0.0.0" aria-label="本机监听地址" /></ui-form-item>
          </div>
        </template>
        <div v-if="mode === 'dial' || mode === 'poll'" class="onboarding__grid">
          <ui-form-item label="设备地址" required><ui-input v-model="draft.connection.host" placeholder="平台可以访问的设备 IP 或域名" aria-label="设备地址" /></ui-form-item>
          <ui-form-item :label="mode === 'poll' ? '端口' : '设备端口'" :required="mode === 'dial'"><ui-input-number v-model="draft.connection.port" :min="1" :max="65535" :placeholder="mode === 'poll' ? '默认 502' : ''" aria-label="设备端口" /></ui-form-item>
          <template v-if="mode === 'poll'">
            <ui-form-item label="站号"><ui-input-number v-model="draft.connection.unitId" :min="0" :max="255" aria-label="站号" /></ui-form-item>
            <ui-form-item label="超时（毫秒）"><ui-input-number v-model="draft.connection.timeoutMs" :min="100" :max="10000" :step="500" aria-label="超时" /></ui-form-item>
          </template>
        </div>
      </section>

      <details class="onboarding__more">
        <summary>安装位置与标签（选填）</summary>
        <ui-form-item label="安装位置 / 备注"><ui-input v-model="draft.device.description" type="textarea" :rows="2" maxlength="1024" /></ui-form-item>
        <div v-for="(row, index) in draft.labels" :key="index" class="onboarding__label-row">
          <ui-input v-model="row.key" placeholder="名称，例如楼层" aria-label="标签名称" />
          <ui-input v-model="row.value" placeholder="内容，例如一层" aria-label="标签内容" />
          <ui-button text @click="draft.labels.splice(index, 1)">移除</ui-button>
        </div>
        <ui-button size="small" @click="draft.labels.push({ key: '', value: '' })">添加标签</ui-button>
      </details>

      <ui-alert v-if="saveError" class="onboarding__alert" :title="saveError" type="error" :closable="false" show-icon />
      <footer class="onboarding__actions">
        <ui-button :disabled="saving" @click="draft.step = 0">上一步</ui-button>
        <ui-button type="primary" :loading="saving" :disabled="!plan" @click="submit">保存并生成接入信息</ui-button>
      </footer>
    </section>

    <!-- 第 3 步：现场配置与验证 -->
    <section v-else class="onboarding__card" aria-labelledby="onboarding-step-verify">
      <h3 id="onboarding-step-verify" class="onboarding__title">现场配置与验证</h3>
      <p class="onboarding__summary"><strong>{{ draft.result?.device?.name }}</strong><span>{{ draft.result?.device?.id }} · {{ draft.result?.product?.name }} · {{ modeLabels[draft.result?.mode] || '' }}</span></p>

      <div v-if="credential" class="onboarding__secret" role="alert">
        <p><strong>设备密钥只显示这一次</strong>请立即复制并交给现场人员，关闭或刷新页面后无法再次读取。</p>
        <div class="onboarding__kv"><span>AccessKey</span><code>{{ credential.accessKey }}</code><ui-button text aria-label="复制 AccessKey" @click="copy(credential.accessKey)"><Copy /></ui-button></div>
        <div class="onboarding__kv"><span>Secret</span><code>{{ credential.secret }}</code><ui-button text aria-label="复制 Secret" @click="copy(credential.secret)"><Copy /></ui-button></div>
        <ui-button size="small" @click="credential = null">我已保存</ui-button>
      </div>
      <p v-else-if="accessInfo" class="onboarding__muted">设备密钥只在首次创建时显示；如已丢失，请在设备详情中重新生成凭据。</p>

      <section class="onboarding__section">
        <div class="onboarding__section-head">
          <h4>{{ mode === 'dial' || draft.result?.mode === 'dial' || draft.result?.mode === 'poll' ? '平台连接设置' : '在现场设备上填写' }}</h4>
          <ui-button size="small" @click="copyAll"><Copy />复制全部</ui-button>
        </div>
        <div class="onboarding__config">
          <div v-for="row in visibleConfiguration" :key="row.name" class="onboarding__kv"><span>{{ row.name }}</span><code>{{ row.value }}</code></div>
        </div>
        <p v-if="draft.result?.mode === 'listener'" class="onboarding__muted">设备连接上述地址后，平台按协议识别设备编号 {{ draft.result?.device?.id }}。</p>
        <p v-else-if="draft.result?.mode === 'dial' || draft.result?.mode === 'poll'" class="onboarding__muted">平台会主动连接设备，现场只需确认设备地址、端口{{ draft.result?.mode === 'poll' ? '和站号' : '' }}可达。</p>
        <details v-if="accessInfo?.sample" class="onboarding__more"><summary>示例报文</summary><pre>{{ JSON.stringify(accessInfo.sample, null, 2) }}</pre></details>
      </section>

      <section class="onboarding__section onboarding__verify" aria-live="polite">
        <div class="onboarding__section-head">
          <h4>接入验证</h4>
          <span class="onboarding__muted">每 5 秒自动刷新{{ statusAt ? ` · ${formatTime(statusAt)}` : '' }}</span>
          <ui-button size="small" :loading="refreshing" @click="refreshStatus">立即刷新</ui-button>
        </div>
        <ui-alert v-if="statusError" :title="statusError" type="warning" :closable="false" show-icon />
        <template v-if="diagnosis">
          <div class="onboarding__diagnosis" :class="`is-${diagnosis.tone}`">
            <ui-tag :type="diagnosisTagTypes[diagnosis.tone]">{{ diagnosis.title }}</ui-tag>
            <p>{{ diagnosis.nextAction }}</p>
            <small v-if="diagnosis.previousParsedAt">上次成功解析：{{ formatTime(diagnosis.previousParsedAt) }}</small>
          </div>
          <ol class="onboarding__progress">
            <li v-for="check in diagnosis.checks" :key="check.key" :class="`is-${check.state}`">
              <component :is="checkIcons[check.state]" class="onboarding__check-icon" :aria-label="checkStates[check.state]" />
              <strong>{{ check.label }}</strong>
              <small>{{ check.detail }}{{ check.at ? ` · ${formatTime(check.at)}` : '' }}</small>
            </li>
          </ol>
        </template>
        <p v-else-if="!statusError" class="onboarding__muted">正在读取接入状态…</p>
        <details v-if="status?.profile?.lastError"><summary>接入点最近错误</summary><pre>{{ status.profile.lastError }}</pre></details>
        <details v-if="status?.ingest?.parseError"><summary>解析错误</summary><pre>{{ status.ingest.parseError }}</pre></details>
        <p v-if="status?.ingest?.simulationCount" class="onboarding__muted">另有 {{ status.ingest.simulationCount }} 条测试或管理接口上报，不计入本次验证。</p>
        <div v-if="values.length" class="onboarding__values">
          <h5>最近解析的数据</h5>
          <div v-for="item in values" :key="item.name" class="onboarding__kv"><span>{{ item.name }}</span><code>{{ item.value }}{{ item.unit ? ` ${item.unit}` : '' }}</code></div>
        </div>
      </section>

      <footer class="onboarding__actions">
        <ui-button v-if="status?.ingest?.rawMessageId" v-permission="'menu:raw'" @click="openRaw">查看原始报文</ui-button>
        <ui-button @click="openDetail">设备详情</ui-button>
        <ui-button v-permission="'POST /api/v1/device-registry'" @click="addAnother">继续添加同型号设备</ui-button>
        <ui-button type="primary" @click="finish">完成</ui-button>
      </footer>
    </section>

    <ui-drawer :model-value="protocolHelper" title="从报文或点表生成协议" size="min(780px, 100vw)" @close="protocolHelper = false">
      <p class="onboarding__muted">发布协议后关闭抽屉，即可在“通信协议”中选择。</p>
      <ProtocolAssistantView v-if="protocolHelper" @saved="protocolSaved" @navigate="protocolHelper = false" />
    </ui-drawer>
  </section>
</template>

<style scoped>
.onboarding { max-width: 960px; margin: 0 auto; color: var(--text); }
.onboarding__header { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--space-4); margin-bottom: var(--space-4); }
.onboarding__header h2 { margin: 0; color: var(--text-strong); font-size: var(--font-size-xl); font-weight: var(--font-weight-semibold); }
.onboarding__header p { margin: var(--space-1) 0 0; color: var(--text-muted); font-size: var(--font-size-sm); }
.onboarding__header-actions { display: flex; flex: none; flex-wrap: wrap; gap: var(--space-2); }
.onboarding__steps { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--space-2); margin: 0 0 var(--space-4); padding: 0; list-style: none; }
.onboarding__steps li { display: flex; align-items: center; gap: var(--space-3); min-width: 0; padding: var(--space-3); background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-lg); }
.onboarding__steps li.is-active { border-color: var(--primary-border); box-shadow: inset 0 -2px 0 var(--primary); }
.onboarding__step-number { display: grid; flex: none; place-items: center; width: 24px; height: 24px; color: var(--text-secondary); background: var(--surface-muted); border-radius: var(--radius-full); font-size: var(--font-size-xs); font-weight: var(--font-weight-semibold); }
.is-active .onboarding__step-number { color: var(--text-inverse); background: var(--primary); }
.is-done .onboarding__step-number { color: var(--success-text); background: var(--success-soft); }
.onboarding__step-copy { display: grid; min-width: 0; }
.onboarding__step-copy strong { color: var(--text-strong); font-size: var(--font-size-sm); font-weight: var(--font-weight-semibold); }
.onboarding__step-copy small { color: var(--text-muted); font-size: var(--font-size-xs); }
.onboarding__card { padding: var(--space-5) var(--space-6); background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-lg); box-shadow: var(--shadow-xs); }
.onboarding__title { margin: 0 0 var(--space-4); color: var(--text-strong); font-size: var(--font-size-lg); font-weight: var(--font-weight-semibold); }
.onboarding__fields { margin-top: var(--space-4); }
.onboarding__grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 var(--space-4); }
.onboarding__grid > * { min-width: 0; }
.onboarding__grid :deep(.ui-input-number) { width: 100%; }
.onboarding__muted { margin: var(--space-1) 0 var(--space-3); color: var(--text-muted); font-size: var(--font-size-sm); }
.onboarding__alert { margin: var(--space-3) 0; }
.onboarding__summary { display: flex; flex-wrap: wrap; align-items: baseline; gap: var(--space-1) var(--space-3); margin: 0 0 var(--space-4); padding: var(--space-3) var(--space-4); background: var(--surface-muted); border-radius: var(--radius-md); font-size: var(--font-size-sm); }
.onboarding__summary strong { color: var(--text-strong); font-weight: var(--font-weight-semibold); }
.onboarding__summary span { color: var(--text-secondary); overflow-wrap: anywhere; }
.onboarding__hint-row { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); padding: var(--space-3) 0 0; color: var(--text-muted); font-size: var(--font-size-sm); }
.onboarding__hint-row > span { flex: 1 1 280px; }
.onboarding__more { margin: var(--space-3) 0; }
.onboarding__more summary { width: max-content; max-width: 100%; color: var(--primary-text); font-size: var(--font-size-sm); font-weight: var(--font-weight-medium); cursor: pointer; }
.onboarding__more[open] summary { margin-bottom: var(--space-3); }
.onboarding__preflight { margin-top: var(--space-4); padding: var(--space-4); background: var(--surface-muted); border: 1px solid var(--border); border-radius: var(--radius-lg); }
.onboarding__plan { display: flex; flex-wrap: wrap; align-items: baseline; gap: var(--space-1) var(--space-3); margin-bottom: var(--space-3); }
.onboarding__plan span, .onboarding__plan small { color: var(--text-muted); font-size: var(--font-size-xs); }
.onboarding__plan strong { color: var(--text-strong); font-weight: var(--font-weight-semibold); }
.onboarding__checks { display: grid; gap: var(--space-2); margin: 0; padding: 0; list-style: none; }
.onboarding__checks li { display: flex; align-items: flex-start; gap: var(--space-2); font-size: var(--font-size-sm); }
.onboarding__checks li span { color: var(--text-secondary); overflow-wrap: anywhere; }
.onboarding__checks li strong { margin-right: var(--space-2); color: var(--text-strong); font-weight: var(--font-weight-medium); }
.onboarding__check-icon { flex: none; width: 16px; height: 16px; margin-top: 2px; color: var(--text-muted); }
.is-passed .onboarding__check-icon { color: var(--success); }
.is-warning .onboarding__check-icon { color: var(--warning); }
.is-failed .onboarding__check-icon { color: var(--danger); }
.onboarding__actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: var(--space-2); margin-top: var(--space-5); padding-top: var(--space-4); border-top: 1px solid var(--border); }
.onboarding__id-field { display: flex; gap: var(--space-2); width: 100%; }
.onboarding__id-field :deep(.ui-input) { flex: 1; min-width: 0; }
.onboarding__section { margin-top: var(--space-4); padding-top: var(--space-4); border-top: 1px solid var(--border); }
.onboarding__section h4 { margin: 0 0 var(--space-3); color: var(--text-strong); font-size: var(--font-size-md); font-weight: var(--font-weight-semibold); }
.onboarding__section-head { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2) var(--space-3); margin-bottom: var(--space-3); }
.onboarding__section-head h4 { flex: 1 1 auto; margin: 0; }
.onboarding__section-head .onboarding__muted { margin: 0; }
.onboarding__options { display: grid; gap: var(--space-2); margin-bottom: var(--space-3); }
.onboarding__option { display: flex; align-items: flex-start; gap: var(--space-3); padding: var(--space-3); border: 1px solid var(--border); border-radius: var(--radius-md); cursor: pointer; }
.onboarding__option:hover { background: var(--surface-hover); }
.onboarding__option.is-selected { border-color: var(--primary-border); background: var(--primary-soft); }
.onboarding__option.is-disabled { cursor: not-allowed; opacity: .6; }
.onboarding__option input { margin-top: 3px; accent-color: var(--primary); }
.onboarding__option span { display: grid; gap: 2px; min-width: 0; }
.onboarding__option strong { color: var(--text-strong); font-size: var(--font-size-sm); font-weight: var(--font-weight-semibold); overflow-wrap: anywhere; }
.onboarding__option small { color: var(--text-muted); font-size: var(--font-size-xs); }
.onboarding__label-row { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto; gap: var(--space-2); align-items: center; margin-bottom: var(--space-2); }
.onboarding__secret { display: grid; gap: var(--space-2); margin-bottom: var(--space-4); padding: var(--space-4); background: var(--warning-soft); border: 1px solid var(--warning-border); border-radius: var(--radius-lg); }
.onboarding__secret p { margin: 0; color: var(--warning-text); font-size: var(--font-size-sm); }
.onboarding__secret p strong { display: block; margin-bottom: 2px; }
.onboarding__secret > .ui-button { justify-self: start; }
.onboarding__config, .onboarding__values { display: grid; }
.onboarding__kv { display: grid; grid-template-columns: 120px minmax(0, 1fr) auto; gap: var(--space-3); align-items: center; padding: var(--space-2) 0; border-bottom: 1px solid var(--border); }
.onboarding__kv:last-child { border-bottom: 0; }
.onboarding__kv span { color: var(--text-muted); font-size: var(--font-size-xs); }
.onboarding__kv code { padding: 0; color: var(--text-strong); background: none; font-family: var(--font-mono); font-size: var(--font-size-xs); overflow-wrap: anywhere; word-break: break-all; }
.onboarding__verify details { margin-top: var(--space-2); font-size: var(--font-size-sm); }
.onboarding__verify summary { color: var(--primary-text); cursor: pointer; }
.onboarding__diagnosis { display: grid; gap: var(--space-1); margin-bottom: var(--space-3); padding: var(--space-3) var(--space-4); background: var(--info-soft); border: 1px solid var(--info-border); border-radius: var(--radius-md); }
.onboarding__diagnosis.is-success { background: var(--success-soft); border-color: var(--success-border); }
.onboarding__diagnosis.is-warning { background: var(--warning-soft); border-color: var(--warning-border); }
.onboarding__diagnosis.is-error { background: var(--danger-soft); border-color: var(--danger-border); }
.onboarding__diagnosis .ui-tag { justify-self: start; }
.onboarding__diagnosis p { margin: 0; color: var(--text); font-size: var(--font-size-sm); }
.onboarding__diagnosis small { color: var(--text-muted); font-size: var(--font-size-xs); }
.onboarding__progress { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: var(--space-2); margin: 0; padding: 0; list-style: none; }
.onboarding__progress li { display: grid; grid-template-columns: 16px minmax(0, 1fr); column-gap: var(--space-2); align-content: start; padding: var(--space-2) var(--space-3); border: 1px solid var(--border); border-radius: var(--radius-md); }
.onboarding__progress li.is-passed { border-color: var(--success-border); }
.onboarding__progress li.is-failed { border-color: var(--danger-border); background: var(--danger-soft); }
.onboarding__progress .onboarding__check-icon { grid-row: span 2; }
.onboarding__progress strong { color: var(--text-strong); font-size: var(--font-size-sm); font-weight: var(--font-weight-medium); }
.onboarding__progress small { color: var(--text-muted); font-size: var(--font-size-xs); overflow-wrap: anywhere; }
.onboarding__values { margin-top: var(--space-3); }
.onboarding__values h5 { margin: 0 0 var(--space-1); color: var(--text-secondary); font-size: var(--font-size-sm); font-weight: var(--font-weight-medium); }
pre { max-height: 240px; margin: var(--space-2) 0 0; overflow: auto; }
@media (max-width: 900px) {
  .onboarding__progress { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 767px) {
  .onboarding__header { flex-direction: column; gap: var(--space-3); }
  .onboarding__header p { display: none; }
  .onboarding__header-actions { width: 100%; }
  .onboarding__header-actions > * { flex: 1 1 0; }
  .onboarding__steps { gap: var(--space-1); }
  .onboarding__steps li { gap: var(--space-2); padding: var(--space-2); }
  .onboarding__step-copy small { display: none; }
  .onboarding__card { padding: var(--space-4); }
  .onboarding__grid { grid-template-columns: 1fr; }
  .onboarding__progress { grid-template-columns: 1fr; gap: 0; }
  .onboarding__progress li { padding: var(--space-2) 0; border: 0; border-bottom: 1px solid var(--border); border-radius: 0; }
  .onboarding__progress li.is-failed { padding-inline: var(--space-2); }
  .onboarding__kv { grid-template-columns: 1fr auto; gap: 2px var(--space-2); }
  .onboarding__kv span { grid-column: 1 / -1; }
  .onboarding__label-row { grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); }
  .onboarding__actions { flex-direction: column-reverse; align-items: stretch; }
}
</style>
