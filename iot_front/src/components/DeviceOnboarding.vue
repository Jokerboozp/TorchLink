<script setup>
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { api, apiAll, formatTime, notifyError, session } from '../api'
import { can } from '../permissions'
import { createClientId } from '../clientId'
import { categories } from '../labels'
import { UiMessage } from '../ui/feedback.js'
import { CheckCircle2, Clock3, XCircle } from '@lucide/vue'
import ProtocolAssistantView from '../views/ProtocolAssistantView.vue'
import OnboardingProfileEditor from './OnboardingProfileEditor.vue'

const emit = defineEmits(['close', 'done', 'navigate', 'detail'])
const products = ref([]), packages = ref([]), releases = ref([]), profiles = ref([]), devices = ref([])
const loading = ref(false), busy = ref(false), error = ref(''), check = ref(null), credential = ref(null)
const children = ref([]), childrenLoading = ref(false)
const helper = ref('')
const fresh = () => ({ step:0, scenario:'existing', productId:'', newProductId:'product_'+createClientId().replaceAll('-','').slice(0,12), newName:'', category:'other', protocolPackageId:'', transport:'', manufacturer:'', model:'', idKind:'', idLocation:'', name:'', deviceId:'', generatedId:'device_'+createClientId().replaceAll('-','').slice(0,12), deviceRole:'DIRECT', parentId:'', childAddress:'', childType:'', profileId:'', description:'', labels:[{key:'',value:''}], createdProductId:'', createdDeviceId:'', checkSince:0 })
const draft = reactive(fresh())
const key = () => `iot:device-onboarding:${session.tenant}:${session.user}`
const product = computed(() => products.value.find(p => p.id === draft.productId))
const parent = computed(() => devices.value.find(d => d.id === draft.parentId))
const parentProfile = computed(() => profiles.value.find(p => p.id === parent.value?.tags?.connectorProfileId))
const possibleParents = computed(() => devices.value.filter(d => d.deviceRole !== 'CHILD' && (d.deviceRole === 'GATEWAY' || profiles.value.some(p => p.id === d.tags?.connectorProfileId && p.childProducts?.length))))
const childMappings = computed(() => parentProfile.value?.childProducts || [])
const isStandard = computed(() => product.value?.protocolPackageId === 'iot-standard@1.0.0')
const validProtocol = computed(() => isStandard.value || packages.value.some(p => p.id === product.value?.protocolPackageId) || releases.value.some(p => p.id === product.value?.protocolPackageId))
const transport = computed(() => product.value?.transport || '')
const connections = computed(() => profiles.value.filter(p => p.productId === draft.productId && (!p.deviceId || p.deviceId === (draft.createdDeviceId || draft.deviceId || draft.generatedId)) && (transport.value.includes('TCP') ? p.network === 'tcp' : transport.value.includes('UDP') ? p.network === 'udp' : true)))
const chosenProfile = computed(() => connections.value.find(p => p.id === draft.profileId))
const metadata = computed(() => product.value?.metadata || {})
const targetId = computed(() => draft.deviceId.trim())
const savedDevice = computed(() => devices.value.find(d => d.id === draft.createdDeviceId))
const registered = computed(() => Boolean(draft.createdDeviceId))
const status = computed(() => {
  if (!check.value) return { text:'等待读取接入状态', tone:'info', next:'刷新接入检查。' }
  const d = check.value, ingest = d.ingest || {}
  if (d.product?.status !== 'ENABLED') return { text:'设备模板已停用或不存在', tone:'warning', next:'请在设备模板中核对状态和协议绑定。' }
  if (!validProtocol.value) return { text:'设备通信协议已失效', tone:'warning', next:'在设备模板或协议工作区检查已发布版本。' }
  if (d.device?.status !== 'ENABLED') return { text:'设备已停用', tone:'warning', next:'在设备日常管理中核对设备状态。' }
  if (draft.scenario === 'child' && !d.parent) return { text:'所属主设备当前不可查看', tone:'warning', next:'检查主设备是否仍存在，以及当前账号是否有主设备权限。' }
  if (!isStandard.value && !d.profile) return { text:'尚未关联平台连接配置', tone:'warning', next:'选择已有连接；如果没有可用连接，前往平台连接配置补齐。' }
  if (d.profile && (!d.profile.enabled || d.profile.runtimeStatus === 'DISABLED')) return { text:'平台连接配置未启用', tone:'warning', next:'打开平台连接配置，检查启用状态。' }
  if (d.profile && ['ERROR','UNSUPPORTED'].includes(d.profile.runtimeStatus)) return { text:'平台连接服务异常', tone:'error', next:'打开对应平台连接配置，查看运行状态和错误详情。' }
  if (d.profile?.mode === 'listener' && d.profile.connectionMode !== 'dial' && !d.profile.publicHost && !ingest.rawReceived) return { text:'平台对外地址未配置', tone:'warning', next:'在平台连接配置填写现场设备可达的域名或 IP。' }
  if (ingest.stage === 'PARSE_FAILED') return { text:'收到数据，解析失败', tone:'error', next:'查看原始报文，并核对已发布的协议版本与真实报文。' }
  if (ingest.stage === 'RAW_RECEIVED') return { text:'收到数据，等待解析', tone:'info', next:'稍后刷新；持续未解析时查看原始报文处理状态。' }
  if (ingest.stage === 'PARSED' && ingest.stale) return { text:'曾解析成功，最近没有新数据', tone:'warning', next:'核对设备供电和连接，查看最近接收时间。' }
  if (ingest.stage === 'PARSED') return { text:ingest.continuouslyUpdating ? '本次解析成功，数据持续更新' : '本次解析成功，等待下一次上报', tone:'success', next:'可进入日常管理，继续观察设备数据。' }
  if (isStandard.value && !d.accessInfo?.[d.connector === 'MQTT' ? 'mqttBroker' : 'httpUrl']) return { text:'平台对外地址未配置', tone:'warning', next:'请管理员配置设备接入的对外地址，再按设备端信息连接。' }
  if (ingest.previousParsedAt) return { text:'曾接入成功，等待本次验证', tone:'info', next:`历史成功时间 ${formatTime(ingest.previousParsedAt)}；请让现场设备重新上报并刷新。` }
  return { text:'等待本次设备上报', tone:'info', next:'按下方现场设备连接信息配置设备，然后刷新检查。' }
})
const checkItems = computed(() => {
  const d = check.value, ingest = d?.ingest || {}, profile = d?.profile
  const serviceFailed = isStandard.value
    ? Boolean(d && !d.accessInfo?.[d.connector === 'MQTT' ? 'mqttBroker' : 'httpUrl'])
    : Boolean(profile && (!profile.enabled || ['DISABLED','ERROR','UNSUPPORTED'].includes(profile.runtimeStatus)))
  const serviceReady = Boolean(ingest.rawReceived || (profile?.enabled && ['LISTENING','CONNECTED','ONLINE'].includes(profile.runtimeStatus)))
  return [
    { label:'配置保存', state:ingest.configurationSaved ? 'passed' : 'waiting', text:ingest.configurationSaved ? '已保存' : '等待确认' },
    { label:'接收服务', state:serviceFailed ? 'failed' : serviceReady ? 'passed' : 'waiting', text:serviceFailed ? (isStandard.value ? '对外地址未配置' : '连接服务异常或未启用') : serviceReady ? '服务已接收或运行中' : isStandard.value && d?.accessInfo ? '接口已配置，等待现场' : '等待连接配置' },
    { label:'当前设备数据', state:ingest.rawReceived ? 'passed' : 'waiting', text:ingest.rawReceived ? `已接收 · ${formatTime(ingest.receivedAt)}` : '本次尚未收到' },
    { label:'解析结果', state:ingest.parseError ? 'failed' : ingest.parsed ? 'passed' : 'waiting', text:ingest.parseError ? '解析失败' : ingest.parsed ? '本次解析成功' : '等待现场解析' },
    { label:'持续更新', state:ingest.stale ? 'failed' : ingest.continuouslyUpdating ? 'passed' : 'waiting', text:ingest.stale ? '超过 15 分钟无新数据' : ingest.continuouslyUpdating ? '已有连续上报' : '等待后续上报' }
  ]
})
const checkIcons = { passed:CheckCircle2, waiting:Clock3, failed:XCircle }
const values = computed(() => Object.entries(check.value?.ingest?.standardMessage?.properties || {}).map(([id,value]) => {
  const field = check.value?.product?.thingModel?.properties?.find(item => item.identifier === id)
  return { name:field?.name || id, unit:field?.unit || '', value }
}))

