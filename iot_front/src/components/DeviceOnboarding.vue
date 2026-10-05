<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { api, apiAll, formatTime, isAbort, session } from '../api'
import { useListLoader } from '../composables/useListLoader'
import { createClientId } from '../clientId'
import { statusLabel, transportLabel } from '../presentation'
import { UiMessage } from '../ui/feedback.js'
import { CheckCircle2, CircleDashed, Copy, XCircle } from '@lucide/vue'
import OnboardingDiagnosis from './OnboardingDiagnosis.vue'
import {
  configurationText,
  enrollRequest,
  fieldConfiguration,
  modeLabels,
  preflightQuery,
  usesPlatformIdentity,
  restoreEnrollDraft
} from '../onboardingPlan'

const props = defineProps({ initialProductId: { type: String, default: '' }, draftId: { type: String, default: '' }, trial: Boolean })
const emit = defineEmits(['close', 'done', 'navigate', 'detail', 'enrolled'])
const steps = [
  { title: '选择设备模板', hint: '复用已验证的接入配置' },
  { title: '设备与连接', hint: '填写编号和连接参数' },
  { title: '现场配置与验证', hint: '设备上报并确认结果' }
]
const randomId = prefix => `${prefix}_${createClientId().replaceAll('-', '').slice(0, 12)}`
const blankConnection = () => ({ choice: '', transport: '', port: null, host: '', unitId: 1, timeoutMs: 3000 })
const fresh = () => ({
  step: 0,
  productId: '',
  device: { id: '', name: '', role: '', description: '' },
  labels: [],
  connection: blankConnection(),
  requestId: createClientId(),
  result: null,
  checkSince: 0
})
const draft = reactive(fresh())
const products = ref([]),
  loading = ref(false),
  loadError = ref('')
const preflight = ref(null),
  checking = ref(false),
  preflightError = ref('')
const saving = ref(false),
  saveError = ref('')
// 设备密钥只保存在内存中，刷新页面或离开向导后不可再读取。
const credential = ref(null)
const status = ref(null),
  statusError = ref(''),
  statusAt = ref(0),
  refreshing = ref(false)
const serverDraftId = ref(props.draftId || createClientId()),
  draftRevision = ref(0),
  draftSaving = ref(false),
  draftError = ref(''),
  draftSavedAt = ref(0)
const draftWaiters = []
const identityToken = session.token
const activeIdentity = () => !disposed && session.token === identityToken
let draftTimer = 0,
  draftReady = false,
  draftDirty = false,
  disposed = false
const draftSteps = ['template', 'connection', 'verify']
async function saveDraft() {
  if (!draftReady || !activeIdentity()) return
  draftDirty = true
  if (draftSaving.value) return new Promise(resolve => draftWaiters.push(resolve))
  draftSaving.value = true
  try {
    while (draftDirty && activeIdentity()) {
      draftDirty = false
      const request = enrollRequest(draft, plan.value || { mode: '' })
      request.trial = props.trial
      request.device.tags = Object.fromEntries(
        Object.entries(request.device.tags || {}).filter(([key]) => !/(secret|token|password|access.?key|密钥|令牌|密码)/i.test(key))
      )
      const record = await api(`/api/v1/onboarding/drafts/${encodeURIComponent(serverDraftId.value)}`, {
        method: 'PUT',
        body: JSON.stringify({ revision: draftRevision.value, step: draftSteps[draft.step], productId: draft.productId, request })
      })
      draftRevision.value = record.revision
      draftSavedAt.value = record.updatedAt
      draftError.value = ''
      persist()
    }
  } catch (cause) {
    draftDirty = false
    draftError.value = cause.message || '草稿保存失败'
    if (cause.status === 409) draftReady = false
  } finally {
    draftSaving.value = false
    draftWaiters.splice(0).forEach(resolve => resolve())
  }
}
async function restoreServerDraft(id = props.draftId || serverDraftId.value) {
  if (!id) return
  const record = await api(`/api/v1/onboarding/drafts/${encodeURIComponent(id)}`)
  if (!activeIdentity()) return
  const body = typeof record.body === 'string' ? JSON.parse(record.body) : record.body
  Object.assign(draft, fresh(), restoreEnrollDraft(body?.request || {}))
  draft.requestId ||= createClientId()
  draft.step = Math.max(0, draftSteps.indexOf(body?.step))
  draftRevision.value = record.revision
  draftSavedAt.value = record.updatedAt
  draft.checkSince = record.createdAt
  if (draft.step === 2 && draft.device.id)
    draft.result = {
      device: { id: draft.device.id, name: draft.device.name },
      product: { id: draft.productId },
      mode: body?.request?.connection?.mode
    }
}

