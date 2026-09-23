<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api, apiAll, formatTime, notifyError, session } from '../api'
import { can } from '../permissions'
import { createClientId } from '../clientId'
import { categories } from '../labels'
import { UiMessage } from '../ui/feedback.js'

const emit = defineEmits(['close', 'done', 'navigate', 'detail'])
const products = ref([]), packages = ref([]), releases = ref([]), profiles = ref([]), devices = ref([])
const loading = ref(false), busy = ref(false), error = ref(''), check = ref(null), credential = ref(null)
const children = ref([]), childrenLoading = ref(false)
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
function finish() { localStorage.removeItem(key()); emit('done') }
function reset() { localStorage.removeItem(key()); Object.assign(draft, fresh()); credential.value = null; check.value = null; error.value = ''; children.value = [] }
onMounted(() => { restore(); load() })
</script>

<template>
  <section class="onboarding-workspace">
    <div class="page-toolbar"><strong>接入设备</strong><ui-button @click="emit('close')">返回设备列表</ui-button><ui-button :loading="loading" @click="load">刷新配置</ui-button><ui-button @click="reset">重新开始</ui-button></div>
    <ui-alert v-if="error" :title="error" type="warning" :closable="false" show-icon class="top-gap" />
    <div class="onboarding-steps"><span v-for="(title,index) in ['选择设备模板','填写设备信息','完成连接设置','检查设备数据']" :key="title" :class="{active:draft.step===index,done:draft.step>index}">{{ index+1 }}. {{ title }}</span></div>

    <ui-card v-if="draft.step===0" class="surface-card" shadow="never">
      <h3>这次要接入哪种设备？</h3>
      <ui-radio-group :model-value="draft.scenario" @update:model-value="selectScenario"><ui-radio-button value="existing">已有型号</ui-radio-button><ui-radio-button value="new">首次接入新型号</ui-radio-button><ui-radio-button value="child">主设备下的子设备</ui-radio-button></ui-radio-group>
      <template v-if="draft.scenario==='child'">
        <p>子设备沿用主设备的网络连接。先选择已经登记且已配置子设备类型的主设备。</p>
        <ui-form-item label="所属主设备" required><ui-select v-model="draft.parentId" filterable placeholder="选择主设备" @change="changeParent"><ui-option v-for="d in possibleParents" :key="d.id" :value="d.id" :label="`${d.name} · ${d.id}`" /></ui-select></ui-form-item>
        <p v-if="draft.parentId && !parentProfile">主设备尚未关联平台连接配置。<ui-button link @click="navigate('profiles')">去补齐连接</ui-button></p>
        <p v-else-if="draft.parentId && !childMappings.length">主设备协议未声明子设备类型映射。<ui-button link @click="navigate('profiles')">配置子设备产品</ui-button></p>
        <div v-else-if="childMappings.length"><ui-form-item label="子设备模板" required><ui-select v-model="draft.productId" filterable placeholder="选择映射的设备模板"><ui-option v-for="m in childMappings" :key="m.type" :value="m.productId" :label="`${products.find(p=>p.id===m.productId)?.name||m.productId} · ${m.type}`" /></ui-select></ui-form-item>
          <div v-if="childrenLoading">正在读取已登记的子设备…</div><div v-else-if="children.length"><p>已登记的子设备（可能由协议发现或人工登记）：</p><div v-for="row in children" :key="row.device.id" class="onboarding-choice"><span>{{ row.device.name }} · 地址 {{ row.device.tags?.childAddress || '未提供' }}</span><ui-button @click="inspectChild(row)">查看接入结果</ui-button></div></div><p v-else>目前没有已登记的子设备。可以按主设备协议中的地址先行登记；此处不会伪造发现结果。</p></div>
      </template>
      <template v-else-if="draft.scenario==='existing'">
        <p>设备模板是同型号或共用通信协议的一套配置。设备类别相同不代表协议相同。</p>
        <ui-form-item label="选择设备模板" required><ui-select v-model="draft.productId" filterable placeholder="按名称或型号选择"><ui-option v-for="p in products" :key="p.id" :value="p.id" :label="`${p.name} ${p.metadata?.manufacturer||''} ${p.metadata?.model||''} · ${p.transport||'通信方式未设置'}${p.status==='ENABLED'?'':' · 未启用'}`" /></ui-select></ui-form-item>
        <p v-if="product">{{ product.description || '暂无用途说明' }} · 协议 {{ product.protocolPackageId || '未配置' }}</p>
        <p v-if="!products.length">暂无设备模板。<ui-button link @click="selectScenario('new')">首次接入新型号</ui-button></p>
      </template>
      <template v-else>
        <p>先创建一套共用配置，再登记现场设备。只能选择已发布协议。</p>
        <div class="onboarding-grid"><ui-form-item label="模板名称" required><ui-input v-model="draft.newName" placeholder="例如 厂商及型号" /></ui-form-item><ui-form-item label="设备分类"><ui-select v-model="draft.category"><ui-option v-for="(title,id) in categories" :key="id" :value="id" :label="title" /></ui-select></ui-form-item><ui-form-item label="厂商（如已知）"><ui-input v-model="draft.manufacturer" /></ui-form-item><ui-form-item label="型号（如已知）"><ui-input v-model="draft.model" /></ui-form-item></div>
        <ui-form-item label="设备通信协议" required><ui-select v-model="draft.protocolPackageId" filterable placeholder="选择与现场设备匹配的已发布协议" @change="chooseProtocol"><ui-option value="iot-standard@1.0.0" label="标准设备上报 · HTTP / MQTT" /><ui-option v-for="p in [...packages.map(x=>({id:x.id,name:x.name})),...releases]" :key="p.id" :value="p.id" :label="p.name" /></ui-select></ui-form-item>
        <ui-form-item v-if="selectedProtocol()?.transport==='MQTT_HTTP' || selectedProtocol()?.transport==='TCP_UDP'" label="通信方式"><ui-select v-model="draft.transport"><ui-option v-for="t in selectedProtocol()?.transport==='MQTT_HTTP'?['HTTP','MQTT']:['TCP','UDP']" :key="t" :value="t" :label="t" /></ui-select></ui-form-item>
        <div class="onboarding-grid"><ui-form-item label="编号类型（如已知）"><ui-input v-model="draft.idKind" placeholder="例如 IMEI、序列号、设备地址" /></ui-form-item><ui-form-item label="编号位置（如已知）"><ui-input v-model="draft.idLocation" placeholder="例如设备铭牌、厂家配置工具" /></ui-form-item></div>
        <p>没有匹配协议？请向厂家索取型号、通信协议说明、真实报文样例或点表。<ui-button v-permission="'menu:protocols'" link @click="navigate('protocols')">前往设备通信协议工作区</ui-button></p>
      </template>
      <div class="onboarding-actions"><ui-button type="primary" :loading="busy" @click="advanceTemplate">下一步</ui-button></div>
    </ui-card>

    <ui-card v-else-if="draft.step===1" class="surface-card" shadow="never"><h3>填写设备信息</h3><p>模板：{{ product?.name || '设备模板不可用' }}。已继承通信协议、通信方式和数据格式。</p>
      <ui-form-item label="设备名称" required><ui-input v-model="draft.name" placeholder="例如 一层东侧烟感" /></ui-form-item>
      <template v-if="draft.scenario==='child'"><ui-form-item label="所属主设备"><strong>{{ parent?.name || '主设备不可用' }}</strong></ui-form-item><ui-form-item label="子设备类型" required><ui-select v-model="draft.childType"><ui-option v-for="m in childMappings.filter(x=>x.productId===draft.productId)" :key="m.type" :value="m.type" :label="m.type" /></ui-select></ui-form-item><ui-form-item label="子设备地址" required><ui-input v-model="draft.childAddress" placeholder="按主设备协议填写地址" /></ui-form-item></template>
      <template v-else><ui-form-item label="实际设备编号" required><ui-input v-model="draft.deviceId" :disabled="registered" :placeholder="metadata.idKind ? `填写${metadata.idKind}` : '填写设备上报的标识'" /></ui-form-item><p>{{ metadata.idKind ? `编号类型：${metadata.idKind}。` : '请核对协议说明，确认编号是 IMEI、序列号、设备地址还是上报标识。' }}{{ metadata.idLocation ? `可在${metadata.idLocation}找到。` : '如不清楚位置，请查看铭牌或咨询厂家。' }}编号须与协议上报的设备标识一致。</p><p v-if="isStandard">标准 HTTP / MQTT 设备可使用平台生成编号，但需将此编号配置到设备的上报地址或 Topic 中。<ui-button link @click="draft.deviceId=draft.generatedId">使用平台编号</ui-button></p><ui-form-item label="接入关系"><ui-radio-group v-model="draft.deviceRole"><ui-radio-button value="DIRECT">独立接入</ui-radio-button><ui-radio-button value="GATEWAY">下接其他设备</ui-radio-button></ui-radio-group></ui-form-item><ui-form-item label="安装位置 / 备注"><ui-input v-model="draft.description" type="textarea" :rows="2" /></ui-form-item><h4>标签</h4><div v-for="(row,index) in draft.labels" :key="index" class="onboarding-choice"><ui-input v-model="row.key" placeholder="名称" /><ui-input v-model="row.value" placeholder="内容" /><ui-button @click="draft.labels.splice(index,1)">移除</ui-button></div><ui-button @click="draft.labels.push({key:'',value:''})">添加标签</ui-button></template>
      <div class="onboarding-actions"><ui-button @click="draft.step=0">上一步</ui-button><ui-button type="primary" @click="advanceInfo">下一步</ui-button></div>
    </ui-card>

    <ui-card v-else-if="draft.step===2" class="surface-card" shadow="never"><h3>完成连接设置</h3>
      <p v-if="draft.scenario==='child'">这台子设备通过 {{ parent?.name || '主设备' }} 接入。平台使用主设备已有连接，只需确认子设备地址 {{ draft.childAddress }}。</p>
      <p v-else-if="isStandard">{{ transport }} 使用当前设备的独立凭据。保存后会显示设备端上报地址、身份信息和示例；密钥只在首次创建时显示。</p>
      <template v-else><p>选择适用的平台连接配置。共享 TCP / UDP 监听可供多台设备使用；不会为每台设备重复创建端口。</p><ui-form-item label="平台连接配置"><ui-select v-model="draft.profileId" clearable filterable placeholder="选择已有连接"><ui-option v-for="p in connections" :key="p.id" :value="p.id" :label="`${p.id} · ${p.host}:${p.port} · ${p.enabled?'已启用':'未启用'} · ${p.runtimeStatus||'待检查'}`" /></ui-select></ui-form-item><p v-if="!connections.length">没有适用的连接。可先登记设备，再配置连接并返回继续。<ui-button link @click="navigate('profiles')">前往平台连接配置</ui-button></p><p v-if="chosenProfile?.mode==='poll' || chosenProfile?.connectionMode==='dial'">平台将主动连接 {{ chosenProfile.host }}:{{ chosenProfile.port }}。{{ chosenProfile.mode==='poll' ? `站号 ${chosenProfile.unitId}，采集周期由连接配置决定。` : '' }}</p><p v-else-if="chosenProfile">平台监听 {{ chosenProfile.host }}:{{ chosenProfile.port }}；现场设备应填写平台对外地址，不能填写 0.0.0.0。<span v-if="!chosenProfile.publicHost || ['0.0.0.0','::','[::]'].includes(chosenProfile.publicHost)">当前未配置对外地址。<ui-button link @click="navigate('profiles')">去设置</ui-button></span><strong v-else>设备端服务器：{{ chosenProfile.publicHost }}:{{ chosenProfile.port }}</strong></p></template>
      <div class="onboarding-actions"><ui-button v-if="!registered" @click="draft.step=1">上一步</ui-button><ui-button type="primary" :loading="busy" @click="registered ? (draft.step=3,refreshCheck()) : saveDevice()">{{ registered ? '进入接入检查' : '保存设备并检查' }}</ui-button></div>
    </ui-card>

    <ui-card v-else class="surface-card" shadow="never"><h3>检查设备数据 · {{ savedDevice?.name || draft.name }}</h3><p>当前只检查本次登记或检测开始后，与这台设备及配置匹配的现场报文。刷新不会发送报文或产生告警。</p>
      <div class="onboarding-status"><ui-tag :type="status.tone">{{ status.text }}</ui-tag><span>{{ status.next }}</span></div>
      <div class="onboarding-grid"><div><strong>配置保存</strong><p>{{ check?.ingest?.configurationSaved ? '已保存' : '等待确认' }}</p></div><div><strong>接收服务</strong><p>{{ isStandard ? (check?.accessInfo?.[check?.connector==='MQTT'?'mqttBroker':'httpUrl'] ? '标准接口已配置，对外可达性待现场确认' : '平台对外地址未配置') : check?.profile ? (check.profile.enabled ? (check.profile.runtimeStatus || '已启用，运行待确认') : '未启用') : '尚未关联连接' }}</p></div><div><strong>当前设备数据</strong><p>{{ check?.ingest?.rawReceived ? `已接收 · ${formatTime(check.ingest.receivedAt)}` : '本次尚未收到' }}</p></div><div><strong>解析结果</strong><p>{{ check?.ingest?.parsed ? '本次解析成功' : check?.ingest?.parseError ? '解析失败' : '尚未解析成功' }}</p></div><div><strong>持续更新</strong><p>{{ check?.ingest?.continuouslyUpdating ? '本次已有连续上报' : check?.ingest?.stale ? '超过 15 分钟无新数据' : '等待后续上报' }}</p></div></div>
      <p v-if="check?.ingest?.simulationCount">本次另有 {{ check.ingest.simulationCount }} 条模拟或管理接口上报记录，未计入现场接入检查。</p>
      <details v-if="check?.profile?.lastError"><summary>查看连接运行错误</summary><pre>{{ check.profile.lastError }}</pre></details>
      <ui-alert v-if="check?.ingest?.parseError" title="收到报文，但未能解析。请核对真实报文与已发布协议版本。" type="warning" :closable="false" /><details v-if="check?.ingest?.parseError"><summary>查看技术错误</summary><pre>{{ check.ingest.parseError }}</pre></details>
      <div v-if="values.length"><h4>最近解析的数据</h4><div v-for="item in values" :key="item.name" class="onboarding-choice"><span>{{ item.name }}</span><strong>{{ typeof item.value==='object' ? JSON.stringify(item.value) : item.value }}{{ item.unit ? ` ${item.unit}` : '' }}</strong></div></div>
      <section v-if="check?.accessInfo" class="onboarding-info"><h4>在现场设备上填写</h4><p v-if="check.connector==='HTTP'">上报地址：<code>{{ check.accessInfo.httpUrl || '未配置平台对外 HTTP 地址' }}</code></p><template v-if="check.connector==='MQTT'"><p>Broker：<code>{{ check.accessInfo.mqttBroker || '未配置对外 MQTT 地址' }}</code></p><p>Client ID：<code>{{ check.accessInfo.clientId }}</code> · 上行 Topic：<code>{{ check.accessInfo.upTopic }}</code></p><p>先以设备 AccessKey 和 Secret 调用 <code>{{ check.accessInfo.tokenEndpoint }}</code> 换取短期 MQTT token，再以返回的 username / token 连接 Broker；不能直接把 Secret 当作 MQTT 密码。</p></template><p>设备 AccessKey：<code>{{ check.accessInfo.username }}</code>。Secret 仅首次创建或轮换时显示。</p><details><summary>标准属性上报示例</summary><pre>{{ JSON.stringify(check.accessInfo.sample,null,2) }}</pre></details></section>
      <section v-else-if="check?.parent" class="onboarding-info"><h4>在平台上设置</h4><p>所属主设备：{{ check.parent.name }}。子设备地址由主设备协议识别，不需独立网络连接。</p></section>
      <section v-else-if="check?.profile" class="onboarding-info"><h4>{{ check.profile.mode==='poll' || check.profile.connectionMode==='dial' ? '在平台上设置' : '在现场设备上填写' }}</h4><p v-if="check.profile.mode==='poll' || check.profile.connectionMode==='dial'">设备地址 {{ check.profile.host }}:{{ check.profile.port }}，站号 {{ check.profile.unitId }}。</p><p v-else>服务器 {{ check.profile.publicHost ? `${check.profile.publicHost}:${check.profile.port}` : '对外地址尚未配置' }}。平台监听地址 {{ check.profile.host }} 仅用于服务端。</p></section>
      <section v-if="!isStandard && draft.scenario!=='child'" class="onboarding-info"><h4>在平台上设置</h4><ui-form-item label="选择平台连接配置"><ui-select v-model="draft.profileId" filterable placeholder="选择适用连接"><ui-option v-for="p in connections" :key="p.id" :value="p.id" :label="`${p.id} · ${p.host}:${p.port} · ${p.enabled?'已启用':'未启用'}`" /></ui-select></ui-form-item><ui-button :loading="busy" :disabled="!chosenProfile" @click="linkProfile">关联该连接并重新检查</ui-button><ui-button v-permission="'menu:profiles'" link @click="navigate('profiles')">管理平台连接配置</ui-button></section>
      <div v-if="credential" class="onboarding-info"><ui-alert title="设备 Secret 只显示这一次，请安全保存。关闭或刷新后无法再读取。" type="warning" :closable="false" /><p>AccessKey：<code>{{ credential.accessKey }}</code></p><p>Secret：<code class="break-all">{{ credential.secret }}</code></p><ui-button @click="credential=null">我已保存</ui-button></div>
      <p v-else-if="isStandard && check?.credentialEnabled">首次 Secret 未保留在草稿中；若丢失，请在设备连接详情重新生成。</p>
      <div class="onboarding-actions"><ui-button @click="draft.step=2">返回连接设置</ui-button><ui-button :loading="loading" @click="refreshCheck">刷新接入检查</ui-button><ui-button v-if="check?.ingest?.rawMessageId" v-permission="'menu:raw'" @click="navigate('raw',{deviceId:draft.createdDeviceId,rawMessageId:check.ingest.rawMessageId})">查看原始报文</ui-button><ui-button @click="emit('detail',draft.createdDeviceId)">连接详情</ui-button><ui-button type="primary" @click="finish">进入日常管理</ui-button></div>
    </ui-card>
  </section>
