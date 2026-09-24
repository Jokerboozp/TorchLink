<script setup>
import { createClientId } from '../clientId' /* 引入当前代码需要的依赖。 */
import CommandValueInput from './CommandValueInput.vue' /* 引入当前代码需要的依赖。 */
import { commandBody } from '../commandForm' /* 引入当前代码需要的依赖。 */
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue' /* 引入当前代码需要的依赖。 */
import { UiMessageBox, UiMessage } from '../ui/feedback.js' /* 引入当前代码需要的依赖。 */
import { api, formatTime, notifyError, pretty, session } from '../api' /* 引入当前代码需要的依赖。 */
import { transportLabel, statusLabel } from '../presentation' /* 引入当前代码需要的依赖。 */
import { commandStatuses, alarmType, alarmLevel, alarmStatuses, connectionStatuses, dataStatuses, businessStatuses, stateSources, messageTypeLabel, label } from '../labels' /* 引入当前代码需要的依赖。 */

const props = defineProps({ deviceId:String }) /* 声明 props。 */
const emit = defineEmits(['close','navigate','device']) /* 声明 emit。 */
const data = ref(null), loading = ref(false), actionBusy = ref(false), error = ref(''), selectedProfile = ref('') /* 声明 data。 */
const credential = ref(null), commandResult = ref(null) /* 声明 credential。 */
const commandReply = ref(null) /* 声明 commandReply。 */
const commandType = ref('') /* 声明 commandType。 */
const commandValues = ref({}) /* 声明 commandValues。 */
const operations = computed(() => data.value?.product?.thingModel?.commands || []) /* 声明 operations。 */
const selectedOperation = computed(() => operations.value.find(item => item.identifier === commandType.value)) /* 声明 selectedOperation。 */
watch(commandType, () => { commandValues.value = {} }) /* 执行当前语句并推进处理流程。 */
const pendingCommand = ref(null), pendingProtocol = ref(null) /* 声明 pendingCommand。 */
const lists = reactive(Object.fromEntries(['history','events','commands','children'].map(key => [key,{items:[],total:0,page:1,loading:false,error:''}]))) /* 声明 lists。 */
const media = window.matchMedia('(max-width: 640px)') /* 声明 media。 */
const narrow = ref(media.matches) /* 声明 narrow。 */
const resize = event => { narrow.value = event.matches } /* 声明 resize。 */
media.addEventListener('change',resize) /* 执行当前语句并推进处理流程。 */
const columns = computed(() => narrow.value ? 1 : 2) /* 声明 columns。 */
const canEdit = computed(() => ['admin','operator'].includes(session.role)) /* 声明 canEdit。 */
const isParent = computed(() => data.value && !data.value.parent && (data.value.device.deviceRole === 'GATEWAY' || data.value.profile?.childProducts?.length)) /* 声明 isParent。 */
const standardAccess = computed(() => Boolean(data.value?.accessInfo && !data.value?.parent)) /* 声明 standardAccess。 */
const hasSessions = computed(() => Boolean(data.value?.parent || data.value?.profile?.mode === 'listener' || data.value?.sessions?.length)) /* 声明 hasSessions。 */
const properties = computed(() => Object.entries(data.value?.latestProperties?.[0]?.properties || {}).map(([key,value]) => ({key,value}))) /* 声明 properties。 */
const displayValue = value => typeof value === 'object' && value !== null ? pretty(value) : String(value ?? '—') /* 声明 displayValue。 */
const base = () => `/api/v1/device-registry/${encodeURIComponent(props.deviceId)}` /* 声明 base。 */
let generation = 0 /* 声明 generation。 */
let controller = new AbortController() /* 声明 controller。 */
const revisions = {} /* 声明 revisions。 */