const storageKey = () => `iot:device-onboarding:v3:${session.tenant}:${session.user}:${props.trial ? props.initialProductId : 'daily'}`
const product = computed(() => products.value.find(item => item.id === draft.productId))
const plan = computed(() => preflight.value?.plan || null)
const mode = computed(() => (plan.value?.mode === 'listener' && draft.connection.choice === 'dial' ? 'dial' : plan.value?.mode || ''))
const category = computed(() => product.value?.category)
const templateName = computed(() => product.value?.name || draft.productId)
// 公共监听由模板准备统一管理，单台登记只选择已有连接或实例地址。
const listeners = computed(() => preflight.value?.profiles || [])
const canContinue = computed(() => Boolean(preflight.value?.ready) && !checking.value && Boolean(product.value))
const liveProfile = computed(() => status.value?.profile || draft.result?.profile || null)
const accessInfo = computed(() => status.value?.accessInfo || draft.result?.accessInfo || null)
const configuration = computed(() =>
  fieldConfiguration({ ...draft.result, profile: liveProfile.value }, accessInfo.value, credential.value)
)
// 密钥只在上方提示框中显示一次；“复制全部”仍包含它。
const visibleConfiguration = computed(() =>
  configuration.value.filter(row => row.name !== 'Secret' && !(credential.value && row.name === 'AccessKey'))
)

function usableListener(profile) {
  return profile.enabled && Boolean(profile.publicHost) && (plan.value?.networks || []).includes(profile.network)
}
function listenerLabel(profile) {
  return `${profile.publicHost || '未配置对外地址'}:${profile.port}`
}

function persist() {
  if (!activeIdentity()) return
  const saved = { ...JSON.parse(JSON.stringify(draft)), serverDraftId: serverDraftId.value, draftRevision: draftRevision.value }
  saved.labels = saved.labels.filter(row => !/(secret|token|password|access.?key|密钥|令牌|密码)/i.test(row.key || ''))
  try {
    localStorage.setItem(storageKey(), JSON.stringify(saved))
  } catch {
    /* 存储不可用时只影响草稿恢复。 */
  }
}
function restore() {
  try {
    const saved = JSON.parse(localStorage.getItem(storageKey()) || 'null')
    if (saved && typeof saved === 'object' && saved.requestId) {
      const { serverDraftId: id, draftRevision: revision, ...value } = saved
      Object.assign(draft, fresh(), value, { connection: { ...blankConnection(), ...value.connection } })
      if (id) {
        serverDraftId.value = id
        draftRevision.value = Number(revision || 0)
      }
    }
  } catch {
    forget()
  }
}
function forget() {
  try {
    localStorage.removeItem(storageKey())
  } catch {
    /* 忽略存储错误。 */
  }
}
watch(
  draft,
  () => {
    persist()
    if (draftReady) {
      clearTimeout(draftTimer)
      draftTimer = setTimeout(saveDraft, 700)
    }
  },
  { deep: true }
)
watch(
  () => draft.step,
  async () => {
    await nextTick()
    document.querySelector('.app-content')?.scrollTo({ top: 0 })
  }
)

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const productData = await apiAll('/api/v1/products')
    if (!activeIdentity()) return
    products.value = (productData.items || []).filter(item => (props.trial ? item.id === props.initialProductId : item.reusable === true))
    if (draft.step < 2) await runPreflight()
    else await refreshStatus()
  } catch (cause) {
    loadError.value = cause.message || '读取设备模板失败'
  } finally {
    loading.value = false
  }
}

