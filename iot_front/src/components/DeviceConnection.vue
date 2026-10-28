<script setup>
import { useMediaQuery } from '../composables/useMediaQuery'
import { createClientId } from '../clientId'
import LinkedCameras from './LinkedCameras.vue'
import OnboardingDiagnosis from './OnboardingDiagnosis.vue'
import DeviceChildrenPanel from './device-connection/DeviceChildrenPanel.vue'
import DeviceCommandsPanel from './device-connection/DeviceCommandsPanel.vue'
import DeviceCredentialsPanel from './device-connection/DeviceCredentialsPanel.vue'
import DeviceSignalsPanel from './device-connection/DeviceSignalsPanel.vue'
import { can } from '../permissions'
import { commandBody } from '../commandForm'
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { UiMessageBox, UiMessage } from '../ui/feedback.js'
import { api, formatTime, notifyError, pretty, session } from '../api'
import { transportLabel, statusLabel } from '../presentation'
import {
  alarmType,
  alarmLevel,
  alarmStatuses,
  connectionStatuses,
  dataStatuses,
  businessStatuses,
  stateSources,
  messageTypeLabel,
  label
} from '../labels'

const props = defineProps({ deviceId: String })
const emit = defineEmits(['close', 'navigate', 'device'])
const data = ref(null),
  loading = ref(false),
  actionBusy = ref(false),
  error = ref(''),
  selectedProfile = ref('')
const credential = ref(null),
  commandResult = ref(null),
  verification = ref(null),
  verificationBusy = ref(false)
async function verifyDevice() {
  if (loading.value || verificationBusy.value || !canVerify.value) return
  const current = generation
  verificationBusy.value = true
  try {
    const result = await api(`${base()}/verification`, { method: 'POST', body: '{}', signal: controller.signal })
    if (current === generation) verification.value = result
  } catch (cause) {
    if (current === generation && cause.name !== 'AbortError') notifyError(cause)
  } finally {
    if (current === generation) verificationBusy.value = false
  }
}
async function readVerification(current) {
  try {
    const result = await api(`${base()}/verification`, { signal: controller.signal })
    if (current === generation) verification.value = result
  } catch (cause) {
    if (current === generation && cause.name !== 'AbortError') verification.value = null
  }
}
const signals = ref([])
async function loadSignals(current) {
  try {
    const result = await api(`${base()}/signals`, { signal: controller.signal })
    if (current === generation) signals.value = result.items || []
  } catch (cause) {
    if (current === generation && cause.name !== 'AbortError') signals.value = []
  }
}
const commandReply = ref(null)
const commandType = ref('')
const commandValues = ref({})
const operations = computed(() => data.value?.product?.thingModel?.commands || [])
const selectedOperation = computed(() => operations.value.find(item => item.identifier === commandType.value))
watch(commandType, () => {
  commandValues.value = {}
})
const pendingCommand = ref(null),
  pendingProtocol = ref(null)
const lists = reactive(
  Object.fromEntries(
    ['history', 'events', 'commands', 'children'].map(key => [key, { items: [], total: 0, page: 1, loading: false, error: '' }])
  )
)
const narrow = useMediaQuery('(max-width: 640px)')
const columns = computed(() => (narrow.value ? 1 : 2))
// 按实际接口权限决定可用操作，不按角色名判断：自定义角色同样适用，服务端仍逐项校验。
const canVerify = computed(() => can('menu:devices') && can('POST /api/v1/device-registry/:id/verification'))
const canCommand = computed(() =>
  can(['POST /api/v1/device-registry/:id/commands', 'POST /api/v2/device-access-profiles/:id/devices/:deviceId/commands'])
)
const canManageCredentials = computed(() =>
  can(['POST /api/v1/device-registry/:id/credentials', 'DELETE /api/v1/device-registry/:id/credentials'])
)
const isParent = computed(
  () => data.value && !data.value.parent && (data.value.device.deviceRole === 'GATEWAY' || data.value.profile?.childProducts?.length)
)
const standardAccess = computed(() => Boolean(data.value?.accessInfo && !data.value?.parent))
const httpAccess = computed(() => data.value?.accessInfo?.kind === 'managed' || data.value?.connector === 'HTTP')
const childTypes = computed(() => data.value?.profile?.childProducts || [])
const hasSessions = computed(() => Boolean(data.value?.parent || data.value?.profile?.mode === 'listener' || data.value?.sessions?.length))
const properties = computed(() =>
  Object.entries(data.value?.latestProperties?.[0]?.properties || {}).map(([key, value]) => ({ key, value }))
)
const displayValue = value => (typeof value === 'object' && value !== null ? pretty(value) : String(value ?? '—'))
const base = () => `/api/v1/device-registry/${encodeURIComponent(props.deviceId)}`
let generation = 0
let controller = new AbortController()
const revisions = {}