function persist() {
  const safe = Object.fromEntries(Object.keys(draft).map(k => [k,draft[k]]))
  safe.labels = draft.labels.filter(row => !/(secret|token|password|access.?key|密钥|令牌|密码)/i.test(row.key || ''))
  localStorage.setItem(key(), JSON.stringify(safe))
}
function restore() {
  try { const saved = JSON.parse(localStorage.getItem(key()) || 'null'); if (saved && typeof saved === 'object') Object.assign(draft, fresh(), saved) }
  catch { localStorage.removeItem(key()) }
}
watch(draft, persist, { deep:true })
watch(() => draft.step, async () => { await nextTick(); const content = document.querySelector('.app-content'); if (content) content.scrollTop = 0 })

async function load() {
  loading.value = true; error.value = ''
  try {
    const [p, registry, catalog, versions, access] = await Promise.all([
      apiAll('/api/v1/products'), apiAll('/api/v1/device-registry'),
      apiAll('/api/v1/protocol-packages'), api('/api/v2/protocols'), api('/api/v1/connectors')
    ])
    products.value = p.items || []
    devices.value = (registry.items || []).map(x => x.device)
    packages.value = (catalog.items || []).filter(x => x.status === 'PUBLISHED')
    releases.value = (versions.items || []).flatMap(x => (x.releases || []).filter(r => r.status === 'PUBLISHED').map(r => ({id:`${x.definition.id}@${r.version}`, name:`${x.definition.name} · ${r.version}`, transport:r.transport, payloadFormat:r.payloadFormat})))
    profiles.value = (access.items || []).map(x => x.profile).filter(Boolean)
    if (draft.createdDeviceId) await refreshCheck()
    if (draft.scenario === 'child' && draft.parentId) await loadChildren()
    if (draft.createdProductId && !products.value.some(x => x.id === draft.createdProductId)) error.value = '已创建的设备模板不存在或当前账号无法查看，请重新选择模板。'
    if (draft.productId && products.value.some(x => x.id === draft.productId && x.status !== 'ENABLED')) error.value = '所选设备模板已停用，请在设备模板中核对状态，或重新选择。'
    if (draft.productId && product.value && !validProtocol.value) error.value = '设备模板的通信协议已失效，请检查已发布版本。'
    if (draft.profileId && !profiles.value.some(x => x.id === draft.profileId)) error.value = '所选平台连接配置已不存在或当前账号无权查看，请重新选择连接。'
    if (draft.createdDeviceId && !devices.value.some(x => x.id === draft.createdDeviceId)) error.value = '已登记的设备不存在或当前账号无法查看，请检查权限或重新开始。'
  } catch (cause) { error.value = cause.message || '读取接入资料失败'; notifyError(cause) }
  finally { loading.value = false }
}
function selectScenario(value) { draft.scenario = value; draft.productId = ''; draft.profileId = ''; draft.step = 0 }
function protocolName(id) { return id === 'iot-standard@1.0.0' ? '标准设备上报' : packages.value.find(item => item.id === id)?.name || releases.value.find(item => item.id === id)?.name || id || '未配置' }
function selectedProtocol() { return [{id:'iot-standard@1.0.0',name:'标准设备上报',transport:'MQTT_HTTP',payloadFormat:'json'}, ...packages.value.map(p => ({id:p.id,name:p.name,transport:p.transport,payloadFormat:p.payloadFormat})), ...releases.value].find(p => p.id === draft.protocolPackageId) }
function chooseProtocol() { const p = selectedProtocol(); draft.transport = p?.transport === 'MQTT_HTTP' ? 'MQTT' : p?.transport === 'TCP_UDP' ? 'TCP' : p?.transport || ''; }
async function makeProduct() {
  if (!draft.newName.trim()) return UiMessage.warning('请填写设备模板名称')
  const protocol = selectedProtocol()
  if (!protocol) return UiMessage.warning('请选择已发布的设备通信协议')
  if (!can('POST /api/v1/products')) return UiMessage.warning('当前账号缺少创建设备模板权限')
  const prior = products.value.find(p => p.id === draft.newProductId)
  if (prior) { draft.createdProductId = prior.id; draft.productId = prior.id; draft.step = 1; return }
  busy.value = true
  try {
    const value = { id:draft.newProductId, name:draft.newName.trim(), category:draft.category, protocolPackageId:protocol.id, transport:draft.transport, payloadFormat:protocol.payloadFormat, status:'ENABLED', metadata:{manufacturer:draft.manufacturer.trim(),model:draft.model.trim(),idKind:draft.idKind.trim(),idLocation:draft.idLocation.trim()} }
    const created = await api('/api/v1/products',{method:'POST',body:JSON.stringify(value)})
    draft.createdProductId = created.id; draft.productId = created.id
    products.value = [...products.value.filter(p => p.id !== created.id), created]
    draft.step = 1
  } catch (cause) { notifyError(cause); await load(); const recovered = products.value.find(p => p.id === draft.newProductId); if (recovered && recovered.name === draft.newName.trim()) { draft.createdProductId = recovered.id; draft.productId = recovered.id; draft.step = 1 } }
  finally { busy.value = false }
}
function advanceTemplate() {
  if (draft.scenario === 'new') return makeProduct()
  if (draft.scenario === 'child' && !draft.parentId) return UiMessage.warning('请先选择所属主设备')
  if (!product.value || product.value.status !== 'ENABLED') return UiMessage.warning('请选择已启用的设备模板')
  if (!validProtocol.value) return UiMessage.warning('模板的通信协议已失效，请在设备模板或协议工作区检查已发布版本')
  if (draft.scenario === 'child' && !childMappings.value.some(m => m.productId === draft.productId)) return UiMessage.warning('主设备连接尚未配置这个子设备模板，请先补齐映射')
  draft.step = 1
}
function advanceInfo() {
  if (!product.value || product.value.status !== 'ENABLED') return UiMessage.warning('设备模板已失效或停用，请返回上一步重新选择')
  if (!validProtocol.value) return UiMessage.warning('模板协议已失效，请检查已发布版本')
  if (!draft.name.trim()) return UiMessage.warning('请填写设备名称')
  if (draft.scenario === 'child') {
    if (!draft.childAddress.trim() || !draft.childType) return UiMessage.warning('请填写协议中的子设备地址并选择类型')
  } else if (!targetId.value) return UiMessage.warning('请填写现场设备上报的实际编号')
  draft.step = 2
}
function tags() {
  const result = {}
  for (const row of draft.labels) if (row.key.trim()) result[row.key.trim()] = row.value
  if (draft.profileId) result.connectorProfileId = draft.profileId
  return result
}
async function saveDevice() {
  if (!product.value || product.value.status !== 'ENABLED') return UiMessage.warning('设备模板已失效或停用，请重新选择')
  if (!validProtocol.value) return UiMessage.warning('模板协议已失效，请检查已发布版本')
  if (!can(draft.scenario === 'child' ? 'POST /api/v1/device-registry/:id/children' : 'POST /api/v1/device-registry')) return UiMessage.warning('当前账号缺少设备登记权限或全部设备范围')
  if (busy.value) return
  if (draft.scenario !== 'child' && devices.value.some(d => d.id === targetId.value) && !draft.createdDeviceId) return UiMessage.warning('设备编号已登记，请在设备列表打开现有设备')
  busy.value = true
  try {
    let result
    if (draft.scenario === 'child') {
      result = await api(`/api/v1/device-registry/${encodeURIComponent(draft.parentId)}/children`,{method:'POST',body:JSON.stringify({address:draft.childAddress.trim(),type:draft.childType,name:draft.name.trim()})})
    } else {
      result = await api('/api/v1/device-registry',{method:'POST',body:JSON.stringify({id:targetId.value,productId:draft.productId,name:draft.name.trim(),deviceRole:draft.deviceRole,status:'ENABLED',description:draft.description,tags:tags()})})
    }
    draft.createdDeviceId = result.device.id
    draft.checkSince = Number(result.device.updatedAt || result.device.createdAt || Date.now())
    devices.value = [...devices.value.filter(d => d.id !== result.device.id), result.device]
    credential.value = result.credential?.secret ? result.credential : null
    draft.step = 3
    await refreshCheck()
  } catch (cause) {
    notifyError(cause)
    // A lost response may follow a successful write. Reconcile by the stable ID.
    await load()
    const recovered = devices.value.find(d => d.id === targetId.value)
    if (recovered && draft.scenario !== 'child') { draft.createdDeviceId = recovered.id; draft.checkSince = Number(recovered.updatedAt || Date.now()); draft.step = 3; UiMessage.warning('设备已登记；首次密钥无法重新读取，若未保存请到连接详情重新生成。') }
  } finally { busy.value = false }
}
async function linkProfile() {
  if (!savedDevice.value || !chosenProfile.value || busy.value) return
  if (!can('PUT /api/v1/device-registry/:id')) return UiMessage.warning('缺少编辑设备权限')
  busy.value = true
  try {
    const d = savedDevice.value
    const result = await api(`/api/v1/device-registry/${encodeURIComponent(d.id)}`,{method:'PUT',body:JSON.stringify({...d,tags:{...d.tags,connectorProfileId:draft.profileId}})})
    devices.value = [...devices.value.filter(x => x.id !== d.id), result.device]
    draft.checkSince = Number(result.device.updatedAt || Date.now())
    await refreshCheck()
  } catch (cause) { notifyError(cause) }
  finally { busy.value = false }
}
async function refreshCheck() {
  if (!draft.createdDeviceId) return
  try { check.value = await api(`/api/v1/device-registry/${encodeURIComponent(draft.createdDeviceId)}/connection?since=${encodeURIComponent(draft.checkSince || Date.now())}`); error.value = '' }
  catch (cause) { error.value = cause.message || '检查失败'; check.value = null }
}
async function loadChildren() {
  if (!draft.parentId || !childMappings.value.length) return
  childrenLoading.value = true
  try { const result = await api(`/api/v1/device-registry/${encodeURIComponent(draft.parentId)}/children`); children.value = result.items || [] }
  catch { children.value = [] }
  finally { childrenLoading.value = false }
}
function changeParent() { children.value = []; draft.childType = ''; draft.productId = ''; if (draft.parentId) loadChildren() }
function inspectChild(row) { draft.createdDeviceId = row.device.id; draft.productId = row.device.productId; draft.checkSince = Date.now(); draft.step = 3; refreshCheck() }
function navigate(page, detail) { persist(); emit('navigate', page, detail) }
async function helperSaved() { await load(); if (draft.step === 3) await refreshCheck() }
async function profileSaved(id) {
  helper.value = ''
  await load()
  if (draft.scenario === 'child' && parent.value && parent.value.tags?.connectorProfileId !== id) {
    if (!can('PUT /api/v1/device-registry/:id')) return UiMessage.warning('连接已保存，当前账号缺少关联主设备的权限')
    try {
      const d = parent.value
      const result = await api(`/api/v1/device-registry/${encodeURIComponent(d.id)}`, {method:'PUT',body:JSON.stringify({...d,tags:{...d.tags,connectorProfileId:id}})})
      devices.value = [...devices.value.filter(x => x.id !== d.id), result.device]
      await loadChildren()
    } catch (cause) { notifyError(cause) }
  } else if (draft.scenario === 'child') {
    await loadChildren()
  } else if (draft.scenario !== 'child') {
    draft.profileId = id
    if (draft.createdDeviceId) await linkProfile()
  }
}
function finish() { localStorage.removeItem(key()); emit('done') }
function reset() { localStorage.removeItem(key()); Object.assign(draft, fresh()); credential.value = null; check.value = null; error.value = ''; children.value = [] }
onMounted(() => { restore(); load() })
</script>

