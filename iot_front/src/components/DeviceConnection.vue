<script setup>
import { createClientId } from '../clientId'
import CommandValueInput from './CommandValueInput.vue'
import { commandBody } from '../commandForm'
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { UiMessageBox, UiMessage } from '../ui/feedback.js'
import { api, formatTime, notifyError, pretty, session } from '../api'
import { transportLabel, statusLabel } from '../presentation'
import { commandStatuses, alarmType, alarmLevel, alarmStatuses, connectionStatuses, dataStatuses, businessStatuses, stateSources, messageTypeLabel, label } from '../labels'
import { diagnosisTagTypes } from '../onboardingPlan'

const props = defineProps({ deviceId:String })
const emit = defineEmits(['close','navigate','device'])
const data = ref(null), loading = ref(false), actionBusy = ref(false), error = ref(''), selectedProfile = ref('')
const credential = ref(null), commandResult = ref(null)
const commandReply = ref(null)
const commandType = ref('')
const commandValues = ref({})
const operations = computed(() => data.value?.product?.thingModel?.commands || [])
const selectedOperation = computed(() => operations.value.find(item => item.identifier === commandType.value))
watch(commandType, () => { commandValues.value = {} })
const pendingCommand = ref(null), pendingProtocol = ref(null)
const lists = reactive(Object.fromEntries(['history','events','commands','children'].map(key => [key,{items:[],total:0,page:1,loading:false,error:''}])))
const media = window.matchMedia('(max-width: 640px)')
const narrow = ref(media.matches)
const resize = event => { narrow.value = event.matches }
media.addEventListener('change',resize)
const columns = computed(() => narrow.value ? 1 : 2)
const canEdit = computed(() => ['admin','operator'].includes(session.role))
const isParent = computed(() => data.value && !data.value.parent && (data.value.device.deviceRole === 'GATEWAY' || data.value.profile?.childProducts?.length))
const standardAccess = computed(() => Boolean(data.value?.accessInfo && !data.value?.parent))
const httpAccess = computed(() => data.value?.accessInfo?.kind === 'managed' || data.value?.connector === 'HTTP')
const childTypes = computed(() => data.value?.profile?.childProducts || [])
const childDialog = ref(false), childSaving = ref(false)
const childForm = reactive({ type:'', address:'', name:'' })
const hasSessions = computed(() => Boolean(data.value?.parent || data.value?.profile?.mode === 'listener' || data.value?.sessions?.length))
const properties = computed(() => Object.entries(data.value?.latestProperties?.[0]?.properties || {}).map(([key,value]) => ({key,value})))
const displayValue = value => typeof value === 'object' && value !== null ? pretty(value) : String(value ?? '—')
const base = () => `/api/v1/device-registry/${encodeURIComponent(props.deviceId)}`
let generation = 0
let controller = new AbortController()
const revisions = {}