</template>

<style scoped>
.onboarding-workspace{max-width:1060px;margin:auto}.onboarding-steps{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:8px;margin:18px 0}.onboarding-steps span{padding:12px;border-radius:8px;background:var(--n-color,#f5f7fa);font-size:13px}.onboarding-steps .active{background:#eaf2ff;color:#195fc7;font-weight:700}.onboarding-steps .done{color:#23845d}.onboarding-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.onboarding-grid>div{min-width:0}.onboarding-actions{display:flex;justify-content:flex-end;flex-wrap:wrap;gap:8px;margin-top:24px}.onboarding-choice{display:flex;align-items:center;gap:8px;justify-content:space-between;margin:8px 0}.onboarding-choice>*{min-width:0;overflow-wrap:anywhere}.onboarding-status{display:flex;align-items:center;gap:12px;margin:16px 0}.onboarding-info{padding:12px 16px;margin-top:16px;border-radius:8px;background:#f7f9fc;overflow-wrap:anywhere}.onboarding-info pre{overflow:auto;max-height:240px}.onboarding-info code{overflow-wrap:anywhere}.onboarding-workspace p{line-height:1.6;color:var(--n-text-color-2,#667085)}@media(max-width:640px){.onboarding-steps,.onboarding-grid{grid-template-columns:1fr}.onboarding-choice,.onboarding-status{flex-wrap:wrap}.onboarding-actions{justify-content:stretch}.onboarding-actions>*{flex:1}}
</style>