<template>
  <section class="onboarding-workspace">
    <header class="onboarding-header">
      <div><span class="onboarding-eyebrow">设备管理 / 接入向导</span><h2>接入现场设备</h2><p>按顺序确认模板、设备编号和连接方式，再查看真实上报结果。</p></div>
      <div class="onboarding-header-actions"><ui-button @click="emit('close')">返回设备列表</ui-button><ui-button :loading="loading" @click="load">刷新配置</ui-button><ui-button @click="reset">重新开始</ui-button></div>
    </header>
    <ui-alert v-if="error" :title="error" type="warning" :closable="false" show-icon class="top-gap" />
    <ol class="onboarding-steps" aria-label="设备接入进度"><li v-for="(title,index) in ['选择设备模板','填写设备信息','完成连接设置','检查设备数据']" :key="title" :class="{active:draft.step===index,done:draft.step>index}" :aria-current="draft.step===index ? 'step' : undefined"><span class="step-number">{{ index+1 }}</span><span class="step-copy"><strong>{{ title }}</strong><small>{{ ['确定接入方式','核对现场标识','确认连接资料','查看真实上报'][index] }}</small></span></li></ol>

    <ui-card v-if="draft.step===0" class="surface-card onboarding-card" shadow="never">
      <div class="step-intro"><span>第 1 步 / 共 4 步</span><h3>选择设备模板</h3><p>先确定设备如何接入，再选择已有模板或创建新模板。</p></div>
      <section class="onboarding-section">
        <div class="section-title"><h4>这次接入哪种设备？</h4><p>请选择最符合现场情况的一项。</p></div>
        <div class="scenario-grid" role="group" aria-label="设备接入方式">
          <button type="button" class="scenario-option" :class="{selected:draft.scenario==='existing'}" :aria-pressed="draft.scenario==='existing'" aria-label="已有型号" @click="selectScenario('existing')"><strong>已有型号</strong><small>选现成模板，登记一台设备</small></button>
          <button type="button" class="scenario-option" :class="{selected:draft.scenario==='new'}" :aria-pressed="draft.scenario==='new'" aria-label="首次接入新型号" @click="selectScenario('new')"><strong>首次接入新型号</strong><small>先建模板，再登记设备</small></button>
          <button type="button" class="scenario-option" :class="{selected:draft.scenario==='child'}" :aria-pressed="draft.scenario==='child'" aria-label="主设备下的子设备" @click="selectScenario('child')"><strong>主设备下的子设备</strong><small>沿用主设备的连接与映射</small></button>
        </div>
      </section>
      <template v-if="draft.scenario==='child'">
        <section class="onboarding-section"><div class="section-title"><h4>选择主设备与子设备模板</h4><p>子设备沿用主设备的网络连接，需要已有的子设备类型映射。</p></div>
          <ui-alert v-if="!possibleParents.length" title="暂无可选主设备。请先登记主设备，再到平台连接配置中设置子设备类型映射。" type="info" :closable="false" class="top-gap" />
          <ui-form-item label="所属主设备" required><ui-select v-model="draft.parentId" filterable placeholder="选择主设备" @change="changeParent"><ui-option v-for="d in possibleParents" :key="d.id" :value="d.id" :label="`${d.name} · ${d.id}`" /></ui-select></ui-form-item>
          <div v-if="draft.parentId && !parentProfile" class="onboarding-helper-prompt">
            <div><strong>主设备还没有平台连接配置</strong><p>在当前向导中补齐连接后，即可继续选择子设备模板。</p></div>
            <div class="onboarding-helper-actions"><ui-button type="primary" @click="helper='profile'">打开连接配置抽屉</ui-button></div>
          </div>
          <div v-else-if="draft.parentId && !childMappings.length" class="onboarding-helper-prompt">
            <div><strong>主设备缺少子设备类型映射</strong><p>在当前向导中编辑主设备连接配置，添加对应的子设备类型。</p></div>
            <div class="onboarding-helper-actions"><ui-button type="primary" @click="helper='profile'">打开连接配置抽屉</ui-button></div>
          </div>
          <div v-else-if="childMappings.length"><ui-form-item label="子设备模板" required><ui-select v-model="draft.productId" filterable placeholder="选择映射的设备模板"><ui-option v-for="m in childMappings" :key="m.type" :value="m.productId" :label="`${products.find(p=>p.id===m.productId)?.name||m.productId} · ${m.type}`" /></ui-select></ui-form-item>
            <div v-if="childrenLoading">正在读取已登记的子设备…</div><div v-else-if="children.length"><p>已登记的子设备（可能由协议发现或人工登记）：</p><div v-for="row in children" :key="row.device.id" class="onboarding-choice"><span>{{ row.device.name }} · 地址 {{ row.device.tags?.childAddress || '未提供' }}</span><ui-button @click="inspectChild(row)">查看接入结果</ui-button></div></div><p v-else>目前没有已登记的子设备。可以按主设备协议中的地址先行登记；此处不会伪造发现结果。</p></div>
        </section>
      </template>
      <template v-else-if="draft.scenario==='existing'">
        <section class="onboarding-section"><div class="section-title"><h4>选择现有模板</h4><p>同型号或共用通信协议的设备可以使用同一模板。</p></div>
          <ui-form-item label="选择设备模板" required><ui-select v-model="draft.productId" filterable placeholder="按名称或型号选择"><ui-option v-for="p in products" :key="p.id" :value="p.id" :label="`${p.name} ${p.metadata?.manufacturer||''} ${p.metadata?.model||''} · ${p.transport||'通信方式未设置'}${p.status==='ENABLED'?'':' · 未启用'}`" /></ui-select></ui-form-item>
          <div v-if="product" class="selection-summary"><strong>{{ product.name }}</strong><span>通信协议：{{ protocolName(product.protocolPackageId) }}</span><span>上报方式：{{ product.transport || '未设置' }}</span><small v-if="product.description">{{ product.description }}</small></div>
          <p v-if="!products.length">暂无设备模板。<ui-button link @click="selectScenario('new')">首次接入新型号</ui-button></p>
        </section>
      </template>
      <template v-else>
        <section class="onboarding-section"><div class="section-title"><h4>模板基本信息</h4><p>先给同型号设备建立一套共用配置。</p></div><div class="onboarding-grid"><ui-form-item label="模板名称" required><ui-input v-model="draft.newName" placeholder="例如 厂商及型号" /></ui-form-item><ui-form-item label="设备分类"><ui-select v-model="draft.category"><ui-option v-for="(title,id) in categories" :key="id" :value="id" :label="title" /></ui-select></ui-form-item></div></section>
        <section class="onboarding-section"><div class="section-title"><h4>通信协议</h4><p>选择与真实设备匹配的已发布协议。</p></div><div class="onboarding-grid"><ui-form-item label="设备通信协议" required><ui-select v-model="draft.protocolPackageId" filterable placeholder="选择已发布协议" @change="chooseProtocol"><ui-option value="iot-standard@1.0.0" label="标准设备上报 · HTTP / MQTT" /><ui-option v-for="p in [...packages.map(x=>({id:x.id,name:x.name})),...releases]" :key="p.id" :value="p.id" :label="p.name" /></ui-select></ui-form-item><ui-form-item v-if="selectedProtocol()?.transport==='MQTT_HTTP' || selectedProtocol()?.transport==='TCP_UDP'" label="通信方式"><ui-select v-model="draft.transport"><ui-option v-for="t in selectedProtocol()?.transport==='MQTT_HTTP'?['HTTP','MQTT']:['TCP','UDP']" :key="t" :value="t" :label="t" /></ui-select></ui-form-item></div>
          <div class="onboarding-helper-prompt">
            <div><strong>找不到匹配的通信协议？</strong><p>准备厂家协议说明、真实报文或点表，在向导内生成协议；复杂协议可进入独立页面开发。</p></div>
            <div class="onboarding-helper-actions">
              <ui-button v-permission="'POST /api/v1/ai/protocol-assistant/generate'" type="primary" @click="helper='protocol'">打开协议生成抽屉</ui-button>
              <ui-button v-permission="'menu:protocols'" @click="navigate('protocols')">前往 Go 协议开发</ui-button>
            </div>
          </div>
        </section>
        <details class="onboarding-optional"><summary>补充型号与编号线索（选填）</summary><div class="onboarding-grid"><ui-form-item label="厂商"><ui-input v-model="draft.manufacturer" /></ui-form-item><ui-form-item label="型号"><ui-input v-model="draft.model" /></ui-form-item><ui-form-item label="编号类型"><ui-input v-model="draft.idKind" placeholder="例如 IMEI、序列号、设备地址" /></ui-form-item><ui-form-item label="编号位置"><ui-input v-model="draft.idLocation" placeholder="例如设备铭牌、厂家配置工具" /></ui-form-item></div></details>
      </template>
      <div class="onboarding-actions"><span>确认模板后，下一步填写现场设备信息。</span><ui-button type="primary" :loading="busy" @click="advanceTemplate">下一步</ui-button></div>
    </ui-card>

    <ui-card v-else-if="draft.step===1" class="surface-card onboarding-card" shadow="never">
      <div class="step-intro"><span>第 2 步 / 共 4 步</span><h3>填写设备信息</h3><p>填写现场可识别的名称和编号。设备编号必须与上报中的标识一致。</p></div>
      <div class="selection-summary selection-summary-compact"><strong>{{ product?.name || '设备模板不可用' }}</strong><span>通信协议：{{ protocolName(product?.protocolPackageId) }}</span><span>通信方式：{{ product?.transport || '未设置' }}</span></div>
      <section class="onboarding-section"><div class="section-title"><h4>设备身份</h4><p>这些信息用于识别现场的这一台设备。</p></div>
        <ui-form-item label="设备名称" required><ui-input v-model="draft.name" placeholder="例如 一层东侧烟感" /></ui-form-item>
        <template v-if="draft.scenario==='child'"><div class="onboarding-parent-line">所属主设备：<strong>{{ parent?.name || '主设备不可用' }}</strong></div><div class="onboarding-grid"><ui-form-item label="子设备类型" required><ui-select v-model="draft.childType"><ui-option v-for="m in childMappings.filter(x=>x.productId===draft.productId)" :key="m.type" :value="m.type" :label="m.type" /></ui-select></ui-form-item><ui-form-item label="子设备地址" required><ui-input v-model="draft.childAddress" placeholder="按主设备协议填写地址" /></ui-form-item></div></template>
        <template v-else><ui-form-item label="实际设备编号" required><ui-input v-model="draft.deviceId" :disabled="registered" :placeholder="metadata.idKind ? `填写${metadata.idKind}` : '填写设备上报的标识'" /></ui-form-item><p class="onboarding-help">{{ metadata.idKind ? `编号类型：${metadata.idKind}。` : '编号可能是 IMEI、序列号或设备地址，请以协议说明为准。' }}{{ metadata.idLocation ? `可在${metadata.idLocation}找到。` : '可查看铭牌或咨询厂家。' }}</p><div v-if="isStandard" class="onboarding-inline-note"><span>没有现场编号？可使用平台编号，并将它配置到设备的上报地址或 Topic。</span><ui-button link @click="draft.deviceId=draft.generatedId">使用平台编号</ui-button></div></template>
      </section>
      <section v-if="draft.scenario!=='child'" class="onboarding-section"><div class="section-title"><h4>接入关系</h4><p>如果这台设备会继续连接其他设备，请选“下接其他设备”。</p></div><ui-radio-group v-model="draft.deviceRole" class="segmented-choice-group" aria-label="接入关系"><ui-radio-button value="DIRECT">独立接入</ui-radio-button><ui-radio-button value="GATEWAY">下接其他设备</ui-radio-button></ui-radio-group></section>
      <details v-if="draft.scenario!=='child'" class="onboarding-optional"><summary>安装位置与标签（选填）</summary><ui-form-item label="安装位置 / 备注"><ui-input v-model="draft.description" type="textarea" :rows="2" /></ui-form-item><div class="onboarding-labels"><h4>设备标签</h4><p>可用名称与内容记录楼层、区域等线索。</p><div v-for="(row,index) in draft.labels" :key="index" class="onboarding-label-row"><label>名称<ui-input v-model="row.key" placeholder="例如楼层" /></label><label>内容<ui-input v-model="row.value" placeholder="例如一层" /></label><ui-button @click="draft.labels.splice(index,1)">移除</ui-button></div><p v-if="!draft.labels.length" class="onboarding-label-empty">尚未添加标签。</p><ui-button @click="draft.labels.push({key:'',value:''})">添加标签</ui-button></div></details>
      <div class="onboarding-actions"><ui-button @click="draft.step=0">上一步</ui-button><ui-button type="primary" @click="advanceInfo">下一步</ui-button></div>
    </ui-card>

    <ui-card v-else-if="draft.step===2" class="surface-card onboarding-card" shadow="never">
      <div class="step-intro"><span>第 3 步 / 共 4 步</span><h3>完成连接设置</h3><p>核对设备与连接方式，然后保存并开始检查现场上报。</p></div>
      <div class="selection-summary selection-summary-compact"><strong>{{ draft.name }}</strong><span>设备编号：{{ draft.scenario==='child' ? draft.childAddress : draft.deviceId }}</span><span>设备模板：{{ product?.name || '不可用' }}</span></div>
      <section v-if="draft.scenario==='child'" class="onboarding-section"><div class="section-title"><h4>沿用主设备连接</h4><p>子设备由主设备协议识别，不需另建网络连接。</p></div><div class="onboarding-inline-note">主设备：<strong>{{ parent?.name || '主设备不可用' }}</strong>　子设备地址：<code>{{ draft.childAddress }}</code></div></section>
      <section v-else-if="isStandard" class="onboarding-section"><div class="section-title"><h4>标准 {{ transport }} 接入</h4><p>平台会为这台设备生成独立凭据。</p></div><ui-alert title="保存后会显示设备端上报地址、身份信息和示例；密钥只在首次创建时显示。" type="info" :closable="false" /></section>
      <section v-else class="onboarding-section">
        <div class="section-title"><h4>选择平台连接配置</h4><p>共享 TCP / UDP 监听可供多台设备使用，不需为每台设备重复创建端口。</p></div>
        <ui-form-item label="平台连接配置"><ui-select v-model="draft.profileId" clearable filterable placeholder="选择已有连接"><ui-option v-for="p in connections" :key="p.id" :value="p.id" :label="`${p.id} · ${p.host}:${p.port} · ${p.enabled?'已启用':'未启用'} · ${p.runtimeStatus||'待检查'}`" /></ui-select></ui-form-item>
        <p v-if="chosenProfile?.mode==='poll' || chosenProfile?.connectionMode==='dial'">平台将主动连接 {{ chosenProfile.host }}:{{ chosenProfile.port }}。{{ chosenProfile.mode==='poll' ? `站号 ${chosenProfile.unitId}，采集周期由连接配置决定。` : '' }}</p>
        <p v-else-if="chosenProfile">平台监听 {{ chosenProfile.host }}:{{ chosenProfile.port }}；现场设备应填写平台对外地址，不能填写 0.0.0.0。<strong v-if="chosenProfile.publicHost && !['0.0.0.0','::','[::]'].includes(chosenProfile.publicHost)">设备端服务器：{{ chosenProfile.publicHost }}:{{ chosenProfile.port }}</strong></p>
        <div v-if="!connections.length || (chosenProfile && (!chosenProfile.publicHost || ['0.0.0.0','::','[::]'].includes(chosenProfile.publicHost)))" class="onboarding-helper-prompt">
          <div><strong>{{ !connections.length ? '还没有适用的平台连接？' : '当前连接缺少对外地址' }}</strong><p>{{ !connections.length ? '在当前向导中创建连接；设备保存后可关联并检查。' : '设备端需要填写可访问的平台地址，请在当前向导中补齐。' }}</p></div>
          <div class="onboarding-helper-actions"><ui-button v-if="!connections.length" v-permission="'POST /api/v2/device-access-profiles'" type="primary" @click="helper='profile-new'">打开连接配置抽屉</ui-button><ui-button v-else v-permission="'PUT /api/v2/device-access-profiles/:id'" type="primary" @click="helper='profile'">打开连接配置抽屉</ui-button></div>
        </div>
        <div v-else class="onboarding-helper-prompt onboarding-helper-prompt-quiet">
          <div><strong>需要使用其他连接？</strong><p>在向导内新建连接，保存后会自动回到当前步骤。</p></div>
          <div class="onboarding-helper-actions"><ui-button v-permission="'POST /api/v2/device-access-profiles'" @click="helper='profile-new'">新建连接配置（抽屉）</ui-button></div>
        </div>
      </section>
      <div class="onboarding-actions"><ui-button v-if="!registered" @click="draft.step=1">上一步</ui-button><ui-button type="primary" :loading="busy" @click="registered ? (draft.step=3,refreshCheck()) : saveDevice()">{{ registered ? '进入接入检查' : '保存设备并检查' }}</ui-button></div>
    </ui-card>

    <ui-card v-else class="surface-card onboarding-card" shadow="never">
      <div class="step-intro"><span>第 4 步 / 共 4 步</span><h3>检查设备数据</h3><p>{{ savedDevice?.name || draft.name }} · {{ draft.createdDeviceId || draft.deviceId }}</p></div>
      <div class="onboarding-status" role="status"><div><small>当前接入结果</small><ui-tag :type="status.tone">{{ status.text }}</ui-tag></div><p>{{ status.next }}</p></div>
      <section class="onboarding-section"><div class="section-title"><h4>检查进度</h4></div>
        <div class="onboarding-check-grid"><div v-for="item in checkItems" :key="item.label" :class="`check-${item.state}`"><component :is="checkIcons[item.state]" class="check-icon" :aria-label="{passed:'通过',waiting:'等待',failed:'失败'}[item.state]" /><strong>{{ item.label }}</strong><p>{{ item.text }}</p></div></div>
      </section>
      <details class="onboarding-check-details"><summary>了解详情</summary><p>仅统计本次配置保存后、与当前设备和协议版本匹配的现场报文。保存成功、连接建立或样本校验均不代表现场解析成功；刷新只查询状态，不发送报文或产生告警。</p><p v-if="check?.ingest?.simulationCount">另有 {{ check.ingest.simulationCount }} 条模拟或管理接口上报记录，未计入本次检查。</p></details>
      <details v-if="check?.profile?.lastError"><summary>查看连接运行错误</summary><pre>{{ check.profile.lastError }}</pre></details>
      <details v-if="check?.ingest?.parseError"><summary>查看解析错误</summary><pre>{{ check.ingest.parseError }}</pre></details>
      <section v-if="values.length" class="onboarding-section"><div class="section-title"><h4>最近解析的数据</h4></div><div v-for="item in values" :key="item.name" class="onboarding-choice"><span>{{ item.name }}</span><strong>{{ typeof item.value==='object' ? JSON.stringify(item.value) : item.value }}{{ item.unit ? ` ${item.unit}` : '' }}</strong></div></section>
      <section v-if="check?.accessInfo" class="onboarding-info"><h4>在现场设备上填写</h4><div class="connection-data-list"><div v-if="check.connector==='HTTP'"><span>上报地址</span><code>{{ check.accessInfo.httpUrl || '未配置平台对外 HTTP 地址' }}</code></div><template v-if="check.connector==='MQTT'"><div><span>Broker</span><code>{{ check.accessInfo.mqttBroker || '未配置对外 MQTT 地址' }}</code></div><div><span>Client ID</span><code>{{ check.accessInfo.clientId }}</code></div><div><span>上行 Topic</span><code>{{ check.accessInfo.upTopic }}</code></div></template><div><span>设备 AccessKey</span><code>{{ check.accessInfo.username }}</code></div></div><p v-if="check.connector==='MQTT'" class="onboarding-help">先用 AccessKey 和 Secret 调用 <code>{{ check.accessInfo.tokenEndpoint }}</code> 换取短期 MQTT token，再以返回的 username / token 连接 Broker；Secret 不能直接作为 MQTT 密码。</p><p class="onboarding-help">Secret 仅在首次创建或轮换时显示。</p><details><summary>标准属性上报示例</summary><pre>{{ JSON.stringify(check.accessInfo.sample,null,2) }}</pre></details></section>
      <section v-else-if="check?.parent" class="onboarding-info"><h4>在平台上设置</h4><p>所属主设备：{{ check.parent.name }}。子设备地址由主设备协议识别，不需独立网络连接。</p></section>
      <section v-else-if="check?.profile" class="onboarding-info"><h4>{{ check.profile.mode==='poll' || check.profile.connectionMode==='dial' ? '在平台上设置' : '在现场设备上填写' }}</h4><p v-if="check.profile.mode==='poll' || check.profile.connectionMode==='dial'">设备地址 {{ check.profile.host }}:{{ check.profile.port }}，站号 {{ check.profile.unitId }}。</p><p v-else>服务器 {{ check.profile.publicHost ? `${check.profile.publicHost}:${check.profile.port}` : '对外地址尚未配置' }}。平台监听地址 {{ check.profile.host }} 仅用于服务端。</p></section>
      <section v-if="!isStandard && draft.scenario!=='child'" class="onboarding-info"><h4>在平台上设置</h4><ui-form-item label="选择平台连接配置"><ui-select v-model="draft.profileId" filterable placeholder="选择适用连接"><ui-option v-for="p in connections" :key="p.id" :value="p.id" :label="`${p.id} · ${p.host}:${p.port} · ${p.enabled?'已启用':'未启用'}`" /></ui-select></ui-form-item><div class="onboarding-helper-actions"><ui-button :loading="busy" :disabled="!chosenProfile" type="primary" @click="linkProfile">关联该连接并重新检查</ui-button><ui-button v-permission="'POST /api/v2/device-access-profiles'" @click="helper='profile-new'">新建连接配置（抽屉）</ui-button><ui-button v-if="chosenProfile && !chosenProfile.publicHost" v-permission="'PUT /api/v2/device-access-profiles/:id'" @click="helper='profile'">补齐对外地址（抽屉）</ui-button></div></section>
      <div v-if="credential" class="onboarding-info onboarding-credential"><ui-alert title="设备 Secret 只显示这一次，请安全保存。关闭或刷新后无法再读取。" type="warning" :closable="false" /><div class="connection-data-list"><div><span>AccessKey</span><code>{{ credential.accessKey }}</code></div><div><span>Secret</span><code class="break-all">{{ credential.secret }}</code></div></div><ui-button @click="credential=null">我已保存</ui-button></div>
      <p v-else-if="isStandard && check?.credentialEnabled">首次 Secret 未保留在草稿中；若丢失，请在设备连接详情重新生成。</p>
      <div class="onboarding-actions onboarding-actions-check"><div class="onboarding-actions-secondary"><ui-button @click="draft.step=2">返回连接设置</ui-button><ui-button :loading="loading" @click="refreshCheck">刷新接入检查</ui-button><ui-button v-if="check?.ingest?.rawMessageId" v-permission="'menu:raw'" @click="navigate('raw',{deviceId:draft.createdDeviceId,rawMessageId:check.ingest.rawMessageId})">查看原始报文</ui-button><ui-button @click="emit('detail',draft.createdDeviceId)">连接详情</ui-button></div><ui-button type="primary" @click="finish">进入日常管理</ui-button></div>
    </ui-card>
    <ui-drawer :model-value="helper!==''" :title="helper==='protocol'?'从报文或点表生成协议':'平台连接配置'" size="min(780px, 100vw)" @close="helper=''">
      <div class="onboarding-helper">
        <template v-if="helper==='protocol'"><p>保存并发布协议后，关闭抽屉即可在模板中选择。专用 Go 源码仍可从设备通信协议页面开发。</p><ProtocolAssistantView @saved="helperSaved" @navigate="helper=''" /></template>
        <OnboardingProfileEditor v-else-if="helper.startsWith('profile')" :key="helper" :product="product" :parent="parent" :device-id="draft.createdDeviceId || draft.deviceId" :existing-ids="profiles.map(p=>p.id)" :profile="helper==='profile-new' ? null : chosenProfile || parentProfile" @saved="profileSaved" @close="helper=''" />
      </div>
    </ui-drawer>
  </section>
