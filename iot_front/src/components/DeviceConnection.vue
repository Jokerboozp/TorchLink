<script setup>
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { ElMessageBox, ElMessage } from 'element-plus'
import { api, formatTime, notifyError, pretty, session } from '../api'
import { transportLabel, statusLabel } from '../presentation'
import { commandStatuses, alarmType, alarmLevel, alarmStatuses, connectionStatuses, dataStatuses, businessStatuses, stateSources, messageTypeLabel, label } from '../labels'

const props = defineProps({ deviceId:String })
const emit = defineEmits(['close','navigate','device'])
const data = ref(null), loading = ref(false), actionBusy = ref(false), error = ref(''), selectedProfile = ref('')
const credential = ref(null), commandResult = ref(null)
const commandType = ref(''), commandData = ref('{}'), command = ref('{"type":""}')
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
    data.value = result; selectedProfile.value = result.profile?.id || ''
    const jobs = [loadList('history'),loadList('events')]
    if (isParent.value) jobs.push(loadList('children'))
    if (result.connector === 'MQTT') jobs.push(loadList('commands'))
    await Promise.all(jobs)
  } catch (cause) {
    if (current === generation && cause.name !== 'AbortError') { data.value = null; error.value = cause.message }
  } finally { if (current === generation) loading.value = false }
}
function selectProfile() { pendingProtocol.value = null; commandResult.value = null; load() }
function newCommand() { pendingCommand.value = null; pendingProtocol.value = null; commandResult.value = null }
function objectJSON(value, title) {
  let parsed
  try { parsed = JSON.parse(value) } catch { throw new Error(`${title}须为有效的 JSON 对象`) }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error(`${title}须为 JSON 对象`)
  return parsed
}
async function action(work) {
  if (actionBusy.value || loading.value || !canEdit.value) return
  actionBusy.value = true
  try { await work() } catch (cause) { if (cause !== 'cancel' && cause !== 'close') notifyError(cause) }
  finally { actionBusy.value = false }
}
async function rotate() {
  await action(async () => {
    await ElMessageBox.confirm('重新生成后旧凭据立即在平台停用；消息服务撤销结果可在下方查看。','重新生成凭据')
    const result = await api(`${base()}/credentials`,{method:'POST'})
    credential.value = result.credential; await load()
  })
}
async function disable() {
  await action(async () => {
    await ElMessageBox.confirm('禁用后设备不能使用此凭据上报或换取MQTT 令牌。','禁用设备凭据')
    await api(`${base()}/credentials`,{method:'DELETE'})
    credential.value = null; ElMessage.success('凭据已禁用'); await load()
  })
}
async function sendMQTT() {
  await action(async () => {
    const type = commandType.value.trim()
    if (!type) throw new Error('请填写命令类型')
    const body = {type,data:objectJSON(commandData.value,'命令参数')}
    await ElMessageBox.confirm('确认向该设备发送此命令？发送成功不代表执行成功。','人工确认命令')
    const signature = JSON.stringify(body)
    if (!pendingCommand.value || pendingCommand.value.signature !== signature) pendingCommand.value = {signature,id:crypto.randomUUID()}
    commandResult.value = await api(`${base()}/commands`,{method:'POST',body:JSON.stringify({...body,id:pendingCommand.value.id,confirmed:true})})
    await loadList('commands')
  })
}
async function send() {
  await action(async () => {
    const body = objectJSON(command.value,'协议命令')
    if (typeof body.type !== 'string' || !body.type.trim()) throw new Error('请在协议命令中填写 type')
    await ElMessageBox.confirm('确认向该设备发送协议命令？请核对设备与参数。','人工确认命令')
    const profileId = data.value.profile.id, signature = JSON.stringify([profileId,props.deviceId,body])
    if (!pendingProtocol.value || pendingProtocol.value.signature !== signature) pendingProtocol.value = {signature,id:crypto.randomUUID()}
    commandResult.value = await api(`/api/v2/device-access-profiles/${encodeURIComponent(profileId)}/devices/${encodeURIComponent(props.deviceId)}/commands`,{method:'POST',body:JSON.stringify({...body,requestId:pendingProtocol.value.id,confirmed:true})})
  })
}
watch(() => props.deviceId,() => {
  data.value = null; selectedProfile.value = ''; credential.value = null; newCommand()
  for (const section of Object.values(lists)) Object.assign(section,{items:[],total:0,page:1,error:'',loading:false})
  load()
},{immediate:true})
onBeforeUnmount(() => { generation++; controller.abort(); media.removeEventListener('change',resize); credential.value = null })
</script>