async function loadList(key) {
  const section = lists[key],
    current = generation,
    revision = (revisions[key] || 0) + 1
  revisions[key] = revision
  section.loading = true
  section.error = ''
  section.items = []
  section.total = 0
  const paths = {
    history: `/history?page=${section.page}&pageSize=20`,
    events: `/history?kind=event&page=${section.page}&pageSize=20`,
    commands: `/commands?page=${section.page}&pageSize=20`,
    children: `/children?page=${section.page}&pageSize=20`
  }
  try {
    const result = await api(base() + paths[key], { signal: controller.signal })
    if (current !== generation || revisions[key] !== revision) return
    section.items = result.items || []
    section.total = Number(result.total || 0)
    if (key === 'commands' && commandResult.value?.id)
      commandResult.value = section.items.find(item => item.id === commandResult.value.id) || commandResult.value
  } catch (cause) {
    if (current === generation && revisions[key] === revision && cause.name !== 'AbortError') section.error = cause.message
  } finally {
    if (current === generation && revisions[key] === revision) section.loading = false
  }
}
async function load() {
  const current = ++generation
  controller.abort()
  controller = new AbortController()
  loading.value = true
  verificationBusy.value = false
  error.value = ''
  try {
    const result = await api(
      `${base()}/connection${selectedProfile.value ? `?profileId=${encodeURIComponent(selectedProfile.value)}` : ''}`,
      { signal: controller.signal }
    )
    if (current !== generation) return
    if (!result?.device?.id) throw new Error('设备连接信息不完整，请刷新后重试。')
    data.value = result
    selectedProfile.value = result.profile?.id || ''
    const jobs = [loadList('history'), loadList('events'), readVerification(current), loadSignals(current)]
    if (isParent.value) jobs.push(loadList('children'))
    if (result.connector === 'MQTT') jobs.push(loadList('commands'))
    await Promise.all(jobs)
  } catch (cause) {
    if (current === generation && cause.name !== 'AbortError') {
      data.value = null
      error.value = cause.message
    }
  } finally {
    if (current === generation) loading.value = false
  }
}
function selectProfile() {
  pendingProtocol.value = null
  commandResult.value = null
  commandReply.value = null
  load()
}
function newCommand() {
  pendingCommand.value = null
  pendingProtocol.value = null
  commandResult.value = null
  commandReply.value = null
}
async function showCommandReply() {
  const id = commandResult.value?.rawMessageId
  if (!id) return
  try {
    const reply = await api(`/api/v1/raw-messages/${encodeURIComponent(id)}`, { signal: controller.signal })
    if (commandResult.value?.rawMessageId === id) commandReply.value = reply
  } catch (cause) {
    if (cause.name !== 'AbortError') notifyError(cause)
  }
}
async function action(work, allowed = true) {
  if (actionBusy.value || loading.value || !allowed) return
  actionBusy.value = true
  try {
    await work()
  } catch (cause) {
    if (cause !== 'cancel' && cause !== 'close') notifyError(cause)
  } finally {
    actionBusy.value = false
  }
}
async function rotate() {
  await action(async () => {
    const current = generation,
      identityToken = session.token
    await UiMessageBox.confirm('重新生成后旧凭据立即在平台停用；消息服务撤销结果可在下方查看。', '重新生成凭据')
    if (current !== generation || session.token !== identityToken) return
    const result = await api(`${base()}/credentials`, { method: 'POST', signal: controller.signal })
    if (current !== generation || session.token !== identityToken) return
    credential.value = result.credential
    await load()
  }, can('POST /api/v1/device-registry/:id/credentials'))
}
async function disable() {
  await action(async () => {
    await UiMessageBox.confirm('禁用后设备不能使用此凭据上报或换取MQTT 令牌。', '禁用设备凭据')
    await api(`${base()}/credentials`, { method: 'DELETE' })
    credential.value = null
    UiMessage.success('凭据已禁用')
    await load()
  }, can('DELETE /api/v1/device-registry/:id/credentials'))
}
async function sendMQTT() {
  await action(async () => {
    const type = commandType.value.trim()
    if (!type) throw new Error('请填写命令类型')
    const body = commandBody(selectedOperation.value, commandValues.value)
    commandReply.value = null
    await UiMessageBox.confirm('确认向该设备发送此命令？发送成功不代表执行成功。', '人工确认命令')
    const signature = JSON.stringify(body)
    if (!pendingCommand.value || pendingCommand.value.signature !== signature) pendingCommand.value = { signature, id: createClientId() }
    commandResult.value = await api(`${base()}/commands`, {
      method: 'POST',
      body: JSON.stringify({ ...body, id: pendingCommand.value.id, confirmed: true })
    })
    await loadList('commands')
  }, can('POST /api/v1/device-registry/:id/commands'))
}
async function send() {
  await action(async () => {
    const form = commandBody(selectedOperation.value, commandValues.value)
    if (Object.keys(form.data).some(key => ['type', 'confirmed', 'requestId', '_scheduled'].includes(key)))
      throw new Error('命令参数包含保留字段，请检查产品命令定义')
    const body = { ...form.data, type: form.type }
    commandReply.value = null
    if (typeof body.type !== 'string' || !body.type.trim()) throw new Error('请在协议命令中填写 type')
    await UiMessageBox.confirm('确认向该设备发送协议命令？请核对设备与参数。', '人工确认命令')
    const profileId = data.value.profile.id,
      signature = JSON.stringify([profileId, props.deviceId, body])
    if (!pendingProtocol.value || pendingProtocol.value.signature !== signature) pendingProtocol.value = { signature, id: createClientId() }
    commandResult.value = await api(
      `/api/v2/device-access-profiles/${encodeURIComponent(profileId)}/devices/${encodeURIComponent(props.deviceId)}/commands`,
      { method: 'POST', body: JSON.stringify({ ...body, requestId: pendingProtocol.value.id, confirmed: true }) }
    )
  }, can('POST /api/v2/device-access-profiles/:id/devices/:deviceId/commands'))
}
function changeListPage(key, page) {
  lists[key].page = page
  loadList(key)
}
watch(
  () => props.deviceId,
  () => {
    data.value = null
    selectedProfile.value = ''
    credential.value = null
    verification.value = null
    verificationBusy.value = false
    commandType.value = ''
    commandValues.value = {}
    newCommand()
    for (const section of Object.values(lists)) Object.assign(section, { items: [], total: 0, page: 1, error: '', loading: false })
    load()
  },
  { immediate: true }
)
onBeforeUnmount(() => {
  generation++
  controller.abort()
  credential.value = null
})
</script>