</template>

<style scoped>
.onboarding-workspace{max-width:1060px;margin:0 auto;color:var(--foreground)}
.onboarding-workspace p{line-height:1.6;color:var(--muted-foreground)}
.onboarding-header{display:flex;justify-content:space-between;align-items:flex-start;gap:24px;padding:22px 26px;background:var(--card);border:1px solid var(--border);border-radius:12px}
.onboarding-eyebrow{display:block;margin-bottom:7px;color:var(--muted-foreground);font-size:12px;font-weight:700;letter-spacing:.04em}
.onboarding-header h2{margin:0;color:var(--text-strong);font-size:22px;line-height:1.3}
.onboarding-header p{margin:9px 0 0;font-size:13px}
.onboarding-header-actions{display:flex;flex-wrap:wrap;justify-content:flex-end;gap:8px;flex:none}
.onboarding-steps{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px;list-style:none;margin:16px 0 20px;padding:0}
.onboarding-steps li{display:flex;align-items:center;gap:10px;min-width:0;padding:12px 14px;background:var(--card);border:1px solid var(--border);border-radius:9px}
.onboarding-steps li.active{background:var(--accent);border-color:var(--border-info)}
.onboarding-steps li.done{background:var(--surface-success);border-color:var(--border-success)}
.step-number{display:grid;place-items:center;flex:none;width:28px;height:28px;color:var(--text-strong);background:var(--surface-subtle);border-radius:50%;font-size:13px;font-weight:700}
.active .step-number{color:var(--foreground);background:var(--brand-flame)}.done .step-number{color:var(--success-foreground);background:var(--surface-success)}
.step-copy{display:flex;flex-direction:column;gap:2px;min-width:0}.step-copy strong{color:var(--text-strong);font-size:13px;line-height:1.3}.step-copy small{color:var(--muted-foreground);font-size:11px;line-height:1.3}
.onboarding-card{border-radius:12px}.onboarding-card :deep(.n-card__content){padding:26px 30px 30px}
.step-intro{padding-bottom:20px;margin-bottom:22px;border-bottom:1px solid var(--border)}.step-intro>span{color:var(--primary);font-size:12px;font-weight:700}.step-intro h3{margin:6px 0 5px;color:var(--text-strong);font-size:22px;line-height:1.3}.step-intro p{margin:0;font-size:13px}
.onboarding-section{margin:0 0 24px}.onboarding-section+.onboarding-section{padding-top:24px;border-top:1px solid var(--border)}.section-title{margin-bottom:16px}.section-title h4{margin:0 0 4px;color:var(--text-strong);font-size:16px}.section-title p{margin:0;font-size:13px}
.scenario-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px}.scenario-option{display:flex;flex-direction:column;align-items:flex-start;gap:7px;min-height:86px;padding:17px 18px;text-align:left;background:var(--card);border:1px solid var(--border);border-radius:9px;cursor:pointer}.scenario-option strong{color:var(--text-strong);font-size:15px}.scenario-option small{color:var(--muted-foreground);font-size:12px;line-height:1.5}.scenario-option:hover{border-color:var(--border-info);background:var(--surface-subtle)}.scenario-option.selected{background:var(--accent);border-color:var(--primary);box-shadow:0 0 0 1px var(--primary)}.scenario-option.selected strong{color:var(--text-strong)}.scenario-option:focus-visible{outline:2px solid var(--primary);outline-offset:2px}
.selection-summary{display:flex;flex-wrap:wrap;align-items:center;gap:6px 18px;padding:14px 17px;background:var(--surface-subtle);border-left:3px solid var(--primary);border-radius:5px;color:var(--text-strong);font-size:13px}.selection-summary strong{flex-basis:100%;color:var(--text-strong);font-size:15px}.selection-summary small{flex-basis:100%;color:var(--text-strong)}.selection-summary-compact{margin-bottom:24px}
.onboarding-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:0 18px}.onboarding-grid>div{min-width:0}
.onboarding-help{margin:7px 0 0;font-size:12px}.onboarding-inline-note{display:flex;align-items:center;flex-wrap:wrap;gap:8px;padding:12px 15px;background:var(--surface-subtle);border:1px solid var(--border);border-radius:7px;color:var(--text-strong);font-size:13px}.onboarding-inline-note :deep(.n-button){margin-left:auto}.onboarding-parent-line{margin:0 0 16px;color:var(--text-strong);font-size:13px}.onboarding-parent-line strong{color:var(--text-strong)}
.onboarding-helper-prompt{display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:14px;margin-top:14px;padding:16px 18px;border:1px solid var(--border-info);border-left:3px solid var(--primary);border-radius:8px;background:var(--surface-subtle)}
.onboarding-helper-prompt>div:first-child{flex:1 1 280px;min-width:0}
.onboarding-helper-prompt strong{display:block;color:var(--text-strong);font-size:14px}
.onboarding-helper-prompt p{margin:4px 0 0;color:var(--muted-foreground);font-size:13px;line-height:1.5}
.onboarding-helper-prompt-quiet{border-left-color:var(--border-info)}
.onboarding-helper-actions{display:flex;align-items:center;flex-wrap:wrap;gap:8px}
.onboarding-helper-actions :deep(.n-button){min-height:34px;white-space:normal;text-align:center}
.onboarding-optional{padding:14px 17px;margin:0 0 22px;border:1px dashed var(--border);border-radius:8px;background:var(--card)}.onboarding-optional summary{cursor:pointer;color:var(--primary);font-size:13px;font-weight:700}.onboarding-optional[open] summary{margin-bottom:18px}.onboarding-optional h4{margin:12px 0 8px;font-size:13px}.onboarding-optional .onboarding-grid{gap:0 18px}
.onboarding-labels{padding:12px;border:1px solid var(--border);border-radius:8px;background:var(--card)}.onboarding-labels h4{margin:0 0 3px}.onboarding-labels>p{margin:0 0 10px;font-size:12px}.onboarding-label-row{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr) auto;gap:8px;align-items:end;margin:10px 0}.onboarding-label-row label{display:grid;min-width:0;gap:5px;color:var(--text-secondary);font-size:12px;font-weight:600}.onboarding-label-row :deep(.n-input){width:100%}.onboarding-label-empty{padding:10px;border:1px dashed var(--border-info);border-radius:7px;background:var(--card)}
.onboarding-choice{display:flex;align-items:center;gap:8px;justify-content:space-between;margin:8px 0}.onboarding-choice>*{min-width:0;overflow-wrap:anywhere}.onboarding-choice :deep(.n-input){flex:1}
.onboarding-status{display:flex;justify-content:space-between;align-items:center;gap:18px;margin:0 0 26px;padding:17px 20px;background:var(--surface-subtle);border:1px solid var(--border-info);border-radius:9px}.onboarding-status>div{display:flex;align-items:center;flex-wrap:wrap;gap:10px}.onboarding-status small{color:var(--muted-foreground);font-size:12px;font-weight:700}.onboarding-status p{margin:0;color:var(--text-strong);font-size:13px}
.onboarding-check-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px}.onboarding-check-grid>div{min-width:0;padding:13px 15px;background:var(--card);border:1px solid var(--border);border-radius:7px}.onboarding-check-grid strong{display:block;color:var(--muted-foreground);font-size:12px;font-weight:600}.onboarding-check-grid p{margin:5px 0 0;color:var(--text-strong);font-size:14px;font-weight:600;overflow-wrap:anywhere}
.onboarding-check-grid>div{display:grid;grid-template-columns:20px minmax(0,1fr);column-gap:8px;align-content:start}
.onboarding-check-grid .check-icon{grid-row:span 2;width:18px;height:18px;margin-top:1px}
.onboarding-check-grid .check-passed .check-icon{color:var(--success)}
.onboarding-check-grid .check-waiting .check-icon{color:var(--muted-foreground)}
.onboarding-check-grid .check-failed .check-icon{color:var(--destructive)}
.onboarding-check-grid .check-failed{border-color:var(--border-danger);background:var(--surface-danger)}
.onboarding-check-grid .check-passed{border-color:var(--border-success);background:var(--surface-success)}
.onboarding-check-grid p{grid-column:2}
.onboarding-check-details{margin:0 0 18px;color:var(--muted-foreground);font-size:13px}
.onboarding-check-details summary{width:max-content;cursor:pointer;color:var(--primary);font-weight:600}
.onboarding-helper{padding:8px 20px 28px}.onboarding-helper>p{color:var(--muted-foreground);font-size:13px;line-height:1.6}
.onboarding-info{padding:19px 21px;margin-top:20px;border:1px solid var(--border);border-radius:9px;background:var(--card);overflow-wrap:anywhere}.onboarding-info h4{margin:0 0 14px;color:var(--text-strong);font-size:16px}.onboarding-info pre{overflow:auto;max-height:240px}.onboarding-info code{overflow-wrap:anywhere}.onboarding-info details{margin-top:14px}.connection-data-list{display:grid;gap:0}.connection-data-list>div{display:grid;grid-template-columns:138px minmax(0,1fr);gap:12px;align-items:start;padding:10px 0;border-bottom:1px solid var(--border)}.connection-data-list>div:last-child{border-bottom:0}.connection-data-list span{color:var(--muted-foreground);font-size:12px}.connection-data-list code{color:var(--text-strong);font-size:12px;line-height:1.6;word-break:break-all}.onboarding-credential{background:var(--surface-subtle);border-color:var(--border-warning)}.onboarding-credential :deep(.n-button){margin-top:12px}
.onboarding-actions{display:flex;justify-content:flex-end;align-items:center;flex-wrap:wrap;gap:10px;margin-top:28px;padding-top:20px;border-top:1px solid var(--border)}.onboarding-actions>span{flex:1;color:var(--muted-foreground);font-size:12px}.onboarding-actions-secondary{display:flex;flex-wrap:wrap;gap:8px;margin-right:auto}
@media(max-width:900px){.onboarding-header{flex-direction:column;gap:16px}.onboarding-header-actions{justify-content:flex-start}.step-copy small{display:none}.onboarding-steps li{padding:10px}.scenario-grid{gap:8px}}
@media(max-width:640px){.onboarding-header{padding:14px 16px;gap:10px}.onboarding-eyebrow,.onboarding-header p{display:none}.onboarding-header h2{font-size:18px}.onboarding-header-actions{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));width:100%}.onboarding-header-actions :deep(.n-button){min-width:0;padding-inline:4px}.onboarding-steps{grid-template-columns:repeat(2,minmax(0,1fr));gap:7px;margin:12px 0 16px}.onboarding-steps li{gap:7px;min-height:49px;padding:8px}.step-number{width:23px;height:23px;font-size:12px}.step-copy strong{font-size:12px}.onboarding-card :deep(.n-card__content){padding:18px 16px}.step-intro{padding-bottom:12px;margin-bottom:15px}.step-intro h3{font-size:18px}.step-intro p{font-size:12px}.scenario-grid,.onboarding-grid,.onboarding-check-grid{grid-template-columns:1fr}.scenario-option{min-height:0;padding:12px 14px;gap:3px}.onboarding-section{margin-bottom:20px}.onboarding-section+.onboarding-section{padding-top:20px}.onboarding-status{align-items:flex-start;flex-direction:column;padding:15px}.onboarding-helper-prompt{padding:14px}.onboarding-helper-actions{width:100%;flex-direction:column;align-items:stretch}.onboarding-helper-actions :deep(.n-button){width:100%}.connection-data-list>div{grid-template-columns:1fr;gap:3px}.onboarding-choice{flex-wrap:wrap}.onboarding-choice :deep(.n-input){flex-basis:calc(50% - 4px)}.onboarding-label-row{grid-template-columns:repeat(2,minmax(0,1fr))}.onboarding-label-row .n-button{justify-self:start}.onboarding-actions>span{display:none}.onboarding-actions-check{align-items:stretch;flex-direction:column}.onboarding-actions-check>.n-button{width:100%}.onboarding-actions-secondary{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));width:100%;margin:0}.onboarding-actions-secondary :deep(.n-button){min-width:0}}
</style>