const preflightLoader = useListLoader(checking)
async function runPreflight() {
  preflight.value = null
  preflightError.value = ''
  if (!draft.productId) return preflightLoader.cancel()
  try {
    const result = await preflightLoader.run(signal => api(`/api/v1/onboarding/preflight?${preflightQuery(draft)}`, { signal }))
    if (!activeIdentity()) return
    preflight.value = result
    prepareConnection(result.plan)
  } catch (cause) {
    if (!isAbort(cause)) preflightError.value = cause.message || '接入预检失败'
  }
}
function prepareConnection(p) {
  const c = draft.connection
  if (p.mode === 'standard' && !['MQTT', 'HTTP'].includes(c.transport)) c.transport = p.connector
  if (p.mode === 'listener') {
    const known = (c.choice === 'dial' && p.dial) || listeners.value.some(item => item.id === c.choice)
    if (!known) c.choice = listeners.value.find(usableListener)?.id || (p.dial ? 'dial' : '')
  } else c.choice = ''
  if (p.mode === 'poll' && !c.port) c.port = 502
}
function chooseProduct() {
  draft.connection = blankConnection()
  runPreflight()
}
function next() {
  if (!preflight.value) return UiMessage.warning('请先选择设备模板')
  if (!preflight.value.ready) return UiMessage.warning(plan.value?.reason || '该设备模板暂不能添加设备')
  if (!draft.device.role) draft.device.role = category.value === 'gateway' ? 'GATEWAY' : 'DIRECT'
  draft.step = 1
}
function usePlatformId() {
  draft.device.id = randomId('device')
}
const validPort = value => Number.isInteger(Number(value)) && Number(value) >= 1 && Number(value) <= 65535
function validate() {
  if (!draft.device.name.trim()) return '请填写设备名称'
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(draft.device.id.trim()))
    return '设备编号须为 1 至 128 位字母、数字、点、横线或下划线，且以字母或数字开头'
  const c = draft.connection
  if (mode.value === 'listener') {
    if (!c.choice || c.choice === 'new') return '请选择模板已有的公共连接'
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
  saving.value = true
  saveError.value = ''
  try {
    const result = await api('/api/v1/onboarding', {
      method: 'POST',
      body: JSON.stringify({ ...enrollRequest(draft, plan.value), trial: props.trial })
    })
    if (!activeIdentity()) return
    credential.value = result.credential?.secret ? result.credential : null
    draft.result = {
      device: result.device,
      product: { id: result.product?.id, name: result.product?.name },
      mode: result.mode,
      profile: result.profile || null,
      accessInfo: result.accessInfo || null,
      reused: Boolean(result.reused)
    }
    draft.checkSince = Number(result.device?.createdAt || Date.now())
    draft.step = 2
    emit('enrolled', result.device)
    await saveDraft()
    status.value = null
    if (result.reused) UiMessage.info('这台设备此前已添加，已恢复接入信息；设备密钥只在首次创建时显示。')
    await refreshStatus()
  } catch (cause) {
    saveError.value = cause.message || '保存失败，请稍后重试'
  } finally {
    saving.value = false
  }
}

const statusLoader = useListLoader(refreshing)
async function refreshStatus() {
  const id = draft.result?.device?.id
  if (!id) return
  try {
    const data = await statusLoader.run(signal =>
      api(`/api/v1/device-registry/${encodeURIComponent(id)}/connection?since=${encodeURIComponent(draft.checkSince || Date.now())}`, {
        signal
      })
    )
    if (!activeIdentity()) return
    status.value = data
    statusError.value = ''
    statusAt.value = Date.now()
  } catch (cause) {
    if (!isAbort(cause)) statusError.value = cause.message || '读取接入状态失败'
  }
}
let timer = 0,
  realtimeTimer = 0
function stopPolling() {
  clearInterval(timer)
  timer = 0
}
function schedulePolling() {
  stopPolling()
  if (draft.step === 2 && draft.result)
    timer = setInterval(() => {
      if (document.visibilityState !== 'hidden') refreshStatus()
    }, 5000)
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
  try {
    await navigator.clipboard.writeText(text)
    UiMessage.success(message)
  } catch {
    UiMessage.warning('浏览器不允许复制，请手动选择文本')
  }
}
function copyAll() {
  copy(configurationText(draft.result, accessInfo.value, credential.value), '接入信息已复制')
}
function openRaw() {
  persist()
  emit('navigate', 'raw', { deviceId: draft.result.device.id, rawMessageId: status.value?.ingest?.rawMessageId })
}
async function addAnother() {
  await saveDraft()
  if (draftError.value || !activeIdentity()) return
  serverDraftId.value = createClientId()
  draftRevision.value = 0
  draftSavedAt.value = 0
  const result = draft.result
  const choice = result?.mode === 'listener' && result.profile?.id ? result.profile.id : ''
  Object.assign(draft, {
    step: 1,
    productId: result?.product?.id || draft.productId,
    device: { id: '', name: '', role: draft.device.role, description: '' },
    labels: [],
    connection: { ...blankConnection(), choice, transport: draft.connection.transport },
    requestId: createClientId(),
    result: null,
    checkSince: 0
  })
  credential.value = null
  status.value = null
  saveError.value = ''
  load()
}
async function reloadDraft() {
  draftReady = false
  clearTimeout(draftTimer)
  try {
    await restoreServerDraft()
    await load()
    if (activeIdentity()) {
      draftError.value = ''
      draftReady = true
    }
  } catch (cause) {
    draftError.value = cause.message
  }
}
async function saveBeforeLeave() {
  clearTimeout(draftTimer)
  await saveDraft()
  if (draftError.value) throw new Error(draftError.value)
}
defineExpose({ saveBeforeLeave })
async function close() {
  clearTimeout(draftTimer)
  await saveDraft()
  if (!draftError.value) emit('close')
}
async function finish() {
  clearTimeout(draftTimer)
  await saveDraft()
  if (!draftError.value) {
    forget()
    emit('done')
  }
}
function openDetail() {
  const id = draft.result?.device?.id
  forget()
  emit('detail', id)
}
function restart() {
  forget()
  Object.assign(draft, fresh())
  credential.value = null
  status.value = null
  preflight.value = null
  saveError.value = ''
  preflightError.value = ''
}

