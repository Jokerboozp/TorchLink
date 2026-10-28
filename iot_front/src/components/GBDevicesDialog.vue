<script setup>
// 国标设备（GB28181）：设备按下方的 SIP 服务器信息向平台注册，注册成功后查询通道目录。
// 注册密码只写不读：编辑时留空保留已保存的密码。
import { computed, reactive, ref, watch } from 'vue'
import { RefreshCw, Plus } from '@lucide/vue'
import { UiMessage } from '../ui/feedback.js'
import { api, formatTime, isAbort, notifyError } from '../api'
import { useListLoader } from '../composables/useListLoader'
import { liveState } from '../liveVideo'
import StatusDot from './layout/StatusDot.vue'
import RowActions from './layout/RowActions.vue'
import { confirmDelete } from '../deleteAction'

const props = defineProps({ modelValue: { type: Boolean, default: false } })
const emit = defineEmits(['update:modelValue', 'changed'])
const visible = computed({ get: () => props.modelValue, set: value => emit('update:modelValue', value) })

const devices = ref([])
const loading = ref(false)
const editorVisible = ref(false)
const editing = ref('')
const saving = ref(false)
const blank = () => ({ deviceId: '', name: '', enabled: true, streamTransport: 'UDP', password: '' })
const form = reactive(blank())
const editingDevice = computed(() => devices.value.find(item => item.deviceId === editing.value) || null)
const sip = computed(() => liveState.status?.gb28181 || null)
const devicePath = id => `/api/v1/integrations/video/gb28181/devices/${encodeURIComponent(id)}`

const loader = useListLoader(loading)
async function load() {
  try {
    const data = await loader.run(signal => api('/api/v1/integrations/video/gb28181/devices', { signal }))
    devices.value = data.items || []
  } catch (error) {
    if (!isAbort(error)) notifyError(error)
  }
}
watch(
  () => props.modelValue,
  open => {
    if (open) load()
  },
  { immediate: true }
)

function deviceState(row) {
  if (!row.enabled) return { tone: 'neutral', label: '已停用' }
  if (row.online) return { tone: 'success', label: '在线' }
  return { tone: row.state?.registeredAt ? 'warning' : 'neutral', label: row.state?.registeredAt ? '离线' : '未注册' }
}

function openEditor(row) {
  Object.assign(
    form,
    blank(),
    row ? { deviceId: row.deviceId, name: row.name, enabled: row.enabled, streamTransport: row.streamTransport || 'UDP' } : {}
  )
  editing.value = row?.deviceId || ''
  editorVisible.value = true
}

async function save() {
  if (saving.value) return
  const id = form.deviceId.trim()
  if (!/^\d{20}$/.test(id)) return UiMessage.warning('设备编号须为 20 位数字，与设备上的 SIP 用户名一致')
  if (!editing.value && !form.password) return UiMessage.warning('请设置注册密码')
  saving.value = true
  try {
    await api(devicePath(id), {
      method: 'PUT',
      body: JSON.stringify({
        name: form.name.trim(),
        enabled: form.enabled,
        streamTransport: form.streamTransport,
        password: form.password
      })
    })
    UiMessage.success('国标设备已保存')
    editorVisible.value = false
    emit('changed')
    await load()
  } catch (error) {
    notifyError(error)
  } finally {
    saving.value = false
  }
}

async function refresh(row) {
  try {
    await api(`${devicePath(row.deviceId)}/refresh`, { method: 'POST' })
    UiMessage.success('已向设备查询通道目录，稍后刷新列表查看')
    setTimeout(load, 2000)
  } catch (error) {
    notifyError(error)
  }
}

function remove(row) {
  return confirmDelete({
    label: row.name || row.deviceId,
    path: devicePath(row.deviceId),
    onDeleted: () => {
      emit('changed')
      return load()
    }
  })
}

function rowActions(row) {
  return [
    {
      key: 'refresh',
      label: '刷新目录',
      hidden: !row.online,
      permission: 'POST /api/v1/integrations/video/gb28181/devices/:deviceId/refresh',
      onClick: () => refresh(row)
    },
    { key: 'edit', label: '编辑', permission: 'PUT /api/v1/integrations/video/gb28181/devices/:deviceId', onClick: () => openEditor(row) },
    {
      key: 'delete',
      label: '删除',
      type: 'danger',
      permission: 'DELETE /api/v1/integrations/video/gb28181/devices/:deviceId',
      onClick: () => remove(row)
    }
  ]
}
</script>