async function loadList(key) {
  const section = lists[key], current = generation, revision = (revisions[key] || 0) + 1
  revisions[key] = revision
  section.loading = true; section.error = ''; section.items = []; section.total = 0
  const paths = {history:`/history?page=${section.page}&pageSize=20`,events:`/history?kind=event&page=${section.page}&pageSize=20`,commands:`/commands?page=${section.page}&pageSize=20`,children:`/children?page=${section.page}&pageSize=20`}
  try {
    const result = await api(base()+paths[key],{signal:controller.signal})
    if (current !== generation || revisions[key] !== revision) return
    section.items = result.items || []; section.total = Number(result.total || 0)
    if (key === 'commands' && commandResult.value?.id) commandResult.value = section.items.find(item => item.id === commandResult.value.id) || commandResult.value
  } catch (cause) {
    if (current === generation && revisions[key] === revision && cause.name !== 'AbortError') section.error = cause.message
  } finally { if (current === generation && revisions[key] === revision) section.loading = false }
}
async function load() {
  const current = ++generation
  controller.abort(); controller = new AbortController()
  loading.value = true; error.value = ''
  try {
    const result = await api(`${base()}/connection${selectedProfile.value ? `?profileId=${encodeURIComponent(selectedProfile.value)}` : ''}`,{signal:controller.signal})
    if (current !== generation) return
    if (!result?.device?.id) throw new Error('设备连接信息不完整，请刷新后重试。')
    data.value = result; selectedProfile.value = result.profile?.id || ''
    const jobs = [loadList('history'),loadList('events')]
    if (isParent.value) jobs.push(loadList('children'))
    if (result.connector === 'MQTT') jobs.push(loadList('commands'))
    await Promise.all(jobs)
  } catch (cause) {
    if (current === generation && cause.name !== 'AbortError') { data.value = null; error.value = cause.message }
  } finally { if (current === generation) loading.value = false }
}
function selectProfile() { pendingProtocol.value = null; commandResult.value = null; commandReply.value = null; load() }
function newCommand() { pendingCommand.value = null; pendingProtocol.value = null; commandResult.value = null; commandReply.value = null }
async function showCommandReply() {
  const id = commandResult.value?.rawMessageId
  if (!id) return
  try {
    const reply = await api(`/api/v1/raw-messages/${encodeURIComponent(id)}`, {signal:controller.signal})
    if (commandResult.value?.rawMessageId === id) commandReply.value = reply
  } catch (cause) { if (cause.name !== 'AbortError') notifyError(cause) }
}
async function action(work) {
  if (actionBusy.value || loading.value || !canEdit.value) return
  actionBusy.value = true
  try { await work() } catch (cause) { if (cause !== 'cancel' && cause !== 'close') notifyError(cause) }
  finally { actionBusy.value = false }
}
async function rotate() {
  await action(async () => {
    await UiMessageBox.confirm('重新生成后旧凭据立即在平台停用；消息服务撤销结果可在下方查看。','重新生成凭据')
    const result = await api(`${base()}/credentials`,{method:'POST'})
    credential.value = result.credential; await load()
  })
}
async function disable() {
  await action(async () => {
    await UiMessageBox.confirm('禁用后设备不能使用此凭据上报或换取MQTT 令牌。','禁用设备凭据')
    await api(`${base()}/credentials`,{method:'DELETE'})
    credential.value = null; UiMessage.success('凭据已禁用'); await load()
  })
}
async function sendMQTT() {
  await action(async () => {
    const type = commandType.value.trim()
    if (!type) throw new Error('请填写命令类型')
    const body = commandBody(selectedOperation.value, commandValues.value)
    commandReply.value = null
    await UiMessageBox.confirm('确认向该设备发送此命令？发送成功不代表执行成功。','人工确认命令')
    const signature = JSON.stringify(body)
    if (!pendingCommand.value || pendingCommand.value.signature !== signature) pendingCommand.value = {signature,id:createClientId()}
    commandResult.value = await api(`${base()}/commands`,{method:'POST',body:JSON.stringify({...body,id:pendingCommand.value.id,confirmed:true})})
    await loadList('commands')
  })
}
async function send() {
  await action(async () => {
    const form = commandBody(selectedOperation.value, commandValues.value)
    if (Object.keys(form.data).some(key => ['type','confirmed','requestId','_scheduled'].includes(key))) throw new Error('命令参数包含保留字段，请检查产品命令定义')
    const body = {...form.data,type:form.type}
    commandReply.value = null
    if (typeof body.type !== 'string' || !body.type.trim()) throw new Error('请在协议命令中填写 type')
    await UiMessageBox.confirm('确认向该设备发送协议命令？请核对设备与参数。','人工确认命令')
    const profileId = data.value.profile.id, signature = JSON.stringify([profileId,props.deviceId,body])
    if (!pendingProtocol.value || pendingProtocol.value.signature !== signature) pendingProtocol.value = {signature,id:createClientId()}
    commandResult.value = await api(`/api/v2/device-access-profiles/${encodeURIComponent(profileId)}/devices/${encodeURIComponent(props.deviceId)}/commands`,{method:'POST',body:JSON.stringify({...body,requestId:pendingProtocol.value.id,confirmed:true})})
  })
}
function openChildDialog() { Object.assign(childForm, { type:childTypes.value[0]?.type || '', address:'', name:'' }); childDialog.value = true }
async function addChild() {
  if (childSaving.value) return
  if (!childForm.type || !childForm.address.trim()) return UiMessage.warning('请选择子设备类型并填写地址')
  childSaving.value = true
  try {
    const result = await api(`${base()}/children`,{method:'POST',body:JSON.stringify({type:childForm.type,address:childForm.address.trim(),name:childForm.name.trim()})})
    childDialog.value = false
    UiMessage.success(result.reused ? '该地址的子设备此前已登记' : '子设备已添加')
    await loadList('children')
  } catch (cause) { notifyError(cause) } finally { childSaving.value = false }
}
watch(() => props.deviceId,() => {
  data.value = null; selectedProfile.value = ''; credential.value = null; commandType.value='';commandValues.value={};newCommand()
  for (const section of Object.values(lists)) Object.assign(section,{items:[],total:0,page:1,error:'',loading:false})
  load()
},{immediate:true})
onBeforeUnmount(() => { generation++; controller.abort(); media.removeEventListener('change',resize); credential.value = null })
</script>