function navigate(page) {
  persist()
  emit('navigate', page)
}

onMounted(async () => {
  try {
    if (props.draftId) await restoreServerDraft()
    else {
      restore()
      if (props.initialProductId && draft.productId !== props.initialProductId) {
        serverDraftId.value = createClientId()
        draftRevision.value = 0
        draftSavedAt.value = 0
        Object.assign(draft, fresh(), { productId: props.initialProductId })
      }
    }
    if (draft.step < 2 && !props.trial && !props.draftId) draft.step = 0
    await load()
    if (!activeIdentity()) return
    if (props.trial && draft.step === 0 && canContinue.value) next()
    if (draft.result?.device?.id) emit('enrolled', draft.result.device)
    draftReady = true
    window.addEventListener('iot:realtime', realtime)
    schedulePolling()
  } catch (cause) {
    loadError.value = cause.message
  }
})
onBeforeUnmount(() => {
  disposed = true
  preflightLoader.cancel()
  statusLoader.cancel()
  credential.value = null
  clearTimeout(draftTimer)
  stopPolling()
  clearTimeout(realtimeTimer)
  window.removeEventListener('iot:realtime', realtime)
})
</script>

<template>
  <section class="onboarding">
    <header v-if="!props.trial" class="onboarding__header">
      <div>
        <h2>{{ props.trial ? '首台设备验证' : '添加设备' }}</h2>
        <p>选择设备模板，填写设备编号和连接方式，再按提示在现场设备上完成配置并确认数据。</p>
      </div>
      <div class="onboarding__header-actions">
        <ui-button v-if="draft.step < 2" @click="restart">重新开始</ui-button>
        <ui-button :loading="draftSaving" @click="close">保存并返回</ui-button>
      </div>
    </header>

    <ol v-if="!props.trial" class="onboarding__steps" aria-label="添加设备进度">
      <li
        v-for="(item, index) in steps"
        :key="item.title"
        :class="{ 'is-active': draft.step === index, 'is-done': draft.step > index }"
        :aria-current="draft.step === index ? 'step' : undefined"
      >
        <span class="onboarding__step-number">{{ index + 1 }}</span>
        <span class="onboarding__step-copy"
          ><strong>{{ item.title }}</strong
          ><small>{{ item.hint }}</small></span
        >
      </li>
    </ol>

    <p v-if="draftSavedAt" class="onboarding__muted">草稿已保存 · {{ formatTime(draftSavedAt) }}</p>
    <ui-alert v-if="draftError" :title="draftError" type="warning" :closable="false" /><ui-button
      v-if="draftError && !draftReady"
      size="small"
      @click="reloadDraft"
      >重新加载服务器草稿</ui-button
    ><ui-button v-if="draftError && draftReady" size="small" @click="saveDraft">重试保存草稿</ui-button>
    <ui-alert v-if="loadError" class="onboarding__alert" :title="loadError" type="error" :closable="false" show-icon />

    <section v-if="draft.step === 0" class="onboarding__card" aria-labelledby="onboarding-step-template">
      <h3 id="onboarding-step-template" class="onboarding__title">这台设备使用哪个模板？</h3>
      <div class="onboarding__fields">
        <ui-form-item label="设备模板" required
          ><ui-select
            v-model="draft.productId"
            filterable
            :disabled="props.trial"
            placeholder="选择已经验证的设备模板"
            :loading="loading"
            @change="chooseProduct"
            ><ui-option
              v-for="item in products"
              :key="item.id"
              :value="item.id"
              :label="item.name || item.id"
              :disabled="item.status !== 'ENABLED'" /></ui-select
        ></ui-form-item>
        <p v-if="!loading && !products.length" class="onboarding__muted">
          还没有可复用的设备模板，请先完成模板的协议、公共连接与首台验证。
        </p>
        <ui-button v-if="!props.trial" v-permission="'menu:products'" size="small" @click="navigate('products')">准备设备模板</ui-button>
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
              <component
                :is="check.state === 'passed' ? CheckCircle2 : check.state === 'failed' ? XCircle : CircleDashed"
                class="onboarding__check-icon"
                aria-hidden="true"
              />
              <span
                ><strong>{{ check.label }}</strong
                >{{ check.detail }}</span
              >
            </li>
          </ul>
        </template>
      </section>

      <footer class="onboarding__actions">
        <ui-button type="primary" :disabled="!canContinue" @click="next">下一步</ui-button>
      </footer>
    </section>

    <section v-else-if="draft.step === 1" class="onboarding__card" aria-labelledby="onboarding-step-device">
      <h3 id="onboarding-step-device" class="onboarding__title">设备与连接</h3>
      <p class="onboarding__summary">
        <strong>{{ templateName }}</strong
        ><span>{{ modeLabels[mode] || '接入方式待确认' }}</span>
      </p>
      <ui-alert v-if="!plan" title="接入条件未确认，请返回上一步重新选择设备模板。" type="warning" :closable="false" show-icon />

      <div class="onboarding__grid">
        <ui-form-item label="设备名称" required
          ><ui-input v-model="draft.device.name" maxlength="256" placeholder="例如 一层东侧烟感" aria-label="设备名称"
        /></ui-form-item>
        <ui-form-item label="设备编号" required>
          <div class="onboarding__id-field">
            <ui-input v-model="draft.device.id" maxlength="128" placeholder="设备上报使用的编号" aria-label="设备编号" />
            <ui-button v-if="usesPlatformIdentity(mode)" @click="usePlatformId">使用平台编号</ui-button>
          </div>
        </ui-form-item>
      </div>
      <p class="onboarding__muted">
        {{
          usesPlatformIdentity(mode)
            ? '标准或 HTTP 接口上报的设备可以使用平台生成的编号；编号保存后不可修改。'
            : '编号须与协议从报文中识别出的设备标识一致，保存后不可修改。'
        }}
      </p>
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
            <ui-radio-group v-model="draft.connection.transport" class="segmented-choice-group" aria-label="上报通道"
              ><ui-radio-button value="MQTT">MQTT</ui-radio-button><ui-radio-button value="HTTP">HTTP</ui-radio-button></ui-radio-group
            >
          </ui-form-item>
          <p class="onboarding__muted">平台为设备签发 AccessKey 和 Secret，保存后显示设备端需要填写的地址和示例。</p>
        </template>
        <p v-else-if="plan?.mode === 'managed'" class="onboarding__muted">
          设备使用平台签发的 AccessKey 和 Secret，通过 HTTP 接口上报原始数据，平台按模板协议解析。
        </p>
        <template v-else-if="plan?.mode === 'listener'">
          <div class="onboarding__options" role="radiogroup" aria-label="接入点">
            <label
              v-for="item in listeners"
              :key="item.id"
              class="onboarding__option"
              :class="{ 'is-selected': draft.connection.choice === item.id, 'is-disabled': !usableListener(item) }"
            >
              <input v-model="draft.connection.choice" type="radio" name="listener" :value="item.id" :disabled="!usableListener(item)" />
              <span
                ><strong>{{ listenerLabel(item) }}</strong
                ><small
                  >{{ transportLabel(String(item.network).toUpperCase()) }} ·
                  {{ item.enabled ? statusLabel(item.runtimeStatus || 'PENDING') : '已停用'
                  }}<template v-if="!item.publicHost"> · 需先在接入点补齐对外地址</template></small
                ></span
              >
            </label>
            <label v-if="plan.dial" class="onboarding__option" :class="{ 'is-selected': draft.connection.choice === 'dial' }">
              <input v-model="draft.connection.choice" type="radio" name="listener" value="dial" />
              <span><strong>平台主动连接设备</strong><small>设备作为 TCP 服务端，平台按地址连接</small></span>
            </label>
          </div>
          <p v-if="!listeners.length && !plan.dial" class="onboarding__muted">该设备模板还没有可用公共连接，请在模板的“公共连接”中准备。</p>
        </template>
        <div v-if="mode === 'dial' || mode === 'poll'" class="onboarding__grid">
          <ui-form-item label="设备地址" required
            ><ui-input v-model="draft.connection.host" placeholder="平台可以访问的设备 IP 或域名" aria-label="设备地址"
          /></ui-form-item>
          <ui-form-item :label="mode === 'poll' ? '端口' : '设备端口'" :required="mode === 'dial'"
            ><ui-input-number
              v-model="draft.connection.port"
              :min="1"
              :max="65535"
              :placeholder="mode === 'poll' ? '默认 502' : ''"
              aria-label="设备端口"
          /></ui-form-item>
          <template v-if="mode === 'poll'">
            <ui-form-item label="站号"
              ><ui-input-number v-model="draft.connection.unitId" :min="0" :max="255" aria-label="站号"
            /></ui-form-item>
            <ui-form-item label="超时（毫秒）"
              ><ui-input-number v-model="draft.connection.timeoutMs" :min="100" :max="10000" :step="500" aria-label="超时"
            /></ui-form-item>
          </template>
        </div>
      </section>

      <details class="onboarding__more">
        <summary>安装位置与标签（选填）</summary>
        <ui-form-item label="安装位置 / 备注"
          ><ui-input v-model="draft.device.description" type="textarea" :rows="2" maxlength="1024"
        /></ui-form-item>
        <div v-for="(row, index) in draft.labels" :key="index" class="onboarding__label-row">
          <ui-input v-model="row.key" placeholder="名称，例如楼层" aria-label="标签名称" />
          <ui-input v-model="row.value" placeholder="内容，例如一层" aria-label="标签内容" />
          <ui-button text @click="draft.labels.splice(index, 1)">移除</ui-button>
        </div>
        <ui-button size="small" @click="draft.labels.push({ key: '', value: '' })">添加标签</ui-button>
      </details>

      <ui-alert v-if="saveError" class="onboarding__alert" :title="saveError" type="error" :closable="false" show-icon />
      <footer class="onboarding__actions">
        <ui-button v-if="!props.trial" :disabled="saving" @click="draft.step = 0">上一步</ui-button
        ><ui-button v-else :disabled="saving" :loading="draftSaving" @click="close">保存草稿并返回验收</ui-button>
        <ui-button type="primary" :loading="saving" :disabled="!plan" @click="submit">保存并生成接入信息</ui-button>
      </footer>
    </section>

    <section v-else class="onboarding__card" aria-labelledby="onboarding-step-verify">
      <h3 id="onboarding-step-verify" class="onboarding__title">现场配置与验证</h3>
      <p class="onboarding__summary">
        <strong>{{ draft.result?.device?.name }}</strong
        ><span>{{ draft.result?.device?.id }} · {{ draft.result?.product?.name }} · {{ modeLabels[draft.result?.mode] || '' }}</span>
      </p>

      <div v-if="credential" class="onboarding__secret" role="alert">
        <p><strong>设备密钥只显示这一次</strong>请立即复制并交给现场人员，关闭或刷新页面后无法再次读取。</p>
        <div class="onboarding__kv">
          <span>AccessKey</span><code>{{ credential.accessKey }}</code
          ><ui-button text aria-label="复制 AccessKey" @click="copy(credential.accessKey)"><Copy /></ui-button>
        </div>
        <div class="onboarding__kv">
          <span>Secret</span><code>{{ credential.secret }}</code
          ><ui-button text aria-label="复制 Secret" @click="copy(credential.secret)"><Copy /></ui-button>
        </div>
        <ui-button size="small" @click="credential = null">我已保存</ui-button>
      </div>
      <p v-else-if="accessInfo" class="onboarding__muted">设备密钥只在首次创建时显示；如已丢失，请在设备详情中重新生成凭据。</p>

      <section class="onboarding__section">
        <div class="onboarding__section-head">
          <h4>
            {{ mode === 'dial' || draft.result?.mode === 'dial' || draft.result?.mode === 'poll' ? '平台连接设置' : '在现场设备上填写' }}
          </h4>
          <ui-button size="small" @click="copyAll"><Copy />复制全部</ui-button>
        </div>
        <div class="onboarding__config">
          <div v-for="row in visibleConfiguration" :key="row.name" class="onboarding__kv">
            <span>{{ row.name }}</span
            ><code>{{ row.value }}</code>
          </div>
        </div>
        <p v-if="draft.result?.mode === 'listener'" class="onboarding__muted">
          设备连接上述地址后，平台按协议识别设备编号 {{ draft.result?.device?.id }}。
        </p>
        <p v-else-if="draft.result?.mode === 'dial' || draft.result?.mode === 'poll'" class="onboarding__muted">
          平台会主动连接设备，现场只需确认设备地址、端口{{ draft.result?.mode === 'poll' ? '和站号' : '' }}可达。
        </p>
        <details v-if="accessInfo?.sample" class="onboarding__more">
          <summary>示例报文</summary>
          <pre>{{ JSON.stringify(accessInfo.sample, null, 2) }}</pre>
        </details>
      </section>

      <OnboardingDiagnosis
        @navigate="(page, detail) => emit('navigate', page, detail)"
        :status="status"
        :error="statusError"
        :refreshing="refreshing"
        :updated-at="statusAt"
        @refresh="refreshStatus"
        @raw="openRaw"
      />

      <footer class="onboarding__actions">
        <ui-button @click="openDetail">设备详情</ui-button>
        <ui-button v-permission="'POST /api/v1/device-registry'" @click="addAnother">继续添加同模板设备</ui-button>
        <ui-button type="primary" @click="finish">保存并退出</ui-button>
      </footer>
    </section>
  </section>