<template>
  <el-drawer :model-value="true" class="device-connection-drawer" title="设备连接与数据" size="min(900px, 100vw)" @close="emit('close')">
    <div class="device-connection" v-loading="loading">
      <div class="connection-toolbar">
        <div><strong>{{data?.device?.name || '设备详情'}}</strong><small>{{props.deviceId}}</small></div>
        <el-button :loading="loading" :disabled="actionBusy" @click="load">刷新</el-button>
      </div>
      <el-alert v-if="error" title="设备详情加载失败" :description="error" type="error" :closable="false" show-icon />
      <el-empty v-if="!data && !loading && !error" description="暂无设备信息" />
      <template v-if="data">
        <section class="connection-section device-summary">
          <h3>连接概览</h3>
          <el-descriptions :column="columns" border>
            <el-descriptions-item label="设备">{{data.device.name || props.deviceId}}</el-descriptions-item>
            <el-descriptions-item label="产品">{{data.product?.name || data.device.productId || '—'}}</el-descriptions-item>
            <el-descriptions-item label="接入方式">{{transportLabel(data.connector) || '未配置'}}</el-descriptions-item>
            <el-descriptions-item label="创建时间">{{formatTime(data.device.createdAt)}}</el-descriptions-item>
            <el-descriptions-item label="当前绑定协议">{{data.protocolId || '—'}}</el-descriptions-item>
            <el-descriptions-item label="绑定版本">{{data.protocolVersion || '—'}}</el-descriptions-item>
            <el-descriptions-item label="连接状态">{{label(connectionStatuses,data.connection?.connectionStatus) || '未知'}}</el-descriptions-item>
            <el-descriptions-item label="数据状态">{{label(dataStatuses,data.connection?.dataStatus) || '未知'}}</el-descriptions-item>
            <el-descriptions-item label="业务状态">{{label(businessStatuses,data.connection?.businessStatus) || '未知'}}</el-descriptions-item>
            <el-descriptions-item label="最后上报">{{formatTime(data.connection?.lastSeenAt)}}</el-descriptions-item>
            <el-descriptions-item label="最后连接">{{formatTime(data.connection?.lastConnectAt)}}</el-descriptions-item>
            <el-descriptions-item label="最后断开">{{formatTime(data.connection?.lastDisconnectAt)}}</el-descriptions-item>
            <el-descriptions-item label="原文接收">{{data.ingest?.rawReceived ? '已收到' : '等待上报'}}</el-descriptions-item>
            <el-descriptions-item label="解析状态">{{data.ingest?.parsed ? '已完成' : data.ingest?.parseError ? '失败' : data.ingest?.rawReceived ? '等待处理' : '等待上报'}}</el-descriptions-item>
            <el-descriptions-item v-if="data.profile" label="运行时">{{data.profile.runtimeStatus ? statusLabel(data.profile.runtimeStatus) : '待确认'}}</el-descriptions-item>
            <el-descriptions-item v-if="data.profile?.collectorId" label="采集器">{{data.profile.collectorId}}</el-descriptions-item>
            <el-descriptions-item v-if="data.parent" label="所属主设备"><el-button link type="primary" @click="emit('device',data.parent.id)">{{data.parent.name || data.parent.id}}</el-button></el-descriptions-item>
            <el-descriptions-item v-if="data.parent" label="子设备地址">{{data.device.tags?.childAddress || '—'}}</el-descriptions-item>
          </el-descriptions>
          <el-alert v-if="data.profile?.lastError || data.ingest?.parseError" class="section-feedback" title="最近接入异常" :description="data.profile?.lastError || data.ingest?.parseError" type="warning" :closable="false" show-icon />
          <div v-if="data.profiles?.length > 1" class="profile-picker">
            <p>检测到多个关联实例，请选择要查看和发送命令的实例。</p>
            <el-select v-model="selectedProfile" :disabled="loading || actionBusy" placeholder="选择接入实例" @change="selectProfile"><el-option v-for="p in data.profiles" :key="p.id" :value="p.id" :label="p.id" /></el-select>
          </div>
        </section>

        <section v-if="standardAccess" class="connection-section device-access-info">
          <h3>设备接入信息</h3>
          <el-descriptions :column="1" border>
            <el-descriptions-item v-if="data.connector==='HTTP'" label="上报接口"><code>{{data.accessInfo.httpUrl}}</code></el-descriptions-item>
            <el-descriptions-item v-if="data.connector==='MQTT'" label="消息服务地址"><code>{{data.accessInfo.mqttBroker || '未配置对外地址'}}</code></el-descriptions-item>
            <el-descriptions-item v-if="data.connector==='MQTT'" label="客户端标识"><code>{{data.accessInfo.clientId}}</code></el-descriptions-item>
            <el-descriptions-item label="接入密钥"><code>{{data.accessInfo.username}}</code></el-descriptions-item>
            <el-descriptions-item v-if="data.connector==='MQTT'" label="上行 Topic"><code>{{data.accessInfo.upTopic}}</code></el-descriptions-item>
            <el-descriptions-item v-if="data.connector==='MQTT'" label="下行 Topic"><code>{{data.accessInfo.downTopic}}</code></el-descriptions-item>
            <el-descriptions-item label="凭据状态">{{data.credentialEnabled ? '已启用' : '已禁用'}}</el-descriptions-item>
          </el-descriptions>
        </section>

        <section v-if="isParent" class="connection-section device-children" v-loading="lists.children.loading">
          <h3>子设备（{{lists.children.total}}）</h3>
          <el-alert v-if="lists.children.error" title="子设备加载失败" :description="lists.children.error" type="error" :closable="false" />
          <el-table v-else :data="lists.children.items" border empty-text="暂无子设备，等待主设备上报登记信息">
            <el-table-column prop="device.name" label="名称" min-width="140" /><el-table-column prop="device.tags.childAddress" label="地址" min-width="90" />
            <el-table-column prop="productName" label="产品" min-width="130" />
            <el-table-column label="协议" min-width="150"><template #default="{row}">{{row.binding?.protocolId || '未配置'}} · {{row.binding?.version || '—'}}</template></el-table-column>
            <el-table-column label="最近上报" min-width="170"><template #default="{row}">{{formatTime(row.runtimeState?.lastSeenAt)}}</template></el-table-column>
            <el-table-column label="状态" min-width="95"><template #default="{row}">{{label(businessStatuses,row.runtimeState?.businessStatus) || '未知'}}</template></el-table-column>
            <el-table-column label="操作" width="110" fixed="right"><template #default="{row}"><el-button link type="primary" @click="emit('device',row.device.id)">查看子设备</el-button></template></el-table-column>
          </el-table>
          <el-pagination v-if="lists.children.total>20" v-model:current-page="lists.children.page" :page-size="20" :total="lists.children.total" layout="prev, pager, next" @current-change="loadList('children')" />
        </section>

        <section v-if="hasSessions" class="connection-section device-sessions">
          <h3>{{data.parent ? '主设备通信会话' : '在线会话'}}</h3>
          <el-table :data="data.sessions || []" border empty-text="暂无已识别的在线会话">
            <el-table-column prop="profileId" label="接入实例" min-width="145" /><el-table-column prop="remoteAddress" label="远端地址" min-width="155" />
            <el-table-column prop="protocolId" label="会话协议" min-width="130" /><el-table-column prop="protocolVersion" label="会话版本" min-width="100" />
            <el-table-column label="最后有效报文" min-width="175"><template #default="{row}">{{formatTime(row.lastSeenAt)}}</template></el-table-column>
          </el-table>
        </section>

        <section class="connection-section device-properties">
          <h3>最新属性 <small>{{formatTime(data.latestProperties?.[0]?.timestamp)}}</small></h3>
          <el-table :data="properties" border empty-text="暂无已解析的属性报文">
            <el-table-column prop="key" label="属性" :width="narrow ? 105 : 200" /><el-table-column label="值" min-width="120"><template #default="{row}"><span class="field-value">{{displayValue(row.value)}}</span></template></el-table-column>
          </el-table>
        </section>

        <section class="connection-section device-latest">
          <h3>最新报文</h3>
          <el-descriptions v-if="data.latest?.messageId" :column="columns" border>
            <el-descriptions-item label="消息类型">{{messageTypeLabel(data.latest.messageType)}}</el-descriptions-item><el-descriptions-item label="时间">{{formatTime(data.latest.timestamp)}}</el-descriptions-item>
            <el-descriptions-item label="标准消息标识" :span="columns"><code>{{data.latest.messageId}}</code></el-descriptions-item>
          </el-descriptions>
          <el-empty v-else description="暂无已解析报文" :image-size="48" />
          <el-collapse v-if="data.latest?.messageId" class="message-detail"><el-collapse-item title="查看完整标准消息" name="message"><pre>{{pretty(data.latest)}}</pre></el-collapse-item></el-collapse>
          <div class="section-actions"><el-button @click="emit('navigate','raw',{deviceId:props.deviceId})">原始报文与回放</el-button><el-button @click="emit('navigate','alarms',{deviceId:props.deviceId})">设备告警</el-button></div>
        </section>

        <section class="connection-section device-history" v-loading="lists.history.loading">
          <h3>连接与状态历史</h3>
          <el-alert v-if="lists.history.error" title="状态历史加载失败" :description="lists.history.error" type="error" :closable="false" />
          <el-table v-else :data="lists.history.items" border empty-text="暂无状态变更">
            <el-table-column label="记录时间" min-width="175"><template #default="{row}">{{formatTime(row.recordedAt)}}</template></el-table-column>
            <el-table-column label="连接" min-width="100"><template #default="{row}">{{label(connectionStatuses,row.state?.connectionStatus)}}</template></el-table-column>
            <el-table-column label="业务" min-width="100"><template #default="{row}">{{label(businessStatuses,row.state?.businessStatus)}}</template></el-table-column>
            <el-table-column label="来源" min-width="100"><template #default="{row}">{{label(stateSources,row.state?.statusSource)}}</template></el-table-column>
          </el-table>
          <el-pagination v-if="lists.history.total>20" v-model:current-page="lists.history.page" :page-size="20" :total="lists.history.total" layout="prev, pager, next" @current-change="loadList('history')" />
        </section>

        <section class="connection-section device-events" v-loading="lists.events.loading">
          <h3>最近事件</h3>
          <el-alert v-if="lists.events.error" title="事件加载失败" :description="lists.events.error" type="error" :closable="false" />
          <el-table v-else :data="lists.events.items" border empty-text="暂无事件">
            <el-table-column label="时间" min-width="175"><template #default="{row}">{{formatTime(row.timestamp)}}</template></el-table-column>
            <el-table-column label="事件内容" min-width="200"><template #default="{row}"><span class="field-value">{{pretty(row.event || {})}}</span></template></el-table-column>
          </el-table>
          <el-pagination v-if="lists.events.total>20" v-model:current-page="lists.events.page" :page-size="20" :total="lists.events.total" layout="prev, pager, next" @current-change="loadList('events')" />
        </section>

        <section class="connection-section device-alarms">
          <h3>最近告警</h3>
          <el-table :data="data.recentAlarms || []" border empty-text="暂无告警">
            <el-table-column label="告警" min-width="100"><template #default="{row}">{{alarmType(row.alarmType)}}</template></el-table-column><el-table-column label="级别" min-width="90"><template #default="{row}">{{alarmLevel(row.alarmLevel)}}</template></el-table-column>
            <el-table-column label="状态" min-width="100"><template #default="{row}">{{label(alarmStatuses,row.status)}}</template></el-table-column><el-table-column label="最近触发" min-width="175"><template #default="{row}">{{formatTime(row.lastTriggeredAt)}}</template></el-table-column>
          </el-table>
        </section>

        <section v-if="data.connector==='MQTT' || (data.profile && data.canCommand)" class="connection-section device-commands">
          <h3>{{data.connector==='MQTT' ? '设备消息命令' : '协议命令'}}</h3>
          <p>已发送不代表设备执行成功。回执到达后刷新可查看结果；重试相同命令不会再次下发。</p>
          <template v-if="canEdit">
            <el-form v-if="data.connector==='MQTT'" label-position="top">
              <el-form-item label="命令类型"><el-select v-if="data.product?.thingModel?.commands?.length" v-model="commandType" placeholder="命令类型"><el-option v-for="c in data.product.thingModel.commands" :key="c.identifier" :value="c.identifier" :label="c.name || c.identifier" /></el-select><el-input v-else v-model="commandType" placeholder="命令类型" /></el-form-item>
              <el-form-item label="命令参数"><el-input v-model="commandData" type="textarea" :rows="4" placeholder="命令参数 JSON 对象" /></el-form-item>
              <el-button :disabled="loading || !data.mqttCommandAvailable || !data.credentialEnabled" :loading="actionBusy" @click="sendMQTT">发送设备消息命令</el-button>
            </el-form>
            <template v-else>
              <el-input v-model="command" type="textarea" :rows="4" placeholder="按协议文档填写命令" />
              <el-button class="section-feedback" :loading="actionBusy" :disabled="loading || !data.profile.enabled || !data.sessions?.length" @click="send">发送命令</el-button>
              <p v-if="!data.profile.enabled || !data.sessions?.length">当前无可用连接或实例已停用，暂时不能下发命令。</p>
            </template>
            <el-button v-if="pendingCommand || pendingProtocol" class="section-feedback" :disabled="actionBusy" @click="newCommand">开始一条新命令</el-button>
          </template>
          <el-alert v-if="commandResult?.lastError" :title="label(commandStatuses,commandResult.status)" :description="commandResult.lastError" type="warning" :closable="false" />
          <pre v-if="commandResult">{{pretty(commandResult)}}</pre>
          <template v-if="data.connector==='MQTT'">
            <el-alert v-if="lists.commands.error" title="命令记录加载失败" :description="lists.commands.error" type="error" :closable="false" />
            <el-table v-else :data="lists.commands.items" border empty-text="暂无命令记录"><el-table-column prop="type" label="命令" min-width="120" /><el-table-column label="状态" min-width="170"><template #default="{row}">{{label(commandStatuses,row.status)}}</template></el-table-column><el-table-column label="回执" min-width="180"><template #default="{row}"><span class="field-value">{{pretty(row.reply || {})}}</span></template></el-table-column></el-table>
            <el-pagination v-if="lists.commands.total>20" v-model:current-page="lists.commands.page" :page-size="20" :total="lists.commands.total" layout="prev, pager, next" @current-change="loadList('commands')" />
          </template>
        </section>

        <section v-if="standardAccess && canEdit" class="connection-section device-credentials">
          <h3>设备凭据</h3><p>重新生成后旧凭据立即停用，新密钥仅显示一次。</p>
          <div class="section-actions"><el-button :disabled="loading || actionBusy || !data.credentialEnabled" @click="disable">禁用凭据</el-button><el-button :disabled="loading || actionBusy" @click="rotate">重新生成凭据</el-button></div>
          <pre v-if="credential">仅本次显示，请妥善保存：{{pretty(credential)}}</pre>
          <p v-for="revocation in data.revocations" :key="revocation.id">旧凭据消息服务撤销：{{revocation.status==='REVOKED' ? '已完成' : '待完成（平台已停用旧凭据）'}}</p>
        </section>
      </template>
    </div>
  </el-drawer>