<template>
  <ui-drawer :model-value="true" class="device-connection-drawer" title="设备连接与数据" size="min(900px, 100vw)" @close="emit('close')">
    <div class="device-connection" v-loading="loading">
      <div class="connection-toolbar">
        <div><strong>{{data?.device?.name || '设备详情'}}</strong><small>{{props.deviceId}}</small></div>
        <ui-button :loading="loading" :disabled="actionBusy" @click="load">刷新</ui-button>
      </div>
      <ui-alert v-if="error" title="设备详情加载失败" :description="error" type="error" :closable="false" show-icon />
      <ui-empty v-if="!data && !loading && !error" description="暂无设备信息" />
      <template v-if="data">
        <section class="connection-section device-summary">
          <h3>当前接入状态</h3>
          <div v-if="data.diagnosis" class="connection-diagnosis" :class="`is-${data.diagnosis.tone}`" role="status"><ui-tag :type="diagnosisTagTypes[data.diagnosis.tone]">{{data.diagnosis.title}}</ui-tag><p>{{data.diagnosis.nextAction}}</p></div>
          <div class="connection-status-grid" role="status"><div><span>业务状态</span><strong>{{label(businessStatuses,data.connection?.businessStatus) || '未知'}}</strong></div><div><span>连接状态</span><strong>{{label(connectionStatuses,data.connection?.connectionStatus) || '未知'}}</strong></div><div><span>数据状态</span><strong>{{label(dataStatuses,data.connection?.dataStatus) || '未知'}}</strong></div><div><span>最近上报</span><strong>{{formatTime(data.connection?.lastSeenAt)}}</strong></div><div><span>原文接收</span><strong>{{data.ingest?.rawReceived ? '已收到' : '等待上报'}}</strong></div><div><span>解析状态</span><strong>{{data.ingest?.parsed ? '已完成' : data.ingest?.parseError ? '失败' : data.ingest?.rawReceived ? '等待处理' : '等待上报'}}</strong></div></div>
          <h4 class="connection-subtitle">设备与协议</h4>
          <ui-descriptions :column="columns" border>
            <ui-descriptions-item label="设备">{{data.device.name || props.deviceId}}</ui-descriptions-item>
            <ui-descriptions-item label="设备模板">{{data.product?.name || data.device.productId || '—'}}</ui-descriptions-item>
            <ui-descriptions-item label="接入方式">{{transportLabel(data.connector) || '未配置'}}</ui-descriptions-item>
            <ui-descriptions-item label="创建时间">{{formatTime(data.device.createdAt)}}</ui-descriptions-item>
            <ui-descriptions-item label="当前绑定协议">{{data.protocolId || '—'}}</ui-descriptions-item>
            <ui-descriptions-item label="绑定版本">{{data.protocolVersion || '—'}}</ui-descriptions-item>
            <ui-descriptions-item label="最后连接">{{formatTime(data.connection?.lastConnectAt)}}</ui-descriptions-item>
            <ui-descriptions-item label="最后断开">{{formatTime(data.connection?.lastDisconnectAt)}}</ui-descriptions-item>
            <ui-descriptions-item v-if="data.profile" label="接入点">{{data.profile.id}}</ui-descriptions-item>
            <ui-descriptions-item v-if="data.profile" label="接入点运行状态">{{data.profile.runtimeStatus ? statusLabel(data.profile.runtimeStatus) : '待确认'}}</ui-descriptions-item>
            <ui-descriptions-item v-if="data.profile?.collectorId" label="采集器">{{data.profile.collectorId}}</ui-descriptions-item>
            <ui-descriptions-item v-if="data.parent" label="所属主设备"><ui-button link type="primary" @click="emit('device',data.parent.id)">{{data.parent.name || data.parent.id}}</ui-button></ui-descriptions-item>
            <ui-descriptions-item v-if="data.parent" label="子设备地址">{{data.device.childAddress || '—'}}</ui-descriptions-item>
          </ui-descriptions>
          <ui-alert v-if="data.profile?.lastError || data.ingest?.parseError" class="section-feedback" title="最近接入异常" :description="data.profile?.lastError || data.ingest?.parseError" type="warning" :closable="false" show-icon />
          <div v-if="data.profiles?.length > 1" class="profile-picker">
            <p>设备关联了多个接入点，请根据用途、地址和状态选择。</p>
            <ui-select v-model="selectedProfile" :disabled="loading || actionBusy" placeholder="选择接入点" @change="selectProfile"><ui-option v-for="p in data.profiles" :key="p.id" :value="p.id" :label="`${p.id} · ${p.host}:${p.port} · ${p.runtimeStatus||'待确认'}`" /></ui-select>
          </div>
        </section>

        <section v-if="standardAccess" class="connection-section device-access-info">
          <h3>设备接入信息</h3>
          <ui-descriptions :column="1" border>
            <ui-descriptions-item v-if="httpAccess" label="上报接口"><code>{{data.accessInfo.httpUrl || '未配置平台对外 HTTP 地址'}}</code></ui-descriptions-item>
            <ui-descriptions-item v-if="data.connector==='MQTT'" label="消息服务地址"><code>{{data.accessInfo.mqttBroker || '未配置对外地址'}}</code></ui-descriptions-item>
            <ui-descriptions-item v-if="data.connector==='MQTT'" label="客户端标识"><code>{{data.accessInfo.clientId}}</code></ui-descriptions-item>
            <ui-descriptions-item label="接入密钥"><code>{{data.accessInfo.username}}</code></ui-descriptions-item>
            <ui-descriptions-item v-if="data.connector==='MQTT'" label="上行 Topic"><code>{{data.accessInfo.upTopic}}</code></ui-descriptions-item>
            <ui-descriptions-item v-if="data.connector==='MQTT'" label="下行 Topic"><code>{{data.accessInfo.downTopic}}</code></ui-descriptions-item>
            <ui-descriptions-item label="凭据状态">{{data.credentialEnabled ? '已启用' : '已禁用'}}</ui-descriptions-item>
          </ui-descriptions>
          <p v-if="data.connector==='MQTT'">设备先用 AccessKey 和 Secret 调用 {{data.accessInfo.tokenEndpoint}} 换取短期 MQTT token，再以返回的 username 和 token 连接 Broker；Secret 不能直接作为 MQTT 密码。</p>
        </section>

        <section v-if="isParent" class="connection-section device-children" v-loading="lists.children.loading">
          <div class="section-heading"><h3>子设备（{{lists.children.total}}）</h3><ui-button v-if="childTypes.length && canEdit" v-permission="'POST /api/v1/device-registry/:id/children'" size="small" @click="openChildDialog">添加子设备</ui-button></div>
          <p v-if="!childTypes.length">接入点尚未配置子设备类型。请在设备模板的“接入点”中添加子设备映射后，再按地址添加子设备。</p>
          <ui-alert v-if="lists.children.error" title="子设备加载失败" :description="lists.children.error" type="error" :closable="false" />
          <ui-table v-else :data="lists.children.items" border empty-text="暂无子设备，等待主设备上报登记信息">
            <ui-table-column prop="device.name" label="名称" min-width="140" /><ui-table-column prop="device.childAddress" label="地址" min-width="90" />
            <ui-table-column prop="productName" label="设备模板" min-width="130" />
            <ui-table-column label="协议" min-width="150"><template #default="{row}">{{row.binding?.protocolId || '未配置'}} · {{row.binding?.version || '—'}}</template></ui-table-column>
            <ui-table-column label="最近上报" min-width="170"><template #default="{row}">{{formatTime(row.runtimeState?.lastSeenAt)}}</template></ui-table-column>
            <ui-table-column label="状态" min-width="95"><template #default="{row}">{{label(businessStatuses,row.runtimeState?.businessStatus) || '未知'}}</template></ui-table-column>
            <ui-table-column label="操作" width="110" fixed="right"><template #default="{row}"><ui-button link type="primary" @click="emit('device',row.device.id)">查看子设备</ui-button></template></ui-table-column>
          </ui-table>
          <ui-pagination v-if="lists.children.total>20" v-model:current-page="lists.children.page" :page-size="20" :total="lists.children.total" layout="prev, pager, next" @current-change="loadList('children')" />
        </section>

        <section v-if="hasSessions" class="connection-section device-sessions">
          <h3>{{data.parent ? '主设备通信会话' : '在线会话'}}</h3>
          <ui-table :data="data.sessions || []" border empty-text="暂无已识别的在线会话">
            <ui-table-column prop="profileId" label="接入点" min-width="145" /><ui-table-column prop="remoteAddress" label="远端地址" min-width="155" />
            <ui-table-column prop="protocolId" label="会话协议" min-width="130" /><ui-table-column prop="protocolVersion" label="会话版本" min-width="100" />
            <ui-table-column label="最后有效报文" min-width="175"><template #default="{row}">{{formatTime(row.lastSeenAt)}}</template></ui-table-column>
          </ui-table>
        </section>

        <section class="connection-section device-properties">
          <h3>最新属性 <small>{{formatTime(data.latestProperties?.[0]?.timestamp)}}</small></h3>
          <ui-table :data="properties" border empty-text="暂无已解析的属性报文">
            <ui-table-column prop="key" label="属性" :width="narrow ? 105 : 200" /><ui-table-column label="值" min-width="120"><template #default="{row}"><span class="field-value">{{displayValue(row.value)}}</span></template></ui-table-column>
          </ui-table>
        </section>

        <section class="connection-section device-latest">
          <h3>最新报文</h3>
          <ui-descriptions v-if="data.latest?.messageId" :column="columns" border>
            <ui-descriptions-item label="消息类型">{{messageTypeLabel(data.latest.messageType)}}</ui-descriptions-item><ui-descriptions-item label="时间">{{formatTime(data.latest.timestamp)}}</ui-descriptions-item>
            <ui-descriptions-item label="标准消息标识" :span="columns"><code>{{data.latest.messageId}}</code></ui-descriptions-item>
          </ui-descriptions>
          <ui-empty v-else description="暂无已解析报文" :image-size="48" />
          <ui-collapse v-if="data.latest?.messageId" class="message-detail"><ui-collapse-item title="查看完整标准消息" name="message"><pre>{{pretty(data.latest)}}</pre></ui-collapse-item></ui-collapse>
          <div class="section-actions"><ui-button v-permission="'menu:raw'" @click="emit('navigate','raw',{deviceId:props.deviceId})">原始报文与回放</ui-button><ui-button v-permission="'menu:alarms'" @click="emit('navigate','alarms',{deviceId:props.deviceId})">设备告警</ui-button></div>
        </section>

        <section class="connection-section device-history" v-loading="lists.history.loading">
          <h3>连接与状态历史</h3>
          <ui-alert v-if="lists.history.error" title="状态历史加载失败" :description="lists.history.error" type="error" :closable="false" />
          <ui-table v-else :data="lists.history.items" border empty-text="暂无状态变更">
            <ui-table-column label="记录时间" min-width="175"><template #default="{row}">{{formatTime(row.recordedAt)}}</template></ui-table-column>
            <ui-table-column label="连接" min-width="100"><template #default="{row}">{{label(connectionStatuses,row.state?.connectionStatus)}}</template></ui-table-column>
            <ui-table-column label="业务" min-width="100"><template #default="{row}">{{label(businessStatuses,row.state?.businessStatus)}}</template></ui-table-column>
            <ui-table-column label="来源" min-width="100"><template #default="{row}">{{label(stateSources,row.state?.statusSource)}}</template></ui-table-column>
          </ui-table>
          <ui-pagination v-if="lists.history.total>20" v-model:current-page="lists.history.page" :page-size="20" :total="lists.history.total" layout="prev, pager, next" @current-change="loadList('history')" />
        </section>

        <section class="connection-section device-events" v-loading="lists.events.loading">
          <h3>最近事件</h3>
          <ui-alert v-if="lists.events.error" title="事件加载失败" :description="lists.events.error" type="error" :closable="false" />
          <ui-table v-else :data="lists.events.items" border empty-text="暂无事件">
            <ui-table-column label="时间" min-width="175"><template #default="{row}">{{formatTime(row.timestamp)}}</template></ui-table-column>
            <ui-table-column label="事件内容" min-width="200"><template #default="{row}"><span class="field-value">{{pretty(row.event || {})}}</span></template></ui-table-column>
          </ui-table>
          <ui-pagination v-if="lists.events.total>20" v-model:current-page="lists.events.page" :page-size="20" :total="lists.events.total" layout="prev, pager, next" @current-change="loadList('events')" />
        </section>

        <section class="connection-section device-alarms">
          <h3>最近告警</h3>
          <ui-table :data="data.recentAlarms || []" border empty-text="暂无告警">
            <ui-table-column label="告警" min-width="100"><template #default="{row}">{{alarmType(row.alarmType)}}</template></ui-table-column><ui-table-column label="级别" min-width="90"><template #default="{row}">{{alarmLevel(row.alarmLevel)}}</template></ui-table-column>
            <ui-table-column label="状态" min-width="100"><template #default="{row}">{{label(alarmStatuses,row.status)}}</template></ui-table-column><ui-table-column label="最近触发" min-width="175"><template #default="{row}">{{formatTime(row.lastTriggeredAt)}}</template></ui-table-column>
          </ui-table>
        </section>

        <section v-if="operations.length && (data.connector==='MQTT' || (data.profile && data.canCommand))" class="connection-section device-commands">
          <h3>设备控制</h3>
          <p>已发送不代表设备执行成功。请核对发送状态和设备应答；结果未知时不要重复发送。</p>
          <template v-if="canEdit">
            <ui-form label-position="top" :disabled="actionBusy || loading">
              <ui-form-item label="设备命令"><ui-select v-model="commandType" aria-label="设备命令" placeholder="选择设备支持的命令"><ui-option v-for="c in operations" :key="c.identifier" :value="c.identifier" :label="c.name || c.identifier" /></ui-select></ui-form-item>
              <ui-form-item v-for="field in selectedOperation?.fields || []" :key="`${commandType}:${field.identifier}`" :label="`${field.name || field.identifier}${field.unit ? `（${field.unit}）` : ''}`" :required="field.required">
                <CommandValueInput v-model="commandValues[field.identifier]" :kind="field.dataType" :label="field.name || field.identifier" />
              </ui-form-item>
              <p v-if="selectedOperation && !selectedOperation.fields?.length">此命令无需参数。</p>
            </ui-form>
            <ui-button v-permission="'POST /api/v1/device-registry/:id/commands'" v-if="data.connector==='MQTT'" :disabled="loading || !data.mqttCommandAvailable || !data.credentialEnabled || !selectedOperation" :loading="actionBusy" @click="sendMQTT">执行命令</ui-button>
            <ui-button v-permission="'POST /api/v2/device-access-profiles/:id/devices/:deviceId/commands'" v-else :loading="actionBusy" :disabled="loading || !data.profile.enabled || !data.sessions?.length || !selectedOperation" @click="send">执行命令</ui-button>
            <p v-if="data.connector!=='MQTT' && (!data.profile.enabled || !data.sessions?.length)">当前没有在线会话或接入点已停用，暂时不能下发命令。</p>
            <ui-button v-if="pendingCommand || pendingProtocol" class="section-feedback" :disabled="actionBusy" @click="newCommand">开始一条新命令</ui-button>
          </template>
          <ui-alert v-if="commandResult?.lastError" :title="label(commandStatuses,String(commandResult.status || '').toUpperCase())" :description="commandResult.lastError" type="warning" :closable="false" />
          <ui-descriptions v-if="commandResult" :column="1" border class="section-feedback"><ui-descriptions-item label="发送状态">{{label(commandStatuses,String(commandResult.status || '').toUpperCase())}}</ui-descriptions-item><ui-descriptions-item label="设备应答">{{commandResult.reply || commandResult.response ? pretty(commandResult.reply || commandResult.response) : commandResult.rawMessageId ? `已收到应答，原始报文：${commandResult.rawMessageId}` : '尚无应答内容'}}</ui-descriptions-item></ui-descriptions>
          <ui-button v-if="commandResult?.rawMessageId" class="section-feedback" @click="showCommandReply">查看应答报文</ui-button>
          <pre v-if="commandReply">{{pretty(commandReply)}}</pre>
          <template v-if="data.connector==='MQTT'">
            <ui-alert v-if="lists.commands.error" title="命令记录加载失败" :description="lists.commands.error" type="error" :closable="false" />
            <ui-table v-else :data="lists.commands.items" border empty-text="暂无命令记录"><ui-table-column prop="type" label="命令" min-width="120" /><ui-table-column label="状态" min-width="170"><template #default="{row}">{{label(commandStatuses,row.status)}}</template></ui-table-column><ui-table-column label="回执" min-width="180"><template #default="{row}"><span class="field-value">{{pretty(row.reply || {})}}</span></template></ui-table-column></ui-table>
            <ui-pagination v-if="lists.commands.total>20" v-model:current-page="lists.commands.page" :page-size="20" :total="lists.commands.total" layout="prev, pager, next" @current-change="loadList('commands')" />
          </template>
        </section>

        <section v-if="standardAccess && canEdit" class="connection-section device-credentials">
          <h3>设备凭据</h3><p>重新生成后旧凭据立即停用，新密钥仅显示一次。</p>
          <div class="section-actions"><ui-button v-permission="'DELETE /api/v1/device-registry/:id/credentials'" :disabled="loading || actionBusy || !data.credentialEnabled" @click="disable">禁用凭据</ui-button><ui-button v-permission="'POST /api/v1/device-registry/:id/credentials'" :disabled="loading || actionBusy" @click="rotate">重新生成凭据</ui-button></div>
          <pre v-if="credential">仅本次显示，请妥善保存：{{pretty(credential)}}</pre>
          <p v-for="revocation in data.revocations" :key="revocation.id">旧凭据消息服务撤销：{{revocation.status==='REVOKED' ? '已完成' : '待完成（平台已停用旧凭据）'}}</p>
        </section>
      </template>
    </div>
    <ui-dialog v-model="childDialog" title="添加子设备" width="min(480px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!childSaving" :show-close="!childSaving">
      <ui-form label-position="top" :disabled="childSaving" @submit.prevent="addChild">
        <ui-form-item label="子设备类型" required><ui-select v-model="childForm.type" aria-label="子设备类型"><ui-option v-for="item in childTypes" :key="item.type" :value="item.type" :label="`${item.type} · 模板 ${item.productId}`" /></ui-select></ui-form-item>
        <ui-form-item label="子设备地址" required><ui-input v-model="childForm.address" placeholder="主设备协议中的子设备地址" aria-label="子设备地址" /></ui-form-item>
        <ui-form-item label="名称"><ui-input v-model="childForm.name" maxlength="256" placeholder="留空时按类型和地址生成" aria-label="子设备名称" /></ui-form-item>
      </ui-form>
      <p class="child-dialog-hint">子设备沿用主设备的连接，由主设备协议按地址识别。</p>
      <template #footer><ui-button :disabled="childSaving" @click="childDialog=false">取消</ui-button><ui-button type="primary" :loading="childSaving" @click="addChild">添加</ui-button></template>
    </ui-dialog>
  </ui-drawer>