<template>
  <ui-drawer :model-value="true" class="device-connection-drawer" title="设备连接与数据" size="min(900px, 100vw)" @close="emit('close')">
    <div class="device-connection" v-loading="loading">
      <div class="connection-toolbar">
        <div>
          <strong>{{ data?.device?.name || '设备详情' }}</strong
          ><small>{{ props.deviceId }}</small>
        </div>
        <ui-button :loading="loading" :disabled="actionBusy" @click="load">刷新</ui-button>
      </div>
      <ui-alert v-if="error" title="设备详情加载失败" :description="error" type="error" :closable="false" show-icon />
      <ui-empty v-if="!data && !loading && !error" description="暂无设备信息" />
      <template v-if="data">
        <section class="connection-section device-summary">
          <h3>当前接入状态</h3>
          <OnboardingDiagnosis
            @navigate="(page, detail) => emit('navigate', page, detail)"
            :status="data"
            :verification="verification"
            :verification-busy="verificationBusy"
            :can-verify="canVerify"
            @refresh="load"
            @verify="verifyDevice"
            @raw="emit('navigate', 'raw', { deviceId: props.deviceId, rawMessageId: data.ingest?.rawMessageId })"
          />
          <div class="connection-status-grid" role="status">
            <div>
              <span>业务状态</span><strong>{{ label(businessStatuses, data.connection?.businessStatus) || '未知' }}</strong>
            </div>
            <div>
              <span>连接状态</span><strong>{{ label(connectionStatuses, data.connection?.connectionStatus) || '未知' }}</strong>
            </div>
            <div>
              <span>数据状态</span><strong>{{ label(dataStatuses, data.connection?.dataStatus) || '未知' }}</strong>
            </div>
            <div>
              <span>最近上报</span><strong>{{ formatTime(data.connection?.lastSeenAt) }}</strong>
            </div>
            <div>
              <span>原文接收</span><strong>{{ data.ingest?.rawReceived ? '已收到' : '等待上报' }}</strong>
            </div>
            <div>
              <span>解析状态</span
              ><strong>{{
                data.ingest?.parsed ? '已完成' : data.ingest?.parseError ? '失败' : data.ingest?.rawReceived ? '等待处理' : '等待上报'
              }}</strong>
            </div>
          </div>
          <h4 class="connection-subtitle">设备与协议</h4>
          <ui-descriptions :column="columns" border>
            <ui-descriptions-item label="设备">{{ data.device.name || props.deviceId }}</ui-descriptions-item>
            <ui-descriptions-item label="设备模板">{{ data.product?.name || data.device.productId || '—' }}</ui-descriptions-item>
            <ui-descriptions-item label="接入方式">{{ transportLabel(data.connector) || '未配置' }}</ui-descriptions-item>
            <ui-descriptions-item label="创建时间">{{ formatTime(data.device.createdAt) }}</ui-descriptions-item>
            <ui-descriptions-item label="当前绑定协议">{{ data.protocolId || '—' }}</ui-descriptions-item>
            <ui-descriptions-item label="绑定版本">{{ data.protocolVersion || '—' }}</ui-descriptions-item>
            <ui-descriptions-item label="最后连接">{{ formatTime(data.connection?.lastConnectAt) }}</ui-descriptions-item>
            <ui-descriptions-item label="最后断开">{{ formatTime(data.connection?.lastDisconnectAt) }}</ui-descriptions-item>
            <ui-descriptions-item v-if="data.profile" label="接入点">{{ data.profile.id }}</ui-descriptions-item>
            <ui-descriptions-item v-if="data.profile" label="接入点运行状态">{{
              data.profile.runtimeStatus ? statusLabel(data.profile.runtimeStatus) : '待确认'
            }}</ui-descriptions-item>
            <ui-descriptions-item v-if="data.profile?.collectorId" label="采集器">{{ data.profile.collectorId }}</ui-descriptions-item>
            <ui-descriptions-item v-if="data.parent" label="所属主设备"
              ><ui-button link type="primary" @click="emit('device', data.parent.id)">{{
                data.parent.name || data.parent.id
              }}</ui-button></ui-descriptions-item
            >
            <ui-descriptions-item v-if="data.parent" label="子设备地址">{{ data.device.childAddress || '—' }}</ui-descriptions-item>
          </ui-descriptions>
          <ui-alert
            v-if="data.profile?.lastError || data.ingest?.parseError"
            class="section-feedback"
            title="最近接入异常"
            :description="data.profile?.lastError || data.ingest?.parseError"
            type="warning"
            :closable="false"
            show-icon
          />
          <div v-if="data.profiles?.length > 1" class="profile-picker">
            <p>设备关联了多个接入点，请根据用途、地址和状态选择。</p>
            <ui-select v-model="selectedProfile" :disabled="loading || actionBusy" placeholder="选择接入点" @change="selectProfile"
              ><ui-option
                v-for="p in data.profiles"
                :key="p.id"
                :value="p.id"
                :label="`${p.id} · ${p.host}:${p.port} · ${p.runtimeStatus || '待确认'}`"
            /></ui-select>
          </div>
        </section>

        <section class="connection-section device-cameras">
          <h3>关联摄像头</h3>
          <LinkedCameras :device-id="data.device.id" />
        </section>

        <section v-if="standardAccess" class="connection-section device-access-info">
          <h3>设备接入信息</h3>
          <ui-descriptions :column="1" border>
            <ui-descriptions-item v-if="httpAccess" label="上报接口"
              ><code>{{ data.accessInfo.httpUrl || '未配置平台对外 HTTP 地址' }}</code></ui-descriptions-item
            >
            <ui-descriptions-item v-if="data.connector === 'MQTT'" label="消息服务地址"
              ><code>{{ data.accessInfo.mqttBroker || '未配置对外地址' }}</code></ui-descriptions-item
            >
            <ui-descriptions-item v-if="data.connector === 'MQTT'" label="客户端标识"
              ><code>{{ data.accessInfo.clientId }}</code></ui-descriptions-item
            >
            <ui-descriptions-item label="接入密钥"
              ><code>{{ data.accessInfo.username }}</code></ui-descriptions-item
            >
            <ui-descriptions-item v-if="data.connector === 'MQTT'" label="上行 Topic"
              ><code>{{ data.accessInfo.upTopic }}</code></ui-descriptions-item
            >
            <ui-descriptions-item v-if="data.connector === 'MQTT'" label="下行 Topic"
              ><code>{{ data.accessInfo.downTopic }}</code></ui-descriptions-item
            >
            <ui-descriptions-item label="凭据状态">{{ data.credentialEnabled ? '已启用' : '已禁用' }}</ui-descriptions-item>
          </ui-descriptions>
          <p v-if="data.connector === 'MQTT'">
            设备先用 AccessKey 和 Secret 调用 {{ data.accessInfo.tokenEndpoint }} 换取短期 MQTT token，再以返回的 username 和 token 连接
            Broker；Secret 不能直接作为 MQTT 密码。
          </p>
        </section>

        <DeviceChildrenPanel
          v-if="isParent"
          :list="lists.children"
          :child-types="childTypes"
          :base="base()"
          @page="page => changeListPage('children', page)"
          @added="loadList('children')"
          @device="id => emit('device', id)"
        />

        <section v-if="hasSessions" class="connection-section device-sessions">
          <h3>{{ data.parent ? '主设备通信会话' : '在线会话' }}</h3>
          <ui-table :data="data.sessions || []" border empty-text="暂无已识别的在线会话">
            <ui-table-column prop="profileId" label="接入点" min-width="145" /><ui-table-column
              prop="remoteAddress"
              label="远端地址"
              min-width="155"
            />
            <ui-table-column prop="protocolId" label="会话协议" min-width="130" /><ui-table-column
              prop="protocolVersion"
              label="会话版本"
              min-width="100"
            />
            <ui-table-column label="最后有效报文" min-width="175"
              ><template #default="{ row }">{{ formatTime(row.lastSeenAt) }}</template></ui-table-column
            >
          </ui-table>
        </section>

        <DeviceSignalsPanel v-if="signals.length" :signals="signals" />

        <section class="connection-section device-properties">
          <h3>
            最新属性 <small>{{ formatTime(data.latestProperties?.[0]?.timestamp) }}</small>
          </h3>
          <ui-table :data="properties" border empty-text="暂无已解析的属性报文">
            <ui-table-column prop="key" label="属性" :width="narrow ? 105 : 200" /><ui-table-column label="值" min-width="120"
              ><template #default="{ row }"
                ><span class="field-value">{{ displayValue(row.value) }}</span></template
              ></ui-table-column
            >
          </ui-table>
        </section>

        <section class="connection-section device-latest">
          <h3>最新报文</h3>
          <ui-descriptions v-if="data.latest?.messageId" :column="columns" border>
            <ui-descriptions-item label="消息类型">{{ messageTypeLabel(data.latest.messageType) }}</ui-descriptions-item
            ><ui-descriptions-item label="时间">{{ formatTime(data.latest.timestamp) }}</ui-descriptions-item>
            <ui-descriptions-item label="标准消息标识" :span="columns"
              ><code>{{ data.latest.messageId }}</code></ui-descriptions-item
            >
          </ui-descriptions>
          <ui-empty v-else description="暂无已解析报文" :image-size="48" />
          <ui-collapse v-if="data.latest?.messageId" class="message-detail"
            ><ui-collapse-item title="查看完整标准消息" name="message">
              <pre>{{ pretty(data.latest) }}</pre>
            </ui-collapse-item></ui-collapse
          >
          <div class="section-actions">
            <ui-button v-permission="'menu:raw'" @click="emit('navigate', 'raw', { deviceId: props.deviceId })">原始报文与回放</ui-button
            ><ui-button v-permission="'menu:alarms'" @click="emit('navigate', 'alarms', { deviceId: props.deviceId })">设备告警</ui-button>
          </div>
        </section>

        <section class="connection-section device-history" v-loading="lists.history.loading">
          <h3>连接与状态历史</h3>
          <ui-alert v-if="lists.history.error" title="状态历史加载失败" :description="lists.history.error" type="error" :closable="false" />
          <ui-table v-else :data="lists.history.items" border empty-text="暂无状态变更">
            <ui-table-column label="记录时间" min-width="175"
              ><template #default="{ row }">{{ formatTime(row.recordedAt) }}</template></ui-table-column
            >
            <ui-table-column label="连接" min-width="100"
              ><template #default="{ row }">{{ label(connectionStatuses, row.state?.connectionStatus) }}</template></ui-table-column
            >
            <ui-table-column label="业务" min-width="100"
              ><template #default="{ row }">{{ label(businessStatuses, row.state?.businessStatus) }}</template></ui-table-column
            >
            <ui-table-column label="来源" min-width="100"
              ><template #default="{ row }">{{ label(stateSources, row.state?.statusSource) }}</template></ui-table-column
            >
          </ui-table>
          <ui-pagination
            v-if="lists.history.total > 20"
            v-model:current-page="lists.history.page"
            :page-size="20"
            :total="lists.history.total"
            layout="prev, pager, next"
            @current-change="loadList('history')"
          />
        </section>

        <section class="connection-section device-events" v-loading="lists.events.loading">
          <h3>最近事件</h3>
          <ui-alert v-if="lists.events.error" title="事件加载失败" :description="lists.events.error" type="error" :closable="false" />
          <ui-table v-else :data="lists.events.items" border empty-text="暂无事件">
            <ui-table-column label="时间" min-width="175"
              ><template #default="{ row }">{{ formatTime(row.timestamp) }}</template></ui-table-column
            >
            <ui-table-column label="事件内容" min-width="200"
              ><template #default="{ row }"
                ><span class="field-value">{{ pretty(row.event || {}) }}</span></template
              ></ui-table-column
            >
          </ui-table>
          <ui-pagination
            v-if="lists.events.total > 20"
            v-model:current-page="lists.events.page"
            :page-size="20"
            :total="lists.events.total"
            layout="prev, pager, next"
            @current-change="loadList('events')"
          />
        </section>

        <section class="connection-section device-alarms">
          <h3>最近告警</h3>
          <ui-table :data="data.recentAlarms || []" border empty-text="暂无告警">
            <ui-table-column label="告警" min-width="100"
              ><template #default="{ row }">{{ alarmType(row.alarmType) }}</template></ui-table-column
            ><ui-table-column label="级别" min-width="90"
              ><template #default="{ row }">{{ alarmLevel(row.alarmLevel) }}</template></ui-table-column
            >
            <ui-table-column label="状态" min-width="100"
              ><template #default="{ row }">{{ label(alarmStatuses, row.status) }}</template></ui-table-column
            ><ui-table-column label="最近触发" min-width="175"
              ><template #default="{ row }">{{ formatTime(row.lastTriggeredAt) }}</template></ui-table-column
            >
          </ui-table>
        </section>

        <DeviceCommandsPanel
          v-if="operations.length && (data.connector === 'MQTT' || (data.profile && data.canCommand))"
          v-model:type="commandType"
          v-model:values="commandValues"
          :data="data"
          :operations="operations"
          :selected-operation="selectedOperation"
          :list="lists.commands"
          :can-command="canCommand"
          :loading="loading"
          :action-busy="actionBusy"
          :pending="Boolean(pendingCommand || pendingProtocol)"
          :command-result="commandResult"
          :command-reply="commandReply"
          @send-mqtt="sendMQTT"
          @send="send"
          @reset="newCommand"
          @reply="showCommandReply"
          @page="page => changeListPage('commands', page)"
        />

        <DeviceCredentialsPanel
          v-if="standardAccess && canManageCredentials"
          :data="data"
          :credential="credential"
          :disabled="loading || actionBusy"
          @disable="disable"
          @rotate="rotate"
        />
      </template>
    </div>
  </ui-drawer>
</template>

<style scoped src="./device-connection/connection-section.css"></style>
<style scoped>
:global(.device-connection-drawer .n-drawer-body-content-wrapper) {
  background: var(--bg);
}
.device-connection {
  min-width: 0;
  color: var(--text);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-normal);
}
.connection-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  margin-bottom: var(--space-4);
}
.connection-toolbar > div {
  min-width: 0;
}
.connection-toolbar strong {
  display: block;
  color: var(--text-strong);
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-semibold);
}
.connection-toolbar small {
  display: block;
  color: var(--text-muted);
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  overflow-wrap: anywhere;
}
.connection-status-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--space-2);
  margin-bottom: var(--space-5);
}
.connection-status-grid > div {
  min-width: 0;
  padding: 10px var(--space-3);
  background: var(--surface-muted);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
}
.connection-status-grid span {
  display: block;
  margin-bottom: 2px;
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}
.connection-status-grid strong {
  display: block;
  color: var(--text-strong);
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  line-height: var(--line-height-tight);
  overflow-wrap: anywhere;
}
.connection-subtitle {
  margin: 0 0 10px;
  color: var(--text-strong);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
}
@media (max-width: 767px) {
  .connection-status-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 6px;
    margin-bottom: var(--space-4);
  }
}
</style>