<template>
  <ui-dialog v-model="visible" title="国标设备（GB28181）" width="min(960px, 96vw)">
    <div class="gb-devices">
      <section class="gb-devices__sip" aria-label="SIP 服务器信息">
        <div class="gb-devices__sip-head">
          <strong>平台 SIP 服务器</strong>
          <StatusDot :tone="sip?.running ? 'success' : 'warning'" :label="sip?.running ? '运行中' : '未运行'" />
        </div>
        <dl v-if="sip?.enabled">
          <div>
            <dt>SIP 服务器编号</dt>
            <dd>{{ sip.serverId }}</dd>
          </div>
          <div>
            <dt>SIP 域</dt>
            <dd>{{ sip.domain }}</dd>
          </div>
          <div>
            <dt>SIP 地址与端口</dt>
            <dd>{{ sip.sipHost || '平台主机 IP' }}:{{ sip.sipPort }}（UDP / TCP）</dd>
          </div>
          <div>
            <dt>媒体接收地址</dt>
            <dd>{{ sip.mediaIp || '未配置' }}</dd>
          </div>
        </dl>
        <small
          >{{ sip?.message || '直播模块未部署时无法接入国标设备。' }} 在设备的“平台接入 /
          GB28181”中填写以上信息，设备编号和注册密码与下方登记一致。</small
        >
      </section>

      <div class="gb-devices__toolbar">
        <ui-button size="small" :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
        <ui-button
          v-permission="'PUT /api/v1/integrations/video/gb28181/devices/:deviceId'"
          size="small"
          type="primary"
          @click="openEditor()"
          ><Plus />登记设备</ui-button
        >
      </div>

      <ui-table v-loading="loading" :data="devices" size="small">
        <ui-table-column label="设备" min-width="200"
          ><template #default="{ row }"
            ><b>{{ row.name || row.deviceId }}</b
            ><small class="subline">{{ row.deviceId }}</small></template
          ></ui-table-column
        >
        <ui-table-column label="状态" width="100"
          ><template #default="{ row }"><StatusDot v-bind="deviceState(row)" /></template
        ></ui-table-column>
        <ui-table-column label="厂商 / 型号" min-width="140"
          ><template #default="{ row }">{{
            [row.state?.manufacturer, row.state?.model].filter(Boolean).join(' / ') || '—'
          }}</template></ui-table-column
        >
        <ui-table-column label="通道" width="80"
          ><template #default="{ row }">{{ row.state?.channels?.length || 0 }}</template></ui-table-column
        >
        <ui-table-column label="媒体传输" width="90"
          ><template #default="{ row }">{{ row.streamTransport }}</template></ui-table-column
        >
        <ui-table-column label="最近心跳" min-width="150"
          ><template #default="{ row }"
            >{{ row.state?.lastSeenAt ? formatTime(row.state.lastSeenAt) : '—'
            }}<small v-if="row.state?.remoteAddr" class="subline">{{ row.state.remoteAddr }}</small></template
          ></ui-table-column
        >
        <ui-table-column label="操作" width="190" align="right" fixed="right"
          ><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template
        ></ui-table-column>
        <template #empty><ui-empty description="尚未登记国标设备" /></template>
      </ui-table>
    </div>
    <template #footer><ui-button @click="visible = false">关闭</ui-button></template>
  </ui-dialog>

  <ui-dialog
    v-model="editorVisible"
    :title="editing ? '编辑国标设备' : '登记国标设备'"
    width="min(640px, 94vw)"
    :close-on-click-modal="false"
  >
    <ui-form :model="form" label-position="top" :disabled="saving">
      <div class="gb-devices__grid">
        <ui-form-item label="设备编号（20 位）"
          ><ui-input v-model="form.deviceId" :disabled="Boolean(editing)" placeholder="例如 34020000001320000001"
        /></ui-form-item>
        <ui-form-item label="名称"><ui-input v-model="form.name" placeholder="例如：东门 NVR" /></ui-form-item>
        <ui-form-item label="注册密码"
          ><ui-input
            v-model="form.password"
            type="password"
            show-password
            autocomplete="new-password"
            :placeholder="editing ? '已保存，留空则不修改' : '与设备上配置的 SIP 密码一致'"
        /></ui-form-item>
        <ui-form-item label="媒体传输"
          ><ui-radio-group v-model="form.streamTransport" class="segmented-choice-group"
            ><ui-radio-button value="UDP">UDP</ui-radio-button><ui-radio-button value="TCP">TCP</ui-radio-button></ui-radio-group
          ></ui-form-item
        >
        <ui-form-item label="启用"><ui-switch v-model="form.enabled" /></ui-form-item>
      </div>
      <p class="gb-devices__hint">UDP 适合同一局域网；设备与平台之间有防火墙或丢包时选择 TCP（由设备连接媒体服务）。</p>
      <ui-table v-if="editingDevice?.state?.channels?.length" :data="editingDevice.state.channels" size="small" max-height="240">
        <ui-table-column label="通道" min-width="200"
          ><template #default="{ row }"
            ><b>{{ row.name || row.channelId }}</b
            ><small class="subline">{{ row.channelId }}</small></template
          ></ui-table-column
        >
        <ui-table-column label="状态" width="90"
          ><template #default="{ row }">{{
            row.status === 'ON' ? '在线' : row.status === 'OFF' ? '离线' : row.status || '—'
          }}</template></ui-table-column
        >
        <ui-table-column label="厂商 / 型号" min-width="140"
          ><template #default="{ row }">{{ [row.manufacturer, row.model].filter(Boolean).join(' / ') || '—' }}</template></ui-table-column
        >
      </ui-table>
    </ui-form>
    <template #footer
      ><ui-button :disabled="saving" @click="editorVisible = false">取消</ui-button
      ><ui-button type="primary" :loading="saving" @click="save">保存</ui-button></template
    >
  </ui-dialog>
</template>

<style scoped>
.gb-devices {
  display: grid;
  gap: var(--space-3);
}
.gb-devices__sip {
  display: grid;
  gap: var(--space-2);
  padding: 12px 16px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--surface);
}
.gb-devices__sip-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3);
}
.gb-devices__sip dl {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
  gap: var(--space-2) var(--space-4);
  margin: 0;
}
.gb-devices__sip dt {
  color: var(--text-muted);
  font-size: 12px;
}
.gb-devices__sip dd {
  margin: 2px 0 0;
  color: var(--text);
  font-size: var(--font-size-sm);
  word-break: break-all;
}
.gb-devices__sip small,
.gb-devices__hint {
  margin: 0;
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.6;
}
.gb-devices__toolbar {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: var(--space-2);
}
.gb-devices__grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 0 var(--space-3);
}
.gb-devices :deep(.n-form-item) {
  margin-bottom: 0;
}
</style>