async function loadList(key) { /* 定义 loadList 函数。 */
  const section = lists[key], current = generation, revision = (revisions[key] || 0) + 1 /* 声明 section。 */
  revisions[key] = revision /* 更新 revisions[key] 的值。 */
  section.loading = true; section.error = ''; section.items = []; section.total = 0 /* 更新 section.loading 的值。 */
  const paths = {history:`/history?page=${section.page}&pageSize=20`,events:`/history?kind=event&page=${section.page}&pageSize=20`,commands:`/commands?page=${section.page}&pageSize=20`,children:`/children?page=${section.page}&pageSize=20`} /* 声明 paths。 */
  try { /* 执行当前语句并推进处理流程。 */
    const result = await api(base()+paths[key],{signal:controller.signal}) /* 声明 result。 */
    if (current !== generation || revisions[key] !== revision) return /* 判断条件并选择处理分支。 */
    section.items = result.items || []; section.total = Number(result.total || 0) /* 更新 section.items 的值。 */
    if (key === 'commands' && commandResult.value?.id) commandResult.value = section.items.find(item => item.id === commandResult.value.id) || commandResult.value /* 判断条件并选择处理分支。 */
  } catch (cause) { /* 结束当前表达式或代码块。 */
    if (current === generation && revisions[key] === revision && cause.name !== 'AbortError') section.error = cause.message /* 判断条件并选择处理分支。 */
  } finally { if (current === generation && revisions[key] === revision) section.loading = false } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
async function load() { /* 定义 load 函数。 */
  const current = ++generation /* 声明 current。 */
  controller.abort(); controller = new AbortController() /* 执行当前语句并推进处理流程。 */
  loading.value = true; error.value = '' /* 更新 loading.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const result = await api(`${base()}/connection${selectedProfile.value ? `?profileId=${encodeURIComponent(selectedProfile.value)}` : ''}`,{signal:controller.signal}) /* 声明 result。 */
    if (current !== generation) return /* 判断条件并选择处理分支。 */
    if (!result?.device?.id) throw new Error('设备连接信息不完整，请刷新后重试。')
    data.value = result; selectedProfile.value = result.profile?.id || '' /* 更新 data.value 的值。 */
    const jobs = [loadList('history'),loadList('events')] /* 声明 jobs。 */
    if (isParent.value) jobs.push(loadList('children')) /* 判断条件并选择处理分支。 */
    if (result.connector === 'MQTT') jobs.push(loadList('commands')) /* 判断条件并选择处理分支。 */
    await Promise.all(jobs) /* 等待异步操作完成。 */
  } catch (cause) { /* 结束当前表达式或代码块。 */
    if (current === generation && cause.name !== 'AbortError') { data.value = null; error.value = cause.message } /* 判断条件并选择处理分支。 */
  } finally { if (current === generation) loading.value = false } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
function selectProfile() { pendingProtocol.value = null; commandResult.value = null; commandReply.value = null; load() } /* 定义 selectProfile 函数。 */
function newCommand() { pendingCommand.value = null; pendingProtocol.value = null; commandResult.value = null; commandReply.value = null } /* 定义 newCommand 函数。 */
async function showCommandReply() { /* 定义 showCommandReply 函数。 */
  const id = commandResult.value?.rawMessageId /* 声明 id。 */
  if (!id) return /* 判断条件并选择处理分支。 */
  try { /* 执行当前语句并推进处理流程。 */
    const reply = await api(`/api/v1/raw-messages/${encodeURIComponent(id)}`, {signal:controller.signal}) /* 声明 reply。 */
    if (commandResult.value?.rawMessageId === id) commandReply.value = reply /* 判断条件并选择处理分支。 */
  } catch (cause) { if (cause.name !== 'AbortError') notifyError(cause) } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
async function action(work) { /* 定义 action 函数。 */
  if (actionBusy.value || loading.value || !canEdit.value) return /* 判断条件并选择处理分支。 */
  actionBusy.value = true /* 更新 actionBusy.value 的值。 */
  try { await work() } catch (cause) { if (cause !== 'cancel' && cause !== 'close') notifyError(cause) } /* 执行当前语句并推进处理流程。 */
  finally { actionBusy.value = false } /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
async function rotate() { /* 定义 rotate 函数。 */
  await action(async () => { /* 等待异步操作完成。 */
    await UiMessageBox.confirm('重新生成后旧凭据立即在平台停用；消息服务撤销结果可在下方查看。','重新生成凭据') /* 等待异步操作完成。 */
    const result = await api(`${base()}/credentials`,{method:'POST'}) /* 声明 result。 */
    credential.value = result.credential; await load() /* 更新 credential.value 的值。 */
  }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
async function disable() { /* 定义 disable 函数。 */
  await action(async () => { /* 等待异步操作完成。 */
    await UiMessageBox.confirm('禁用后设备不能使用此凭据上报或换取MQTT 令牌。','禁用设备凭据') /* 等待异步操作完成。 */
    await api(`${base()}/credentials`,{method:'DELETE'}) /* 等待异步操作完成。 */
    credential.value = null; UiMessage.success('凭据已禁用'); await load() /* 更新 credential.value 的值。 */
  }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
async function sendMQTT() { /* 定义 sendMQTT 函数。 */
  await action(async () => { /* 等待异步操作完成。 */
    const type = commandType.value.trim() /* 声明 type。 */
    if (!type) throw new Error('请填写命令类型') /* 判断条件并选择处理分支。 */
    const body = commandBody(selectedOperation.value, commandValues.value) /* 声明 body。 */
    commandReply.value = null /* 更新 commandReply.value 的值。 */
    await UiMessageBox.confirm('确认向该设备发送此命令？发送成功不代表执行成功。','人工确认命令') /* 等待异步操作完成。 */
    const signature = JSON.stringify(body) /* 声明 signature。 */
    if (!pendingCommand.value || pendingCommand.value.signature !== signature) pendingCommand.value = {signature,id:createClientId()} /* 判断条件并选择处理分支。 */
    commandResult.value = await api(`${base()}/commands`,{method:'POST',body:JSON.stringify({...body,id:pendingCommand.value.id,confirmed:true})}) /* 更新 commandResult.value 的值。 */
    await loadList('commands') /* 等待异步操作完成。 */
  }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
async function send() { /* 定义 send 函数。 */
  await action(async () => { /* 等待异步操作完成。 */
    const form = commandBody(selectedOperation.value, commandValues.value) /* 声明 form。 */
    if (Object.keys(form.data).some(key => ['type','confirmed','requestId','_scheduled'].includes(key))) throw new Error('命令参数包含保留字段，请检查产品命令定义') /* 判断条件并选择处理分支。 */
    const body = {...form.data,type:form.type} /* 声明 body。 */
    commandReply.value = null /* 更新 commandReply.value 的值。 */
    if (typeof body.type !== 'string' || !body.type.trim()) throw new Error('请在协议命令中填写 type') /* 判断条件并选择处理分支。 */
    await UiMessageBox.confirm('确认向该设备发送协议命令？请核对设备与参数。','人工确认命令') /* 等待异步操作完成。 */
    const profileId = data.value.profile.id, signature = JSON.stringify([profileId,props.deviceId,body]) /* 声明 profileId。 */
    if (!pendingProtocol.value || pendingProtocol.value.signature !== signature) pendingProtocol.value = {signature,id:createClientId()} /* 判断条件并选择处理分支。 */
    commandResult.value = await api(`/api/v2/device-access-profiles/${encodeURIComponent(profileId)}/devices/${encodeURIComponent(props.deviceId)}/commands`,{method:'POST',body:JSON.stringify({...body,requestId:pendingProtocol.value.id,confirmed:true})}) /* 更新 commandResult.value 的值。 */
  }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
watch(() => props.deviceId,() => { /* 执行当前语句并推进处理流程。 */
  data.value = null; selectedProfile.value = ''; credential.value = null; commandType.value='';commandValues.value={};newCommand() /* 更新 data.value 的值。 */
  for (const section of Object.values(lists)) Object.assign(section,{items:[],total:0,page:1,error:'',loading:false}) /* 循环处理当前数据。 */
  load() /* 执行当前语句并推进处理流程。 */
},{immediate:true}) /* 结束当前表达式或代码块。 */
onBeforeUnmount(() => { generation++; controller.abort(); media.removeEventListener('change',resize); credential.value = null }) /* 执行当前语句并推进处理流程。 */
</script>

<template>
  <ui-drawer :model-value="true" class="device-connection-drawer" title="设备连接与数据" size="min(900px, 100vw)" @close="emit('close')"> <!-- 渲染 ui-drawer 界面元素。 -->
    <div class="device-connection" v-loading="loading"> <!-- 渲染 div 界面元素。 -->
      <div class="connection-toolbar"> <!-- 渲染 div 界面元素。 -->
        <div><strong>{{data?.device?.name || '设备详情'}}</strong><small>{{props.deviceId}}</small></div> <!-- 渲染 div 界面元素。 -->
        <ui-button :loading="loading" :disabled="actionBusy" @click="load">刷新</ui-button> <!-- 渲染 ui-button 界面元素。 -->
      </div> <!-- 结束当前界面区域。 -->
      <ui-alert v-if="error" title="设备详情加载失败" :description="error" type="error" :closable="false" show-icon /> <!-- 渲染 ui-alert 界面元素。 -->
      <ui-empty v-if="!data && !loading && !error" description="暂无设备信息" /> <!-- 渲染 ui-empty 界面元素。 -->
      <template v-if="data">
        <section class="connection-section device-summary"> <!-- 渲染 section 界面元素。 -->
          <h3>当前接入状态</h3>
          <div class="connection-status-grid" role="status"><div><span>业务状态</span><strong>{{label(businessStatuses,data.connection?.businessStatus) || '未知'}}</strong></div><div><span>连接状态</span><strong>{{label(connectionStatuses,data.connection?.connectionStatus) || '未知'}}</strong></div><div><span>数据状态</span><strong>{{label(dataStatuses,data.connection?.dataStatus) || '未知'}}</strong></div><div><span>最近上报</span><strong>{{formatTime(data.connection?.lastSeenAt)}}</strong></div><div><span>原文接收</span><strong>{{data.ingest?.rawReceived ? '已收到' : '等待上报'}}</strong></div><div><span>解析状态</span><strong>{{data.ingest?.parsed ? '已完成' : data.ingest?.parseError ? '失败' : data.ingest?.rawReceived ? '等待处理' : '等待上报'}}</strong></div></div>
          <h4 class="connection-subtitle">设备与协议</h4>
          <ui-descriptions :column="columns" border> <!-- 渲染 ui-descriptions 界面元素。 -->
            <ui-descriptions-item label="设备">{{data.device.name || props.deviceId}}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            <ui-descriptions-item label="设备模板">{{data.product?.name || data.device.productId || '—'}}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            <ui-descriptions-item label="接入方式">{{transportLabel(data.connector) || '未配置'}}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            <ui-descriptions-item label="创建时间">{{formatTime(data.device.createdAt)}}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            <ui-descriptions-item label="当前绑定协议">{{data.protocolId || '—'}}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            <ui-descriptions-item label="绑定版本">{{data.protocolVersion || '—'}}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            <ui-descriptions-item label="最后连接">{{formatTime(data.connection?.lastConnectAt)}}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            <ui-descriptions-item label="最后断开">{{formatTime(data.connection?.lastDisconnectAt)}}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            <ui-descriptions-item v-if="data.profile" label="平台连接配置">{{data.profile.id}}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            <ui-descriptions-item v-if="data.profile" label="连接运行状态">{{data.profile.runtimeStatus ? statusLabel(data.profile.runtimeStatus) : '待确认'}}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            <ui-descriptions-item v-if="data.profile?.collectorId" label="采集器">{{data.profile.collectorId}}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            <ui-descriptions-item v-if="data.parent" label="所属主设备"><ui-button link type="primary" @click="emit('device',data.parent.id)">{{data.parent.name || data.parent.id}}</ui-button></ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            <ui-descriptions-item v-if="data.parent" label="子设备地址">{{data.device.tags?.childAddress || '—'}}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
          </ui-descriptions> <!-- 结束当前界面区域。 -->
          <ui-alert v-if="data.profile?.lastError || data.ingest?.parseError" class="section-feedback" title="最近接入异常" :description="data.profile?.lastError || data.ingest?.parseError" type="warning" :closable="false" show-icon /> <!-- 渲染 ui-alert 界面元素。 -->
          <div v-if="data.profiles?.length > 1" class="profile-picker"> <!-- 渲染 div 界面元素。 -->
            <p>检测到多个关联的平台连接配置，请根据用途、地址和状态选择。</p> <!-- 渲染 p 界面元素。 -->
            <ui-select v-model="selectedProfile" :disabled="loading || actionBusy" placeholder="选择平台连接配置" @change="selectProfile"><ui-option v-for="p in data.profiles" :key="p.id" :value="p.id" :label="`${p.id} · ${p.host}:${p.port} · ${p.runtimeStatus||'待确认'}`" /></ui-select> <!-- 渲染 ui-select 界面元素。 -->
          </div> <!-- 结束当前界面区域。 -->
        </section> <!-- 结束当前界面区域。 -->

        <section v-if="standardAccess" class="connection-section device-access-info"> <!-- 渲染 section 界面元素。 -->
          <h3>设备接入信息</h3> <!-- 渲染 h3 界面元素。 -->
          <ui-descriptions :column="1" border> <!-- 渲染 ui-descriptions 界面元素。 -->
            <ui-descriptions-item v-if="data.connector==='HTTP'" label="上报接口"><code>{{data.accessInfo.httpUrl || '未配置平台对外 HTTP 地址'}}</code></ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            <ui-descriptions-item v-if="data.connector==='MQTT'" label="消息服务地址"><code>{{data.accessInfo.mqttBroker || '未配置对外地址'}}</code></ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            <ui-descriptions-item v-if="data.connector==='MQTT'" label="客户端标识"><code>{{data.accessInfo.clientId}}</code></ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            <ui-descriptions-item label="接入密钥"><code>{{data.accessInfo.username}}</code></ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            <ui-descriptions-item v-if="data.connector==='MQTT'" label="上行 Topic"><code>{{data.accessInfo.upTopic}}</code></ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            <ui-descriptions-item v-if="data.connector==='MQTT'" label="下行 Topic"><code>{{data.accessInfo.downTopic}}</code></ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            <ui-descriptions-item label="凭据状态">{{data.credentialEnabled ? '已启用' : '已禁用'}}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
          </ui-descriptions> <!-- 结束当前界面区域。 -->
          <p v-if="data.connector==='MQTT'">设备先用 AccessKey 和 Secret 调用 {{data.accessInfo.tokenEndpoint}} 换取短期 MQTT token，再以返回的 username 和 token 连接 Broker；Secret 不能直接作为 MQTT 密码。</p>
        </section> <!-- 结束当前界面区域。 -->

        <section v-if="isParent" class="connection-section device-children" v-loading="lists.children.loading"> <!-- 渲染 section 界面元素。 -->
          <h3>子设备（{{lists.children.total}}）</h3> <!-- 渲染 h3 界面元素。 -->
          <ui-alert v-if="lists.children.error" title="子设备加载失败" :description="lists.children.error" type="error" :closable="false" /> <!-- 渲染 ui-alert 界面元素。 -->
          <ui-table v-else :data="lists.children.items" border empty-text="暂无子设备，等待主设备上报登记信息"> <!-- 渲染 ui-table 界面元素。 -->
            <ui-table-column prop="device.name" label="名称" min-width="140" /><ui-table-column prop="device.tags.childAddress" label="地址" min-width="90" /> <!-- 渲染 ui-table-column 界面元素。 -->
            <ui-table-column prop="productName" label="设备模板" min-width="130" /> <!-- 渲染 ui-table-column 界面元素。 -->
            <ui-table-column label="协议" min-width="150"><template #default="{row}">{{row.binding?.protocolId || '未配置'}} · {{row.binding?.version || '—'}}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
            <ui-table-column label="最近上报" min-width="170"><template #default="{row}">{{formatTime(row.runtimeState?.lastSeenAt)}}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
            <ui-table-column label="状态" min-width="95"><template #default="{row}">{{label(businessStatuses,row.runtimeState?.businessStatus) || '未知'}}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
            <ui-table-column label="操作" width="110" fixed="right"><template #default="{row}"><ui-button link type="primary" @click="emit('device',row.device.id)">查看子设备</ui-button></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
          </ui-table> <!-- 结束当前界面区域。 -->
          <ui-pagination v-if="lists.children.total>20" v-model:current-page="lists.children.page" :page-size="20" :total="lists.children.total" layout="prev, pager, next" @current-change="loadList('children')" /> <!-- 渲染 ui-pagination 界面元素。 -->
        </section> <!-- 结束当前界面区域。 -->

        <section v-if="hasSessions" class="connection-section device-sessions"> <!-- 渲染 section 界面元素。 -->
          <h3>{{data.parent ? '主设备通信会话' : '在线会话'}}</h3> <!-- 渲染 h3 界面元素。 -->
          <ui-table :data="data.sessions || []" border empty-text="暂无已识别的在线会话"> <!-- 渲染 ui-table 界面元素。 -->
            <ui-table-column prop="profileId" label="平台连接配置" min-width="145" /><ui-table-column prop="remoteAddress" label="远端地址" min-width="155" /> <!-- 渲染 ui-table-column 界面元素。 -->
            <ui-table-column prop="protocolId" label="会话协议" min-width="130" /><ui-table-column prop="protocolVersion" label="会话版本" min-width="100" /> <!-- 渲染 ui-table-column 界面元素。 -->
            <ui-table-column label="最后有效报文" min-width="175"><template #default="{row}">{{formatTime(row.lastSeenAt)}}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
          </ui-table> <!-- 结束当前界面区域。 -->
        </section> <!-- 结束当前界面区域。 -->

        <section class="connection-section device-properties"> <!-- 渲染 section 界面元素。 -->
          <h3>最新属性 <small>{{formatTime(data.latestProperties?.[0]?.timestamp)}}</small></h3> <!-- 渲染 h3 界面元素。 -->
          <ui-table :data="properties" border empty-text="暂无已解析的属性报文"> <!-- 渲染 ui-table 界面元素。 -->
            <ui-table-column prop="key" label="属性" :width="narrow ? 105 : 200" /><ui-table-column label="值" min-width="120"><template #default="{row}"><span class="field-value">{{displayValue(row.value)}}</span></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
          </ui-table> <!-- 结束当前界面区域。 -->
        </section> <!-- 结束当前界面区域。 -->

        <section class="connection-section device-latest"> <!-- 渲染 section 界面元素。 -->
          <h3>最新报文</h3> <!-- 渲染 h3 界面元素。 -->
          <ui-descriptions v-if="data.latest?.messageId" :column="columns" border> <!-- 渲染 ui-descriptions 界面元素。 -->
            <ui-descriptions-item label="消息类型">{{messageTypeLabel(data.latest.messageType)}}</ui-descriptions-item><ui-descriptions-item label="时间">{{formatTime(data.latest.timestamp)}}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
            <ui-descriptions-item label="标准消息标识" :span="columns"><code>{{data.latest.messageId}}</code></ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
          </ui-descriptions> <!-- 结束当前界面区域。 -->
          <ui-empty v-else description="暂无已解析报文" :image-size="48" /> <!-- 渲染 ui-empty 界面元素。 -->
          <ui-collapse v-if="data.latest?.messageId" class="message-detail"><ui-collapse-item title="查看完整标准消息" name="message"><pre>{{pretty(data.latest)}}</pre></ui-collapse-item></ui-collapse> <!-- 渲染 ui-collapse 界面元素。 -->
          <div class="section-actions"><ui-button v-permission="'menu:raw'" @click="emit('navigate','raw',{deviceId:props.deviceId})">原始报文与回放</ui-button><ui-button v-permission="'menu:alarms'" @click="emit('navigate','alarms',{deviceId:props.deviceId})">设备告警</ui-button></div> <!-- 渲染 div 界面元素。 -->
        </section> <!-- 结束当前界面区域。 -->

        <section class="connection-section device-history" v-loading="lists.history.loading"> <!-- 渲染 section 界面元素。 -->
          <h3>连接与状态历史</h3> <!-- 渲染 h3 界面元素。 -->
          <ui-alert v-if="lists.history.error" title="状态历史加载失败" :description="lists.history.error" type="error" :closable="false" /> <!-- 渲染 ui-alert 界面元素。 -->
          <ui-table v-else :data="lists.history.items" border empty-text="暂无状态变更"> <!-- 渲染 ui-table 界面元素。 -->
            <ui-table-column label="记录时间" min-width="175"><template #default="{row}">{{formatTime(row.recordedAt)}}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
            <ui-table-column label="连接" min-width="100"><template #default="{row}">{{label(connectionStatuses,row.state?.connectionStatus)}}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
            <ui-table-column label="业务" min-width="100"><template #default="{row}">{{label(businessStatuses,row.state?.businessStatus)}}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
            <ui-table-column label="来源" min-width="100"><template #default="{row}">{{label(stateSources,row.state?.statusSource)}}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
          </ui-table> <!-- 结束当前界面区域。 -->
          <ui-pagination v-if="lists.history.total>20" v-model:current-page="lists.history.page" :page-size="20" :total="lists.history.total" layout="prev, pager, next" @current-change="loadList('history')" /> <!-- 渲染 ui-pagination 界面元素。 -->
        </section> <!-- 结束当前界面区域。 -->

        <section class="connection-section device-events" v-loading="lists.events.loading"> <!-- 渲染 section 界面元素。 -->
          <h3>最近事件</h3> <!-- 渲染 h3 界面元素。 -->
          <ui-alert v-if="lists.events.error" title="事件加载失败" :description="lists.events.error" type="error" :closable="false" /> <!-- 渲染 ui-alert 界面元素。 -->
          <ui-table v-else :data="lists.events.items" border empty-text="暂无事件"> <!-- 渲染 ui-table 界面元素。 -->
            <ui-table-column label="时间" min-width="175"><template #default="{row}">{{formatTime(row.timestamp)}}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
            <ui-table-column label="事件内容" min-width="200"><template #default="{row}"><span class="field-value">{{pretty(row.event || {})}}</span></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
          </ui-table> <!-- 结束当前界面区域。 -->
          <ui-pagination v-if="lists.events.total>20" v-model:current-page="lists.events.page" :page-size="20" :total="lists.events.total" layout="prev, pager, next" @current-change="loadList('events')" /> <!-- 渲染 ui-pagination 界面元素。 -->
        </section> <!-- 结束当前界面区域。 -->

        <section class="connection-section device-alarms"> <!-- 渲染 section 界面元素。 -->
          <h3>最近告警</h3> <!-- 渲染 h3 界面元素。 -->
          <ui-table :data="data.recentAlarms || []" border empty-text="暂无告警"> <!-- 渲染 ui-table 界面元素。 -->
            <ui-table-column label="告警" min-width="100"><template #default="{row}">{{alarmType(row.alarmType)}}</template></ui-table-column><ui-table-column label="级别" min-width="90"><template #default="{row}">{{alarmLevel(row.alarmLevel)}}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
            <ui-table-column label="状态" min-width="100"><template #default="{row}">{{label(alarmStatuses,row.status)}}</template></ui-table-column><ui-table-column label="最近触发" min-width="175"><template #default="{row}">{{formatTime(row.lastTriggeredAt)}}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
          </ui-table> <!-- 结束当前界面区域。 -->
        </section> <!-- 结束当前界面区域。 -->

        <section v-if="operations.length && (data.connector==='MQTT' || (data.profile && data.canCommand))" class="connection-section device-commands"> <!-- 渲染 section 界面元素。 -->
          <h3>设备控制</h3> <!-- 渲染 h3 界面元素。 -->
          <p>已发送不代表设备执行成功。请核对发送状态和设备应答；结果未知时不要重复发送。</p> <!-- 渲染 p 界面元素。 -->
          <template v-if="canEdit">
            <ui-form label-position="top" :disabled="actionBusy || loading"> <!-- 渲染 ui-form 界面元素。 -->
              <ui-form-item label="设备命令"><ui-select v-model="commandType" aria-label="设备命令" placeholder="选择设备支持的命令"><ui-option v-for="c in operations" :key="c.identifier" :value="c.identifier" :label="c.name || c.identifier" /></ui-select></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
              <ui-form-item v-for="field in selectedOperation?.fields || []" :key="`${commandType}:${field.identifier}`" :label="`${field.name || field.identifier}${field.unit ? `（${field.unit}）` : ''}`" :required="field.required"> <!-- 渲染 ui-form-item 界面元素。 -->
                <CommandValueInput v-model="commandValues[field.identifier]" :kind="field.dataType" :label="field.name || field.identifier" /> <!-- 渲染 CommandValueInput 界面元素。 -->
              </ui-form-item> <!-- 结束当前界面区域。 -->
              <p v-if="selectedOperation && !selectedOperation.fields?.length">此命令无需参数。</p> <!-- 渲染 p 界面元素。 -->
            </ui-form> <!-- 结束当前界面区域。 -->
            <ui-button v-permission="'POST /api/v1/device-registry/:id/commands'" v-if="data.connector==='MQTT'" :disabled="loading || !data.mqttCommandAvailable || !data.credentialEnabled || !selectedOperation" :loading="actionBusy" @click="sendMQTT">执行命令</ui-button> <!-- 渲染 ui-button 界面元素。 -->
            <ui-button v-permission="'POST /api/v2/device-access-profiles/:id/devices/:deviceId/commands'" v-else :loading="actionBusy" :disabled="loading || !data.profile.enabled || !data.sessions?.length || !selectedOperation" @click="send">执行命令</ui-button> <!-- 渲染 ui-button 界面元素。 -->
            <p v-if="data.connector!=='MQTT' && (!data.profile.enabled || !data.sessions?.length)">当前无可用连接或平台连接配置已停用，暂时不能下发命令。</p> <!-- 渲染 p 界面元素。 -->
            <ui-button v-if="pendingCommand || pendingProtocol" class="section-feedback" :disabled="actionBusy" @click="newCommand">开始一条新命令</ui-button> <!-- 渲染 ui-button 界面元素。 -->
          </template>
          <ui-alert v-if="commandResult?.lastError" :title="label(commandStatuses,String(commandResult.status || '').toUpperCase())" :description="commandResult.lastError" type="warning" :closable="false" />
          <ui-descriptions v-if="commandResult" :column="1" border class="section-feedback"><ui-descriptions-item label="发送状态">{{label(commandStatuses,String(commandResult.status || '').toUpperCase())}}</ui-descriptions-item><ui-descriptions-item label="设备应答">{{commandResult.reply || commandResult.response ? pretty(commandResult.reply || commandResult.response) : commandResult.rawMessageId ? `已收到应答，原始报文：${commandResult.rawMessageId}` : '尚无应答内容'}}</ui-descriptions-item></ui-descriptions>
          <ui-button v-if="commandResult?.rawMessageId" class="section-feedback" @click="showCommandReply">查看应答报文</ui-button>
          <pre v-if="commandReply">{{pretty(commandReply)}}</pre>
          <template v-if="data.connector==='MQTT'">
            <ui-alert v-if="lists.commands.error" title="命令记录加载失败" :description="lists.commands.error" type="error" :closable="false" /> <!-- 渲染 ui-alert 界面元素。 -->
            <ui-table v-else :data="lists.commands.items" border empty-text="暂无命令记录"><ui-table-column prop="type" label="命令" min-width="120" /><ui-table-column label="状态" min-width="170"><template #default="{row}">{{label(commandStatuses,row.status)}}</template></ui-table-column><ui-table-column label="回执" min-width="180"><template #default="{row}"><span class="field-value">{{pretty(row.reply || {})}}</span></template></ui-table-column></ui-table> <!-- 渲染 ui-table 界面元素。 -->
            <ui-pagination v-if="lists.commands.total>20" v-model:current-page="lists.commands.page" :page-size="20" :total="lists.commands.total" layout="prev, pager, next" @current-change="loadList('commands')" /> <!-- 渲染 ui-pagination 界面元素。 -->
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
  </ui-drawer>
</template>

<style scoped>
:global(.device-connection-drawer .el-drawer__header) { margin-bottom:0; padding:20px 24px; border-bottom:1px solid var(--border); color:var(--accent-foreground); background:var(--card); } /* 设置  样式。 */
:global(.device-connection-drawer .el-drawer__body) { background:var(--surface-subtle); padding:20px; } /* 设置  样式。 */
.device-connection { min-width:0; color:var(--text-strong); font-size:13px; line-height:1.6; } /* 定义当前元素的样式规则。 */
.connection-toolbar { display:flex; align-items:center; justify-content:space-between; gap:16px; margin-bottom:16px; } /* 定义当前元素的样式规则。 */
.connection-toolbar > div { min-width:0; } /* 定义当前元素的样式规则。 */
.connection-toolbar strong { display:block; font-size:17px; color:var(--text-strong); } /* 定义当前元素的样式规则。 */
.connection-toolbar small { display:block; overflow-wrap:anywhere; color:var(--accent-foreground); } /* 定义当前元素的样式规则。 */
.connection-section { min-width:0; margin:0 0 16px; padding:18px; border:1px solid var(--border); border-radius:8px; background:var(--card); } /* 定义当前元素的样式规则。 */
.connection-status-grid { display:grid; grid-template-columns:repeat(3,minmax(0,1fr)); gap:9px; margin-bottom:20px; }
.connection-status-grid > div { min-width:0; padding:11px 13px; border:1px solid var(--border); border-radius:7px; background:var(--surface-subtle); }
.connection-status-grid span { display:block; margin-bottom:4px; color:var(--accent-foreground); font-size:12px; }
.connection-status-grid strong { display:block; color:var(--accent-foreground); font-size:14px; line-height:1.45; overflow-wrap:anywhere; }
.connection-subtitle { margin:0 0 10px; color:var(--accent-foreground); font-size:13px; font-weight:650; }
h3 { display:flex; flex-wrap:wrap; gap:8px; align-items:baseline; margin:0 0 14px; font-size:14px; font-weight:650; line-height:1.5; color:var(--accent-foreground); } /* 设置 h3 { display 样式。 */
h3 small { font-size:13px; font-weight:400; color:var(--accent-foreground); } /* 设置 h3 small { font-size 样式。 */
p { margin:10px 0; color:var(--accent-foreground); overflow-wrap:anywhere; } /* 设置 p { margin 样式。 */
code,.field-value { color:inherit; font:inherit; overflow-wrap:anywhere; word-break:break-word; white-space:pre-wrap; } /* 设置 code,.field-value { color 样式。 */
pre { max-height:320px; overflow:auto; white-space:pre-wrap; overflow-wrap:anywhere; background:var(--surface-subtle); border:1px solid var(--border); border-radius:6px; padding:12px; color:var(--accent-foreground); margin:12px 0 0; } /* 设置 pre { max-height 样式。 */
.section-actions { display:flex; flex-wrap:wrap; gap:8px; margin-top:14px; } /* 定义当前元素的样式规则。 */
.section-actions :deep(.el-button + .el-button) { margin-left:0; } /* 定义当前元素的样式规则。 */
.section-feedback,.profile-picker,.message-detail { margin-top:14px; } /* 定义当前元素的样式规则。 */
:deep(.el-descriptions__table) { table-layout:fixed; } /* 设置  样式。 */
:deep(.el-descriptions__label.el-descriptions__cell.is-bordered-label) { width:120px; background:var(--surface-subtle); color:var(--accent-foreground); font-weight:500; } /* 设置  样式。 */
:deep(.el-descriptions__content.el-descriptions__cell.is-bordered-content) { background:var(--card); color:var(--accent-foreground); overflow-wrap:anywhere; } /* 设置  样式。 */
:deep(.el-descriptions__cell) { padding:10px 12px !important; border-color:var(--border) !important; } /* 设置  样式。 */
:deep(.el-pagination) { margin-top:12px; justify-content:flex-end; } /* 设置  样式。 */
:deep(.el-collapse-item__header) { font-size:13px; font-weight:600; color:var(--accent-foreground); } /* 设置  样式。 */
@media (max-width:640px) { /* 按屏幕条件调整样式。 */
  :global(.device-connection-drawer .el-drawer__header) { padding:16px; } /* 设置  样式。 */
  :global(.device-connection-drawer .el-drawer__body) { padding:12px; } /* 设置  样式。 */
  .connection-section { padding:12px; margin-bottom:12px; } /* 定义当前元素的样式规则。 */
  .connection-status-grid { grid-template-columns:repeat(2,minmax(0,1fr)); gap:7px; margin-bottom:16px; }
  .connection-status-grid > div { padding:9px; }
  :deep(.el-descriptions__label.el-descriptions__cell.is-bordered-label) { width:100px; } /* 设置  样式。 */
  :deep(.el-descriptions__cell) { padding:9px !important; } /* 设置  样式。 */
} /* 结束当前样式规则。 */
</style>