</template>

<style scoped>
.onboarding {
  max-width: 960px;
  margin: 0 auto;
  color: var(--text);
}
.onboarding__header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-4);
  margin-bottom: var(--space-4);
}
.onboarding__header h2 {
  margin: 0;
  color: var(--text-strong);
  font-size: var(--font-size-xl);
  font-weight: var(--font-weight-semibold);
}
.onboarding__header p {
  margin: var(--space-1) 0 0;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}
.onboarding__header-actions {
  display: flex;
  flex: none;
  flex-wrap: wrap;
  gap: var(--space-2);
}
.onboarding__steps {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--space-2);
  margin: 0 0 var(--space-4);
  padding: 0;
  list-style: none;
}
.onboarding__steps li {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
  padding: var(--space-3);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
}
.onboarding__steps li.is-active {
  border-color: var(--primary-border);
  box-shadow: inset 0 -2px 0 var(--primary);
}
.onboarding__step-number {
  display: grid;
  flex: none;
  place-items: center;
  width: 24px;
  height: 24px;
  color: var(--text-secondary);
  background: var(--surface-muted);
  border-radius: var(--radius-full);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
}
.is-active .onboarding__step-number {
  color: var(--text-inverse);
  background: var(--primary);
}
.is-done .onboarding__step-number {
  color: var(--success-text);
  background: var(--success-soft);
}
.onboarding__step-copy {
  display: grid;
  min-width: 0;
}
.onboarding__step-copy strong {
  color: var(--text-strong);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
}
.onboarding__step-copy small {
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}
.onboarding__card {
  padding: var(--space-5) var(--space-6);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-xs);
}
.onboarding__title {
  margin: 0 0 var(--space-4);
  color: var(--text-strong);
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-semibold);
}
.onboarding__fields {
  margin-top: var(--space-4);
}
.onboarding__grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 var(--space-4);
}
.onboarding__grid > * {
  min-width: 0;
}
.onboarding__grid :deep(.ui-input-number) {
  width: 100%;
}
.onboarding__muted {
  margin: var(--space-1) 0 var(--space-3);
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}
.onboarding__alert {
  margin: var(--space-3) 0;
}
.onboarding__summary {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--space-1) var(--space-3);
  margin: 0 0 var(--space-4);
  padding: var(--space-3) var(--space-4);
  background: var(--surface-muted);
  border-radius: var(--radius-md);
  font-size: var(--font-size-sm);
}
.onboarding__summary strong {
  color: var(--text-strong);
  font-weight: var(--font-weight-semibold);
}
.onboarding__summary span {
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}
.onboarding__more {
  margin: var(--space-3) 0;
}
.onboarding__more summary {
  width: max-content;
  max-width: 100%;
  color: var(--primary-text);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  cursor: pointer;
}
.onboarding__more[open] summary {
  margin-bottom: var(--space-3);
}
.onboarding__preflight {
  margin-top: var(--space-4);
  padding: var(--space-4);
  background: var(--surface-muted);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
}
.onboarding__plan {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--space-1) var(--space-3);
  margin-bottom: var(--space-3);
}
.onboarding__plan span,
.onboarding__plan small {
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}
.onboarding__plan strong {
  color: var(--text-strong);
  font-weight: var(--font-weight-semibold);
}
.onboarding__checks {
  display: grid;
  gap: var(--space-2);
  margin: 0;
  padding: 0;
  list-style: none;
}
.onboarding__checks li {
  display: flex;
  align-items: flex-start;
  gap: var(--space-2);
  font-size: var(--font-size-sm);
}
.onboarding__checks li span {
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}
.onboarding__checks li strong {
  margin-right: var(--space-2);
  color: var(--text-strong);
  font-weight: var(--font-weight-medium);
}
.onboarding__check-icon {
  flex: none;
  width: 16px;
  height: 16px;
  margin-top: 2px;
  color: var(--text-muted);
}
.is-passed .onboarding__check-icon {
  color: var(--success);
}
.is-warning .onboarding__check-icon {
  color: var(--warning);
}
.is-failed .onboarding__check-icon {
  color: var(--danger);
}
.onboarding__actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: var(--space-2);
  margin-top: var(--space-5);
  padding-top: var(--space-4);
  border-top: 1px solid var(--border);
}
.onboarding__id-field {
  display: flex;
  gap: var(--space-2);
  width: 100%;
}
.onboarding__id-field :deep(.ui-input) {
  flex: 1;
  min-width: 0;
}
.onboarding__section {
  margin-top: var(--space-4);
  padding-top: var(--space-4);
  border-top: 1px solid var(--border);
}
.onboarding__section h4 {
  margin: 0 0 var(--space-3);
  color: var(--text-strong);
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
}
.onboarding__section-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2) var(--space-3);
  margin-bottom: var(--space-3);
}
.onboarding__section-head h4 {
  flex: 1 1 auto;
  margin: 0;
}
.onboarding__section-head .onboarding__muted {
  margin: 0;
}
.onboarding__options {
  display: grid;
  gap: var(--space-2);
  margin-bottom: var(--space-3);
}
.onboarding__option {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  padding: var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  cursor: pointer;
}
.onboarding__option:hover {
  background: var(--surface-hover);
}
.onboarding__option.is-selected {
  border-color: var(--primary-border);
  background: var(--primary-soft);
}
.onboarding__option.is-disabled {
  cursor: not-allowed;
  opacity: 0.6;
}
.onboarding__option input {
  margin-top: 3px;
  accent-color: var(--primary);
}
.onboarding__option span {
  display: grid;
  gap: 2px;
  min-width: 0;
}
.onboarding__option strong {
  color: var(--text-strong);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  overflow-wrap: anywhere;
}
.onboarding__option small {
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}
.onboarding__label-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto;
  gap: var(--space-2);
  align-items: center;
  margin-bottom: var(--space-2);
}
.onboarding__secret {
  display: grid;
  gap: var(--space-2);
  margin-bottom: var(--space-4);
  padding: var(--space-4);
  background: var(--warning-soft);
  border: 1px solid var(--warning-border);
  border-radius: var(--radius-lg);
}
.onboarding__secret p {
  margin: 0;
  color: var(--warning-text);
  font-size: var(--font-size-sm);
}
.onboarding__secret p strong {
  display: block;
  margin-bottom: 2px;
}
.onboarding__secret > .ui-button {
  justify-self: start;
}
.onboarding__config {
  display: grid;
}
.onboarding__kv {
  display: grid;
  grid-template-columns: 120px minmax(0, 1fr) auto;
  gap: var(--space-3);
  align-items: center;
  padding: var(--space-2) 0;
  border-bottom: 1px solid var(--border);
}
.onboarding__kv:last-child {
  border-bottom: 0;
}
.onboarding__kv span {
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}
.onboarding__kv code {
  padding: 0;
  color: var(--text-strong);
  background: none;
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  overflow-wrap: anywhere;
  word-break: break-all;
}
pre {
  max-height: 240px;
  margin: var(--space-2) 0 0;
  overflow: auto;
}
@media (max-width: 767px) {
  .onboarding__header {
    flex-direction: column;
    gap: var(--space-3);
  }
  .onboarding__header p {
    display: none;
  }
  .onboarding__header-actions {
    width: 100%;
  }
  .onboarding__header-actions > * {
    flex: 1 1 0;
  }
  .onboarding__steps {
    gap: var(--space-1);
  }
  .onboarding__steps li {
    gap: var(--space-2);
    padding: var(--space-2);
  }
  .onboarding__step-copy small {
    display: none;
  }
  .onboarding__card {
    padding: var(--space-4);
  }
  .onboarding__grid {
    grid-template-columns: 1fr;
  }
  .onboarding__kv {
    grid-template-columns: 1fr auto;
    gap: 2px var(--space-2);
  }
  .onboarding__kv span {
    grid-column: 1 / -1;
  }
  .onboarding__label-row {
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  }
  .onboarding__actions {
    flex-direction: column-reverse;
    align-items: stretch;
  }
}
</style>