</template>

<style scoped>
:global(.device-connection-drawer .n-drawer-body-content-wrapper) { background:var(--bg); }
.device-connection { min-width:0; color:var(--text); font-size:var(--font-size-sm); line-height:var(--line-height-normal); }
.connection-toolbar { display:flex; align-items:center; justify-content:space-between; gap:var(--space-4); margin-bottom:var(--space-4); }
.connection-toolbar > div { min-width:0; }
.connection-toolbar strong { display:block; color:var(--text-strong); font-size:var(--font-size-lg); font-weight:var(--font-weight-semibold); }
.connection-toolbar small { display:block; color:var(--text-muted); font-family:var(--font-mono); font-size:var(--font-size-xs); overflow-wrap:anywhere; }
.connection-section { min-width:0; margin:0 0 var(--space-4); padding:var(--space-4) var(--space-5); background:var(--surface); border:1px solid var(--border); border-radius:var(--radius-lg); }
.connection-status-grid { display:grid; grid-template-columns:repeat(3,minmax(0,1fr)); gap:var(--space-2); margin-bottom:var(--space-5); }
.connection-status-grid > div { min-width:0; padding:10px var(--space-3); background:var(--surface-muted); border:1px solid var(--border); border-radius:var(--radius-md); }
.connection-status-grid span { display:block; margin-bottom:2px; color:var(--text-muted); font-size:var(--font-size-xs); }
.connection-status-grid strong { display:block; color:var(--text-strong); font-size:var(--font-size-md); font-weight:var(--font-weight-semibold); line-height:var(--line-height-tight); overflow-wrap:anywhere; }
.connection-diagnosis { display:grid; gap:var(--space-1); margin-bottom:var(--space-4); padding:var(--space-3) var(--space-4); background:var(--info-soft); border:1px solid var(--info-border); border-radius:var(--radius-md); }
.connection-diagnosis.is-success { background:var(--success-soft); border-color:var(--success-border); }
.connection-diagnosis.is-warning { background:var(--warning-soft); border-color:var(--warning-border); }
.connection-diagnosis.is-error { background:var(--danger-soft); border-color:var(--danger-border); }
.connection-diagnosis .ui-tag { justify-self:start; }
.connection-diagnosis p { margin:0; color:var(--text); }
.section-heading { display:flex; align-items:center; justify-content:space-between; gap:var(--space-3); margin-bottom:var(--space-3); }
.section-heading h3 { margin:0; }
.child-dialog-hint { margin:0; color:var(--text-muted); font-size:var(--font-size-xs); }
.connection-subtitle { margin:0 0 10px; color:var(--text-strong); font-size:var(--font-size-sm); font-weight:var(--font-weight-semibold); }
h3 { display:flex; flex-wrap:wrap; align-items:baseline; gap:var(--space-2); margin:0 0 var(--space-3); color:var(--text-strong); font-size:var(--font-size-md); font-weight:var(--font-weight-semibold); line-height:1.5; }
h3 small { color:var(--text-muted); font-size:var(--font-size-xs); font-weight:400; }
p { margin:10px 0; color:var(--text-secondary); overflow-wrap:anywhere; }
code, .field-value { overflow-wrap:anywhere; word-break:break-word; white-space:pre-wrap; }
.field-value { color:inherit; font:inherit; }
pre { max-height:320px; margin:var(--space-3) 0 0; }
.section-actions { display:flex; flex-wrap:wrap; gap:var(--space-2); margin-top:var(--space-3); }
.section-feedback, .profile-picker, .message-detail { margin-top:var(--space-3); }
:deep(.n-descriptions-table) { table-layout:fixed; }
:deep(.n-descriptions-table-header) { width:120px; }
:deep(.ui-pagination) { margin-top:var(--space-3); justify-content:flex-end; }
@media (max-width:767px) {
  .connection-section { padding:var(--space-3); margin-bottom:var(--space-3); }
  .connection-status-grid { grid-template-columns:repeat(2,minmax(0,1fr)); gap:6px; margin-bottom:var(--space-4); }
  :deep(.n-descriptions-table-header) { width:96px; }
}
</style>