</template>

<style scoped>
:global(.device-connection-drawer .el-drawer__header) { margin-bottom:0; padding:20px 24px; border-bottom:1px solid #d5dde8; color:#172b4d; background:#fff; }
:global(.device-connection-drawer .el-drawer__body) { background:#f1f4f8; padding:20px; }
.device-connection { min-width:0; color:#1e293b; font-size:13px; line-height:1.6; }
.connection-toolbar { display:flex; align-items:center; justify-content:space-between; gap:16px; margin-bottom:16px; }
.connection-toolbar > div { min-width:0; }
.connection-toolbar strong { display:block; font-size:17px; color:#0f172a; }
.connection-toolbar small { display:block; overflow-wrap:anywhere; color:#52637a; }
.connection-section { min-width:0; margin:0 0 16px; padding:18px; border:1px solid #d5dde8; border-radius:8px; background:#fff; }
h3 { display:flex; flex-wrap:wrap; gap:8px; align-items:baseline; margin:0 0 14px; font-size:14px; font-weight:650; line-height:1.5; color:#172b4d; }
h3 small { font-size:12px; font-weight:400; color:#52637a; }
p { margin:10px 0; color:#52637a; overflow-wrap:anywhere; }
code,.field-value { color:inherit; font:inherit; overflow-wrap:anywhere; word-break:break-word; white-space:pre-wrap; }
pre { max-height:320px; overflow:auto; white-space:pre-wrap; overflow-wrap:anywhere; background:#f4f6fa; border:1px solid #d5dde8; border-radius:6px; padding:12px; color:#334155; margin:12px 0 0; }
.section-actions { display:flex; flex-wrap:wrap; gap:8px; margin-top:14px; }
.section-actions :deep(.el-button + .el-button) { margin-left:0; }
.section-feedback,.profile-picker,.message-detail { margin-top:14px; }
:deep(.el-descriptions__table) { table-layout:fixed; }
:deep(.el-descriptions__label.el-descriptions__cell.is-bordered-label) { width:120px; background:#edf2f7; color:#3c4e66; font-weight:500; }
:deep(.el-descriptions__content.el-descriptions__cell.is-bordered-content) { background:#fff; color:#172b4d; overflow-wrap:anywhere; }
:deep(.el-descriptions__cell) { padding:10px 12px !important; border-color:#d5dde8 !important; }
:deep(.el-table) { --el-table-header-bg-color:#edf2f7; --el-table-border-color:#d5dde8; --el-table-header-text-color:#3c4e66; --el-table-text-color:#172b4d; }
:deep(.el-table .cell) { overflow-wrap:anywhere; }
:deep(.el-pagination) { margin-top:12px; justify-content:flex-end; }
:deep(.el-collapse-item__header) { font-size:13px; font-weight:600; color:#172b4d; }
@media (max-width:640px) {
  :global(.device-connection-drawer .el-drawer__header) { padding:16px; }
  :global(.device-connection-drawer .el-drawer__body) { padding:12px; }
  .connection-section { padding:12px; margin-bottom:12px; }
  :deep(.el-descriptions__label.el-descriptions__cell.is-bordered-label) { width:100px; }
  :deep(.el-descriptions__cell) { padding:9px !important; }
}
</style>
